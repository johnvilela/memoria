package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tracked project with a pending digest for session s1
func digestFixture(t *testing.T) (proj, cfgPath string) {
	t.Helper()
	proj = t.TempDir()
	cfgPath = testConfig(t, proj)
	d := digestFile(proj, "s1")
	if err := os.MkdirAll(filepath.Dir(d), 0o755); err != nil {
		t.Fatal(err)
	}
	obs := "---\nkind: session-digest\nsession_id: s1\n---\n\n@user-prompt 'add a queue'\n"
	if err := os.WriteFile(d, []byte(obs), 0o644); err != nil {
		t.Fatal(err)
	}
	return proj, cfgPath
}

func TestDigestForeground(t *testing.T) {
	proj, cfgPath := digestFixture(t)
	prompt := stubProcessor(t, `{"title":"Queue work","body_markdown":"did queue work\n","tags":["queue"]}`, nil)
	var buf bytes.Buffer
	if code := runDigest(proj, cfgPath, []string{"s1", "--foreground"}, &buf); code != 0 {
		t.Fatalf("digest = %d: %s", code, buf.String())
	}
	b, err := os.ReadFile(filepath.Join(proj, "wiki", "sessions", "s1.md"))
	if err != nil {
		t.Fatalf("session page not written: %v", err)
	}
	want := "---\ntags: [queue]\n---\n\n# Queue work\n\ndid queue work\n"
	if string(b) != want {
		t.Fatalf("page = %q, want %q", b, want)
	}
	for _, w := range []string{"FAITHFULNESS", "add a queue", "(none)"} {
		if !strings.Contains(*prompt, w) {
			t.Fatalf("prompt missing %q", w)
		}
	}
	st, _ := loadStatus(statusPath(cfgPath))
	if st[filepath.Base(proj)].State != "done" {
		t.Fatalf("status = %+v, want done", st[filepath.Base(proj)])
	}
}

