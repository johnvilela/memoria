package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// captureHook appends "DATETIME - HOOK_NAME - DATA" to
// <project>/.memoria/sessions/<session_id>.md for tracked projects.
// Untracked project, missing config, or bad payload → silent no-op.
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

	dir := filepath.Join(proj, ".memoria", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, sid+".md"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s - %s - %s\n", time.Now().Format(time.RFC3339), name, data)
	return err
}
