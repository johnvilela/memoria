package main

import (
	"flag"
	"fmt"
	"io"
	"sort"
)

// runCommit commits the current project's wiki folder — modified and new pages
// — with the same message pattern as the auto-commit. Explicit, so it ignores
// wiki_auto_commit; staging is pathspec-scoped, so the user's other staged
// files stay staged.
func runCommit(cwd, configPath string, args []string, out io.Writer) int {
	usage := func() { fmt.Fprintln(out, `usage: memoria commit [-m "subject"]`) }
	fs := flag.NewFlagSet("commit", flag.ContinueOnError)
	fs.SetOutput(out)
	msg := fs.String("m", "", "commit subject (default: the wiki_commit_message pattern)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() > 0 {
		usage()
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
	wikiRoot := wikiRootFor(cfg, proj)
	paths, err := wikiChanges(wikiRoot)
	if err != nil {
		fmt.Fprintf(out, "error: %s: %v\n", wikiRoot, err)
		return 1
	}
	if len(paths) == 0 {
		fmt.Fprintln(out, "No wiki changes to commit.")
		return 0
	}
	sort.Strings(paths)
	subject := *msg
	if subject == "" {
		subject = wikiCommitMessage(cfg, wikiRoot, "update", pageSummary(paths), len(paths))
	}
	if err := commitWikiGit(wikiRoot, subject); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	fmt.Fprintln(out, subject)
	return 0
}
