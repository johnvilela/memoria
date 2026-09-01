---
tags: [adr, sessions, lifecycle, pr-workflow]
---

# ADR 0014: `memoria finalize` is the pre-PR flush

**Problem**: wiki writes were gated on session end ([[concepts/session-capture]]), and memoria is branch-blind — pages land on whatever branch is checked out when the background job fires. The common failure: a feature PR merges while the chat is still open, so the session's wiki pages appear only after the agent closes, on main (protected) or mid-way into the next feature, forcing a separate trailing PR ([[gotchas/auto-apply-rewrites-wiki-mid-session]] is the same blindness from the other side).

**Decision**: an explicit finalize verb, mirroring ai-memory's `finalize-session` ([[research/ai-memory-handling-of-memoria-flaws]] §3). Three pieces:

- **`memoria finalize [<sid>] [--no-apply]`** (finalize.go): stamps `ended_at` + marks the queue entry ended (`finalizeSession` — the same state the session-end hook leaves), then runs `generateProposal` inline. An explicit command applies regardless of `auto_apply` (the [[decisions/0010-wiki-auto-commit-is-opt-in]] reasoning); `--no-apply` keeps the review gate.
- **`memoria_consolidate end_current=true`** (MCP): finalizes the caller's still-open pending session first, then the normal job/poll/apply flow. Guarded so polls are harmless: only a pending digest without `ended_at` is finalized.
- **Pre-PR nudge**: a successful `gh pr create` captured from a claude-code session with unflushed observations emits PostToolUse `additionalContext` JSON on stdout telling the agent to flush and commit the wiki to the branch. Additive context only — never a block ([[decisions/0003-never-block-the-agent]]).

**Why no new machinery**: the chat continuing after finalize is the already-existing incarnation mechanism (`<sid>-2.md`, [[gotchas/implicit-session-end]]), and appends racing the processing window are covered by [[decisions/0009-apply-time-race-detection-via-digest-size]]. The trailing incarnation ("created PR, merged") is small and [[concepts/session-decay]] handles it.

**Rejected for now**: branch stamping at capture and `memoria commit --pr` automation for wiki writes that still land late — add only if trailing writes stay painful with finalize available.

New workflow: finish feature → `memoria finalize` (or the MCP flush) → commit `wiki/` to the feature branch → PR carries code and wiki together.
