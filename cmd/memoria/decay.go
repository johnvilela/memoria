package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// first clock seam in the codebase; var so tests can pin the date
var now = time.Now

func today() string { return now().Format("2006-01-02") }

// upsertFrontLine inserts or replaces the "key:" line in a page's YAML
// frontmatter, synthesizing the block when the page has none.
func upsertFrontLine(content, key, line string) string {
	rest, ok := strings.CutPrefix(content, "---\n")
	if !ok {
		return "---\n" + line + "\n---\n\n" + content
	}
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "---\n" + line + "\n---\n\n" + content
	}
	var lines []string
	replaced := false
	for _, l := range strings.Split(rest[:end], "\n") {
		if strings.HasPrefix(l, key+":") {
			l, replaced = line, true
		}
		lines = append(lines, l)
	}
	if !replaced {
		lines = append(lines, line)
	}
	return "---\n" + strings.Join(lines, "\n") + rest[end:]
}

// pageLastUsed reads a page's lastUsed date, "" when absent.
func pageLastUsed(content string) string {
	front, _ := splitFrontmatter(content)
	return frontKey(front, "lastUsed")
}

// touchLastUsed stamps today on a delivered sessions/ page. Best-effort and
// silent: non-sessions paths (incl. trash/), missing files and already-today
// pages are no-ops. Never commits — the user owns the commit, and date
// granularity caps the noise at one write per page per day.
// ponytail: lock-free last-writer-wins like every other wiki write; per-wiki
// flock if a real clobber ever shows up.
func touchLastUsed(wikiRoot, relPath string) {
	if !strings.HasPrefix(relPath, "sessions/") {
		return
	}
	p := filepath.Join(wikiRoot, filepath.FromSlash(relPath))
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	if pageLastUsed(string(b)) == today() {
		return
	}
	_ = os.WriteFile(p, []byte(upsertFrontLine(string(b), "lastUsed", "lastUsed: "+today())), 0o644)
}

// stampSessions returns content carrying the deterministic lastUsed line for
// a sessions/ page: the existing page's date when it has one, else today.
// The upsert also overrides any LLM-authored lastUsed line, so the field
// stays memoria's regardless of what a processor emits. Other paths pass
// through untouched.
func stampSessions(wikiRoot, relPath, content string) string {
	if !strings.HasPrefix(relPath, "sessions/") {
		return content
	}
	lu := today()
	if b, err := os.ReadFile(filepath.Join(wikiRoot, filepath.FromSlash(relPath))); err == nil {
		if d := pageLastUsed(string(b)); d != "" {
			lu = d
		}
	}
	return upsertFrontLine(content, "lastUsed", "lastUsed: "+lu)
}

// writeWikiPage is the chokepoint for every renderPage-based wiki write:
// MkdirAll + WriteFile with the sessions lastUsed stamp applied.
func writeWikiPage(wikiRoot, relPath string, tags []string, body string) error {
	dst := filepath.Join(wikiRoot, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, []byte(stampSessions(wikiRoot, relPath, renderPage(tags, body))), 0o644)
}

// decayDays applies the 15/30 defaults; 0 or absent means default.
func decayDays(cfg config) (soft, hard int) {
	soft, hard = cfg.DecaySoftDays, cfg.DecayHardDays
	if soft == 0 {
		soft = 15
	}
	if hard == 0 {
		hard = 30
	}
	return soft, hard
}

// agedPast reports whether lu is a valid date more than days old; ok=false
// means the stamp is missing or garbled and the page should be adopted.
// Day-granular on both sides, so a page ages at midnight, not mid-day.
func agedPast(lu string, days int) (aged, ok bool) {
	t, err := time.Parse("2006-01-02", lu)
	if err != nil {
		return false, false
	}
	tod, _ := time.Parse("2006-01-02", today())
	return t.AddDate(0, 0, days).Before(tod), true
}

// decaySweep ages a wiki's episodic pages: sessions/ pages unused for soft
// days move to trash/, trashed ones unused for hard days are removed for
// good (a trashed page is hidden from search, so its date freezes there).
// Unstamped pages are adopted — stamped today, never deleted on first sight —
// which also makes the first run on a pre-existing wiki safe. Deterministic
// file reads and date compares; runs only from processAll (the cron's
// background context), never in an agent-facing call.
func decaySweep(cfg config, wikiRoot string, out io.Writer) {
	soft, hard := decayDays(cfg)
	var changed []string
	adopt := func(p, rel, content string) {
		_ = os.WriteFile(p, []byte(upsertFrontLine(content, "lastUsed", "lastUsed: "+today())), 0o644)
		changed = append(changed, rel)
	}
	sweep := func(subdir string, days int, expire func(p, rel string)) {
		dir := filepath.Join(wikiRoot, filepath.FromSlash(subdir))
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			rel := subdir + "/" + e.Name()
			p := filepath.Join(dir, e.Name())
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			aged, ok := agedPast(pageLastUsed(string(b)), days)
			if !ok {
				adopt(p, rel, string(b))
			} else if aged {
				expire(p, rel)
			}
		}
	}
	// hard pass first: a page the soft pass trashes below always survives
	// its own sweep and is purged by a later one — a recovery window even
	// for pages already past both thresholds when the first sweep runs
	sweep("trash/sessions", hard, func(p, rel string) {
		if err := os.Remove(p); err != nil {
			logf("decay", "%s: %v", rel, err)
			return
		}
		fmt.Fprintf(out, "purged %s (unused > %d days)\n", rel, hard)
		changed = append(changed, rel)
	})
	sweep("sessions", soft, func(p, rel string) {
		if _, err := trashPage(wikiRoot, rel); err != nil {
			logf("decay", "%s: %v", rel, err)
			return
		}
		fmt.Fprintf(out, "decayed %s → trash/ (unused > %d days)\n", rel, soft)
		changed = append(changed, rel)
	})
	if len(changed) > 0 {
		commitWiki(cfg, wikiRoot, "decay sweep", pageSummary(changed), len(changed))
	}
}
