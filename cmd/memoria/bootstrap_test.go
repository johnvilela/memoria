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
	if code := runBootstrap(proj, cfgPath, &out); code != 0 {
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
	if code := runBootstrap(proj, cfgPath, &out); code != 0 {
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
	if code := runBootstrap(proj, cfgPath, &out); code != 0 {
		t.Fatalf("first run exit = %d", code)
	}
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if code := runBootstrap(proj, cfgPath, &out); code != 0 {
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
	if code := runBootstrap(proj, cfgPath, &out); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	b, _ := os.ReadFile(cfgPath)
	if string(b) != string(garbage) {
		t.Fatal("malformed config was rewritten")
	}
}
