package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return string(out)
}

// commitFixture: a repo whose wiki/index.md is committed, then edited, plus a
// brand-new page in a brand-new subfolder. Returns the project dir and config.
func commitFixture(t *testing.T) (dir, cfgPath string) {
	t.Helper()
	dir, wikiRoot := wikiRepo(t)
	gitIn(t, dir, "add", "wiki")
	gitIn(t, dir, "commit", "-q", "-m", "docs(wiki): initial")
	if err := os.WriteFile(filepath.Join(wikiRoot, "index.md"), []byte("# wiki\nedited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wikiRoot, "gotchas"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wikiRoot, "gotchas", "new.md"), []byte("# new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, testConfig(t, dir)
}

func TestCommitNewAndModifiedPages(t *testing.T) {
	dir, cfgPath := commitFixture(t)
	var buf bytes.Buffer
	if code := runCommit(dir, cfgPath, nil, &buf); code != 0 {
		t.Fatalf("commit = %d: %s", code, buf.String())
	}
	subject := lastSubject(t, dir)
	for _, w := range []string{"docs(wiki): update — 2 page(s)", "index.md", "gotchas/new.md"} {
		if !strings.Contains(subject, w) {
			t.Fatalf("subject %q missing %q", subject, w)
		}
	}
	// -uall: the new page inside a new folder must be committed individually
	files := gitIn(t, dir, "show", "--name-only", "--format=", "HEAD")
	for _, w := range []string{"wiki/index.md", "wiki/gotchas/new.md"} {
		if !strings.Contains(files, w) {
			t.Fatalf("commit missing %q, has:\n%s", w, files)
		}
	}
}

func TestCommitMessageOverride(t *testing.T) {
	dir, cfgPath := commitFixture(t)
	var buf bytes.Buffer
	if code := runCommit(dir, cfgPath, []string{"-m", "docs(wiki): rewrite onboarding"}, &buf); code != 0 {
		t.Fatalf("commit = %d: %s", code, buf.String())
	}
	if got, want := lastSubject(t, dir), "docs(wiki): rewrite onboarding"; got != want {
		t.Fatalf("subject = %q, want %q", got, want)
	}
}

// The pathspec commit must not steal the user's staged non-wiki files.
func TestCommitLeavesUserStagingAlone(t *testing.T) {
	dir, cfgPath := commitFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main // edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "main.go")
	var buf bytes.Buffer
	if code := runCommit(dir, cfgPath, nil, &buf); code != 0 {
		t.Fatalf("commit = %d: %s", code, buf.String())
	}
	if staged := gitIn(t, dir, "diff", "--cached", "--name-only"); !strings.Contains(staged, "main.go") {
		t.Fatalf("user's staged main.go was consumed, staged now: %q", staged)
	}
}

func TestCommitCleanWikiIsNoop(t *testing.T) {
	dir, cfgPath := commitFixture(t)
	gitIn(t, dir, "add", "wiki")
	gitIn(t, dir, "commit", "-q", "-m", "docs(wiki): mine")
	var buf bytes.Buffer
	if code := runCommit(dir, cfgPath, nil, &buf); code != 0 {
		t.Fatalf("commit = %d: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "No wiki changes") {
		t.Fatalf("missing no-changes message: %q", buf.String())
	}
	if got := lastSubject(t, dir); got != "docs(wiki): mine" {
		t.Fatalf("committed anyway: %q", got)
	}
}

func TestCommitOutsideGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if code := runCommit(dir, testConfig(t, dir), nil, &buf); code != 1 {
		t.Fatalf("commit = %d, want 1: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "git repo") {
		t.Fatalf("unhelpful error: %q", buf.String())
	}
}

func TestCommitUntrackedProject(t *testing.T) {
	var buf bytes.Buffer
	if code := runCommit(t.TempDir(), testConfig(t), nil, &buf); code != 1 {
		t.Fatalf("commit = %d, want 1: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "not inside a tracked project") {
		t.Fatalf("unhelpful error: %q", buf.String())
	}
}
