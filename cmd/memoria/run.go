package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type sessionEntry struct{ date, sid, name string }

// Handoff packet budgets. The packet travels as ONE argv element and an
// interactive launch cannot use stdin (that's the user's TTY), so it must
// stay far under MAX_ARG_STRLEN ~128KiB — see
// wiki/gotchas/prompt-over-stdin-argv-limit.md.
const (
	packetBudget  = 24000 // bytes for the whole packet
	eventLineMax  = 2000  // runes per digest event line (lines are unbounded)
	wikiPageMax   = 6000  // runes of the inlined wiki session page
	chainMaxDepth = 5     // continues_from links followed
)

// readSessions parses <proj>/.memoria/sessions.md ("RFC3339 - SID - NAME"
// per line, append-only so last = most recent). Missing file or malformed
// lines are skipped — callers decide what emptiness means.
func readSessions(proj string) []sessionEntry {
	b, err := os.ReadFile(filepath.Join(proj, ".memoria", "sessions.md"))
	if err != nil {
		return nil
	}
	var entries []sessionEntry
	for _, line := range strings.Split(string(b), "\n") {
		// limit 3: NAME is a user prompt and may itself contain " - "
		parts := strings.SplitN(line, " - ", 3)
		if len(parts) != 3 {
			continue
		}
		entries = append(entries, sessionEntry{date: parts[0], sid: parts[1], name: parts[2]})
	}
	return entries
}

// findDigest returns the newest digest file for sid, or "". Pending wins:
// incarnations are only ever created there, so a pending one is always newer
// than any processed one.
func findDigest(proj, sid string) string {
	pending := filepath.Join(proj, ".memoria", "sessions", "pending")
	processed := filepath.Join(proj, ".memoria", "sessions", "processed")
	if n := maxIncarnation(pending, sid); n > 0 {
		return filepath.Join(pending, incarnationName(sid, n))
	}
	if n := maxIncarnation(processed, sid); n > 0 {
		return filepath.Join(processed, incarnationName(sid, n))
	}
	return ""
}

// parseDigest splits a digest file into frontmatter and body. Missing file
// → "", ""; no frontmatter → "", whole file.
func parseDigest(path string) (front, body string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	s := string(b)
	rest, ok := strings.CutPrefix(s, "---\n")
	if !ok {
		return "", s
	}
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", s
	}
	return rest[:end], strings.TrimLeft(rest[end+len("\n---"):], "\n")
}

