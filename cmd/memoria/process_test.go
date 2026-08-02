package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tracked project with one ended pending session and an existing wiki page
func processFixture(t *testing.T) (proj, cfgPath, digest string) {
	t.Helper()
	proj = t.TempDir()
	cfgPath = testConfig(t, proj)
	digest = digestFile(proj, "s1")
	if err := os.MkdirAll(filepath.Dir(digest), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nkind: session-digest\n---\n\n@user-prompt 'add a queue'\n@post-tool-use Write /p/queue.go\n"
	if err := os.WriteFile(digest, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := queueAdd(queuePath(cfgPath), filepath.Base(proj), digest); err != nil {
		t.Fatal(err)
	}
	if err := queueMarkEnded(queuePath(cfgPath), filepath.Base(proj), digest); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proj, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(proj, "wiki", "index.md")
	if err := os.WriteFile(existing, []byte("# Index\n\nold index\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return proj, cfgPath, digest
}

// stubProcessor replaces invokeProcessor, capturing the prompt
func stubProcessor(t *testing.T, response string, err error) *string {
	t.Helper()
	var prompt string
	orig := invokeProcessor
	invokeProcessor = func(cfg config, dir, p string) (string, error) {
		prompt = p
		return response, err
	}
	t.Cleanup(func() { invokeProcessor = orig })
	return &prompt
}

const goodProposalPages = `{"pages":[
	{"path":"index.md","title":"Index","tags":[],"body_markdown":"# Index\n\n[[concepts/queue]]\n"},
	{"path":"concepts/queue.md","title":"Queue","tags":["queue","worker"],"body_markdown":"# Queue\n\nHow the queue works.\n"}
]}`

func TestProcessWritesProposal(t *testing.T) {
	proj, cfgPath, digest := processFixture(t)
	prompt := stubProcessor(t, "```json\n"+goodProposalPages+"\n```", nil)
	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("process = %d: %s", code, buf.String())
	}
	b, err := os.ReadFile(filepath.Join(proj, ".memoria", "proposal.json"))
	if err != nil {
		t.Fatalf("proposal not written: %v", err)
	}
	var p proposal
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p.Project != filepath.Base(proj) || len(p.Sessions) != 1 || p.Sessions[0] != digest || len(p.Pages) != 2 {
		t.Fatalf("proposal meta wrong: %+v", p)
	}
	for _, w := range []string{"FAITHFULNESS", "old index", "add a queue", `"pages"`} {
		if !strings.Contains(*prompt, w) {
			t.Fatalf("prompt missing %q", w)
		}
	}
	if !strings.Contains(buf.String(), "concepts/queue.md") || !strings.Contains(buf.String(), "--apply") {
		t.Fatalf("summary missing: %s", buf.String())
	}
}

func TestProcessNothingPending(t *testing.T) {
	proj := t.TempDir()
	cfgPath := testConfig(t, proj)
	stubProcessor(t, "", fmt.Errorf("must not be called"))
	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, nil, &buf); code != 0 {
		t.Fatalf("process = %d: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "Nothing to process") {
		t.Fatalf("missing message: %s", buf.String())
	}
}

func TestProcessSkipsUnendedSessions(t *testing.T) {
	proj, cfgPath, digest := processFixture(t)
	// add a second, un-ended session — only the ended one may reach the prompt
	d2 := digestFile(proj, "s2")
	if err := os.WriteFile(d2, []byte("@user-prompt 'unfinished work'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := queueAdd(queuePath(cfgPath), filepath.Base(proj), d2); err != nil {
		t.Fatal(err)
	}
	prompt := stubProcessor(t, goodProposalPages, nil)
	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("process = %d: %s", code, buf.String())
	}
	if strings.Contains(*prompt, "unfinished work") {
		t.Fatal("un-ended session leaked into prompt")
	}
	var p proposal
	b, _ := os.ReadFile(filepath.Join(proj, ".memoria", "proposal.json"))
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Sessions) != 1 || p.Sessions[0] != digest {
		t.Fatalf("sessions = %v, want only ended", p.Sessions)
	}
}

func TestProcessRejectsBadPages(t *testing.T) {
	for _, bad := range []string{
		`{"pages":[{"path":"../evil.md","title":"x","body_markdown":"y"}]}`,
		`{"pages":[{"path":"secrets/x.md","title":"x","body_markdown":"y"}]}`,
		`{"pages":[{"path":"concepts/x.txt","title":"x","body_markdown":"y"}]}`,
		`{"pages":[{"path":"trash/x.md","title":"x","body_markdown":"y"}]}`,
		`{"pages":[{"path":"concepts/x.md","title":"","body_markdown":""}]}`,
		`{"pages":[]}`,
		`not json at all`,
	} {
		proj, cfgPath, _ := processFixture(t)
		stubProcessor(t, bad, nil)
		var buf bytes.Buffer
		if code := runProcess(proj, cfgPath, []string{"--foreground"}, &buf); code != 1 {
			t.Fatalf("bad proposal %q accepted: %d %s", bad, code, buf.String())
		}
		if _, err := os.Stat(filepath.Join(proj, ".memoria", "proposal.json")); !os.IsNotExist(err) {
			t.Fatalf("proposal written despite invalid pages: %q", bad)
		}
	}
}

func TestProcessDropsInvalidPagesKeepsRest(t *testing.T) {
	proj, cfgPath, _ := processFixture(t)
	stubProcessor(t, `{"pages":[
		{"path":"concepts/queue.md","title":"Queue","body_markdown":"# Queue\n"},
		{"path":"secrets/x.md","title":"x","body_markdown":"y"}
	]}`, nil)
	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("process = %d: %s", code, buf.String())
	}
	var p proposal
	b, err := os.ReadFile(filepath.Join(proj, ".memoria", "proposal.json"))
	if err != nil {
		t.Fatalf("proposal not written: %v", err)
	}
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Pages) != 1 || p.Pages[0].Path != "concepts/queue.md" {
		t.Fatalf("pages = %+v, want only concepts/queue.md", p.Pages)
	}
	if !strings.Contains(buf.String(), "warning:") || !strings.Contains(buf.String(), "secrets/x.md") {
		t.Fatalf("warning missing: %s", buf.String())
	}
	st, _ := os.ReadFile(statusPath(cfgPath))
	if !strings.Contains(string(st), "dropped") {
		t.Fatalf("status detail missing dropped note: %s", st)
	}
}

