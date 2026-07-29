package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

//go:embed lint-prompt.md
var defaultLintPrompt string

// appended by Go, never stored in the editable prompt file, so user edits
// can't break parsing
const lintContract = `Output ONLY a JSON object, no code fences, no commentary:
{"findings":[{"kind":"contradiction"|"stale"|"duplicate","severity":"warning"|"info","message":"...","pages":["path/a.md","path/b.md"]}]}
"findings" may be []. Page paths verbatim from the input.`

const lintFixPrompt = `You resolve lint findings in a personal coding-knowledge wiki.
Given the findings and the full content of the pages involved, return the
smallest set of page changes that removes the conflict: update a page to fix a
contradiction, delete a page that is stale or fully duplicated elsewhere.
Only touch the pages listed in the findings.

Output ONLY a JSON object, no code fences, no commentary:
{"pages":[{"action":"update"|"create"|"delete","path":"...","title":"...","content":"full markdown"}]}
"path" is relative to the wiki root. "delete" needs only "path"; every other
action needs a non-empty title and content.`

type lintFinding struct {
	Kind     string   `json:"kind"`
	Severity string   `json:"severity"`
	Message  string   `json:"message"`
	Pages    []string `json:"pages"`
}

// lintPage keeps the action/content shape of the lint fix contract — the
// consolidation pipeline moved to wikiPage's body_markdown/tags shape.
type lintPage struct {
	Action  string `json:"action"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

type lintDenied struct {
	lintFinding
	Reason string `json:"reason"`
}

// runLint audits the wiki for contradictions via the configured processor
// (detached by default; --foreground runs inline). The single report at
// .memoria/lint.jsonl is overwritten by each run; --review prints it,
// --apply resolves it and updates the wiki, --deny rejects it with a reason
// that future lint runs see.
func runLint(cwd, configPath string, args []string, out io.Writer) int {
	fs := flag.NewFlagSet("lint", flag.ContinueOnError)
	fs.SetOutput(out)
	review := fs.Bool("review", false, "print the findings of the lint report")
	apply := fs.Bool("apply", false, "resolve the lint report via the processor and update the wiki")
	deny := fs.String("deny", "", "reject the lint report, recording why (fed back to future lint runs)")
	denySet := false
	foreground := fs.Bool("foreground", false, "run the processor in this terminal instead of detaching")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	fs.Visit(func(f *flag.Flag) { denySet = denySet || f.Name == "deny" })
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
	lintPath := filepath.Join(proj, ".memoria", "lint.jsonl")
	deniedPath := filepath.Join(proj, ".memoria", "lint-denied.jsonl")

	switch {
	case denySet:
		return lintDeny(lintPath, deniedPath, *deny, out)
	case *apply:
		return lintApply(cfg, wikiRoot, lintPath, out)
	case *review:
		return lintReview(lintPath, out)
	case !*foreground:
		return detachLint(cwd, configPath, wikiRoot, p.Name, out)
	default:
		return generateLintReport(cfg, configPath, wikiRoot, lintPath, deniedPath, p.Name, out)
	}
}

// detachLint mirrors detachProcess: the LLM call runs in a detached child so
// the terminal stays free. Shares the per-project status entry with process —
// one background job per project at a time.
func detachLint(cwd, configPath, wikiRoot, projName string, out io.Writer) int {
	if len(readWiki(wikiRoot)) < 2 {
		fmt.Fprintln(out, "Nothing to lint — the wiki needs at least two pages")
		return 0
	}
	sPath := statusPath(configPath)
	if st, _ := loadStatus(sPath); st[projName].State == "running" && pidAlive(st[projName].PID) {
		fmt.Fprintf(out, "error: processing already running for %s (pid %d)\n", projName, st[projName].PID)
		return 1
	}
	pid, err := spawnDetached(cwd, runLogPath(configPath, projName), "lint", "--foreground")
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if err := statusSet(sPath, projName, "running", pid, ""); err != nil {
		logf("lint", "%s: status: %v", projName, err)
	}
	logf("lint", "%s: detached pid %d", projName, pid)
	fmt.Fprintf(out, "Linting the wiki in background (pid %d).\n", pid)
	fmt.Fprintln(out, "Follow with: memoria status — the report lands at .memoria/lint.jsonl")
	return 0
}

func generateLintReport(cfg config, configPath, wikiRoot, lintPath, deniedPath, projName string, out io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintln(out, "error:", err)
		logf("lint", "%s: %v", projName, err)
		if serr := statusSet(statusPath(configPath), projName, "error", 0, collapse(err.Error(), 300)); serr != nil {
			logf("lint", "%s: status: %v", projName, serr)
		}
		notify(cfg, "memoria", projName+": lint failed — see memoria status")
		return 1
	}
	done := func(detail string) {
		if serr := statusSet(statusPath(configPath), projName, "done", 0, detail); serr != nil {
			logf("lint", "%s: status: %v", projName, serr)
		}
	}

	wiki := readWiki(wikiRoot)
	if len(wiki) < 2 {
		fmt.Fprintln(out, "Nothing to lint — the wiki needs at least two pages")
		done("nothing to lint")
		return 0
	}
	rules, err := loadPromptFile(configPath, "lint-prompt.md", defaultLintPrompt)
	if err != nil {
		return fail(err)
	}
	raw, err := invokeProcessor(cfg, buildLintPrompt(rules, wiki, readDenied(deniedPath)))
	if err != nil {
		return fail(err)
	}
	jsonStr, err := extractJSON(raw)
	if err != nil {
		return fail(err)
	}
	var rep struct {
		Findings []lintFinding `json:"findings"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &rep); err != nil {
		return fail(fmt.Errorf("processor returned invalid JSON: %w", err))
	}
	if err := validateFindings(rep.Findings, wiki); err != nil {
		return fail(err)
	}
	if len(rep.Findings) == 0 {
		_ = os.Remove(lintPath) // a clean run invalidates any older report
		fmt.Fprintln(out, "No conflicts found — the wiki is internally consistent.")
		done("lint: no conflicts found")
		notify(cfg, "memoria", "Lint clean for "+projName+" — no conflicts found")
		return 0
	}

	if err := os.MkdirAll(filepath.Dir(lintPath), 0o755); err != nil {
		return fail(err)
	}
	var b strings.Builder
	for _, f := range rep.Findings {
		line, err := json.Marshal(f)
		if err != nil {
			return fail(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(lintPath, []byte(b.String()), 0o644); err != nil {
		return fail(err)
	}
	fmt.Fprintf(out, "Lint report with %d finding(s): %s\n", len(rep.Findings), lintPath)
	printFindings(out, rep.Findings)
	fmt.Fprintln(out, "Review with: memoria lint --review — resolve with: memoria lint --apply — reject with: memoria lint --deny \"why\"")
	done(fmt.Sprintf("lint report ready: %d finding(s) — review with memoria lint --review", len(rep.Findings)))
	notify(cfg, "memoria", fmt.Sprintf("Lint found %d conflict(s) in %s — review with memoria lint --review", len(rep.Findings), projName))
	logf("lint", "%s: %d findings in %s", projName, len(rep.Findings), lintPath)
	return 0
}

// lintReview prints the findings of the current report.
func lintReview(lintPath string, out io.Writer) int {
	findings, err := readFindings(lintPath)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	fmt.Fprintf(out, "%s — %d finding(s):\n", lintPath, len(findings))
	printFindings(out, findings)
	return 0
}

// lintApply feeds the report's findings plus the full conflicting pages back
// to the processor and writes the validated fix to the wiki, consuming the
// report. The LLM never writes files: it returns JSON, Go validates and
// writes, and deletes are confined to pages the findings name.
func lintApply(cfg config, wikiRoot, lintPath string, out io.Writer) int {
	findings, err := readFindings(lintPath)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	wiki := readWiki(wikiRoot)
	if err := validateFindings(findings, wiki); err != nil {
		fmt.Fprintln(out, "error: report invalid:", err)
		return 1
	}
	if len(findings) == 0 {
		_ = os.Remove(lintPath)
		fmt.Fprintln(out, "Report has no findings — nothing to apply.")
		return 0
	}

	raw, err := invokeProcessor(cfg, buildLintFixPrompt(findings, wiki))
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	jsonStr, err := extractJSON(raw)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	var fix struct {
		Pages []lintPage `json:"pages"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &fix); err != nil {
		fmt.Fprintln(out, "error: processor returned invalid JSON:", err)
		return 1
	}
	if err := validateLintFix(fix.Pages, findings); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	for _, pg := range fix.Pages {
		dst := filepath.Join(wikiRoot, filepath.FromSlash(pg.Path))
		if pg.Action == "delete" {
			if err := os.Remove(dst); err != nil {
				fmt.Fprintln(out, "error:", err)
				return 1
			}
			fmt.Fprintf(out, "deleted %s\n", dst)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
		if err := os.WriteFile(dst, []byte(pg.Content), 0o644); err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
		fmt.Fprintf(out, "wrote %s\n", dst)
	}
	_ = os.Remove(lintPath)
	fmt.Fprintf(out, "Applied %d change(s); report consumed.\n", len(fix.Pages))
	logf("lint", "applied %d changes from %s", len(fix.Pages), lintPath)
	return 0
}

// lintDeny rejects the current report: findings move to lint-denied.jsonl
// with the user's reason, which future lint runs show the processor so the
// same false positives don't come back.
func lintDeny(lintPath, deniedPath, reason string, out io.Writer) int {
	if strings.TrimSpace(reason) == "" {
		fmt.Fprintln(out, "error: --deny needs a reason, e.g. memoria lint --deny \"pages cover different things\"")
		return 1
	}
	findings, err := readFindings(lintPath)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	f, err := os.OpenFile(deniedPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	for _, fd := range findings {
		line, _ := json.Marshal(lintDenied{lintFinding: fd, Reason: reason})
		if _, err := f.Write(append(line, '\n')); err != nil {
			f.Close()
			fmt.Fprintln(out, "error:", err)
			return 1
		}
	}
	if err := f.Close(); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	_ = os.Remove(lintPath)
	fmt.Fprintf(out, "Denied %d finding(s); future lint runs will see why.\n", len(findings))
	logf("lint", "denied %d findings: %s", len(findings), collapse(reason, 120))
	return 0
}

// validateFindings is the trust boundary for lint output: known kinds and
// severities only, and every cited page must actually exist in the wiki.
func validateFindings(findings []lintFinding, wiki map[string]string) error {
	for _, f := range findings {
		if f.Kind != "contradiction" && f.Kind != "stale" && f.Kind != "duplicate" {
			return fmt.Errorf("finding %q: invalid kind %q", f.Message, f.Kind)
		}
		if f.Severity != "warning" && f.Severity != "info" {
			return fmt.Errorf("finding %q: invalid severity %q", f.Message, f.Severity)
		}
		if f.Message == "" || len(f.Pages) == 0 {
			return fmt.Errorf("finding with empty message or pages")
		}
		for _, p := range f.Pages {
			if _, ok := wiki[p]; !ok {
				return fmt.Errorf("finding %q cites unknown page %q", f.Message, p)
			}
		}
	}
	return nil
}

// validateLintFix reuses the wiki path rules and additionally confines
// deletes to pages the findings name — the fix pass may not remove
// unrelated wiki content.
func validateLintFix(pages []lintPage, findings []lintFinding) error {
	if len(pages) == 0 {
		return fmt.Errorf("fix has no pages")
	}
	cited := map[string]bool{}
	for _, f := range findings {
		for _, p := range f.Pages {
			cited[p] = true
		}
	}
	for _, p := range pages {
		if !validPagePath(p.Path) {
			return fmt.Errorf("page path %q outside the wiki structure", p.Path)
		}
		switch p.Action {
		case "create", "update":
			if p.Title == "" || p.Content == "" {
				return fmt.Errorf("page %q: empty title or content", p.Path)
			}
		case "delete":
			if !cited[p.Path] {
				return fmt.Errorf("delete of %q not backed by any finding", p.Path)
			}
		default:
			return fmt.Errorf("page %q: invalid action %q", p.Path, p.Action)
		}
	}
	return nil
}

func buildLintPrompt(rules string, wiki map[string]string, denied []lintDenied) string {
	var b strings.Builder
	b.WriteString(rules)
	b.WriteString("\n\n--- PAGE PREVIEWS ---\n")
	for _, p := range slices.Sorted(maps.Keys(wiki)) {
		preview := []rune(wiki[p])
		if len(preview) > 400 {
			preview = preview[:400]
		}
		fmt.Fprintf(&b, "\n### %s\n\n%s\n", p, string(preview))
	}
	if len(denied) > 0 {
		b.WriteString("\n--- PREVIOUSLY DENIED FINDINGS ---\nThe user rejected these findings; do NOT report them again. Each 'reason' explains why:\n\n")
		for _, d := range denied {
			line, _ := json.Marshal(d)
			b.Write(line)
			b.WriteByte('\n')
		}
	}
	b.WriteString("\n--- OUTPUT FORMAT ---\n" + lintContract + "\n")
	return b.String()
}

func buildLintFixPrompt(findings []lintFinding, wiki map[string]string) string {
	var b strings.Builder
	b.WriteString(lintFixPrompt)
	b.WriteString("\n\n--- FINDINGS ---\n")
	pages := map[string]bool{}
	for _, f := range findings {
		line, _ := json.Marshal(f)
		b.Write(line)
		b.WriteByte('\n')
		for _, p := range f.Pages {
			pages[p] = true
		}
	}
	b.WriteString("\n--- PAGES ---\n")
	for _, p := range slices.Sorted(maps.Keys(pages)) {
		fmt.Fprintf(&b, "\n### %s\n\n%s\n", p, wiki[p])
	}
	return b.String()
}

func readFindings(path string) ([]lintFinding, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("no lint report at %s — run memoria lint first", path)
	}
	if err != nil {
		return nil, err
	}
	var findings []lintFinding
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var f lintFinding
		if err := json.Unmarshal([]byte(line), &f); err != nil {
			return nil, fmt.Errorf("%s: bad jsonl line: %w", path, err)
		}
		findings = append(findings, f)
	}
	return findings, nil
}

// readDenied returns past denials; a missing or corrupt file just means none.
func readDenied(path string) []lintDenied {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var denied []lintDenied
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var d lintDenied
		if err := json.Unmarshal([]byte(line), &d); err == nil {
			denied = append(denied, d)
		}
	}
	return denied
}

func printFindings(out io.Writer, findings []lintFinding) {
	for _, f := range findings {
		fmt.Fprintf(out, "  [%s/%s] %s\n          %s\n", f.Kind, f.Severity, f.Message, strings.Join(f.Pages, " ↔ "))
	}
}
