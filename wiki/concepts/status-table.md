---
tags: [status, cli, tui, lipgloss]
---

# `memoria status`: styled lipgloss table

Shipped on branch `feat/status-table` as **PR #5** (open, not approved at session close), [[sessions/de6b74fc-4caf-4133-ab00-ee307afe6d78]]. Replaces the four raw `fmt.Fprintf` prose lines `runStatus` used to print with a borderless lipgloss table, after a screenshot showed the prose output as "ugly and hard to read."

## No new dependency

`bubbles`, `bubbletea`, `lipgloss`, and `golang.org/x/term` were already direct `go.mod` dependencies before this change. The table uses `lipgloss/table` rather than bubbles' interactive table component — bubbles was deliberately skipped: a static render only needs `lipgloss/table`, and bubbles would only earn its keep for a future interactive live-refresh view of status.

## Data model is unchanged

`procStatus` still holds only `State`, `PID`, `StartedAt`, `FinishedAt`, and a free-form `Detail` string. "Pages applied" / "sessions" counts are still baked into `Detail` by the producers (process.go, lint.go, seed.go, digest.go, mcp.go) rather than being structured fields — this feature only changed how the existing data renders, not what data status tracks.

## Rendering

Each row shows PROJECT / STATUS / DETAIL / FINISHED. State is a colored glyph using base ANSI colors so it follows the terminal's own theme: green `●` done, red `✗` error, yellow `◌` running. `Detail` is capped at 80 chars via the existing `collapse` helper. Timestamps render as absolute plus relative age, e.g. `Aug 21 12:02 (5d ago)`.

Example output:

```
PROJECT STATUS DETAIL FINISHED
bsj ● done applied 4 pages from 1 sessions Aug 21 12:02 (5d ago)
eparts ● done applied 5 pages from 1 sessions Aug 26 20:31 (2h ago)
```

## TTY handling

Piped/non-TTY output automatically degrades to plain text — no new `NO_COLOR` handling was added; this relies on lipgloss/termenv's own detection, the same behavior already present elsewhere in the codebase.

## Tests

The pre-existing `TestRunStatusOutput` asserts via `strings.Contains` on substrings (`"alive"`, `"running"`, `"process died"`, etc.) — these survived the restyle because lipgloss wraps whole rendered strings rather than splitting mid-substring. A new header-row assertion was added alongside them.

## Version

`const version` was bumped to **0.10.0** in the same PR, satisfying CI's `version-check` gate ([[decisions/0012-ci-cd-release-pipeline]]). As of session close the PR was pushed but not merged, so no tag/release has fired yet — that happens automatically only on merge to `main`.

Related: [[concepts/ci-release-pipeline]] (the CI gate this PR must pass), [[rules/no-ai-attribution]] (a correction made to this same PR's body).