func TestProcessCustomFolderTarget(t *testing.T) {
	proj, cfgPath, _ := processFixture(t)
	if err := os.MkdirAll(filepath.Join(proj, "wiki", "research"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubProcessor(t, `{"pages":[{"path":"research/notes.md","title":"Notes","body_markdown":"# Notes\n"}]}`, nil)
	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("process = %d: %s", code, buf.String())
	}
	var p proposal
	b, _ := os.ReadFile(filepath.Join(proj, ".memoria", "proposal.json"))
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Pages) != 1 || p.Pages[0].Path != "research/notes.md" {
		t.Fatalf("pages = %+v, want research/notes.md", p.Pages)
	}
}

func TestProcessApply(t *testing.T) {
	proj, cfgPath, digest := processFixture(t)
	stubProcessor(t, goodProposalPages, nil)
	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("process = %d: %s", code, buf.String())
	}
	buf.Reset()
	if code := runProcess(proj, cfgPath, []string{"--apply"}, &buf); code != 0 {
		t.Fatalf("apply = %d: %s", code, buf.String())
	}
	b, err := os.ReadFile(filepath.Join(proj, "wiki", "concepts", "queue.md"))
	if err != nil || !strings.Contains(string(b), "How the queue works") {
		t.Fatalf("wiki page not written: %v %q", err, b)
	}
	if !strings.HasPrefix(string(b), "---\ntags: [queue, worker]\n---\n\n") {
		t.Fatalf("tags frontmatter not rendered: %q", b)
	}
	b, _ = os.ReadFile(filepath.Join(proj, "wiki", "index.md"))
	if !strings.Contains(string(b), "[[concepts/queue]]") {
		t.Fatalf("index not updated: %q", b)
	}
	if strings.HasPrefix(string(b), "---") {
		t.Fatalf("empty tags must not render frontmatter: %q", b)
	}
	if _, err := os.Stat(digest); !os.IsNotExist(err) {
		t.Fatal("digest still in pending/")
	}
	moved := filepath.Join(proj, ".memoria", "sessions", "processed", "s1.md")
	if _, err := os.Stat(moved); err != nil {
		t.Fatalf("digest not moved to processed/: %v", err)
	}
	qb, _ := os.ReadFile(queuePath(cfgPath))
	if strings.Contains(string(qb), "s1.md") {
		t.Fatalf("queue entry not removed:\n%s", qb)
	}
	if _, err := os.Stat(filepath.Join(proj, ".memoria", "proposal.json")); !os.IsNotExist(err) {
		t.Fatal("proposal.json not deleted after apply")
	}
}

func TestProcessApplyRejectsTamperedProposal(t *testing.T) {
	proj, cfgPath, digest := processFixture(t)
	p := proposal{Project: filepath.Base(proj), Sessions: []string{digest},
		Pages: []wikiPage{{Path: "../../evil.md", Title: "x", BodyMarkdown: "y"}}}
	b, _ := json.Marshal(p)
	if err := os.WriteFile(filepath.Join(proj, ".memoria", "proposal.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, []string{"--apply"}, &buf); code != 1 {
		t.Fatalf("tampered proposal applied: %d %s", code, buf.String())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(proj), "evil.md")); !os.IsNotExist(err) {
		t.Fatal("escaped the wiki root")
	}
}

