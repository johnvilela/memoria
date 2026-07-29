---
tags: [architecture, pipeline, config]
---

# Architecture overview

memoria is "long-term memory and a per-project wiki for code agents, built from their chat sessions" (README intro). It captures knowledge from agent conversations via hooks, digests it into markdown files inside the project, and makes it readable later — by humans or future agent sessions — through the CLI or a built-in MCP server. Inspired by Fabio Akita's ai-memory and Andrej Karpathy's LLM wiki idea (README).

The pipeline, end to end (README §How it works):

1. **Hooks** fire on agent session events and append to a live digest — see [[concepts/session-capture]].
2. Ended digests land in a **pending queue** at `~/.config/memoria/pending.yaml`, grouped by project.
3. `memoria process` **consolidates** the ended sessions into a JSON proposal of wiki pages, which a review step applies — see [[concepts/consolidation-pipeline]].
4. The curated `wiki/` folder is meant to be committed with the project; the raw `.memoria/` digests are gitignored ([[decisions/0001-plain-markdown-no-db]]).
5. Agents read it back through six MCP tools ([[concepts/mcp-server]]) or `memoria search` / `memoria run` ([[concepts/recall-and-run]]).

**Implementation**: Go with stdlib CLI dispatch, entrypoint in `cmd/memoria/` (README §Development). `scripts/test.sh` runs `go vet + go test -race`; `scripts/build.sh all` cross-compiles to `dist/`.

**Config** lives at `~/.config/memoria/config.yaml` with keys `projects`, `processor`, `notifications`, `auto_apply`, `cron`, and `gemini_api_key` when the gemini processor is chosen; the file is written 0600. The gemini key can also come from the `GEMINI_API_KEY` env var. Session processors: claude-code | codex | ollama | gemini (README §Commands, init). A debug log for background work lives at `~/.config/memoria` (commit de5dfee).