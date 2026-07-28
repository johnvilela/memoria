package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
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
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("config written without --processor: %v", err)
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
