package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
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
	clientLine := ""
	if client != "" {
		clientLine = "client: " + client + "\n"
	}
	writeDigestFile(t, proj, state, name, clientLine, "@user-prompt 'hi'\n")
}

// writeDigestFile writes a digest with extra frontmatter lines (e.g.
// "client: codex\ncontinues_from: ../processed/s1.md\n") and a custom body.
func writeDigestFile(t *testing.T, proj, state, name, extraFront, body string) {
	t.Helper()
	dir := filepath.Join(proj, ".memoria", "sessions", state)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nschema_version: 2\nkind: session-digest\n" + extraFront + "---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// stubGit replaces gitCheckpoint with a canned result.
func stubGit(t *testing.T, out string) {
	t.Helper()
	orig := gitCheckpoint
	gitCheckpoint = func(dir string) string { return out }
	t.Cleanup(func() { gitCheckpoint = orig })
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

func TestDigestEvents(t *testing.T) {
	long := strings.Repeat("x", 3000)
	body := "@user-prompt 'hi'\n@user-prompt 'hi'\n\n@post-tool-use Edit a.go\n@user-prompt 'hi'\n@stop '" + long + "'\n"
	got := digestEvents(body)
	if len(got) != 4 {
		t.Fatalf("events = %d (%q), want 4", len(got), got)
	}
	if got[0] != "@user-prompt 'hi'" || got[1] != "@post-tool-use Edit a.go" {
		t.Fatalf("order/dedup wrong: %q", got[:2])
	}
	if got[2] != "@user-prompt 'hi'" {
		t.Fatalf("non-consecutive duplicate must be kept: %q", got[2])
	}
	if !strings.HasSuffix(got[3], "...") || len([]rune(got[3])) != eventLineMax+3 {
		t.Fatalf("long line not capped: %d runes", len([]rune(got[3])))
	}
}

func TestParseDigest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.md")
	content := "---\nschema_version: 2\nclient: codex\ncontinues_from: ../processed/s1.md\n---\n\n@user-prompt 'hi'\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	front, body := parseDigest(path)
	if !strings.Contains(front, "client: codex") || strings.Contains(front, "---") {
		t.Fatalf("front = %q", front)
	}
	if body != "@user-prompt 'hi'\n" {
		t.Fatalf("body = %q", body)
	}
	if got := frontKey(front, "client"); got != "codex" {
		t.Fatalf("client = %q", got)
	}
	if got := frontKey(front, "continues_from"); got != "../processed/s1.md" {
		t.Fatalf("continues_from = %q", got)
	}
	if got := frontKey(front, "ended_at"); got != "" {
		t.Fatalf("absent key = %q", got)
	}

	bare := filepath.Join(dir, "bare.md")
	if err := os.WriteFile(bare, []byte("@stop 'x'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	front, body = parseDigest(bare)
	if front != "" || body != "@stop 'x'\n" {
		t.Fatalf("no frontmatter: front %q body %q", front, body)
	}

	front, body = parseDigest(filepath.Join(dir, "missing.md"))
	if front != "" || body != "" {
		t.Fatalf("missing file: front %q body %q", front, body)
	}
}

func TestDigestChain(t *testing.T) {
	proj := t.TempDir()
	pending := filepath.Join(proj, ".memoria", "sessions", "pending")
	processed := filepath.Join(proj, ".memoria", "sessions", "processed")

	writeDigestFile(t, proj, "processed", "s1.md", "", "@user-prompt 'one'\n")
	writeDigestFile(t, proj, "pending", "s1-2.md", "continues_from: ../processed/s1.md\n", "@stop 'two'\n")
	chain := digestChain(filepath.Join(pending, "s1-2.md"))
	if len(chain) != 2 || !strings.HasSuffix(chain[0], filepath.Join("processed", "s1.md")) ||
		!strings.HasSuffix(chain[1], filepath.Join("pending", "s1-2.md")) {
		t.Fatalf("chain = %v, want oldest-first pair", chain)
	}

	writeDigestFile(t, proj, "pending", "s2.md", "continues_from: ../processed/nope.md\n", "")
	if chain := digestChain(filepath.Join(pending, "s2.md")); len(chain) != 1 {
		t.Fatalf("missing link target: chain = %v, want 1", chain)
	}

	writeDigestFile(t, proj, "processed", "a.md", "continues_from: ../processed/b.md\n", "")
	writeDigestFile(t, proj, "processed", "b.md", "continues_from: ../processed/a.md\n", "")
	if chain := digestChain(filepath.Join(processed, "a.md")); len(chain) != 2 {
		t.Fatalf("cycle: chain = %v, want 2", chain)
	}

	writeDigestFile(t, proj, "processed", "c-1.md", "", "")
	for i := 2; i <= 7; i++ {
		writeDigestFile(t, proj, "processed", fmt.Sprintf("c-%d.md", i),
			fmt.Sprintf("continues_from: ../processed/c-%d.md\n", i-1), "")
	}
	chain = digestChain(filepath.Join(processed, "c-7.md"))
	if len(chain) != chainMaxDepth {
		t.Fatalf("depth cap: %d links, want %d", len(chain), chainMaxDepth)
	}
	if !strings.HasSuffix(chain[0], "c-3.md") || !strings.HasSuffix(chain[len(chain)-1], "c-7.md") {
		t.Fatalf("depth cap must keep the newest end: %v", chain)
	}
}

func TestGitCheckpoint(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	if got := gitCheckpoint(t.TempDir()); got != "" {
		t.Fatalf("non-repo = %q, want empty", got)
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "first"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	got := gitCheckpoint(dir)
	if !strings.Contains(got, "HEAD: ") || !strings.Contains(got, "first") || !strings.Contains(got, "clean") {
		t.Fatalf("clean repo = %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := gitCheckpoint(dir); !strings.Contains(got, "dirty") {
		t.Fatalf("dirty repo = %q", got)
	}
}

func TestRunHandoffPacket(t *testing.T) {
	proj, cfgPath := runFixture(t)
	// chain: processed incarnation 1 ← pending incarnation 2 (picker's bbb-222)
	writeDigestFile(t, proj, "processed", "bbb-222.md", "client: claude-code\n",
		"@user-prompt 'fix the parser'\n@post-tool-use Edit cmd/run.go\n")
	writeDigestFile(t, proj, "pending", "bbb-222-2.md",
		"client: claude-code\ncontinues_from: ../processed/bbb-222.md\n",
		"@user-prompt 'continue'\n@user-prompt 'continue'\n@stop 'tests pass, commit next'\n@subagent-stop 'commit the wiki too'\n@session-end reason: prompt_input_exit\n")
	wikiDir := filepath.Join(proj, "wiki", "sessions")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wikiDir, "bbb-222.md"), []byte("The parser fix, summarized."), 0o644); err != nil {
		t.Fatal(err)
	}
	stubTTY(t, true)
	stubSelect(t, "bbb-222")
	stubGit(t, "HEAD: abc123 fix parser\nWorktree: clean")
	call := stubAgent(t, 0)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"true"}, &buf); code != 0 {
		t.Fatalf("run = %d: %s", code, buf.String())
	}
	if len(call.args) != 1 {
		t.Fatalf("args = %d, want one packet", len(call.args))
	}
	p := call.args[0]
	for _, want := range []string{
		"NOT starting a new task",
		"claude-code",
		"ALREADY RAN",
		"HEAD: abc123 fix parser",
		"@user-prompt 'fix the parser'",
		"@stop 'tests pass, commit next'",
		"The parser fix, summarized.",
		filepath.Join("processed", "bbb-222.md"),
		filepath.Join("pending", "bbb-222-2.md"),
		"Last reported state: @stop 'tests pass, commit next'",
		"Do NOT start working yet",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("packet missing %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, "Last reported state: @subagent-stop") {
		t.Fatalf("subagent-stop promoted over main @stop:\n%s", p)
	}
	if strings.Contains(p, "Continue the work from exactly") {
		t.Fatalf("old auto-continue footer still present:\n%s", p)
	}
	if strings.Count(p, "@user-prompt 'continue'") != 1 {
		t.Fatalf("consecutive duplicate not deduped:\n%s", p)
	}
	if strings.Index(p, "'fix the parser'") > strings.Index(p, "'continue'") {
		t.Fatalf("chain order wrong: incarnation 1 must come first\n%s", p)
	}
	last := 0
	for _, heading := range []string{"# Resuming", "## Git checkpoint", "## Session history", "## Session summary page", "## Continue"} {
		i := strings.Index(p, heading)
		if i < last {
			t.Fatalf("section %q missing or out of order (idx %d < %d)\n%s", heading, i, last, p)
		}
		last = i
	}
}

func TestRunHandoffMultirepoCheckpoint(t *testing.T) {
	proj := t.TempDir() // parent is NOT a repo; two child repos
	for _, svc := range []string{"svc-a", "svc-b"} {
		if err := os.MkdirAll(filepath.Join(proj, svc, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	orig := gitCheckpoint
	gitCheckpoint = func(dir string) string {
		switch dir {
		case filepath.Join(proj, "svc-a"):
			return "HEAD: aaa svc-a work"
		case filepath.Join(proj, "svc-b"):
			return "HEAD: bbb svc-b work"
		}
		return "" // parent root: not a repo
	}
	t.Cleanup(func() { gitCheckpoint = orig })
	writeDigestFile(t, proj, "pending", "ddd-444.md", "client: claude-code\n",
		"@user-prompt 'fix both services'\n"+
			"@post-tool-use Write "+filepath.Join(proj, "svc-a", "main.go")+"\n"+
			"@post-tool-use Bash 'go test ./...'\n"+
			"@post-tool-use Edit "+filepath.Join(proj, "svc-b", "api.go")+" error: 'oops'\n"+
			"@stop 'done'\n")
	digest := filepath.Join(proj, ".memoria", "sessions", "pending", "ddd-444.md")
	p := buildHandoff(proj, filepath.Join(proj, "wiki"), "ddd-444", digest, true)
	if !strings.Contains(p, "per touched repo") {
		t.Fatalf("multirepo checkpoint section missing:\n%s", p)
	}
	for _, want := range []string{"### svc-a", "HEAD: aaa svc-a work", "### svc-b", "HEAD: bbb svc-b work"} {
		if !strings.Contains(p, want) {
			t.Fatalf("packet missing %q:\n%s", want, p)
		}
	}
	if strings.Index(p, "### svc-b") > strings.Index(p, "### svc-a") {
		t.Fatalf("newest-touched repo must come first:\n%s", p)
	}
}

func TestRunHandoffLeadSubagentFallback(t *testing.T) {
	proj := t.TempDir()
	stubGit(t, "")
	writeDigestFile(t, proj, "pending", "ccc-333.md", "client: claude-code\n",
		"@user-prompt 'do thing'\n@subagent-stop 'internal note'\n")
	digest := filepath.Join(proj, ".memoria", "sessions", "pending", "ccc-333.md")
	p := buildHandoff(proj, filepath.Join(proj, "wiki"), "ccc-333", digest, true)
	if !strings.Contains(p, "Last reported state (internal subagent note — not a user request): @subagent-stop 'internal note'") {
		t.Fatalf("fallback lead missing or unlabeled:\n%s", p)
	}
}

func TestRunHandoffPacketOmissions(t *testing.T) {
	proj, cfgPath := runFixture(t) // bbb-222: no client, no wiki page
	stubTTY(t, true)
	stubSelect(t, "bbb-222")
	stubGit(t, "")
	call := stubAgent(t, 0)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"true"}, &buf); code != 0 {
		t.Fatalf("run = %d: %s", code, buf.String())
	}
	p := call.args[0]
	if strings.Contains(p, "## Git checkpoint") || strings.Contains(p, "## Session summary page") {
		t.Fatalf("empty sections must be omitted:\n%s", p)
	}
	if !strings.Contains(p, "unknown harness") {
		t.Fatalf("missing client fallback absent:\n%s", p)
	}
	if !strings.Contains(p, filepath.Join("pending", "bbb-222.md")) {
		t.Fatalf("digest path missing:\n%s", p)
	}
}

func TestRunHandoffPacketBudget(t *testing.T) {
	proj, cfgPath := runFixture(t)
	var body strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&body, "@post-tool-use Bash 'marker-%02d %s'\n", i, strings.Repeat("x", 1500))
	}
	writeDigestFile(t, proj, "pending", "bbb-222.md", "", body.String())
	stubTTY(t, true)
	stubSelect(t, "bbb-222")
	stubGit(t, "HEAD: abc123 x\nWorktree: clean")
	call := stubAgent(t, 0)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"true"}, &buf); code != 0 {
		t.Fatalf("run = %d: %s", code, buf.String())
	}
	p := call.args[0]
	if len(p) > packetBudget+1000 {
		t.Fatalf("packet = %d bytes, want ~%d", len(p), packetBudget)
	}
	if !strings.Contains(p, "marker-39") {
		t.Fatal("newest event missing")
	}
	if strings.Contains(p, "marker-00") {
		t.Fatal("oldest event should be dropped first")
	}
	if !strings.Contains(p, "older events omitted") {
		t.Fatal("truncation note missing")
	}
	for _, want := range []string{"NOT starting a new task", "HEAD: abc123 x", "## Continue"} {
		if !strings.Contains(p, want) {
			t.Fatalf("fixed section lost to budget: %q", want)
		}
	}
}

