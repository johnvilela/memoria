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

const goodSeedPages = `{"pages":[
	{"path":"index.md","title":"Home","body_markdown":"# Home\n"},
	{"path":"concepts/engine.md","title":"Engine","body_markdown":"# Engine\n","tags":["engine","rocket"]}
],"rationale":"seeded from history"}`

// seedConfig writes a config with a processor and returns its path
func seedConfig(t *testing.T, cfg config) string {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "memoria", "config.yaml")
	if err := saveConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

// fatalProcessor fails the test if the processor is ever invoked
func fatalProcessor(t *testing.T) {
	t.Helper()
	orig := invokeProcessor
	invokeProcessor = func(cfg config, dir, prompt string) (string, error) {
		t.Error("processor must not be invoked")
		return "", fmt.Errorf("must not be called")
	}
	t.Cleanup(func() { invokeProcessor = orig })
}

func TestHasCommits(t *testing.T) {
	if hasCommits(t.TempDir()) {
		t.Fatal("no repo reported commits")
	}
	if hasCommits(gitDir(t, false)) {
		t.Fatal("empty repo reported commits")
	}
	if !hasCommits(gitDir(t, true)) {
		t.Fatal("repo with commit reported none")
	}
}

func TestSeedWiki(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "memoria", "config.yaml")
	dir := gitDir(t, true)
	wikiRoot := filepath.Join(dir, "wiki")
	prompt := stubProcessor(t, goodSeedPages, nil)

	var buf bytes.Buffer
	rationale, err := seedWiki(config{Processor: "claude-code"}, dir, wikiRoot, cfgPath, &buf)
	if err != nil {
		t.Fatalf("seedWiki: %v", err)
	}
	if rationale != "seeded from history" {
		t.Fatalf("rationale = %q", rationale)
	}
	b, err := os.ReadFile(filepath.Join(wikiRoot, "index.md"))
	if err != nil || string(b) != "# Home\n" {
		t.Fatalf("index.md = %q, %v (want no frontmatter)", b, err)
	}
	b, err = os.ReadFile(filepath.Join(wikiRoot, "concepts", "engine.md"))
	if err != nil || string(b) != "---\ntags: [engine, rocket]\n---\n\n# Engine\n" {
		t.Fatalf("engine.md = %q, %v (want tags frontmatter)", b, err)
	}
	for _, want := range []string{"rocket engine", "main.go", "Required JSON shape"} {
		if !strings.Contains(*prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
	if strings.Contains(*prompt, "OUTPUT FORMAT") {
		t.Fatal("prompt has the Go-appended contract; seed-prompt.md carries its own")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfgPath), "seed-prompt.md")); !os.IsNotExist(err) {
		t.Fatalf("embedded default should not be written to disk: %v", err)
	}
	if !strings.Contains(buf.String(), "index.md") {
		t.Fatalf("output should list written pages: %q", buf.String())
	}
}

func TestSeedWikiRejectsInvalid(t *testing.T) {
	for _, bad := range []string{
		`{"pages":[{"path":"../evil.md","title":"x","body_markdown":"y"}]}`,
		`{"pages":[{"path":"concepts/x.md","title":"","body_markdown":"y"}]}`,
		`{"pages":[{"path":"concepts/x.md","title":"x","body_markdown":""}]}`,
		`{"pages":[]}`,
	} {
		cfgPath := filepath.Join(t.TempDir(), "memoria", "config.yaml")
		dir := gitDir(t, true)
		wikiRoot := filepath.Join(dir, "wiki")
		stubProcessor(t, bad, nil)
		var buf bytes.Buffer
		if _, err := seedWiki(config{Processor: "claude-code"}, dir, wikiRoot, cfgPath, &buf); err == nil {
			t.Fatalf("invalid pages accepted: %q", bad)
		}
		if _, err := os.Stat(wikiRoot); !os.IsNotExist(err) {
			t.Fatalf("wiki written despite invalid pages: %q", bad)
		}
	}
}

