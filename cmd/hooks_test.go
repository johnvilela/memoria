package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("settings not valid JSON: %v", err)
	}
	return m
}

func hookCommands(t *testing.T, settings map[string]any, event string) []string {
	t.Helper()
	hooks, _ := settings["hooks"].(map[string]any)
	entries, _ := hooks[event].([]any)
	var cmds []string
	for _, e := range entries {
		em, _ := e.(map[string]any)
		inner, _ := em["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if c, ok := hm["command"].(string); ok {
				cmds = append(cmds, c)
			}
		}
	}
	return cmds
}

func TestInstallHooksFreshClaude(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude", "settings.json")
	if err := installHooks(claudeEvents, path, "/opt/bin/memoria", "claude-code"); err != nil {
		t.Fatal(err)
	}
	s := readSettings(t, path)
	hooks, _ := s["hooks"].(map[string]any)
	if len(hooks) != 11 {
		t.Fatalf("claude events installed = %d, want 11", len(hooks))
	}
	cmds := hookCommands(t, s, "SessionStart")
	if len(cmds) != 1 || cmds[0] != "/opt/bin/memoria hook session-start --client claude-code" {
		t.Fatalf("SessionStart command = %v", cmds)
	}
	if got := hookCommands(t, s, "UserPromptSubmit"); len(got) != 1 || got[0] != "/opt/bin/memoria hook user-prompt --client claude-code" {
		t.Fatalf("UserPromptSubmit command = %v", got)
	}
}

func TestInstallHooksCodexLacksNotification(t *testing.T) {
	if len(codexEvents) != 10 {
		t.Fatalf("codex events = %d, want 10", len(codexEvents))
	}
	if _, ok := codexEvents["Notification"]; ok {
		t.Fatal("codex must not register Notification")
	}
	path := filepath.Join(t.TempDir(), "hooks.json")
	if err := installHooks(codexEvents, path, "/opt/bin/memoria", "codex"); err != nil {
		t.Fatal(err)
	}
	s := readSettings(t, path)
	hooks, _ := s["hooks"].(map[string]any)
	if _, ok := hooks["Notification"]; ok {
		t.Fatal("Notification installed for codex")
	}
}

func TestInstallHooksMergePreservesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	existing := `{
		"model": "opus",
		"hooks": {
			"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "other-tool --check"}]}]
		}
	}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installHooks(claudeEvents, path, "/opt/bin/memoria", "claude-code"); err != nil {
		t.Fatal(err)
	}
	s := readSettings(t, path)
	if s["model"] != "opus" {
		t.Fatal("unrelated settings key lost")
	}
	cmds := hookCommands(t, s, "PreToolUse")
	if len(cmds) != 2 {
		t.Fatalf("PreToolUse commands = %v, want foreign + memoria", cmds)
	}
	if cmds[0] != "other-tool --check" {
		t.Fatal("foreign hook lost or reordered")
	}
}

func TestInstallHooksIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := installHooks(claudeEvents, path, "/opt/bin/memoria", "claude-code"); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	if err := installHooks(claudeEvents, path, "/opt/bin/memoria", "claude-code"); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Fatal("second install changed file")
	}
}

func TestInstallHooksReplacesOldStyleCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	existing := `{
		"hooks": {
			"SessionStart": [{"hooks": [{"type": "command", "command": "/old/path/memoria hook session-start"}]}]
		}
	}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installHooks(claudeEvents, path, "/opt/bin/memoria", "claude-code"); err != nil {
		t.Fatal(err)
	}
	cmds := hookCommands(t, readSettings(t, path), "SessionStart")
	if len(cmds) != 1 || cmds[0] != "/opt/bin/memoria hook session-start --client claude-code" {
		t.Fatalf("old-style command not replaced: %v", cmds)
	}
}

func allowRules(t *testing.T, settings map[string]any) []string {
	t.Helper()
	perms, _ := settings["permissions"].(map[string]any)
	entries, _ := perms["allow"].([]any)
	var rules []string
	for _, e := range entries {
		if s, ok := e.(string); ok {
			rules = append(rules, s)
		}
	}
	return rules
}

func TestInstallTrustFresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := installTrust(path); err != nil {
		t.Fatal(err)
	}
	if rules := allowRules(t, readSettings(t, path)); len(rules) != 1 || rules[0] != "mcp__memoria" {
		t.Fatalf("allow = %v, want [mcp__memoria]", rules)
	}
}

func TestInstallTrustPreservesAndDedupes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	existing := `{"model":"opus","permissions":{"allow":["Bash(ls:*)"],"deny":["WebFetch"]}}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	for range 2 { // idempotent on re-runs
		if err := installTrust(path); err != nil {
			t.Fatal(err)
		}
	}
	settings := readSettings(t, path)
	rules := allowRules(t, settings)
	if len(rules) != 2 || rules[0] != "Bash(ls:*)" || rules[1] != "mcp__memoria" {
		t.Fatalf("allow = %v, want existing rule kept + mcp__memoria once", rules)
	}
	if settings["model"] != "opus" {
		t.Fatalf("unrelated keys must survive: %v", settings)
	}
	perms, _ := settings["permissions"].(map[string]any)
	if deny, _ := perms["deny"].([]any); len(deny) != 1 {
		t.Fatalf("deny list must survive: %v", perms)
	}
}
