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
func captureHook(name string, stdin io.Reader, configPath string) error {
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
	proj := matchProject(cwd, cfg.Projects)
	if proj == "" {
		return nil
	}
	projName := projectAt(cfg, proj).Name
	if name == "user-prompt" {
		prompt, _ := payload["prompt"].(string)
		if err := indexSession(proj, sid, prompt); err != nil {
			return err
		}
	}
	return appendDigest(proj, projName, sid, name, payload, queuePath(configPath))
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
// the central pending queue in sync.
func appendDigest(proj, projName, sid, name string, payload map[string]any, queueFile string) error {
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
		link := ""
		if continuesFrom != "" {
			link = "continues_from: " + continuesFrom + "\n"
		}
		fmt.Fprintf(f, `---
schema_version: 2
kind: session-digest
session_id: %s
project: %s
project_root: %s
started_at: %s
%s---

`, sid, projName, proj, now, link)
		f.Close()
	} else if !os.IsExist(err) {
		return err
	}
	if name == "session-end" {
		if err := setEndedAt(path, now); err != nil {
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

// setEndedAt inserts or updates "ended_at:" in the digest's frontmatter.
func setEndedAt(path, ts string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return nil
	}
	for i := 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "ended_at:") {
			lines[i] = "ended_at: " + ts
			return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
		}
		if lines[i] == "---" {
			lines = slices.Insert(lines, i, "ended_at: "+ts)
			return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
		}
	}
	return nil
}