func TestProcessApplyWithoutProposal(t *testing.T) {
	proj, cfgPath, _ := processFixture(t)
	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, []string{"--apply"}, &buf); code != 1 {
		t.Fatalf("apply without proposal = %d, want 1", code)
	}
}

func TestWikiPromptOverride(t *testing.T) {
	proj, cfgPath, _ := processFixture(t)
	stubProcessor(t, goodProposalPages, nil)
	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("process = %d: %s", code, buf.String())
	}
	pp := filepath.Join(filepath.Dir(cfgPath), "wiki-prompt.md")
	if _, err := os.Stat(pp); !os.IsNotExist(err) {
		t.Fatalf("embedded default should not be written to disk: %v", err)
	}
	if err := os.WriteFile(pp, []byte("CUSTOM RULES ONLY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt := stubProcessor(t, goodProposalPages, nil)
	if code := runProcess(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("second process = %d: %s", code, buf.String())
	}
	if !strings.Contains(*prompt, "CUSTOM RULES ONLY") || strings.Contains(*prompt, "FAITHFULNESS") {
		t.Fatal("user-edited prompt file not respected")
	}
}

func TestExtractJSON(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a":1}`, `{"a":1}`},
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		{"here you go:\n{\"a\":1}\ndone", `{"a":1}`},
	}
	for _, tc := range cases {
		got, err := extractJSON(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("extractJSON(%q) = %q, %v", tc.in, got, err)
		}
	}
	if _, err := extractJSON("no braces here"); err == nil {
		t.Fatal("want error for missing JSON")
	}
}

// stubSpawn replaces spawnDetached, recording the call
func stubSpawn(t *testing.T, pid int) *[]string {
	t.Helper()
	var got []string
	orig := spawnDetached
	spawnDetached = func(dir, logFile string, args ...string) (int, error) {
		got = append([]string{dir}, args...)
		return pid, nil
	}
	t.Cleanup(func() { spawnDetached = orig })
	return &got
}

func TestProcessDetachesByDefault(t *testing.T) {
	proj, cfgPath, _ := processFixture(t)
	stubProcessor(t, "", fmt.Errorf("parent must not invoke the processor"))
	spawned := stubSpawn(t, 4242)
	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, nil, &buf); code != 0 {
		t.Fatalf("process = %d: %s", code, buf.String())
	}
	want := []string{proj, "process", "--foreground"}
	if len(*spawned) != 3 || (*spawned)[0] != want[0] || (*spawned)[1] != want[1] || (*spawned)[2] != want[2] {
		t.Fatalf("spawn args = %v, want %v", *spawned, want)
	}
	if !strings.Contains(buf.String(), "4242") || !strings.Contains(buf.String(), "background") {
		t.Fatalf("detach message missing: %s", buf.String())
	}
	st, _ := loadStatus(statusPath(cfgPath))
	e := st[filepath.Base(proj)]
	if e.State != "running" || e.PID != 4242 {
		t.Fatalf("status not running: %+v", e)
	}
}

func TestProcessRefusesConcurrentRun(t *testing.T) {
	proj, cfgPath, _ := processFixture(t)
	if err := statusSet(statusPath(cfgPath), filepath.Base(proj), "running", os.Getpid(), ""); err != nil {
		t.Fatal(err)
	}
	spawned := stubSpawn(t, 4242)
	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, nil, &buf); code != 1 {
		t.Fatalf("concurrent run allowed: %d %s", code, buf.String())
	}
	if len(*spawned) != 0 {
		t.Fatalf("spawned despite running: %v", *spawned)
	}
	if !strings.Contains(buf.String(), "already running") {
		t.Fatalf("message missing: %s", buf.String())
	}
}

func TestProcessStaleRunningRespawns(t *testing.T) {
	proj, cfgPath, _ := processFixture(t)
	if err := statusSet(statusPath(cfgPath), filepath.Base(proj), "running", 999999999, ""); err != nil {
		t.Fatal(err)
	}
	spawned := stubSpawn(t, 4242)
	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, nil, &buf); code != 0 {
		t.Fatalf("stale running blocked respawn: %d %s", code, buf.String())
	}
	if len(*spawned) == 0 {
		t.Fatal("not respawned over dead pid")
	}
}

