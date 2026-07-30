package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
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

func digestFile(proj, sid string) string {
	return filepath.Join(proj, ".memoria", "sessions", "pending", sid+".md")
}

func readDigest(t *testing.T, proj, sid string) string {
	t.Helper()
	b, err := os.ReadFile(digestFile(proj, sid))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCaptureHookWritesDigest(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	in := strings.NewReader(fmt.Sprintf(`{"session_id":"abc123","cwd":%q,"prompt":"hello\nworld","permission_mode":"auto"}`, proj))
	if err := captureHook("user-prompt", nil, in, cfg); err != nil {
		t.Fatal(err)
	}
	got := readDigest(t, proj, "abc123")
	for _, w := range []string{
		"schema_version: 2", "kind: session-digest", "session_id: abc123",
		"project: " + filepath.Base(proj), "project_root: " + proj,
		"@user-prompt 'hello world'",
	} {
		if !strings.Contains(got, w) {
			t.Fatalf("digest missing %q:\n%s", w, got)
		}
	}
	m := regexp.MustCompile(`started_at: (\S+)`).FindStringSubmatch(got)
	if m == nil {
		t.Fatalf("no started_at:\n%s", got)
	}
	if _, err := time.Parse(time.RFC3339, m[1]); err != nil {
		t.Fatalf("started_at %q not RFC3339: %v", m[1], err)
	}
}

func TestCaptureHookAppendsChronologically(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	if err := captureHook("session-start", nil, payload("s1", proj), cfg); err != nil {
		t.Fatal(err)
	}
	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, "hi"), cfg); err != nil {
		t.Fatal(err)
	}
	got := readDigest(t, proj, "s1")
	if strings.Count(got, "---") != 2 {
		t.Fatalf("frontmatter written more than once:\n%s", got)
	}
	if strings.Index(got, "@session-start") > strings.Index(got, "@user-prompt") {
		t.Fatalf("events out of order:\n%s", got)
	}
}

func TestRenderEvent(t *testing.T) {
	cases := []struct {
		name    string
		payload string // JSON
		want    string
	}{
		{"session-start", `{"source":"startup"}`, "@session-start source: startup"},
		{"session-start", `{}`, "@session-start"},
		{"user-prompt", `{"prompt":"fix   the\nbug"}`, "@user-prompt 'fix the bug'"},
		{"post-tool-use", `{"tool_name":"Write","tool_input":{"file_path":"/a/b.go"}}`, "@post-tool-use Write /a/b.go"},
		{"post-tool-use", `{"tool_name":"Edit","tool_input":{"file_path":"/a/b.go"}}`, "@post-tool-use Edit /a/b.go"},
		{"post-tool-use", `{"tool_name":"NotebookEdit","tool_input":{"file_path":"/n.ipynb"}}`, "@post-tool-use Edit /n.ipynb"},
		{"post-tool-use", `{"tool_name":"Bash","tool_input":{"command":"go test  ./..."}}`, "@post-tool-use Bash 'go test ./...'"},
		{"post-tool-use", `{"tool_name":"Bash","tool_input":{"command":"go build"},"tool_response":{"error":"exit 1:\nundefined: foo"}}`,
			"@post-tool-use Bash 'go build' error: 'exit 1: undefined: foo'"},
		{"post-tool-use", `{"tool_name":"Bash","tool_input":{"command":"x"},"tool_response":{"is_error":true}}`,
			"@post-tool-use Bash 'x' error: 'unspecified'"},
		{"post-tool-use", `{"tool_name":"Read","tool_input":{"file_path":"/a/b.go"}}`, ""},
		{"post-tool-use", `{"tool_name":"Grep","tool_input":{"pattern":"x"}}`, ""},
		{"pre-tool-use", `{"tool_name":"Bash"}`, ""},
		{"stop", `{"last_assistant_message":"done,\ntests pass"}`, "@stop 'done, tests pass'"},
		{"stop", `{}`, ""},
		{"subagent-stop", `{"agent_type":"Explore","last_assistant_message":"found it"}`, "@subagent-stop Explore 'found it'"},
		{"subagent-stop", `{}`, ""},
		{"pre-compact", `{}`, "@pre-compact"},
		{"post-compact", `{}`, "@post-compact"},
		{"session-end", `{"reason":"exit"}`, "@session-end reason: exit"},
		{"session-end", `{}`, "@session-end"},
		{"notification", `{"message":"x"}`, ""},
		{"subagent-start", `{}`, ""},
	}
	for _, tc := range cases {
		var p map[string]any
		if err := json.Unmarshal([]byte(tc.payload), &p); err != nil {
			t.Fatal(err)
		}
		if got := renderEvent(tc.name, p); got != tc.want {
			t.Errorf("renderEvent(%s, %s) = %q, want %q", tc.name, tc.payload, got, tc.want)
		}
	}
}

