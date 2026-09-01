package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const asciiArt = `
 ███╗   ███╗███████╗███╗   ███╗ ██████╗ ██████╗ ██╗ █████╗
 ████╗ ████║██╔════╝████╗ ████║██╔═══██╗██╔══██╗██║██╔══██╗
 ██╔████╔██║█████╗  ██╔████╔██║██║   ██║██████╔╝██║███████║
 ██║╚██╔╝██║██╔══╝  ██║╚██╔╝██║██║   ██║██╔══██╗██║██╔══██║
 ██║ ╚═╝ ██║███████╗██║ ╚═╝ ██║╚██████╔╝██║  ██║██║██║  ██║
 ╚═╝     ╚═╝╚══════╝╚═╝     ╚═╝ ╚═════╝ ╚═╝  ╚═╝╚═╝╚═╝  ╚═╝`

type command struct {
	name, desc string
	internal   bool
}

var commands = []command{
	{name: "help", desc: "Show this help screen"},
	{name: "version", desc: "Print the memoria version"},
	{name: "init", desc: "Install hooks into one or more code agents and choose the session processor (--client a,b, --processor, --notification, --auto-apply, --auto-commit, --cron)"},
	{name: "setup", desc: "Reconfigure processor, notifications, auto-apply, auto-commit, schedule, global capture, or add hooks to more agents (--client a,b, --processor, --notification, --auto-apply, --auto-commit, --cron [expr|off], --cron-apply, --global[=false], --global-path <folder>)"},
	{name: "process", desc: "Consolidate pending sessions into the project wiki in the background (review, then --apply; --inspect follows a running job; --all sweeps every project)"},
	{name: "lint", desc: "Audit the wiki for contradictions in the background (--review, --apply a fix, --deny \"why\")"},
	{name: "run", desc: "Launch a code agent here, continuing a previous session (--new, --session <id|name>; no flags → pick from the last 5)"},
	{name: "search", desc: "Find wiki pages by text or #tag, pick one to read (@project/@all searches other projects, --trash includes deleted pages)"},
	{name: "finalize", desc: "End the current session now and write its wiki pages — flush before a PR so they ride the same branch (--no-apply reviews first; optional session id)"},
	{name: "commit", desc: "Commit the wiki folder's changes — new and modified pages (-m overrides the subject)"},
	{name: "mcp", desc: "Serve the memoria tools to code agents over stdio", internal: true},
	{name: "status", desc: "Show background processing state per project"},
	{name: "list", desc: "List registered projects — name, wiki folder, and whether the path still exists"},
	{name: "bootstrap", desc: "Register current folder as a tracked project and seed the wiki from git history (--wiki <name>, --background); an existing wiki folder is adopted as-is; --global [--global-path <folder>] captures unregistered folders too"},
	{name: "remove", desc: "Pick a registered project and remove it from memoria (config and pending/status state; project files untouched)"},
	{name: "update", desc: "Check GitHub for a newer release and replace this binary (-y installs without asking)"},
	{name: "digest", desc: "Compile one session's digest into its wiki session page", internal: true},
	{name: "hook", desc: "Receive hook data from a code agent", internal: true},
}

func renderHelp() string {
	var (
		tag = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
		cmd = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Width(12)
	)

	// ponytail: art printed raw — lipgloss pads multi-line blocks and mangles box-drawing alignment
	var b strings.Builder
	b.WriteString(asciiArt)
	b.WriteString("\n\n")
	b.WriteString(tag.Render(" AI long-term memory and wiki per project, built from code-agent chats."))
	b.WriteString("\n\n Usage: memoria <command>\n\n Commands:\n")
	for _, c := range commands {
		mark := ""
		if c.internal {
			mark = tag.Render(" (internal)")
		}
		fmt.Fprintf(&b, "   %s%s%s\n", cmd.Render(c.name), c.desc, mark)
	}
	return b.String()
}
