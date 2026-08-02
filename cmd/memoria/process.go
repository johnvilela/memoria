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

//go:embed prompts/wiki-prompt.md
var defaultWikiPrompt string

// appended by Go, never stored in the editable prompt file, so user edits
// can't break parsing
const jsonContract = `Output ONLY a ConsolidatedBatch JSON object, no code fences, no commentary:
{"pages":[{"path":"...","title":"...","tags":["tag-1"],"body_markdown":"markdown body"}]}
"path" is relative to the wiki root, always ending in .md. Valid targets: "index.md", the suggested categories concepts/, decisions/, gotchas/, rules/, sessions/, and any other top-level folder already present in the CURRENT WIKI section. Do NOT invent new top-level folders; trash/, _global/ and dot-folders are reserved.
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
	// digest byte length at the moment the prompt was built, so apply can tell
	// whether a still-live session appended events the processor never saw.
	// Absent in proposals written before this field existed.
	Sizes map[string]int64 `json:"session_sizes,omitempty"`
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
	warnReservedDirs(wikiRoot, out)

	if *inspect {
		return inspectProcess(configPath, p.Name, out)
	}
	if *apply {
		return applyProposal(cfg, proj, wikiRoot, proposalPath, queuePath(configPath), p.Name, out)
	}
	if !*foreground {
		return detachProcess(cfg, cwd, configPath, p.Name, out)
	}
	return generateProposal(cfg, proj, wikiRoot, proposalPath, configPath, p.Name, out)
}

// detachProcess hands the slow part (invoking the LLM) to a detached child
// running `process --foreground`, so the terminal and any active agent stay
// free. Progress is tracked in status.yaml (see memoria status).
func detachProcess(cfg config, cwd, configPath, projName string, out io.Writer) int {
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
	if cfg.AutoApply {
		fmt.Fprintln(out, "Follow with: memoria process --inspect — pages will be applied automatically")
	} else {
		fmt.Fprintln(out, "Follow with: memoria process --inspect — the proposal lands at .memoria/proposal.json")
	}
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
		// with auto_apply generateProposal already applied — a second apply
		// would fail on the consumed proposal
		if apply && !cfg.AutoApply {
			if code := applyProposal(cfg, root, wikiRoot, proposalPath, queuePath(configPath), p.Name, out); code != 0 {
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
// still exists and carries content, plus their contents. Contentless digests
// are retired on the way past.
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
		if !digestHasContent(string(b)) {
			retireEmptyDigest(qPath, projName, e.Path)
			continue
		}
		sessions = append(sessions, e.Path)
		digests[e.Path] = string(b)
	}
	return sessions, digests, nil
}

// batchSessionIDs returns the session id of every digest in the batch, read
// from frontmatter (authoritative — the filename carries an incarnation
// suffix). Digests predating session_id contribute nothing and simply opt out
// of canonicalization.
func batchSessionIDs(sessions []string) []string {
	var sids []string
	for _, p := range sessions {
		front, _ := parseDigest(p)
		if sid := frontKey(front, "session_id"); sid != "" {
			sids = append(sids, sid)
		}
	}
	return sids
}

// canonicalizeSessionPaths pins each episodic page to its session id. The
// processor names the page after the digest file often enough that the
// incarnation suffix leaks through, and sessions/<sid>-2.md is invisible to
// run, recall and digest — all three look up the bare id. Matching is against
// the batch's known ids, never the shape of the name: a uuid can legitimately
// end in "-400060889612".
//
// A sessions/ page naming no id in the batch is dropped. validPagePath waves
// through any name under sessions/, so this is the only thing standing between
// a processor typo and an orphan page nothing can find.
func canonicalizeSessionPaths(pages []wikiPage, sids []string) (out []wikiPage, dropped []string) {
	taken := map[string]bool{}
	for _, p := range pages {
		name, isSession := strings.CutPrefix(p.Path, "sessions/")
		if !isSession {
			out = append(out, p)
			continue
		}
		name = strings.TrimSuffix(name, ".md")
		match := ""
		for _, sid := range sids {
			if name == sid {
				match = sid
				break
			}
			// "<sid>-2", "<sid>-3", ... — an incarnation of a known session
			if rest, ok := strings.CutPrefix(name, sid+"-"); ok && isDigits(rest) {
				match = sid
			}
		}
		if match == "" {
			dropped = append(dropped, fmt.Sprintf("page %q dropped: names no session in this batch", p.Path))
			continue
		}
		p.Path = "sessions/" + match + ".md"
		if taken[p.Path] {
			dropped = append(dropped, fmt.Sprintf("page %q dropped: another page already claims %s", p.Path, p.Path))
			continue
		}
		taken[p.Path] = true
		out = append(out, p)
	}
	return out, dropped
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// digestHasContent reports whether a digest holds any event beyond the
// session bookends. A session that starts and immediately ends — a resume the
// user backs out of, an abandoned window — leaves only @session-start and
// @session-end; asked to consolidate that, the processor rightly returns zero
// pages, which the caller can only read as a hard failure.
func digestHasContent(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "@") {
			continue
		}
		switch name, _, _ := strings.Cut(line[1:], " "); name {
		case "session-start", "session-end":
		default:
			return true
		}
	}
	return false
}

// retireEmptyDigest clears a contentless digest out of the worklist — same
// archive move a consumed session gets. Without it the entry rides along in
// every later batch and keeps the project's status red. Best-effort: a digest
// left behind is noise, never lost work.
func retireEmptyDigest(qPath, projName, digestPath string) {
	dst := filepath.Join(filepath.Dir(filepath.Dir(digestPath)), "processed", filepath.Base(digestPath))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err == nil {
		_ = os.Rename(digestPath, dst)
	}
	if err := queueRemove(qPath, projName, digestPath); err != nil {
		logf("process", "%s: queue cleanup: %v", projName, err)
	}
	logf("process", "%s: retired empty digest %s", projName, filepath.Base(digestPath))
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
	raw, err := invokeProcessor(cfg, proj, prompt)
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
	// pin session pages before the shared validator: it accepts any name under
	// sessions/, and lint's fix pass legitimately edits pages outside a batch
	proposed, droppedPages := canonicalizeSessionPaths(pp.Pages, batchSessionIDs(sessions))
	pages, dropped := validatePages(proposed, wikiRoot)
	droppedPages = append(droppedPages, dropped...)
	for _, d := range droppedPages {
		fmt.Fprintln(out, "warning:", d)
		logf("process", "%s: warning: %s", projName, d)
	}
	if len(pages) == 0 {
		return fail(fmt.Errorf("proposal has no valid pages (%d dropped)", len(droppedPages)))
	}
	droppedNote := ""
	if len(droppedPages) > 0 {
		droppedNote = fmt.Sprintf(" — dropped %d invalid page(s)", len(droppedPages))
	}

	sizes := make(map[string]int64, len(sessions))
	for _, s := range sessions {
		sizes[s] = int64(len(digests[s]))
	}
	prop := proposal{
		Project:     projName,
		GeneratedAt: time.Now().Format(time.RFC3339),
		Sessions:    sessions,
		Pages:       pages,
		Sizes:       sizes,
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
	if cfg.AutoApply {
		if code := applyProposal(cfg, proj, wikiRoot, proposalPath, queuePath(configPath), projName, out); code != 0 {
			return fail(fmt.Errorf("auto-apply failed — proposal kept at %s", proposalPath))
		}
		done(fmt.Sprintf("applied %d pages from %d sessions", len(prop.Pages), len(sessions)) + droppedNote)
		notify(cfg, "memoria", fmt.Sprintf("Applied %d wiki page(s) for %s", len(prop.Pages), projName))
		logf("process", "%s: auto-applied %d pages from %d sessions", projName, len(prop.Pages), len(sessions))
		return 0
	}
	fmt.Fprintf(out, "Review %s then run: memoria process --apply\n", proposalPath)
	done(fmt.Sprintf("proposal ready: %d pages from %d sessions — review and run memoria process --apply", len(prop.Pages), len(sessions)) + droppedNote)
	notify(cfg, "memoria", "Proposal ready for "+projName+" — review and run memoria process --apply")
	logf("process", "%s: proposal with %d pages from %d sessions", projName, len(prop.Pages), len(sessions))
	return 0
}

func applyProposal(cfg config, proj, wikiRoot, proposalPath, qPath, projName string, out io.Writer) int {
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
	pages, dropped := validatePages(prop.Pages, wikiRoot)
	for _, d := range dropped {
		fmt.Fprintln(out, "warning:", d)
	}
	if len(pages) == 0 {
		fmt.Fprintf(out, "error: proposal has no valid pages (%d dropped)\n", len(dropped))
		return 1
	}
	prop.Pages = pages
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
	archived := 0
	for _, s := range prop.Sessions {
		// only move files that live inside this project
		if !strings.HasPrefix(s, proj+string(filepath.Separator)) {
			continue
		}
		if !digestConsumed(s, prop.Sizes) {
			// a session still live in this project appended while the
			// processor ran — those events were never in the prompt. Leave it
			// queued so the next pass consolidates the whole digest.
			// ponytail: costs one re-consolidation; a session that never stops
			// emitting keeps deferring its archive but gets a fresh page each pass.
			fmt.Fprintf(out, "kept %s — grew during processing, will reprocess\n", filepath.Base(s))
			logf("process", "%s: kept growing digest %s", projName, filepath.Base(s))
			continue
		}
		_ = os.Rename(s, filepath.Join(processedDir, filepath.Base(s)))
		if err := queueRemove(qPath, projName, s); err != nil {
			fmt.Fprintln(out, "warning: queue cleanup:", err)
		}
		archived++
	}
	_ = os.Remove(proposalPath)
	paths := make([]string, len(prop.Pages))
	for i, pg := range prop.Pages {
		paths[i] = pg.Path
	}
	commitWiki(cfg, wikiRoot, "apply proposal", pageSummary(paths), len(paths))
	fmt.Fprintf(out, "Applied %d page(s); %d session(s) moved to processed/\n", len(prop.Pages), archived)
	logf("process", "%s: applied %d pages, %d sessions processed", projName, len(prop.Pages), archived)
	return 0
}

// digestConsumed reports whether the digest still holds exactly what the
// prompt was built from. Digests are append-only, so a byte-length match is
// conclusive — no clock, no mtime granularity. A proposal predating Sizes has
// no entry and is trusted, as are digests outside the map.
func digestConsumed(path string, sizes map[string]int64) bool {
	want, ok := sizes[path]
	if !ok {
		return true
	}
	fi, err := os.Stat(path)
	if err != nil {
		return true // gone already; the rename is a no-op either way
	}
	return fi.Size() == want
}

// validatePages is the trust boundary for LLM output: valid paths only (see
// validPagePath), no traversal, nothing empty. Invalid pages are dropped
// with a reason instead of failing the batch — callers warn and fail only
// when nothing survives.
func validatePages(pages []wikiPage, wikiRoot string) (valid []wikiPage, dropped []string) {
	dirs := wikiDirs(wikiRoot)
	for _, p := range pages {
		switch {
		case p.Title == "" || p.BodyMarkdown == "":
			dropped = append(dropped, fmt.Sprintf("page %q dropped: empty title or body_markdown", p.Path))
		case !validPagePath(p.Path, dirs):
			dropped = append(dropped, fmt.Sprintf("page %q dropped: path outside the wiki structure", p.Path))
		default:
			valid = append(valid, p)
		}
	}
	return valid, dropped
}

// validPagePath accepts index.md, the native categories, and any top-level
// folder already present in the wiki (dirs) — the LLM may not invent new
// ones. trash/, _global/ and dot-prefixed segments are reserved.
func validPagePath(p string, dirs map[string]bool) bool {
	if !strings.HasSuffix(p, ".md") || strings.Contains(p, "..") ||
		strings.HasPrefix(p, "/") || p != path.Clean(p) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if strings.HasPrefix(seg, ".") {
			return false
		}
	}
	if p == "index.md" {
		return true
	}
	first, _, found := strings.Cut(p, "/")
	if !found || first == "trash" || first == "_global" {
		return false
	}
	for _, c := range []string{"concepts", "decisions", "gotchas", "rules", "sessions"} {
		if first == c {
			return true
		}
	}
	return dirs[first]
}

// warnReservedDirs tells the user when a wiki folder shadows a reserved
// name — its pages are read into prompts but memoria will never write there.
func warnReservedDirs(wikiRoot string, out io.Writer) {
	if wikiDirs(wikiRoot)["_global"] {
		fmt.Fprintln(out, "warning: wiki folder _global/ is reserved for memoria; its pages are read but never written")
	}
}

// wikiDirs lists top-level dirs in the wiki root; missing root → empty map.
func wikiDirs(root string) map[string]bool {
	dirs := map[string]bool{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return dirs
	}
	for _, e := range entries {
		if e.IsDir() {
			dirs[e.Name()] = true
		}
	}
	return dirs
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
		fmt.Fprintf(&b, "\n### %s\n", p)
		// a reopened session writes a fresh digest, but its page keeps the bare
		// session id — and applying overwrites that page wholesale. continues_from
		// points at the previous *digest*, so name the wiki page outright rather
		// than leave the model to derive it.
		if note := continuationNote(digests[p], wiki); note != "" {
			fmt.Fprintf(&b, "\n%s\n", note)
		}
		fmt.Fprintf(&b, "\n%s\n", digests[p])
	}
	b.WriteString("\n--- OUTPUT FORMAT ---\n" + jsonContract + "\n")
	return b.String()
}

// continuationNote flags a digest that resumes a session already holding a
// wiki page, naming the page to extend. Empty for a first incarnation or when
// the session has no page yet — nothing to preserve in either case.
func continuationNote(digest string, wiki map[string]string) string {
	front, _ := splitFrontmatter(digest)
	sid := frontKey(front, "session_id")
	if sid == "" || frontKey(front, "continues_from") == "" {
		return ""
	}
	page := "sessions/" + sid + ".md"
	if _, ok := wiki[page]; !ok {
		return ""
	}
	return fmt.Sprintf("(continues session %s — %s in CURRENT WIKI is this same session's"+
		" earlier work. Extend it: keep its existing sections and add this stretch."+
		" Do not re-summarize it shorter.)", sid, page)
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
