package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGuidePage(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGuideListsOnlyRegisteredAndGlobalWikis(t *testing.T) {
	_, configPath := initEnv(t)
	alpha := t.TempDir()
	beta := t.TempDir()
	missing := filepath.Join(t.TempDir(), "gone")
	global := t.TempDir()

	writeGuidePage(t, filepath.Join(alpha, "wiki"), "index.md", `---
tags: [index]
---
# Alpha wiki

Tracks durable architecture decisions for Alpha.
`)
	writeGuidePage(t, filepath.Join(alpha, "wiki"), "decisions/one.md", "# One\n")
	writeGuidePage(t, filepath.Join(alpha, "wiki"), "trash/sessions/old.md", "# Old\n")
	writeGuidePage(t, filepath.Join(beta, "knowledge"), "rules/style.md", "# Style\n")
	writeGuidePage(t, filepath.Join(global, "wiki"), "index.md", "# Global\n\nShared preferences across projects.\n")
	// An unregistered wiki must never appear.
	unregistered := filepath.Join(t.TempDir(), "wiki")
	writeGuidePage(t, unregistered, "index.md", "# Unregistered\n")

	if err := saveConfig(configPath, config{
		Projects: []project{
			{Name: "alpha", Path: alpha},
			{Name: "beta", Path: beta, Wiki: "knowledge"},
			{Name: "missing", Path: missing},
		},
		Global:     true,
		GlobalPath: global,
	}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := run([]string{"guide"}, strings.NewReader(""), &out); code != 0 {
		t.Fatalf("guide = %d: %s", code, out.String())
	}
	s := out.String()
	for _, want := range []string{
		"alpha", filepath.Join(alpha, "wiki"), "Pages: 2",
		"Tracks durable architecture decisions for Alpha.",
		"beta", filepath.Join(beta, "knowledge"), "1 Markdown page across rules.",
		"missing", filepath.Join(missing, "wiki"), "Wiki directory is missing.",
		globalName, filepath.Join(global, "wiki"), "Shared preferences across projects.",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("guide output missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, unregistered) || strings.Contains(s, "Old") {
		t.Fatalf("guide included unregistered or trashed content:\n%s", s)
	}
}

func TestGuideWithoutRegisteredWikis(t *testing.T) {
	initEnv(t)
	var out bytes.Buffer
	if code := run([]string{"guide"}, strings.NewReader(""), &out); code != 0 {
		t.Fatalf("guide = %d: %s", code, out.String())
	}
	if out.String() != "No registered wikis.\n" {
		t.Fatalf("output = %q", out.String())
	}
}
