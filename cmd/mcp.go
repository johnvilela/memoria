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

// mcpInstructions is surfaced to the client at initialize — the why/when
// pitch; per-tool contracts live in the tool descriptions.
const mcpInstructions = `memoria is this project's long-term memory: a curated wiki of decisions, rules, gotchas and concepts distilled from past agent sessions. Its pages are project ground truth — every claim is grounded in what actually happened here, so trust them over guesses about the codebase; if the code contradicts a page, the code has moved on — update the page.

Workflow: call memoria_search before starting non-trivial work (prefix @<project> or @all to reach sibling projects); call memoria_recall to resume or explain earlier sessions; and when you discover something durable mid-session — a decision, a gotcha, a rule — save it immediately with memoria_write_page. Unsaved findings die with the session.

Before creating a PR, flush the session: call memoria_consolidate with end_current=true, apply it, and commit the wiki changes to the feature branch — otherwise the wiki for that work lands only after the chat closes, on whatever branch is checked out then.`

type pageHit struct {
	Project string `json:"project"`
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
}

type mcpSearchOut struct {
	Matches []pageHit `json:"matches"`
}

type mcpRecallOut struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
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
	Path         string   `json:"path" jsonschema:"wiki-relative path ending in .md: index.md, under the suggested concepts/, decisions/, gotchas/, rules/ or sessions/, or under an existing top-level wiki folder; prefer non-sessions folders for durable knowledge (sessions/ decays); trash/, _global/ and dot-folders are reserved"`
	Title        string   `json:"title" jsonschema:"short page title"`
	BodyMarkdown string   `json:"body_markdown" jsonschema:"markdown body without frontmatter; replaces the whole page — include [[wikilinks]] to related pages"`
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

// resolveWorkspace resolves cwd to a tracked project — or the _global
// pseudo-project when global mode is on; every MCP tool call and the CLI
// search start here.
// For global runs the returned cfg carries the pinned commit policy
// (globalCommitCfg), so callers must not re-apply it.
func resolveWorkspace(cwd, configPath string) (cfg config, proj, projName, wikiRoot string, err error) {
	cfg, err = loadConfig(configPath)
	if err != nil {
		return cfg, "", "", "", err
	}
	p, ok := resolveProject(cfg, configPath, cwd)
	if !ok {
		return cfg, "", "", "", fmt.Errorf("not inside a tracked project (run memoria bootstrap first)")
	}
	if p.Name == globalName {
		cfg = globalCommitCfg(cfg)
	}
	wikiName := p.Wiki
	if wikiName == "" {
		wikiName = "wiki"
	}
	return cfg, p.Path, p.Name, filepath.Join(p.Path, wikiName), nil
}

