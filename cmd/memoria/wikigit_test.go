package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// wikiRepo returns a git repo (with identity set repo-locally, since
// commitWiki runs plain git) holding an uncommitted wiki/index.md.
func wikiRepo(t *testing.T) (dir, wikiRoot string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir = gitDir(t, true)
	for _, kv := range [][]string{{"user.email", "t@t"}, {"user.name", "t"}} {
		if out, err := exec.Command("git", "-C", dir, "config", kv[0], kv[1]).CombinedOutput(); err != nil {
			t.Fatalf("git config: %v %s", err, out)
		}
	}
	wikiRoot = filepath.Join(dir, "wiki")
	if err := os.MkdirAll(wikiRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wikiRoot, "index.md"), []byte("# wiki\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, wikiRoot
}

func lastSubject(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%s").Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestCommitWikiDefaultMessage(t *testing.T) {
	dir, wikiRoot := wikiRepo(t)
	commitWiki(config{}, wikiRoot, "seed wiki", "1 page(s) (index.md)", 1)
	if got, want := lastSubject(t, dir), "docs(wiki): seed wiki — 1 page(s) (index.md)"; got != want {
		t.Fatalf("subject = %q, want %q", got, want)
	}
}

func TestCommitWikiCustomMessage(t *testing.T) {
	dir, wikiRoot := wikiRepo(t)
	cfg := config{WikiCommitMessage: "wiki: {action} [{count}] {project}"}
	commitWiki(cfg, wikiRoot, "lint fix", "ignored", 3)
	if got, want := lastSubject(t, dir), "wiki: lint fix [3] "+filepath.Base(dir); got != want {
		t.Fatalf("subject = %q, want %q", got, want)
	}
}

// The pathspec commit must not steal the user's staged non-wiki files.
func TestCommitWikiLeavesUserStagingAlone(t *testing.T) {
	dir, wikiRoot := wikiRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main // edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "add", "main.go").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	commitWiki(config{}, wikiRoot, "apply proposal", "1 page(s) (index.md)", 1)
	if got := lastSubject(t, dir); !strings.HasPrefix(got, "docs(wiki):") {
		t.Fatalf("wiki commit missing, HEAD subject = %q", got)
	}
	out, err := exec.Command("git", "-C", dir, "diff", "--cached", "--name-only").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "main.go") {
		t.Fatalf("user's staged main.go was consumed, staged now: %q", out)
	}
}

func TestCommitWikiNonGitNoop(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	wikiRoot := filepath.Join(t.TempDir(), "wiki")
	if err := os.MkdirAll(wikiRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	commitWiki(config{}, wikiRoot, "seed wiki", "x", 1) // must not panic or create a repo
	if _, err := os.Stat(filepath.Join(wikiRoot, ".git")); err == nil {
		t.Fatal("commitWiki must not create a git repo")
	}
}
