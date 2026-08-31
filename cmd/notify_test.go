package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubNotify replaces notifyCmd, recording every title/body pair
func stubNotify(t *testing.T) *[][2]string {
	t.Helper()
	var got [][2]string
	orig := notifyCmd
	notifyCmd = func(title, body string) error {
		got = append(got, [2]string{title, body})
		return nil
	}
	t.Cleanup(func() { notifyCmd = orig })
	return &got
}

func TestNotifyDisabledByDefault(t *testing.T) {
	got := stubNotify(t)
	notify(config{}, "memoria", "should not fire")
	if len(*got) != 0 {
		t.Fatalf("notified despite disabled config: %v", *got)
	}
}

func TestNotifyEnabled(t *testing.T) {
	got := stubNotify(t)
	notify(config{Notifications: true}, "memoria", "proposal ready")
	if len(*got) != 1 || (*got)[0] != [2]string{"memoria", "proposal ready"} {
		t.Fatalf("notify call wrong: %v", *got)
	}
}

func TestNotifyErrorOnlyLogs(t *testing.T) {
	origCmd := notifyCmd
	notifyCmd = func(title, body string) error { return fmt.Errorf("no dbus") }
	t.Cleanup(func() { notifyCmd = origCmd })
	origLog := logPath
	logPath = filepath.Join(t.TempDir(), "memoria.log")
	t.Cleanup(func() { logPath = origLog })

	notify(config{Notifications: true}, "memoria", "x")
	b, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(b), "no dbus") {
		t.Fatalf("notify error not logged: %v %q", err, b)
	}
}
