package main

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// runList prints the registered projects with their wiki folder and whether
// the registered path still exists — a renamed or deleted project shows as
// missing and can be dropped with memoria remove.
func runList(configPath string, out io.Writer) int {
	cfg, err := loadConfig(configPath)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if len(cfg.Projects) == 0 {
		fmt.Fprintln(out, "No projects registered.")
		return 0
	}
	t := table.New().
		Border(lipgloss.Border{}).
		BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
		BorderHeader(false).BorderColumn(false).BorderRow(false).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return tuiFaint.PaddingRight(2)
			}
			return lipgloss.NewStyle().PaddingRight(2)
		}).
		Headers("PROJECT", "WIKI", "STATUS", "PATH")
	stale := false
	for _, p := range cfg.Projects {
		wiki := p.Wiki
		if wiki == "" {
			wiki = "wiki"
		}
		state := statusOK.Render("● ok")
		if fi, err := os.Stat(p.Path); err != nil || !fi.IsDir() {
			state = statusErr.Render("✗ missing")
			stale = true
		}
		t.Row(p.Name, wiki, state, p.Path)
	}
	fmt.Fprintln(out, t)
	if stale {
		fmt.Fprintln(out, "Stale entries can be dropped with memoria remove.")
	}
	if cfg.Global {
		fmt.Fprintf(out, "Global capture: on (%s)\n", globalRoot(cfg, configPath))
	}
	return 0
}
