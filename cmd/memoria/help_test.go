package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderHelpContainsASCIIArt(t *testing.T) {
	out := renderHelp()
	// figlet-style art spans multiple lines of box-drawing/blocks; check a distinctive fragment
	if !strings.Contains(out, asciiArt) {
		t.Fatal("help output missing ASCII art")
	}
	if lines := strings.Count(asciiArt, "\n"); lines < 3 {
		t.Fatalf("ASCII art too flat: %d lines", lines)
	}
}

func TestRenderHelpListsCommands(t *testing.T) {
	out := renderHelp()
	for _, want := range []string{"help", "init", "hook", "(internal)"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q", want)
		}
	}
	if strings.Contains(out, "coming soon") {
		t.Error("no commands should be pending anymore")
	}
}

func TestRenderHelpUsageLine(t *testing.T) {
	if !strings.Contains(renderHelp(), "Usage: memoria <command>") {
		t.Fatal("help output missing usage line")
	}
}

func TestRunDispatch(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code int
	}{
		{"no args", nil, 0},
		{"help", []string{"help"}, 0},
		{"--help", []string{"--help"}, 0},
		{"-h", []string{"-h"}, 0},
		{"unknown", []string{"bogus"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if code := run(tc.args, strings.NewReader(""), &buf); code != tc.code {
				t.Fatalf("run(%v) = %d, want %d", tc.args, code, tc.code)
			}
			if !strings.Contains(buf.String(), "Usage: memoria <command>") {
				t.Fatal("dispatch did not print help")
			}
		})
	}
}
