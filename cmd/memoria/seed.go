package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed prompts/seed-prompt.md
var defaultSeedPrompt string

var seedOptions = []option{
	{"no", "No", "start with an empty wiki"},
	{"yes", "Yes", "may take a while — runs in this terminal, don't close it"},
}

func hasCommits(dir string) bool {
	return exec.Command("git", "-C", dir, "rev-parse", "--verify", "HEAD").Run() == nil
}

// maybeSeedWiki offers to generate the wiki from git history right after
// bootstrap registers the project: TTY yes/no then a foreground spinner, or
// detached with --background (implies yes, works without a TTY). Foreground
// is best-effort — a refusal or failure never fails bootstrap.
func maybeSeedWiki(cfg config, p project, configPath string, background bool, out io.Writer) int {
	if cfg.Processor == "" {
		return 0
	}
	if !hasCommits(p.Path) {
		fmt.Fprintln(out, "note: no git history in "+p.Path+" — skipping wiki seed")
		return 0
	}
	if background {
		return detachSeed(p.Path, configPath, p.Name, out)
	}
	if !isTTY() {
		return 0
	}
	v, err := selectOption("Auto-generate the wiki from the existing git history and code?", seedOptions)
	if err != nil || v != "yes" {
		return 0
	}
	wikiName := p.Wiki
	if wikiName == "" {
		wikiName = "wiki"
	}
	var rationale string
	err = withSpinner("Generating wiki from git history and code (this can take a few minutes)...", func() error {
		var serr error
		rationale, serr = seedWiki(cfg, p.Path, filepath.Join(p.Path, wikiName), configPath, out)
		return serr
	})
	if err != nil {
		fmt.Fprintln(out, "error: wiki generation failed:", err)
		return 0
	}
	if rationale != "" {
		fmt.Fprintln(out, rationale)
	}
	return 0
}

// detachSeed hands the slow part to a detached child running
// `bootstrap --seed-foreground`. Shares the per-project status entry with
// process/lint — one background job per project at a time.
func detachSeed(cwd, configPath, projName string, out io.Writer) int {
	sPath := statusPath(configPath)
	if st, _ := loadStatus(sPath); st[projName].State == "running" && pidAlive(st[projName].PID) {
		fmt.Fprintf(out, "error: processing already running for %s (pid %d)\n", projName, st[projName].PID)
		return 1
	}
	pid, err := spawnDetached(cwd, runLogPath(configPath, projName), "bootstrap", "--seed-foreground")
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if err := statusSet(sPath, projName, "running", pid, ""); err != nil {
		logf("seed", "%s: status: %v", projName, err)
	}
	logf("seed", "%s: detached pid %d", projName, pid)
	fmt.Fprintf(out, "Generating the wiki from git history in background (pid %d) — this can take a few minutes.\n", pid)
	fmt.Fprintln(out, "Follow with: memoria status")
	return 0
}

// runSeedForeground is the detached child: seed the tracked project at cwd,
// reporting through status.yaml like process/lint.
func runSeedForeground(cfg config, cwd, configPath string, out io.Writer) int {
	proj := matchProject(cwd, cfg.Projects)
	if proj == "" {
		fmt.Fprintln(out, "error: not inside a tracked project (run memoria bootstrap first)")
		return 1
	}
	p := projectAt(cfg, proj)
	fail := func(err error) int {
		fmt.Fprintln(out, "error:", err)
		logf("seed", "%s: %v", p.Name, err)
		if serr := statusSet(statusPath(configPath), p.Name, "error", 0, collapse(err.Error(), 300)); serr != nil {
			logf("seed", "%s: status: %v", p.Name, serr)
		}
		notify(cfg, "memoria", p.Name+": wiki seeding failed — see memoria status")
		return 1
	}
	wikiName := p.Wiki
	if wikiName == "" {
		wikiName = "wiki"
	}
	rationale, err := seedWiki(cfg, proj, filepath.Join(proj, wikiName), configPath, out)
	if err != nil {
		return fail(err)
	}
	detail := "wiki seeded"
	if rationale != "" {
		detail += " — " + rationale
	}
	if serr := statusSet(statusPath(configPath), p.Name, "done", 0, collapse(detail, 300)); serr != nil {
		logf("seed", "%s: status: %v", p.Name, serr)
	}
	notify(cfg, "memoria", "Wiki seeded for "+p.Name)
	logf("seed", "%s: wiki seeded", p.Name)
	return 0
}

// seedWiki asks the processor for wiki pages built from the repo's committed
// content, then validates and writes them (same trust boundary as
// process --apply). Returns the model's rationale.
func seedWiki(cfg config, dir, wikiRoot, configPath string, out io.Writer) (string, error) {
	rules, err := loadPromptFile(configPath, "seed-prompt.md", defaultSeedPrompt)
	if err != nil {
		return "", err
	}
	raw, err := invokeProcessor(cfg, buildSeedPrompt(rules, dir))
	if err != nil {
		return "", err
	}
	jsonStr, err := extractJSON(raw)
	if err != nil {
		return "", err
	}
	var pp struct {
		Pages     []wikiPage `json:"pages"`
		Rationale string     `json:"rationale"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &pp); err != nil {
		return "", fmt.Errorf("processor returned invalid JSON: %w", err)
	}
	if err := validatePages(pp.Pages); err != nil {
		return "", err
	}
	for _, pg := range pp.Pages {
		dst := filepath.Join(wikiRoot, filepath.FromSlash(pg.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(dst, []byte(renderPage(pg.Tags, pg.BodyMarkdown)), 0o644); err != nil {
			return "", err
		}
		fmt.Fprintf(out, "wrote %s\n", dst)
	}
	fmt.Fprintf(out, "Wiki seeded with %d page(s) in %s\n", len(pp.Pages), wikiRoot)
	return pp.Rationale, nil
}

// buildSeedPrompt reads everything from HEAD, never the working tree — the
// background run must not capture files the user is mid-editing.
// ponytail: context = git log + file tree + README; deep per-file code
// reading when this proves too shallow.
func buildSeedPrompt(rules, dir string) string {
	git := func(args ...string) string {
		out, _ := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
		return string(out)
	}
	var b strings.Builder
	b.WriteString(rules)
	b.WriteString("\n\nThe wiki is empty. Seed it from this project's git history and code:\n")
	b.WriteString("\n--- GIT LOG (newest first) ---\n" + git("log", "--oneline", "-n", "300"))
	b.WriteString("\n--- FILES ---\n" + git("ls-tree", "-r", "--name-only", "HEAD"))
	for _, n := range []string{"README.md", "README"} {
		if rb := git("show", "HEAD:"+n); rb != "" {
			b.WriteString("\n--- " + n + " ---\n" + rb + "\n")
			break
		}
	}
	return b.String()
}

