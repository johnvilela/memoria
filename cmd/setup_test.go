package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runSetupCmd(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	code := run(append([]string{"setup"}, args...), strings.NewReader(""), &buf)
	return code, buf.String()
}

func TestSetupRequiresExistingConfig(t *testing.T) {
	initEnv(t)
	code, out := runSetupCmd(t, "--processor", "ollama")
	if code != 1 || !strings.Contains(out, "memoria init") {
		t.Fatalf("setup without config: code=%d out=%q, want error pointing at init", code, out)
	}
}

func TestSetupProcessorOnlyLeavesHooksAlone(t *testing.T) {
	home, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Processor: "claude-code", Notifications: true}); err != nil {
		t.Fatal(err)
	}
	code, out := runSetupCmd(t, "--processor", "ollama")
	if code != 0 {
		t.Fatalf("setup = %d: %s", code, out)
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Processor != "ollama" || !cfg.Notifications {
		t.Fatalf("config = %+v, want processor updated + notifications preserved", cfg)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatal("setup installed hooks")
	}
}

func TestSetupNotificationFalsePreservesProcessor(t *testing.T) {
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Processor: "ollama", Notifications: true}); err != nil {
		t.Fatal(err)
	}
	code, out := runSetupCmd(t, "--notification=false")
	if code != 0 {
		t.Fatalf("setup = %d: %s", code, out)
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Notifications || cfg.Processor != "ollama" {
		t.Fatalf("config = %+v, want notifications off + processor kept", cfg)
	}
}

func TestSetupNoProcessorHintWhenConfigured(t *testing.T) {
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Processor: "ollama"}); err != nil {
		t.Fatal(err)
	}
	code, out := runSetupCmd(t, "--notification")
	if code != 0 {
		t.Fatalf("setup = %d: %s", code, out)
	}
	if strings.Contains(out, "No processor configured") {
		t.Fatalf("misleading hint with processor configured: %q", out)
	}
}

func TestSetupNoFlagsNonTTY(t *testing.T) {
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Processor: "ollama"}); err != nil {
		t.Fatal(err)
	}
	code, out := runSetupCmd(t)
	if code != 1 || !strings.Contains(out, "usage") {
		t.Fatalf("no-op setup: code=%d out=%q, want usage + 1", code, out)
	}
}

func TestSetupRejectsUnknownProcessor(t *testing.T) {
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Processor: "ollama"}); err != nil {
		t.Fatal(err)
	}
	code, out := runSetupCmd(t, "--processor", "gpt5")
	if code != 1 || !strings.Contains(out, "gpt5") {
		t.Fatalf("unknown processor: code=%d out=%q", code, out)
	}
}

func TestSetupSingleFlagSkipsPrompts(t *testing.T) {
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Processor: "ollama"}); err != nil {
		t.Fatal(err)
	}
	// TTY on: any passed flag must still skip all prompts (a real prompt
	// would fail here — bubbletea can't run against the test's stdin)
	orig := isTTY
	isTTY = func() bool { return true }
	t.Cleanup(func() { isTTY = orig })
	code, out := runSetupCmd(t, "--notification")
	if code != 0 || strings.Contains(out, "aborted") {
		t.Fatalf("setup with flag opened prompts: code=%d out=%q", code, out)
	}
	cfg, _ := loadConfig(cfgPath)
	if !cfg.Notifications || cfg.Processor != "ollama" {
		t.Fatalf("config = %+v, want notifications on + processor kept", cfg)
	}
}

func TestSetupClientFlagInstallsHooks(t *testing.T) {
	home, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Processor: "ollama"}); err != nil {
		t.Fatal(err)
	}
	// TTY on: --client must stay surgical, no prompts
	orig := isTTY
	isTTY = func() bool { return true }
	t.Cleanup(func() { isTTY = orig })
	code, out := runSetupCmd(t, "--client", "codex")
	if code != 0 || strings.Contains(out, "aborted") {
		t.Fatalf("setup --client codex: code=%d out=%q", code, out)
	}
	for _, rel := range []string{filepath.Join(".codex", "hooks.json"), filepath.Join(".codex", "config.toml")} {
		if _, err := os.Stat(filepath.Join(home, rel)); err != nil {
			t.Fatalf("%s missing: %v", rel, err)
		}
	}
	cfg, _ := loadConfig(cfgPath)
	if len(cfg.Clients) != 1 || cfg.Clients[0] != "codex" {
		t.Fatalf("clients = %v, want [codex]", cfg.Clients)
	}
	if cfg.Processor != "ollama" {
		t.Fatalf("processor changed: %+v", cfg)
	}
}

