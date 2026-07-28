package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// runBootstrap registers cwd as a tracked project in the config file,
// gitignores .memoria/ and creates the wiki folder (wikiName, default "wiki";
// custom names are saved in the config). An existing wiki folder is an error.
func runBootstrap(cwd, configPath, wikiName string, out io.Writer) int {
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

	folder := wikiName
	if folder == "" {
		folder = "wiki"
	}
	wikiPath := filepath.Join(cwd, folder)
	if _, err := os.Stat(wikiPath); err == nil {
		// fail before writing anything, so a retry with --wiki works fully
		fmt.Fprintf(out, "error: %s already exists, pick another name with --wiki <name>\n", wikiPath)
		return 1
	}

	if err := addGitignoreEntry(cwd); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if err := os.MkdirAll(wikiPath, 0o755); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if err := os.WriteFile(filepath.Join(wikiPath, ".gitkeep"), nil, 0o644); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}

	cfg.Projects = append(cfg.Projects, project{Name: filepath.Base(cwd), Path: cwd, Wiki: wikiName})
	if err := saveConfig(configPath, cfg); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	fmt.Fprintf(out, "Registered %s (%s)\n", filepath.Base(cwd), cwd)
	return 0
}

// addGitignoreEntry appends ".memoria/" to <proj>/.gitignore, creating the
// file if missing; no-op when the entry is already there.
func addGitignoreEntry(proj string) error {
	path := filepath.Join(proj, ".gitignore")
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == ".memoria/" {
			return nil
		}
	}
	s := string(b)
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return os.WriteFile(path, []byte(s+".memoria/\n"), 0o644)
}
