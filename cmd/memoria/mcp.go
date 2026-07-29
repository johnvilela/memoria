package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type pageHit struct {
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
}

type mcpSearchOut struct {
	Matches []pageHit `json:"matches"`
}

// mcpJobOut is the shared result of the three background LLM tools: digest,
// consolidate and lint all report started/running/done through it.
type mcpJobOut struct {
	State    string        `json:"state"` // started | running | done | idle
	Detail   string        `json:"detail,omitempty"`
	Page     string        `json:"page,omitempty"`
	Content  string        `json:"content,omitempty"`
	Pages    []string      `json:"pages,omitempty"`
	Findings []lintFinding `json:"findings,omitempty"`
}

type mcpWritePageIn struct {
	Path         string   `json:"path" jsonschema:"wiki-relative path: index.md or under concepts/, decisions/, gotchas/, rules/ or sessions/, ending in .md"`
	Title        string   `json:"title" jsonschema:"short page title"`
	BodyMarkdown string   `json:"body_markdown" jsonschema:"markdown body without frontmatter"`
	Tags         []string `json:"tags,omitempty" jsonschema:"0-5 short kebab-case tags"`
}

type mcpWriteOut struct {
	Path    string `json:"path"`
	Written bool   `json:"written"`
}

type mcpDeleteOut struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted"`
}

// mcpProject resolves cwd to a tracked project; every tool call starts here.
func mcpProject(cwd, configPath string) (cfg config, proj, projName, wikiRoot string, err error) {
	cfg, err = loadConfig(configPath)
	if err != nil {
		return cfg, "", "", "", err
	}
	proj = matchProject(cwd, cfg.Projects)
	if proj == "" {
		return cfg, "", "", "", fmt.Errorf("not inside a tracked project (run memoria bootstrap first)")
	}
	p := projectAt(cfg, proj)
	wikiName := p.Wiki
	if wikiName == "" {
		wikiName = "wiki"
	}
	return cfg, proj, p.Name, filepath.Join(proj, wikiName), nil
}

func mcpSearch(cwd, configPath, query string, includeTrash bool) (mcpSearchOut, error) {
	if strings.TrimSpace(query) == "" {
		return mcpSearchOut{}, fmt.Errorf("empty query")
	}
	_, _, _, wikiRoot, err := mcpProject(cwd, configPath)
	if err != nil {
		return mcpSearchOut{}, err
	}
	wiki := readWikiTrash(wikiRoot, includeTrash)
	hits := searchWiki(wiki, query)
	out := mcpSearchOut{Matches: []pageHit{}}
	for _, h := range hits {
		m := pageHit{Path: h}
		if len(hits) <= 3 {
			m.Content = wiki[h]
		}
		out.Matches = append(out.Matches, m)
	}
	return out, nil
}

// mcpJob is the poll dance shared by the background tools: running → wait,
// artifact ready → done, else spawn. One job slot per project, like the CLI.
func mcpJob(cwd, configPath, projName string, ready func(procStatus) (mcpJobOut, bool), spawnArgs ...string) (mcpJobOut, error) {
	sPath := statusPath(configPath)
	st, _ := loadStatus(sPath)
	s := st[projName]
	if s.State == "running" && pidAlive(s.PID) {
		return mcpJobOut{State: "running", Detail: "job still running — call again in a bit"}, nil
	}
	if r, ok := ready(s); ok {
		return r, nil
	}
	detail := "job started — call again in a few minutes"
	if s.State == "error" {
		// ponytail: a failed run auto-retries on the next poll, previous error rides along
		detail = "previous run failed (" + s.Detail + ") — retrying"
	}
	pid, err := spawnDetached(cwd, runLogPath(configPath, projName), spawnArgs...)
	if err != nil {
		return mcpJobOut{}, err
	}
	if err := statusSet(sPath, projName, "running", pid, ""); err != nil {
		logf("mcp", "%s: status: %v", projName, err)
	}
	return mcpJobOut{State: "started", Detail: detail}, nil
}