func TestCaptureHookSkipsNoiseHooks(t *testing.T) {
	for _, hook := range []string{"notification", "subagent-start", "pre-tool-use", "weird-future-event"} {
		proj := t.TempDir()
		cfg := testConfig(t, proj)
		if err := captureHook(hook, nil, payload("s6", proj), cfg); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(proj, ".memoria")); !os.IsNotExist(err) {
			t.Fatalf("%s should not write anything", hook)
		}
	}
}

func TestCaptureHookSessionEnd(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, "hi"), cfg); err != nil {
		t.Fatal(err)
	}
	in := strings.NewReader(fmt.Sprintf(`{"session_id":"s1","cwd":%q,"reason":"exit"}`, proj))
	if err := captureHook("session-end", nil, in, cfg); err != nil {
		t.Fatal(err)
	}
	got := readDigest(t, proj, "s1")
	head := got[:strings.Index(got, "\n---\n")] // frontmatter block
	if !strings.Contains(head, "ended_at: ") {
		t.Fatalf("ended_at not in frontmatter:\n%s", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "@session-end reason: exit") {
		t.Fatalf("@session-end not last line:\n%s", got)
	}
}

func TestCaptureHookSessionEndFirstEvent(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	if err := captureHook("session-end", nil, payload("s1", proj), cfg); err != nil {
		t.Fatal(err)
	}
	got := readDigest(t, proj, "s1")
	if !strings.Contains(got, "kind: session-digest") || !strings.Contains(got, "ended_at: ") ||
		!strings.Contains(got, "@session-end") {
		t.Fatalf("lone session-end digest wrong:\n%s", got)
	}
}

func TestReopenedSessionGetsNewIncarnation(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	processed := filepath.Join(proj, ".memoria", "sessions", "processed")
	if err := os.MkdirAll(processed, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, "round one"), cfg); err != nil {
		t.Fatal(err)
	}
	// simulate process --apply: digest moves to processed/
	if err := os.Rename(digestFile(proj, "s1"), filepath.Join(processed, "s1.md")); err != nil {
		t.Fatal(err)
	}

	// user reopens the session
	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, "round two"), cfg); err != nil {
		t.Fatal(err)
	}
	inc2 := filepath.Join(proj, ".memoria", "sessions", "pending", "s1-2.md")
	b, err := os.ReadFile(inc2)
	if err != nil {
		t.Fatalf("s1-2.md not created: %v", err)
	}
	if !strings.Contains(string(b), "continues_from: ../processed/s1.md") {
		t.Fatalf("missing continues_from:\n%s", b)
	}
	if !strings.Contains(string(b), "round two") {
		t.Fatalf("event line missing:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(processed, "s1.md")); err != nil {
		t.Fatal("original left processed/")
	}

	// further events keep appending to the same incarnation
	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, "still round two"), cfg); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(inc2)
	if strings.Count(string(b), "---") != 2 || !strings.Contains(string(b), "still round two") {
		t.Fatalf("did not append to s1-2.md:\n%s", b)
	}

	// queue points at the new incarnation
	qb, _ := os.ReadFile(queuePath(cfg))
	if !strings.Contains(string(qb), "s1-2.md") {
		t.Fatalf("queue missing s1-2.md:\n%s", qb)
	}

	// second reopen: s1-2 processed too -> s1-3, linked to s1-2
	if err := os.Rename(inc2, filepath.Join(processed, "s1-2.md")); err != nil {
		t.Fatal(err)
	}
	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, "round three"), cfg); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(filepath.Join(proj, ".memoria", "sessions", "pending", "s1-3.md"))
	if err != nil {
		t.Fatalf("s1-3.md not created: %v", err)
	}
	if !strings.Contains(string(b), "continues_from: ../processed/s1-2.md") {
		t.Fatalf("s1-3 not linked to s1-2:\n%s", b)
	}
}