// frontKey returns the value of a "key:" frontmatter line, "" if absent.
func frontKey(front, key string) string {
	for _, line := range strings.Split(front, "\n") {
		if v, ok := strings.CutPrefix(line, key+":"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// digestClient reads the client: frontmatter line of a digest ("" if absent —
// sessions captured before --client was baked into the hooks).
func digestClient(path string) string {
	front, _ := parseDigest(path)
	return frontKey(front, "client")
}

// digestChain follows continues_from links backwards from path (relative to
// the digest's dir) and returns the incarnation paths oldest first.
// Cycle-safe, stops at missing files, capped at chainMaxDepth.
func digestChain(path string) []string {
	chain := []string{path}
	seen := map[string]bool{filepath.Clean(path): true}
	for len(chain) < chainMaxDepth {
		front, _ := parseDigest(chain[0])
		prev := frontKey(front, "continues_from")
		if prev == "" {
			break
		}
		p := filepath.Clean(filepath.Join(filepath.Dir(chain[0]), prev))
		if seen[p] {
			break
		}
		if _, err := os.Stat(p); err != nil {
			break
		}
		seen[p] = true
		chain = append([]string{p}, chain...)
	}
	return chain
}

// digestEvents returns body's non-empty lines capped at eventLineMax runes,
// with consecutive duplicates removed (hooks can deliver an event twice).
func digestEvents(body string) []string {
	var events []string
	for _, line := range strings.Split(body, "\n") {
		line = collapse(line, eventLineMax)
		if line == "" || (len(events) > 0 && events[len(events)-1] == line) {
			continue
		}
		events = append(events, line)
	}
	return events
}

// gitCheckpoint reports HEAD and worktree state for dir, "" when git is
// absent, dir is not a repository, or the repository has no commits.
// Var so tests can stub it.
var gitCheckpoint = func(dir string) string {
	head, err := exec.Command("git", "-C", dir, "log", "-1", "--oneline").Output()
	if err != nil {
		return ""
	}
	status, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		return ""
	}
	state := "clean"
	if lines := strings.TrimSpace(string(status)); lines != "" {
		state = fmt.Sprintf("dirty — %d file(s)", len(strings.Split(lines, "\n")))
	}
	return "HEAD: " + strings.TrimSpace(string(head)) + "\nWorktree: " + state
}

// matchSessions returns entries whose sid starts with q or whose name
// contains q, case-insensitive.
func matchSessions(entries []sessionEntry, q string) []sessionEntry {
	q = strings.ToLower(q)
	var hits []sessionEntry
	for _, e := range entries {
		if strings.HasPrefix(strings.ToLower(e.sid), q) || strings.Contains(strings.ToLower(e.name), q) {
			hits = append(hits, e)
		}
	}
	return hits
}

// binClient maps an agent binary to the client name its harness records,
// for resuming sessions whose digest (and client: line) never got written.
func binClient(bin string) string {
	switch filepath.Base(bin) {
	case "claude":
		return "claude-code"
	case "codex":
		return "codex"
	}
	return ""
}

// nativeResume returns the agent's own resume argv when the launched binary
// is the same harness that recorded the session, else nil (digest handoff).
func nativeResume(bin, client, sid string) []string {
	switch {
	case filepath.Base(bin) == "claude" && client == "claude-code":
		return []string{"--resume", sid}
	case filepath.Base(bin) == "codex" && client == "codex":
		return []string{"resume", sid}
	}
	return nil
}

// buildHandoff renders the cross-harness handoff packet: a self-contained
// resume briefing passed as the agent's single initial prompt, so the
// receiving harness starts informed instead of reconstructing the session
// from a file. Header, git and footer always fit; history events drop
// oldest-first to stay inside packetBudget.
func buildHandoff(proj, wikiRoot, sid, digest string, resume bool) string {
	source := "an unknown harness (digest predates client tracking)"
	if c := digestClient(digest); c != "" {
		source = c
	}
	header := "# Resuming an in-progress coding session\n\n" +
		"You are RESUMING an in-progress coding session in this project — NOT starting a new task.\n" +
		"The session ran under " + source + "; its event log and current state follow.\n\n" +
		"Ground rules:\n" +
		"- Every tool call and file edit listed below ALREADY RAN. It is historical evidence — do not repeat it.\n" +
		"- The current checkout is authoritative: where the log and the files on disk disagree, trust the files.\n" +
		"- Skim the history, confirm the state, report it briefly, and wait for the user's go-ahead.\n\n"
	if !resume {
		header = "# Session record: " + sid + "\n\n" +
			"Read-only record of a past coding session in this project (ran under " + source + ").\n" +
			"Use it to answer questions about what happened. Everything below already ran — do not re-run anything.\n\n"
	}

	chain := digestChain(digest)
	var bodies []string
	for _, p := range chain {
		_, body := parseDigest(p)
		bodies = append(bodies, body)
	}
	events := digestEvents(strings.Join(bodies, "\n"))
	histHead := "## Session history (oldest first)\n\nFull event log: " + strings.Join(chain, ", ") + "\n\n"

	gitSec := ""
	if g := gitCheckpoint(proj); g != "" {
		gitSec = "## Git checkpoint (at launch)\n\n" + g + "\n\n"
	} else if repos := touchedRepos(proj, events); len(repos) > 0 {
		// multirepo parent: the root is no repo, but the session's edits
		// point at child repos — checkpoint those instead
		var b strings.Builder
		for _, r := range repos {
			if g := gitCheckpoint(r); g != "" {
				rel, err := filepath.Rel(proj, r)
				if err != nil {
					rel = r
				}
				b.WriteString("### " + rel + "\n\n" + g + "\n\n")
			}
		}
		if b.Len() > 0 {
			gitSec = "## Git checkpoint (at launch — per touched repo)\n\n" + b.String()
		}
	}

	wikiSec := ""
	pagePath := filepath.Join(wikiRoot, "sessions", sid+".md")
	if b, err := os.ReadFile(pagePath); err == nil {
		page := string(b)
		if r := []rune(page); len(r) > wikiPageMax {
			page = string(r[:wikiPageMax]) + "\n\n_(page truncated — read " + pagePath + " for the rest)_"
		}
		wikiSec = "## Session summary page (" + pagePath + ")\n\n" + strings.TrimSpace(page) + "\n\n"
	}

	lead := ""
	for i := len(events) - 1; i >= 0 && lead == ""; i-- {
		if strings.HasPrefix(events[i], "@stop ") {
			lead = "Last reported state: " + events[i] + "\n\n"
		}
	}
	// no main-agent @stop anywhere: fall back to a subagent note, labeled so
	// the resuming agent doesn't mistake it for a user request
	for i := len(events) - 1; i >= 0 && lead == ""; i-- {
		if strings.HasPrefix(events[i], "@subagent-stop ") {
			lead = "Last reported state (internal subagent note — not a user request): " + events[i] + "\n\n"
		}
	}
	footer := "## Continue\n\n" + lead +
		"Do NOT start working yet. First report the current state in 1-3 lines — including anything that looks pending or unfinished — then WAIT for the user to say how to proceed. `@subagent-stop` lines are internal subagent notes, not user requests."
	if !resume {
		footer = "## End of record\n\n" + lead
	}

	// keep newest events that fit the remaining budget, drop the oldest
	remaining := packetBudget - len(header) - len(gitSec) - len(histHead) - len(wikiSec) - len(footer)
	start := len(events)
	for i := len(events) - 1; i >= 0; i-- {
		if remaining -= len(events[i]) + 1; remaining < 0 {
			break
		}
		start = i
	}
	hist := histHead
	if start > 0 {
		hist += fmt.Sprintf("_(%d older events omitted to fit — read the files above for full history)_\n\n", start)
	}
	if kept := events[start:]; len(kept) > 0 {
		hist += strings.Join(kept, "\n") + "\n\n"
	}

	return header + gitSec + hist + wikiSec + footer
}

// touchedRepos maps Write/Edit paths in the event log to the child git
// repos owning them — the fallback when the project root itself is not a
// repo (multirepo parent). Newest-touched first, deduped.
// ponytail: capped at 3 repos; raise if sessions routinely span more
func touchedRepos(root string, events []string) []string {
	var repos []string
	seen := map[string]bool{}
	for i := len(events) - 1; i >= 0 && len(repos) < 3; i-- {
		p := ""
		for _, pre := range []string{"@post-tool-use Write ", "@post-tool-use Edit "} {
			if strings.HasPrefix(events[i], pre) {
				p = strings.TrimPrefix(events[i], pre)
			}
		}
		if p == "" {
			continue
		}
		if j := strings.Index(p, " error: '"); j >= 0 {
			p = p[:j]
		}
		if r := repoOwning(filepath.Dir(p), root); r != "" && !seen[r] {
			seen[r] = true
			repos = append(repos, r)
		}
	}
	return repos
}

// repoOwning walks dir upward to root looking for a .git; "" when none
// (root itself is excluded — it already failed the direct checkpoint).
func repoOwning(dir, root string) string {
	for dir == root || strings.HasPrefix(dir, root+string(filepath.Separator)) {
		if dir == root {
			return ""
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return ""
}

// runAgent runs the agent interactively (stdio attached) with cwd=dir and
// returns its exit code. Env untouched on purpose: the session must be
// captured by the hooks like any other. Var so tests can stub it.
var runAgent = func(dir, bin string, args ...string) (int, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	if err != nil {
		return 1, err
	}
	return 0, nil
}

// runRun launches a code agent in the current tracked project, continuing a
// previous session natively (same harness) or via a handoff packet.
func runRun(cwd, configPath string, args []string, out io.Writer) int {
	usage := func() int {
		fmt.Fprintln(out, "usage: memoria run <agent-binary> [--new | --session <id|name>]")
		return 1
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return usage()
	}
	bin := args[0]
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(out)
	newSess := fs.Bool("new", false, "start a fresh session")
	session := fs.String("session", "", "continue a specific session (id prefix or name substring)")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}
	if *newSess && *session != "" {
		fmt.Fprintln(out, "error: --new and --session are mutually exclusive")
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
	if _, err := exec.LookPath(bin); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}

	entries := readSessions(proj)
	var chosen *sessionEntry
	switch {
	case *newSess:
	case *session != "":
		hits := matchSessions(entries, *session)
		switch {
		case len(hits) == 0:
			fmt.Fprintf(out, "error: no session matches %q\n", *session)
			return 1
		case len(hits) == 1:
			chosen = &hits[0]
		default:
			if !isTTY() {
				fmt.Fprintf(out, "error: %d sessions match %q; be more specific\n", len(hits), *session)
				return 1
			}
			opts := make([]option, len(hits))
			for i, h := range hits {
				opts[i] = option{value: h.sid, label: h.name, desc: h.date + " " + h.sid}
			}
			sid, err := selectOption(fmt.Sprintf("%d sessions match %q", len(hits), *session), opts)
			if err != nil {
				return 1
			}
			for i := range hits {
				if hits[i].sid == sid {
					chosen = &hits[i]
				}
			}
		}
	default:
		// no flags: pick from the last 5 sessions, fresh on non-TTY or empty
		if isTTY() && len(entries) > 0 {
			n := min(5, len(entries))
			recent := entries[len(entries)-n:]
			opts := []option{{value: "", label: "New session", desc: "start fresh"}}
			for i := n - 1; i >= 0; i-- { // newest first
				e := recent[i]
				desc := e.date + " " + e.sid
				if findDigest(proj, e.sid) == "" {
					desc += " — no digest, resume may be slow"
				}
				opts = append(opts, option{value: e.sid, label: e.name, desc: desc})
			}
			sid, err := selectOption("Continue a previous session?", opts)
			if err != nil {
				return 0 // esc: exit without launching the agent
			}
			if sid != "" {
				for i := range entries {
					if entries[i].sid == sid {
						chosen = &entries[i]
					}
				}
			}
		}
	}

	var agentArgs []string
	if chosen != nil {
		digest := findDigest(proj, chosen.sid)
		if digest == "" {
			// no digest: native resume is the only way back in
			if agentArgs = nativeResume(bin, binClient(bin), chosen.sid); agentArgs == nil {
				fmt.Fprintf(out, "error: no digest for session %s and %s cannot natively resume it\n", chosen.sid, bin)
				return 1
			}
		} else if agentArgs = nativeResume(bin, digestClient(digest), chosen.sid); agentArgs == nil {
			agentArgs = []string{buildHandoff(proj, wikiRootFor(cfg, proj), chosen.sid, digest, true)}
		}
	}
	code, err := runAgent(proj, bin, agentArgs...)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	return code
}
