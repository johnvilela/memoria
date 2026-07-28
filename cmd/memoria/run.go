package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var runOptions = []option{
	{value: "no", label: "No", desc: "start a fresh session"},
	{value: "yes", label: "Yes", desc: "hand the last session over to the agent"},
}

type sessionEntry struct{ date, sid, name string }

// readSessions parses <proj>/.memoria/sessions.md ("RFC3339 - SID - NAME"
// per line, append-only so last = most recent). Missing file or malformed
// lines are skipped — callers decide what emptiness means.
func readSessions(proj string) []sessionEntry {
	b, err := os.ReadFile(filepath.Join(proj, ".memoria", "sessions.md"))
	if err != nil {
		return nil
	}
	var entries []sessionEntry
	for _, line := range strings.Split(string(b), "\n") {
		// limit 3: NAME is a user prompt and may itself contain " - "
		parts := strings.SplitN(line, " - ", 3)
		if len(parts) != 3 {
			continue
		}
		entries = append(entries, sessionEntry{date: parts[0], sid: parts[1], name: parts[2]})
	}
	return entries
}

// findDigest returns the newest digest file for sid, or "". Pending wins:
// incarnations are only ever created there, so a pending one is always newer
// than any processed one.
func findDigest(proj, sid string) string {
	pending := filepath.Join(proj, ".memoria", "sessions", "pending")
	processed := filepath.Join(proj, ".memoria", "sessions", "processed")
	if n := maxIncarnation(pending, sid); n > 0 {
		return filepath.Join(pending, incarnationName(sid, n))
	}
	if n := maxIncarnation(processed, sid); n > 0 {
		return filepath.Join(processed, incarnationName(sid, n))
	}
	return ""
}

// digestClient reads the client: frontmatter line of a digest ("" if absent —
// sessions captured before --client was baked into the hooks).
func digestClient(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	rest, ok := strings.CutPrefix(string(b), "---\n")
	if !ok {
		return ""
	}
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ""
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		if v, ok := strings.CutPrefix(line, "client:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// matchSessions returns entries whose sid starts with q or whose name
// contains q, case-insensitive.
func matchSessions(entries []sessionEntry, q string) []sessionEntry {
	q = strings.ToLower(q)
	var hits []sessionEntry
	for _, e := range entries {
		if strings.HasPrefix(strings.ToLower(e.sid), q) || strings.Contains(strings.ToLower(e.name), q) {
			hits = append(hits, e)
		}
	}
	return hits
}

// nativeResume returns the agent's own resume argv when the launched binary
// is the same harness that recorded the session, else nil (digest handoff).
func nativeResume(bin, client, sid string) []string {
	switch {
	case filepath.Base(bin) == "claude" && client == "claude-code":
		return []string{"--resume", sid}
	case filepath.Base(bin) == "codex" && client == "codex":
		return []string{"resume", sid}
	}
	return nil
}

func handoffPrompt(digest string) string {
	return "Read the session digest at " + digest +
		" to catch up on what a previous coding session in this project did, then continue that work from where it stopped."
}

// runAgent runs the agent interactively (stdio attached) with cwd=dir and
// returns its exit code. Env untouched on purpose: the session must be
// captured by the hooks like any other. Var so tests can stub it.
var runAgent = func(dir, bin string, args ...string) (int, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	if err != nil {
		return 1, err
	}
	return 0, nil
}

// runRun launches a code agent in the current tracked project, continuing a
// previous session natively (same harness) or via a digest-pointer prompt.
func runRun(cwd, configPath string, args []string, out io.Writer) int {
	usage := func() int {
		fmt.Fprintln(out, "usage: memoria run <agent-binary> [--new | --session <id|name> | --last-session]")
		return 1
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return usage()
	}
	bin := args[0]
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(out)
	newSess := fs.Bool("new", false, "start a fresh session")
	session := fs.String("session", "", "continue a specific session (id prefix or name substring)")
	last := fs.Bool("last-session", false, "continue the most recent session")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}
	set := 0
	for _, on := range []bool{*newSess, *session != "", *last} {
		if on {
			set++
		}
	}
	if set > 1 {
		fmt.Fprintln(out, "error: --new, --session and --last-session are mutually exclusive")
		return 1
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	proj := matchProject(cwd, cfg.Projects)
	if proj == "" {
		fmt.Fprintln(out, "error: not inside a tracked project (run memoria bootstrap first)")
		return 1
	}
	if _, err := exec.LookPath(bin); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}

	entries := readSessions(proj)
	var chosen *sessionEntry
	switch {
	case *newSess:
	case *last:
		if len(entries) == 0 {
			fmt.Fprintln(out, "error: no sessions recorded")
			return 1
		}
		chosen = &entries[len(entries)-1]
	case *session != "":
		hits := matchSessions(entries, *session)
		switch {
		case len(hits) == 0:
			fmt.Fprintf(out, "error: no session matches %q\n", *session)
			return 1
		case len(hits) == 1:
			chosen = &hits[0]
		default:
			if !isTTY() {
				fmt.Fprintf(out, "error: %d sessions match %q; be more specific\n", len(hits), *session)
				return 1
			}
			opts := make([]option, len(hits))
			for i, h := range hits {
				opts[i] = option{value: h.sid, label: h.name, desc: h.date + " " + h.sid}
			}
			sid, err := selectOption(fmt.Sprintf("%d sessions match %q", len(hits), *session), opts)
			if err != nil {
				return 1
			}
			for i := range hits {
				if hits[i].sid == sid {
					chosen = &hits[i]
				}
			}
		}
	default:
		// no flags: offer the last session, fall back to fresh on any gap
		if isTTY() && len(entries) > 0 {
			e := entries[len(entries)-1]
			if findDigest(proj, e.sid) != "" {
				v, err := selectOption(fmt.Sprintf("Continue from where the last session stopped? (%s)", e.name), runOptions)
				if err == nil && v == "yes" {
					chosen = &e
				}
			}
		}
	}

	var agentArgs []string
	if chosen != nil {
		digest := findDigest(proj, chosen.sid)
		if digest == "" {
			fmt.Fprintf(out, "error: no digest found for session %s\n", chosen.sid)
			return 1
		}
		if agentArgs = nativeResume(bin, digestClient(digest), chosen.sid); agentArgs == nil {
			agentArgs = []string{handoffPrompt(digest)}
		}
	}
	code, err := runAgent(proj, bin, agentArgs...)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	return code
}