func mcpDigest(cwd, configPath, sessionID string) (mcpJobOut, error) {
	_, proj, projName, wikiRoot, err := mcpProject(cwd, configPath)
	if err != nil {
		return mcpJobOut{}, err
	}
	sid := sessionID
	if sid == "" {
		// default = most recent session (usually the caller's own)
		entries := readSessions(proj)
		if len(entries) == 0 {
			return mcpJobOut{}, fmt.Errorf("no sessions recorded for this project")
		}
		sid = entries[len(entries)-1].sid
	}
	if sid != filepath.Base(sid) || sid == "." || sid == ".." {
		return mcpJobOut{}, fmt.Errorf("invalid session id %q", sid)
	}
	if findDigest(proj, sid) == "" {
		return mcpJobOut{}, fmt.Errorf("no digest found for session %s", sid)
	}
	rel := "sessions/" + sid + ".md"
	ready := func(s procStatus) (mcpJobOut, bool) {
		if s.State != "done" || s.Detail != "session page written: "+rel {
			return mcpJobOut{}, false
		}
		b, err := os.ReadFile(filepath.Join(wikiRoot, "sessions", sid+".md"))
		if err != nil {
			return mcpJobOut{}, false
		}
		return mcpJobOut{State: "done", Page: rel, Content: string(b)}, true
	}
	return mcpJob(cwd, configPath, projName, ready, "digest", sid, "--foreground")
}

func mcpConsolidate(cwd, configPath string, apply bool) (mcpJobOut, error) {
	_, proj, projName, wikiRoot, err := mcpProject(cwd, configPath)
	if err != nil {
		return mcpJobOut{}, err
	}
	proposalPath := filepath.Join(proj, ".memoria", "proposal.json")
	if apply {
		pages, err := proposalPages(proposalPath)
		if err != nil {
			return mcpJobOut{}, err
		}
		var buf strings.Builder
		if code := applyProposal(proj, wikiRoot, proposalPath, queuePath(configPath), projName, &buf); code != 0 {
			return mcpJobOut{}, fmt.Errorf("apply failed: %s", strings.TrimSpace(buf.String()))
		}
		return mcpJobOut{State: "done", Detail: "proposal applied", Pages: pages}, nil
	}
	ready := func(procStatus) (mcpJobOut, bool) {
		pages, err := proposalPages(proposalPath)
		if err != nil {
			return mcpJobOut{}, false
		}
		return mcpJobOut{State: "done", Pages: pages,
			Detail: "proposal ready — review the pages, then call again with apply=true"}, true
	}
	// nothing pending and no proposal waiting → spawning would loop forever
	if sessions, _, err := collectEnded(queuePath(configPath), projName); err == nil && len(sessions) == 0 {
		if r, ok := ready(procStatus{}); ok {
			return r, nil
		}
		return mcpJobOut{State: "idle", Detail: "no ended sessions to consolidate"}, nil
	}
	return mcpJob(cwd, configPath, projName, ready, "process", "--foreground")
}

func proposalPages(proposalPath string) ([]string, error) {
	b, err := os.ReadFile(proposalPath)
	if err != nil {
		return nil, fmt.Errorf("no proposal ready — run memoria_consolidate first")
	}
	var prop proposal
	if err := json.Unmarshal(b, &prop); err != nil {
		return nil, err
	}
	pages := make([]string, len(prop.Pages))
	for i, p := range prop.Pages {
		pages[i] = p.Path
	}
	return pages, nil
}

func mcpLint(cwd, configPath string) (mcpJobOut, error) {
	_, proj, projName, _, err := mcpProject(cwd, configPath)
	if err != nil {
		return mcpJobOut{}, err
	}
	lintPath := filepath.Join(proj, ".memoria", "lint.jsonl")
	ready := func(s procStatus) (mcpJobOut, bool) {
		if findings, err := readFindings(lintPath); err == nil {
			return mcpJobOut{State: "done", Findings: findings,
				Detail: "review with memoria lint --review, fix with --apply or reject with --deny"}, true
		}
		// no report file: only a finished clean run counts as done
		if s.State == "done" && strings.Contains(s.Detail, "lint") {
			return mcpJobOut{State: "done", Findings: []lintFinding{}, Detail: s.Detail}, true
		}
		return mcpJobOut{}, false
	}
	return mcpJob(cwd, configPath, projName, ready, "lint", "--foreground")
}