func TestCaptureHookSubdirCwd(t *testing.T) {
	proj := t.TempDir()
	sub := filepath.Join(proj, "src", "deep")
	cfg := testConfig(t, proj)
	if err := captureHook("user-prompt", nil, promptPayload("s2", sub, "hi"), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(digestFile(proj, "s2")); err != nil {
		t.Fatal("digest not written to project root")
	}
}

func TestCaptureHookUntrackedProject(t *testing.T) {
	proj := t.TempDir()
	other := t.TempDir()
	cfg := testConfig(t, proj)
	if err := captureHook("user-prompt", nil, promptPayload("s3", other, "hi"), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(other, ".memoria")); !os.IsNotExist(err) {
		t.Fatal(".memoria created in untracked project")
	}
}

func TestCaptureHookMissingConfig(t *testing.T) {
	proj := t.TempDir()
	if err := captureHook("stop", nil, payload("s5", proj), filepath.Join(t.TempDir(), "nope.yaml")); err != nil {
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
		if err := captureHook("user-prompt", nil, promptPayload(sid, proj, "hi"), cfg); err != nil {
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
	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, "hello world"), cfg); err != nil {
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
	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, "first"), cfg); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(indexFile(proj))
	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, "second"), cfg); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(indexFile(proj))
	if string(before) != string(after) {
		t.Fatalf("index changed on repeat prompt: %q -> %q", before, after)
	}
	if err := captureHook("user-prompt", nil, promptPayload("s2", proj, "other session"), cfg); err != nil {
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
	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, long), cfg); err != nil {
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

func TestCaptureHookNoCaptureEnv(t *testing.T) {
	t.Setenv("MEMORIA_NO_CAPTURE", "1")
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, "hi"), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".memoria")); !os.IsNotExist(err) {
		t.Fatal("captured despite MEMORIA_NO_CAPTURE")
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

func TestCaptureHookClientFrontmatter(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	if err := captureHook("user-prompt", []string{"--client", "claude-code"}, promptPayload("s1", proj, "hi"), cfg); err != nil {
		t.Fatal(err)
	}
	if got := readDigest(t, proj, "s1"); !strings.Contains(got, "\nclient: claude-code\n") {
		t.Fatalf("frontmatter missing client: %q", got)
	}
}

func TestCaptureHookNoClientNoLine(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, "hi"), cfg); err != nil {
		t.Fatal(err)
	}
	if got := readDigest(t, proj, "s1"); strings.Contains(got, "client:") {
		t.Fatalf("client line written without --client: %q", got)
	}
}

func TestHookSessionEndSpawnsConsolidation(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	setAutoApply(t, cfg)
	spawned := stubSpawn(t, 4242)
	if err := captureHook("session-end", nil, payload("s1", proj), cfg); err != nil {
		t.Fatal(err)
	}
	want := []string{proj, "process", "--foreground"}
	if strings.Join(*spawned, " ") != strings.Join(want, " ") {
		t.Fatalf("spawned %v, want %v", *spawned, want)
	}
	st, _ := loadStatus(statusPath(cfg))
	if st[filepath.Base(proj)].State != "running" || st[filepath.Base(proj)].PID != 4242 {
		t.Fatalf("status = %+v", st[filepath.Base(proj)])
	}
}

func TestHookSessionEndNoAutoApplyNoSpawn(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	spawned := stubSpawn(t, 4242)
	if err := captureHook("session-end", nil, payload("s1", proj), cfg); err != nil {
		t.Fatal(err)
	}
	if len(*spawned) != 0 {
		t.Fatalf("spawned %v without auto_apply", *spawned)
	}
}

func TestHookSessionEndBusyNoSpawn(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	setAutoApply(t, cfg)
	if err := statusSet(statusPath(cfg), filepath.Base(proj), "running", os.Getpid(), ""); err != nil {
		t.Fatal(err)
	}
	spawned := stubSpawn(t, 4242)
	if err := captureHook("session-end", nil, payload("s1", proj), cfg); err != nil {
		t.Fatal(err)
	}
	if len(*spawned) != 0 {
		t.Fatalf("spawned %v while busy", *spawned)
	}
}

func stopPayload(sid, cwd, msg string) *strings.Reader {
	b, _ := json.Marshal(map[string]any{"session_id": sid, "cwd": cwd, "last_assistant_message": msg})
	return strings.NewReader(string(b))
}

