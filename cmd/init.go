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
	"slices"
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

var autoApplyOptions = []option{
	{"disabled", "Disabled", "default — review proposals and lint fixes yourself"},
	{"enabled", "Enabled", "session end consolidates and writes the wiki automatically, lint auto-fixes"},
}

var autoCommitOptions = []option{
	{"disabled", "Disabled", "default — commit the wiki yourself with memoria commit"},
	{"enabled", "Enabled", "every applied wiki change is committed automatically"},
}

func runInit(args []string, configPath string, out io.Writer) int {
	usage := func() {
		fmt.Fprintln(out, "usage: memoria init [<client>...] [--client claude-code,codex] [--processor claude-code|codex|ollama|gemini] [--notification] [--auto-apply] [--auto-commit] [--cron [<expr|preset|off>]] [--cron-apply]")
	}
	args = normalizeCronArgs(args)
	// positional clients only as leading args, so flags after them still parse
	var clients []string
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		clients, args = append(clients, args[0]), args[1:]
	}
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(out)
	clientFlag := fs.String("client", "", "agents to install capture hooks for (comma-separated)")
	processor := fs.String("processor", "", "AI provider that processes sessions")
	notification := fs.Bool("notification", false, "desktop notification when background processing finishes")
	autoApply := fs.Bool("auto-apply", false, "autopilot: session end consolidates and applies without review")
	autoCommit := fs.Bool("auto-commit", false, "commit the wiki after every applied change")
	cron := fs.String("cron", "", "schedule for background processing (cron expression, preset, or off)")
	cronApply := fs.Bool("cron-apply", false, "scheduled runs apply proposals without review")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	// set flags must differ from omitted ones (--notification=false vs nothing)
	var notifSet, autoSet, commitSet, cronSet, cronApplySet bool
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
		}
	})
	if fs.NArg() > 0 || (len(clients) > 0 && *clientFlag != "") {
		usage()
		return 1
	}
	if *clientFlag != "" {
		if clients = splitClients(*clientFlag); len(clients) == 0 {
			usage()
			return 1
		}
	}
	if *processor != "" {
		if _, known := processorBins[*processor]; !known {
			fmt.Fprintf(out, "unknown processor: %q\n", *processor)
			return 1
		}
	}
	if len(clients) == 0 {
		if !isTTY() {
			usage()
			return 1
		}
		v, err := selectMulti("Install capture hooks for which agents?", clientOptions)
		if err != nil {
			fmt.Fprintln(out, "aborted")
			return 1
		}
		if len(v) == 0 {
			fmt.Fprintln(out, "no agents selected")
			return 1
		}
		clients = v
	}
	if code := installClients(clients, configPath, out, usage); code != 0 {
		return code
	}
	ensureGitignore(out)
	ensurePathEnv(out)

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
	if !autoSet && isTTY() {
		v, err := selectOption("Auto-apply: consolidate and write the wiki on session end, without review?", autoApplyOptions)
		if err != nil {
			fmt.Fprintln(out, "aborted")
			return 1
		}
		*autoApply, autoSet = v == "enabled", true
	}
	if !commitSet && isTTY() {
		v, err := selectOption("Auto-commit: commit the wiki after every applied change?", autoCommitOptions)
		if err != nil {
			fmt.Fprintln(out, "aborted")
			return 1
		}
		*autoCommit, commitSet = v == "enabled", true
	}
	if !cronSet && isTTY() {
		spec, applySel, chosen, err := promptCron("")
		if err != nil {
			fmt.Fprintln(out, "aborted")
			return 1
		}
		if chosen {
			*cron, cronSet = spec, true
			*cronApply, cronApplySet = applySel, true
		}
	}
	if code := saveInitConfig(*processor, *notification, notifSet, *autoApply, autoSet, *autoCommit, commitSet, configPath, out); code != 0 {
		return code
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

// ensurePathEnv puts the directory holding the running binary on PATH for
// future shells: when missing, appends the export line to the rc file of the
// user's shell ($SHELL). macOS zsh lacks ~/.local/bin by default, so a bare
// `memoria` in a new terminal fails right after install without this.
// Best-effort: init never fails over it.
func ensurePathEnv(out io.Writer) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	dir := filepath.Dir(exe)
	if slices.Contains(filepath.SplitList(os.Getenv("PATH")), dir) {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	rc, line := shellRC(os.Getenv("SHELL"), home, dir)
	data, _ := os.ReadFile(rc)
	if strings.Contains(string(data), line) {
		return
	}
	if err := os.MkdirAll(filepath.Dir(rc), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(rc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(out, "note: %s is not on your PATH and %s could not be updated: %v\n", dir, rc, err)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "\n%s\n", line)
	fmt.Fprintf(out, "added %s to PATH in %s — restart your shell to pick it up\n", dir, rc)
}

// shellRC picks the rc file and PATH line for a $SHELL value. dir under $HOME
// is written home-relative so the line survives a moved home directory.
// ponytail: bash on macOS reads .bash_profile, not .bashrc — macOS default is
// zsh, revisit if a bash-on-mac report shows up.
func shellRC(shell, home, dir string) (rc, line string) {
	if rel, err := filepath.Rel(home, dir); err == nil && !strings.HasPrefix(rel, "..") {
		dir = "$HOME/" + filepath.ToSlash(rel)
	}
	switch filepath.Base(shell) {
	case "fish":
		return filepath.Join(home, ".config", "fish", "config.fish"), fmt.Sprintf("fish_add_path %q", dir)
	case "zsh":
		return filepath.Join(home, ".zshrc"), fmt.Sprintf("export PATH=%q", dir+":$PATH")
	case "bash":
		return filepath.Join(home, ".bashrc"), fmt.Sprintf("export PATH=%q", dir+":$PATH")
	default:
		return filepath.Join(home, ".profile"), fmt.Sprintf("export PATH=%q", dir+":$PATH")
	}
}

// saveInitConfig persists every init choice in a single config write, then
// runs the warn-only verifications. Nothing chosen → config untouched.
func saveInitConfig(proc string, notifEnabled, notifSet, autoEnabled, autoSet, commitEnabled, commitSet bool, configPath string, out io.Writer) int {
	cfg, err := loadConfig(configPath)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if proc == "" {
		// hint only when nothing is configured yet — setup calls this too
		if cfg.Processor == "" {
			fmt.Fprintln(out, "No processor configured — rerun with --processor <claude-code|codex|ollama|gemini> to set one.")
		}
		if !notifSet && !autoSet && !commitSet {
			return 0
		}
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
	if autoSet {
		cfg.AutoApply = autoEnabled
	}
	if commitSet {
		cfg.WikiAutoCommit = commitEnabled
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
	if autoSet {
		state := "disabled"
		if autoEnabled {
			state = "enabled"
		}
		fmt.Fprintf(out, "Auto-apply %s in %s\n", state, configPath)
	}
	if commitSet {
		state := "disabled"
		if commitEnabled {
			state = "enabled"
		}
		fmt.Fprintf(out, "Wiki auto-commit %s in %s\n", state, configPath)
	}
	return 0
}

// splitClients parses a comma-separated --client value, dropping empties.
func splitClients(s string) []string {
	var names []string
	for _, n := range strings.Split(s, ",") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	return names
}

// normalizeClient maps accepted names/aliases to canonical; "" = unknown.
func normalizeClient(name string) string {
	switch name {
	case "claude", "claude-code":
		return "claude-code"
	case "codex":
		return "codex"
	}
	return ""
}

// installClients validates all names first (fail fast — a typo installs
// nothing), then installs hooks+MCP per agent and records them in the config.
func installClients(names []string, configPath string, out io.Writer, usage func()) int {
	var clients []string
	for _, n := range names {
		c := normalizeClient(n)
		if c == "" {
			fmt.Fprintf(out, "unknown client: %q\n", n)
			usage()
			return 1
		}
		if !slices.Contains(clients, c) {
			clients = append(clients, c)
		}
	}
	for _, c := range clients {
		if code := installClientHooks(c, out, usage); code != 0 {
			return code
		}
	}
	recordClients(configPath, out, clients...)
	return 0
}

// recordClients merges names into the config's clients list and returns the
// merged list. Save failure is a warning only — the hooks are the real
// effect; detectClients backfill recovers the record later.
func recordClients(configPath string, out io.Writer, names ...string) []string {
	cfg, err := loadConfig(configPath)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintln(out, "warning: could not record clients:", err)
		return names
	}
	changed := false
	for _, n := range names {
		if !slices.Contains(cfg.Clients, n) {
			cfg.Clients = append(cfg.Clients, n)
			changed = true
		}
	}
	if changed {
		if err := saveConfig(configPath, cfg); err != nil {
			fmt.Fprintln(out, "warning: could not record clients:", err)
		}
	}
	return cfg.Clients
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
		client = "claude-code"
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
	if err := installHooks(events, settingsPath, bin, client); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	fmt.Fprintf(out, "Installed %d %s hooks in %s\n", len(events), label, settingsPath)
	mcpPath := filepath.Join(home, ".claude.json")
	installMCP := installMCPClaude
	if client == "codex" {
		mcpPath = filepath.Join(home, ".codex", "config.toml")
		installMCP = installMCPCodex
	}
	if err := installMCP(mcpPath, bin); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	fmt.Fprintf(out, "Registered memoria MCP server in %s\n", mcpPath)
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
