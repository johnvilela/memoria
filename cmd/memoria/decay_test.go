package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubNow pins the clock to a fixed date, restoring it after the test.
func stubNow(t *testing.T, day string) {
	t.Helper()
	d, err := time.Parse("2006-01-02", day)
	if err != nil {
		t.Fatal(err)
	}
	orig := now
	now = func() time.Time { return d }
	t.Cleanup(func() { now = orig })
}

func writePage(t *testing.T, wikiRoot, rel, content string) string {
	t.Helper()
	p := filepath.Join(wikiRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUpsertFrontLine(t *testing.T) {
	got := upsertFrontLine("---\ntags: [a]\nlastUsed: 2026-01-01\n---\n\nbody\n", "lastUsed", "lastUsed: 2026-08-31")
	if !strings.Contains(got, "lastUsed: 2026-08-31") || strings.Contains(got, "2026-01-01") {
		t.Fatalf("replace = %q", got)
	}
	if !strings.Contains(got, "tags: [a]") || !strings.HasSuffix(got, "body\n") {
		t.Fatalf("replace must keep the rest intact: %q", got)
	}

	got = upsertFrontLine("---\ntags: [a]\n---\n\nbody\n", "lastUsed", "lastUsed: 2026-08-31")
	if !strings.Contains(got, "tags: [a]\nlastUsed: 2026-08-31\n---") {
		t.Fatalf("insert = %q", got)
	}

	got = upsertFrontLine("# T\n\nbody\n", "lastUsed", "lastUsed: 2026-08-31")
	if !strings.HasPrefix(got, "---\nlastUsed: 2026-08-31\n---\n\n# T") {
		t.Fatalf("synthesize = %q", got)
	}

	got = upsertFrontLine("---\ntags: [a]\nno closing fence\n", "lastUsed", "lastUsed: 2026-08-31")
	if !strings.HasPrefix(got, "---\nlastUsed: 2026-08-31\n---\n\n") {
		t.Fatalf("unterminated frontmatter should fall back to a fresh block: %q", got)
	}
}

func TestPageLastUsed(t *testing.T) {
	if got := pageLastUsed("---\ntags: [a]\nlastUsed: 2026-07-01\n---\n\nbody\n"); got != "2026-07-01" {
		t.Fatalf("lastUsed = %q, want 2026-07-01", got)
	}
	if got := pageLastUsed("# T\n\nbody\n"); got != "" {
		t.Fatalf("no frontmatter = %q, want empty", got)
	}
}

func TestTouchLastUsed(t *testing.T) {
	stubNow(t, "2026-08-31")
	wikiRoot := t.TempDir()

	p := writePage(t, wikiRoot, "sessions/s1.md", "---\ntags: [session]\n---\n\n# S\n")
	touchLastUsed(wikiRoot, "sessions/s1.md")
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "lastUsed: 2026-08-31") {
		t.Fatalf("sessions page should be stamped: %q", b)
	}

	c := writePage(t, wikiRoot, "concepts/x.md", "# X\n")
	touchLastUsed(wikiRoot, "concepts/x.md")
	b, _ = os.ReadFile(c)
	if strings.Contains(string(b), "lastUsed") {
		t.Fatalf("non-sessions page must not be stamped: %q", b)
	}

	tr := writePage(t, wikiRoot, "trash/sessions/s2.md", "---\nlastUsed: 2026-01-01\n---\n\n# S2\n")
	touchLastUsed(wikiRoot, "trash/sessions/s2.md")
	b, _ = os.ReadFile(tr)
	if !strings.Contains(string(b), "lastUsed: 2026-01-01") {
		t.Fatalf("trashed page's clock must stay frozen: %q", b)
	}

	touchLastUsed(wikiRoot, "sessions/missing.md") // must not create or panic
	if _, err := os.Stat(filepath.Join(wikiRoot, "sessions", "missing.md")); !os.IsNotExist(err) {
		t.Fatal("touch must not create missing pages")
	}

	// already stamped today: the file must not be rewritten (1 write/day/page)
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(p, past, past); err != nil {
		t.Fatal(err)
	}
	touchLastUsed(wikiRoot, "sessions/s1.md")
	fi, _ := os.Stat(p)
	if !fi.ModTime().Equal(past) {
		t.Fatal("already-today page must not be rewritten")
	}
}

func TestStampSessions(t *testing.T) {
	stubNow(t, "2026-08-31")
	wikiRoot := t.TempDir()

	got := stampSessions(wikiRoot, "sessions/new.md", "# N\n")
	if !strings.Contains(got, "lastUsed: 2026-08-31") {
		t.Fatalf("new sessions page gets today: %q", got)
	}

	writePage(t, wikiRoot, "sessions/old.md", "---\ntags: [x]\nlastUsed: 2026-07-01\n---\n\n# O\n")
	got = stampSessions(wikiRoot, "sessions/old.md", "---\ntags: [x]\n---\n\n# O rewritten\n")
	if !strings.Contains(got, "lastUsed: 2026-07-01") {
		t.Fatalf("rewrite must preserve the existing date: %q", got)
	}

	got = stampSessions(wikiRoot, "sessions/new.md", "---\nlastUsed: 1999-01-01\n---\n\n# smuggled\n")
	if strings.Contains(got, "1999-01-01") || strings.Count(got, "lastUsed:") != 1 {
		t.Fatalf("LLM-authored lastUsed must be overridden, exactly once: %q", got)
	}

	if got = stampSessions(wikiRoot, "concepts/c.md", "# C\n"); got != "# C\n" {
		t.Fatalf("non-sessions pass through untouched: %q", got)
	}
}

func TestWriteWikiPage(t *testing.T) {
	stubNow(t, "2026-08-31")
	wikiRoot := t.TempDir()

	if err := writeWikiPage(wikiRoot, "sessions/s1.md", []string{"session"}, "# S\n"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(wikiRoot, "sessions", "s1.md"))
	if !strings.Contains(string(b), "tags: [session]") || !strings.Contains(string(b), "lastUsed: 2026-08-31") {
		t.Fatalf("sessions page carries tags and lastUsed: %q", b)
	}

	if err := writeWikiPage(wikiRoot, "concepts/deep/c.md", nil, "# C\n"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(wikiRoot, "concepts", "deep", "c.md"))
	if string(b) != "# C\n" {
		t.Fatalf("non-sessions tagless page stays bare: %q", b)
	}
}

func TestDecayDays(t *testing.T) {
	if s, h := decayDays(config{}); s != 15 || h != 30 {
		t.Fatalf("defaults = %d/%d, want 15/30", s, h)
	}
	if s, h := decayDays(config{DecaySoftDays: 2, DecayHardDays: 5}); s != 2 || h != 5 {
		t.Fatalf("configured = %d/%d, want 2/5", s, h)
	}
}

func TestDecaySweep(t *testing.T) {
	stubNow(t, "2026-08-31")
	commits := stubCommitWiki(t)
	wikiRoot := t.TempDir()

	writePage(t, wikiRoot, "sessions/fresh.md", "---\nlastUsed: 2026-08-20\n---\n\n# F\n")
	writePage(t, wikiRoot, "sessions/stale.md", "---\ntags: [x]\nlastUsed: 2026-08-01\n---\n\n# S\n")
	writePage(t, wikiRoot, "sessions/unstamped.md", "# U\n")
	writePage(t, wikiRoot, "trash/sessions/ancient.md", "---\ntags: [deleted]\nlastUsed: 2026-07-01\n---\n\n# A\n")
	writePage(t, wikiRoot, "trash/sessions/recent.md", "---\nlastUsed: 2026-08-25\n---\n\n# R\n")
	writePage(t, wikiRoot, "trash/sessions/unstamped2.md", "# U2\n")
	writePage(t, wikiRoot, "trash/concepts/keep.md", "# K\n")
	writePage(t, wikiRoot, "concepts/live.md", "# L\n")

	var buf bytes.Buffer
	decaySweep(config{WikiAutoCommit: true}, wikiRoot, &buf)

	read := func(rel string) string {
		b, _ := os.ReadFile(filepath.Join(wikiRoot, filepath.FromSlash(rel)))
		return string(b)
	}
	if !strings.Contains(read("sessions/fresh.md"), "2026-08-20") {
		t.Fatal("fresh page must survive untouched")
	}
	if read("sessions/stale.md") != "" {
		t.Fatal("stale page must leave sessions/")
	}
	trashed := read("trash/sessions/stale.md")
	if !strings.Contains(trashed, "deleted") || !strings.Contains(trashed, "lastUsed: 2026-08-01") {
		t.Fatalf("stale page lands in trash with deleted tag and frozen date: %q", trashed)
	}
	if !strings.Contains(read("sessions/unstamped.md"), "lastUsed: 2026-08-31") {
		t.Fatal("unstamped page is adopted, not deleted")
	}
	if read("trash/sessions/ancient.md") != "" {
		t.Fatal("ancient trash must be purged")
	}
	if !strings.Contains(read("trash/sessions/recent.md"), "2026-08-25") {
		t.Fatal("recent trash must survive")
	}
	if !strings.Contains(read("trash/sessions/unstamped2.md"), "lastUsed: 2026-08-31") {
		t.Fatal("unstamped trash is adopted, not purged")
	}
	if strings.Contains(read("trash/concepts/keep.md"), "lastUsed") {
		t.Fatal("non-sessions trash is out of scope")
	}
	if strings.Contains(read("concepts/live.md"), "lastUsed") {
		t.Fatal("non-sessions pages are out of scope")
	}
	if len(*commits) != 1 {
		t.Fatalf("one commit for the whole sweep, got %d", len(*commits))
	}

	// second run: nothing aged past a threshold since, no commit
	buf.Reset()
	decaySweep(config{WikiAutoCommit: true}, wikiRoot, &buf)
	if len(*commits) != 1 {
		t.Fatalf("idle sweep must not commit, got %d commits", len(*commits))
	}
}

// --- deterministic stamp across every write and delivery site ---

func TestApplyStampsAndPreservesLastUsed(t *testing.T) {
	stubNow(t, "2026-08-31")
	stubCommitWiki(t)
	proj, cfgPath, _ := processFixture(t)
	wikiRoot := filepath.Join(proj, "wiki")
	writePage(t, wikiRoot, "sessions/s1.md", "---\ntags: [x]\nlastUsed: 2026-07-01\n---\n\nold\n")
	prop := `{"project":"` + filepath.Base(proj) + `","pages":[
		{"path":"sessions/s1.md","title":"S","tags":["session"],"body_markdown":"# S\n\nnew\n"},
		{"path":"sessions/s2.md","title":"S2","tags":[],"body_markdown":"# S2\n"},
		{"path":"concepts/c.md","title":"C","tags":[],"body_markdown":"# C\n"}]}`
	proposalPath := filepath.Join(proj, ".memoria", "proposal.json")
	if err := os.WriteFile(proposalPath, []byte(prop), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if code := applyProposal(config{}, proj, wikiRoot, proposalPath, queuePath(cfgPath), filepath.Base(proj), &buf); code != 0 {
		t.Fatalf("apply = %d: %s", code, buf.String())
	}
	read := func(rel string) string {
		b, _ := os.ReadFile(filepath.Join(wikiRoot, filepath.FromSlash(rel)))
		return string(b)
	}
	if !strings.Contains(read("sessions/s1.md"), "lastUsed: 2026-07-01") {
		t.Fatalf("rewrite must preserve the date: %q", read("sessions/s1.md"))
	}
	if !strings.Contains(read("sessions/s2.md"), "lastUsed: 2026-08-31") {
		t.Fatalf("new sessions page gets today: %q", read("sessions/s2.md"))
	}
	if strings.Contains(read("concepts/c.md"), "lastUsed") {
		t.Fatalf("non-sessions page stays unstamped: %q", read("concepts/c.md"))
	}
}

func TestMCPWritePagePreservesLastUsed(t *testing.T) {
	stubNow(t, "2026-08-31")
	proj, cfgPath := mcpFixture(t)
	in := mcpWritePageIn{Path: "sessions/s9.md", Title: "S9", BodyMarkdown: "# S9\n", Tags: []string{"x"}}
	if _, err := mcpWritePage(proj, cfgPath, in); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(proj, "wiki", "sessions", "s9.md")
	b, _ := os.ReadFile(page)
	if !strings.Contains(string(b), "lastUsed: 2026-08-31") {
		t.Fatalf("created sessions page gets today: %q", b)
	}
	if err := os.WriteFile(page, []byte("---\ntags: [x]\nlastUsed: 2026-07-01\n---\n\n# S9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mcpWritePage(proj, cfgPath, in); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(page)
	if !strings.Contains(string(b), "lastUsed: 2026-07-01") {
		t.Fatalf("overwrite must preserve the date: %q", b)
	}
}

func TestLintApplyReinjectsLastUsed(t *testing.T) {
	stubNow(t, "2026-08-31")
	stubCommitWiki(t)
	proj, _ := mcpFixture(t)
	wikiRoot := filepath.Join(proj, "wiki")
	writePage(t, wikiRoot, "sessions/s1.md", "---\ntags: [x]\nlastUsed: 2026-07-01\n---\n\n# S stale\n")
	lintPath := lintReportPath(proj)
	if err := os.MkdirAll(filepath.Dir(lintPath), 0o755); err != nil {
		t.Fatal(err)
	}
	finding := `{"kind":"stale","severity":"info","message":"stale session","pages":["sessions/s1.md"]}`
	if err := os.WriteFile(lintPath, []byte(finding+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fix := `{"pages":[{"action":"update","path":"sessions/s1.md","title":"S","content":"---\ntags: [x]\nlastUsed: 1999-01-01\n---\n\n# S fixed\n"}]}`
	stubProcessor(t, fix, nil)
	var buf bytes.Buffer
	if code := lintApply(config{}, wikiRoot, lintPath, &buf); code != 0 {
		t.Fatalf("lint apply = %d: %s", code, buf.String())
	}
	b, _ := os.ReadFile(filepath.Join(wikiRoot, "sessions", "s1.md"))
	if !strings.Contains(string(b), "lastUsed: 2026-07-01") || strings.Contains(string(b), "1999-01-01") {
		t.Fatalf("lint fix must not author lastUsed: %q", b)
	}
}

func TestSearchTouchesDeliveredPage(t *testing.T) {
	stubNow(t, "2026-08-31")
	proj, cfgPath := searchFixture(t)
	wikiRoot := filepath.Join(proj, "wiki")
	writePage(t, wikiRoot, "sessions/sess1.md", "# Sess\n\nunique deploy ritual\n")
	stubTTY(t, false)
	var buf bytes.Buffer
	if code := runSearch(proj, cfgPath, []string{"deploy ritual"}, &buf); code != 0 {
		t.Fatalf("search = %d: %s", code, buf.String())
	}
	b, _ := os.ReadFile(filepath.Join(wikiRoot, "sessions", "sess1.md"))
	if !strings.Contains(string(b), "lastUsed: 2026-08-31") {
		t.Fatalf("delivered page must be stamped: %q", b)
	}
}

func TestSearchHeadlessListTouchesNothing(t *testing.T) {
	stubNow(t, "2026-08-31")
	proj, cfgPath := searchFixture(t)
	wikiRoot := filepath.Join(proj, "wiki")
	writePage(t, wikiRoot, "sessions/a.md", "# A\n\nshared needle here\n")
	writePage(t, wikiRoot, "sessions/b.md", "# B\n\nshared needle here\n")
	stubTTY(t, false)
	var buf bytes.Buffer
	if code := runSearch(proj, cfgPath, []string{"shared needle"}, &buf); code != 0 {
		t.Fatalf("search = %d: %s", code, buf.String())
	}
	for _, rel := range []string{"sessions/a.md", "sessions/b.md"} {
		b, _ := os.ReadFile(filepath.Join(wikiRoot, filepath.FromSlash(rel)))
		if strings.Contains(string(b), "lastUsed") {
			t.Fatalf("path-only listing must not stamp %s: %q", rel, b)
		}
	}
}

func TestMCPSearchTouchesInlineOnly(t *testing.T) {
	stubNow(t, "2026-08-31")
	proj, cfgPath := mcpFixture(t)
	wikiRoot := filepath.Join(proj, "wiki")
	writePage(t, wikiRoot, "sessions/a.md", "# A\n\ninlined needle\n")
	writePage(t, wikiRoot, "sessions/b.md", "# B\n\ninlined needle\n")
	if _, err := mcpSearch(proj, cfgPath, "inlined needle", false); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"sessions/a.md", "sessions/b.md"} {
		b, _ := os.ReadFile(filepath.Join(wikiRoot, filepath.FromSlash(rel)))
		if !strings.Contains(string(b), "lastUsed: 2026-08-31") {
			t.Fatalf("inlined page must be stamped %s: %q", rel, b)
		}
	}
	for _, n := range []string{"c", "d", "e", "f"} {
		writePage(t, wikiRoot, "sessions/"+n+".md", "# X\n\nlisted needle\n")
	}
	if _, err := mcpSearch(proj, cfgPath, "listed needle", false); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"c", "d", "e", "f"} {
		b, _ := os.ReadFile(filepath.Join(wikiRoot, "sessions", n+".md"))
		if strings.Contains(string(b), "lastUsed") {
			t.Fatalf("path-only hit must not be stamped: %q", b)
		}
	}
}

func TestMCPRecallTouches(t *testing.T) {
	stubNow(t, "2026-08-31")
	proj, cfgPath := mcpFixture(t)
	if _, err := mcpRecall(proj, cfgPath, "s1"); err != nil {
		t.Fatalf("recall without a wiki page must still work: %v", err)
	}
	wikiRoot := filepath.Join(proj, "wiki")
	writePage(t, wikiRoot, "sessions/s1.md", "# S1\n\nsummary\n")
	if _, err := mcpRecall(proj, cfgPath, "s1"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(wikiRoot, "sessions", "s1.md"))
	if !strings.Contains(string(b), "lastUsed: 2026-08-31") {
		t.Fatalf("recall must stamp the session page: %q", b)
	}
}

func TestMCPDigestDoneTouches(t *testing.T) {
	stubNow(t, "2026-08-31")
	proj, cfgPath := mcpFixture(t)
	wikiRoot := filepath.Join(proj, "wiki")
	writePage(t, wikiRoot, "sessions/s1.md", "---\nlastUsed: 2026-08-20\n---\n\n# S1\n")
	projName := filepath.Base(proj)
	if err := statusSet(statusPath(cfgPath), projName, "done", 0, "session page written: sessions/s1.md"); err != nil {
		t.Fatal(err)
	}
	res, err := mcpDigest(proj, cfgPath, "s1")
	if err != nil || res.State != "done" {
		t.Fatalf("digest poll = %+v, %v", res, err)
	}
	b, _ := os.ReadFile(filepath.Join(wikiRoot, "sessions", "s1.md"))
	if !strings.Contains(string(b), "lastUsed: 2026-08-31") {
		t.Fatalf("delivered digest page must be re-stamped: %q", b)
	}
}

func TestRunResumeTouchesSessionPage(t *testing.T) {
	stubNow(t, "2026-08-31")
	stubPathBin(t, "claude")
	stubTTY(t, false)

	// handoff branch: digest has no client, so claude can't natively resume
	proj, cfgPath := runFixture(t)
	writePage(t, filepath.Join(proj, "wiki"), "sessions/bbb-222.md", "# B\n\nsummary\n")
	stubAgent(t, 0)
	var buf bytes.Buffer
	if code := runRun(proj, cfgPath, []string{"claude", "--session", "bbb"}, &buf); code != 0 {
		t.Fatalf("run handoff = %d: %s", code, buf.String())
	}
	b, _ := os.ReadFile(filepath.Join(proj, "wiki", "sessions", "bbb-222.md"))
	if !strings.Contains(string(b), "lastUsed: 2026-08-31") {
		t.Fatalf("handoff resume must stamp the page: %q", b)
	}

	// native branch: same harness, page never read for the resume itself
	proj2, cfgPath2 := runFixture(t)
	writeRunDigest(t, proj2, "pending", "bbb-222.md", "claude-code")
	writePage(t, filepath.Join(proj2, "wiki"), "sessions/bbb-222.md", "# B\n\nsummary\n")
	call := stubAgent(t, 0)
	buf.Reset()
	if code := runRun(proj2, cfgPath2, []string{"claude", "--session", "bbb"}, &buf); code != 0 {
		t.Fatalf("run native = %d: %s", code, buf.String())
	}
	if len(call.args) == 0 || call.args[0] != "--resume" {
		t.Fatalf("expected native resume, got args %v", call.args)
	}
	b, _ = os.ReadFile(filepath.Join(proj2, "wiki", "sessions", "bbb-222.md"))
	if !strings.Contains(string(b), "lastUsed: 2026-08-31") {
		t.Fatalf("native resume must stamp the page too: %q", b)
	}
}

// --- sweep wiring into process --all ---

func TestProcessAllSweepsWithoutSessions(t *testing.T) {
	stubNow(t, "2026-08-31")
	stubCommitWiki(t)
	proj := t.TempDir()
	cfgPath := testConfig(t, proj)
	wikiRoot := filepath.Join(proj, "wiki")
	writePage(t, wikiRoot, "sessions/stale.md", "---\nlastUsed: 2026-08-01\n---\n\n# S\n")
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if code := processAll(cfg, cfgPath, false, &buf); code != 0 {
		t.Fatalf("process --all = %d: %s", code, buf.String())
	}
	if _, err := os.Stat(filepath.Join(wikiRoot, "sessions", "stale.md")); !os.IsNotExist(err) {
		t.Fatal("zero-pending project must still be swept")
	}
	if _, err := os.Stat(filepath.Join(wikiRoot, "trash", "sessions", "stale.md")); err != nil {
		t.Fatalf("stale page should land in trash: %v", err)
	}
}

func TestProcessAllSkipsSweepWhenRunning(t *testing.T) {
	stubNow(t, "2026-08-31")
	stubCommitWiki(t)
	proj := t.TempDir()
	cfgPath := testConfig(t, proj)
	wikiRoot := filepath.Join(proj, "wiki")
	writePage(t, wikiRoot, "sessions/stale.md", "---\nlastUsed: 2026-08-01\n---\n\n# S\n")
	if err := statusSet(statusPath(cfgPath), filepath.Base(proj), "running", os.Getpid(), ""); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	processAll(cfg, cfgPath, false, &buf)
	if _, err := os.Stat(filepath.Join(wikiRoot, "sessions", "stale.md")); err != nil {
		t.Fatal("a project with a live job must not be swept")
	}
}

func TestDecaySweepGraceWindow(t *testing.T) {
	// mid-day clock: a page exactly soft_days old must not age (day
	// granularity), and a page this run's soft pass just trashed must
	// survive this run's hard pass — purge waits for a later sweep
	orig := now
	mid, _ := time.Parse(time.RFC3339, "2026-08-31T15:30:00Z")
	now = func() time.Time { return mid }
	t.Cleanup(func() { now = orig })
	stubCommitWiki(t)
	wikiRoot := t.TempDir()
	writePage(t, wikiRoot, "sessions/edge.md", "---\nlastUsed: 2026-08-16\n---\n\n# E\n")
	writePage(t, wikiRoot, "sessions/verystale.md", "---\nlastUsed: 2026-07-01\n---\n\n# V\n")
	var buf bytes.Buffer
	decaySweep(config{}, wikiRoot, &buf)
	if _, err := os.Stat(filepath.Join(wikiRoot, "sessions", "edge.md")); err != nil {
		t.Fatal("a page exactly soft_days old must survive until the day after")
	}
	if _, err := os.Stat(filepath.Join(wikiRoot, "trash", "sessions", "verystale.md")); err != nil {
		t.Fatal("a page trashed by this run's soft pass must survive this run's hard pass")
	}
}