func TestSetupClientFlagUnknown(t *testing.T) {
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Processor: "ollama"}); err != nil {
		t.Fatal(err)
	}
	code, out := runSetupCmd(t, "--client", "emacs")
	if code != 1 || !strings.Contains(out, "emacs") {
		t.Fatalf("setup --client emacs: code=%d out=%q", code, out)
	}
}

// stubSetupPrompts forces TTY and answers "keep" to every single-select.
func stubSetupPrompts(t *testing.T, multi func(string, []option) ([]string, error)) {
	t.Helper()
	origTTY, origSel, origMulti := isTTY, selectOption, selectMulti
	t.Cleanup(func() { isTTY, selectOption, selectMulti = origTTY, origSel, origMulti })
	isTTY = func() bool { return true }
	selectOption = func(title string, opts []option) (string, error) { return "keep", nil }
	selectMulti = multi
}

func TestSetupInteractiveAddsAgent(t *testing.T) {
	home, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Processor: "ollama", Clients: []string{"claude-code"}}); err != nil {
		t.Fatal(err)
	}
	var gotOpts []option
	stubSetupPrompts(t, func(title string, opts []option) ([]string, error) {
		gotOpts = opts
		return []string{"codex"}, nil
	})
	code, out := runSetupCmd(t)
	if code != 0 {
		t.Fatalf("setup = %d: %s", code, out)
	}
	if !strings.Contains(out, "installed for: claude-code") {
		t.Fatalf("missing installed line: %q", out)
	}
	if len(gotOpts) != 1 || gotOpts[0].value != "codex" {
		t.Fatalf("multi-select opts = %v, want only codex", gotOpts)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "hooks.json")); err != nil {
		t.Fatalf("codex hooks missing: %v", err)
	}
	cfg, _ := loadConfig(cfgPath)
	if len(cfg.Clients) != 2 {
		t.Fatalf("clients = %v, want both", cfg.Clients)
	}
}

