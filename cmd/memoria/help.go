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
	{name: "init", desc: "Install code-agent hooks and choose the session processor (--client, --processor, --notification)"},
	{name: "process", desc: "Consolidate pending sessions into the project wiki in the background (review, then --apply)"},
	{name: "lint", desc: "Audit the wiki for contradictions in the background (--review, --apply a fix, --deny \"why\")"},
	{name: "status", desc: "Show background processing state per project"},
	{name: "bootstrap", desc: "Register current folder as a tracked project and seed the wiki from git history (--wiki <name>, --background)"},
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
