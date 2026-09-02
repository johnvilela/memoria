---
tags: [gotcha, mcp, consolidate, status]
---

# MCP `memoria_consolidate` can return a stale `done` status instead of respawning

Hit for real in [[sessions/b83a5def-41be-43be-82b5-902466d6a5e6]], while flushing a session before opening a PR ([[decisions/0014-finalize-pre-pr-flush]]): calling `memoria_consolidate` with `end_current=true` — meant to finalize the just-ended session and run a fresh consolidation — returned an earlier run's stale `done` status (\"applied 4 pages from 1 sessions\") instead of spawning a new job for the newly-ended session. The workaround at the time was the CLI two-step: `memoria process --foreground` then `--apply`.

## Root cause

The ready predicate in `mcpConsolidate` (`cmd/mcp.go`) accepted *any* `done` status whose detail started with \"applied\" as proof there was nothing left to do — including a leftover slot from a previous, unrelated run. An applied run consumes its sessions, so a `done`+\"applied\" status is stale by definition whenever ended sessions are still sitting in the queue.

## Fix

Shipped on branch `fix/mcp-consolidate-trust` → PR #16 (stacked on PR #15), bundled with [[concepts/mcp-auto-trust]] in the same commit: `fix(mcp): respawn consolidate past stale done status; auto-allow memoria tools on init`. Only an unconsumed `proposal.json` now counts as \"ready\" in the spawn path, so the job respawns past the stale `done` slot instead of trusting it; the done+\"applied\" acceptance moved to the nothing-pending branch, where it's still the correct report for a genuine auto-apply outcome. Pinned by a new `TestMCPConsolidateStaleDoneRespawns` test (written and confirmed red first); full suite (414 tests) green under `go vet` + `-race` afterward.

## Related

[[concepts/mcp-server]] (the `memoria_consolidate` tool and its one-job-per-project polling contract), [[decisions/0003-never-block-the-agent]] (why consolidate is a detached, poll-based job in the first place), [[concepts/mcp-auto-trust]] (shipped in the same commit/PR).