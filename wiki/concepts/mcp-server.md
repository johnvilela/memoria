---
tags: [mcp, tools, agents, recall]
---

# MCP server and its seven tools

`memoria mcp` is an internal stdio MCP server for agents, registered by `memoria init` (commit 5f54f75): for claude-code in `~/.claude.json`, for codex in `~/.codex/config.toml` (README §Commands). Sessions get seven tools (README §MCP server):

| Tool | What it does |
|------|--------------|
| `memoria_search` | Search the wiki by text or `#tag` (`include_trash` to look inside `trash/`); lead the query with `@project`/`@all` to reach sibling projects ([[concepts/cross-project-search]]); page content is inlined only when there are ≤3 hits, otherwise paths; matches always carry a `project` field |
| `memoria_recall` | Read-only record of a past session — git checkpoint, event log, last reported state, wiki summary page when one exists. No LLM call, nothing written (commit `d6edeea`) |
| `memoria_digest` | **Writes/overwrites** `sessions/<sid>.md` by LLM-compiling the session's observation log (background job — call again to poll). Its description routes recall questions to `memoria_recall`/`memoria_search` |
| `memoria_consolidate` | Batch-consolidate ended sessions; when the proposal is ready the agent reviews the page list and calls again with `apply=true`; `idle` (no ended sessions) is a success state, not an error |
| `memoria_lint` | Audit the wiki for contradictions and return the findings; no MCP apply path — fix cited pages with `memoria_write_page`/`memoria_delete_page`, or the user runs the CLI's `--apply`/`--deny` |
| `memoria_write_page` | Create or update a wiki page (path validated, tags frontmatter rendered by memoria); full replace, not a patch — include `[[wikilinks]]` so the page joins the graph |
| `memoria_delete_page` | Move a page to `trash/`, tagged `deleted` and hidden from search; recoverable — only trashed `sessions/` pages are ever purged |

**Tool descriptions (plus, since the trust rewrite below, a server-level `Instructions` string) are the routing layer.** Until PR #12's trust rewrite, the server set no MCP `instructions` at all — per-tool descriptions plus the AGENTS.md recall block were everything an agent saw when choosing a tool. That bit hard in [[sessions/67c500e5-dc86-4a64-8e79-a76444932b79]]: a codex session asked "what did we do?" and, with `memoria_digest` the only recall-shaped tool on offer, LLM-compiled and wrote a wiki page as a side effect of answering. The fix (commit `d6edeea`) was three-fold: the `memoria_recall` tool, a reworded digest description stating that it writes, and a routing sentence in the AGENTS.md block ([[concepts/recall-and-run]]) — recall questions go to `memoria_recall` (read-only); `memoria_digest` is for when the user wants the session saved.

## The MCP trust rewrite (PR #12's second commit, [[sessions/075393cf-e94c-4b79-a4c5-4feec580aa66]])

Goal, per the user: get the agent to search memory before working, write durable pages as it goes, treat results as project ground truth, and use recall/digest for continuity — sell the tools' use, not just document them. Every text surface an agent reads was rewritten:

- **New server-level `Instructions`**: `mcp.NewServer` now passes `&mcp.ServerOptions{Instructions: mcpInstructions}` instead of `nil`. The pinned SDK (`github.com/modelcontextprotocol/go-sdk` v1.6.1) already supported `ServerOptions.Instructions`; memoria had never wired it up. It's surfaced to every client once, at MCP `initialize`, and pitches: pages are project ground truth (grounded in what actually happened — trust them over guesses about the code); search before non-trivial work; save durable findings immediately with `memoria_write_page` (unsaved findings die with the session); use `memoria_recall` to resume or explain earlier sessions.
- **All seven tool descriptions rewritten** to lead with *when to use it*, then the contract that was previously undocumented — notably: `memoria_search` now tells the agent to narrow and re-query on a >3-hit paths-only result, since only an inlined read refreshes a `sessions/` page's `lastUsed` ([[concepts/session-decay]]); `memoria_recall` tells the agent to fall back to `memoria_search` on error; `memoria_consolidate` documents the `idle` success state and that `apply=true` without a ready proposal errors; `memoria_write_page`'s rewrite is the centerpiece — "save durable knowledge the moment you discover it... don't wait for session end: pages outside sessions/ never decay... full replace, not a patch... reference related pages inline with `[[wikilinks]]` ... so the page joins the graph instead of becoming an orphan island"; `memoria_delete_page` now states deletion is recoverable.
- **A stale runtime detail string fixed**: `memoria_lint`'s findings detail used to tell the agent to run CLI-only flags (`memoria lint --review/--apply/--deny`) even though MCP has no lint-apply path; it now points at `memoria_write_page`/`memoria_delete_page` for the fix, or names the CLI flags as the user's path.
- **Bootstrap's AGENTS.md block** ([[concepts/recall-and-run]]) rewritten around the same goals, plus one new safety line: "If the code contradicts a page, the code won — update the page" — trust without blindness, feeding back into the write-as-you-go loop.
- **The four processor prompts** (`wiki-prompt.md`, `seed-prompt.md`, `digest-prompt.md`, `lint-prompt.md` — [[decisions/0004-embedded-prompts-with-file-override]]) each gained one line: write for an agent audience, imperative and actionable, with confidence matched to what the observations actually support — since the wiki itself is a trust surface future agents read as ground truth.

Verified with a scratch-dir `memoria bootstrap` run (regenerated this repo's own AGENTS.md block in place) and a raw stdio JSON-RPC `initialize` handshake confirming the new `instructions` field is actually sent. No separate version bump — shipped under the same 0.13.0 as [[concepts/cross-project-search]].

`memoria_recall` is deterministic: it resolves the session via `resolveSession` (the helper extracted in the same change and shared with `memoria_digest` — defaults to the newest session in `.memoria/sessions.md`, validates the sid, requires a digest file) and returns `buildHandoff` output in recall framing ("read-only record … do not re-run anything") instead of the resume framing `memoria run` uses ([[concepts/recall-and-run]]).

The long-running tools (digest, consolidate, lint) never block the agent: the first call starts a detached job, later calls poll its state — the same one-job-per-project tracking `memoria status` shows ([[decisions/0003-never-block-the-agent]]). The consolidate tool preserves the review gate: the agent reviews the proposal before calling again with `apply=true` ([[decisions/0002-llm-never-writes-files]]).

Deleted pages land in `wiki/trash/` instead of vanishing, and stay hidden from `memoria search` unless `--trash` is passed ([[decisions/0001-plain-markdown-no-db]]).