---
tags: [index]
---

# memoria

Long-term memory and a per-project wiki for code agents, built from their chat sessions. Start with [[concepts/architecture-overview]] for the pipeline end to end.

## Concepts — how it works

- [[concepts/architecture-overview]] — capture → consolidate → recall pipeline, config, processors
- [[concepts/session-capture]] — hooks, live digests, implicit session end, incarnations
- [[concepts/consolidation-pipeline]] — `process`, JSON proposal, lint, cron timer
- [[concepts/mcp-server]] — the six MCP tools agents get
- [[concepts/recall-and-run]] — bootstrap, AGENTS.md recall block, `run`'s session picker, `search`

## Decisions — why it's shaped this way

- [[decisions/0001-plain-markdown-no-db]] — plain `.md` in the repo, versioned with the project
- [[decisions/0002-llm-never-writes-files]] — proposal + review gate
- [[decisions/0003-never-block-the-agent]] — LLM-driven work runs detached
- [[decisions/0004-embedded-prompts-with-file-override]] — prompts ship in the binary, a config file overrides
- [[decisions/0005-auto-apply-is-opt-in]] — autopilot off by default

## Gotchas — what bit us

- [[gotchas/hooks-global-capture-opt-in]] — hooks are global, capture needs `bootstrap`
- [[gotchas/implicit-session-end]] — a new session ends the previous one; reopens spawn incarnations
- [[gotchas/prompt-over-stdin-argv-limit]] — processor prompt goes over stdin (E2BIG)
- [[gotchas/stale-prompt-overrides]] — old materialized prompt files silently pin prompts
- [[gotchas/module-path-mismatch-breaks-go-install]] — fix `go.mod` path before tagging a release

## Sessions — the episodic log

- [[sessions/2f4ed960-d4d1-4d74-87e8-7b4104179fb4]] — processor E2BIG root cause + `process --inspect`
- [[sessions/85a0d12d-60b4-4442-b107-f40c097122d8]] — MVP loop closed: eight commits from prompt embed to autopilot
- [[sessions/4f8bca2c-fb25-45fb-a0ee-cc7e9a42e5d3]] — `run` rework: session picker replaces `--last-session`
- [[sessions/c113e710-6cad-49b4-8ae1-4cb15e1b5b54]] — three empty capture-only sessions right after the run rework