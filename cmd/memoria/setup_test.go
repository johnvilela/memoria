package main

import (
	"bytes"
	"os"
	"path/filepath"
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

func TestSetupCronSmoke(t *testing.T) {
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Processor: "ollama"}); err != nil {
		t.Fatal(err)
	}
	stubSystemctl(t)
	code, out := runSetupCmd(t, "--cron", "0 8 * * *")
	if code != 0 {
		t.Fatalf("setup = %d: %s", code, out)
	}
	tmr, err := os.ReadFile(filepath.Join(unitDir(cfgPath), "memoria-process.timer"))
	if err != nil || !strings.Contains(string(tmr), "OnCalendar=*-*-* 08:00:00") {
		t.Fatalf("timer = %q, %v", tmr, err)
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
