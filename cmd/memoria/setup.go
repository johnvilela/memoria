package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

// runSetup reconfigures an existing install: processor, notifications, cron,
// and adds capture hooks for more agents without touching existing ones.
func runSetup(args []string, configPath string, out io.Writer) int {
	usage := func() {
		fmt.Fprintln(out, "usage: memoria setup [--client claude-code,codex] [--processor claude-code|codex|ollama|gemini] [--notification] [--auto-apply] [--auto-commit] [--cron <expr|preset|off>] [--cron-apply] [--global] [--global-path <folder>]")
	}
	args = normalizeCronArgs(args)
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(out)
	clientFlag := fs.String("client", "", "agents to add capture hooks for (comma-separated)")
	processor := fs.String("processor", "", "AI provider that processes sessions")
	notification := fs.Bool("notification", false, "desktop notification when background processing finishes")
	autoApply := fs.Bool("auto-apply", false, "autopilot: session end consolidates and applies without review")
	autoCommit := fs.Bool("auto-commit", false, "commit the wiki after every applied change")
	cron := fs.String("cron", "", "schedule for background processing (cron expression, preset, or off)")
	cronApply := fs.Bool("cron-apply", false, "scheduled runs apply proposals without review")
	global := fs.Bool("global", false, "capture sessions in unregistered folders (--global=false disables)")
	gpath := fs.String("global-path", "", "move the global capture root (empty = back to the config folder)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	var notifSet, autoSet, commitSet, cronSet, cronApplySet, globalSet, pathSet bool
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "notification":
			notifSet = true
		case "auto-apply":
			autoSet = true
		case "auto-commit":
			commitSet = true
		case "cron":
			cronSet = true
		case "cron-apply":
			cronApplySet = true
		case "global":
			globalSet = true
		case "global-path":
			pathSet = true
		}
	})
	if fs.NArg() > 0 {
		usage()
		return 1
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(out, "error: no config found — run memoria init first")
		} else {
			fmt.Fprintln(out, "error:", err)
		}
		return 1
	}
	if *processor != "" {
		if _, known := processorBins[*processor]; !known {
			fmt.Fprintf(out, "unknown processor: %q\n", *processor)
			return 1
		}
	}

	var clients []string
	if *clientFlag != "" {
		if clients = splitClients(*clientFlag); len(clients) == 0 {
			usage()
			return 1
		}
	}

	// any flag given = change exactly that, keep the rest — no prompts
	anySet := *clientFlag != "" || *processor != "" || notifSet || autoSet || commitSet || cronSet || cronApplySet || globalSet || pathSet
	if !anySet {
		if !isTTY() {
			usage()
			return 1
		}
		aborted := func() int { fmt.Fprintln(out, "aborted"); return 1 }
		home, _ := os.UserHomeDir()
		installed := recordClients(configPath, out, detectClients(home)...)
		list := "none"
		if len(installed) > 0 {
			list = strings.Join(installed, ", ")
		}
		fmt.Fprintln(out, "Capture hooks installed for:", list)
		var missing []option
		for _, o := range clientOptions {
			if !slices.Contains(installed, o.value) {
				missing = append(missing, o)
			}
		}
		if len(missing) > 0 {
			v, err := selectMulti("Add capture hooks for more agents?", missing)
			if err != nil {
				return aborted()
			}
			clients = v
		}
		if *processor == "" {
			cur := cfg.Processor
			if cur == "" {
				cur = "none"
			}
			opts := append([]option{{"keep", "Keep current (" + cur + ")", ""}}, processorOptions...)
			v, err := selectOption("Which provider should process sessions into wiki/memories?", opts)
			if err != nil {
				return aborted()
			}
			if v != "keep" {
				*processor = v
			}
		}
		if !notifSet {
			cur := "disabled"
			if cfg.Notifications {
				cur = "enabled"
			}
			opts := append([]option{{"keep", "Keep current (" + cur + ")", ""}}, notificationOptions...)
			v, err := selectOption("Desktop notification when background processing finishes?", opts)
			if err != nil {
				return aborted()
			}
			if v != "keep" {
				*notification, notifSet = v == "enabled", true
			}
		}
		if !autoSet {
			cur := "disabled"
			if cfg.AutoApply {
				cur = "enabled"
			}
			opts := append([]option{{"keep", "Keep current (" + cur + ")", ""}}, autoApplyOptions...)
			v, err := selectOption("Auto-apply: consolidate and write the wiki on session end, without review?", opts)
			if err != nil {
				return aborted()
			}
			if v != "keep" {
				*autoApply, autoSet = v == "enabled", true
			}
		}
		if !commitSet {
			cur := "disabled"
			if cfg.WikiAutoCommit {
				cur = "enabled"
			}
			opts := append([]option{{"keep", "Keep current (" + cur + ")", ""}}, autoCommitOptions...)
			v, err := selectOption("Auto-commit: commit the wiki after every applied change?", opts)
			if err != nil {
				return aborted()
			}
			if v != "keep" {
				*autoCommit, commitSet = v == "enabled", true
			}
		}
		if !cronSet {
			cur := cfg.Cron
			if cur == "" {
				cur = "disabled"
			}
			spec, applySel, chosen, err := promptCron("Keep current (" + cur + ")")
			if err != nil {
				return aborted()
			}
			if chosen {
				*cron, cronSet = spec, true
				*cronApply, cronApplySet = applySel, true
			}
		}
	}

	if len(clients) > 0 {
		if code := installClients(clients, configPath, out, usage); code != 0 {
			return code
		}
	}
	if *processor != "" || notifSet || autoSet || commitSet {
		if code := saveInitConfig(*processor, *notification, notifSet, *autoApply, autoSet, *autoCommit, commitSet, configPath, out); code != 0 {
			return code
		}
	}
	if globalSet || pathSet {
		if code := applyGlobalSetting(*global, globalSet, *gpath, pathSet, configPath, out); code != 0 {
			return code
		}
	}
	if cronSet || cronApplySet {
		spec := ""
		if cronSet {
			spec = *cron
		}
		return applyCronSetting(spec, *cronApply, cronApplySet, configPath, out)
	}
	return 0
}
