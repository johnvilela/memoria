package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func runInit(args []string, out io.Writer) int {
	usage := func() { fmt.Fprintln(out, "usage: memoria init <claude-code|codex>") }
	if len(args) != 1 {
		usage()
		return 1
	}

	var (
		events map[string]string
		label  string
		rel    []string
		note   string
	)
	switch args[0] {
	case "claude", "claude-code":
		events, label = claudeEvents, "Claude Code"
		rel = []string{".claude", "settings.json"}
	case "codex":
		events, label = codexEvents, "Codex"
		rel = []string{".codex", "hooks.json"}
		note = "Note: run /hooks inside Codex once to trust the new hooks. Codex has no Notification event; that hook was skipped."
	default:
		fmt.Fprintf(out, "unknown agent: %q\n", args[0])
		usage()
		return 1
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	bin, err := os.Executable()
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	settingsPath := filepath.Join(append([]string{home}, rel...)...)
	if err := installHooks(events, settingsPath, bin); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	fmt.Fprintf(out, "Installed %d %s hooks in %s\n", len(events), label, settingsPath)
	fmt.Fprintf(out, "Tracked projects are read from %s — add this project's path there to start capturing.\n", defaultConfigPath())
	if note != "" {
		fmt.Fprintln(out, note)
	}
	return 0
}
