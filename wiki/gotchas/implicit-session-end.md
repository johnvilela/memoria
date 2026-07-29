---
tags: [gotcha, sessions, lifecycle, queue]
---

# Sessions end implicitly — and reopening spawns incarnations

From commit 3275e45 "feat(hook): queue pending digests, implicit ended, reopen incarnations" and README §How it works.

**Implicit end**: a session counts as ended not only when `session-end` fires, but also **when a new session starts in the same project**. The stated reason: "so crashed sessions don't linger". The surprise is the flip side — starting a fresh session marks the previous one ended, making it eligible for consolidation even though it never fired `session-end`. Don't assume an unconsolidated session is still 'open' just because it never ended cleanly.

**Incarnations**: reopening an *already-processed* session doesn't append to the archived digest — it starts a numbered incarnation (`<sid>-2.md`) linked to the archived one via `continues_from` (README). One agent session ID can therefore map to multiple digest files across the pending/processed lifecycle.

Both behaviors feed the pending queue at `~/.config/memoria/pending.yaml` that `memoria process` consumes — see [[concepts/session-capture]] and [[concepts/consolidation-pipeline]].