func TestRunHandoffPacketWikiCapAndOverride(t *testing.T) {
	proj := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := "projects:\n  - name: p\n    path: " + proj + "\n    wiki: notes\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proj, ".memoria"), 0o755); err != nil {
		t.Fatal(err)
	}
	index := "2026-07-28T11:00:00-03:00 - bbb-222 - add search command\n"
	if err := os.WriteFile(filepath.Join(proj, ".memoria", "sessions.md"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	writeDigestFile(t, proj, "pending", "bbb-222.md", "", "@user-prompt 'hi'\n")
	pageDir := filepath.Join(proj, "notes", "sessions")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	page := "SUMMARYSTART " + strings.Repeat("w", 20000) + " SUMMARYEND"
	if err := os.WriteFile(filepath.Join(pageDir, "bbb-222.md"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	stubTTY(t, true)
	stubSelect(t, "bbb-222")
	stubGit(t, "")
	call := stubAgent(t, 0)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"true"}, &buf); code != 0 {
		t.Fatalf("run = %d: %s", code, buf.String())
	}
	p := call.args[0]
	if !strings.Contains(p, "SUMMARYSTART") {
		t.Fatal("wiki page head missing")
	}
	if strings.Contains(p, "SUMMARYEND") {
		t.Fatal("wiki page not truncated")
	}
	if !strings.Contains(p, "page truncated") {
		t.Fatal("wiki truncation note missing")
	}
	if !strings.Contains(p, filepath.Join("notes", "sessions", "bbb-222.md")) {
		t.Fatal("wiki override path not used")
	}
}
