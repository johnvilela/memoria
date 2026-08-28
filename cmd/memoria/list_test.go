package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListEmpty(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	var out strings.Builder
	if code := runList(cfgPath, &out); code != 0 {
		t.Fatalf("exit = %d, want 0 (out: %s)", code, out.String())
	}
	if !strings.Contains(out.String(), "No projects registered") {
		t.Fatalf("output %q missing empty message", out.String())
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatal("config file created")
	}
}

func TestListOutput(t *testing.T) {
	live := t.TempDir()
	stale := filepath.Join(t.TempDir(), "renamed-away") // never created
	cfgPath := testConfig(t, live, stale)
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Projects = append(cfg.Projects, project{Name: "kbproj", Path: live, Wiki: "kb"})
	if err := saveConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if code := runList(cfgPath, &out); code != 0 {
		t.Fatalf("exit = %d, want 0 (out: %s)", code, out.String())
	}
	s := out.String()
	for _, w := range []string{
		"PROJECT", "WIKI", "STATUS", "PATH",
		filepath.Base(live), live,
		filepath.Base(stale), "missing", "memoria remove",
		"kbproj", "kb", "ok",
	} {
		if !strings.Contains(s, w) {
			t.Fatalf("output missing %q:\n%s", w, s)
		}
	}
	if strings.Contains(s, "Global capture") {
		t.Fatalf("global line printed with global off:\n%s", s)
	}
}

func TestListAllLiveNoStaleHint(t *testing.T) {
	cfgPath := testConfig(t, t.TempDir())
	var out strings.Builder
	if code := runList(cfgPath, &out); code != 0 {
		t.Fatalf("exit = %d, want 0 (out: %s)", code, out.String())
	}
	if strings.Contains(out.String(), "memoria remove") {
		t.Fatalf("stale hint printed with no stale entries:\n%s", out.String())
	}
}

func TestListGlobalLine(t *testing.T) {
	cfgPath := testConfig(t, t.TempDir())
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Global = true
	if err := saveConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if code := runList(cfgPath, &out); code != 0 {
		t.Fatalf("exit = %d, want 0 (out: %s)", code, out.String())
	}
	if !strings.Contains(out.String(), "Global capture: on ("+filepath.Dir(cfgPath)+")") {
		t.Fatalf("output %q missing global line", out.String())
	}
}

func TestListMalformedConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	garbage := []byte("projects: [broken\n")
	if err := os.WriteFile(cfgPath, garbage, 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if code := runList(cfgPath, &out); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	b, _ := os.ReadFile(cfgPath)
	if string(b) != string(garbage) {
		t.Fatal("malformed config was rewritten")
	}
}
