package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// canonical memoria hook names, passed as `memoria hook <name>`
var canonicalHooks = []string{
	"session-start", "user-prompt", "pre-tool-use", "post-tool-use",
	"pre-compact", "post-compact", "notification", "stop", "session-end",
	"subagent-start", "subagent-stop",
}

// agent event name → canonical memoria name
var claudeEvents = map[string]string{
	"SessionStart":     "session-start",
	"UserPromptSubmit": "user-prompt",
	"PreToolUse":       "pre-tool-use",
	"PostToolUse":      "post-tool-use",
	"PreCompact":       "pre-compact",
	"PostCompact":      "post-compact",
	"Notification":     "notification",
	"Stop":             "stop",
	"SessionEnd":       "session-end",
	"SubagentStart":    "subagent-start",
	"SubagentStop":     "subagent-stop",
}

// Codex has no Notification event; everything else matches Claude Code.
var codexEvents = func() map[string]string {
	m := map[string]string{}
	for k, v := range claudeEvents {
		if k != "Notification" {
			m[k] = v
		}
	}
	return m
}()

// installHooks merges memoria command hooks into an agent settings file
// (~/.claude/settings.json or ~/.codex/hooks.json — same JSON shape),
// preserving every existing key and hook. Idempotent.
func installHooks(events map[string]string, settingsPath, binPath, client string) error {
	raw := map[string]any{}
	b, err := os.ReadFile(settingsPath)
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &raw); err != nil {
			return fmt.Errorf("parse %s: %w", settingsPath, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return err
	}

	hooks, _ := raw["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for event, name := range events {
		entries, _ := hooks[event].([]any)
		cmd := binPath + " hook " + name + " --client " + client
		// an existing memoria hook is re-pointed in place: covers moved
		// binaries and pre---client installs without duplicating entries
		if hm := findMemoriaHook(entries, name); hm != nil {
			hm["command"] = cmd
			hooks[event] = entries
			continue
		}
		entries = append(entries, map[string]any{
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": cmd,
			}},
		})
		hooks[event] = entries
	}
	raw["hooks"] = hooks

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(settingsPath, append(out, '\n'), 0o644)
}

// detectClients returns agents whose settings file already carries memoria
// hooks — backfills the config's clients list for pre-feature installs.
// ponytail: substring check, not JSON parse — mirrors findMemoriaHook matching.
func detectClients(home string) []string {
	var found []string
	for _, c := range []struct{ client, rel string }{
		{"claude-code", filepath.Join(".claude", "settings.json")},
		{"codex", filepath.Join(".codex", "hooks.json")},
	} {
		b, err := os.ReadFile(filepath.Join(home, c.rel))
		if err == nil && strings.Contains(string(b), " hook session-start") {
			found = append(found, c.client)
		}
	}
	return found
}

// findMemoriaHook returns the hook map whose command runs `hook <name>`,
// matching both the old bare form and the current --client form.
func findMemoriaHook(entries []any, name string) map[string]any {
	for _, e := range entries {
		em, _ := e.(map[string]any)
		inner, _ := em["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			cmd, _ := hm["command"].(string)
			if strings.HasSuffix(cmd, " hook "+name) || strings.Contains(cmd, " hook "+name+" ") {
				return hm
			}
		}
	}
	return nil
}
