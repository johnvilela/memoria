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
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

//go:embed wiki-prompt.md
var defaultWikiPrompt string

// appended by Go, never stored in the editable prompt file, so user edits
// can't break parsing
const jsonContract = `Output ONLY a JSON object, no code fences, no commentary:
{"pages":[{"action":"create"|"update","path":"...","title":"...","content":"full markdown"}]}
"path" is relative to the wiki root: "index.md" or under concepts/, decisions/, gotchas/ or rules/, always ending in .md.
"content" is the complete markdown file body. Propose at least one page.`

type wikiPage struct {
	Action  string `json:"action"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

type proposal struct {
	Project     string     `json:"project"`
	GeneratedAt string     `json:"generated_at"`
	Sessions    []string   `json:"sessions"`
	Pages       []wikiPage `json:"pages"`
}

// runProcess consolidates a project's ended pending sessions into a wiki
// proposal (default) or applies a reviewed proposal (--apply). The LLM never
// writes files: it returns JSON, Go validates and writes.
func runProcess(cwd, configPath string, args []string, out io.Writer) int {
	fs := flag.NewFlagSet("process", flag.ContinueOnError)
	fs.SetOutput(out)
	apply := fs.Bool("apply", false, "write the reviewed proposal to the wiki")
	if err := fs.Parse(args); err != nil {
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
	proposalPath := filepath.Join(proj, ".memoria", "proposal.json")

	if *apply {
		return applyProposal(proj, wikiRoot, proposalPath, queuePath(configPath), p.Name, out)
	}
	return generateProposal(cfg, proj, wikiRoot, proposalPath, configPath, p.Name, out)
}

func generateProposal(cfg config, proj, wikiRoot, proposalPath, configPath, projName string, out io.Writer) int {
	queue, err := loadQueue(queuePath(configPath))
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	var sessions []string
	digests := map[string]string{}
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
	if len(sessions) == 0 {
		fmt.Fprintln(out, "Nothing to process — no ended pending sessions for", projName)
		return 0
	}

	rules, err := loadWikiPrompt(configPath)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	prompt := buildPrompt(rules, readWiki(wikiRoot), digests)
	raw, err := invokeProcessor(cfg, prompt)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		logf("process", "%s: processor: %v", projName, err)
		return 1
	}
	jsonStr, err := extractJSON(raw)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	var pp struct {
		Pages []wikiPage `json:"pages"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &pp); err != nil {
		fmt.Fprintln(out, "error: processor returned invalid JSON:", err)
		return 1
	}
	if err := validatePages(pp.Pages); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}

	prop := proposal{
		Project:     projName,
		GeneratedAt: time.Now().Format(time.RFC3339),
		Sessions:    sessions,
		Pages:       pp.Pages,
	}
	b, err := json.MarshalIndent(prop, "", "  ")
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	if err := os.WriteFile(proposalPath, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	fmt.Fprintf(out, "Proposal from %d session(s):\n", len(sessions))
	for _, pg := range prop.Pages {
		fmt.Fprintf(out, "  %-6s %s — %s\n", pg.Action, pg.Path, pg.Title)
	}
	fmt.Fprintf(out, "Review %s then run: memoria process --apply\n", proposalPath)
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
		if err := os.WriteFile(dst, []byte(pg.Content), 0o644); err != nil {
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
// the four wiki categories (or index.md), no traversal, nothing empty.
func validatePages(pages []wikiPage) error {
	if len(pages) == 0 {
		return fmt.Errorf("proposal has no pages")
	}
	for _, p := range pages {
		if p.Action != "create" && p.Action != "update" {
			return fmt.Errorf("page %q: invalid action %q", p.Path, p.Action)
		}
		if p.Title == "" || p.Content == "" {
			return fmt.Errorf("page %q: empty title or content", p.Path)
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
	for _, c := range []string{"concepts/", "decisions/", "gotchas/", "rules/"} {
		if strings.HasPrefix(p, c) {
			return true
		}
	}
	return false
}

// loadWikiPrompt returns the user-editable prompt, materializing the embedded
// default next to the config on first use.
func loadWikiPrompt(configPath string) (string, error) {
	p := filepath.Join(filepath.Dir(configPath), "wiki-prompt.md")
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(p, []byte(defaultWikiPrompt), 0o644); err != nil {
			return "", err
		}
		return defaultWikiPrompt, nil
	}
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// readWiki returns every .md file under root keyed by wiki-relative path.
func readWiki(root string) map[string]string {
	wiki := map[string]string{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
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
