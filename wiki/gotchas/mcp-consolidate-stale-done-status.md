---
tags: [gotcha, mcp, consolidate, status]
---

# MCP `memoria_consolidate` can return a stale `done` status instead of respawning

Hit for real in [[sessions/b83a5def-41be-43be-82b5-902466d6a5e6]], while flushing a session before opening a PR ([[decisions/0014-finalize-pre-pr-flush]]): calling `memoria_consolidate` with `end_current=true` — meant to finalize the just-ended session and run a fresh consolidation — returned an earlier run's stale `done` status (\"applied 4 pages from 1 sessions\") instead of spawning a new job for the newly-ended session. The workaround at the time was the CLI two-step: `memoria process --foreground` then `--apply`.

## Fix

Shipped on branch `fix/mcp-consolidate-trust`, bundled with [[concepts/mcp-auto-trust]] in the same commit: `fix(mcp): respawn consolidate past stale done status; auto-allow memoria tools on init`. The consolidate path in `cmd/mcp.go` no longer treats a previous run's `done` status as reason to skip spawning a new job when there's new ended-session work pending — it respawns instead. The exact mechanism isn't visible in capture (only the edited file and the regression test name are recorded, not the diff), but the fix is pinned by a new `TestMCPConsolidateStaleDoneRespawns` test, and the full suite was green afterward.

## Related

[[concepts/mcp-server]] (the `memoria_consolidate` tool and its one-job-per-project polling contract), [[decisions/0003-never-block-the-agent]] (why consolidate is a detached, poll-based job in the first place).