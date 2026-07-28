package main

import (
	"bytes"
	"strings"
	"testing"
)

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
