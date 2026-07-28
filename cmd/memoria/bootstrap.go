package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// runBootstrap registers cwd as a tracked project in the config file.
func runBootstrap(cwd, configPath string, out io.Writer) int {
	cwd = filepath.Clean(cwd)

	cfg, err := loadConfig(configPath)
	if err != nil && !os.IsNotExist(err) {
		// never rewrite a config we couldn't parse
		fmt.Fprintln(out, "error:", err)
		return 1
	}

	for _, p := range cfg.Projects {
		if filepath.Clean(p.Path) == cwd {
			fmt.Fprintf(out, "%s already registered\n", cwd)
			return 0
		}
	}

	cfg.Projects = append(cfg.Projects, project{Name: filepath.Base(cwd), Path: cwd})
	if err := saveConfig(configPath, cfg); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	fmt.Fprintf(out, "Registered %s (%s)\n", filepath.Base(cwd), cwd)
	return 0
}
