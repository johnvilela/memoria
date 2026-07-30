package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// isolates HOME and XDG_CONFIG_HOME so init never touches real settings/config
func initEnv(t *testing.T) (home, configPath string) {
	t.Helper()
	home = t.TempDir()
	cfgDir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	return home, filepath.Join(cfgDir, "memoria", "config.yaml")
}

func runInitCmd(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	code := run(append([]string{"init"}, args...), strings.NewReader(""), &buf)
	return code, buf.String()
}

func TestInitRequiresAgentArg(t *testing.T) {
	var buf bytes.Buffer
	if code := run([]string{"init"}, strings.NewReader(""), &buf); code != 1 {
		t.Fatalf("init without agent = %d, want 1", code)
	}
	if !strings.Contains(buf.String(), "usage: memoria init") {
		t.Fatalf("missing init usage, got %q", buf.String())
	}
}

func TestInitRejectsUnknownAgent(t *testing.T) {
	var buf bytes.Buffer
	if code := run([]string{"init", "emacs"}, strings.NewReader(""), &buf); code != 1 {
		t.Fatalf("init emacs = %d, want 1", code)
	}
	if !strings.Contains(buf.String(), "emacs") {
		t.Fatal("error should name the unknown agent")
	}
}

func TestInitClientFlagAndPositionalEquivalent(t *testing.T) {
	for _, args := range [][]string{
		{"claude-code", "--processor", "ollama"},
		{"--client", "claude-code", "--processor", "ollama"},
	} {
		home, cfgPath := initEnv(t)
		code, out := runInitCmd(t, args...)
		if code != 0 {
			t.Fatalf("init %v = %d: %s", args, code, out)
		}
		if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); err != nil {
			t.Fatalf("init %v: hooks not installed: %v", args, err)
		}
		b, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatalf("init %v: config not written: %v", args, err)
		}
		if !strings.Contains(string(b), "processor: ollama") {
			t.Fatalf("init %v: processor not saved: %q", args, b)
		}
		if !strings.Contains(out, "coming soon") {
			t.Fatalf("init %v: ollama placeholder note missing: %q", args, out)
		}
	}
}

func TestInitNonTTYSkipsProcessor(t *testing.T) {
	_, cfgPath := initEnv(t)
	code, out := runInitCmd(t, "claude-code")
	if code != 0 {
		t.Fatalf("init claude-code = %d: %s", code, out)
	}
	if !strings.Contains(out, "No processor configured") {
		t.Fatalf("missing skip hint: %q", out)
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if cfg.Processor != "" {
		t.Fatalf("processor set without --processor: %+v", cfg)
	}
	if len(cfg.Clients) != 1 || cfg.Clients[0] != "claude-code" {
		t.Fatalf("clients = %v, want [claude-code]", cfg.Clients)
	}
}

func TestInitMultipleClients(t *testing.T) {
	for _, args := range [][]string{
		{"claude-code", "codex"},
		{"--client", "claude-code,codex"},
	} {
		home, cfgPath := initEnv(t)
		code, out := runInitCmd(t, args...)
		if code != 0 {
			t.Fatalf("init %v = %d: %s", args, code, out)
		}
		for _, rel := range []string{
			filepath.Join(".claude", "settings.json"),
			filepath.Join(".codex", "hooks.json"),
			".claude.json",
			filepath.Join(".codex", "config.toml"),
		} {
			if _, err := os.Stat(filepath.Join(home, rel)); err != nil {
				t.Fatalf("init %v: %s missing: %v", args, rel, err)
			}
		}
		cfg, _ := loadConfig(cfgPath)
		if len(cfg.Clients) != 2 || cfg.Clients[0] != "claude-code" || cfg.Clients[1] != "codex" {
			t.Fatalf("init %v: clients = %v, want [claude-code codex]", args, cfg.Clients)
		}
	}
}

func TestInitUnknownClientFailsFast(t *testing.T) {
	home, _ := initEnv(t)
	code, out := runInitCmd(t, "claude-code", "emacs")
	if code != 1 || !strings.Contains(out, "emacs") {
		t.Fatalf("init claude-code emacs: code=%d out=%q, want 1 naming emacs", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatal("hooks installed despite unknown client in the list")
	}
}

func TestInitClientAliasDedup(t *testing.T) {
	_, cfgPath := initEnv(t)
	code, out := runInitCmd(t, "claude", "claude-code")
	if code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	cfg, _ := loadConfig(cfgPath)
	if len(cfg.Clients) != 1 || cfg.Clients[0] != "claude-code" {
		t.Fatalf("clients = %v, want [claude-code] once", cfg.Clients)
	}
}

func TestInitInteractiveMultiSelect(t *testing.T) {
	origTTY, origMulti := isTTY, selectMulti
	t.Cleanup(func() { isTTY, selectMulti = origTTY, origMulti })
	isTTY = func() bool { return true }

	home, cfgPath := initEnv(t)
	stubSystemctl(t)
	selectMulti = func(title string, opts []option) ([]string, error) {
		return []string{"claude-code", "codex"}, nil
	}
	// remaining flags silence the other interactive prompts
	code, out := runInitCmd(t, "--processor", "ollama", "--notification=false", "--auto-apply=false", "--cron", "off")
	if code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "hooks.json")); err != nil {
		t.Fatalf("codex hooks missing: %v", err)
	}
	cfg, _ := loadConfig(cfgPath)
	if len(cfg.Clients) != 2 {
		t.Fatalf("clients = %v, want both", cfg.Clients)
	}

	initEnv(t)
	selectMulti = func(title string, opts []option) ([]string, error) { return nil, nil }
	code, out = runInitCmd(t)
	if code != 1 || !strings.Contains(out, "no agents selected") {
		t.Fatalf("empty selection: code=%d out=%q, want 1 + message", code, out)
	}
}

