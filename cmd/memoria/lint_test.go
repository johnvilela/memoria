package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tracked project with two wiki pages that contradict each other
func lintFixture(t *testing.T) (proj, cfgPath string) {
	t.Helper()
	proj = t.TempDir()
	cfgPath = testConfig(t, proj)
	for p, c := range map[string]string{
		"concepts/queue.md":      "# Queue\n\n" + strings.Repeat("c", 400) + "TAILMARKER",
		"decisions/queue-lib.md": "# Queue library\n\nQueue uses mutexes.\n",
	} {
		dst := filepath.Join(proj, "wiki", filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return proj, cfgPath
}

const goodLintFindings = `{"findings":[{"kind":"contradiction","severity":"warning","message":"channels vs mutexes","pages":["concepts/queue.md","decisions/queue-lib.md"]}]}`

const lintFindingLine = `{"kind":"contradiction","severity":"warning","message":"channels vs mutexes","pages":["concepts/queue.md","decisions/queue-lib.md"]}`

func lintReportPath(proj string) string {
	return filepath.Join(proj, ".memoria", "lint.jsonl")
}

// writeLintReport puts a ready report in place, as a lint run would
func writeLintReport(t *testing.T, proj string) string {
	t.Helper()
	p := lintReportPath(proj)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(lintFindingLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLintWritesReport(t *testing.T) {
	proj, cfgPath := lintFixture(t)
	prompt := stubProcessor(t, "```json\n"+goodLintFindings+"\n```", nil)
	var buf bytes.Buffer
	if code := runLint(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("lint = %d: %s", code, buf.String())
	}
	b, err := os.ReadFile(lintReportPath(proj))
	if err != nil {
		t.Fatalf("report not written: %v", err)
	}
	if got := strings.Count(strings.TrimSpace(string(b)), "\n") + 1; got != 1 {
		t.Fatalf("want 1 jsonl line, got %d: %q", got, b)
	}
	if !strings.Contains(string(b), "channels vs mutexes") {
		t.Fatalf("finding missing from report: %q", b)
	}
	for _, w := range []string{"FAITHFULNESS", "concepts/queue.md", "ccc", `"findings"`} {
		if !strings.Contains(*prompt, w) {
			t.Fatalf("prompt missing %q", w)
		}
	}
	if strings.Contains(*prompt, "TAILMARKER") {
		t.Fatal("preview not truncated to ~400 chars")
	}
	if !strings.Contains(buf.String(), "--review") {
		t.Fatalf("summary missing review hint: %s", buf.String())
	}
	st, _ := loadStatus(statusPath(cfgPath))
	if e := st[filepath.Base(proj)]; e.State != "done" || !strings.Contains(e.Detail, "1 finding") {
		t.Fatalf("done status wrong: %+v", e)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfgPath), "lint-prompt.md")); err != nil {
		t.Fatalf("lint prompt not materialized: %v", err)
	}
}

func TestLintOverwritesReport(t *testing.T) {
	proj, cfgPath := lintFixture(t)
	p := lintReportPath(proj)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"kind":"stale","severity":"info","message":"OLD FINDING","pages":["concepts/queue.md"]}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubProcessor(t, goodLintFindings, nil)
	var buf bytes.Buffer
	if code := runLint(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("lint = %d: %s", code, buf.String())
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "OLD FINDING") || !strings.Contains(string(b), "channels vs mutexes") {
		t.Fatalf("report not overwritten: %q", b)
	}
}

func TestLintNoFindings(t *testing.T) {
	proj, cfgPath := lintFixture(t)
	writeLintReport(t, proj) // stale report from an earlier run
	stubProcessor(t, `{"findings":[]}`, nil)
	var buf bytes.Buffer
	if code := runLint(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("lint = %d: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "No conflicts") {
		t.Fatalf("missing message: %s", buf.String())
	}
	if _, err := os.Stat(lintReportPath(proj)); !os.IsNotExist(err) {
		t.Fatal("stale report survived a clean run")
	}
}

func TestLintNothingToLint(t *testing.T) {
	proj := t.TempDir()
	cfgPath := testConfig(t, proj)
	if err := os.MkdirAll(filepath.Join(proj, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubProcessor(t, "", fmt.Errorf("must not be called"))
	var buf bytes.Buffer
	if code := runLint(proj, cfgPath, nil, &buf); code != 0 {
		t.Fatalf("lint = %d: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "Nothing to lint") {
		t.Fatalf("missing message: %s", buf.String())
	}
}

func TestLintRejectsBadFindings(t *testing.T) {
	for _, bad := range []string{
		`{"findings":[{"kind":"typo","severity":"warning","message":"m","pages":["concepts/queue.md"]}]}`,
		`{"findings":[{"kind":"stale","severity":"critical","message":"m","pages":["concepts/queue.md"]}]}`,
		`{"findings":[{"kind":"stale","severity":"warning","message":"","pages":["concepts/queue.md"]}]}`,
		`{"findings":[{"kind":"stale","severity":"warning","message":"m","pages":["invented/nope.md"]}]}`,
		`{"findings":[{"kind":"stale","severity":"warning","message":"m","pages":[]}]}`,
		`not json at all`,
	} {
		proj, cfgPath := lintFixture(t)
		stubProcessor(t, bad, nil)
		var buf bytes.Buffer
		if code := runLint(proj, cfgPath, []string{"--foreground"}, &buf); code != 1 {
			t.Fatalf("bad findings %q accepted: %d %s", bad, code, buf.String())
		}
		if _, err := os.Stat(lintReportPath(proj)); !os.IsNotExist(err) {
			t.Fatalf("report written despite invalid findings: %q", bad)
		}
	}
}

func TestLintDetachesByDefault(t *testing.T) {
	proj, cfgPath := lintFixture(t)
	stubProcessor(t, "", fmt.Errorf("parent must not invoke the processor"))
	spawned := stubSpawn(t, 4242)
	var buf bytes.Buffer
	if code := runLint(proj, cfgPath, nil, &buf); code != 0 {
		t.Fatalf("lint = %d: %s", code, buf.String())
	}
	want := []string{proj, "lint", "--foreground"}
	if len(*spawned) != 3 || (*spawned)[0] != want[0] || (*spawned)[1] != want[1] || (*spawned)[2] != want[2] {
		t.Fatalf("spawn args = %v, want %v", *spawned, want)
	}
	st, _ := loadStatus(statusPath(cfgPath))
	if e := st[filepath.Base(proj)]; e.State != "running" || e.PID != 4242 {
		t.Fatalf("status not running: %+v", e)
	}
}

func TestLintReview(t *testing.T) {
	proj, cfgPath := lintFixture(t)
	writeLintReport(t, proj)
	var buf bytes.Buffer
	if code := runLint(proj, cfgPath, []string{"--review"}, &buf); code != 0 {
		t.Fatalf("review = %d: %s", code, buf.String())
	}
	out := buf.String()
	for _, w := range []string{"contradiction", "warning", "channels vs mutexes", "decisions/queue-lib.md"} {
		if !strings.Contains(out, w) {
			t.Fatalf("review missing %q: %s", w, out)
		}
	}
}

func TestLintReviewWithoutReport(t *testing.T) {
	proj, cfgPath := lintFixture(t)
	var buf bytes.Buffer
	if code := runLint(proj, cfgPath, []string{"--review"}, &buf); code != 1 {
		t.Fatalf("review without report = %d, want 1", code)
	}
}

func TestLintApply(t *testing.T) {
	proj, cfgPath := lintFixture(t)
	report := writeLintReport(t, proj)
	fix := `{"pages":[
		{"action":"update","path":"concepts/queue.md","title":"Queue","content":"# Queue\n\nresolved: channels\n"},
		{"action":"delete","path":"decisions/queue-lib.md"}
	]}`
	prompt := stubProcessor(t, fix, nil)
	var buf bytes.Buffer
	if code := runLint(proj, cfgPath, []string{"--apply"}, &buf); code != 0 {
		t.Fatalf("apply = %d: %s", code, buf.String())
	}
	for _, w := range []string{"TAILMARKER", "Queue uses mutexes", "channels vs mutexes"} {
		if !strings.Contains(*prompt, w) {
			t.Fatalf("apply prompt missing %q", w)
		}
	}
	b, err := os.ReadFile(filepath.Join(proj, "wiki", "concepts", "queue.md"))
	if err != nil || !strings.Contains(string(b), "resolved: channels") {
		t.Fatalf("page not updated: %v %q", err, b)
	}
	if _, err := os.Stat(filepath.Join(proj, "wiki", "decisions", "queue-lib.md")); !os.IsNotExist(err) {
		t.Fatal("superseded page not deleted")
	}
	if _, err := os.Stat(report); !os.IsNotExist(err) {
		t.Fatal("report not consumed after apply")
	}
}

func TestLintApplyRejectsBadFix(t *testing.T) {
	for _, bad := range []string{
		`{"pages":[{"action":"create","path":"../evil.md","title":"x","content":"y"}]}`,
		`{"pages":[{"action":"update","path":"concepts/queue.md","title":"","content":""}]}`,
		`{"pages":[{"action":"delete","path":"concepts/unrelated.md"}]}`,
		`{"pages":[]}`,
	} {
		proj, cfgPath := lintFixture(t)
		report := writeLintReport(t, proj)
		stubProcessor(t, bad, nil)
		var buf bytes.Buffer
		if code := runLint(proj, cfgPath, []string{"--apply"}, &buf); code != 1 {
			t.Fatalf("bad fix %q accepted: %d %s", bad, code, buf.String())
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(proj), "evil.md")); !os.IsNotExist(err) {
			t.Fatal("escaped the wiki root")
		}
		if _, err := os.Stat(report); err != nil {
			t.Fatalf("report consumed despite failed apply: %v", err)
		}
	}
}

func TestLintApplyWithoutReport(t *testing.T) {
	proj, cfgPath := lintFixture(t)
	var buf bytes.Buffer
	if code := runLint(proj, cfgPath, []string{"--apply"}, &buf); code != 1 {
		t.Fatalf("apply without report = %d, want 1", code)
	}
}

func TestLintDeny(t *testing.T) {
	proj, cfgPath := lintFixture(t)
	writeLintReport(t, proj)
	var buf bytes.Buffer
	if code := runLint(proj, cfgPath, []string{"--deny", "pages cover different queues"}, &buf); code != 0 {
		t.Fatalf("deny = %d: %s", code, buf.String())
	}
	if _, err := os.Stat(lintReportPath(proj)); !os.IsNotExist(err) {
		t.Fatal("report not consumed after deny")
	}
	b, err := os.ReadFile(filepath.Join(proj, ".memoria", "lint-denied.jsonl"))
	if err != nil {
		t.Fatalf("denied file not written: %v", err)
	}
	for _, w := range []string{"channels vs mutexes", "pages cover different queues"} {
		if !strings.Contains(string(b), w) {
			t.Fatalf("denied entry missing %q: %q", w, b)
		}
	}

	// a later lint run must show the denial to the processor
	prompt := stubProcessor(t, `{"findings":[]}`, nil)
	if code := runLint(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("lint after deny = %d: %s", code, buf.String())
	}
	if !strings.Contains(*prompt, "pages cover different queues") {
		t.Fatal("denied findings not fed back into the lint prompt")
	}
}

func TestLintDenyAppends(t *testing.T) {
	proj, cfgPath := lintFixture(t)
	var buf bytes.Buffer
	writeLintReport(t, proj)
	if code := runLint(proj, cfgPath, []string{"--deny", "first reason"}, &buf); code != 0 {
		t.Fatalf("deny = %d: %s", code, buf.String())
	}
	writeLintReport(t, proj)
	if code := runLint(proj, cfgPath, []string{"--deny", "second reason"}, &buf); code != 0 {
		t.Fatalf("second deny = %d: %s", code, buf.String())
	}
	b, _ := os.ReadFile(filepath.Join(proj, ".memoria", "lint-denied.jsonl"))
	if !strings.Contains(string(b), "first reason") || !strings.Contains(string(b), "second reason") {
		t.Fatalf("denials not appended: %q", b)
	}
}

func TestLintDenyErrors(t *testing.T) {
	proj, cfgPath := lintFixture(t)
	var buf bytes.Buffer
	if code := runLint(proj, cfgPath, []string{"--deny", "no report yet"}, &buf); code != 1 {
		t.Fatalf("deny without report = %d, want 1", code)
	}
	writeLintReport(t, proj)
	buf.Reset()
	if code := runLint(proj, cfgPath, []string{"--deny", ""}, &buf); code != 1 {
		t.Fatalf("deny without reason = %d, want 1", code)
	}
	if _, err := os.Stat(lintReportPath(proj)); err != nil {
		t.Fatal("report consumed despite empty reason")
	}
}

func TestLintPromptEditable(t *testing.T) {
	proj, cfgPath := lintFixture(t)
	stubProcessor(t, goodLintFindings, nil)
	var buf bytes.Buffer
	if code := runLint(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("lint = %d: %s", code, buf.String())
	}
	pp := filepath.Join(filepath.Dir(cfgPath), "lint-prompt.md")
	if err := os.WriteFile(pp, []byte("CUSTOM LINT RULES\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt := stubProcessor(t, goodLintFindings, nil)
	if code := runLint(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("second lint = %d: %s", code, buf.String())
	}
	if !strings.Contains(*prompt, "CUSTOM LINT RULES") || strings.Contains(*prompt, "FAITHFULNESS") {
		t.Fatal("user-edited lint prompt not respected")
	}
}
