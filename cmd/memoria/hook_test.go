package main

import (
	"encoding/json"
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

var lineRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[^ ]* - user-prompt - \{"prompt":"hello"\}$`)

func TestCaptureHookWritesLine(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	in := strings.NewReader(fmt.Sprintf(`{"session_id":"abc123","cwd":%q,"prompt":"hello","permission_mode":"auto","transcript_path":"/x.jsonl"}`, proj))
	if err := captureHook("user-prompt", in, cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(sessionFile(proj, "abc123"))
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSuffix(string(b), "\n")
	if !lineRe.MatchString(line) {
		t.Fatalf("line %q does not match filtered DATETIME - HOOK - DATA pattern", line)
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
	if !strings.Contains(lines[0], " - session-start") || !strings.Contains(lines[1], " - stop") {
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
	in := strings.NewReader(fmt.Sprintf(`{"session_id":"s4","cwd":%q,"custom_field":"kept"}`, proj))
	if err := captureHook("weird-future-event", in, cfg); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(sessionFile(proj, "s4"))
	if !strings.Contains(string(b), ` - other - {"custom_field":"kept"}`) {
		t.Fatalf("unknown hook not logged as other with noise stripped: %s", b)
	}
}

func TestCaptureHookFiltersPerHook(t *testing.T) {
	cases := []struct {
		hook    string
		extra   string // extra JSON fields injected into the payload
		want    string // expected line suffix after "HOOK"
		notWant []string
	}{
		{"pre-tool-use", `"tool_name":"Bash","tool_input":{"command":"ls"}`,
			` - pre-tool-use - {"tool_name":"Bash"}`, []string{"tool_input"}},
		{"post-tool-use", `"tool_name":"Bash","tool_input":{"command":"ls"},"tool_response":{"stdout":"ok","stderr":"","interrupted":false,"isImage":false},"duration_ms":30`,
			`"stdout":"ok"`, []string{"stderr", "interrupted", "isImage", "duration_ms"}},
		{"stop", `"last_assistant_message":"done","background_tasks":[]`,
			` - stop - {"last_assistant_message":"done"}`, []string{"background_tasks"}},
		{"subagent-stop", `"last_assistant_message":"sub done","agent_type":"","agent_id":"x1"`,
			` - subagent-stop - {"last_assistant_message":"sub done"}`, []string{"agent_id", "agent_type"}},
		{"session-start", `"source":"startup"`,
			` - session-start - {"source":"startup"}`, nil},
		{"session-end", `"reason":"exit"`,
			` - session-end - {"reason":"exit"}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.hook, func(t *testing.T) {
			proj := t.TempDir()
			cfg := testConfig(t, proj)
			in := strings.NewReader(fmt.Sprintf(`{"session_id":"sf","cwd":%q,%s}`, proj, tc.extra))
			if err := captureHook(tc.hook, in, cfg); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(sessionFile(proj, "sf"))
			if !strings.Contains(string(b), tc.want) {
				t.Fatalf("line %q missing %q", b, tc.want)
			}
			for _, nw := range tc.notWant {
				if strings.Contains(string(b), nw) {
					t.Fatalf("line %q should not contain %q", b, nw)
				}
			}
		})
	}
}

func TestCaptureHookSkipsNoiseHooks(t *testing.T) {
	for _, hook := range []string{"notification", "subagent-start"} {
		proj := t.TempDir()
		cfg := testConfig(t, proj)
		if err := captureHook(hook, payload("s6", proj), cfg); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(proj, ".memoria")); !os.IsNotExist(err) {
			t.Fatalf("%s should not write anything", hook)
		}
	}
}

func TestCaptureHookCompactMarker(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	if err := captureHook("pre-compact", payload("s7", proj), cfg); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(sessionFile(proj, "s7"))
	line := strings.TrimSuffix(string(b), "\n")
	if !strings.HasSuffix(line, " - pre-compact") {
		t.Fatalf("compact marker line %q should end with hook name, no data", line)
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

func promptPayload(sid, cwd, prompt string) *strings.Reader {
	b, _ := json.Marshal(map[string]any{"session_id": sid, "cwd": cwd, "prompt": prompt})
	return strings.NewReader(string(b))
}

func indexFile(proj string) string {
	return filepath.Join(proj, ".memoria", "sessions.md")
}

func TestIndexSessionFirstPrompt(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	if err := captureHook("user-prompt", promptPayload("s1", proj, "hello world"), cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(indexFile(proj))
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSuffix(string(b), "\n")
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T[^ ]+ - s1 - hello world$`).MatchString(line) {
		t.Fatalf("index line %q wrong", line)
	}
}

func TestIndexSessionOncePerID(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	if err := captureHook("user-prompt", promptPayload("s1", proj, "first"), cfg); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(indexFile(proj))
	if err := captureHook("user-prompt", promptPayload("s1", proj, "second"), cfg); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(indexFile(proj))
	if string(before) != string(after) {
		t.Fatalf("index changed on repeat prompt: %q -> %q", before, after)
	}
	if err := captureHook("user-prompt", promptPayload("s2", proj, "other session"), cfg); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(indexFile(proj))
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], " - s2 - other session") {
		t.Fatalf("index lines wrong: %v", lines)
	}
}

func TestIndexSessionSanitizesName(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	long := "line one\nline two\t" + strings.Repeat("x", 100)
	if err := captureHook("user-prompt", promptPayload("s1", proj, long), cfg); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(indexFile(proj))
	line := strings.TrimSuffix(string(b), "\n")
	if strings.Count(string(b), "\n") != 1 {
		t.Fatalf("multi-line prompt leaked newlines: %q", b)
	}
	if !strings.Contains(line, "line one line two ") {
		t.Fatalf("newlines/tabs not collapsed: %q", line)
	}
	name := line[strings.LastIndex(line, " - ")+3:]
	if got := len([]rune(name)); got > 83 { // 80 + "..."
		t.Fatalf("name not truncated: %d runes", got)
	}
	if !strings.HasSuffix(name, "...") {
		t.Fatalf("truncated name missing ellipsis: %q", name)
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