func TestInitRejectsUnknownProcessor(t *testing.T) {
	initEnv(t)
	code, out := runInitCmd(t, "claude-code", "--processor", "gpt5")
	if code != 1 || !strings.Contains(out, "gpt5") {
		t.Fatalf("unknown processor: code=%d out=%q", code, out)
	}
}

func TestInitProcessorSavedWithWarningWhenBinaryMissing(t *testing.T) {
	_, cfgPath := initEnv(t)
	t.Setenv("PATH", t.TempDir()) // empty dir: no claude binary
	code, out := runInitCmd(t, "claude-code", "--processor", "claude-code")
	if code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	if !strings.Contains(out, "warning") || !strings.Contains(out, "claude") {
		t.Fatalf("missing PATH warning: %q", out)
	}
	b, err := os.ReadFile(cfgPath)
	if err != nil || !strings.Contains(string(b), "processor: claude-code") {
		t.Fatalf("processor not saved despite warning: %v %q", err, b)
	}
	info, _ := os.Stat(cfgPath)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 600", info.Mode().Perm())
	}
}

func TestInitGeminiEnvKey(t *testing.T) {
	_, cfgPath := initEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "sekret" {
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()
	orig := geminiModelsURL
	geminiModelsURL = srv.URL
	defer func() { geminiModelsURL = orig }()

	t.Setenv("GEMINI_API_KEY", "sekret")
	code, out := runInitCmd(t, "claude-code", "--processor", "gemini")
	if code != 0 {
		t.Fatalf("init gemini = %d: %s", code, out)
	}
	if strings.Contains(out, "warning") {
		t.Fatalf("valid key produced warning: %q", out)
	}
	b, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(b), "processor: gemini") || !strings.Contains(string(b), "gemini_api_key: sekret") {
		t.Fatalf("gemini config not saved: %q", b)
	}

	t.Setenv("GEMINI_API_KEY", "wrong")
	code, out = runInitCmd(t, "claude-code", "--processor", "gemini")
	if code != 0 || !strings.Contains(out, "warning") {
		t.Fatalf("bad key: code=%d out=%q, want saved with warning", code, out)
	}
}

func TestInitGeminiNoKeyNonTTY(t *testing.T) {
	initEnv(t)
	t.Setenv("GEMINI_API_KEY", "")
	code, out := runInitCmd(t, "claude-code", "--processor", "gemini")
	if code != 1 || !strings.Contains(out, "GEMINI_API_KEY") {
		t.Fatalf("gemini without key: code=%d out=%q, want error naming GEMINI_API_KEY", code, out)
	}
}