// fakeClaudeHome points HOME at a temp dir holding a ~/.claude/sessions live
// file titled title for sid, plus a non-matching and a garbage neighbor.
func fakeClaudeHome(t *testing.T, sid, title string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"1.json": `{"sessionId":"someone-else","name":"wrong"}`,
		"2.json": fmt.Sprintf(`{"pid":1,"sessionId":%q,"name":%q,"status":"busy"}`, sid, title),
		"3.json": `not json at all`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestClaudeTitle(t *testing.T) {
	fakeClaudeHome(t, "s1", "Fix the picker")
	if got := claudeTitle("s1"); got != "Fix the picker" {
		t.Fatalf("claudeTitle = %q", got)
	}
	if got := claudeTitle("unknown-sid"); got != "" {
		t.Fatalf("unknown sid got %q", got)
	}
	t.Setenv("HOME", t.TempDir()) // no .claude/sessions dir
	if got := claudeTitle("s1"); got != "" {
		t.Fatalf("missing dir got %q", got)
	}
}

func TestCaptureHookStopCapturesTitle(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	fakeClaudeHome(t, "s1", "Session titles in run")
	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, "first prompt"), cfg); err != nil {
		t.Fatal(err)
	}
	if err := captureHook("stop", []string{"--client", "claude-code"}, stopPayload("s1", proj, "done"), cfg); err != nil {
		t.Fatal(err)
	}
	if got := readDigest(t, proj, "s1"); !strings.Contains(got, "\ntitle: Session titles in run\n") {
		t.Fatalf("digest missing title:\n%s", got)
	}
	idx, err := os.ReadFile(indexFile(proj))
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(idx))
	parts := strings.SplitN(line, " - ", 3)
	if len(parts) != 3 || parts[1] != "s1" || parts[2] != "Session titles in run" {
		t.Fatalf("index line = %q", line)
	}
	if _, err := time.Parse(time.RFC3339, parts[0]); err != nil {
		t.Fatalf("date slot broken: %q", parts[0])
	}
}

func TestCaptureHookStopTitleIdempotent(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	fakeClaudeHome(t, "s1", "Stable title")
	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, "hi"), cfg); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := captureHook("stop", []string{"--client", "claude-code"}, stopPayload("s1", proj, "done"), cfg); err != nil {
			t.Fatal(err)
		}
	}
	if got := readDigest(t, proj, "s1"); strings.Count(got, "title:") != 1 {
		t.Fatalf("want exactly one title line:\n%s", got)
	}
	idx, _ := os.ReadFile(indexFile(proj))
	if n := strings.Count(string(idx), "Stable title"); n != 1 {
		t.Fatalf("index rewritten badly (%d matches):\n%s", n, idx)
	}
}

func TestCaptureHookCodexNoTitle(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	fakeClaudeHome(t, "s1", "Should not appear")
	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, "first prompt"), cfg); err != nil {
		t.Fatal(err)
	}
	if err := captureHook("stop", []string{"--client", "codex"}, stopPayload("s1", proj, "done"), cfg); err != nil {
		t.Fatal(err)
	}
	if got := readDigest(t, proj, "s1"); strings.Contains(got, "title:") {
		t.Fatalf("codex session got a title:\n%s", got)
	}
	idx, _ := os.ReadFile(indexFile(proj))
	if !strings.Contains(string(idx), "first prompt") {
		t.Fatalf("index lost first-prompt name:\n%s", idx)
	}
}

func TestRenameSessionPreservesOtherLines(t *testing.T) {
	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, ".memoria"), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		"2026-01-01T00:00:00Z - aaa - keep me",
		"2026-01-02T00:00:00Z - bbb - name - with - dashes",
		"2026-01-03T00:00:00Z - ccc - also keep",
		"",
	}
	if err := os.WriteFile(indexFile(proj), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := renameSession(proj, "bbb", "New Title"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(indexFile(proj))
	want := "2026-01-01T00:00:00Z - aaa - keep me\n2026-01-02T00:00:00Z - bbb - New Title\n2026-01-03T00:00:00Z - ccc - also keep\n"
	if string(b) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", b, want)
	}
}

func TestCaptureHookStopNoDigestNoError(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	fakeClaudeHome(t, "s1", "Titled but empty")
	if err := captureHook("stop", []string{"--client", "claude-code"}, stopPayload("s1", proj, ""), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(digestFile(proj, "s1")); !os.IsNotExist(err) {
		t.Fatalf("digest unexpectedly created: %v", err)
	}
}
