package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// writes a config.yaml tracking the given projects, returns its path
func testConfig(t *testing.T, projects ...string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("projects:\n")
	for _, p := range projects {
		fmt.Fprintf(&b, "  - name: %s\n    path: %s\n", filepath.Base(p), p)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func payload(sid, cwd string) *strings.Reader {
	return strings.NewReader(fmt.Sprintf(`{"session_id":%q,"cwd":%q,"hook_event_name":"X"}`, sid, cwd))
}

func sessionFile(proj, sid string) string {
	return filepath.Join(proj, ".memoria", "sessions", sid+".md")
}

var lineRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[^ ]* - pre-tool-use - \{.*"session_id":"abc123".*\}$`)

func TestCaptureHookWritesLine(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	if err := captureHook("pre-tool-use", payload("abc123", proj), cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(sessionFile(proj, "abc123"))
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSuffix(string(b), "\n")
	if !lineRe.MatchString(line) {
		t.Fatalf("line %q does not match DATETIME - HOOK - DATA pattern", line)
	}
}

func TestCaptureHookAppendsChronologically(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	for _, name := range []string{"session-start", "stop"} {
		if err := captureHook(name, payload("s1", proj), cfg); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(sessionFile(proj, "s1"))
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if !strings.Contains(lines[0], " - session-start - ") || !strings.Contains(lines[1], " - stop - ") {
		t.Fatalf("wrong order: %v", lines)
	}
}

func TestCaptureHookSubdirCwd(t *testing.T) {
	proj := t.TempDir()
	sub := filepath.Join(proj, "src", "deep")
	cfg := testConfig(t, proj)
	if err := captureHook("stop", payload("s2", sub), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sessionFile(proj, "s2")); err != nil {
		t.Fatal("session file not written to project root")
	}
}

func TestCaptureHookUntrackedProject(t *testing.T) {
	proj := t.TempDir()
	other := t.TempDir()
	cfg := testConfig(t, proj)
	if err := captureHook("stop", payload("s3", other), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(other, ".memoria")); !os.IsNotExist(err) {
		t.Fatal(".memoria created in untracked project")
	}
}

func TestCaptureHookUnknownNameLogsOther(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	if err := captureHook("weird-future-event", payload("s4", proj), cfg); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(sessionFile(proj, "s4"))
	if !strings.Contains(string(b), " - other - ") {
		t.Fatalf("unknown hook not logged as other: %s", b)
	}
}

func TestCaptureHookMissingConfig(t *testing.T) {
	proj := t.TempDir()
	if err := captureHook("stop", payload("s5", proj), filepath.Join(t.TempDir(), "nope.yaml")); err != nil {
		t.Fatalf("missing config must be silent, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".memoria")); !os.IsNotExist(err) {
		t.Fatal(".memoria created without config")
	}
}

func TestCaptureHookRejectsBadSessionID(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	for _, sid := range []string{"../evil", "a/b", ".", ".."} {
		if err := captureHook("stop", payload(sid, proj), cfg); err != nil {
			t.Fatalf("sid %q: want silent skip, got %v", sid, err)
		}
	}
	if _, err := os.Stat(filepath.Join(proj, ".memoria")); !os.IsNotExist(err) {
		t.Fatal("file written for bad session id")
	}
}

func TestMatchProject(t *testing.T) {
	cases := []struct {
		cwd   string
		paths []string
		want  string
	}{
		{"/a/proj", []string{"/a/proj"}, "/a/proj"},
		{"/a/proj/sub", []string{"/a/proj"}, "/a/proj"},
		{"/a/project2", []string{"/a/proj"}, ""},
		{"/a/proj/sub", []string{"/a", "/a/proj"}, "/a/proj"},
		{"/a/proj", []string{"/a/proj/"}, "/a/proj"},
		{"/elsewhere", []string{"/a/proj"}, ""},
	}
	for _, tc := range cases {
		var projects []project
		for _, p := range tc.paths {
			projects = append(projects, project{Name: filepath.Base(p), Path: p})
		}
		if got := matchProject(tc.cwd, projects); got != tc.want {
			t.Errorf("matchProject(%q, %v) = %q, want %q", tc.cwd, tc.paths, got, tc.want)
		}
	}
}
