package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const logMaxSize = 1 << 20 // 1MB, then rotated to .old

// logPath is a var so tests can redirect it. Empty (no user config dir)
// disables logging.
var logPath = func() string {
	p := defaultConfigPath()
	if p == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(p), "memoria.log")
}()

// logf appends one timestamped line to the debug log. Background work
// (detached digests, cronjobs, hooks) has no visible stdout, so this file is
// the only trail. Every error is ignored: logging must never break the hook.
func logf(component, format string, args ...any) {
	if logPath == "" {
		return
	}
	if info, err := os.Stat(logPath); err == nil && info.Size() > logMaxSize {
		_ = os.Rename(logPath, logPath+".old")
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	msg := collapse(fmt.Sprintf(format, args...), 500)
	fmt.Fprintf(f, "%s [%s] %s\n", time.Now().Format(time.RFC3339), component, msg)
}
