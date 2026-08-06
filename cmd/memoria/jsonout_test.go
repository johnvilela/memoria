package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// a page body shaped like the real thing that broke kakei: quoted tokens,
// backticks, newlines. No quote in it is followed by , : } ] — every one of
// them is inside repairJSON's reach, which is what makes the slip generator
// below exhaustive.
const quoteHeavyBody = "# Commit rules\n\nUse \"feat\" for features and \"fix\" for fixes.\nRun `go test` before every commit.\nNever pass \"--no-verify\" or \"--amend\".\n"

type testBatch struct {
	Pages []wikiPage `json:"pages"`
}

func TestRepairJSONIsIdentityOnValidJSON(t *testing.T) {
	for _, s := range []string{
		`{"a":1}`,
		`{"pages":[]}`,
		goodProposalPages,
		goodLintFindings,
		`{"body":"he said \"hi\" and left"}`,
		`{"body":"a, b: c} d] e"}`,
		`{"body":"acentuação — ✓ ünïcode"}`,
		`{"a":{"b":["c","d"]},"e":null,"f":true}`,
		`{"body":"trailing quote \""}`,
	} {
		if got := repairJSON(s); got != s {
			t.Errorf("repairJSON altered valid JSON:\n in: %s\nout: %s", s, got)
		}
	}
}

// the reported kakei failure: a dropped backslash before "feat
func TestRepairJSONRecoversUnescapedQuote(t *testing.T) {
	raw := `{"pages":[{"path":"rules/commits.md","title":"Commits","tags":["git"],"body_markdown":"Use "feat" for features."}]}`
	var b testBatch
	if err := json.Unmarshal([]byte(repairJSON(raw)), &b); err != nil {
		t.Fatalf("repair did not recover: %v", err)
	}
	if len(b.Pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(b.Pages))
	}
	if want := `Use "feat" for features.`; b.Pages[0].BodyMarkdown != want {
		t.Fatalf("body = %q, want %q", b.Pages[0].BodyMarkdown, want)
	}
	if b.Pages[0].Path != "rules/commits.md" {
		t.Fatalf("path mangled: %q", b.Pages[0].Path)
	}
}