func TestForegroundWritesStatus(t *testing.T) {
	proj, cfgPath, _ := processFixture(t)
	stubProcessor(t, goodProposalPages, nil)
	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("process = %d: %s", code, buf.String())
	}
	st, _ := loadStatus(statusPath(cfgPath))
	e := st[filepath.Base(proj)]
	if e.State != "done" || !strings.Contains(e.Detail, "2 page") {
		t.Fatalf("done status wrong: %+v", e)
	}

	proj2, cfgPath2, _ := processFixture(t)
	stubProcessor(t, "", fmt.Errorf("claude exploded"))
	buf.Reset()
	if code := runProcess(proj2, cfgPath2, []string{"--foreground"}, &buf); code != 1 {
		t.Fatalf("processor error swallowed: %d %s", code, buf.String())
	}
	st, _ = loadStatus(statusPath(cfgPath2))
	e = st[filepath.Base(proj2)]
	if e.State != "error" || !strings.Contains(e.Detail, "claude exploded") {
		t.Fatalf("error status wrong: %+v", e)
	}
}

// enableNotifications flips the flag in an existing test config
func enableNotifications(t *testing.T, cfgPath string) {
	t.Helper()
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Notifications = true
	if err := saveConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
}

func TestProcessNotifiesOnDoneAndError(t *testing.T) {
	proj, cfgPath, _ := processFixture(t)
	enableNotifications(t, cfgPath)
	stubProcessor(t, goodProposalPages, nil)
	got := stubNotify(t)
	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("process = %d: %s", code, buf.String())
	}
	if len(*got) != 1 || !strings.Contains((*got)[0][1], "Proposal ready") {
		t.Fatalf("success notification wrong: %v", *got)
	}

	proj2, cfgPath2, _ := processFixture(t)
	enableNotifications(t, cfgPath2)
	stubProcessor(t, "", fmt.Errorf("claude exploded"))
	got = stubNotify(t)
	buf.Reset()
	if code := runProcess(proj2, cfgPath2, []string{"--foreground"}, &buf); code != 1 {
		t.Fatalf("process = %d: %s", code, buf.String())
	}
	if len(*got) != 1 || !strings.Contains((*got)[0][1], "failed") {
		t.Fatalf("error notification wrong: %v", *got)
	}
}

func TestProcessNoNotificationWhenDisabled(t *testing.T) {
	proj, cfgPath, _ := processFixture(t)
	stubProcessor(t, goodProposalPages, nil)
	got := stubNotify(t)
	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("process = %d: %s", code, buf.String())
	}
	if len(*got) != 0 {
		t.Fatalf("notified despite disabled config: %v", *got)
	}
}

func TestInvokeGemini(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "k123" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprintln(w, `{"candidates":[{"content":{"parts":[{"text":"{\"pages\":[]}"}]}}]}`)
	}))
	defer srv.Close()
	orig := geminiGenerateURL
	geminiGenerateURL = srv.URL
	defer func() { geminiGenerateURL = orig }()
	t.Setenv("GEMINI_API_KEY", "")

	out, err := invokeProcessor(config{Processor: "gemini", GeminiAPIKey: "k123"}, "", "hi")
	if err != nil || out != `{"pages":[]}` {
		t.Fatalf("gemini = %q, %v", out, err)
	}
	if _, err := invokeProcessor(config{Processor: "gemini", GeminiAPIKey: "bad"}, "", "hi"); err == nil {
		t.Fatal("bad key should error")
	}
	if _, err := invokeProcessor(config{Processor: "ollama"}, "", "hi"); err == nil || !strings.Contains(err.Error(), "coming soon") {
		t.Fatalf("ollama placeholder: %v", err)
	}
	if _, err := invokeProcessor(config{}, "", "hi"); err == nil {
		t.Fatal("no processor should error")
	}
}

