package main

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type project struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
	Wiki string `yaml:"wiki,omitempty"` // wiki folder name; empty means "wiki"
}

type config struct {
	Projects      []project `yaml:"projects"`
	Processor     string    `yaml:"processor,omitempty"`      // AI provider that processes sessions into wiki/memories
	GeminiAPIKey  string    `yaml:"gemini_api_key,omitempty"` // only when processor is gemini
	Notifications bool      `yaml:"notifications,omitempty"`  // desktop notification when background processing finishes
	Cron          string    `yaml:"cron,omitempty"`           // schedule input, verbatim (systemd timer runs process --all)
	CronApply     bool      `yaml:"cron_apply,omitempty"`     // timer applies proposals without review
	AutoApply     bool      `yaml:"auto_apply,omitempty"`     // autopilot: session end consolidates, proposals and lint fixes apply without review
}

func loadConfig(path string) (config, error) {
	var c config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	return c, yaml.Unmarshal(b, &c)
}

func saveConfig(path string, c config) error {
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// 0600: the config can hold an API key
	return os.WriteFile(path, b, 0o600)
}

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "memoria", "config.yaml")
}

// projectAt returns the config entry for a project root (fallback: bare name).
func projectAt(cfg config, root string) project {
	for _, p := range cfg.Projects {
		if filepath.Clean(p.Path) == root {
			return p
		}
	}
	return project{Name: filepath.Base(root), Path: root}
}

// wikiRootFor returns the project's wiki folder (project.Wiki, default "wiki").
func wikiRootFor(cfg config, proj string) string {
	name := projectAt(cfg, proj).Wiki
	if name == "" {
		name = "wiki"
	}
	return filepath.Join(proj, name)
}

// matchProject returns the longest tracked project path that contains cwd, or "".
func matchProject(cwd string, projects []project) string {
	cwd = filepath.Clean(cwd)
	best := ""
	for _, p := range projects {
		path := filepath.Clean(p.Path)
		if cwd == path || strings.HasPrefix(cwd, path+string(filepath.Separator)) {
			if len(path) > len(best) {
				best = path
			}
		}
	}
	return best
}
