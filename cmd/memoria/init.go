package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var clientOptions = []option{
	{"claude-code", "Claude Code", "hooks into ~/.claude/settings.json"},
	{"codex", "Codex", "hooks into ~/.codex/hooks.json"},
}

var processorOptions = []option{
	{"claude-code", "Claude Code", "default — uses the claude CLI"},
	{"codex", "Codex", "uses the codex CLI"},
	{"ollama", "Ollama", "local model, auto-install coming soon"},
	{"gemini", "Gemini", "Google API, needs an API key"},
}

// processor value → CLI binary to look for ("" = no binary, verified another way)
var processorBins = map[string]string{
	"claude-code": "claude", "codex": "codex", "ollama": "ollama", "gemini": "",
}

func runInit(args []string, configPath string, out io.Writer) int {
	usage := func() {
		fmt.Fprintln(out, "usage: memoria init [<client>] [--client claude-code|codex] [--processor claude-code|codex|ollama|gemini]")
	}
	// positional client only as the first arg, so flags after it still parse
	client := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		client, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(out)
	clientFlag := fs.String("client", "", "agent to install capture hooks for")
	processor := fs.String("processor", "", "AI provider that processes sessions")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() > 0 || (client != "" && *clientFlag != "") {
		usage()
		return 1
	}
	if *clientFlag != "" {
		client = *clientFlag
	}
	if client == "" {
		if !isTTY() {
			usage()
			return 1
		}
		v, err := selectOption("Install capture hooks for which agent?", clientOptions)
		if err != nil {
			fmt.Fprintln(out, "aborted")
			return 1
		}
		client = v
	}
	if code := installClientHooks(client, out, usage); code != 0 {
		return code
	}

	if *processor == "" {
		if !isTTY() {
			fmt.Fprintln(out, "No processor configured — rerun with --processor <claude-code|codex|ollama|gemini> to set one.")
			return 0
		}
		v, err := selectOption("Which provider should process sessions into wiki/memories?", processorOptions)
		if err != nil {
			fmt.Fprintln(out, "aborted")
			return 1
		}
		*processor = v
	}
	return configureProcessor(*processor, configPath, out)
}

// installClientHooks wires memoria into the chosen agent's global settings.
func installClientHooks(client string, out io.Writer, usage func()) int {
	var (
		events map[string]string
		label  string
		rel    []string
		note   string
	)
	switch client {
	case "claude", "claude-code":
		events, label = claudeEvents, "Claude Code"
		rel = []string{".claude", "settings.json"}
	case "codex":
		events, label = codexEvents, "Codex"
		rel = []string{".codex", "hooks.json"}
		note = "Note: run /hooks inside Codex once to trust the new hooks. Codex has no Notification event; that hook was skipped."
	default:
		fmt.Fprintf(out, "unknown client: %q\n", client)
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
	fmt.Fprintf(out, "Tracked projects are read from %s — run memoria bootstrap inside a project to start capturing.\n", defaultConfigPath())
	if note != "" {
		fmt.Fprintln(out, note)
	}
	return 0
}

// configureProcessor persists the processor choice (and gemini key) to the
// config, then verifies it — warnings only, the choice is saved regardless.
func configureProcessor(proc, configPath string, out io.Writer) int {
	bin, known := processorBins[proc]
	if !known {
		fmt.Fprintf(out, "unknown processor: %q\n", proc)
		return 1
	}
	cfg, err := loadConfig(configPath)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	cfg.Processor = proc
	if proc == "gemini" {
		key := os.Getenv("GEMINI_API_KEY")
		if key == "" {
			key = cfg.GeminiAPIKey
		}
		if key == "" {
			if !isTTY() {
				fmt.Fprintln(out, "error: gemini needs an API key — set GEMINI_API_KEY or run memoria init interactively")
				return 1
			}
			if key, err = promptSecret("Gemini API key"); err != nil || key == "" {
				fmt.Fprintln(out, "aborted")
				return 1
			}
		}
		cfg.GeminiAPIKey = key
	}
	if err := saveConfig(configPath, cfg); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	fmt.Fprintf(out, "Processor set to %s in %s\n", proc, configPath)

	if proc == "gemini" {
		if err := checkGeminiKey(cfg.GeminiAPIKey); err != nil {
			fmt.Fprintln(out, "warning: gemini key check failed:", err)
		}
	} else if _, err := exec.LookPath(bin); err != nil {
		fmt.Fprintf(out, "warning: %s not found on PATH\n", bin)
	}
	if proc == "ollama" {
		fmt.Fprintln(out, "ollama auto-install coming soon")
	}
	return 0
}

// var so tests can point it at a httptest server
var geminiModelsURL = "https://generativelanguage.googleapis.com/v1beta/models"

func checkGeminiKey(key string) error {
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(geminiModelsURL + "?key=" + url.QueryEscape(key))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %s", resp.Status)
	}
	return nil
}
