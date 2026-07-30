package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

const defaultWikiCommitMessage = "docs(wiki): {action} — {summary}"

// commitWiki stages the wiki folder and commits it with the configured
// message pattern. No-op unless the wiki sits inside a git repo (an ancestor
// .git counts — hasGitDir only checks one dir); any git failure (identity
// unset, index.lock race, nothing to commit) is logged and swallowed —
// auto-commit must never break an apply. Var so tests can stub it.
var commitWiki = func(cfg config, wikiRoot, action, summary string, count int) {
	git := func(args ...string) ([]byte, error) {
		return exec.Command("git", append([]string{"-C", wikiRoot}, args...)...).CombinedOutput()
	}
	if _, err := git("rev-parse", "--is-inside-work-tree"); err != nil {
		return
	}
	if out, err := git("status", "--porcelain", "--", "."); err != nil || len(out) == 0 {
		return
	}
	msg := cfg.WikiCommitMessage
	if msg == "" {
		msg = defaultWikiCommitMessage
	}
	msg = strings.NewReplacer(
		"{action}", action,
		"{summary}", summary,
		"{count}", fmt.Sprint(count),
		"{project}", filepath.Base(filepath.Dir(wikiRoot)),
	).Replace(msg)
	if out, err := git("add", "-A", "."); err != nil {
		logf("wiki-commit", "%s: add: %v (%s)", wikiRoot, err, collapse(string(out), 200))
		return
	}
	// pathspec commit: only wiki paths land in it — the user's staged
	// non-wiki files stay staged and untouched
	if out, err := git("commit", "-m", msg, "--", "."); err != nil {
		logf("wiki-commit", "%s: commit: %v (%s)", wikiRoot, err, collapse(string(out), 200))
		return
	}
	logf("wiki-commit", "%s: %s", wikiRoot, msg)
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
