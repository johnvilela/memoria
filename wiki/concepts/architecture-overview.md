---
tags: [architecture, pipeline, config]
---

memoria is "long-term memory and a per-project wiki for code agents, built from their chat sessions" (README intro). It captures knowledge from agent conversations via hooks, digests it into markdown files inside the project, and makes it readable later — by humans or future agent sessions — through the CLI or a built-in MCP server. Inspired by Fabio Akita's ai-memory and Andrej Karpathy's LLM wiki idea (README).

The pipeline, end to end (README §How it works):

1. **Hooks** fire on agent session events and append to a live digest — see [[concepts/session-capture]].
2. Ended digests land in a **pending queue** at `~/.config/memoria/pending.yaml`, grouped by project.
3. `memoria process` **consolidates** the ended sessions into a JSON proposal of wiki pages, which a review step applies — see [[concepts/consolidation-pipeline]].
4. The curated `wiki/` folder is meant to be committed with the project; the raw `.memoria/` digests are gitignored ([[decisions/0001-plain-markdown-no-db]]).
5. Agents read it back through six MCP tools ([[concepts/mcp-server]]) or `memoria search` / `memoria run` ([[concepts/recall-and-run]]).

**Wiki structure and extensibility** (commit 06c9311): The wiki uses a flat top-level folder convention. Five suggested categories — concepts/, decisions/, gotchas/, rules/, sessions/ — are always valid write targets. Users can create additional top-level folders (research/, to-do/, docs/, etc.); once a custom folder exists, the LLM can write pages there. Reserved names: trash/ (used by deletion), _global_ (reserved), and dot-prefixed folders. Invalid page paths are dropped with a warning (drop-and-warn pattern); proposals fail only when zero valid pages remain — no longer all-or-nothing rejection ([[sessions/44c49197-2dea-4615-8ec2-27849c744218]]).

**Implementation**: Go with stdlib CLI dispatch, entrypoint in `cmd/` (README §Development). `scripts/test.sh` runs `go vet + go test -race`; `scripts/build.sh all` cross-compiles to `dist/`.

**Config** lives at `~/.config/memoria/config.yaml` with keys `projects`, `processor`, `notifications`, `auto_apply`, `cron`, `clients` (the agents with capture hooks installed, recorded since commit `7128a3d` — see [[concepts/init-setup-multi-agent]]), and `gemini_api_key` when the gemini processor is chosen; the file is written 0600. The gemini key can also come from the `GEMINI_API_KEY` env var. Session processors: claude-code | codex | ollama | gemini (README §Commands, init). A debug log for background work lives at `~/.config/memoria` (commit de5dfee).