func TestInitNotificationFlag(t *testing.T) {
	_, cfgPath := initEnv(t)
	// no --processor: the notification flag must still be honored
	code, out := runInitCmd(t, "claude-code", "--notification")
	if code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	if !strings.Contains(out, "Notifications enabled") {
		t.Fatalf("confirmation missing: %q", out)
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil || !cfg.Notifications {
		t.Fatalf("notifications not saved: %v %+v", err, cfg)
	}
}

func TestInitNotificationFalseFlag(t *testing.T) {
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Notifications: true}); err != nil {
		t.Fatal(err)
	}
	code, out := runInitCmd(t, "claude-code", "--processor", "ollama", "--notification=false")
	if code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Notifications {
		t.Fatal("--notification=false did not disable")
	}
	if cfg.Processor != "ollama" {
		t.Fatalf("processor lost: %+v", cfg)
	}
}

func TestInitNotificationOmittedPreservesConfig(t *testing.T) {
	_, cfgPath := initEnv(t)
	if err := saveConfig(cfgPath, config{Notifications: true}); err != nil {
		t.Fatal(err)
	}
	code, out := runInitCmd(t, "claude-code") // non-TTY, no flags
	if code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	cfg, _ := loadConfig(cfgPath)
	if !cfg.Notifications {
		t.Fatal("omitted flag reset notifications")
	}
}

func TestInitNotificationWarnsWhenNotifySendMissing(t *testing.T) {
	initEnv(t)
	t.Setenv("PATH", t.TempDir()) // empty dir: no notify-send
	code, out := runInitCmd(t, "claude-code", "--notification")
	if code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	if !strings.Contains(out, "warning") || !strings.Contains(out, "notify-send") {
		t.Fatalf("missing notify-send warning: %q", out)
	}
}

// gitDir creates a temp git repo; withCommit adds one commit touching main.go.
func gitDir(t *testing.T, withCommit bool) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q")
	if withCommit {
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", ".")
		git("commit", "-q", "-m", "feat: add rocket engine")
	}
	return dir
}

func TestHookCommandIsSilentAndAlwaysZero(t *testing.T) {
	var buf bytes.Buffer
	// no name, empty stdin, garbage stdin — all exit 0, no stdout
	for _, args := range [][]string{{"hook"}, {"hook", "stop"}, {"hook", "stop"}} {
		buf.Reset()
		if code := run(args, strings.NewReader("not json"), &buf); code != 0 {
			t.Fatalf("run(%v) = %d, want 0", args, code)
		}
		if buf.Len() != 0 {
			t.Fatalf("run(%v) wrote to stdout: %q", args, buf.String())
		}
	}
}

func TestInitAddsGitignore(t *testing.T) {
	initEnv(t)
	dir := gitDir(t, false)
	t.Chdir(dir)
	code, out := runInitCmd(t, "claude-code")
	if code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(".gitignore not created:", err)
	}
	if !strings.Contains(string(b), ".memoria/") {
		t.Fatalf(".gitignore = %q, missing .memoria/", b)
	}
}

func TestInitGitignoreAppendsExisting(t *testing.T) {
	initEnv(t)
	dir := gitDir(t, false)
	gi := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gi, []byte("node_modules"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if code, out := runInitCmd(t, "claude-code"); code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	b, _ := os.ReadFile(gi)
	if got := string(b); got != "node_modules\n.memoria/\n" {
		t.Fatalf(".gitignore = %q, want existing content preserved + entry", got)
	}
}

func TestInitSkipsGitignoreOutsideGitRepo(t *testing.T) {
	initEnv(t)
	dir := t.TempDir()
	t.Chdir(dir)
	if code, out := runInitCmd(t, "claude-code"); code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); !os.IsNotExist(err) {
		t.Fatal(".gitignore created outside a git repo")
	}
}

func TestInitAutoApplyFlag(t *testing.T) {
	_, cfgPath := initEnv(t)
	if code, out := runInitCmd(t, "claude-code", "--processor", "ollama", "--auto-apply"); code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AutoApply {
		t.Fatal("auto_apply not saved")
	}

	_, cfgPath = initEnv(t)
	if code, out := runInitCmd(t, "claude-code", "--processor", "ollama"); code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	if cfg, _ := loadConfig(cfgPath); cfg.AutoApply {
		t.Fatal("auto_apply enabled without the flag")
	}
}
