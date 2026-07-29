---
tags: [session, run, cli, tui]
---

# Session: `run` rework — drop `--last-session`, pick from recent sessions

**2026-07-29 10:12–10:46 · project memoria · claude-code**

Request: remove the `--last-session` flag from `memoria run`; when the user doesn't specify a session, show a session selection with the last 5 sessions.

**Recon first** (Explore subagent) mapped the whole run path before editing — the durable internals are folded into [[concepts/recall-and-run]]:

- `cmd/memoria/run.go` (238 lines): the agent binary is the first positional arg; `--new` / `--session <id|name>` / `--last-session` were mutually exclusive stdlib flags. `--last-session` took `entries[len(entries)-1]` from `readSessions` — literally the last line of `.memoria/sessions.md`, no TTY needed. With no flags on a TTY, run offered a two-option yes/no "Continue from where the last session stopped?" prompt, with "No" listed first.
- Recency in `sessions.md` is positional (append-only file, last line = newest); the parsed `date` field is never sorted on.
- The picker to reuse already existed: `selectOption` in `cmd/memoria/tui.go` (Bubble Tea v1.3.10), already used by multi-match `--session` and by `memoria search`; `isTTY()` is a stubable var.
- Shared-helper caution: `mcpDigest` (mcp.go:132) defaults to the newest session via the same `readSessions` / `entries[len-1]` pattern, and `runDigest` uses `findDigest` — those helpers serve both `run` and the MCP side, so touch them carefully.

**Work**: plan file written, then edits to `run.go` (×6), `tui.go` (×1) and `run_test.go` (×3), removing the flag and adding the no-session picker per the request. Tests recon flagged for rework: `TestRunMutuallyExclusive`, the four `TestRunLastSession*`, and `TestRunDefault*`.

**Close**: the final action was a grep for `last-session` / `memoria run` references across README.md, AGENTS.md and `wiki/concepts/recall-and-run.md`; the digest records no doc edits, test run, or commit after it — the session ended at prompt input. Recon's reference list for the flag: help.go:29, the run.go usage string, README lines 45/58/88, AGENTS.md:47, and the flag list in [[concepts/recall-and-run]] (updated in this consolidation).