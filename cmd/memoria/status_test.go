package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestInspectNothingRunning(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := statusSet(statusPath(cfgPath), "proj", "done", 0, "proposal ready"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if code := inspectProcess(cfgPath, "proj", &buf); code != 0 {
		t.Fatalf("inspect = %d: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "No background run") {
		t.Fatalf("want no-run message, got: %s", buf.String())
	}
}

// End to end with a real short-lived pid: inspect must stream the run log
// while the process lives and print the final status once it exits.
func TestInspectFollowsRunningJob(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	origPoll := inspectPoll
	inspectPoll = 20 * time.Millisecond
	defer func() { inspectPoll = origPoll }()

	job := exec.Command("sleep", "0.3")
	if err := job.Start(); err != nil {
		t.Fatal(err)
	}
	if err := statusSet(statusPath(cfgPath), "proj", "running", job.Process.Pid, ""); err != nil {
		t.Fatal(err)
	}
	logFile := runLogPath(cfgPath, "proj")
	if err := os.WriteFile(logFile, []byte("Invoking claude-code with 2 session(s)...\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = job.Wait()
		_ = statusSet(statusPath(cfgPath), "proj", "done", 0, "proposal ready: 5 pages")
		f, _ := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0o644)
		f.WriteString("Proposal from 2 session(s)\n")
		f.Close()
	}()

	var buf bytes.Buffer
	if code := inspectProcess(cfgPath, "proj", &buf); code != 0 {
		t.Fatalf("inspect = %d: %s", code, buf.String())
	}
	got := buf.String()
	for _, w := range []string{"Following pid", "Invoking claude-code", "Proposal from 2 session(s)", "proposal ready: 5 pages"} {
		if !strings.Contains(got, w) {
			t.Fatalf("inspect output missing %q:\n%s", w, got)
		}
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
