package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// flat collapses whitespace runs into single spaces. Digest lines are one
// event per line, unbounded length.
func flat(s string) string { return strings.Join(strings.Fields(s), " ") }

// collapse squashes whitespace runs into single spaces and truncates to max
// runes with an ellipsis.
func collapse(s string, max int) string {
	s = flat(s)
	if r := []rune(s); len(r) > max {
		s = string(r[:max]) + "..."
	}
	return s
}

// renderEvent returns the "@hook ..." digest line for an event, or "" for
// events not worth digesting (pre-tool-use, Read, notifications, unknowns).
func renderEvent(name string, payload map[string]any) string {
	str := func(k string) string { s, _ := payload[k].(string); return s }
	switch name {
	case "session-start":
		if s := str("source"); s != "" {
			return "@session-start source: " + s
		}
		return "@session-start"
	case "user-prompt":
		return "@user-prompt '" + flat(str("prompt")) + "'"
	case "post-tool-use":
		return renderTool(payload)
	case "stop":
		if m := flat(str("last_assistant_message")); m != "" {
			return "@stop '" + m + "'"
		}
	case "subagent-stop":
		line := "@subagent-stop"
		if t := str("agent_type"); t != "" {
			line += " " + t
		}
		if m := flat(str("last_assistant_message")); m != "" {
			line += " '" + m + "'"
		}
		if line != "@subagent-stop" {
			return line
		}
	case "pre-compact", "post-compact":
		return "@" + name
	case "session-end":
		if r := str("reason"); r != "" {
			return "@session-end reason: " + r
		}
		return "@session-end"
	}
	return ""
}

// renderTool renders Write/Edit/NotebookEdit/Bash post-tool-use events; other
// tools (Read, Grep, ...) are noise in a digest. Deleted files surface as
// Bash rm lines.
func renderTool(payload map[string]any) string {
	tool, _ := payload["tool_name"].(string)
	input, _ := payload["tool_input"].(map[string]any)
	var detail string
	switch tool {
	case "Write":
		detail, _ = input["file_path"].(string)
	case "Edit", "NotebookEdit":
		tool = "Edit"
		detail, _ = input["file_path"].(string)
	case "Bash":
		if cmd, _ := input["command"].(string); cmd != "" {
			detail = "'" + flat(cmd) + "'"
		}
	default:
		return ""
	}
	if detail == "" {
		return ""
	}
	line := "@post-tool-use " + tool + " " + detail
	if e := toolError(payload["tool_response"]); e != "" {
		line += " error: '" + e + "'"
	}
	return line
}

// toolError extracts an error message from a tool_response.
// ponytail: error/is_error keys only; stderr heuristics if this misses too much
func toolError(resp any) string {
	m, ok := resp.(map[string]any)
	if !ok {
		return ""
	}
	if e, _ := m["error"].(string); e != "" {
		return flat(e)
	}
	if isErr, _ := m["is_error"].(bool); isErr {
		return "unspecified"
	}
	return ""
}

// indexSession appends "DATETIME - SESSION_ID - NAME" to
// <project>/.memoria/sessions.md, naming the session after its first user
// prompt. One entry per session id; later prompts are ignored.
func indexSession(proj, sid, prompt string) error {
	if prompt == "" {
		return nil
	}
	path := filepath.Join(proj, ".memoria", "sessions.md")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(existing), " - "+sid+" - ") {
		return nil
	}
	name := collapse(prompt, 80)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s - %s - %s\n", time.Now().Format(time.RFC3339), sid, name)
	return err
}

