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
			writeAgentsFiles(cwd, folder, out)
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
	writeAgentsFiles(cwd, folder, out)
	return maybeSeedWiki(cfg, project{Name: filepath.Base(cwd), Path: cwd, Wiki: wikiName}, configPath, background, out)
}

const memoriaBlockTmpl = `<!-- memoria:start -->
## Project memory (memoria)

Curated long-term memory from past agent sessions lives in ` + "`%s/`" + `:
decisions made, rules to follow, gotchas hit, concepts explained.
Before non-trivial changes: read ` + "`%s/index.md`" + `, then grep ` + "`%s/`" + `
for keywords. Pages carry YAML ` + "`tags:`" + ` frontmatter for topic lookup.
<!-- memoria:end -->`

// writeAgentsFiles puts the recall instructions into <proj>/AGENTS.md —
// replacing an existing marker block, else appending — and creates a
// CLAUDE.md shim when none exists. Warn-only: registration and seeding must
// survive an unwritable file.
func writeAgentsFiles(proj, folder string, out io.Writer) {
	if err := writeAgentsBlock(proj, folder); err != nil {
		fmt.Fprintln(out, "warning:", err)
		return
	}
	fmt.Fprintln(out, "Wrote project memory instructions to AGENTS.md")
}

func writeAgentsBlock(proj, folder string) error {
	const start, end = "<!-- memoria:start -->", "<!-- memoria:end -->"
	block := fmt.Sprintf(memoriaBlockTmpl, folder, folder, folder)
	path := filepath.Join(proj, "AGENTS.md")
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	s := string(b)
	if i, j := strings.Index(s, start), strings.Index(s, end); i >= 0 && j > i {
		s = s[:i] + block + s[j+len(end):]
	} else {
		if s != "" && !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		if s != "" {
			s += "\n"
		}
		s += block + "\n"
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		return err
	}
	claudePath := filepath.Join(proj, "CLAUDE.md")
	if _, err := os.Stat(claudePath); os.IsNotExist(err) {
		shim := "# CLAUDE.md\n\nRead [AGENTS.md](AGENTS.md) for project context.\n"
		return os.WriteFile(claudePath, []byte(shim), 0o644)
	}
	return nil
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
