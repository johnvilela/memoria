package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// runBootstrap registers cwd as a tracked project in the config file,
// gitignores .memoria/, creates the wiki folder (wikiName, default "wiki";
// custom names are saved in the config) and offers to seed it from git
// history. An existing wiki folder is an error. seedForeground is the
// internal child mode spawned by --background: seed only, no registration.
func runBootstrap(cwd, configPath, wikiName string, background, seedForeground bool, out io.Writer) int {
	cwd = filepath.Clean(cwd)

	cfg, err := loadConfig(configPath)
	if err != nil && !os.IsNotExist(err) {
		// never rewrite a config we couldn't parse
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if seedForeground {
		return runSeedForeground(cfg, cwd, configPath, out)
	}

	for _, p := range cfg.Projects {
		if filepath.Clean(p.Path) == cwd {
			fmt.Fprintf(out, "%s already registered\n", cwd)
			folder := p.Wiki
			if folder == "" {
				folder = "wiki"
			}
			if len(readWiki(filepath.Join(cwd, folder))) > 0 {
				return 0
			}
			// registered but the wiki never got pages — still offer seeding
			return maybeSeedWiki(cfg, p, configPath, background, out)
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
	return maybeSeedWiki(cfg, project{Name: filepath.Base(cwd), Path: cwd, Wiki: wikiName}, configPath, background, out)
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