// captureHook appends the event as an "@hook ..." line to the session digest
// at <project>/.memoria/sessions/pending/<session_id>.md for tracked
// projects. Untracked project, missing config, or bad payload → silent no-op.
// out carries only additive hook JSON (the pre-PR nudge) — never a block.
func captureHook(name string, hookArgs []string, stdin io.Reader, out io.Writer, configPath string) error {
	client := ""
	if len(hookArgs) >= 2 && hookArgs[0] == "--client" {
		client = hookArgs[1]
	}
	// set by tooling (tests, nested agents) so their sessions aren't captured
	if os.Getenv("MEMORIA_NO_CAPTURE") != "" {
		return nil
	}
	if !slices.Contains(canonicalHooks, name) {
		return nil
	}
	var payload map[string]any
	if err := json.NewDecoder(stdin).Decode(&payload); err != nil {
		return nil
	}
	sid, _ := payload["session_id"].(string)
	cwd, _ := payload["cwd"].(string)
	if sid == "" || cwd == "" || sid != filepath.Base(sid) || sid == "." || sid == ".." {
		return nil
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil
	}
	p, ok := resolveProject(cfg, configPath, cwd)
	if !ok {
		return nil
	}
	proj, projName := p.Path, p.Name
	sourceRoot := ""
	if projName == globalName {
		sourceRoot = cwd
	}
	if name == "user-prompt" {
		prompt, _ := payload["prompt"].(string)
		if err := indexSession(proj, sid, prompt); err != nil {
			return err
		}
	}
	// checked before this event is appended: a PR created from a session whose
	// digest already holds observations means unflushed wiki work
	nudge := client == "claude-code" && name == "post-tool-use" &&
		strings.Contains(bashCmd(payload), "gh pr create") &&
		toolError(payload["tool_response"]) == "" && digestPending(proj, sid)
	if err := appendDigest(proj, projName, sourceRoot, sid, name, client, payload, queuePath(configPath)); err != nil {
		return err
	}
	if nudge {
		hookContext(out, "PostToolUse", "memoria: this session's wiki pages haven't been written yet — "+
			"they normally land only after the chat closes, on whatever branch is checked out then. "+
			"To ship them in this PR: call memoria_consolidate with end_current=true, apply it, and "+
			"commit the wiki changes to this branch (CLI: memoria finalize).")
	}
	if client == "claude-code" && (name == "stop" || name == "session-end") {
		if err := captureTitle(proj, sid); err != nil {
			return err
		}
	}
	if name == "session-end" && cfg.AutoApply {
		autoConsolidate(configPath, proj, projName)
	}
	return nil
}

// bashCmd returns the Bash command of a tool payload, "" for other tools.
func bashCmd(payload map[string]any) string {
	if tool, _ := payload["tool_name"].(string); tool != "Bash" {
		return ""
	}
	input, _ := payload["tool_input"].(map[string]any)
	cmd, _ := input["command"].(string)
	return cmd
}

// digestPending reports whether the session has a pending digest holding real
// observations — work captured but not yet turned into wiki pages.
func digestPending(proj, sid string) bool {
	path, _ := resolveDigestPath(proj, sid)
	b, err := os.ReadFile(path)
	return err == nil && digestHasContent(string(b))
}

// hookContext hands the agent extra context through the hook protocol —
// stdout JSON with exit 0 is additive, never blocking (ADR 0003).
func hookContext(out io.Writer, event, ctx string) {
	_ = json.NewEncoder(out).Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     event,
			"additionalContext": ctx,
		},
	})
}

// autoConsolidate is the auto_apply trigger: session end spawns a detached
// consolidation run (which also writes the sessions/ page and auto-applies).
// Best-effort — a hook must never block or fail the agent, so everything is
// logf-only. Busy project slot = skip; the next session end or cron sweeps it.
func autoConsolidate(configPath, proj, projName string) {
	sPath := statusPath(configPath)
	if st, _ := loadStatus(sPath); st[projName].State == "running" && pidAlive(st[projName].PID) {
		logf("hook", "%s: auto-consolidate skipped, job running (pid %d)", projName, st[projName].PID)
		return
	}
	pid, err := spawnDetached(proj, runLogPath(configPath, projName), "process", "--foreground")
	if err != nil {
		logf("hook", "%s: auto-consolidate spawn: %v", projName, err)
		return
	}
	if err := statusSet(sPath, projName, "running", pid, ""); err != nil {
		logf("hook", "%s: status: %v", projName, err)
	}
	logf("hook", "%s: auto-consolidate spawned pid %d", projName, pid)
}

// incarnationName returns the digest file name of the nth incarnation of a
// session: sid.md, then sid-2.md, sid-3.md for reopens after processing.
func incarnationName(sid string, n int) string {
	if n <= 1 {
		return sid + ".md"
	}
	return fmt.Sprintf("%s-%d.md", sid, n)
}

// maxIncarnation returns the highest incarnation of sid present in dir (0 = none).
func maxIncarnation(dir, sid string) int {
	max := 0
	if _, err := os.Stat(filepath.Join(dir, sid+".md")); err == nil {
		max = 1
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		mid, ok := strings.CutPrefix(e.Name(), sid+"-")
		if !ok {
			continue
		}
		mid, ok = strings.CutSuffix(mid, ".md")
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(mid); err == nil && n > max {
			max = n
		}
	}
	return max
}