// sweepFixture registers one project per marker in a single config; non-empty
// markers get an ended pending digest containing the marker text.
func sweepFixture(t *testing.T, markers []string) (projs []string, cfgPath string) {
	t.Helper()
	for range markers {
		projs = append(projs, t.TempDir())
	}
	cfgPath = testConfig(t, projs...)
	for i, m := range markers {
		if m == "" {
			continue
		}
		d := digestFile(projs[i], "s1")
		if err := os.MkdirAll(filepath.Dir(d), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(d, []byte("@user-prompt '"+m+"'\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := queueAdd(queuePath(cfgPath), filepath.Base(projs[i]), d); err != nil {
			t.Fatal(err)
		}
		if err := queueMarkEnded(queuePath(cfgPath), filepath.Base(projs[i]), d); err != nil {
			t.Fatal(err)
		}
	}
	return projs, cfgPath
}

// countingProcessor stubs invokeProcessor with a per-call response, recording
// call count and prompts.
func countingProcessor(t *testing.T, respond func(call int) (string, error)) (*int, *[]string) {
	t.Helper()
	calls := 0
	var prompts []string
	orig := invokeProcessor
	invokeProcessor = func(cfg config, dir, p string) (string, error) {
		calls++
		prompts = append(prompts, p)
		return respond(calls)
	}
	t.Cleanup(func() { invokeProcessor = orig })
	return &calls, &prompts
}

func TestProcessAllSweepsProjects(t *testing.T) {
	projs, cfgPath := sweepFixture(t, []string{"alpha work", "beta work", ""})
	calls, _ := countingProcessor(t, func(int) (string, error) { return goodProposalPages, nil })
	var buf bytes.Buffer
	// cwd is an unrelated dir — --all must not need a tracked project
	if code := runProcess(t.TempDir(), cfgPath, []string{"--all"}, &buf); code != 0 {
		t.Fatalf("process --all = %d: %s", code, buf.String())
	}
	for _, p := range projs[:2] {
		if _, err := os.Stat(filepath.Join(p, ".memoria", "proposal.json")); err != nil {
			t.Fatalf("proposal missing in %s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(projs[2], ".memoria", "proposal.json")); !os.IsNotExist(err) {
		t.Fatal("proposal written for project without work")
	}
	if *calls != 2 {
		t.Fatalf("processor invoked %d times, want 2", *calls)
	}
}

func TestProcessAllApply(t *testing.T) {
	projs, cfgPath := sweepFixture(t, []string{"alpha", "beta"})
	countingProcessor(t, func(int) (string, error) { return goodProposalPages, nil })
	var buf bytes.Buffer
	if code := runProcess(t.TempDir(), cfgPath, []string{"--all", "--apply"}, &buf); code != 0 {
		t.Fatalf("process --all --apply = %d: %s", code, buf.String())
	}
	for _, p := range projs {
		if _, err := os.Stat(filepath.Join(p, "wiki", "concepts", "queue.md")); err != nil {
			t.Fatalf("wiki not written in %s: %v", p, err)
		}
		if _, err := os.Stat(filepath.Join(p, ".memoria", "sessions", "processed", "s1.md")); err != nil {
			t.Fatalf("digest not moved in %s: %v", p, err)
		}
		if _, err := os.Stat(filepath.Join(p, ".memoria", "proposal.json")); !os.IsNotExist(err) {
			t.Fatalf("proposal not consumed in %s", p)
		}
	}
	qb, _ := os.ReadFile(queuePath(cfgPath))
	if strings.Contains(string(qb), "s1.md") {
		t.Fatalf("queue entries not removed:\n%s", qb)
	}
}

func TestProcessAllContinuesOnFailure(t *testing.T) {
	projs, cfgPath := sweepFixture(t, []string{"alpha", "beta"})
	countingProcessor(t, func(call int) (string, error) {
		if call == 1 {
			return "", fmt.Errorf("boom")
		}
		return goodProposalPages, nil
	})
	var buf bytes.Buffer
	if code := runProcess(t.TempDir(), cfgPath, []string{"--all"}, &buf); code != 1 {
		t.Fatalf("failure swallowed: %d %s", code, buf.String())
	}
	if _, err := os.Stat(filepath.Join(projs[1], ".memoria", "proposal.json")); err != nil {
		t.Fatalf("second project not processed after first failed: %v", err)
	}
	st, _ := loadStatus(statusPath(cfgPath))
	if st[filepath.Base(projs[0])].State != "error" {
		t.Fatalf("failed project status = %+v, want error", st[filepath.Base(projs[0])])
	}
}

func TestProcessAllNothingPending(t *testing.T) {
	_, cfgPath := sweepFixture(t, []string{""})
	stubProcessor(t, "", fmt.Errorf("must not be called"))
	var buf bytes.Buffer
	if code := runProcess(t.TempDir(), cfgPath, []string{"--all"}, &buf); code != 0 {
		t.Fatalf("process --all = %d: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "Nothing to process") {
		t.Fatalf("missing message: %s", buf.String())
	}
}

func TestProcessAllSkipsRunningProject(t *testing.T) {
	projs, cfgPath := sweepFixture(t, []string{"alpha work", "beta work"})
	if err := statusSet(statusPath(cfgPath), filepath.Base(projs[0]), "running", os.Getpid(), ""); err != nil {
		t.Fatal(err)
	}
	calls, prompts := countingProcessor(t, func(int) (string, error) { return goodProposalPages, nil })
	var buf bytes.Buffer
	if code := runProcess(t.TempDir(), cfgPath, []string{"--all"}, &buf); code != 0 {
		t.Fatalf("process --all = %d: %s", code, buf.String())
	}
	if *calls != 1 || strings.Contains((*prompts)[0], "alpha work") {
		t.Fatalf("running project reached the processor: calls=%d", *calls)
	}
	if _, err := os.Stat(filepath.Join(projs[1], ".memoria", "proposal.json")); err != nil {
		t.Fatalf("non-running project skipped: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projs[0], ".memoria", "proposal.json")); !os.IsNotExist(err) {
		t.Fatal("running project processed")
	}
}

func TestRenderPage(t *testing.T) {
	if got := renderPage([]string{"a", "b"}, "# T\n\nbody\n"); got != "---\ntags: [a, b]\n---\n\n# T\n\nbody\n" {
		t.Fatalf("with tags: %q", got)
	}
	if got := renderPage(nil, "# T\n"); got != "# T\n" {
		t.Fatalf("no tags: %q", got)
	}
}

func TestValidPagePath(t *testing.T) {
	dirs := map[string]bool{"research": true, "trash": true, "_global": true, ".obsidian": true}
	for p, want := range map[string]bool{
		"index.md":            true,
		"concepts/x.md":       true,
		"sessions/s1.md":      true,
		"research/x.md":       true,  // existing custom folder
		"notes/x.md":          false, // folder does not exist
		"sessions/../x.md":    false,
		"trash/x.md":          false, // reserved even though listed in dirs
		"trash/concepts/x.md": false,
		"_global/x.md":        false, // reserved
		".obsidian/x.md":      false, // dot-dir reserved
		"concepts/.hidden.md": false, // dot-file reserved
		"research.md":         false, // top-level file outside index.md
	} {
		if validPagePath(p, dirs) != want {
			t.Fatalf("validPagePath(%q) = %v, want %v", p, !want, want)
		}
	}
}

func TestWikiDirs(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "x.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := wikiDirs(root)
	if !got["notes"] || len(got) != 1 {
		t.Fatalf("wikiDirs = %v, want only notes", got)
	}
	if got := wikiDirs(filepath.Join(root, "missing")); len(got) != 0 {
		t.Fatalf("missing root: %v, want empty", got)
	}
}

func TestReadWikiSkipsTrash(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"concepts/live.md", "trash/concepts/dead.md", "trash/gone.md"} {
		dst := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wiki := readWiki(root)
	if _, ok := wiki["concepts/live.md"]; !ok || len(wiki) != 1 {
		t.Fatalf("readWiki = %v, want only concepts/live.md", wiki)
	}
}

func setAutoApply(t *testing.T, cfgPath string) {
	t.Helper()
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AutoApply = true
	if err := saveConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
}

func TestProcessForegroundAutoApplies(t *testing.T) {
	proj, cfgPath, digest := processFixture(t)
	setAutoApply(t, cfgPath)
	stubProcessor(t, goodProposalPages, nil)
	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("process = %d: %s", code, buf.String())
	}
	b, err := os.ReadFile(filepath.Join(proj, "wiki", "concepts", "queue.md"))
	if err != nil || !strings.Contains(string(b), "How the queue works") {
		t.Fatalf("page not auto-applied: %v %q", err, b)
	}
	if _, err := os.Stat(filepath.Join(proj, ".memoria", "proposal.json")); !os.IsNotExist(err) {
		t.Fatal("proposal left behind despite auto_apply")
	}
	if _, err := os.Stat(digest); !os.IsNotExist(err) {
		t.Fatal("digest not archived")
	}
	st, _ := loadStatus(statusPath(cfgPath))
	if !strings.Contains(st[filepath.Base(proj)].Detail, "applied") {
		t.Fatalf("status = %+v, want applied detail", st[filepath.Base(proj)])
	}
}

func TestProcessAllAutoApplyNoDouble(t *testing.T) {
	projs, cfgPath := sweepFixture(t, []string{"alpha work"})
	setAutoApply(t, cfgPath)
	countingProcessor(t, func(int) (string, error) { return goodProposalPages, nil })
	var buf bytes.Buffer
	if code := runProcess(t.TempDir(), cfgPath, []string{"--all", "--apply"}, &buf); code != 0 {
		t.Fatalf("process --all --apply = %d: %s", code, buf.String())
	}
	if _, err := os.Stat(filepath.Join(projs[0], "wiki", "concepts", "queue.md")); err != nil {
		t.Fatalf("page not written: %v", err)
	}
}

func TestDigestHasContent(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"bookends only", "---\nkind: session-digest\n---\n\n@session-start source: resume\n@session-end reason: prompt_input_exit\n", false},
		{"frontmatter only", "---\nkind: session-digest\n---\n", false},
		{"user prompt", "---\n---\n\n@session-start source: startup\n@user-prompt 'fix the parser'\n@session-end\n", true},
		{"tool use", "---\n---\n\n@session-start source: resume\n@post-tool-use Write /p/a.go\n", true},
		{"prose mentioning @session-start", "---\n---\n\nthe agent logged @session-start twice\n", false},
	}
	for _, c := range cases {
		if got := digestHasContent(c.body); got != c.want {
			t.Errorf("%s: digestHasContent = %v, want %v", c.name, got, c.want)
		}
	}
}

// an empty digest must never reach the processor: the LLM rightly returns zero
// pages for it, which used to fail the whole batch and leave the entry stuck in
// the queue re-failing forever.
func TestCollectEndedRetiresEmptyDigests(t *testing.T) {
	proj, cfgPath, real := processFixture(t)
	projName := filepath.Base(proj)
	empty := digestFile(proj, "s2")
	body := "---\nkind: session-digest\n---\n\n@session-start source: resume\n@session-end reason: prompt_input_exit\n"
	if err := os.WriteFile(empty, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := queueAdd(queuePath(cfgPath), projName, empty); err != nil {
		t.Fatal(err)
	}
	if err := queueMarkEnded(queuePath(cfgPath), projName, empty); err != nil {
		t.Fatal(err)
	}

	sessions, digests, err := collectEnded(queuePath(cfgPath), projName)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0] != real {
		t.Fatalf("sessions = %v, want only %s", sessions, real)
	}
	if _, ok := digests[empty]; ok {
		t.Error("empty digest reached the prompt")
	}
	if _, err := os.Stat(empty); !os.IsNotExist(err) {
		t.Error("empty digest still in pending/")
	}
	archived := filepath.Join(proj, ".memoria", "sessions", "processed", "s2.md")
	if _, err := os.Stat(archived); err != nil {
		t.Errorf("empty digest not archived: %v", err)
	}
	q, err := loadQueue(queuePath(cfgPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range q[projName] {
		if e.Path == empty {
			t.Error("empty digest still queued")
		}
	}
}

// a project whose only ended session is empty has nothing to process — it must
// report that, not error out and fire a failure notification.
func TestProcessEmptyDigestOnlyIsNotAnError(t *testing.T) {
	proj := t.TempDir()
	cfgPath := testConfig(t, proj)
	projName := filepath.Base(proj)
	empty := digestFile(proj, "s1")
	if err := os.MkdirAll(filepath.Dir(empty), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nkind: session-digest\n---\n\n@session-start source: resume\n@session-end reason: prompt_input_exit\n"
	if err := os.WriteFile(empty, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := queueAdd(queuePath(cfgPath), projName, empty); err != nil {
		t.Fatal(err)
	}
	if err := queueMarkEnded(queuePath(cfgPath), projName, empty); err != nil {
		t.Fatal(err)
	}
	stubProcessor(t, "", fmt.Errorf("processor must not be invoked"))

	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("process = %d, want 0: %s", code, buf.String())
	}
	st, _ := loadStatus(statusPath(cfgPath))
	if st[projName].State != "done" {
		t.Fatalf("status = %+v, want done", st[projName])
	}
}

func TestCanonicalizeSessionPaths(t *testing.T) {
	// a uuid whose last segment is all digits — the incarnation suffix must be
	// matched against known ids, never guessed off the shape of the name
	digitTail := "9f2c1a7b-4e55-4c31-8a90-400060889612"
	sids := []string{"s1", digitTail}
	pages := []wikiPage{
		{Path: "sessions/s1-2.md", Title: "T", BodyMarkdown: "b"},
		{Path: "sessions/" + digitTail + ".md", Title: "T", BodyMarkdown: "b"},
		{Path: "concepts/queue.md", Title: "T", BodyMarkdown: "b"},
		{Path: "sessions/not-a-session.md", Title: "T", BodyMarkdown: "b"},
	}
	got, dropped := canonicalizeSessionPaths(pages, sids)
	want := []string{"sessions/s1.md", "sessions/" + digitTail + ".md", "concepts/queue.md"}
	if len(got) != len(want) {
		t.Fatalf("got %d pages, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Path != w {
			t.Errorf("page %d = %q, want %q", i, got[i].Path, w)
		}
	}
	if len(dropped) != 1 || !strings.Contains(dropped[0], "not-a-session") {
		t.Errorf("dropped = %v, want the unknown session page", dropped)
	}
}

// two incarnations of one session collapse onto the same path — the loser must
// be reported, not silently overwritten at write time.
func TestCanonicalizeSessionPathsDropsCollision(t *testing.T) {
	pages := []wikiPage{
		{Path: "sessions/s1.md", Title: "first", BodyMarkdown: "b"},
		{Path: "sessions/s1-2.md", Title: "second", BodyMarkdown: "b"},
	}
	got, dropped := canonicalizeSessionPaths(pages, []string{"s1"})
	if len(got) != 1 || got[0].Title != "first" {
		t.Fatalf("got %+v, want only the first page", got)
	}
	if len(dropped) != 1 || !strings.Contains(dropped[0], "sessions/s1.md") {
		t.Errorf("dropped = %v, want a collision reason naming the target", dropped)
	}
}

// end to end: the processor names the page after the digest file, memoria pins
// it back to the session id so run/recall/digest can still find it.
func TestProcessPinsSessionPageToSessionID(t *testing.T) {
	proj, cfgPath, digest := processFixture(t)
	body := "---\nkind: session-digest\nsession_id: s1\n---\n\n@user-prompt 'redesign the day view'\n"
	if err := os.WriteFile(digest, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	stubProcessor(t, `{"pages":[{"path":"sessions/s1-2.md","title":"Day view","tags":[],"body_markdown":"# Day view\n\nwork\n"}]}`, nil)

	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("process = %d: %s", code, buf.String())
	}
	b, err := os.ReadFile(filepath.Join(proj, ".memoria", "proposal.json"))
	if err != nil {
		t.Fatal(err)
	}
	var prop proposal
	if err := json.Unmarshal(b, &prop); err != nil {
		t.Fatal(err)
	}
	if len(prop.Pages) != 1 || prop.Pages[0].Path != "sessions/s1.md" {
		t.Fatalf("pages = %+v, want sessions/s1.md", prop.Pages)
	}
}

// the processor runs for minutes; an agent still live in the project keeps
// appending to a digest already flagged ended. Archiving it then would bury
// events that were never in the prompt.
func TestApplyKeepsDigestThatGrewDuringProcessing(t *testing.T) {
	proj, cfgPath, digest := processFixture(t)
	projName := filepath.Base(proj)
	stubProcessor(t, goodProposalPages, nil)

	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("process = %d: %s", code, buf.String())
	}
	f, err := os.OpenFile(digest, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("@post-tool-use Edit /p/late.go\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	buf.Reset()
	if code := runProcess(proj, cfgPath, []string{"--apply"}, &buf); code != 0 {
		t.Fatalf("apply = %d: %s", code, buf.String())
	}
	if _, err := os.Stat(filepath.Join(proj, "wiki", "concepts", "queue.md")); err != nil {
		t.Fatalf("page not written: %v", err)
	}
	if _, err := os.Stat(digest); err != nil {
		t.Errorf("digest archived despite growing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".memoria", "sessions", "processed", "s1.md")); !os.IsNotExist(err) {
		t.Error("digest moved to processed/ despite growing")
	}
	q, err := loadQueue(queuePath(cfgPath))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range q[projName] {
		if e.Path == digest {
			found = true
		}
	}
	if !found {
		t.Error("queue entry dropped despite the digest growing")
	}
}

// a proposal written before session_sizes existed carries no sizes — it must
// still archive, not stall forever.
func TestApplyArchivesProposalWithoutSizes(t *testing.T) {
	proj, cfgPath, digest := processFixture(t)
	prop := proposal{
		Project:     filepath.Base(proj),
		GeneratedAt: "2026-08-01T00:00:00-03:00",
		Sessions:    []string{digest},
		Pages:       []wikiPage{{Path: "concepts/queue.md", Title: "Queue", BodyMarkdown: "# Queue\n"}},
	}
	b, err := json.Marshal(prop)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "session_sizes") {
		t.Fatal("nil sizes must be omitted from the proposal")
	}
	if err := os.WriteFile(filepath.Join(proj, ".memoria", "proposal.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if code := runProcess(proj, cfgPath, []string{"--apply"}, &buf); code != 0 {
		t.Fatalf("apply = %d: %s", code, buf.String())
	}
	if _, err := os.Stat(filepath.Join(proj, ".memoria", "sessions", "processed", "s1.md")); err != nil {
		t.Errorf("digest not archived: %v", err)
	}
}

// an incarnation's page overwrites the previous one wholesale, so the prompt
// has to say the existing page is the same session's earlier work — the model
// otherwise has to infer it from a continues_from path pointing at a digest
// file, not at the wiki page.
func TestBuildPromptFlagsContinuation(t *testing.T) {
	sid := "c589857b-ebc9-470b-9bc1-40006eb896ae"
	page := "sessions/" + sid + ".md"
	wiki := map[string]string{page: "# Earlier\n\nfirst stretch\n"}
	cont := "---\nsession_id: " + sid + "\ncontinues_from: ../processed/" + sid + ".md\n---\n\n@user-prompt 'more'\n"
	fresh := "---\nsession_id: " + sid + "\n---\n\n@user-prompt 'more'\n"

	t.Run("continuation with an existing page", func(t *testing.T) {
		got := buildPrompt("rules", wiki, map[string]string{"/p/d-2.md": cont})
		if !strings.Contains(got, page) || !strings.Contains(got, "continues session") {
			t.Errorf("prompt does not name the page to extend:\n%s", got)
		}
	})
	t.Run("continuation with no existing page", func(t *testing.T) {
		got := buildPrompt("rules", map[string]string{}, map[string]string{"/p/d-2.md": cont})
		if strings.Contains(got, "continues session") {
			t.Error("flagged a continuation with nothing to extend")
		}
	})
	t.Run("first incarnation", func(t *testing.T) {
		got := buildPrompt("rules", wiki, map[string]string{"/p/d.md": fresh})
		if strings.Contains(got, "continues session") {
			t.Error("flagged a non-continuation")
		}
	})
}
