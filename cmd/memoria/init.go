package main

import (
	"encoding/json"
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

var notificationOptions = []option{
	{"disabled", "Disabled", "default — check memoria status manually"},
	{"enabled", "Enabled", "notify-send when the proposal is ready or processing fails"},
}

func runInit(args []string, configPath string, out io.Writer) int {
	usage := func() {
		fmt.Fprintln(out, "usage: memoria init [<client>] [--client claude-code|codex] [--processor claude-code|codex|ollama|gemini] [--notification]")
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
	notification := fs.Bool("notification", false, "desktop notification when background processing finishes")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	// --notification=false must differ from an omitted flag
	notifSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "notification" {
			notifSet = true
		}
	})
	if fs.NArg() > 0 || (client != "" && *clientFlag != "") {
		usage()
		return 1
	}
	if *clientFlag != "" {
		client = *clientFlag
	}
	if *processor != "" {
		if _, known := processorBins[*processor]; !known {
			fmt.Fprintf(out, "unknown processor: %q\n", *processor)
			return 1
		}
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
	ensureGitignore(out)

	if *processor == "" && isTTY() {
		v, err := selectOption("Which provider should process sessions into wiki/memories?", processorOptions)
		if err != nil {
			fmt.Fprintln(out, "aborted")
			return 1
		}
		*processor = v
	}
	if !notifSet && isTTY() {
		v, err := selectOption("Desktop notification when background processing finishes?", notificationOptions)
		if err != nil {
			fmt.Fprintln(out, "aborted")
			return 1
		}
		*notification, notifSet = v == "enabled", true
	}
	if code := saveInitConfig(*processor, *notification, notifSet, configPath, out); code != 0 {
		return code
	}
	maybeSeedWiki(configPath, out)
	return 0
}

// ensureGitignore adds .memoria/ to cwd's .gitignore (creating it if missing)
// when cwd is a git repo. Best-effort: init never fails over it.
func ensureGitignore(out io.Writer) {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	if _, err := os.Stat(filepath.Join(cwd, ".git")); err != nil {
		return
	}
	if err := addGitignoreEntry(cwd); err != nil {
		fmt.Fprintln(out, "warning: could not update .gitignore:", err)
	}
}

var seedOptions = []option{
	{"no", "No", "start with an empty wiki"},
	{"yes", "Yes", "may take a while — runs in this terminal, don't close it"},
}

// maybeSeedWiki offers to bootstrap the wiki from git history + code. Only in
// a TTY, only with a processor configured, only when the repo has commits.
// Best-effort: a refusal or failure never fails init.
func maybeSeedWiki(configPath string, out io.Writer) {
	if !isTTY() {
		return
	}
	cwd, err := os.Getwd()
	if err != nil || !hasCommits(cwd) {
		return
	}
	cfg, err := loadConfig(configPath)
	if err != nil || cfg.Processor == "" {
		return
	}
	v, err := selectOption("Auto-generate the wiki from the existing git history and code?", seedOptions)
	if err != nil || v != "yes" {
		return
	}
	wikiName := projectAt(cfg, matchProject(cwd, cfg.Projects)).Wiki
	if wikiName == "" {
		wikiName = "wiki"
	}
	err = withSpinner("Generating wiki from git history and code (this can take a few minutes)...", func() error {
		return seedWiki(cfg, cwd, filepath.Join(cwd, wikiName), configPath, out)
	})
	if err != nil {
		fmt.Fprintln(out, "error: wiki generation failed:", err)
	}
}

func hasCommits(dir string) bool {
	return exec.Command("git", "-C", dir, "rev-parse", "--verify", "HEAD").Run() == nil
}

// seedWiki asks the processor for wiki pages built from the repo's git log,
// file tree and README, then validates and writes them (same trust boundary
// as process --apply).
func seedWiki(cfg config, dir, wikiRoot, configPath string, out io.Writer) error {
	rules, err := loadWikiPrompt(configPath)
	if err != nil {
		return err
	}
	raw, err := invokeProcessor(cfg, buildSeedPrompt(rules, dir))
	if err != nil {
		return err
	}
	jsonStr, err := extractJSON(raw)
	if err != nil {
		return err
	}
	var pp struct {
		Pages []wikiPage `json:"pages"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &pp); err != nil {
		return fmt.Errorf("processor returned invalid JSON: %w", err)
	}
	if err := validatePages(pp.Pages); err != nil {
		return err
	}
	for _, pg := range pp.Pages {
		dst := filepath.Join(wikiRoot, filepath.FromSlash(pg.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, []byte(pg.Content), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "wrote %s\n", dst)
	}
	fmt.Fprintf(out, "Wiki seeded with %d page(s) in %s\n", len(pp.Pages), wikiRoot)
	return nil
}

// ponytail: context = git log + file tree + README; deep per-file code
// reading when this proves too shallow.
func buildSeedPrompt(rules, dir string) string {
	git := func(args ...string) string {
		out, _ := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
		return string(out)
	}
	var b strings.Builder
	b.WriteString(rules)
	b.WriteString("\n\nThe wiki is empty. Seed it from this project's git history and code:\n")
	b.WriteString("\n--- GIT LOG (newest first) ---\n" + git("log", "--oneline", "-n", "300"))
	b.WriteString("\n--- FILES ---\n" + git("ls-files"))
	for _, n := range []string{"README.md", "README"} {
		if rb, err := os.ReadFile(filepath.Join(dir, n)); err == nil {
			b.WriteString("\n--- " + n + " ---\n" + string(rb) + "\n")
			break
		}
	}
	b.WriteString("\n--- OUTPUT FORMAT ---\n" + jsonContract + "\n")
	return b.String()
}

// saveInitConfig persists every init choice in a single config write, then
// runs the warn-only verifications. Nothing chosen → config untouched.
func saveInitConfig(proc string, notifEnabled, notifSet bool, configPath string, out io.Writer) int {
	if proc == "" {
		fmt.Fprintln(out, "No processor configured — rerun with --processor <claude-code|codex|ollama|gemini> to set one.")
		if !notifSet {
			return 0
		}
	}
	cfg, err := loadConfig(configPath)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if proc != "" {
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
	}
	if notifSet {
		cfg.Notifications = notifEnabled
	}
	if err := saveConfig(configPath, cfg); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}

	// warn-only verification — the choices are saved regardless
	if proc != "" {
		fmt.Fprintf(out, "Processor set to %s in %s\n", proc, configPath)
		if proc == "gemini" {
			if err := checkGeminiKey(cfg.GeminiAPIKey); err != nil {
				fmt.Fprintln(out, "warning: gemini key check failed:", err)
			}
		} else if _, err := exec.LookPath(processorBins[proc]); err != nil {
			fmt.Fprintf(out, "warning: %s not found on PATH\n", processorBins[proc])
		}
		if proc == "ollama" {
			fmt.Fprintln(out, "ollama auto-install coming soon")
		}
	}
	if notifSet {
		state := "disabled"
		if notifEnabled {
			state = "enabled"
		}
		fmt.Fprintf(out, "Notifications %s in %s\n", state, configPath)
		if notifEnabled {
			if _, err := exec.LookPath("notify-send"); err != nil {
				fmt.Fprintln(out, "warning: notify-send not found on PATH")
			}
		}
	}
	return 0
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
