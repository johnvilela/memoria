package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// removeTTY forces the TTY gate and stubs the picker to return pick (or an
// abort error when pick is empty), capturing the options it was shown.
func removeTTY(t *testing.T, pick string) *[]option {
	t.Helper()
	origTTY, origSel := isTTY, selectOption
	t.Cleanup(func() { isTTY, selectOption = origTTY, origSel })
	isTTY = func() bool { return true }
	var shown []option
	selectOption = func(title string, opts []option) (string, error) {
		shown = opts
		if pick == "" {
			return "", fmt.Errorf("aborted")
		}
		return pick, nil
	}
	return &shown
}

func TestRemoveProject(t *testing.T) {
	gone := t.TempDir() // registered, then "renamed": path removed below
	keep := t.TempDir()
	page := filepath.Join(gone, "wiki", "index.md")
	if err := os.MkdirAll(filepath.Dir(page), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(page, []byte("# Home\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := testConfig(t, gone, keep)
	goneName, keepName := filepath.Base(gone), filepath.Base(keep)
	if err := queueAdd(queuePath(cfgPath), goneName, "/x/s1.md"); err != nil {
		t.Fatal(err)
	}
	if err := queueAdd(queuePath(cfgPath), keepName, "/x/s2.md"); err != nil {
		t.Fatal(err)
	}
	if err := statusSet(statusPath(cfgPath), goneName, "done", 0, "ok"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runLogPath(cfgPath, goneName), []byte("log"), 0o644); err != nil {
		t.Fatal(err)
	}
	shown := removeTTY(t, gone)

	var out strings.Builder
	if code := runRemove(cfgPath, &out); code != 0 {
		t.Fatalf("exit = %d, want 0 (out: %s)", code, out.String())
	}
	if !strings.Contains(out.String(), "Removed "+goneName) {
		t.Fatalf("output %q missing Removed", out.String())
	}
	if len(*shown) != 2 || (*shown)[0].value != gone || (*shown)[1].value != keep {
		t.Fatalf("picker options = %+v, want both projects by path", *shown)
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Projects) != 1 || cfg.Projects[0].Path != keep {
		t.Fatalf("projects = %+v, want only %s", cfg.Projects, keep)
	}
	q, _ := loadQueue(queuePath(cfgPath))
	if _, ok := q[goneName]; ok {
		t.Fatal("removed project's pending entries kept")
	}
	if len(q[keepName]) != 1 {
		t.Fatalf("other project's queue touched: %+v", q)
	}
	st, _ := loadStatus(statusPath(cfgPath))
	if _, ok := st[goneName]; ok {
		t.Fatal("removed project's status entry kept")
	}
	if _, err := os.Stat(runLogPath(cfgPath, goneName)); !os.IsNotExist(err) {
		t.Fatal("removed project's run log kept")
	}
	if b, _ := os.ReadFile(page); string(b) != "# Home\n" {
		t.Fatalf("project files touched: %q", b)
	}
}

func TestRemoveFlagsMissingPath(t *testing.T) {
	live := t.TempDir()
	stale := filepath.Join(t.TempDir(), "renamed-away") // never created
	cfgPath := testConfig(t, live, stale)
	shown := removeTTY(t, stale)

	var out strings.Builder
	if code := runRemove(cfgPath, &out); code != 0 {
		t.Fatalf("exit = %d, want 0 (out: %s)", code, out.String())
	}
	if len(*shown) != 2 || strings.Contains((*shown)[0].desc, "missing") || !strings.Contains((*shown)[1].desc, "missing") {
		t.Fatalf("picker options = %+v, want only the stale entry marked missing", *shown)
	}
	cfg, _ := loadConfig(cfgPath)
	if len(cfg.Projects) != 1 || cfg.Projects[0].Path != live {
		t.Fatalf("projects = %+v, want only %s", cfg.Projects, live)
	}
}

func TestRemoveSharedNameKeepsSidecar(t *testing.T) {
	// a moved folder can be registered twice under the same base name —
	// removing one entry must keep the shared name-keyed sidecar state
	a := filepath.Join(t.TempDir(), "app")
	b := filepath.Join(t.TempDir(), "app")
	for _, p := range []string{a, b} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath := testConfig(t, a, b)
	if err := queueAdd(queuePath(cfgPath), "app", "/x/s1.md"); err != nil {
		t.Fatal(err)
	}
	if err := statusSet(statusPath(cfgPath), "app", "done", 0, "ok"); err != nil {
		t.Fatal(err)
	}
	removeTTY(t, a)

	var out strings.Builder
	if code := runRemove(cfgPath, &out); code != 0 {
		t.Fatalf("exit = %d, want 0 (out: %s)", code, out.String())
	}
	cfg, _ := loadConfig(cfgPath)
	if len(cfg.Projects) != 1 || cfg.Projects[0].Path != b {
		t.Fatalf("projects = %+v, want only %s", cfg.Projects, b)
	}
	q, _ := loadQueue(queuePath(cfgPath))
	if len(q["app"]) != 1 {
		t.Fatalf("shared-name queue dropped: %+v", q)
	}
	st, _ := loadStatus(statusPath(cfgPath))
	if _, ok := st["app"]; !ok {
		t.Fatal("shared-name status dropped")
	}
}

func TestRemoveNonTTY(t *testing.T) {
	cfgPath := testConfig(t, t.TempDir())
	origSel := selectOption
	selectOption = func(title string, opts []option) (string, error) {
		t.Errorf("selectOption called without a TTY")
		return "", fmt.Errorf("no")
	}
	t.Cleanup(func() { selectOption = origSel })

	var out strings.Builder
	if code := runRemove(cfgPath, &out); code != 1 {
		t.Fatalf("exit = %d, want 1 (out: %s)", code, out.String())
	}
	if !strings.Contains(out.String(), "interactive") {
		t.Fatalf("output %q missing interactive error", out.String())
	}
	cfg, _ := loadConfig(cfgPath)
	if len(cfg.Projects) != 1 {
		t.Fatalf("config changed: %+v", cfg.Projects)
	}
}

func TestRemoveNoProjects(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	var out strings.Builder
	if code := runRemove(cfgPath, &out); code != 1 {
		t.Fatalf("exit = %d, want 1 (out: %s)", code, out.String())
	}
	if !strings.Contains(out.String(), "no registered projects") {
		t.Fatalf("output %q missing no-projects message", out.String())
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatal("config file created")
	}
}

func TestRemoveMalformedConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	garbage := []byte("projects: [broken\n")
	if err := os.WriteFile(cfgPath, garbage, 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if code := runRemove(cfgPath, &out); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	b, _ := os.ReadFile(cfgPath)
	if string(b) != string(garbage) {
		t.Fatal("malformed config was rewritten")
	}
}

func TestRemoveAborted(t *testing.T) {
	cfgPath := testConfig(t, t.TempDir())
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	removeTTY(t, "") // esc

	var out strings.Builder
	if code := runRemove(cfgPath, &out); code != 0 {
		t.Fatalf("exit = %d, want 0 (out: %s)", code, out.String())
	}
	after, _ := os.ReadFile(cfgPath)
	if string(before) != string(after) {
		t.Fatal("config changed on abort")
	}
}

func TestRemoveRefusesWhileRunning(t *testing.T) {
	proj := t.TempDir()
	cfgPath := testConfig(t, proj)
	name := filepath.Base(proj)
	if err := statusSet(statusPath(cfgPath), name, "running", os.Getpid(), ""); err != nil {
		t.Fatal(err)
	}
	removeTTY(t, proj)

	var out strings.Builder
	if code := runRemove(cfgPath, &out); code != 1 {
		t.Fatalf("exit = %d, want 1 (out: %s)", code, out.String())
	}
	if !strings.Contains(out.String(), "running") {
		t.Fatalf("output %q missing running error", out.String())
	}
	cfg, _ := loadConfig(cfgPath)
	if len(cfg.Projects) != 1 {
		t.Fatalf("config changed: %+v", cfg.Projects)
	}
}
