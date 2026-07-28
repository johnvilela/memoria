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
	if code := runBootstrap(proj, cfgPath, "", &out); code != 0 {
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
	if code := runBootstrap(proj, cfgPath, "", &out); code != 0 {
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
	if code := runBootstrap(proj, cfgPath, "", &out); code != 0 {
		t.Fatalf("first run exit = %d", code)
	}
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if code := runBootstrap(proj, cfgPath, "", &out); code != 0 {
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
	if code := runBootstrap(proj, cfgPath, "", &out); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	b, _ := os.ReadFile(cfgPath)
	if string(b) != string(garbage) {
		t.Fatal("malformed config was rewritten")
	}
}

func TestBootstrapCreatesWikiAndGitignore(t *testing.T) {
	proj := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")

	var out strings.Builder
	if code := runBootstrap(proj, cfgPath, "", &out); code != 0 {
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
	if code := runBootstrap(proj, cfgPath, "", &out); code != 1 {
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
	if code := runBootstrap(proj, cfgPath, "docs", &out); code != 0 {
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
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	gi := filepath.Join(proj, ".gitignore")
	// no trailing newline on purpose
	if err := os.WriteFile(gi, []byte("node_modules"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if code := runBootstrap(proj, cfgPath, "", &out); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	b, _ := os.ReadFile(gi)
	if string(b) != "node_modules\n.memoria/\n" {
		t.Fatalf(".gitignore = %q, want existing content preserved + entry", b)
	}
}

func TestBootstrapGitignoreNoDuplicate(t *testing.T) {
	proj := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	gi := filepath.Join(proj, ".gitignore")
	if err := os.WriteFile(gi, []byte(".memoria/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if code := runBootstrap(proj, cfgPath, "", &out); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	b, _ := os.ReadFile(gi)
	if string(b) != ".memoria/\n" {
		t.Fatalf(".gitignore = %q, entry duplicated", b)
	}
}
