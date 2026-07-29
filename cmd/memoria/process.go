package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

//go:embed wiki-prompt.md
var defaultWikiPrompt string

// appended by Go, never stored in the editable prompt file, so user edits
// can't break parsing
const jsonContract = `Output ONLY a ConsolidatedBatch JSON object, no code fences, no commentary:
{"pages":[{"path":"...","title":"...","tags":["tag-1"],"body_markdown":"markdown body"}]}
"path" is relative to the wiki root and encodes the kind: "index.md" or under concepts/, decisions/, gotchas/, rules/ or sessions/, always ending in .md.
The episodic session page goes at "sessions/<session_id>.md" (session_id from the digest frontmatter).
"body_markdown" is the page body without frontmatter — memoria writes the tags frontmatter itself.
"tags" are 0-5 short kebab-case tags. 1-5 pages.`

type wikiPage struct {
	Path         string   `json:"path"`
	Title        string   `json:"title"`
	BodyMarkdown string   `json:"body_markdown"`
	Tags         []string `json:"tags"`
}

// renderPage prefixes the body with the tags frontmatter every wiki writer
// (apply, seed, digest, MCP write) shares. No tags, no frontmatter.
func renderPage(tags []string, body string) string {
	if len(tags) == 0 {
		return body
	}
	return "---\ntags: [" + strings.Join(tags, ", ") + "]\n---\n\n" + body
}

type proposal struct {
	Project     string     `json:"project"`
	GeneratedAt string     `json:"generated_at"`
	Sessions    []string   `json:"sessions"`
	Pages       []wikiPage `json:"pages"`
}

// spawnDetached re-execs the CLI in its own session so long processor runs
// never block the user's terminal or agent. stdout/stderr land in logFile
// (truncated per run) so `memoria process --inspect` can follow along.
var spawnDetached = func(dir, logFile string, args ...string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	lf, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	defer lf.Close()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Stdout = lf
	cmd.Stderr = lf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, nil
}

// runProcess consolidates a project's ended pending sessions into a wiki
// proposal (detached by default; --foreground runs inline) or applies a
// reviewed proposal (--apply). The LLM never writes files: it returns JSON,
// Go validates and writes.
func runProcess(cwd, configPath string, args []string, out io.Writer) int {
	fs := flag.NewFlagSet("process", flag.ContinueOnError)
	fs.SetOutput(out)
	apply := fs.Bool("apply", false, "write the reviewed proposal to the wiki")
	foreground := fs.Bool("foreground", false, "run the processor in this terminal instead of detaching")
	inspect := fs.Bool("inspect", false, "follow the running background process until it finishes")
	all := fs.Bool("all", false, "sweep every tracked project (the systemd timer's entrypoint)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if *all {
		return processAll(cfg, configPath, *apply, out)
	}
	proj := matchProject(cwd, cfg.Projects)
	if proj == "" {
		fmt.Fprintln(out, "error: not inside a tracked project (run memoria bootstrap first)")
		return 1
	}
	p := projectAt(cfg, proj)
	wikiName := p.Wiki
	if wikiName == "" {
		wikiName = "wiki"
	}
	wikiRoot := filepath.Join(proj, wikiName)
	proposalPath := filepath.Join(proj, ".memoria", "proposal.json")

	if *inspect {
		return inspectProcess(configPath, p.Name, out)
	}
	if *apply {
		return applyProposal(proj, wikiRoot, proposalPath, queuePath(configPath), p.Name, out)
	}
	if !*foreground {
		return detachProcess(cwd, configPath, p.Name, out)
	}
	return generateProposal(cfg, proj, wikiRoot, proposalPath, configPath, p.Name, out)
}

// detachProcess hands the slow part (invoking the LLM) to a detached child
// running `process --foreground`, so the terminal and any active agent stay
// free. Progress is tracked in status.yaml (see memoria status).
func detachProcess(cwd, configPath, projName string, out io.Writer) int {
	sessions, _, err := collectEnded(queuePath(configPath), projName)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if len(sessions) == 0 {
		fmt.Fprintln(out, "Nothing to process — no ended pending sessions for", projName)
		return 0
	}
	sPath := statusPath(configPath)
	if st, _ := loadStatus(sPath); st[projName].State == "running" && pidAlive(st[projName].PID) {
		fmt.Fprintf(out, "error: processing already running for %s (pid %d)\n", projName, st[projName].PID)
		return 1
	}
	pid, err := spawnDetached(cwd, runLogPath(configPath, projName), "process", "--foreground")
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if err := statusSet(sPath, projName, "running", pid, ""); err != nil {
		logf("process", "%s: status: %v", projName, err)
	}
	logf("process", "%s: detached pid %d for %d sessions", projName, pid, len(sessions))
	fmt.Fprintf(out, "Processing %d session(s) in background (pid %d).\n", len(sessions), pid)
	fmt.Fprintln(out, "Follow with: memoria process --inspect — the proposal lands at .memoria/proposal.json")
	return 0
}

