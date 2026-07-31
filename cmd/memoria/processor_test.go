package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression for "fork/exec claude: argument list too long": prompts larger
// than the kernel's ~128KiB per-argv-element cap (MAX_ARG_STRLEN) must reach
// the processor via stdin, never argv. A 119KB session digest plus the wiki
// blew this in production.
func TestRunProcessorCmdLargePromptViaStdin(t *testing.T) {
	prompt := strings.Repeat("session digest line\n", 15000) // ~300KB, > MAX_ARG_STRLEN
	out, err := runProcessorCmd("cat", nil, t.TempDir(), prompt)
	if err != nil {
		t.Fatalf("large prompt failed: %v", err)
	}
	if out != prompt {
		t.Fatalf("stdin roundtrip mismatch: got %d bytes, want %d", len(out), len(prompt))
	}
}

// Prompt must not appear in argv at all — even small ones — so growth never
// reintroduces E2BIG. args() prints argv; the prompt should be absent.
func TestRunProcessorCmdPromptNotInArgv(t *testing.T) {
	out, err := runProcessorCmd("sh", []string{"-c", `printf '%s' "$*"`, "sh"}, t.TempDir(), "SECRET_PROMPT")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(out, "SECRET_PROMPT") {
		t.Fatal("prompt leaked into argv")
	}
}

func TestRunProcessorCmdReportsStderr(t *testing.T) {
	_, err := runProcessorCmd("sh", []string{"-c", "echo boom >&2; exit 1"}, t.TempDir(), "p")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want stderr in error, got: %v", err)
	}
}

// stubProcessorBin drops a fake AI CLI on PATH that prints its argv and cwd.
func stubProcessorBin(t *testing.T, name string) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\nprintf '%s|%s' \"$*\" \"$PWD\"\n"
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// Regression for "Not inside a trusted directory": a project with .git is
// trusted by codex natively, so it runs there without --skip-git-repo-check.
func TestInvokeProcessorCodexGitRepoTrusted(t *testing.T) {
	stubProcessorBin(t, "codex")
	// EvalSymlinks so the expectation matches the child's $PWD: on macOS
	// t.TempDir() hands back /var/... but the resolved cwd is /private/var/...
	proj, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(proj, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := invokeProcessor(config{Processor: "codex"}, proj, "hi")
	if err != nil {
		t.Fatalf("codex: %v", err)
	}
	if strings.Contains(out, "--skip-git-repo-check") {
		t.Fatalf("git repo must not need --skip-git-repo-check: %q", out)
	}
	if !strings.HasSuffix(out, "|"+proj) {
		t.Fatalf("codex should run in project dir %s, got %q", proj, out)
	}
	if !strings.Contains(out, "-m gpt-5.4.mini") || !strings.Contains(out, "model_reasoning_effort=high") {
		t.Fatalf("codex should default to cheap model + high effort: %q", out)
	}
}

// Without .git codex falls back to a temp cwd plus --skip-git-repo-check.
func TestInvokeProcessorCodexNonGitSkipsCheck(t *testing.T) {
	stubProcessorBin(t, "codex")
	out, err := invokeProcessor(config{Processor: "codex"}, t.TempDir(), "hi")
	if err != nil {
		t.Fatalf("codex: %v", err)
	}
	if !strings.Contains(out, "--skip-git-repo-check") {
		t.Fatalf("non-git dir must pass --skip-git-repo-check: %q", out)
	}
}

// Wiki work is text digestion — claude defaults to haiku, and
// processor_model/processor_effort override the defaults.
func TestInvokeProcessorModelFlags(t *testing.T) {
	stubProcessorBin(t, "claude")
	out, err := invokeProcessor(config{Processor: "claude-code"}, "", "hi")
	if err != nil {
		t.Fatalf("claude: %v", err)
	}
	if !strings.Contains(out, "--model haiku") {
		t.Fatalf("claude should default to haiku: %q", out)
	}
	out, err = invokeProcessor(config{Processor: "claude-code", ProcessorModel: "sonnet"}, "", "hi")
	if err != nil {
		t.Fatalf("claude: %v", err)
	}
	if !strings.Contains(out, "--model sonnet") {
		t.Fatalf("processor_model should override: %q", out)
	}

	stubProcessorBin(t, "codex")
	out, err = invokeProcessor(config{Processor: "codex", ProcessorModel: "gpt-6", ProcessorEffort: "medium"}, "", "hi")
	if err != nil {
		t.Fatalf("codex: %v", err)
	}
	if !strings.Contains(out, "-m gpt-6") || !strings.Contains(out, "model_reasoning_effort=medium") {
		t.Fatalf("codex overrides should apply: %q", out)
	}
}
