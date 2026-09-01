package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestQueueAddAndMarkEnded(t *testing.T) {
	q := filepath.Join(t.TempDir(), "pending.yaml")

	if err := queueAdd(q, "memoria", "/p/a.md"); err != nil {
		t.Fatal(err)
	}
	if err := queueAdd(q, "memoria", "/p/a.md"); err != nil { // dedupe
		t.Fatal(err)
	}
	if err := queueAdd(q, "memoria", "/p/b.md"); err != nil {
		t.Fatal(err)
	}
	if err := queueAdd(q, "other", "/o/c.md"); err != nil {
		t.Fatal(err)
	}
	if err := queueMarkEnded(q, "memoria", "/p/a.md"); err != nil {
		t.Fatal(err)
	}
	// absent entry: created already ended
	if err := queueMarkEnded(q, "other", "/o/d.md"); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(q)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Count(s, "/p/a.md") != 1 {
		t.Fatalf("dedupe failed:\n%s", s)
	}
	for _, w := range []string{"memoria:", "other:", "/p/b.md", "/o/c.md", "/o/d.md", "ended: true"} {
		if !strings.Contains(s, w) {
			t.Fatalf("queue missing %q:\n%s", w, s)
		}
	}
	// a.md ended, b.md not: exactly two ended entries (a.md and d.md)
	if strings.Count(s, "ended: true") != 2 {
		t.Fatalf("want 2 ended entries:\n%s", s)
	}
}

func TestQueueEndOthers(t *testing.T) {
	q := filepath.Join(t.TempDir(), "pending.yaml")
	for _, p := range []string{"/p/a.md", "/p/b.md", "/p/c.md"} {
		if err := queueAdd(q, "memoria", p); err != nil {
			t.Fatal(err)
		}
	}
	if err := queueAdd(q, "other", "/o/x.md"); err != nil {
		t.Fatal(err)
	}
	if err := queueEndOthers(q, "memoria", "/p/c.md"); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadQueue(q)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range loaded["memoria"] {
		if e.Path == "/p/c.md" && e.Ended {
			t.Fatal("kept session marked ended")
		}
		if e.Path != "/p/c.md" && !e.Ended {
			t.Fatalf("%s not ended", e.Path)
		}
	}
	if loaded["other"][0].Ended {
		t.Fatal("other project's session marked ended")
	}
}

func TestNewSessionEndsPreviousOnes(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	// s1 starts and never fires session-end (crash, kill, still open)
	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, "first"), io.Discard, cfg); err != nil {
		t.Fatal(err)
	}
	// s2 starts: s1 must now count as ended
	if err := captureHook("user-prompt", nil, promptPayload("s2", proj, "second"), io.Discard, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadQueue(queuePath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]bool{}
	for _, e := range loaded[filepath.Base(proj)] {
		byPath[filepath.Base(e.Path)] = e.Ended
	}
	if !byPath["s1.md"] {
		t.Fatalf("s1 not ended after s2 started: %v", byPath)
	}
	if byPath["s2.md"] {
		t.Fatalf("s2 (active) marked ended: %v", byPath)
	}
}

func TestQueueRemove(t *testing.T) {
	q := filepath.Join(t.TempDir(), "pending.yaml")
	if err := queueAdd(q, "memoria", "/p/a.md"); err != nil {
		t.Fatal(err)
	}
	if err := queueAdd(q, "memoria", "/p/b.md"); err != nil {
		t.Fatal(err)
	}
	if err := queueRemove(q, "memoria", "/p/a.md"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(q)
	if strings.Contains(string(b), "/p/a.md") || !strings.Contains(string(b), "/p/b.md") {
		t.Fatalf("remove wrong:\n%s", b)
	}
	// last entry gone → project key gone
	if err := queueRemove(q, "memoria", "/p/b.md"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(q)
	if strings.Contains(string(b), "memoria") {
		t.Fatalf("empty project key kept:\n%s", b)
	}
	// absent entry: no-op
	if err := queueRemove(q, "ghost", "/nope.md"); err != nil {
		t.Fatal(err)
	}
}

func TestQueueDropProject(t *testing.T) {
	q := filepath.Join(t.TempDir(), "pending.yaml")
	if err := queueAdd(q, "memoria", "/p/a.md"); err != nil {
		t.Fatal(err)
	}
	if err := queueAdd(q, "memoria", "/p/b.md"); err != nil {
		t.Fatal(err)
	}
	if err := queueAdd(q, "other", "/o/c.md"); err != nil {
		t.Fatal(err)
	}
	if err := queueDropProject(q, "memoria"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(q)
	if strings.Contains(string(b), "memoria") || !strings.Contains(string(b), "/o/c.md") {
		t.Fatalf("drop wrong:\n%s", b)
	}
	// absent key and missing file: no-ops
	if err := queueDropProject(q, "ghost"); err != nil {
		t.Fatal(err)
	}
	if err := queueDropProject(filepath.Join(t.TempDir(), "pending.yaml"), "memoria"); err != nil {
		t.Fatal(err)
	}
}

func TestQueueConcurrentAdds(t *testing.T) {
	q := filepath.Join(t.TempDir(), "pending.yaml")
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := queueAdd(q, "memoria", fmt.Sprintf("/p/%d.md", i)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	loaded, err := loadQueue(q)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded["memoria"]) != 20 {
		t.Fatalf("lost updates: want 20 entries, got %d", len(loaded["memoria"]))
	}
}

func TestCaptureHookRegistersInQueue(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	q := queuePath(cfg)

	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, "hi"), io.Discard, cfg); err != nil {
		t.Fatal(err)
	}
	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, "again"), io.Discard, cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(q)
	if err != nil {
		t.Fatalf("queue not written: %v", err)
	}
	if strings.Count(string(b), digestFile(proj, "s1")) != 1 {
		t.Fatalf("want exactly one s1 entry:\n%s", b)
	}
	if !regexp.MustCompile(`(?m)^"?` + regexp.QuoteMeta(filepath.Base(proj)) + `"?:`).Match(b) {
		t.Fatalf("not grouped by project:\n%s", b)
	}
	if strings.Contains(string(b), "ended") {
		t.Fatalf("ended before session-end:\n%s", b)
	}

	if err := captureHook("session-end", nil, payload("s1", proj), io.Discard, cfg); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(q)
	if !strings.Contains(string(b), "ended: true") {
		t.Fatalf("session-end did not mark entry:\n%s", b)
	}

	if err := captureHook("user-prompt", nil, promptPayload("s2", proj, "hi"), io.Discard, cfg); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(q)
	if !strings.Contains(string(b), digestFile(proj, "s2")) {
		t.Fatalf("second session missing:\n%s", b)
	}
}
