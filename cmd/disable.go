package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// runDisable removes every memoria capture hook and the memoria-owned
// scheduler. MCP registration and all captured/curated data stay available.
func runDisable(configPath string, out io.Writer) int {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}

	paths := []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(home, ".codex", "hooks.json"),
	}
	removed := 0
	hooksOK := true
	for _, path := range paths {
		n, err := uninstallHooks(path)
		if err != nil {
			fmt.Fprintf(out, "error: remove memoria hooks from %s: %v\n", path, err)
			hooksOK = false
			continue
		}
		removed += n
	}

	scheduleOK := true
	if err := removeSchedule(out); err != nil {
		fmt.Fprintln(out, "error: remove background schedule:", err)
		scheduleOK = false
	}

	cfg, err := loadConfig(configPath)
	configExists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if configExists {
		if hooksOK {
			cfg.Clients = nil
		}
		if scheduleOK {
			cfg.Cron, cfg.CronApply = "", false
		}
		if err := saveConfig(configPath, cfg); err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
	}

	if !hooksOK || !scheduleOK {
		return 1
	}
	fmt.Fprintf(out, "Memoria capture disabled: removed %d hooks and stopped background scheduling.\n", removed)
	fmt.Fprintln(out, "MCP access, project registry, session data, and wiki data were left untouched.")
	return 0
}

// uninstallHooks removes only memoria hook commands from a shared agent JSON
// file. Foreign hooks and unrelated settings remain byte-for-byte equivalent
// after JSON reformatting. Missing files are already disabled.
func uninstallHooks(path string) (int, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return 0, fmt.Errorf("parse %s: %w", path, err)
	}
	hooks, _ := raw["hooks"].(map[string]any)
	if hooks == nil {
		return 0, nil
	}

	removed := 0
	for event, value := range hooks {
		entries, ok := value.([]any)
		if !ok {
			continue
		}
		keptEntries := make([]any, 0, len(entries))
		for _, entry := range entries {
			em, ok := entry.(map[string]any)
			if !ok {
				keptEntries = append(keptEntries, entry)
				continue
			}
			inner, ok := em["hooks"].([]any)
			if !ok {
				keptEntries = append(keptEntries, entry)
				continue
			}
			keptHooks := make([]any, 0, len(inner))
			for _, hook := range inner {
				hm, _ := hook.(map[string]any)
				command, _ := hm["command"].(string)
				if isMemoriaHookCommand(command) {
					removed++
					continue
				}
				keptHooks = append(keptHooks, hook)
			}
			if len(keptHooks) > 0 {
				em["hooks"] = keptHooks
				keptEntries = append(keptEntries, entry)
			}
		}
		if len(keptEntries) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = keptEntries
		}
	}
	if removed == 0 {
		return 0, nil
	}
	if len(hooks) == 0 {
		delete(raw, "hooks")
	} else {
		raw["hooks"] = hooks
	}
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return 0, err
	}
	return removed, nil
}

func isMemoriaHookCommand(command string) bool {
	for _, name := range canonicalHooks {
		marker := " hook " + name
		i := strings.Index(command, marker)
		if i < 0 {
			continue
		}
		binary := strings.Trim(strings.TrimSpace(command[:i]), "\"'")
		base := strings.ToLower(filepath.Base(binary))
		if base == "memoria" || base == "memoria.exe" {
			return true
		}
	}
	return false
}
