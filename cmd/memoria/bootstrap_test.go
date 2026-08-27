package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapFreshConfig(t *testing.T) {
	proj := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "memoria", "config.yaml")

	var out strings.Builder
	if code := runBootstrap(proj, cfgPath, "", false, false, &out); code != 0 {
		t.Fatalf("exit = %d, want 0 (out: %s)", code, out.String())
	}
	if !strings.Contains(out.String(), "Registered") {
		t.Fatalf("output %q missing Registered", out.String())
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	want := project{Name: filepath.Base(proj), Path: proj}
	if len(cfg.Projects) != 1 || cfg.Projects[0] != want {
		t.Fatalf("projects = %+v, want [%+v]", cfg.Projects, want)
	}
}

func TestBootstrapAppendsToExisting(t *testing.T) {
	existing := t.TempDir()
	proj := t.TempDir()
	cfgPath := testConfig(t, existing)

	var out strings.Builder
	if code := runBootstrap(proj, cfgPath, "", false, false, &out); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Projects) != 2 || cfg.Projects[0].Path != existing || cfg.Projects[1].Path != proj {
		t.Fatalf("projects = %+v, want [%s %s]", cfg.Projects, existing, proj)
	}
}

func TestBootstrapIdempotent(t *testing.T) {
	proj := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")

	var out strings.Builder
	if code := runBootstrap(proj, cfgPath, "", false, false, &out); code != 0 {
		t.Fatalf("first run exit = %d", code)
	}
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if code := runBootstrap(proj, cfgPath, "", false, false, &out); code != 0 {
		t.Fatalf("second run exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "already registered") {
		t.Fatalf("output %q missing already registered", out.String())
	}
	after, _ := os.ReadFile(cfgPath)
	if string(before) != string(after) {
		t.Fatal("config changed on second run")
	}
}

func TestBootstrapMalformedConfig(t *testing.T) {
	proj := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	garbage := []byte("projects: [broken\n")
	if err := os.WriteFile(cfgPath, garbage, 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if code := runBootstrap(proj, cfgPath, "", false, false, &out); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	b, _ := os.ReadFile(cfgPath)
	if string(b) != string(garbage) {
		t.Fatal("malformed config was rewritten")
	}
}

// mkGitDir marks a temp dir as a git repo (bare .git folder is enough for
// the os.Stat gates under test).
func mkGitDir(t *testing.T, proj string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(proj, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapCreatesWikiAndGitignore(t *testing.T) {
	proj := t.TempDir()
	mkGitDir(t, proj)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")

	var out strings.Builder
	if code := runBootstrap(proj, cfgPath, "", false, false, &out); code != 0 {
		t.Fatalf("exit = %d, want 0 (out: %s)", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(proj, "wiki", ".gitkeep")); err != nil {
		t.Fatal("wiki/.gitkeep not created:", err)
	}
	b, err := os.ReadFile(filepath.Join(proj, ".gitignore"))
	if err != nil {
		t.Fatal(".gitignore not created:", err)
	}
	if !strings.Contains(string(b), ".memoria/\n") {
		t.Fatalf(".gitignore %q missing .memoria/ entry", b)
	}
}

func TestBootstrapWikiExistsFails(t *testing.T) {
	proj := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.Mkdir(filepath.Join(proj, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if code := runBootstrap(proj, cfgPath, "", false, false, &out); code != 1 {
		t.Fatalf("exit = %d, want 1 (out: %s)", code, out.String())
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatal("config written despite wiki error")
	}
	if _, err := os.Stat(filepath.Join(proj, ".gitignore")); !os.IsNotExist(err) {
		t.Fatal(".gitignore written despite wiki error")
	}
}

func TestBootstrapCustomWikiName(t *testing.T) {
	proj := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	// default "wiki" folder existing must not matter when a custom name is given
	if err := os.Mkdir(filepath.Join(proj, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if code := runBootstrap(proj, cfgPath, "docs", false, false, &out); code != 0 {
		t.Fatalf("exit = %d, want 0 (out: %s)", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(proj, "docs", ".gitkeep")); err != nil {
		t.Fatal("docs/.gitkeep not created:", err)
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Projects) != 1 || cfg.Projects[0].Wiki != "docs" {
		t.Fatalf("projects = %+v, want wiki name saved as docs", cfg.Projects)
	}
}

func TestBootstrapGitignoreAppend(t *testing.T) {
	proj := t.TempDir()
	mkGitDir(t, proj)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	gi := filepath.Join(proj, ".gitignore")
	// no trailing newline on purpose
	if err := os.WriteFile(gi, []byte("node_modules"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if code := runBootstrap(proj, cfgPath, "", false, false, &out); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	b, _ := os.ReadFile(gi)
	if string(b) != "node_modules\n.memoria/\n" {
		t.Fatalf(".gitignore = %q, want existing content preserved + entry", b)
	}
}

func TestBootstrapGitignoreNoDuplicate(t *testing.T) {
	proj := t.TempDir()
	mkGitDir(t, proj)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	gi := filepath.Join(proj, ".gitignore")
	if err := os.WriteFile(gi, []byte(".memoria/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if code := runBootstrap(proj, cfgPath, "", false, false, &out); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	b, _ := os.ReadFile(gi)
	if string(b) != ".memoria/\n" {
		t.Fatalf(".gitignore = %q, entry duplicated", b)
	}
}

func TestBootstrapNonRepoWarnsAndSkipsGitignore(t *testing.T) {
	proj := t.TempDir() // no .git — multirepo parent / plain folder
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")

	var out strings.Builder
	if code := runBootstrap(proj, cfgPath, "", false, false, &out); code != 0 {
		t.Fatalf("exit = %d, want 0 (out: %s)", code, out.String())
	}
	if !strings.Contains(out.String(), "not a git repository") {
		t.Fatalf("output %q missing non-repo warning", out.String())
	}
	if _, err := os.Stat(filepath.Join(proj, ".gitignore")); !os.IsNotExist(err) {
		t.Fatal(".gitignore created in non-repo folder")
	}
	if _, err := os.Stat(filepath.Join(proj, "wiki", ".gitkeep")); err != nil {
		t.Fatal("wiki still must be created:", err)
	}
}

func TestBootstrapWritesAgentsBlock(t *testing.T) {
	proj := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	var out strings.Builder
	if code := runBootstrap(proj, cfgPath, "", false, false, &out); code != 0 {
		t.Fatalf("exit = %d (out: %s)", code, out.String())
	}
	b, err := os.ReadFile(filepath.Join(proj, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md not created: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "<!-- memoria:start -->") || !strings.Contains(s, "<!-- memoria:end -->") {
		t.Fatalf("markers missing: %q", s)
	}
	if !strings.Contains(s, "wiki/index.md") {
		t.Fatalf("wiki folder not referenced: %q", s)
	}
	c, err := os.ReadFile(filepath.Join(proj, "CLAUDE.md"))
	if err != nil || !strings.Contains(string(c), "Read [AGENTS.md](AGENTS.md)") {
		t.Fatalf("CLAUDE.md shim = %q, %v", c, err)
	}
}

func TestBootstrapAppendsToExistingAgents(t *testing.T) {
	proj := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	existing := "# My Project\n\nsome docs"
	if err := os.WriteFile(filepath.Join(proj, "AGENTS.md"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	claude := "# CLAUDE.md\n\ncustom claude instructions\n"
	if err := os.WriteFile(filepath.Join(proj, "CLAUDE.md"), []byte(claude), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if code := runBootstrap(proj, cfgPath, "", false, false, &out); code != 0 {
		t.Fatalf("exit = %d (out: %s)", code, out.String())
	}
	b, _ := os.ReadFile(filepath.Join(proj, "AGENTS.md"))
	if !strings.HasPrefix(string(b), "# My Project\n\nsome docs\n\n<!-- memoria:start -->") {
		t.Fatalf("existing content mangled: %q", b)
	}
	c, _ := os.ReadFile(filepath.Join(proj, "CLAUDE.md"))
	if string(c) != claude {
		t.Fatalf("existing CLAUDE.md touched: %q", c)
	}
}

func TestBootstrapRepairsBlockOnRerun(t *testing.T) {
	proj := t.TempDir()
	cfgPath := testConfig(t, proj)
	agents := filepath.Join(proj, "AGENTS.md")
	stale := "# Docs\n\n<!-- memoria:start -->\nSTALE INSTRUCTIONS\n<!-- memoria:end -->\n\n## After\n"
	if err := os.WriteFile(agents, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if code := runBootstrap(proj, cfgPath, "", false, false, &out); code != 0 {
		t.Fatalf("exit = %d (out: %s)", code, out.String())
	}
	b, _ := os.ReadFile(agents)
	s := string(b)
	if strings.Contains(s, "STALE INSTRUCTIONS") {
		t.Fatalf("block not replaced: %q", s)
	}
	if !strings.HasPrefix(s, "# Docs\n\n<!-- memoria:start -->") || !strings.Contains(s, "\n\n## After\n") {
		t.Fatalf("surrounding content mangled: %q", s)
	}
	if strings.Count(s, "<!-- memoria:start -->") != 1 || strings.Count(s, "<!-- memoria:end -->") != 1 {
		t.Fatalf("markers duplicated: %q", s)
	}
}

func TestBootstrapBlockUsesCustomWikiName(t *testing.T) {
	proj := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config{Projects: []project{{Name: "p", Path: proj, Wiki: "kb"}}}
	if err := saveConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if code := runBootstrap(proj, cfgPath, "", false, false, &out); code != 0 {
		t.Fatalf("exit = %d (out: %s)", code, out.String())
	}
	b, _ := os.ReadFile(filepath.Join(proj, "AGENTS.md"))
	if !strings.Contains(string(b), "kb/index.md") {
		t.Fatalf("custom wiki name missing: %q", b)
	}
}

func TestBootstrapGlobalEnables(t *testing.T) {
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	var out strings.Builder
	if code := runBootstrapGlobal(cfgPath, "", &out); code != 0 {
		t.Fatalf("exit = %d (out: %s)", code, out.String())
	}
	if strings.Contains(out.String(), "already enabled") {
		t.Fatalf("fresh enable claimed already enabled: %s", out.String())
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Global || cfg.GlobalPath != "" {
		t.Fatalf("cfg = %+v, want global on with empty path", cfg)
	}
	if len(cfg.Projects) != 0 {
		t.Fatalf("projects touched: %+v", cfg.Projects)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "wiki", ".gitkeep")); err != nil {
		t.Fatal("wiki/.gitkeep missing")
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "wiki", ".git")); err != nil {
		t.Fatal("default global wiki must be its own git repo")
	}
	// config.yaml can hold an API key — the config dir must never become a repo
	if _, err := os.Stat(filepath.Join(cfgDir, ".git")); !os.IsNotExist(err) {
		t.Fatal("config dir was turned into a git repo")
	}
	if fi, _ := os.Stat(cfgPath); fi.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestBootstrapGlobalPath(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	var out strings.Builder
	if code := runBootstrapGlobal(cfgPath, "gdir", &out); code != 0 {
		t.Fatalf("exit = %d (out: %s)", code, out.String())
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(cfg.GlobalPath) || filepath.Base(cfg.GlobalPath) != "gdir" {
		t.Fatalf("global_path = %q, want the relative --global-path stored absolute", cfg.GlobalPath)
	}
	root := cfg.GlobalPath
	if _, err := os.Stat(filepath.Join(root, "wiki", ".gitkeep")); err != nil {
		t.Fatal("wiki/.gitkeep missing under --global-path root")
	}
	// --global-path = the user's folder: memoria never touches git there
	for _, p := range []string{filepath.Join(root, ".git"), filepath.Join(root, "wiki", ".git")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s created — git must not be touched for --global-path roots", p)
		}
	}
}

func TestBootstrapGlobalFlagErrors(t *testing.T) {
	for _, args := range [][]string{
		{"bootstrap", "--global-path", "x"},
		{"bootstrap", "--global", "--wiki", "x"},
		{"bootstrap", "--global", "--background"},
	} {
		var out strings.Builder
		if code := run(args, strings.NewReader(""), &out); code != 1 {
			t.Fatalf("%v: exit = %d, want 1 (out: %s)", args, code, out.String())
		}
	}
}

func TestBootstrapGlobalIdempotent(t *testing.T) {
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	var out strings.Builder
	if code := runBootstrapGlobal(cfgPath, "", &out); code != 0 {
		t.Fatalf("first run exit = %d (out: %s)", code, out.String())
	}
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(cfgDir, "wiki", "concepts", "a.md")
	if err := os.MkdirAll(filepath.Dir(page), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(page, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if code := runBootstrapGlobal(cfgPath, "", &out); code != 0 {
		t.Fatalf("second run exit = %d (out: %s)", code, out.String())
	}
	if !strings.Contains(out.String(), "already enabled") {
		t.Fatalf("output %q missing already enabled", out.String())
	}
	after, _ := os.ReadFile(cfgPath)
	if string(before) != string(after) {
		t.Fatal("config changed on second run")
	}
	if b, _ := os.ReadFile(page); string(b) != "body" {
		t.Fatal("existing wiki page touched on re-run")
	}
}

func TestBootstrapGlobalPathChangeWarnsPending(t *testing.T) {
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	var out strings.Builder
	if code := runBootstrapGlobal(cfgPath, "", &out); code != 0 {
		t.Fatalf("enable exit = %d", code)
	}
	if err := queueAdd(queuePath(cfgPath), globalName, "/old/root/.memoria/sessions/pending/s1.md"); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	newRoot := filepath.Join(t.TempDir(), "g")
	if code := runBootstrapGlobal(cfgPath, newRoot, &out); code != 0 {
		t.Fatalf("path change exit = %d (out: %s)", code, out.String())
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.GlobalPath != newRoot {
		t.Fatalf("global_path = %q, want %q", cfg.GlobalPath, newRoot)
	}
	if !strings.Contains(out.String(), "changed") || !strings.Contains(out.String(), "pending") {
		t.Fatalf("output %q missing root-change note and pending warning", out.String())
	}

	// entries already under the destination root are not stranded — moving
	// again must not warn about them
	if err := queueRemove(queuePath(cfgPath), globalName, "/old/root/.memoria/sessions/pending/s1.md"); err != nil {
		t.Fatal(err)
	}
	next := filepath.Join(t.TempDir(), "g2")
	if err := queueAdd(queuePath(cfgPath), globalName, filepath.Join(next, ".memoria", "sessions", "pending", "s2.md")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := runBootstrapGlobal(cfgPath, next, &out); code != 0 {
		t.Fatalf("second path change exit = %d (out: %s)", code, out.String())
	}
	if strings.Contains(out.String(), "pending") {
		t.Fatalf("warned about entries already under the new root: %s", out.String())
	}
}

func TestBootstrapGlobalMalformedConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	garbage := []byte("projects: [broken\n")
	if err := os.WriteFile(cfgPath, garbage, 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if code := runBootstrapGlobal(cfgPath, "", &out); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	b, _ := os.ReadFile(cfgPath)
	if string(b) != string(garbage) {
		t.Fatal("malformed config was rewritten")
	}
}
