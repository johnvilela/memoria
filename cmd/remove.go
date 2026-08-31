package main

import (
	"fmt"
	"io"
	"os"
	"slices"
)

// runRemove unregisters a project picked interactively: drops its config
// entry plus its pending.yaml key, status.yaml entry and run log. Files in
// the project folder (wiki, .memoria/, AGENTS.md) are never touched, so
// re-running bootstrap there fully restores the registration — the esc-able
// picker is confirmation enough, no extra prompt.
func runRemove(configPath string, out io.Writer) int {
	cfg, err := loadConfig(configPath)
	if err != nil && !os.IsNotExist(err) {
		// never rewrite a config we couldn't parse
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if len(cfg.Projects) == 0 {
		fmt.Fprintln(out, "no registered projects")
		return 1
	}
	if !isTTY() {
		fmt.Fprintln(out, "error: memoria remove is interactive — run it in a terminal")
		return 1
	}

	// value is the path: the registry's unique key — names can collide
	opts := make([]option, len(cfg.Projects))
	for i, p := range cfg.Projects {
		desc := p.Path
		if fi, err := os.Stat(p.Path); err != nil || !fi.IsDir() {
			desc += " (missing)"
		}
		opts[i] = option{value: p.Path, label: p.Name, desc: desc}
	}
	sel, err := selectOption("Remove which project from memoria? (project files are not touched)", opts)
	if err != nil {
		return 0 // esc: nothing removed
	}

	idx := slices.IndexFunc(cfg.Projects, func(p project) bool { return p.Path == sel })
	if idx < 0 {
		fmt.Fprintln(out, "error: no project at", sel)
		return 1
	}
	p := cfg.Projects[idx]
	if st, _ := loadStatus(statusPath(configPath)); st[p.Name].State == "running" && pidAlive(st[p.Name].PID) {
		fmt.Fprintf(out, "error: processing running for %s (pid %d) — wait for it to finish\n", p.Name, st[p.Name].PID)
		return 1
	}

	cfg.Projects = slices.Delete(cfg.Projects, idx, idx+1)
	if err := saveConfig(configPath, cfg); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}

	// sidecar state is keyed by name — a remaining project can share it
	// (e.g. a moved folder registered twice), so only clean orphaned keys
	shared := false
	for _, q := range cfg.Projects {
		if q.Name == p.Name {
			shared = true
		}
	}
	if !shared {
		if err := queueDropProject(queuePath(configPath), p.Name); err != nil {
			fmt.Fprintln(out, "warning:", err)
		}
		if err := statusDelete(statusPath(configPath), p.Name); err != nil {
			fmt.Fprintln(out, "warning:", err)
		}
		if err := os.Remove(runLogPath(configPath, p.Name)); err != nil && !os.IsNotExist(err) {
			fmt.Fprintln(out, "warning:", err)
		}
	}

	fmt.Fprintf(out, "Removed %s (%s)\n", p.Name, p.Path)
	fmt.Fprintln(out, "The wiki and .memoria/ folders in the project were left untouched — re-run memoria bootstrap there to re-register.")
	return 0
}
