package main

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

const defaultWikiCommitMessage = "docs(wiki): {action} — {summary}"

var (
	errNotWikiRepo     = errors.New("not inside a git repo")
	errNothingToCommit = errors.New("no wiki changes to commit")
)

func gitWiki(wikiRoot string, args ...string) ([]byte, error) {
	return exec.Command("git", append([]string{"-C", wikiRoot}, args...)...).CombinedOutput()
}

// commitWiki auto-commits after a wiki write. Opt-in: no-op unless the user
// enabled wiki_auto_commit (memoria init --auto-commit) — otherwise the wiki
// is theirs to commit, with memoria commit or by hand. Any git failure
// (identity unset, index.lock race, not a repo) is logged and swallowed:
// auto-commit must never break an apply. Var so tests can stub it.
var commitWiki = func(cfg config, wikiRoot, action, summary string, count int) {
	if !cfg.WikiAutoCommit {
		return
	}
	if err := commitWikiGit(wikiRoot, wikiCommitMessage(cfg, wikiRoot, action, summary, count)); err != nil {
		logf("wiki-commit", "%s: %v", wikiRoot, err)
	}
}

// wikiCommitMessage renders the configured pattern for one wiki change.
func wikiCommitMessage(cfg config, wikiRoot, action, summary string, count int) string {
	msg := cfg.WikiCommitMessage
	if msg == "" {
		msg = defaultWikiCommitMessage
	}
	return strings.NewReplacer(
		"{action}", action,
		"{summary}", summary,
		"{count}", fmt.Sprint(count),
		"{project}", filepath.Base(filepath.Dir(wikiRoot)),
	).Replace(msg)
}

// commitWikiGit stages the wiki folder and commits it with msg.
func commitWikiGit(wikiRoot, msg string) error {
	changed, err := wikiChanges(wikiRoot)
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		return errNothingToCommit
	}
	if out, err := gitWiki(wikiRoot, "add", "-A", "."); err != nil {
		return fmt.Errorf("add: %v (%s)", err, collapse(string(out), 200))
	}
	// pathspec commit: only wiki paths land in it — the user's staged
	// non-wiki files stay staged and untouched
	if out, err := gitWiki(wikiRoot, "commit", "-m", msg, "--", "."); err != nil {
		return fmt.Errorf("commit: %v (%s)", err, collapse(string(out), 200))
	}
	logf("wiki-commit", "%s: %s", wikiRoot, msg)
	return nil
}

// wikiChanges lists the wiki's modified and new pages, relative to the wiki
// folder. errNotWikiRepo when the wiki isn't inside a git repo (an ancestor
// .git counts).
func wikiChanges(wikiRoot string) ([]string, error) {
	if _, err := gitWiki(wikiRoot, "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil, errNotWikiRepo
	}
	// -uall: without it a whole new folder collapses into one entry.
	// -z: NUL-separated and never quoted, so non-ASCII page names survive.
	out, err := exec.Command("git", "-C", wikiRoot, "status", "--porcelain", "-z", "-uall", "--", ".").Output()
	if err != nil {
		return nil, fmt.Errorf("status: %v", err)
	}
	// porcelain paths are repo-root relative; the prefix makes them wiki-relative
	pre, _ := exec.Command("git", "-C", wikiRoot, "rev-parse", "--show-prefix").Output()
	prefix := strings.TrimSpace(string(pre))

	var paths []string
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		if f[0] == 'R' || f[0] == 'C' {
			i++ // rename/copy: the field after the new path is the old one
		}
		paths = append(paths, strings.TrimPrefix(f[3:], prefix))
	}
	return paths, nil
}

// pageSummary renders "N page(s) (a, b, c, …)" capped at three paths.
func pageSummary(paths []string) string {
	shown := paths
	suffix := ""
	if len(shown) > 3 {
		shown = shown[:3]
		suffix = ", …"
	}
	return fmt.Sprintf("%d page(s) (%s%s)", len(paths), strings.Join(shown, ", "), suffix)
}
