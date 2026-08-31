---
tags: [hooks, digests, sessions, titles, queue]
---

# Session capture: hooks, live digests, incarnations

`memoria init` wires agent hooks (Claude Code, Codex) that run `memoria hook <event> --client <name>` on each session event (README §How it works). Hooks are installed globally but only projects registered with `memoria bootstrap` are captured — see [[gotchas/hooks-global-capture-opt-in]]. Hooks never block the agent, consistent with [[decisions/0003-never-block-the-agent]].

**Live digests** (commit 8c4237f "write session digest directly as @hook annotated lines"): each event appends an `@hook` annotated line to `<project>/.memoria/sessions/pending/<session_id>.md`. The file is YAML frontmatter plus a chronological stream: full prompts, file writes/edits, Bash commands with errors, assistant stop messages. Captured fields are filtered, and sessions are indexed in `.memoria/sessions.md` by their first user prompt (commit 4419b50).

**Session titles** (commit `c2e7e3a`, [[sessions/7facd470-c6e9-489b-b490-58832dabc6e2]]): on `stop` and `session-end` from `--client claude-code`, `captureTitle` (hook.go) copies the agent's live session title into memoria. `claudeTitle` scans `~/.claude/sessions/*.json` — one file per running Claude Code process, holding `sessionId` and `name` — and matches the sid; the hit is collapsed to 80 chars, written as `title:` frontmatter into the pending digest (via `setFront`, the generalized `setEndedAt`), and swapped into the NAME slot of the session's `.memoria/sessions.md` line (`renameSession`, exact-sid match, safe with " - " inside names). Best-effort throughout: any missing file or parse error is a silent no-op, and an unchanged title skips both rewrites. Codex sessions keep the first-prompt name — codex's `threads.name` in `~/.codex/state_5.sqlite` is empty unless the user manually renamed the thread, so there is nothing better to fetch. The rewritten NAME is what `memoria run`'s picker shows ([[concepts/recall-and-run]]).

**Session end** is either explicit (`session-end` fires) or implicit (a new session starts in the same project), so crashed sessions don't linger in the queue (commit 3275e45) — details in [[gotchas/implicit-session-end]]. New digests are registered in `~/.config/memoria/pending.yaml`, grouped by project; `memoria process` consumes this worklist.

**Incarnations**: reopening an already-processed session starts a numbered incarnation (`<sid>-2.md`) linked to the archived one via `continues_from` frontmatter (commit 3275e45, README §How it works). A plain resume of a still-pending session keeps appending to the same digest file — the [[sessions/7facd470-c6e9-489b-b490-58832dabc6e2]] digest holds an end and a `source: resume` start back-to-back.

The internal command `memoria digest <sid>` compiles one session's observation log into a clean `sessions/<sid>.md` wiki page, driven by the per-session prompt `cmd/prompts/digest-prompt.md` ([[decisions/0004-embedded-prompts-with-file-override]]).
