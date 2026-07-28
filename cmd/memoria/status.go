package main

import (
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

// status.yaml next to the config: one entry per project tracking its latest
// background processing run (running | done | error).
type procStatus struct {
	State      string `yaml:"state"`
	PID        int    `yaml:"pid,omitempty"`
	StartedAt  string `yaml:"started_at,omitempty"`
	FinishedAt string `yaml:"finished_at,omitempty"`
	Detail     string `yaml:"detail,omitempty"`
}

func statusPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "status.yaml")
}

func loadStatus(path string) (map[string]procStatus, error) {
	st := map[string]procStatus{}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	return st, yaml.Unmarshal(b, &st)
}

// statusSet records a state transition. "running" starts a fresh entry;
// "done"/"error" stamp finished_at + detail and keep pid/started_at.
func statusSet(path, projName, state string, pid int, detail string) error {
	st, err := loadStatus(path)
	if err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339)
	e := st[projName]
	e.State = state
	e.Detail = detail
	if state == "running" {
		e.PID = pid
		e.StartedAt = now
		e.FinishedAt = ""
	} else {
		e.FinishedAt = now
	}
	st[projName] = e
	b, err := yaml.Marshal(st)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// runLogPath is where a project's detached run writes its stdout/stderr,
// truncated on each new run. process/lint/seed share it — one background job
// per project at a time.
func runLogPath(configPath, projName string) string {
	return filepath.Join(filepath.Dir(configPath), projName+".run.log")
}

// inspectPoll is a var so tests can speed the tail loop up.
var inspectPoll = 500 * time.Millisecond

// inspectProcess streams the running background job's output until it exits,
// then prints the final status. Detaching (ctrl-c) doesn't touch the job.
func inspectProcess(configPath, projName string, out io.Writer) int {
	sPath := statusPath(configPath)
	st, err := loadStatus(sPath)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	e := st[projName]
	if e.State != "running" || !pidAlive(e.PID) {
		fmt.Fprintf(out, "No background run for %s — memoria status has the last result.\n", projName)
		return 0
	}
	f, err := os.Open(runLogPath(configPath, projName))
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	defer f.Close()
	fmt.Fprintf(out, "Following pid %d (ctrl-c detaches, the run keeps going)\n", e.PID)
	for pidAlive(e.PID) {
		_, _ = io.Copy(out, f)
		time.Sleep(inspectPoll)
	}
	_, _ = io.Copy(out, f)
	if st, err := loadStatus(sPath); err == nil {
		fe := st[projName]
		fmt.Fprintf(out, "%s: %s — %s\n", projName, fe.State, fe.Detail)
	}
	return 0
}

// pidAlive reports whether the process exists (EPERM still means alive).
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// runStatus prints the background processing state of every project.
func runStatus(configPath string, out io.Writer) int {
	st, err := loadStatus(statusPath(configPath))
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if len(st) == 0 {
		fmt.Fprintln(out, "No processing recorded.")
		return 0
	}
	for _, name := range slices.Sorted(maps.Keys(st)) {
		e := st[name]
		switch {
		case e.State == "running" && pidAlive(e.PID):
			fmt.Fprintf(out, "%s: running (pid %d, started %s)\n", name, e.PID, e.StartedAt)
		case e.State == "running":
			fmt.Fprintf(out, "%s: error — process died (started %s)\n", name, e.StartedAt)
		case e.State == "error":
			fmt.Fprintf(out, "%s: error — %s (finished %s)\n", name, e.Detail, e.FinishedAt)
		default:
			fmt.Fprintf(out, "%s: %s — %s (finished %s)\n", name, e.State, e.Detail, e.FinishedAt)
		}
	}
	return 0
}
