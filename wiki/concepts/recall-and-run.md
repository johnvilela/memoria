---
tags: [bootstrap, recall, run, cli, agents-md]
---

# Recall: bootstrap, AGENTS.md block, run, search

Capture is opt-in per project: `memoria bootstrap`, run inside the project, registers the folder as tracked (commit f5d66ee), gitignores `.memoria/`, creates the wiki folder (`--wiki <name>` to rename), and offers to seed the wiki from git history (`--background` detaches; seed prompt at `cmd/memoria/seed-prompt.md`). Wiki seeding originally lived in `init` and was moved to `bootstrap` (commit 37a7a77).

**Recall block**: bootstrap writes a marker block into the project's `AGENTS.md` telling agents where the memory lives and to prefer the MCP tools, plus a `CLAUDE.md` shim; the block is repaired on re-run (commit 0eca8b2, README §Commands). This is how a fresh agent session learns the wiki exists — see [[concepts/mcp-server]].

**`memoria run <agent-binary>`** (commit 0643004) launches any agent on PATH inside the project, continuing a previous session: same harness → native resume (`claude --resume`, `codex resume`); different harness → a handoff prompt pointing at the session digest. Session selection, reworked in [[sessions/4f8bca2c-fb25-45fb-a0ee-cc7e9a42e5d3]]: `--new` forces a fresh session; `--session <id|name>` matches by sid prefix or name substring, case-insensitive (multiple hits open a picker on a TTY); with no session given, run shows a selection over the most recent sessions (the rework's spec: last 5). That rework retired the `--last-session` flag and the earlier yes/no "continue last session?" default prompt — the picker covers both.

**How run resolves sessions**: `.memoria/sessions.md` is an append-only index — one `RFC3339 - SID - NAME` line per session (NAME = the first user prompt collapsed to 80 chars), written by `indexSession` in `cmd/memoria/hook.go` with per-sid dedup. Recency is positional: last line = newest; the `date` field is parsed but never sorted on. `findDigest` maps a sid to its digest file, preferring `pending/` over `processed/` and taking the highest incarnation ([[gotchas/implicit-session-end]]); `digestClient` reads the digest's `client:` frontmatter to choose native resume vs handoff. The MCP `memoria_digest` default ("newest session", mcp.go) rides the same `readSessions` helper — changes there touch both the CLI and MCP paths.

**Interactive selection** lives in `cmd/memoria/tui.go` on Bubble Tea (`charmbracelet/bubbletea` v1.3.10 with bubbles + lipgloss): `selectOption(title, opts)` renders an arrow/j-k list (enter selects, esc aborts); `isTTY()` is a stubable var so tests can force either path. The CLI itself is framework-free: manual switch dispatch in `main.go` plus one stdlib FlagSet per subcommand.

**`memoria search [--trash] <text | #tag>`** (commit fca4954) finds wiki pages by content substring or frontmatter tag and prints the chosen one, using the same `selectOption` picker when several match; trashed pages stay hidden unless `--trash`.