func TestSetupInteractiveBackfill(t *testing.T) {
	home, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Processor: "ollama"}); err != nil {
		t.Fatal(err)
	}
	// pre-feature install: hooks on disk, nothing recorded in config
	settings := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"/usr/bin/memoria hook session-start --client claude-code"}]}]}}`
	if err := os.WriteFile(settings, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	stubSetupPrompts(t, func(title string, opts []option) ([]string, error) { return nil, nil })
	code, out := runSetupCmd(t)
	if code != 0 {
		t.Fatalf("setup = %d: %s", code, out)
	}
	if !strings.Contains(out, "installed for: claude-code") {
		t.Fatalf("backfill not shown: %q", out)
	}
	cfg, _ := loadConfig(cfgPath)
	if len(cfg.Clients) != 1 || cfg.Clients[0] != "claude-code" {
		t.Fatalf("clients = %v, want backfilled [claude-code]", cfg.Clients)
	}
}

func TestSetupAllInstalledSkipsMultiSelect(t *testing.T) {
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Processor: "ollama", Clients: []string{"claude-code", "codex"}}); err != nil {
		t.Fatal(err)
	}
	stubSetupPrompts(t, func(title string, opts []option) ([]string, error) {
		t.Fatal("multi-select shown with all agents installed")
		return nil, nil
	})
	code, out := runSetupCmd(t)
	if code != 0 {
		t.Fatalf("setup = %d: %s", code, out)
	}
}

func TestSetupCronSmoke(t *testing.T) {
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Processor: "ollama"}); err != nil {
		t.Fatal(err)
	}
	stubSystemctl(t)
	stubLaunchctl(t)
	code, out := runSetupCmd(t, "--cron", "0 8 * * *")
	if code != 0 {
		t.Fatalf("setup = %d: %s", code, out)
	}
	// the schedule artifact is OS-native: systemd timer on linux, launchd plist
	// on macOS. Both encode "08:00 daily".
	if runtime.GOOS == "darwin" {
		plist, err := os.ReadFile(agentPlist(t))
		if err != nil || !strings.Contains(string(plist), "<integer>8</integer>") {
			t.Fatalf("plist = %q, %v", plist, err)
		}
	} else {
		tmr, err := os.ReadFile(filepath.Join(unitDir(cfgPath), "memoria-process.timer"))
		if err != nil || !strings.Contains(string(tmr), "OnCalendar=*-*-* 08:00:00") {
			t.Fatalf("timer = %q, %v", tmr, err)
		}
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Cron != "0 8 * * *" {
		t.Fatalf("cron = %q", cfg.Cron)
	}
}

func TestSetupAutoApply(t *testing.T) {
	_, cfgPath := initEnv(t)
	if code, out := runInitCmd(t, "claude-code", "--processor", "ollama"); code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	var buf bytes.Buffer
	if code := runSetup([]string{"--auto-apply"}, cfgPath, &buf); code != 0 {
		t.Fatalf("setup = %d: %s", code, buf.String())
	}
	cfg, _ := loadConfig(cfgPath)
	if !cfg.AutoApply {
		t.Fatal("auto_apply not saved by setup")
	}
	// unrelated flag keeps it
	buf.Reset()
	if code := runSetup([]string{"--processor", "ollama"}, cfgPath, &buf); code != 0 {
		t.Fatalf("setup = %d: %s", code, buf.String())
	}
	if cfg, _ := loadConfig(cfgPath); !cfg.AutoApply {
		t.Fatal("auto_apply lost on unrelated setup")
	}
	// and it can be turned off again
	if code := runSetup([]string{"--auto-apply=false"}, cfgPath, &buf); code != 0 {
		t.Fatalf("setup off = %d: %s", code, buf.String())
	}
	if cfg, _ := loadConfig(cfgPath); cfg.AutoApply {
		t.Fatal("auto_apply not disabled")
	}
}

func TestSetupGlobalDisable(t *testing.T) {
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Global: true, GlobalPath: "/data/g"}); err != nil {
		t.Fatal(err)
	}
	code, out := runSetupCmd(t, "--global=false")
	if code != 0 {
		t.Fatalf("setup = %d: %s", code, out)
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Global {
		t.Fatal("global still enabled")
	}
	if cfg.GlobalPath != "/data/g" {
		t.Fatalf("global_path = %q, want kept for a later re-enable", cfg.GlobalPath)
	}
}

func TestSetupGlobalEnableKeepsPath(t *testing.T) {
	_, cfgPath := initEnv(t)
	root := t.TempDir()
	if err := saveConfig(cfgPath, config{GlobalPath: root}); err != nil {
		t.Fatal(err)
	}
	code, out := runSetupCmd(t, "--global")
	if code != 0 {
		t.Fatalf("setup = %d: %s", code, out)
	}
	cfg, _ := loadConfig(cfgPath)
	if !cfg.Global || cfg.GlobalPath != root {
		t.Fatalf("cfg = %+v, want enabled with the stored path kept", cfg)
	}
	if _, err := os.Stat(filepath.Join(root, "wiki", ".gitkeep")); err != nil {
		t.Fatal("ensure steps skipped on re-enable")
	}
	if _, err := os.Stat(filepath.Join(root, "wiki", ".git")); !os.IsNotExist(err) {
		t.Fatal("git touched for a global_path root")
	}
}

func TestSetupGlobalPathChange(t *testing.T) {
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Global: true}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "g")
	code, out := runSetupCmd(t, "--global-path", root)
	if code != 0 {
		t.Fatalf("setup = %d: %s", code, out)
	}
	cfg, _ := loadConfig(cfgPath)
	if !cfg.Global || cfg.GlobalPath != root {
		t.Fatalf("cfg = %+v, want path moved with global kept on", cfg)
	}
	if _, err := os.Stat(filepath.Join(root, "wiki", ".gitkeep")); err != nil {
		t.Fatal("new root not prepared")
	}
}

func TestSetupGlobalPathErrors(t *testing.T) {
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{}); err != nil {
		t.Fatal(err)
	}
	if code, out := runSetupCmd(t, "--global-path", "/x"); code != 1 || !strings.Contains(out, "--global") {
		t.Fatalf("path with global off: code=%d out=%q, want error pointing at --global", code, out)
	}
	if code, out := runSetupCmd(t, "--global=false", "--global-path", "/x"); code != 1 {
		t.Fatalf("--global=false --global-path: code=%d out=%q, want error", code, out)
	}
}
