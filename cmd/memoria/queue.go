package main

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// pending.yaml next to the config: the central worklist the session-processing
// cron reads. Grouped by project name; the cron removes entries it processed.
type pendingEntry struct {
	Path  string `yaml:"path"`
	Ended bool   `yaml:"ended,omitempty"`
}

func queuePath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "pending.yaml")
}

func loadQueue(path string) (map[string][]pendingEntry, error) {
	q := map[string][]pendingEntry{}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return q, nil
	}
	if err != nil {
		return nil, err
	}
	return q, yaml.Unmarshal(b, &q)
}

func saveQueue(path string, q map[string][]pendingEntry) error {
	b, err := yaml.Marshal(q)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// queueAdd registers a pending digest under its project; already-present
// paths are left untouched.
// ponytail: no file lock — concurrent hooks could lose an update; flock if it ever bites
func queueAdd(path, projName, digestPath string) error {
	q, err := loadQueue(path)
	if err != nil {
		return err
	}
	for _, e := range q[projName] {
		if e.Path == digestPath {
			return nil
		}
	}
	q[projName] = append(q[projName], pendingEntry{Path: digestPath})
	return saveQueue(path, q)
}

// queueEndOthers marks every entry of the project as ended except keepPath —
// starting a new session implicitly ends the previous ones (crashed or
// abandoned sessions would otherwise stay pending forever).
func queueEndOthers(path, projName, keepPath string) error {
	q, err := loadQueue(path)
	if err != nil {
		return err
	}
	changed := false
	for i, e := range q[projName] {
		if e.Path != keepPath && !e.Ended {
			q[projName][i].Ended = true
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return saveQueue(path, q)
}

// queueRemove drops a processed entry; an emptied project key disappears.
func queueRemove(path, projName, digestPath string) error {
	q, err := loadQueue(path)
	if err != nil {
		return err
	}
	entries := q[projName][:0]
	for _, e := range q[projName] {
		if e.Path != digestPath {
			entries = append(entries, e)
		}
	}
	if len(entries) == len(q[projName]) {
		return nil
	}
	if len(entries) == 0 {
		delete(q, projName)
	} else {
		q[projName] = entries
	}
	return saveQueue(path, q)
}

// queueMarkEnded flags the entry so the cron knows the session finished; an
// absent entry (queue file deleted mid-session) is created already ended.
func queueMarkEnded(path, projName, digestPath string) error {
	q, err := loadQueue(path)
	if err != nil {
		return err
	}
	for i, e := range q[projName] {
		if e.Path == digestPath {
			if e.Ended {
				return nil
			}
			q[projName][i].Ended = true
			return saveQueue(path, q)
		}
	}
	q[projName] = append(q[projName], pendingEntry{Path: digestPath, Ended: true})
	return saveQueue(path, q)
}
