---
tags: [bootstrap, recall, cli, agents-md]
---

# Recall: bootstrap, AGENTS.md block, run, search

Capture is opt-in per project: `memoria bootstrap`, run inside the project, registers the folder as tracked (commit f5d66ee), gitignores `.memoria/`, creates the wiki folder (`--wiki <name>` to rename), and offers to seed the wiki from git history (`--background` detaches; seed prompt at `cmd/memoria/seed-prompt.md`). Wiki seeding originally lived in `init` and was moved to `bootstrap` (commit 37a7a77).

**Recall block**: bootstrap writes a marker block into the project's `AGENTS.md` telling agents where the memory lives and to prefer the MCP tools, plus a `CLAUDE.md` shim; the block is repaired on re-run (commit 0eca8b2, README §Commands). This is how a fresh agent session learns the wiki exists — see [[concepts/mcp-server]].

**`memoria run <agent-binary>`** (commit 0643004) launches any agent on PATH inside the project, continuing a previous session: same harness → native resume (`claude --resume`, `codex resume`); different harness → a handoff prompt pointing at the session digest. Flags: `--new`, `--session <id|name>`, `--last-session`. This is what carries a finished session into the next one across harnesses.

**`memoria search [--trash] <text | #tag>`** (commit fca4954) finds wiki pages by content substring or frontmatter tag and prints the chosen one; trashed pages stay hidden unless `--trash`.