func TestSeedPromptReadsHEADNotWorktree(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "memoria", "config.yaml")
	dir := gitDir(t, true)
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("committed readme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "docs: add readme")
	// dirty working tree must never reach the prompt
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty readme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt := stubProcessor(t, goodSeedPages, nil)
	var buf bytes.Buffer
	if _, err := seedWiki(config{Processor: "claude-code"}, dir, filepath.Join(dir, "wiki"), cfgPath, &buf); err != nil {
		t.Fatalf("seedWiki: %v", err)
	}
	if !strings.Contains(*prompt, "committed readme") {
		t.Fatal("committed README missing from prompt")
	}
	if strings.Contains(*prompt, "dirty readme") {
		t.Fatal("working-tree README leaked into prompt")
	}
}

func TestBootstrapNonTTYNeverSeeds(t *testing.T) {
	dir := gitDir(t, true)
	cfgPath := seedConfig(t, config{Processor: "claude-code"})
	fatalProcessor(t)
	var out strings.Builder
	if code := runBootstrap(dir, cfgPath, "", false, false, &out); code != 0 {
		t.Fatalf("bootstrap = %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Registered") {
		t.Fatalf("output %q missing Registered", out.String())
	}
}

func TestBootstrapBackgroundDetaches(t *testing.T) {
	dir := gitDir(t, true)
	cfgPath := seedConfig(t, config{Processor: "claude-code"})
	fatalProcessor(t)
	spawned := stubSpawn(t, 4242)
	var out strings.Builder
	if code := runBootstrap(dir, cfgPath, "", true, false, &out); code != 0 {
		t.Fatalf("bootstrap --background = %d: %s", code, out.String())
	}
	want := []string{dir, "bootstrap", "--seed-foreground"}
	if len(*spawned) != 3 || (*spawned)[0] != want[0] || (*spawned)[1] != want[1] || (*spawned)[2] != want[2] {
		t.Fatalf("spawn args = %v, want %v", *spawned, want)
	}
	for _, w := range []string{"4242", "background", "memoria status", "minutes"} {
		if !strings.Contains(out.String(), w) {
			t.Fatalf("detach message missing %q: %s", w, out.String())
		}
	}
	st, _ := loadStatus(statusPath(cfgPath))
	if e := st[filepath.Base(dir)]; e.State != "running" || e.PID != 4242 {
		t.Fatalf("status not running: %+v", e)
	}
	cfg, _ := loadConfig(cfgPath)
	if len(cfg.Projects) != 1 || cfg.Projects[0].Path != dir {
		t.Fatalf("project not registered before detach: %+v", cfg.Projects)
	}
}

func TestBootstrapBackgroundRefusesConcurrentRun(t *testing.T) {
	dir := gitDir(t, true)
	cfgPath := seedConfig(t, config{Processor: "claude-code"})
	if err := statusSet(statusPath(cfgPath), filepath.Base(dir), "running", os.Getpid(), ""); err != nil {
		t.Fatal(err)
	}
	spawned := stubSpawn(t, 4242)
	var out strings.Builder
	if code := runBootstrap(dir, cfgPath, "", true, false, &out); code != 1 {
		t.Fatalf("concurrent run allowed: %d %s", code, out.String())
	}
	if len(*spawned) != 0 {
		t.Fatalf("spawned despite running: %v", *spawned)
	}
	if !strings.Contains(out.String(), "already running") {
		t.Fatalf("message missing: %s", out.String())
	}
}

func TestBootstrapBackgroundSkipsPreconditions(t *testing.T) {
	// no commits
	dir := gitDir(t, false)
	cfgPath := seedConfig(t, config{Processor: "claude-code"})
	spawned := stubSpawn(t, 4242)
	var out strings.Builder
	if code := runBootstrap(dir, cfgPath, "", true, false, &out); code != 0 {
		t.Fatalf("bootstrap = %d: %s", code, out.String())
	}
	if len(*spawned) != 0 || !strings.Contains(out.String(), "Registered") {
		t.Fatalf("seeded without commits: %v %s", *spawned, out.String())
	}

	// no processor
	dir2 := gitDir(t, true)
	cfgPath2 := filepath.Join(t.TempDir(), "config.yaml")
	*spawned = nil
	out.Reset()
	if code := runBootstrap(dir2, cfgPath2, "", true, false, &out); code != 0 {
		t.Fatalf("bootstrap = %d: %s", code, out.String())
	}
	if len(*spawned) != 0 || !strings.Contains(out.String(), "Registered") {
		t.Fatalf("seeded without processor: %v %s", *spawned, out.String())
	}
}

func TestBootstrapSeedForegroundChild(t *testing.T) {
	dir := gitDir(t, true)
	cfgPath := seedConfig(t, config{Processor: "claude-code",
		Projects: []project{{Name: filepath.Base(dir), Path: dir}}})
	stubProcessor(t, goodSeedPages, nil)
	var out strings.Builder
	if code := runBootstrap(dir, cfgPath, "", false, true, &out); code != 0 {
		t.Fatalf("seed child = %d: %s", code, out.String())
	}
	if strings.Contains(out.String(), "egistered") {
		t.Fatalf("child re-ran registration: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "wiki", "concepts", "engine.md")); err != nil {
		t.Fatalf("wiki not seeded: %v", err)
	}
	st, _ := loadStatus(statusPath(cfgPath))
	if e := st[filepath.Base(dir)]; e.State != "done" || !strings.Contains(e.Detail, "seeded from history") {
		t.Fatalf("done status wrong: %+v", e)
	}

	dir2 := gitDir(t, true)
	cfgPath2 := seedConfig(t, config{Processor: "claude-code",
		Projects: []project{{Name: filepath.Base(dir2), Path: dir2}}})
	stubProcessor(t, "", fmt.Errorf("claude exploded"))
	out.Reset()
	if code := runBootstrap(dir2, cfgPath2, "", false, true, &out); code != 1 {
		t.Fatalf("processor error swallowed: %d %s", code, out.String())
	}
	st, _ = loadStatus(statusPath(cfgPath2))
	if e := st[filepath.Base(dir2)]; e.State != "error" || !strings.Contains(e.Detail, "claude exploded") {
		t.Fatalf("error status wrong: %+v", e)
	}
}

func TestBootstrapRerunEmptyWikiSeeds(t *testing.T) {
	dir := gitDir(t, true)
	cfgPath := filepath.Join(t.TempDir(), "memoria", "config.yaml")
	var out strings.Builder
	if code := runBootstrap(dir, cfgPath, "", false, false, &out); code != 0 {
		t.Fatalf("register = %d: %s", code, out.String())
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Processor = "claude-code"
	if err := saveConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	// wiki only has .gitkeep — re-run may still seed
	spawned := stubSpawn(t, 4242)
	out.Reset()
	if code := runBootstrap(dir, cfgPath, "", true, false, &out); code != 0 {
		t.Fatalf("re-run = %d: %s", code, out.String())
	}
	if len(*spawned) != 3 || (*spawned)[2] != "--seed-foreground" {
		t.Fatalf("empty-wiki re-run did not seed: %v", *spawned)
	}
	if !strings.Contains(out.String(), "already registered") {
		t.Fatalf("missing already registered: %s", out.String())
	}

	// wiki with a page — plain early exit
	if err := os.WriteFile(filepath.Join(dir, "wiki", "index.md"), []byte("# Home\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	*spawned = nil
	out.Reset()
	if code := runBootstrap(dir, cfgPath, "", true, false, &out); code != 0 {
		t.Fatalf("re-run with pages = %d: %s", code, out.String())
	}
	if len(*spawned) != 0 {
		t.Fatalf("seeded a non-empty wiki: %v", *spawned)
	}
	if !strings.Contains(out.String(), "already registered") {
		t.Fatalf("missing already registered: %s", out.String())
	}
}
