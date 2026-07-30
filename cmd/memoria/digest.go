package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

//go:embed prompts/digest-prompt.md
var defaultDigestPrompt string

// runDigest compiles one session's digest into a clean episodic page at
// wiki/sessions/<sid>.md (detached by default; --foreground runs inline).
// Internal: spawned by the MCP memoria_digest tool, usable by hand.
func runDigest(cwd, configPath string, args []string, out io.Writer) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(out, "usage: memoria digest <session-id> [--foreground]")
		return 1
	}
	sid := args[0]
	fs := flag.NewFlagSet("digest", flag.ContinueOnError)
	fs.SetOutput(out)
	foreground := fs.Bool("foreground", false, "run the processor in this terminal instead of detaching")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}
	// sid arrives over MCP — never let it name a path
	if sid != filepath.Base(sid) || sid == "." || sid == ".." {
		fmt.Fprintf(out, "error: invalid session id %q\n", sid)
		return 1
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
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
	if !*foreground {
		return detachDigest(cwd, configPath, proj, p.Name, sid, out)
	}
	return digestForeground(cfg, proj, wikiRoot, configPath, p.Name, sid, out)
}

// detachDigest hands the slow LLM call to a detached `digest --foreground`
// child, same status.yaml tracking as process/lint.
func detachDigest(cwd, configPath, proj, projName, sid string, out io.Writer) int {
	if findDigest(proj, sid) == "" {
		fmt.Fprintf(out, "error: no digest found for session %s\n", sid)
		return 1
	}
	sPath := statusPath(configPath)
	if st, _ := loadStatus(sPath); st[projName].State == "running" && pidAlive(st[projName].PID) {
		fmt.Fprintf(out, "error: a background job is already running for %s (pid %d)\n", projName, st[projName].PID)
		return 1
	}
	pid, err := spawnDetached(cwd, runLogPath(configPath, projName), "digest", sid, "--foreground")
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if err := statusSet(sPath, projName, "running", pid, ""); err != nil {
		logf("digest", "%s: status: %v", projName, err)
	}
	logf("digest", "%s: detached pid %d for session %s", projName, pid, sid)
	fmt.Fprintf(out, "Compiling session page in background (pid %d) — follow with memoria status.\n", pid)
	return 0
}

func digestForeground(cfg config, proj, wikiRoot, configPath, projName, sid string, out io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintln(out, "error:", err)
		logf("digest", "%s: %v", projName, err)
		if serr := statusSet(statusPath(configPath), projName, "error", 0, collapse(err.Error(), 300)); serr != nil {
			logf("digest", "%s: status: %v", projName, serr)
		}
		notify(cfg, "memoria", projName+": session page failed — see memoria status")
		return 1
	}

	digestPath := findDigest(proj, sid)
	if digestPath == "" {
		return fail(fmt.Errorf("no digest found for session %s", sid))
	}
	obs, err := os.ReadFile(digestPath)
	if err != nil {
		return fail(err)
	}
	rules, err := loadPromptFile(configPath, "digest-prompt.md", defaultDigestPrompt)
	if err != nil {
		return fail(err)
	}
	pagePath := filepath.Join(wikiRoot, "sessions", sid+".md")
	current, _ := os.ReadFile(pagePath) // missing = first compile

	prompt := buildDigestPrompt(rules, string(current), string(obs))
	fmt.Fprintf(out, "Invoking %s for session %s — this can take a few minutes...\n", cfg.Processor, sid)
	raw, err := invokeProcessor(cfg, proj, prompt)
	if err != nil {
		return fail(err)
	}
	jsonStr, err := extractJSON(raw)
	if err != nil {
		return fail(err)
	}
	var pg struct {
		Title        string   `json:"title"`
		BodyMarkdown string   `json:"body_markdown"`
		Tags         []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &pg); err != nil {
		return fail(fmt.Errorf("processor returned invalid JSON: %w", err))
	}
	if pg.Title == "" || pg.BodyMarkdown == "" {
		return fail(fmt.Errorf("processor returned empty title or body_markdown"))
	}
	body := pg.BodyMarkdown
	if !strings.HasPrefix(strings.TrimSpace(body), "#") {
		body = "# " + pg.Title + "\n\n" + body
	}
	if err := os.MkdirAll(filepath.Dir(pagePath), 0o755); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(pagePath, []byte(renderPage(pg.Tags, body)), 0o644); err != nil {
		return fail(err)
	}
	rel := "sessions/" + sid + ".md"
	commitWiki(cfg, wikiRoot, "session digest", rel, 1)
	fmt.Fprintf(out, "wrote %s\n", pagePath)
	if serr := statusSet(statusPath(configPath), projName, "done", 0, "session page written: "+rel); serr != nil {
		logf("digest", "%s: status: %v", projName, serr)
	}
	notify(cfg, "memoria", projName+": session page updated — "+rel)
	logf("digest", "%s: wrote %s", projName, rel)
	return 0
}

func buildDigestPrompt(rules, current, obs string) string {
	var b strings.Builder
	b.WriteString(rules)
	b.WriteString("\n\n--- CURRENT PAGE ---\n")
	if current == "" {
		b.WriteString("(none)\n")
	} else {
		b.WriteString(current + "\n")
	}
	b.WriteString("\n--- SESSION OBSERVATIONS ---\n" + obs + "\n")
	return b.String()
}
