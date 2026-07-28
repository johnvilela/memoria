package main

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type config struct {
	Projects []string `yaml:"projects"`
}

func loadConfig(path string) (config, error) {
	var c config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	return c, yaml.Unmarshal(b, &c)
}

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "memoria", "config.yaml")
}

// matchProject returns the longest tracked project that contains cwd, or "".
func matchProject(cwd string, projects []string) string {
	cwd = filepath.Clean(cwd)
	best := ""
	for _, p := range projects {
		p = filepath.Clean(p)
		if cwd == p || strings.HasPrefix(cwd, p+string(filepath.Separator)) {
			if len(p) > len(best) {
				best = p
			}
		}
	}
	return best
}
