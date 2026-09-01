package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const finalizeProposal = `{"pages":[
	{"path":"sessions/s1.md","title":"S1","tags":[],"body_markdown":"# S1\n\nwork\n"},
	{"path":"concepts/flush.md","title":"Flush","tags":["flush"],"body_markdown":"# Flush\n\npre-PR flush.\n"}
]}`

// live un-ended session for proj, captured the way hooks would
func liveSession(t *testing.T, proj, cfg string) {
	t.Helper()
	if err := captureHook("user-prompt", nil, promptPayload("s1", proj, "add finalize"), io.Discard, cfg); err != nil {
		t.Fatal(err)
	}
}

func TestFinalizeEndsAndApplies(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	liveSession(t, proj, cfg)
	stubProcessor(t, finalizeProposal, nil)
	var buf bytes.Buffer
	if code := runFinalize(proj, cfg, nil, &buf); code != 0 {
		t.Fatalf("finalize = %d: %s", code, buf.String())
	}
	if _, err := os.Stat(filepath.Join(proj, "wiki", "concepts", "flush.md")); err != nil {
		t.Fatalf("wiki page not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".memoria", "sessions", "processed", "s1.md")); err != nil {
		t.Fatalf("digest not archived: %v", err)
	}
	front, _ := parseDigest(filepath.Join(proj, ".memoria", "sessions", "processed", "s1.md"))
	if frontKey(front, "ended_at") == "" {
		t.Fatalf("ended_at not stamped:\n%s", front)
	}
	if !strings.Contains(buf.String(), "Commit the wiki changes") {
		t.Fatalf("missing commit hint: %s", buf.String())
	}
}

func TestFinalizeNoApplyLeavesProposal(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	liveSession(t, proj, cfg)
	stubProcessor(t, finalizeProposal, nil)
	var buf bytes.Buffer
	if code := runFinalize(proj, cfg, []string{"--no-apply"}, &buf); code != 0 {
		t.Fatalf("finalize = %d: %s", code, buf.String())
	}
	if _, err := os.Stat(filepath.Join(proj, ".memoria", "proposal.json")); err != nil {
		t.Fatalf("proposal not kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, "wiki", "concepts", "flush.md")); !os.IsNotExist(err) {
		t.Fatal("wiki written despite --no-apply")
	}
	q, _ := loadQueue(queuePath(cfg))
	name := filepath.Base(proj)
	if len(q[name]) != 1 || !q[name][0].Ended {
		t.Fatalf("queue entry not ended: %+v", q[name])
	}
}

func TestFinalizeAlreadyProcessed(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	liveSession(t, proj, cfg)
	processed := filepath.Join(proj, ".memoria", "sessions", "processed")
	if err := os.MkdirAll(processed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(digestFile(proj, "s1"), filepath.Join(processed, "s1.md")); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if code := runFinalize(proj, cfg, nil, &buf); code != 1 || !strings.Contains(buf.String(), "already processed") {
		t.Fatalf("finalize = %d: %s", code, buf.String())
	}
}

func TestFinalizeBusyJob(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	liveSession(t, proj, cfg)
	if err := statusSet(statusPath(cfg), filepath.Base(proj), "running", os.Getpid(), ""); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if code := runFinalize(proj, cfg, nil, &buf); code != 1 || !strings.Contains(buf.String(), "already running") {
		t.Fatalf("finalize = %d: %s", code, buf.String())
	}
}

func TestMCPConsolidateEndCurrent(t *testing.T) {
	proj := t.TempDir()
	cfg := testConfig(t, proj)
	liveSession(t, proj, cfg)
	spawned := stubSpawn(t, 4242)
	res, err := mcpConsolidate(proj, cfg, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "started" {
		t.Fatalf("state = %q: %+v", res.State, res)
	}
	front, _ := parseDigest(digestFile(proj, "s1"))
	if frontKey(front, "ended_at") == "" {
		t.Fatalf("ended_at not stamped:\n%s", front)
	}
	q, _ := loadQueue(queuePath(cfg))
	if name := filepath.Base(proj); len(q[name]) != 1 || !q[name][0].Ended {
		t.Fatalf("queue entry not ended: %+v", q[name])
	}
	if !strings.Contains(strings.Join(*spawned, " "), "process --foreground") {
		t.Fatalf("spawned %v", *spawned)
	}
}

func bashPayload(sid, cwd, command, toolErr string) *strings.Reader {
	p := map[string]any{"session_id": sid, "cwd": cwd,
		"tool_name": "Bash", "tool_input": map[string]any{"command": command}}
	if toolErr != "" {
		p["tool_response"] = map[string]any{"error": toolErr}
	}
	b, _ := json.Marshal(p)
	return strings.NewReader(string(b))
}

func TestCaptureHookPRNudge(t *testing.T) {
	claude := []string{"--client", "claude-code"}
	cases := []struct {
		name    string
		client  []string
		command string
		toolErr string
		prior   bool // a prior observation exists
		want    bool
	}{
		{"fires", claude, "gh pr create --fill", "", true, true},
		{"other command", claude, "git push", "", true, false},
		{"failed pr create", claude, "gh pr create", "exit 1", true, false},
		{"no prior observations", claude, "gh pr create", "", false, false},
		{"codex client", []string{"--client", "codex"}, "gh pr create", "", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proj := t.TempDir()
			cfg := testConfig(t, proj)
			if tc.prior {
				if err := captureHook("user-prompt", tc.client, promptPayload("s1", proj, "make a PR"), io.Discard, cfg); err != nil {
					t.Fatal(err)
				}
			}
			var buf bytes.Buffer
			if err := captureHook("post-tool-use", tc.client, bashPayload("s1", proj, tc.command, tc.toolErr), &buf, cfg); err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(buf.String(), "additionalContext"); got != tc.want {
				t.Fatalf("nudge = %v, want %v: %q", got, tc.want, buf.String())
			}
			if tc.want && (!strings.Contains(buf.String(), "end_current=true") || !strings.Contains(buf.String(), "memoria finalize")) {
				t.Fatalf("nudge text incomplete: %q", buf.String())
			}
		})
	}
}