// resolveDigestPath picks the live digest file for a session. A pending
// incarnation wins; otherwise a session whose digest was already processed
// gets a fresh numbered incarnation linked to the processed one.
func resolveDigestPath(proj, sid string) (path, continuesFrom string) {
	pending := filepath.Join(proj, ".memoria", "sessions", "pending")
	processed := filepath.Join(proj, ".memoria", "sessions", "processed")
	if n := maxIncarnation(pending, sid); n > 0 {
		return filepath.Join(pending, incarnationName(sid, n)), ""
	}
	if n := maxIncarnation(processed, sid); n > 0 {
		return filepath.Join(pending, incarnationName(sid, n+1)),
			"../processed/" + incarnationName(sid, n)
	}
	return filepath.Join(pending, sid+".md"), ""
}

// appendDigest ensures the digest file exists (frontmatter written on first
// event, whichever hook that is), appends the rendered event line, and keeps
// the central pending queue in sync. sourceRoot is set only for global
// captures: the digest lives under the global root but the frontmatter
// records the session's real source folder.
func appendDigest(proj, projName, sourceRoot, sid, name, client string, payload map[string]any, queueFile string) error {
	line := renderEvent(name, payload)
	if line == "" {
		return nil
	}
	path, continuesFrom := resolveDigestPath(proj, sid)
	now := time.Now().Format(time.RFC3339)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	created := false
	// O_EXCL: only the first event of a session writes the frontmatter
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644); err == nil {
		created = true
		fmProject, fmRoot := projName, proj
		if sourceRoot != "" {
			fmProject, fmRoot = filepath.Base(sourceRoot), sourceRoot
		}
		link := ""
		if continuesFrom != "" {
			link = "continues_from: " + continuesFrom + "\n"
		}
		clientLine := ""
		if client != "" {
			clientLine = "client: " + client + "\n"
		}
		fmt.Fprintf(f, `---
schema_version: 2
kind: session-digest
session_id: %s
project: %s
project_root: %s
%sstarted_at: %s
%s---

`, sid, fmProject, fmRoot, clientLine, now, link)
		f.Close()
	} else if !os.IsExist(err) {
		return err
	}
	if name == "session-end" {
		if err := setFront(path, "ended_at", now); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, line); err != nil {
		return err
	}
	// queue last: a queue failure never blocks the digest, only gets logged
	if created {
		if err := queueAdd(queueFile, projName, path); err != nil {
			return err
		}
		// a new session implicitly ends the project's previous ones
		if err := queueEndOthers(queueFile, projName, path); err != nil {
			return err
		}
	}
	if name == "session-end" {
		return queueMarkEnded(queueFile, projName, path)
	}
	return nil
}

// setFront inserts or updates a "key:" line in the digest's frontmatter.
func setFront(path, key, val string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return nil
	}
	for i := 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], key+":") {
			lines[i] = key + ": " + val
			return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
		}
		if lines[i] == "---" {
			lines = slices.Insert(lines, i, key+": "+val)
			return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
		}
	}
	return nil
}

// claudeTitle returns the live title of a running Claude Code session from
// ~/.claude/sessions/*.json, "" when the file is gone or sid is unknown.
func claudeTitle(sid string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".claude", "sessions")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s struct {
			SessionID string `json:"sessionId"`
			Name      string `json:"name"`
		}
		if json.Unmarshal(b, &s) == nil && s.SessionID == sid && s.Name != "" {
			return s.Name
		}
	}
	return ""
}

// captureTitle copies the agent's live session title into the current digest
// incarnation's frontmatter and the sessions.md NAME slot. Missing title,
// missing digest, or unchanged title are no-ops — hooks are best-effort.
// ponytail: codex skipped — threads.name is null unless manually renamed and
// we have no sqlite driver; shell out to sqlite3 if codex ships real titles
func captureTitle(proj, sid string) error {
	title := collapse(claudeTitle(sid), 80)
	if title == "" {
		return nil
	}
	path, _ := resolveDigestPath(proj, sid)
	front, _ := parseDigest(path)
	if frontKey(front, "title") == title {
		return nil
	}
	if err := setFront(path, "title", title); err != nil {
		return nil
	}
	return renameSession(proj, sid, title)
}

// renameSession rewrites the NAME slot of sid's sessions.md line in place.
func renameSession(proj, sid, title string) error {
	path := filepath.Join(proj, ".memoria", "sessions.md")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		// limit 3: NAME may itself contain " - " (see readSessions)
		parts := strings.SplitN(line, " - ", 3)
		if len(parts) != 3 || parts[1] != sid {
			continue
		}
		if parts[2] == title {
			return nil
		}
		lines[i] = parts[0] + " - " + sid + " - " + title
		return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
	}
	return nil
}
