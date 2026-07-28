package main

import (
	"strings"
	"testing"
)

// Regression for "fork/exec claude: argument list too long": prompts larger
// than the kernel's ~128KiB per-argv-element cap (MAX_ARG_STRLEN) must reach
// the processor via stdin, never argv. A 119KB session digest plus the wiki
// blew this in production.
func TestRunProcessorCmdLargePromptViaStdin(t *testing.T) {
	prompt := strings.Repeat("session digest line\n", 15000) // ~300KB, > MAX_ARG_STRLEN
	out, err := runProcessorCmd("cat", nil, prompt)
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
	out, err := runProcessorCmd("sh", []string{"-c", `printf '%s' "$*"`, "sh"}, "SECRET_PROMPT")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(out, "SECRET_PROMPT") {
		t.Fatal("prompt leaked into argv")
	}
}

func TestRunProcessorCmdReportsStderr(t *testing.T) {
	_, err := runProcessorCmd("sh", []string{"-c", "echo boom >&2; exit 1"}, "p")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want stderr in error, got: %v", err)
	}
}
