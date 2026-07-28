package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// runSetup reconfigures an existing install: processor, notifications, cron.
// Hooks stay init-only.
func runSetup(args []string, configPath string, out io.Writer) int {
	usage := func() {
		fmt.Fprintln(out, "usage: memoria setup [--processor claude-code|codex|ollama|gemini] [--notification] [--cron <expr|preset|off>] [--cron-apply]")
	}
	args = normalizeCronArgs(args)
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(out)
	processor := fs.String("processor", "", "AI provider that processes sessions")
	notification := fs.Bool("notification", false, "desktop notification when background processing finishes")
	cron := fs.String("cron", "", "schedule for background processing (cron expression, preset, or off)")
	cronApply := fs.Bool("cron-apply", false, "scheduled runs apply proposals without review")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	var notifSet, cronSet, cronApplySet bool
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "notification":
			notifSet = true
		case "cron":
			cronSet = true
		case "cron-apply":
			cronApplySet = true
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

	// any flag given = change exactly that, keep the rest — no prompts
	anySet := *processor != "" || notifSet || cronSet || cronApplySet
	if !anySet {
		if !isTTY() {
			usage()
			return 1
		}
		aborted := func() int { fmt.Fprintln(out, "aborted"); return 1 }
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

	if *processor != "" || notifSet {
		if code := saveInitConfig(*processor, *notification, notifSet, configPath, out); code != 0 {
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