func mcpWritePage(cwd, configPath string, in mcpWritePageIn) (mcpWriteOut, error) {
	_, _, _, wikiRoot, err := mcpProject(cwd, configPath)
	if err != nil {
		return mcpWriteOut{}, err
	}
	page := wikiPage{Path: in.Path, Title: in.Title, BodyMarkdown: in.BodyMarkdown, Tags: in.Tags}
	if err := validatePages([]wikiPage{page}); err != nil {
		return mcpWriteOut{}, err
	}
	dst := filepath.Join(wikiRoot, filepath.FromSlash(in.Path))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return mcpWriteOut{}, err
	}
	if err := os.WriteFile(dst, []byte(renderPage(in.Tags, in.BodyMarkdown)), 0o644); err != nil {
		return mcpWriteOut{}, err
	}
	logf("mcp", "wrote %s", dst)
	return mcpWriteOut{Path: in.Path, Written: true}, nil
}

func mcpDeletePage(cwd, configPath, pagePath string) (mcpDeleteOut, error) {
	_, _, _, wikiRoot, err := mcpProject(cwd, configPath)
	if err != nil {
		return mcpDeleteOut{}, err
	}
	if !validPagePath(pagePath) {
		return mcpDeleteOut{}, fmt.Errorf("page path %q outside the wiki structure", pagePath)
	}
	src := filepath.Join(wikiRoot, filepath.FromSlash(pagePath))
	b, err := os.ReadFile(src)
	if err != nil {
		return mcpDeleteOut{}, fmt.Errorf("page %q not found", pagePath)
	}
	rel := pagePath
	dst := filepath.Join(wikiRoot, "trash", filepath.FromSlash(rel))
	for n := 2; ; n++ {
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			break
		}
		rel = strings.TrimSuffix(pagePath, ".md") + fmt.Sprintf("-%d.md", n)
		dst = filepath.Join(wikiRoot, "trash", filepath.FromSlash(rel))
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return mcpDeleteOut{}, err
	}
	if err := os.WriteFile(dst, []byte(addDeletedTag(string(b))), 0o644); err != nil {
		return mcpDeleteOut{}, err
	}
	if err := os.Remove(src); err != nil {
		return mcpDeleteOut{}, err
	}
	logf("mcp", "trashed %s → trash/%s", pagePath, rel)
	return mcpDeleteOut{Path: "trash/" + rel, Deleted: true}, nil
}

// addDeletedTag marks a trashed page in its frontmatter tags, creating the
// frontmatter when the page has none.
func addDeletedTag(content string) string {
	tags := pageTags(content)
	if !slices.Contains(tags, "deleted") {
		tags = append(tags, "deleted")
	}
	line := "tags: [" + strings.Join(tags, ", ") + "]"
	rest, ok := strings.CutPrefix(content, "---\n")
	if !ok {
		return "---\n" + line + "\n---\n\n" + content
	}
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "---\n" + line + "\n---\n\n" + content
	}
	var lines []string
	replaced := false
	for _, l := range strings.Split(rest[:end], "\n") {
		if strings.HasPrefix(l, "tags:") {
			l, replaced = line, true
		}
		lines = append(lines, l)
	}
	if !replaced {
		lines = append(lines, line)
	}
	return "---\n" + strings.Join(lines, "\n") + rest[end:]
}

