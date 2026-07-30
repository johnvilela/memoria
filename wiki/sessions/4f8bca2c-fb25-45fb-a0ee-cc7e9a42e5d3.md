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

**Close of incarnation 1**: the final action was a grep for `last-session` / `memoria run` references across README.md, AGENTS.md and `wiki/concepts/recall-and-run.md`; the digest records no doc edits, test run, or commit after it — the session ended at prompt input. Recon's reference list for the flag: help.go:29, the run.go usage string, README lines 45/58/88, AGENTS.md:47, and the flag list in [[concepts/recall-and-run]].

## Incarnation 2: rework finished

The session was reopened the same day as a numbered incarnation (`4f8bca2c...-2.md`, 10:52–13:38, `continues_from` the processed digest — the mechanic described in [[gotchas/implicit-session-end]]). Its digest holds only two near-identical `@subagent-stop` messages reporting completion: `--last-session` removed, interactive picker added showing "New session" plus the last 5 sessions, with warnings on digest-less ones; all tests pass. Both messages name the next step as committing; no commit appears in the digest.

**Committed later the same day**: the rework landed as `0d9d968` — `feat(run): replace --last-session flag with interactive session picker` (10 files, staged set only) via a `/git-commit` session at 18:00 ([[sessions/cf99acf5-2ee6-4118-90b5-1a2964766475]]).