package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

// finalizeSession marks a still-open session ended — the same state the
// session-end hook leaves behind: ended_at stamped, queue entry flagged. The
// chat can keep going; once the digest is processed, later events open a fresh
// incarnation (resolveDigestPath).
func finalizeSession(configPath, projName, digestPath string) error {
	if err := setFront(digestPath, "ended_at", time.Now().Format(time.RFC3339)); err != nil {
		return err
	}
	return queueMarkEnded(queuePath(configPath), projName, digestPath)
}

// runFinalize ends the current session and consolidates it inline, so wiki
// pages land in the working tree now — before a PR is created — instead of
// trailing the merge on whatever branch happens to be checked out later. An
// explicit command applies without review regardless of auto_apply (same
// reasoning as memoria commit); --no-apply keeps the proposal for review.
func runFinalize(cwd, configPath string, args []string, out io.Writer) int {
	sid := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sid, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("finalize", flag.ContinueOnError)
	fs.SetOutput(out)
	noApply := fs.Bool("no-apply", false, "generate the proposal but leave it for review")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	p, ok := resolveProject(cfg, configPath, cwd)
	if !ok {
		fmt.Fprintln(out, "error: not inside a tracked project (run memoria bootstrap first)")
		return 1
	}
	if p.Name == globalName {
		cfg = globalCommitCfg(cfg)
	}
	proj := p.Path
	wikiName := p.Wiki
	if wikiName == "" {
		wikiName = "wiki"
	}
	sid, digestPath, err := resolveSession(proj, sid)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if filepath.Base(filepath.Dir(digestPath)) != "pending" {
		fmt.Fprintf(out, "error: session %s is already processed — nothing to finalize\n", sid)
		return 1
	}
	sPath := statusPath(configPath)
	if st, _ := loadStatus(sPath); st[p.Name].State == "running" && pidAlive(st[p.Name].PID) {
		fmt.Fprintf(out, "error: a background job is already running for %s (pid %d) — retry when it finishes\n", p.Name, st[p.Name].PID)
		return 1
	}
	if err := finalizeSession(configPath, p.Name, digestPath); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	fmt.Fprintf(out, "Session %s marked ended.\n", sid)
	cfg.AutoApply = !*noApply // explicit finalize applies; --no-apply reviews
	code := generateProposal(cfg, proj, filepath.Join(proj, wikiName),
		filepath.Join(proj, ".memoria", "proposal.json"), configPath, p.Name, out)
	if code == 0 && !*noApply && !cfg.WikiAutoCommit {
		fmt.Fprintln(out, "Commit the wiki changes to your branch (memoria commit) so they ride in your PR.")
	}
	return code
}
