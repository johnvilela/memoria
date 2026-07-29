---
tags: [mcp, tools, agents]
---

# MCP server and its six tools

`memoria mcp` is an internal stdio MCP server for agents, registered by `memoria init` (commit 5f54f75): for claude-code in `~/.claude.json`, for codex in `~/.codex/config.toml` (README §Commands). Sessions get six tools out of the box (README §MCP server):

| Tool | What it does |
|------|--------------|
| `memoria_search` | Search the wiki by text or `#tag` (`include_trash` to look inside `trash/`) |
| `memoria_digest` | Compile a session's observation log into a clean `sessions/<sid>.md` page (background LLM job — call again to poll) |
| `memoria_consolidate` | Batch-consolidate ended sessions; when the proposal is ready the agent reviews the page list and calls again with `apply=true` |
| `memoria_lint` | Audit the wiki for contradictions and return the findings |
| `memoria_write_page` | Create or update a wiki page (path validated, tags frontmatter rendered by memoria) |
| `memoria_delete_page` | Move a page to `trash/`, tagged `deleted` and hidden from search |

The long-running tools (digest, consolidate, lint) never block the agent: the first call starts a detached job, later calls poll its state — the same one-job-per-project tracking `memoria status` shows ([[decisions/0003-never-block-the-agent]]). The consolidate tool preserves the review gate: the agent reviews the proposal before calling again with `apply=true` ([[decisions/0002-llm-never-writes-files]]).

Deleted pages land in `wiki/trash/` instead of vanishing, and stay hidden from `memoria search` unless `--trash` is passed ([[decisions/0001-plain-markdown-no-db]]).