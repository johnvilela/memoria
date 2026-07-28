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
func installHooks(events map[string]string, settingsPath, binPath string) error {
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
		// ponytail: idempotency by " hook <name>" suffix — re-running after the
		// binary moves won't update the old path; re-point by editing settings
		if hasMemoriaHook(entries, name) {
			continue
		}
		entries = append(entries, map[string]any{
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": binPath + " hook " + name,
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

func hasMemoriaHook(entries []any, name string) bool {
	for _, e := range entries {
		em, _ := e.(map[string]any)
		inner, _ := em["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if cmd, _ := hm["command"].(string); strings.HasSuffix(cmd, " hook "+name) {
				return true
			}
		}
	}
	return false
}
