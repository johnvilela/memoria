package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type agentCall struct {
	dir, bin string
	args     []string
}

// stubSelect replaces the picker: captures the offered options and returns
// the canned value (or an abort error when value is "ABORT").
func stubSelect(t *testing.T, value string) *[]option {
	t.Helper()
	var seen []option
	orig := selectOption
	selectOption = func(title string, opts []option) (string, error) {
		seen = opts
		if value == "ABORT" {
			return "", os.ErrClosed
		}
		return value, nil
	}
	t.Cleanup(func() { selectOption = orig })
	return &seen
}

func stubAgent(t *testing.T, exit int) *agentCall {
	t.Helper()
	var call agentCall
	orig := runAgent
	runAgent = func(dir, bin string, args ...string) (int, error) {
		call = agentCall{dir: dir, bin: bin, args: args}
		return exit, nil
	}
	t.Cleanup(func() { runAgent = orig })
	return &call
}

func runFixture(t *testing.T) (proj, cfgPath string) {
	t.Helper()
	proj = t.TempDir()
	cfgPath = testConfig(t, proj)
	index := "2026-07-27T10:00:00-03:00 - aaa-111 - fix the parser - part one\n" +
		"2026-07-28T11:00:00-03:00 - bbb-222 - add search command\n"
	if err := os.MkdirAll(filepath.Join(proj, ".memoria", "sessions", "pending"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".memoria", "sessions.md"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRunDigest(t, proj, "pending", "bbb-222.md", "")
	return proj, cfgPath
}

func writeRunDigest(t *testing.T, proj, state, name, client string) {
	t.Helper()
	dir := filepath.Join(proj, ".memoria", "sessions", state)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	clientLine := ""
	if client != "" {
		clientLine = "client: " + client + "\n"
	}
	body := "---\nschema_version: 2\nkind: session-digest\n" + clientLine + "---\n\n@user-prompt 'hi'\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadSessions(t *testing.T) {
	proj, _ := runFixture(t)
	got := readSessions(proj)
	if len(got) != 2 {
		t.Fatalf("entries = %d, want 2", len(got))
	}
	if got[0].sid != "aaa-111" || got[0].name != "fix the parser - part one" {
		t.Fatalf("name with ' - ' mangled: %+v", got[0])
	}
	if got[1].sid != "bbb-222" || got[1].date != "2026-07-28T11:00:00-03:00" {
		t.Fatalf("order or date wrong: %+v", got[1])
	}
	if got := readSessions(t.TempDir()); len(got) != 0 {
		t.Fatalf("missing index = %v, want empty", got)
	}
}

func TestReadSessionsSkipsMalformed(t *testing.T) {
	proj := t.TempDir()
	os.MkdirAll(filepath.Join(proj, ".memoria"), 0o755)
	os.WriteFile(filepath.Join(proj, ".memoria", "sessions.md"),
		[]byte("garbage line\n2026-07-28T11:00:00-03:00 - sid-1 - ok\n\n"), 0o644)
	got := readSessions(proj)
	if len(got) != 1 || got[0].sid != "sid-1" {
		t.Fatalf("got %+v, want only sid-1", got)
	}
}

func TestMatchSessions(t *testing.T) {
	entries := []sessionEntry{
		{sid: "aaa-111", name: "Fix the Parser"},
		{sid: "bbb-222", name: "add search"},
	}
	if got := matchSessions(entries, "AAA"); len(got) != 1 || got[0].sid != "aaa-111" {
		t.Fatalf("sid prefix = %+v", got)
	}
	if got := matchSessions(entries, "parser"); len(got) != 1 || got[0].sid != "aaa-111" {
		t.Fatalf("name substring = %+v", got)
	}
	if got := matchSessions(entries, "zzz"); len(got) != 0 {
		t.Fatalf("no match = %+v", got)
	}
}

func TestFindDigest(t *testing.T) {
	proj := t.TempDir()
	if got := findDigest(proj, "s1"); got != "" {
		t.Fatalf("none = %q", got)
	}
	writeRunDigest(t, proj, "processed", "s1.md", "")
	if got := findDigest(proj, "s1"); !strings.HasSuffix(got, "processed/s1.md") {
		t.Fatalf("processed = %q", got)
	}
	writeRunDigest(t, proj, "pending", "s1-2.md", "")
	if got := findDigest(proj, "s1"); !strings.HasSuffix(got, "pending/s1-2.md") {
		t.Fatalf("pending incarnation must win: %q", got)
	}
}

func TestDigestClient(t *testing.T) {
	proj := t.TempDir()
	writeRunDigest(t, proj, "pending", "s1.md", "claude-code")
	if got := digestClient(findDigest(proj, "s1")); got != "claude-code" {
		t.Fatalf("client = %q", got)
	}
	writeRunDigest(t, proj, "pending", "s2.md", "")
	if got := digestClient(findDigest(proj, "s2")); got != "" {
		t.Fatalf("absent client = %q, want empty", got)
	}
}

func TestNativeResume(t *testing.T) {
	if got := nativeResume("/usr/bin/claude", "claude-code", "s1"); len(got) != 2 || got[0] != "--resume" || got[1] != "s1" {
		t.Fatalf("claude = %v", got)
	}
	if got := nativeResume("codex", "codex", "s1"); len(got) != 2 || got[0] != "resume" || got[1] != "s1" {
		t.Fatalf("codex = %v", got)
	}
	if got := nativeResume("codex", "claude-code", "s1"); got != nil {
		t.Fatalf("harness mismatch = %v, want nil", got)
	}
	if got := nativeResume("aider", "", "s1"); got != nil {
		t.Fatalf("unknown bin = %v, want nil", got)
	}
}

func TestRunUsageNoBinary(t *testing.T) {
	proj, cfgPath := runFixture(t)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, nil, &buf); code != 1 {
		t.Fatalf("no binary = %d, want 1", code)
	}
	if !strings.Contains(buf.String(), "usage:") {
		t.Fatalf("output = %q", buf.String())
	}
	if code := runRun(proj, cfgPath, []string{"--new"}, &buf); code != 1 {
		t.Fatalf("flag as binary = %d, want 1", code)
	}
}

func TestRunMutuallyExclusive(t *testing.T) {
	proj, cfgPath := runFixture(t)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"true", "--new", "--session", "x"}, &buf); code != 1 {
		t.Fatalf("exclusive flags = %d, want 1", code)
	}
	if !strings.Contains(buf.String(), "mutually exclusive") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestRunOutsideProject(t *testing.T) {
	_, cfgPath := runFixture(t)
	var buf bytes.Buffer
	if code := runRun(t.TempDir(), cfgPath, []string{"true", "--new"}, &buf); code != 1 {
		t.Fatalf("untracked = %d, want 1", code)
	}
	if !strings.Contains(buf.String(), "not inside a tracked project") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestRunBinaryNotFound(t *testing.T) {
	proj, cfgPath := runFixture(t)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"definitely-not-a-binary-xyz", "--new"}, &buf); code != 1 {
		t.Fatalf("missing binary = %d, want 1", code)
	}
}