// runMCP serves the memoria tools over stdio. stdout belongs to the protocol:
// diagnostics go to the file log only.
func runMCP(configPath string, out io.Writer) int {
	srv := mcp.NewServer(&mcp.Implementation{Name: "memoria", Version: "0.1"}, nil)
	cwd := func() (string, error) { return os.Getwd() }

	type searchIn struct {
		Query        string `json:"query" jsonschema:"text substring or #tag to find wiki pages"`
		IncludeTrash bool   `json:"include_trash,omitempty" jsonschema:"also search deleted pages under trash/"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "memoria_search",
		Description: "Search this project's memory wiki by text or #tag. Trashed (deleted) pages are excluded unless include_trash is set."},
		func(ctx context.Context, req *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, mcpSearchOut, error) {
			d, err := cwd()
			if err != nil {
				return nil, mcpSearchOut{}, err
			}
			res, err := mcpSearch(d, configPath, in.Query, in.IncludeTrash)
			return nil, res, err
		})

	type digestIn struct {
		SessionID string `json:"session_id,omitempty" jsonschema:"session to compile; defaults to the most recent session"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "memoria_digest",
		Description: "Compile a session's observation log into its clean wiki page at sessions/<id>.md. Background LLM job: first call starts it, call again to poll until state=done."},
		func(ctx context.Context, req *mcp.CallToolRequest, in digestIn) (*mcp.CallToolResult, mcpJobOut, error) {
			d, err := cwd()
			if err != nil {
				return nil, mcpJobOut{}, err
			}
			res, err := mcpDigest(d, configPath, in.SessionID)
			return nil, res, err
		})

	type consolidateIn struct {
		Apply bool `json:"apply,omitempty" jsonschema:"write the ready proposal's pages to the wiki"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "memoria_consolidate",
		Description: "Consolidate ended sessions into durable wiki pages. Background LLM job: poll until state=done, review the proposed pages, then call again with apply=true to write them."},
		func(ctx context.Context, req *mcp.CallToolRequest, in consolidateIn) (*mcp.CallToolResult, mcpJobOut, error) {
			d, err := cwd()
			if err != nil {
				return nil, mcpJobOut{}, err
			}
			res, err := mcpConsolidate(d, configPath, in.Apply)
			return nil, res, err
		})

	type lintIn struct{}
	mcp.AddTool(srv, &mcp.Tool{Name: "memoria_lint",
		Description: "Audit the wiki for contradictions, stale or duplicate pages. Background LLM job: first call starts it, call again to poll the findings."},
		func(ctx context.Context, req *mcp.CallToolRequest, in lintIn) (*mcp.CallToolResult, mcpJobOut, error) {
			d, err := cwd()
			if err != nil {
				return nil, mcpJobOut{}, err
			}
			res, err := mcpLint(d, configPath)
			return nil, res, err
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "memoria_write_page",
		Description: "Create or update a wiki page. memoria renders the tags frontmatter; send only the markdown body."},
		func(ctx context.Context, req *mcp.CallToolRequest, in mcpWritePageIn) (*mcp.CallToolResult, mcpWriteOut, error) {
			d, err := cwd()
			if err != nil {
				return nil, mcpWriteOut{}, err
			}
			res, err := mcpWritePage(d, configPath, in)
			return nil, res, err
		})

	type deleteIn struct {
		Path string `json:"path" jsonschema:"wiki-relative path of the page to move to trash/"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "memoria_delete_page",
		Description: "Move a wiki page to trash/. The page gets a 'deleted' tag and disappears from search unless include_trash is used; wikilinks to it may remain."},
		func(ctx context.Context, req *mcp.CallToolRequest, in deleteIn) (*mcp.CallToolResult, mcpDeleteOut, error) {
			d, err := cwd()
			if err != nil {
				return nil, mcpDeleteOut{}, err
			}
			res, err := mcpDeletePage(d, configPath, in.Path)
			return nil, res, err
		})

	// stdout carries the protocol — even errors only go to the file log.
	// The client closing stdin is the normal way to stop us; the SDK
	// reports that as "server is closing" without wrapping io.EOF.
	err := srv.Run(context.Background(), &mcp.StdioTransport{})
	if err != nil && !errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "server is closing") {
		logf("mcp", "server: %v", err)
		return 1
	}
	return 0
}
