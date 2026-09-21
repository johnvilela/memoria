package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDisableRemovesAllMemoriaHooksAndSchedule(t *testing.T) {
	if testing.Short() {
		t.Skip("scheduler behavior")
	}
	home, configPath := initEnv(t)
	systemctl := stubSystemctl(t)
	stubLaunchctl(t)

	claudePath := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(claudePath), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"theme":"dark","hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"other-tool --check"}]}]}}`
	if err := os.WriteFile(claudePath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installHooks(claudeEvents, claudePath, "/old/memoria", "claude-code"); err != nil {
		t.Fatal(err)
	}
	codexPath := filepath.Join(home, ".codex", "hooks.json")
	if err := installHooks(codexEvents, codexPath, "/new/memoria", "codex"); err != nil {
		t.Fatal(err)
	}

	if err := saveConfig(configPath, config{
		Clients:   []string{"claude-code", "codex"},
		Cron:      "daily",
		CronApply: true,
	}); err != nil {
		t.Fatal(err)
	}
	var schedulePaths []string
	switch runtime.GOOS {
	case "linux":
		schedulePaths = []string{
			filepath.Join(unitDir(configPath), "memoria-process.service"),
			filepath.Join(unitDir(configPath), "memoria-process.timer"),
		}
	case "darwin":
		schedulePaths = []string{agentPlist(t)}
	}
	if _, err := installSchedule("daily", "/bin/memoria", []string{"process", "--all"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := run([]string{"disable"}, strings.NewReader(""), &out); code != 0 {
		t.Fatalf("disable = %d: %s", code, out.String())
	}
	for _, path := range []string{claudePath, codexPath} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "memoria hook ") {
			t.Fatalf("memoria hook remains in %s:\n%s", path, b)
		}
	}
	claude := readSettings(t, claudePath)
	if got := hookCommands(t, claude, "SessionStart"); len(got) != 1 || got[0] != "other-tool --check" {
		t.Fatalf("foreign hook changed: %v", got)
	}
	if claude["theme"] != "dark" {
		t.Fatal("unrelated Claude setting lost")
	}

	for _, path := range schedulePaths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s remains after disable", path)
		}
	}
	if runtime.GOOS == "linux" && !systemctlCalled(*systemctl, "disable") {
		t.Fatalf("systemctl calls = %v, want disable", *systemctl)
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Clients) != 0 || cfg.Cron != "" || cfg.CronApply {
		t.Fatalf("disable state not saved: %+v", cfg)
	}
	if !strings.Contains(out.String(), "disabled") || !strings.Contains(out.String(), "wiki data") {
		t.Fatalf("missing confirmation: %q", out.String())
	}
}

func TestDisableIsIdempotentWithoutConfig(t *testing.T) {
	_, configPath := initEnv(t)
	stubSystemctl(t)
	stubLaunchctl(t)

	for range 2 {
		var out bytes.Buffer
		if code := run([]string{"disable"}, strings.NewReader(""), &out); code != 0 {
			t.Fatalf("disable = %d: %s", code, out.String())
		}
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatal("disable created a missing config")
	}
}
