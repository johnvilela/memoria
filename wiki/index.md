---
tags: [index]
---

# memoria

Long-term memory and a per-project wiki for code agents, built from their chat sessions. Start with [[concepts/architecture-overview]] for the pipeline end to end.

## Concepts — how it works

- [[concepts/architecture-overview]] — capture → consolidate → recall pipeline, config, processors
- [[concepts/session-capture]] — hooks, live digests, session titles, implicit session end, incarnations
- [[concepts/consolidation-pipeline]] — `process`, JSON proposal, lint, cron timer
- [[concepts/mcp-server]] — the seven MCP tools agents get; `memoria_recall` is the read-only recall path
- [[concepts/init-setup-multi-agent]] — multi-agent hook/MCP install, the `clients:` registry in config
- [[concepts/recall-and-run]] — bootstrap, AGENTS.md recall block, `run`'s session picker + cross-harness handoff packet, `search`
- [[concepts/handoff-vs-ai-memory]] — research: ai-memory's deterministic workstream handoff vs memoria's — spawned the packet handoff (`ad5d206`)
- [[research/ai-memory-workstream-comparison]] — external (chatgpt) deep-dive on ai-memory v1.19.2 that fed the `run` rework

## Decisions — why it's shaped this way

- [[decisions/0001-plain-markdown-no-db]] — plain `.md` in the repo, versioned with the project
- [[decisions/0002-llm-never-writes-files]] — proposal + review gate
- [[decisions/0003-never-block-the-agent]] — LLM-driven work runs detached
- [[decisions/0004-embedded-prompts-with-file-override]] — prompts ship in the binary (`cmd/memoria/prompts/`), a config file overrides
- [[decisions/0005-auto-apply-is-opt-in]] — autopilot off by default

## Gotchas — what bit us

- [[gotchas/hooks-global-capture-opt-in]] — hooks are global, capture needs `bootstrap`
- [[gotchas/implicit-session-end]] — a new session ends the previous one; reopens spawn incarnations
- [[gotchas/prompt-over-stdin-argv-limit]] — processor prompt goes over stdin (E2BIG)
- [[gotchas/stale-prompt-overrides]] — old materialized prompt files silently pin prompts
- [[gotchas/module-path-mismatch-breaks-go-install]] — fix `go.mod` path before tagging a release
- [[gotchas/auto-apply-rewrites-wiki-mid-session]] — background auto-apply consolidation can modify `wiki/` while a session is running

## Sessions — the episodic log

- [[sessions/2f4ed960-d4d1-4d74-87e8-7b4104179fb4]] — processor E2BIG root cause + `process --inspect`
- [[sessions/85a0d12d-60b4-4442-b107-f40c097122d8]] — MVP loop closed: eight commits from prompt embed to autopilot
- [[sessions/4f8bca2c-fb25-45fb-a0ee-cc7e9a42e5d3]] — `run` rework: session picker replaces `--last-session` (finished in incarnation 2)
- [[sessions/c113e710-6cad-49b4-8ae1-4cb15e1b5b54]] — three empty capture-only sessions right after the run rework
- [[sessions/0075f9e5-98eb-4d2e-bd20-3e8f5c52b069]] — ai-memory research: why their handoff is faster
- [[sessions/cf99acf5-2ee6-4118-90b5-1a2964766475]] — run rework committed: `0d9d968` via /git-commit
- [[sessions/f99b3c0c-68d0-4132-a4b1-cc2538dc1ce7]] — ai-memory research prompt re-asked after /clear; no work captured
- [[sessions/703f30e4-aabc-48d9-bf89-9bbd90d2428a]] — wiki research + session pages committed: `3252ce0`
- [[sessions/e915be64-f9af-4a50-a147-8a4831159754]] — `run` handoff rebuilt: digest pointer → self-contained packet (`ad5d206`)
- [[sessions/80f131bd-209e-4610-8bf3-07dac9154545]] — multi-agent init/setup shipped: `7128a3d`
- [[sessions/019fb0a0-48d0-71f1-954f-7036553b2133]] — handoff packet dogfooded: codex resumes the claude-code session
- [[sessions/6d290df9-cc9e-4703-bcc4-2143eba007aa]] — handoff round-trip: claude-code resumes the codex session; obsidian ignore committed (`c0ee15d`)
- [[sessions/7facd470-c6e9-489b-b490-58832dabc6e2]] — claude session titles captured into digests and the run picker (`c2e7e3a`), plus prompts moved to `cmd/memoria/prompts/` (`c478df6`)
- [[sessions/019fb0c0-23aa-7583-9035-ad2d71dd4ac6]] — wiki committed as `147ed73` over a packet handoff; "what did we do?" answered from the session's own page
- [[sessions/67c500e5-dc86-4a64-8e79-a76444932b79]] — `memoria_recall` shipped (`d6edeea`): read-only "what did we do?" answers, digest rerouted