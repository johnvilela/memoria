---
tags: [consolidation, lint, cron, background-jobs]
---

# Consolidation pipeline: process, proposal, lint, cron

`memoria process` consolidates ended sessions into the project wiki (commit 1fca070). It detaches by default and returns immediately, because "the processor call can take minutes and must never block an active agent session" (README §How it works) — see [[decisions/0003-never-block-the-agent]]. `memoria status` tracks the run (running / done / error per project), and with `notifications: true` a finished run pings the desktop via notify-send (commit 899cef2).

The configured processor receives the ended sessions plus the current wiki and returns a **JSON proposal**: 1-5 pages under `concepts/`, `decisions/`, `gotchas/`, `rules/` and `sessions/` (plus `index.md`), connected by `[[wikilinks]]`, tags rendered as YAML frontmatter. The LLM never writes files — the proposal lands in `.memoria/proposal.json`; `process --apply` writes `wiki/` and archives the sessions to `.memoria/sessions/processed/` ([[decisions/0002-llm-never-writes-files]]).

Flags (commit 5c50d83): `--inspect` follows a running job, `--all` sweeps every project (the timer's entrypoint), `--foreground` opts out of detaching.

**Lint** (commit c5fb59b): `memoria lint` audits the wiki for contradictions / stale / duplicate pages in the background; `--review` prints the findings, `--apply` fixes them via a second LLM pass, `--deny "why"` rejects them with a reason future runs remember. Prompt: `cmd/memoria/lint-prompt.md`.

**Cron** (commit 5c50d83, README §Cron): `--cron` installs a systemd user timer running `process --all` on a schedule (`hourly`, `every 3 hours`, `8 times a day`, or 5-field cron); `--cron-apply` makes the timer apply proposals without review. `memoria setup` reconfigures processor / notifications / auto-apply / schedule without touching hooks.

**Autopilot**: `--auto-apply` removes every manual step — see [[decisions/0005-auto-apply-is-opt-in]].