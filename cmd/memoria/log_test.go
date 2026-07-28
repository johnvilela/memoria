package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain disables logging so unrelated tests never touch the real
// ~/.config/memoria/memoria.log; log tests redirect logPath themselves.
// isTTY is forced false so init tests never open the interactive TUI.
// spawnDetached errors so no test ever re-execs the test binary; detach
// tests stub it explicitly.
func TestMain(m *testing.M) {
	logPath = ""
	isTTY = func() bool { return false }
	spawnDetached = func(dir, logFile string, args ...string) (int, error) {
		return 0, fmt.Errorf("spawnDetached not stubbed")
	}
	notifyCmd = func(title, body string) error { return nil } // never shell out to notify-send
	runSystemctl = func(args ...string) error { return nil }  // never touch the real systemd
	os.Exit(m.Run())
}

func TestLogf(t *testing.T) {
	orig := logPath
	logPath = filepath.Join(t.TempDir(), "memoria.log")
	defer func() { logPath = orig }()

	logf("digest", "hello %d", 42)
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "[digest] hello 42") {
		t.Fatalf("line missing: %q", b)
	}

	// past the cap, next write rotates to .old
	if err := os.WriteFile(logPath, make([]byte, logMaxSize+1), 0o644); err != nil {
		t.Fatal(err)
	}
	logf("digest", "after rotate")
	if _, err := os.Stat(logPath + ".old"); err != nil {
		t.Fatalf(".old missing: %v", err)
	}
	b, _ = os.ReadFile(logPath)
	if !strings.Contains(string(b), "after rotate") || len(b) > 200 {
		t.Fatalf("fresh file wrong: %d bytes %q", len(b), b)
	}
}

func TestLogfDisabled(t *testing.T) {
	orig := logPath
	logPath = ""
	defer func() { logPath = orig }()
	logf("digest", "should not panic")
}