func TestRunNewFresh(t *testing.T) {
	proj, cfgPath := runFixture(t)
	call := stubAgent(t, 7)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"true", "--new"}, &buf); code != 7 {
		t.Fatalf("exit code not propagated: %d, want 7", code)
	}
	if call.dir != proj || call.bin != "true" || len(call.args) != 0 {
		t.Fatalf("call = %+v", call)
	}
}

func TestRunDefaultNonTTYFresh(t *testing.T) {
	proj, cfgPath := runFixture(t)
	stubTTY(t, false)
	call := stubAgent(t, 0)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"true"}, &buf); code != 0 {
		t.Fatalf("run = %d: %s", code, buf.String())
	}
	if len(call.args) != 0 {
		t.Fatalf("non-tty should launch fresh, args = %v", call.args)
	}
}

func TestRunDefaultNoSessionsFresh(t *testing.T) {
	proj := t.TempDir()
	cfgPath := testConfig(t, proj)
	stubTTY(t, true)
	call := stubAgent(t, 0)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"true"}, &buf); code != 0 {
		t.Fatalf("run = %d: %s", code, buf.String())
	}
	if len(call.args) != 0 {
		t.Fatalf("args = %v, want fresh", call.args)
	}
}

func TestRunDefaultPickerContents(t *testing.T) {
	proj, cfgPath := runFixture(t)
	var index strings.Builder
	for _, sid := range []string{"ccc-333", "ddd-444", "eee-555", "fff-666", "ggg-777"} {
		index.WriteString("2026-07-29T10:00:00-03:00 - " + sid + " - work on " + sid + "\n")
	}
	f, err := os.OpenFile(filepath.Join(proj, ".memoria", "sessions.md"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(index.String())
	f.Close()
	writeRunDigest(t, proj, "pending", "ggg-777.md", "")
	stubTTY(t, true)
	seen := stubSelect(t, "")
	stubAgent(t, 0)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"true"}, &buf); code != 0 {
		t.Fatalf("run = %d: %s", code, buf.String())
	}
	opts := *seen
	if len(opts) != 6 {
		t.Fatalf("options = %d, want 6 (new + 5)", len(opts))
	}
	if opts[0].value != "" || opts[0].label != "New session" {
		t.Fatalf("first option = %+v, want New session", opts[0])
	}
	if opts[1].value != "ggg-777" || opts[5].value != "ccc-333" {
		t.Fatalf("order = %q..%q, want newest first ggg-777..ccc-333", opts[1].value, opts[5].value)
	}
	if strings.Contains(opts[1].desc, "no digest") {
		t.Fatalf("ggg-777 has a digest, desc = %q", opts[1].desc)
	}
	if !strings.Contains(opts[2].desc, "no digest") {
		t.Fatalf("fff-666 has no digest, desc = %q", opts[2].desc)
	}
}

