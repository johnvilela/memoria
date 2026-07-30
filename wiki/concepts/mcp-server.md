---
tags: [mcp, tools, agents, recall]
---

# MCP server and its seven tools

`memoria mcp` is an internal stdio MCP server for agents, registered by `memoria init` (commit 5f54f75): for claude-code in `~/.claude.json`, for codex in `~/.codex/config.toml` (README §Commands). Sessions get seven tools (README §MCP server):

| Tool | What it does |
|------|--------------|
| `memoria_search` | Search the wiki by text or `#tag` (`include_trash` to look inside `trash/`); page content is inlined only when there are ≤3 hits, otherwise paths |
| `memoria_recall` | Read-only record of a past session — git checkpoint, event log, last reported state, wiki summary page when one exists. No LLM call, nothing written (commit `d6edeea`) |
| `memoria_digest` | **Writes/overwrites** `sessions/<sid>.md` by LLM-compiling the session's observation log (background job — call again to poll). Its description routes recall questions to `memoria_recall`/`memoria_search` |
| `memoria_consolidate` | Batch-consolidate ended sessions; when the proposal is ready the agent reviews the page list and calls again with `apply=true` |
| `memoria_lint` | Audit the wiki for contradictions and return the findings |
| `memoria_write_page` | Create or update a wiki page (path validated, tags frontmatter rendered by memoria) |
| `memoria_delete_page` | Move a page to `trash/`, tagged `deleted` and hidden from search |

**Tool descriptions are the routing layer.** The server sets no MCP `instructions` — the per-tool descriptions plus the AGENTS.md recall block are everything an agent sees when choosing a tool. That bit hard in [[sessions/67c500e5-dc86-4a64-8e79-a76444932b79]]: a codex session asked "what did we do?" and, with `memoria_digest` the only recall-shaped tool on offer, LLM-compiled and wrote a wiki page as a side effect of answering. The fix (commit `d6edeea`) was three-fold: the `memoria_recall` tool, a reworded digest description stating that it writes, and a routing sentence in the AGENTS.md block ([[concepts/recall-and-run]]) — recall questions go to `memoria_recall` (read-only); `memoria_digest` is for when the user wants the session saved.

`memoria_recall` is deterministic: it resolves the session via `resolveSession` (the helper extracted in the same change and shared with `memoria_digest` — defaults to the newest session in `.memoria/sessions.md`, validates the sid, requires a digest file) and returns `buildHandoff` output in recall framing ("read-only record … do not re-run anything") instead of the resume framing `memoria run` uses ([[concepts/recall-and-run]]).

The long-running tools (digest, consolidate, lint) never block the agent: the first call starts a detached job, later calls poll its state — the same one-job-per-project tracking `memoria status` shows ([[decisions/0003-never-block-the-agent]]). The consolidate tool preserves the review gate: the agent reviews the proposal before calling again with `apply=true` ([[decisions/0002-llm-never-writes-files]]).

Deleted pages land in `wiki/trash/` instead of vanishing, and stay hidden from `memoria search` unless `--trash` is passed ([[decisions/0001-plain-markdown-no-db]]).