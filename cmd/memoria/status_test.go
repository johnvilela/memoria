package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusSetAndLoad(t *testing.T) {
	sp := filepath.Join(t.TempDir(), "status.yaml")
	if err := statusSet(sp, "proj", "running", 4242, ""); err != nil {
		t.Fatal(err)
	}
	st, err := loadStatus(sp)
	if err != nil {
		t.Fatal(err)
	}
	e := st["proj"]
	if e.State != "running" || e.PID != 4242 || e.StartedAt == "" || e.FinishedAt != "" {
		t.Fatalf("running entry wrong: %+v", e)
	}

	if err := statusSet(sp, "proj", "done", 0, "proposal ready: 2 pages"); err != nil {
		t.Fatal(err)
	}
	st, _ = loadStatus(sp)
	e = st["proj"]
	if e.State != "done" || e.PID != 4242 || e.FinishedAt == "" || e.Detail != "proposal ready: 2 pages" {
		t.Fatalf("done entry wrong: %+v", e)
	}
	if e.StartedAt == "" {
		t.Fatal("done must keep started_at")
	}
}

func TestPidAlive(t *testing.T) {
	if !pidAlive(os.Getpid()) {
		t.Fatal("own pid should be alive")
	}
	if pidAlive(999999999) || pidAlive(0) || pidAlive(-1) {
		t.Fatal("bogus pids should be dead")
	}
}

func TestRunStatusEmpty(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	var buf bytes.Buffer
	if code := runStatus(cfgPath, &buf); code != 0 {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(buf.String(), "No processing recorded") {
		t.Fatalf("empty message missing: %s", buf.String())
	}
}

func TestRunStatusOutput(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	sp := statusPath(cfgPath)
	if err := statusSet(sp, "alive", "running", os.Getpid(), ""); err != nil {
		t.Fatal(err)
	}
	if err := statusSet(sp, "crashed", "running", 999999999, ""); err != nil {
		t.Fatal(err)
	}
	if err := statusSet(sp, "finished", "done", 0, "proposal ready: 3 pages"); err != nil {
		t.Fatal(err)
	}
	if err := statusSet(sp, "broken", "error", 0, "processor: exit status 1"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if code := runStatus(cfgPath, &buf); code != 0 {
		t.Fatalf("status = %d: %s", code, buf.String())
	}
	got := buf.String()
	for _, w := range []string{
		"alive", "running",
		"crashed", "process died",
		"finished", "proposal ready: 3 pages",
		"broken", "processor: exit status 1",
	} {
		if !strings.Contains(got, w) {
			t.Fatalf("status output missing %q:\n%s", w, got)
		}
	}
}