// processAll sweeps every tracked project with ended pending sessions — no
// cwd needed. Sequential on purpose: status.yaml and pending.yaml are
// read-modify-write without locking. With apply, proposals are written to the
// wiki immediately after generation (no human review).
func processAll(cfg config, configPath string, apply bool, out io.Writer) int {
	worked, failed := 0, 0
	for _, p := range cfg.Projects {
		root := filepath.Clean(p.Path)
		sessions, _, err := collectEnded(queuePath(configPath), p.Name)
		if err != nil {
			fmt.Fprintln(out, "error:", err)
			failed++
			continue
		}
		if len(sessions) == 0 {
			continue
		}
		sPath := statusPath(configPath)
		if st, _ := loadStatus(sPath); st[p.Name].State == "running" && pidAlive(st[p.Name].PID) {
			fmt.Fprintf(out, "%s: processing already running (pid %d), skipped\n", p.Name, st[p.Name].PID)
			continue
		}
		if err := statusSet(sPath, p.Name, "running", os.Getpid(), ""); err != nil {
			logf("process", "%s: status: %v", p.Name, err)
		}
		wikiName := p.Wiki
		if wikiName == "" {
			wikiName = "wiki"
		}
		wikiRoot := filepath.Join(root, wikiName)
		proposalPath := filepath.Join(root, ".memoria", "proposal.json")
		worked++
		if code := generateProposal(cfg, root, wikiRoot, proposalPath, configPath, p.Name, out); code != 0 {
			failed++
			continue
		}
		if apply {
			if code := applyProposal(root, wikiRoot, proposalPath, queuePath(configPath), p.Name, out); code != 0 {
				failed++
			}
		}
	}
	if worked == 0 {
		fmt.Fprintln(out, "Nothing to process — no ended pending sessions")
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// collectEnded returns the project's ended queue entries whose digest file
// still exists, plus their contents.
func collectEnded(qPath, projName string) (sessions []string, digests map[string]string, err error) {
	queue, err := loadQueue(qPath)
	if err != nil {
		return nil, nil, err
	}
	digests = map[string]string{}
	for _, e := range queue[projName] {
		if !e.Ended {
			continue
		}
		b, err := os.ReadFile(e.Path)
		if err != nil {
			continue // dead entry; the queue keeps it until someone cleans up
		}
		sessions = append(sessions, e.Path)
		digests[e.Path] = string(b)
	}
	return sessions, digests, nil
}

func generateProposal(cfg config, proj, wikiRoot, proposalPath, configPath, projName string, out io.Writer) int {
	// runs detached most of the time — status.yaml is how the outcome
	// reaches the user (memoria status)
	fail := func(err error) int {
		fmt.Fprintln(out, "error:", err)
		logf("process", "%s: %v", projName, err)
		if serr := statusSet(statusPath(configPath), projName, "error", 0, collapse(err.Error(), 300)); serr != nil {
			logf("process", "%s: status: %v", projName, serr)
		}
		notify(cfg, "memoria", projName+": processing failed — see memoria status")
		return 1
	}
	done := func(detail string) {
		if serr := statusSet(statusPath(configPath), projName, "done", 0, detail); serr != nil {
			logf("process", "%s: status: %v", projName, serr)
		}
	}

	sessions, digests, err := collectEnded(queuePath(configPath), projName)
	if err != nil {
		return fail(err)
	}
	if len(sessions) == 0 {
		fmt.Fprintln(out, "Nothing to process — no ended pending sessions for", projName)
		done("nothing to process")
		return 0
	}

	rules, err := loadWikiPrompt(configPath)
	if err != nil {
		return fail(err)
	}
	prompt := buildPrompt(rules, readWiki(wikiRoot), digests)
	fmt.Fprintf(out, "Invoking %s with %d session(s) — this can take a few minutes...\n", cfg.Processor, len(sessions))
	raw, err := invokeProcessor(cfg, prompt)
	if err != nil {
		return fail(err)
	}
	jsonStr, err := extractJSON(raw)
	if err != nil {
		return fail(err)
	}
	var pp struct {
		Pages []wikiPage `json:"pages"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &pp); err != nil {
		return fail(fmt.Errorf("processor returned invalid JSON: %w", err))
	}
	if err := validatePages(pp.Pages); err != nil {
		return fail(err)
	}

	prop := proposal{
		Project:     projName,
		GeneratedAt: time.Now().Format(time.RFC3339),
		Sessions:    sessions,
		Pages:       pp.Pages,
	}
	b, err := json.MarshalIndent(prop, "", "  ")
	if err != nil {
		return fail(err)
	}
	if err := os.WriteFile(proposalPath, append(b, '\n'), 0o644); err != nil {
		return fail(err)
	}
	fmt.Fprintf(out, "Proposal from %d session(s):\n", len(sessions))
	for _, pg := range prop.Pages {
		fmt.Fprintf(out, "  %s — %s\n", pg.Path, pg.Title)
	}
	fmt.Fprintf(out, "Review %s then run: memoria process --apply\n", proposalPath)
	done(fmt.Sprintf("proposal ready: %d pages from %d sessions — review and run memoria process --apply", len(prop.Pages), len(sessions)))
	notify(cfg, "memoria", "Proposal ready for "+projName+" — review and run memoria process --apply")
	logf("process", "%s: proposal with %d pages from %d sessions", projName, len(prop.Pages), len(sessions))
	return 0
}

func applyProposal(proj, wikiRoot, proposalPath, qPath, projName string, out io.Writer) int {
	b, err := os.ReadFile(proposalPath)
	if err != nil {
		fmt.Fprintln(out, "error: no proposal found — run memoria process first")
		return 1
	}
	var prop proposal
	if err := json.Unmarshal(b, &prop); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if prop.Project != projName {
		fmt.Fprintf(out, "error: proposal is for %q, current project is %q\n", prop.Project, projName)
		return 1
	}
	if err := validatePages(prop.Pages); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	for _, pg := range prop.Pages {
		dst := filepath.Join(wikiRoot, filepath.FromSlash(pg.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
		if err := os.WriteFile(dst, []byte(renderPage(pg.Tags, pg.BodyMarkdown)), 0o644); err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
		fmt.Fprintf(out, "wrote %s\n", dst)
	}
	processedDir := filepath.Join(proj, ".memoria", "sessions", "processed")
	if err := os.MkdirAll(processedDir, 0o755); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	for _, s := range prop.Sessions {
		// only move files that live inside this project
		if !strings.HasPrefix(s, proj+string(filepath.Separator)) {
			continue
		}
		_ = os.Rename(s, filepath.Join(processedDir, filepath.Base(s)))
		if err := queueRemove(qPath, projName, s); err != nil {
			fmt.Fprintln(out, "warning: queue cleanup:", err)
		}
	}
	_ = os.Remove(proposalPath)
	fmt.Fprintf(out, "Applied %d page(s); %d session(s) moved to processed/\n", len(prop.Pages), len(prop.Sessions))
	logf("process", "%s: applied %d pages, %d sessions processed", projName, len(prop.Pages), len(prop.Sessions))
	return 0
}

// validatePages is the trust boundary for LLM output: only .md files inside
// the wiki categories (or index.md), no traversal, nothing empty.
func validatePages(pages []wikiPage) error {
	if len(pages) == 0 {
		return fmt.Errorf("proposal has no pages")
	}
	for _, p := range pages {
		if p.Title == "" || p.BodyMarkdown == "" {
			return fmt.Errorf("page %q: empty title or body_markdown", p.Path)
		}
		if !validPagePath(p.Path) {
			return fmt.Errorf("page path %q outside the wiki structure", p.Path)
		}
	}
	return nil
}

func validPagePath(p string) bool {
	if !strings.HasSuffix(p, ".md") || strings.Contains(p, "..") ||
		strings.HasPrefix(p, "/") || p != path.Clean(p) {
		return false
	}
	if p == "index.md" {
		return true
	}
	for _, c := range []string{"concepts/", "decisions/", "gotchas/", "rules/", "sessions/"} {
		if strings.HasPrefix(p, c) {
			return true
		}
	}
	return false
}

// loadWikiPrompt returns the prompt from the file next to the config if the
// user created one, else the embedded default.
func loadWikiPrompt(configPath string) (string, error) {
	return loadPromptFile(configPath, "wiki-prompt.md", defaultWikiPrompt)
}

func loadPromptFile(configPath, name, def string) (string, error) {
	b, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), name))
	if os.IsNotExist(err) {
		return def, nil
	}
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// readWiki returns every .md file under root keyed by wiki-relative path.
// trash/ is invisible everywhere — prompts, lint, search — unless a caller
// reads it explicitly (search --trash).
func readWiki(root string) map[string]string {
	wiki := map[string]string{}
	trash := filepath.Join(root, "trash")
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p == trash {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".md") {
			return nil
		}
		if b, err := os.ReadFile(p); err == nil {
			rel, _ := filepath.Rel(root, p)
			wiki[filepath.ToSlash(rel)] = string(b)
		}
		return nil
	})
	return wiki
}

func buildPrompt(rules string, wiki, digests map[string]string) string {
	var b strings.Builder
	b.WriteString(rules)
	b.WriteString("\n\n--- CURRENT WIKI ---\n")
	if len(wiki) == 0 {
		b.WriteString("(empty)\n")
	}
	for _, p := range slices.Sorted(maps.Keys(wiki)) {
		fmt.Fprintf(&b, "\n### %s\n\n%s\n", p, wiki[p])
	}
	b.WriteString("\n--- SESSION DIGESTS ---\n")
	for _, p := range slices.Sorted(maps.Keys(digests)) {
		fmt.Fprintf(&b, "\n### %s\n\n%s\n", p, digests[p])
	}
	b.WriteString("\n--- OUTPUT FORMAT ---\n" + jsonContract + "\n")
	return b.String()
}

// extractJSON tolerates fences/chatter around the object.
func extractJSON(s string) (string, error) {
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i < 0 || j <= i {
		return "", fmt.Errorf("no JSON object in processor output (%d bytes)", len(s))
	}
	return s[i : j+1], nil
}