func TestRunDefaultPickerNew(t *testing.T) {
	proj, cfgPath := runFixture(t)
	stubTTY(t, true)
	stubSelect(t, "")
	call := stubAgent(t, 0)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"true"}, &buf); code != 0 {
		t.Fatalf("run = %d: %s", code, buf.String())
	}
	if len(call.args) != 0 {
		t.Fatalf("args = %v, want fresh", call.args)
	}
}

func TestRunDefaultPickerHandoff(t *testing.T) {
	proj, cfgPath := runFixture(t)
	stubTTY(t, true)
	stubSelect(t, "bbb-222")
	call := stubAgent(t, 0)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"true"}, &buf); code != 0 {
		t.Fatalf("run = %d: %s", code, buf.String())
	}
	if len(call.args) != 1 || !strings.Contains(call.args[0], filepath.Join("pending", "bbb-222.md")) {
		t.Fatalf("handoff args = %v, want one prompt naming the digest", call.args)
	}
}

func TestRunDefaultPickerNativeResume(t *testing.T) {
	proj, cfgPath := runFixture(t)
	writeRunDigest(t, proj, "pending", "bbb-222.md", "claude-code")
	stubTTY(t, true)
	stubSelect(t, "bbb-222")
	call := stubAgent(t, 0)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"/usr/bin/claude"}, &buf); code != 0 {
		t.Fatalf("run = %d: %s", code, buf.String())
	}
	if len(call.args) != 2 || call.args[0] != "--resume" || call.args[1] != "bbb-222" {
		t.Fatalf("native resume args = %v", call.args)
	}
}

func TestRunDefaultPickerAbort(t *testing.T) {
	proj, cfgPath := runFixture(t)
	stubTTY(t, true)
	stubSelect(t, "ABORT")
	call := stubAgent(t, 0)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"true"}, &buf); code != 0 {
		t.Fatalf("abort = %d, want 0", code)
	}
	if call.bin != "" {
		t.Fatalf("agent launched on abort: %+v", call)
	}
}

func TestRunDigestlessNativeResume(t *testing.T) {
	proj, cfgPath := runFixture(t)
	stubTTY(t, true)
	stubSelect(t, "aaa-111") // no digest in fixture
	call := stubAgent(t, 0)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"/usr/bin/claude"}, &buf); code != 0 {
		t.Fatalf("run = %d: %s", code, buf.String())
	}
	if len(call.args) != 2 || call.args[0] != "--resume" || call.args[1] != "aaa-111" {
		t.Fatalf("digest-less resume args = %v", call.args)
	}
}

func TestRunDigestlessUnknownBin(t *testing.T) {
	proj, cfgPath := runFixture(t)
	stubTTY(t, true)
	stubSelect(t, "aaa-111")
	stubAgent(t, 0)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"true"}, &buf); code != 1 {
		t.Fatalf("run = %d, want 1", code)
	}
	if !strings.Contains(buf.String(), "no digest") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestRunSessionUniqueMatch(t *testing.T) {
	proj, cfgPath := runFixture(t)
	os.Remove(filepath.Join(proj, ".memoria", "sessions", "pending", "bbb-222.md"))
	writeRunDigest(t, proj, "processed", "aaa-111.md", "")
	call := stubAgent(t, 0)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"true", "--session", "parser"}, &buf); code != 0 {
		t.Fatalf("run = %d: %s", code, buf.String())
	}
	if len(call.args) != 1 || !strings.Contains(call.args[0], filepath.Join("processed", "aaa-111.md")) {
		t.Fatalf("args = %v, want handoff with processed digest", call.args)
	}
}

func TestRunSessionNoMatch(t *testing.T) {
	proj, cfgPath := runFixture(t)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"true", "--session", "zzz"}, &buf); code != 1 {
		t.Fatalf("run = %d, want 1", code)
	}
	if !strings.Contains(buf.String(), "no session matches") {
		t.Fatalf("output = %q", buf.String())
	}
}