// The regression guard: marshal a body correctly, then drop one backslash at a
// time and assert every single variant round-trips. Covers the whole class of
// model slip, not one sample — and needs no AI to generate.
func TestRepairJSONRecoversEverySingleDroppedBackslash(t *testing.T) {
	good, err := json.Marshal(testBatch{Pages: []wikiPage{{
		Path: "rules/commits.md", Title: "Commits",
		Tags: []string{"git"}, BodyMarkdown: quoteHeavyBody,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(good)
	// every \" in the doc belongs to the body — keys and values use bare quotes
	slips := 0
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '\\' || s[i+1] != '"' {
			continue
		}
		slips++
		broken := s[:i] + s[i+1:]
		if json.Unmarshal([]byte(broken), new(testBatch)) == nil {
			t.Fatalf("variant at %d is not actually broken", i)
		}
		var b testBatch
		if err := json.Unmarshal([]byte(repairJSON(broken)), &b); err != nil {
			t.Fatalf("dropped backslash at %d unrecovered: %v\n%s", i, err, broken)
		}
		if len(b.Pages) != 1 || b.Pages[0].BodyMarkdown != quoteHeavyBody {
			t.Fatalf("dropped backslash at %d changed the body:\n%q", i, b.Pages[0].BodyMarkdown)
		}
	}
	if slips < 6 {
		t.Fatalf("only %d escaped quotes in the fixture — test is not exercising much", slips)
	}
}

func TestRepairJSONRecoversRawControlChars(t *testing.T) {
	raw := "{\"pages\":[{\"path\":\"a.md\",\"body_markdown\":\"line one\nline\ttwo\"}]}"
	var b testBatch
	if err := json.Unmarshal([]byte(repairJSON(raw)), &b); err != nil {
		t.Fatalf("repair did not recover: %v", err)
	}
	if want := "line one\nline\ttwo"; b.Pages[0].BodyMarkdown != want {
		t.Fatalf("body = %q, want %q", b.Pages[0].BodyMarkdown, want)
	}
}

// ponytail: documented ceiling — a stray quote sitting right before a comma is
// indistinguishable from a terminator. It must fail loudly, never silently
// truncate the body.
func TestRepairJSONKnownCeiling(t *testing.T) {
	raw := `{"pages":[{"path":"a.md","body_markdown":"say "hi", ok"}]}`
	var b testBatch
	if err := json.Unmarshal([]byte(repairJSON(raw)), &b); err == nil {
		t.Fatalf("ambiguous quote silently accepted: %+v", b)
	}
}

func TestParseProcessorJSONCleanInput(t *testing.T) {
	var b testBatch
	var out bytes.Buffer
	repaired, err := parseProcessorJSON("```json\n"+goodProposalPages+"\n```", t.TempDir(), "process", &out, &b)
	if err != nil {
		t.Fatalf("clean input rejected: %v", err)
	}
	if repaired {
		t.Error("clean input reported as repaired")
	}
	if out.Len() != 0 {
		t.Errorf("clean input warned: %s", out.String())
	}
	if len(b.Pages) != 2 {
		t.Fatalf("pages = %d, want 2", len(b.Pages))
	}
}

func TestParseProcessorJSONTolueratesChatter(t *testing.T) {
	var b testBatch
	var out bytes.Buffer
	if _, err := parseProcessorJSON("here you go:\n"+goodProposalPages+"\ndone", t.TempDir(), "process", &out, &b); err != nil {
		t.Fatalf("chatter rejected: %v", err)
	}
	if len(b.Pages) != 2 {
		t.Fatalf("pages = %d, want 2", len(b.Pages))
	}
}

func TestParseProcessorJSONFlagsRepair(t *testing.T) {
	proj := t.TempDir()
	var b testBatch
	var out bytes.Buffer
	raw := `{"pages":[{"path":"a.md","body_markdown":"Use "feat" here."}]}`
	repaired, err := parseProcessorJSON(raw, proj, "process", &out, &b)
	if err != nil {
		t.Fatalf("repairable input rejected: %v", err)
	}
	if !repaired {
		t.Error("repair not reported")
	}
	if !strings.Contains(out.String(), "repaired") {
		t.Errorf("no warning printed: %q", out.String())
	}
	// the raw stays on disk so a bad repair is auditable
	dump, err := os.ReadFile(filepath.Join(proj, ".memoria", "last-processor-error.txt"))
	if err != nil {
		t.Fatalf("raw not kept after repair: %v", err)
	}
	if !strings.Contains(string(dump), raw) {
		t.Errorf("dump missing the raw output:\n%s", dump)
	}
}

func TestParseProcessorJSONDumpsUnrecoverableOutput(t *testing.T) {
	for name, raw := range map[string]string{
		"no braces":  "the model just talked instead",
		"ambiguous":  `{"pages":[{"path":"a.md","body_markdown":"say "hi", ok"}]}`,
		"truncated":  `{"pages":[{"path":"a.md","body_markdown":"cut off`,
		"wrong type": `{"pages":"not an array"}`,
	} {
		t.Run(name, func(t *testing.T) {
			proj := t.TempDir()
			var b testBatch
			var out bytes.Buffer
			repaired, err := parseProcessorJSON(raw, proj, "process", &out, &b)
			if err == nil {
				t.Fatalf("bad output accepted: %+v", b)
			}
			if repaired {
				t.Error("failed parse reported as repaired")
			}
			dumpPath := filepath.Join(proj, ".memoria", "last-processor-error.txt")
			if !strings.Contains(err.Error(), dumpPath) {
				t.Errorf("error does not name the dump: %v", err)
			}
			dump, rerr := os.ReadFile(dumpPath)
			if rerr != nil {
				t.Fatalf("no dump written: %v", rerr)
			}
			if !strings.Contains(string(dump), raw) {
				t.Errorf("dump missing the raw output:\n%s", dump)
			}
		})
	}
}

// the offset json.SyntaxError carries is the whole point of the dump — without
// it "invalid character 'f'" says nothing about where.
func TestParseProcessorJSONDumpShowsOffsetContext(t *testing.T) {
	proj := t.TempDir()
	var b testBatch
	var out bytes.Buffer
	raw := `{"pages":[{"path":"a.md","body_markdown":"say "hi", ok"}]}`
	if _, err := parseProcessorJSON(raw, proj, "process", &out, &b); err == nil {
		t.Fatal("bad output accepted")
	}
	dump, err := os.ReadFile(filepath.Join(proj, ".memoria", "last-processor-error.txt"))
	if err != nil {
		t.Fatal(err)
	}
	head := string(dump)
	if i := strings.Index(head, "\n\n"); i > 0 {
		head = head[:i]
	}
	for _, want := range []string{"process", "offset", "ok"} {
		if !strings.Contains(head, want) {
			t.Errorf("dump header missing %q:\n%s", want, head)
		}
	}
}

func TestParseProcessorJSONWithoutProjectDir(t *testing.T) {
	var b testBatch
	var out bytes.Buffer
	if _, err := parseProcessorJSON("nope", "", "process", &out, &b); err == nil {
		t.Fatal("bad output accepted")
	}
}

// --- the five call sites, end to end ---

const repairableBatch = `{"pages":[
	{"path":"index.md","title":"Index","tags":[],"body_markdown":"# Index\n\nSee "concepts/queue".\n"},
	{"path":"concepts/queue.md","title":"Queue","tags":["queue"],"body_markdown":"Run "go test" first.\n"}
]}`

func TestProcessSurvivesUnescapedQuote(t *testing.T) {
	proj, cfgPath, _ := processFixture(t)
	stubProcessor(t, repairableBatch, nil)
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
	if len(p.Pages) != 2 {
		t.Fatalf("pages = %d, want 2", len(p.Pages))
	}
	if want := "Run \"go test\" first.\n"; p.Pages[1].BodyMarkdown != want {
		t.Fatalf("body = %q, want %q", p.Pages[1].BodyMarkdown, want)
	}
	st, _ := loadStatus(statusPath(cfgPath))
	if !strings.Contains(st[filepath.Base(proj)].Detail, "repaired") {
		t.Errorf("status detail hides the repair: %q", st[filepath.Base(proj)].Detail)
	}
}

func TestDigestSurvivesUnescapedQuote(t *testing.T) {
	proj, cfgPath := digestFixture(t)
	stubProcessor(t, `{"title":"Queue","body_markdown":"# Queue\n\nRan "go test".\n","tags":["queue"]}`, nil)
	var buf bytes.Buffer
	if code := runDigest(proj, cfgPath, []string{"s1", "--foreground"}, &buf); code != 0 {
		t.Fatalf("digest = %d: %s", code, buf.String())
	}
	b, err := os.ReadFile(filepath.Join(proj, "wiki", "sessions", "s1.md"))
	if err != nil {
		t.Fatalf("page not written: %v", err)
	}
	if !strings.Contains(string(b), `Ran "go test".`) {
		t.Fatalf("body mangled: %q", b)
	}
	st, _ := loadStatus(statusPath(cfgPath))
	if !strings.Contains(st[filepath.Base(proj)].Detail, "repaired") {
		t.Errorf("status detail hides the repair: %q", st[filepath.Base(proj)].Detail)
	}
}

func TestLintSurvivesUnescapedQuote(t *testing.T) {
	proj, cfgPath := lintFixture(t)
	bad := `{"findings":[{"kind":"contradiction","severity":"warning","message":"one says "channels" the other mutexes","pages":["concepts/queue.md","decisions/queue-lib.md"]}]}`
	stubProcessor(t, bad, nil)
	var buf bytes.Buffer
	if code := runLint(proj, cfgPath, []string{"--foreground"}, &buf); code != 0 {
		t.Fatalf("lint = %d: %s", code, buf.String())
	}
	f, err := readFindings(lintReportPath(proj))
	if err != nil {
		t.Fatalf("report not written: %v", err)
	}
	if len(f) != 1 || !strings.Contains(f[0].Message, `"channels"`) {
		t.Fatalf("findings mangled: %+v", f)
	}
	st, _ := loadStatus(statusPath(cfgPath))
	if !strings.Contains(st[filepath.Base(proj)].Detail, "repaired") {
		t.Errorf("status detail hides the repair: %q", st[filepath.Base(proj)].Detail)
	}
}

func TestLintApplySurvivesUnescapedQuote(t *testing.T) {
	proj, cfgPath := lintFixture(t)
	writeLintReport(t, proj)
	stubProcessor(t, `{"pages":[{"action":"update","path":"concepts/queue.md","title":"Queue","content":"# Queue\n\nUses "channels".\n"}]}`, nil)
	var buf bytes.Buffer
	if code := runLint(proj, cfgPath, []string{"--apply"}, &buf); code != 0 {
		t.Fatalf("lint --apply = %d: %s", code, buf.String())
	}
	b, err := os.ReadFile(filepath.Join(proj, "wiki", "concepts", "queue.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `Uses "channels".`) {
		t.Fatalf("content mangled: %q", b)
	}
}

func TestSeedSurvivesUnescapedQuote(t *testing.T) {
	proj := t.TempDir()
	cfgPath := testConfig(t, proj)
	wikiRoot := filepath.Join(proj, "wiki")
	if err := os.MkdirAll(wikiRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	stubProcessor(t, `{"pages":[{"path":"index.md","title":"Index","tags":[],"body_markdown":"Build with "go build".\n"}],"rationale":"seeded"}`, nil)
	var buf bytes.Buffer
	if _, err := seedWiki(config{Processor: "claude-code"}, proj, wikiRoot, cfgPath, &buf); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(wikiRoot, "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `Build with "go build".`) {
		t.Fatalf("body mangled: %q", b)
	}
}
