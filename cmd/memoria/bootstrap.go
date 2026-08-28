package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runBootstrap registers cwd as a tracked project in the config file,
// gitignores .memoria/, creates the wiki folder (wikiName, default "wiki";
// custom names are saved in the config) and offers to seed it from git
// history. An existing wiki folder is adopted as-is — registration only,
// seeding offered just when it has no pages; a non-directory at the wiki
// path is an error. seedForeground is the internal child mode spawned by
// --background: seed only, no registration.
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
			warnReservedDirs(filepath.Join(cwd, folder), out)
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
	adopt := false
	if fi, err := os.Stat(wikiPath); err == nil {
		if !fi.IsDir() {
			// fail before writing anything, so a retry with --wiki works fully
			fmt.Fprintf(out, "error: %s exists and is not a folder, pick another name with --wiki <name>\n", wikiPath)
			return 1
		}
		// pre-existing wiki (e.g. the project folder was renamed): adopt it
		adopt = true
	}

	if _, err := os.Stat(filepath.Join(cwd, ".git")); err == nil {
		if err := addGitignoreEntry(cwd); err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
	} else {
		// multirepo parent (or plain folder): still fully supported, but
		// the wiki lives outside version control — say so once
		fmt.Fprintln(out, "warning: "+cwd+" is not a git repository — the wiki will not be versioned")
	}
	if !adopt {
		if err := os.MkdirAll(wikiPath, 0o755); err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
		if err := os.WriteFile(filepath.Join(wikiPath, ".gitkeep"), nil, 0o644); err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
	}

	cfg.Projects = append(cfg.Projects, project{Name: filepath.Base(cwd), Path: cwd, Wiki: wikiName})
	if err := saveConfig(configPath, cfg); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	fmt.Fprintf(out, "Registered %s (%s)\n", filepath.Base(cwd), cwd)
	if adopt {
		fmt.Fprintf(out, "Adopted existing wiki folder %s\n", wikiPath)
		warnReservedDirs(wikiPath, out)
	}
	writeAgentsFiles(cwd, folder, out)
	if adopt && len(readWiki(wikiPath)) > 0 {
		// a wiki with content is never seeded or modified
		return 0
	}
	return maybeSeedWiki(cfg, project{Name: filepath.Base(cwd), Path: cwd, Wiki: wikiName}, configPath, background, out)
}

// runBootstrapGlobal enables global capture: sessions in unregistered folders
// are captured under root (global_path; default: the config folder), wiki at
// <root>/wiki. Default root: the wiki gets its own git repo — never the config
// dir itself, config.yaml can hold an API key. --global-path root: the user's folder,
// git is never touched. Idempotent; re-runs repair the folder structure.
func runBootstrapGlobal(configPath, path string, out io.Writer) int {
	cfg, err := loadConfig(configPath)
	if err != nil && !os.IsNotExist(err) {
		// never rewrite a config we couldn't parse
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if path != "" {
		if path, err = filepath.Abs(path); err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
	}
	wasEnabled, oldRoot := cfg.Global, globalRoot(cfg, configPath)
	cfg.Global, cfg.GlobalPath = true, path
	root := globalRoot(cfg, configPath)

	if wasEnabled && oldRoot == root {
		fmt.Fprintf(out, "global capture already enabled (%s)\n", root)
	} else if wasEnabled {
		fmt.Fprintf(out, "note: global root changed from %s to %s — existing captures stay at %s\n", oldRoot, root, oldRoot)
		// ponytail: no migration — old-root pending entries are processed there by hand
		q, _ := loadQueue(queuePath(configPath))
		stranded := 0
		for _, e := range q[globalName] {
			if !strings.HasPrefix(e.Path, root+string(filepath.Separator)) {
				stranded++
			}
		}
		if stranded > 0 {
			fmt.Fprintf(out, "warning: %d pending global session(s) still reference the old root — run memoria process there first\n", stranded)
		}
	}

	wikiPath := filepath.Join(root, "wiki")
	if err := os.MkdirAll(wikiPath, 0o755); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	gitkeep := filepath.Join(wikiPath, ".gitkeep")
	if _, err := os.Stat(gitkeep); os.IsNotExist(err) {
		if err := os.WriteFile(gitkeep, nil, 0o644); err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
	}
	if path == "" {
		// default root: the wiki tracks itself in its own repo
		if _, err := os.Stat(filepath.Join(wikiPath, ".git")); os.IsNotExist(err) {
			if b, err := exec.Command("git", "init", wikiPath).CombinedOutput(); err != nil {
				fmt.Fprintf(out, "warning: git init: %v (%s)\n", err, collapse(string(b), 200))
			}
		}
		// surface git problems (missing identity) now, not on the first silent auto-commit
		if err := commitWikiGit(wikiPath, "docs(wiki): init global wiki"); err != nil && err != errNothingToCommit {
			fmt.Fprintln(out, "warning: initial wiki commit:", err)
		}
	}
	if err := saveConfig(configPath, cfg); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	fmt.Fprintf(out, "Global capture enabled — sessions in unregistered folders are captured to %s (wiki: %s)\n", root, wikiPath)
	if path == "" {
		fmt.Fprintln(out, "Wiki changes are tracked in their own git repo")
	} else {
		fmt.Fprintln(out, "Git in this folder is yours to manage — memoria will not commit")
	}
	return 0
}

// applyGlobalSetting is setup's --global/--global-path handler: disable global
// capture (keeping global_path for a later re-enable), enable it, or move the
// root — enable and move reuse the bootstrap ensure logic. It reloads the
// config so earlier setup writes in the same run aren't clobbered.
func applyGlobalSetting(enable, enableSet bool, path string, pathSet bool, configPath string, out io.Writer) int {
	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if enableSet && !enable {
		if pathSet {
			fmt.Fprintln(out, "error: --global-path cannot be combined with --global=false")
			return 1
		}
		if !cfg.Global {
			fmt.Fprintln(out, "global capture already disabled")
			return 0
		}
		cfg.Global = false // global_path is kept so re-enabling finds the same root
		if err := saveConfig(configPath, cfg); err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
		fmt.Fprintln(out, "Global capture disabled — captures and wiki stay at", globalRoot(cfg, configPath))
		return 0
	}
	if pathSet && !enableSet && !cfg.Global {
		fmt.Fprintln(out, "error: global capture is off — enable it with --global")
		return 1
	}
	if !pathSet {
		path = cfg.GlobalPath // bare --global keeps the stored root
	}
	return runBootstrapGlobal(configPath, path, out)
}

const memoriaBlockTmpl = `<!-- memoria:start -->
## Project memory (memoria)

Curated long-term memory from past agent sessions lives in ` + "`%s/`" + `:
decisions made, rules to follow, gotchas hit, concepts explained.
Before non-trivial changes: read ` + "`%s/index.md`" + `, then grep ` + "`%s/`" + `
for keywords. Pages carry YAML ` + "`tags:`" + ` frontmatter for topic lookup.
Prefer the memoria MCP tools when available: memoria_search,
memoria_recall, memoria_digest, memoria_consolidate, memoria_lint,
memoria_write_page, memoria_delete_page.
To recall what a past session did, call memoria_recall (read-only).
memoria_digest WRITES the session's wiki page — only when the user
asks to save the session.
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
