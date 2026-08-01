package main

import (
	"os"
	"path/filepath"
	"syscall"
)

// withFlock serializes read-modify-write of a shared yaml file across
// concurrent processes (hooks from parallel sessions, the cron's process
// --all) via a sidecar .lock file. Closing the fd releases the lock.
func withFlock(path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	return fn()
}

// writeFileAtomic writes via temp file + rename so lock-free readers never
// see a torn file. Callers hold the flock, so the fixed temp name is safe.
func writeFileAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
