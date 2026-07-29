---
tags: [hooks, digests, sessions, queue]
---

# Session capture: hooks, live digests, incarnations

`memoria init` wires agent hooks (Claude Code, Codex) that run `memoria hook <event> --client <name>` on each session event (README §How it works). Hooks are installed globally but only projects registered with `memoria bootstrap` are captured — see [[gotchas/hooks-global-capture-opt-in]]. Hooks never block the agent, consistent with [[decisions/0003-never-block-the-agent]].

**Live digests** (commit 8c4237f "write session digest directly as @hook annotated lines"): each event appends an `@hook` annotated line to `<project>/.memoria/sessions/pending/<session_id>.md`. The file is YAML frontmatter plus a chronological stream: full prompts, file writes/edits, Bash commands with errors, assistant stop messages. Captured fields are filtered, and sessions are indexed in `.memoria/sessions.md` by their first user prompt (commit 4419b50).

**Session end** is either explicit (`session-end` fires) or implicit (a new session starts in the same project), so crashed sessions don't linger in the queue (commit 3275e45) — details in [[gotchas/implicit-session-end]]. New digests are registered in `~/.config/memoria/pending.yaml`, grouped by project; `memoria process` consumes this worklist.

**Incarnations**: reopening an already-processed session starts a numbered incarnation (`<sid>-2.md`) linked to the archived one via `continues_from` frontmatter (commit 3275e45, README §How it works).

The internal command `memoria digest <sid>` compiles one session's observation log into a clean `sessions/<sid>.md` wiki page, driven by the per-session prompt `cmd/memoria/digest-prompt.md` ([[decisions/0004-embedded-prompts-with-file-override]]).