func mcpSearch(cwd, configPath, query string, includeTrash bool) (mcpSearchOut, error) {
	sels, q := splitSelectors(strings.TrimSpace(query))
	if q == "" {
		return mcpSearchOut{}, fmt.Errorf("empty query")
	}
	wss, err := searchWorkspaces(cwd, configPath, sels)
	if err != nil {
		return mcpSearchOut{}, err
	}
	hits := searchHits(wss, q, includeTrash)
	out := mcpSearchOut{Matches: []pageHit{}}
	for _, h := range hits {
		m := pageHit{Project: h.project, Path: h.path}
		if len(hits) <= 3 {
			m.Content = h.content
			// inlined content counts as usage; path-only listings don't
			touchLastUsed(h.wikiRoot, h.path)
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

// resolveSession defaults sid to the newest session, validates it and
// requires its digest file. Returns the sid and the digest path.
func resolveSession(proj, sid string) (string, string, error) {
	if sid == "" {
		// default = most recent session (usually the caller's own)
		entries := readSessions(proj)
		if len(entries) == 0 {
			return "", "", fmt.Errorf("no sessions recorded for this project")
		}
		sid = entries[len(entries)-1].sid
	}
	if sid != filepath.Base(sid) || sid == "." || sid == ".." {
		return "", "", fmt.Errorf("invalid session id %q", sid)
	}
	digest := findDigest(proj, sid)
	if digest == "" {
		return "", "", fmt.Errorf("no digest found for session %s", sid)
	}
	return sid, digest, nil
}

func mcpRecall(cwd, configPath, sessionID string) (mcpRecallOut, error) {
	_, proj, _, wikiRoot, err := resolveWorkspace(cwd, configPath)
	if err != nil {
		return mcpRecallOut{}, err
	}
	sid, digest, err := resolveSession(proj, sessionID)
	if err != nil {
		return mcpRecallOut{}, err
	}
	touchLastUsed(wikiRoot, "sessions/"+sid+".md")
	return mcpRecallOut{SessionID: sid, Content: buildHandoff(proj, wikiRoot, sid, digest, false)}, nil
}

func mcpDigest(cwd, configPath, sessionID string) (mcpJobOut, error) {
	_, proj, projName, wikiRoot, err := resolveWorkspace(cwd, configPath)
	if err != nil {
		return mcpJobOut{}, err
	}
	sid, _, err := resolveSession(proj, sessionID)
	if err != nil {
		return mcpJobOut{}, err
	}
	rel := "sessions/" + sid + ".md"
	ready := func(s procStatus) (mcpJobOut, bool) {
		// prefix, not equality: a repaired run appends "— output repaired"
		if s.State != "done" || !strings.HasPrefix(s.Detail, "session page written: "+rel) {
			return mcpJobOut{}, false
		}
		b, err := os.ReadFile(filepath.Join(wikiRoot, "sessions", sid+".md"))
		if err != nil {
			return mcpJobOut{}, false
		}
		// the agent receives the page — that's usage, not just a rewrite
		touchLastUsed(wikiRoot, rel)
		return mcpJobOut{State: "done", Page: rel, Content: string(b)}, true
	}
	return mcpJob(cwd, configPath, projName, ready, "digest", sid, "--foreground")
}

func mcpConsolidate(cwd, configPath string, apply, endCurrent bool) (mcpJobOut, error) {
	cfg, proj, projName, wikiRoot, err := resolveWorkspace(cwd, configPath)
	if err != nil {
		return mcpJobOut{}, err
	}
	if endCurrent {
		// only a live pending session can be ended; polls after the first call
		// (ended_at already set) and processed digests fall through untouched
		if _, digest, err := resolveSession(proj, ""); err == nil &&
			filepath.Base(filepath.Dir(digest)) == "pending" {
			if front, _ := parseDigest(digest); frontKey(front, "ended_at") == "" {
				if err := finalizeSession(configPath, projName, digest); err != nil {
					return mcpJobOut{}, err
				}
			}
		}
	}
	proposalPath := filepath.Join(proj, ".memoria", "proposal.json")
	if apply {
		pages, err := proposalPages(proposalPath)
		if err != nil {
			return mcpJobOut{}, err
		}
		var buf strings.Builder
		if code := applyProposal(cfg, proj, wikiRoot, proposalPath, queuePath(configPath), projName, &buf); code != 0 {
			return mcpJobOut{}, fmt.Errorf("apply failed: %s", strings.TrimSpace(buf.String()))
		}
		return mcpJobOut{State: "done", Detail: "proposal applied", Pages: pages}, nil
	}
	ready := func(s procStatus) (mcpJobOut, bool) {
		pages, err := proposalPages(proposalPath)
		if err == nil {
			return mcpJobOut{State: "done", Pages: pages,
				Detail: "proposal ready — review the pages, then call again with apply=true"}, true
		}
		// auto_apply runs consume the proposal themselves
		if s.State == "done" && strings.HasPrefix(s.Detail, "applied") {
			return mcpJobOut{State: "done", Detail: s.Detail}, true
		}
		return mcpJobOut{}, false
	}
	// nothing pending and no proposal waiting → spawning would loop forever
	if sessions, _, err := collectEnded(queuePath(configPath), projName); err == nil && len(sessions) == 0 {
		st, _ := loadStatus(statusPath(configPath))
		if r, ok := ready(st[projName]); ok {
			return r, nil
		}
		return mcpJobOut{State: "idle", Detail: "no ended sessions to consolidate"}, nil
	}
	return mcpJob(cwd, configPath, projName, ready, "process", "--foreground")
}

func proposalPages(proposalPath string) ([]string, error) {
	b, err := os.ReadFile(proposalPath)
	if err != nil {
		return nil, fmt.Errorf("no proposal ready — call memoria_consolidate without apply first")
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
	_, proj, projName, _, err := resolveWorkspace(cwd, configPath)
	if err != nil {
		return mcpJobOut{}, err
	}
	lintPath := filepath.Join(proj, ".memoria", "lint.jsonl")
	ready := func(s procStatus) (mcpJobOut, bool) {
		if findings, err := readFindings(lintPath); err == nil {
			return mcpJobOut{State: "done", Findings: findings,
				Detail: "no MCP apply — fix cited pages with memoria_write_page/memoria_delete_page, or the user runs memoria lint --apply / --deny"}, true
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
	_, _, projName, wikiRoot, err := resolveWorkspace(cwd, configPath)
	if err != nil {
		return mcpWriteOut{}, err
	}
	page := wikiPage{Path: in.Path, Title: in.Title, BodyMarkdown: in.BodyMarkdown, Tags: in.Tags}
	if valid, dropped := validatePages([]wikiPage{page}, wikiRoot, projName == globalName); len(valid) == 0 {
		return mcpWriteOut{}, errors.New(dropped[0])
	}
	if err := writeWikiPage(wikiRoot, in.Path, in.Tags, in.BodyMarkdown); err != nil {
		return mcpWriteOut{}, err
	}
	logf("mcp", "wrote %s", filepath.Join(wikiRoot, filepath.FromSlash(in.Path)))
	return mcpWriteOut{Path: in.Path, Written: true}, nil
}

func mcpDeletePage(cwd, configPath, pagePath string) (mcpDeleteOut, error) {
	_, _, projName, wikiRoot, err := resolveWorkspace(cwd, configPath)
	if err != nil {
		return mcpDeleteOut{}, err
	}
	if !validPagePath(pagePath, wikiDirs(wikiRoot), projName == globalName) {
		return mcpDeleteOut{}, fmt.Errorf("page path %q outside the wiki structure", pagePath)
	}
	dst, err := trashPage(wikiRoot, pagePath)
	if err != nil {
		return mcpDeleteOut{}, err
	}
	logf("mcp", "trashed %s → %s", pagePath, dst)
	return mcpDeleteOut{Path: dst, Deleted: true}, nil
}

// trashPage moves wikiRoot/<pagePath> to trash/ with a deleted tag, returning
// the trash-relative destination ("trash/<rel>", -2/-3 suffix on collision).
func trashPage(wikiRoot, pagePath string) (string, error) {
	src := filepath.Join(wikiRoot, filepath.FromSlash(pagePath))
	b, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("page %q not found", pagePath)
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
		return "", err
	}
	if err := os.WriteFile(dst, []byte(addDeletedTag(string(b))), 0o644); err != nil {
		return "", err
	}
	if err := os.Remove(src); err != nil {
		return "", err
	}
	return "trash/" + rel, nil
}

// addDeletedTag marks a trashed page in its frontmatter tags, creating the
// frontmatter when the page has none.
func addDeletedTag(content string) string {
	tags := pageTags(content)
	if !slices.Contains(tags, "deleted") {
		tags = append(tags, "deleted")
	}
	return upsertFrontLine(content, "tags", "tags: ["+strings.Join(tags, ", ")+"]")
}

// runMCP serves the memoria tools over stdio. stdout belongs to the protocol:
// diagnostics go to the file log only.
func runMCP(configPath string, out io.Writer) int {
	srv := mcp.NewServer(&mcp.Implementation{Name: "memoria", Version: version},
		&mcp.ServerOptions{Instructions: mcpInstructions})
	cwd := func() (string, error) { return os.Getwd() }

	type searchIn struct {
		Query        string `json:"query" jsonschema:"text substring or #tag to find wiki pages; lead with @project tokens or @all to search other/all registered projects (e.g. '@api queue' or '@all engine')"`
		IncludeTrash bool   `json:"include_trash,omitempty" jsonschema:"also search deleted pages under trash/"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "memoria_search",
		Description: "Search the project's memory wiki — decisions, rules, gotchas and concepts from past sessions. Call this before starting non-trivial work: pages record gotchas and decisions the code alone won't show, and matches are project ground truth. Query by text substring or #tag; lead with @<project-name> tokens (repeatable) or @all to search other/all registered projects (an unknown name errors listing the known ones); from an unregistered folder the global wiki is searched when global mode is on. Page content is inlined only when there are ≤3 hits — more hits return paths only, so narrow the query and search again (only inlined reads refresh a sessions/ page's lastUsed and keep it from decaying). Pages reference each other with [[wikilinks]] — follow relevant links with further searches. Trashed pages are excluded unless include_trash is set (hits come back keyed trash/<orig-path>)."},
		func(ctx context.Context, req *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, mcpSearchOut, error) {
			d, err := cwd()
			if err != nil {
				return nil, mcpSearchOut{}, err
			}
			res, err := mcpSearch(d, configPath, in.Query, in.IncludeTrash)
			return nil, res, err
		})

	type recallIn struct {
		SessionID string `json:"session_id,omitempty" jsonschema:"session to recall; defaults to the most recent session (usually the caller's own)"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "memoria_recall",
		Description: "Resume context from a past session: returns a self-contained handoff packet — git checkpoint, event history following the continues_from chain, and the session's wiki page when one exists. Call it at session start when continuing earlier work, or to answer \"what did we do in that session?\". Read-only, no LLM call; session_id defaults to the most recent session (usually the caller's own), and recalling refreshes the session page's lastUsed so it doesn't decay. If it errors (no sessions or no digest), fall back to memoria_search."},
		func(ctx context.Context, req *mcp.CallToolRequest, in recallIn) (*mcp.CallToolResult, mcpRecallOut, error) {
			d, err := cwd()
			if err != nil {
				return nil, mcpRecallOut{}, err
			}
			res, err := mcpRecall(d, configPath, in.SessionID)
			return nil, res, err
		})

	type digestIn struct {
		SessionID string `json:"session_id,omitempty" jsonschema:"session to compile; defaults to the most recent session"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "memoria_digest",
		Description: "WRITES the wiki page sessions/<id>.md (overwriting any existing one, preserving lastUsed) by compiling the session's observation log with an LLM. Use it when the user wants the session saved, or to hand work-in-progress to a future session — to merely read past work, use memoria_recall or memoria_search instead. Background job: the first call returns state=started; poll until state=done, which returns the full page content inline. A failed run reports the previous error in detail and auto-retries on the next poll."},
		func(ctx context.Context, req *mcp.CallToolRequest, in digestIn) (*mcp.CallToolResult, mcpJobOut, error) {
			d, err := cwd()
			if err != nil {
				return nil, mcpJobOut{}, err
			}
			res, err := mcpDigest(d, configPath, in.SessionID)
			return nil, res, err
		})

	type consolidateIn struct {
		Apply      bool `json:"apply,omitempty" jsonschema:"write the ready proposal's pages to the wiki"`
		EndCurrent bool `json:"end_current,omitempty" jsonschema:"mark the caller's still-open session ended first, then consolidate — the pre-PR flush that lets wiki pages ride the feature branch; pass it on the first call only, not on polls"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "memoria_consolidate",
		Description: "Distill ended sessions into durable wiki pages (concepts, decisions, gotchas, rules). Background LLM job: the first call starts it; poll until state=done, which lists the proposed page paths (paths only — read the pages to review), then call again with apply=true to write them; apply also archives the consumed sessions and may auto-commit the wiki. state=idle means no ended sessions to consolidate — success, not an error. apply=true without a ready proposal errors; when auto_apply is configured the run applies itself and done reports \"applied …\". Pass end_current=true (first call only) to flush the current session before a PR: it marks the session ended so its wiki pages are written now, on the current branch, instead of after the chat closes."},
		func(ctx context.Context, req *mcp.CallToolRequest, in consolidateIn) (*mcp.CallToolResult, mcpJobOut, error) {
			d, err := cwd()
			if err != nil {
				return nil, mcpJobOut{}, err
			}
			res, err := mcpConsolidate(d, configPath, in.Apply, in.EndCurrent)
			return nil, res, err
		})

	type lintIn struct{}
	mcp.AddTool(srv, &mcp.Tool{Name: "memoria_lint",
		Description: "Audit the wiki for internal consistency: findings have kind contradiction|stale|duplicate and severity warning|info. Background LLM job: the first call starts it, call again to poll. Empty findings on done means the wiki is healthy — a valid, useful result. There is no MCP apply: fix cited pages yourself with memoria_write_page / memoria_delete_page, or the user runs memoria lint --apply / --deny."},
		func(ctx context.Context, req *mcp.CallToolRequest, in lintIn) (*mcp.CallToolResult, mcpJobOut, error) {
			d, err := cwd()
			if err != nil {
				return nil, mcpJobOut{}, err
			}
			res, err := mcpLint(d, configPath)
			return nil, res, err
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "memoria_write_page",
		Description: "Save durable knowledge to the wiki the moment you discover it — a decision made, a gotcha hit, a rule agreed, a concept clarified. Don't wait for session end: pages outside sessions/ never decay, and unsaved findings are lost. Full replace, not a patch: the body you send becomes the whole page, so carry forward still-valid content when updating. Reference related pages inline with [[wikilinks]] (e.g. [[decisions/0001-slug]]) so the page joins the graph instead of becoming an orphan island; invented top-level folders are rejected with the reason. memoria renders the tags (and, for sessions/ pages, lastUsed) frontmatter — send only the markdown body."},
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
		Description: "Move a wiki page to trash/<orig-path> (a -N suffix avoids collisions); it gets a 'deleted' tag and vanishes from search unless include_trash is set. Recoverable: durable pages sit in trash/ indefinitely; only trashed sessions/ pages are purged for good after the decay window. Wikilinks pointing at the page may remain."},
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