func TestDigestForegroundIncludesCurrentPage(t *testing.T) {
	proj, cfgPath := digestFixture(t)
	page := filepath.Join(proj, "wiki", "sessions", "s1.md")
	if err := os.MkdirAll(filepath.Dir(page), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(page, []byte("old heuristic page\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt := stubProcessor(t, `{"title":"T","body_markdown":"# T\n\nnew\n","tags":[]}`, nil)
	var buf bytes.Buffer
	if code := runDigest(proj, cfgPath, []string{"s1", "--foreground"}, &buf); code != 0 {
		t.Fatalf("digest = %d: %s", code, buf.String())
	}
	if !strings.Contains(*prompt, "old heuristic page") {
		t.Fatal("current page missing from prompt")
	}
	// body already starts with a heading — no second title prepended
	b, _ := os.ReadFile(page)
	if string(b) != "# T\n\nnew\n" {
		t.Fatalf("page = %q", b)
	}
}

func TestDigestForegroundBadResponse(t *testing.T) {
	for _, bad := range []string{"not json", `{"title":"","body_markdown":"","tags":[]}`} {
		proj, cfgPath := digestFixture(t)
		stubProcessor(t, bad, nil)
		var buf bytes.Buffer
		if code := runDigest(proj, cfgPath, []string{"s1", "--foreground"}, &buf); code != 1 {
			t.Fatalf("bad response %q accepted: %d %s", bad, code, buf.String())
		}
		if _, err := os.Stat(filepath.Join(proj, "wiki", "sessions", "s1.md")); !os.IsNotExist(err) {
			t.Fatal("page written despite bad response")
		}
		st, _ := loadStatus(statusPath(cfgPath))
		if st[filepath.Base(proj)].State != "error" {
			t.Fatalf("status = %+v, want error", st[filepath.Base(proj)])
		}
	}
}

func TestDigestDetaches(t *testing.T) {
	proj, cfgPath := digestFixture(t)
	spawned := stubSpawn(t, 4242)
	var buf bytes.Buffer
	if code := runDigest(proj, cfgPath, []string{"s1"}, &buf); code != 0 {
		t.Fatalf("digest = %d: %s", code, buf.String())
	}
	want := []string{proj, "digest", "s1", "--foreground"}
	if strings.Join(*spawned, " ") != strings.Join(want, " ") {
		t.Fatalf("spawned %v, want %v", *spawned, want)
	}
	st, _ := loadStatus(statusPath(cfgPath))
	if st[filepath.Base(proj)].State != "running" || st[filepath.Base(proj)].PID != 4242 {
		t.Fatalf("status = %+v, want running 4242", st[filepath.Base(proj)])
	}
}

func TestDigestUnknownSession(t *testing.T) {
	proj, cfgPath := digestFixture(t)
	stubSpawn(t, 4242)
	var buf bytes.Buffer
	if code := runDigest(proj, cfgPath, []string{"nope"}, &buf); code != 1 {
		t.Fatalf("unknown session = %d, want 1", code)
	}
	if !strings.Contains(buf.String(), "no digest") {
		t.Fatalf("missing error: %s", buf.String())
	}
}

func TestDigestRejectsBadSid(t *testing.T) {
	proj, cfgPath := digestFixture(t)
	var buf bytes.Buffer
	if code := runDigest(proj, cfgPath, []string{"../evil", "--foreground"}, &buf); code != 1 {
		t.Fatalf("bad sid accepted: %d", code)
	}
}

func TestDigestUnregisteredCwdUsesGlobal(t *testing.T) {
	root, cfgPath, _ := globalProcessFixture(t, "")
	stubProcessor(t, `{"title":"Fix the thing","body_markdown":"fixed it\n","tags":[]}`, nil)
	commits := stubCommitWiki(t)
	var buf bytes.Buffer
	if code := runDigest(t.TempDir(), cfgPath, []string{"g1", "--foreground"}, &buf); code != 0 {
		t.Fatalf("digest = %d: %s", code, buf.String())
	}
	if _, err := os.Stat(filepath.Join(root, "wiki", "sessions", "g1.md")); err != nil {
		t.Fatalf("session page not under the global wiki: %v", err)
	}
	st, _ := loadStatus(statusPath(cfgPath))
	if st[globalName].State != "done" {
		t.Fatalf("status = %+v, want done under %s", st, globalName)
	}
	// default global root: the wiki is its own repo and always commits
	if len(*commits) != 1 || !(*commits)[0] {
		t.Fatalf("commitWiki calls = %v, want one with WikiAutoCommit=true", *commits)
	}
}

func TestDigestGlobalPathNeverCommits(t *testing.T) {
	root, cfgPath, _ := globalProcessFixture(t, t.TempDir())
	stubProcessor(t, `{"title":"Fix the thing","body_markdown":"fixed it\n","tags":[]}`, nil)
	commits := stubCommitWiki(t)
	var buf bytes.Buffer
	if code := runDigest(t.TempDir(), cfgPath, []string{"g1", "--foreground"}, &buf); code != 0 {
		t.Fatalf("digest = %d: %s", code, buf.String())
	}
	if _, err := os.Stat(filepath.Join(root, "wiki", "sessions", "g1.md")); err != nil {
		t.Fatalf("session page not under the custom global root: %v", err)
	}
	// custom global_path: the user's folder, git never touched
	if len(*commits) != 1 || (*commits)[0] {
		t.Fatalf("commitWiki calls = %v, want one with WikiAutoCommit=false", *commits)
	}
}

func TestDigestDetachGlobal(t *testing.T) {
	_, cfgPath, _ := globalProcessFixture(t, "")
	spawned := stubSpawn(t, 4242)
	unreg := t.TempDir()
	var buf bytes.Buffer
	if code := runDigest(unreg, cfgPath, []string{"g1"}, &buf); code != 0 {
		t.Fatalf("digest = %d: %s", code, buf.String())
	}
	want := []string{unreg, "digest", "g1", "--foreground"}
	if strings.Join(*spawned, " ") != strings.Join(want, " ") {
		t.Fatalf("spawned %v, want %v", *spawned, want)
	}
	st, _ := loadStatus(statusPath(cfgPath))
	if st[globalName].State != "running" || st[globalName].PID != 4242 {
		t.Fatalf("status = %+v, want running 4242 under %s", st, globalName)
	}
}
