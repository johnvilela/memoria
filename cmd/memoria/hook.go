package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// hookFields whitelists what each hook saves. Empty list = timestamp-only
// marker line. Hooks absent from both maps ("other") keep the full payload
// minus noiseKeys.
var hookFields = map[string][]string{
	"session-start": {"source"},
	"user-prompt":   {"prompt"},
	"pre-tool-use":  {"tool_name"},
	"post-tool-use": {"tool_name", "tool_input", "tool_response"},
	"stop":          {"last_assistant_message"},
	"subagent-stop": {"agent_type", "last_assistant_message"},
	"session-end":   {"reason"},
	"pre-compact":   {},
	"post-compact":  {},
}

// skipHooks carry nothing worth saving; no line is written.
var skipHooks = map[string]bool{"notification": true, "subagent-start": true}

// noiseKeys are stripped from "other" payloads: ids, paths, and agent
// bookkeeping present on every event.
var noiseKeys = map[string]bool{
	"session_id": true, "cwd": true, "hook_event_name": true,
	"transcript_path": true, "prompt_id": true, "permission_mode": true,
	"effort": true, "duration_ms": true, "tool_use_id": true,
	"stop_hook_active": true, "background_tasks": true, "session_crons": true,
	"agent_id": true, "agent_transcript_path": true,
}

// filterPayload reduces payload to what's worth keeping for the given hook.
// ok=false means the event should not be logged at all.
func filterPayload(name string, payload map[string]any) (map[string]any, bool) {
	if skipHooks[name] {
		return nil, false
	}
	fields, known := hookFields[name]
	kept := map[string]any{}
	if !known { // "other": unknown shape, keep everything but noise
		for k, v := range payload {
			if !noiseKeys[k] {
				kept[k] = v
			}
		}
	} else {
		for _, k := range fields {
			if v, ok := payload[k]; ok {
				kept[k] = v
			}
		}
	}
	return dropEmpty(kept), true
}

// dropEmpty removes "", false, empty arrays and empty maps, recursively.
// ponytail: false counts as empty — flags like interrupted:false are noise,
// interrupted:true survives.
func dropEmpty(m map[string]any) map[string]any {
	for k, v := range m {
		switch t := v.(type) {
		case nil:
			delete(m, k)
		case string:
			if t == "" {
				delete(m, k)
			}
		case bool:
			if !t {
				delete(m, k)
			}
		case []any:
			if len(t) == 0 {
				delete(m, k)
			}
		case map[string]any:
			if len(dropEmpty(t)) == 0 {
				delete(m, k)
			}
		}
	}
	return m
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
	name := strings.Join(strings.Fields(prompt), " ")
	if r := []rune(name); len(r) > 80 {
		name = string(r[:80]) + "..."
	}
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

// captureHook appends "DATETIME - HOOK_NAME - DATA" (DATA filtered per hook,
// omitted when empty) to <project>/.memoria/sessions/<session_id>.md for
// tracked projects. Untracked project, missing config, or bad payload →
// silent no-op.
func captureHook(name string, stdin io.Reader, configPath string) error {
	if !slices.Contains(canonicalHooks, name) {
		name = "other"
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
	kept, ok := filterPayload(name, payload)
	if !ok {
		return nil
	}
	if name == "user-prompt" {
		prompt, _ := payload["prompt"].(string)
		if err := indexSession(proj, sid, prompt); err != nil {
			return err
		}
	}

	dir := filepath.Join(proj, ".memoria", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	line := fmt.Sprintf("%s - %s", time.Now().Format(time.RFC3339), name)
	if len(kept) > 0 {
		data, err := json.Marshal(kept)
		if err != nil {
			return err
		}
		line += " - " + string(data)
	}
	f, err := os.OpenFile(filepath.Join(dir, sid+".md"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, line)
	return err
}
