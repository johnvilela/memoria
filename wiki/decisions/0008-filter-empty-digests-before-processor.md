---
tags: [adr, queue, filter, safety]
---

**Decision** (commit pending, [[sessions/1ac019b3-5e9e-468e-a503-db42b7fa7ffd]]): empty session digests (containing only `@session-start` and `@session-end` with no events) are filtered out and retired to `processed/` before being sent to the processor. This prevents the LLM from being called on valueless input and prevents poisoned queue entries from re-failing forever.

## Rationale

A session can end immediately after starting — e.g., the user resumes a session and quits before any work, producing `@session-start source: resume` + `@session-end reason: prompt_input_exit` (zero events). Such digests are valid data but convey nothing worth documenting. Sending them to the LLM is wasteful and, worse, creates a failure case:

- Processor returns `{"pages":[]}`
- System treats zero pages as hard failure (no valid output)
- Queue entry is never removed
- On every subsequent hook fire or cron run, the same digest re-fails
- Poisoned batch: one empty digest blocks all other sessions in its cron cycle

## Implementation

`digestHasContent(body string) bool` checks whether a digest has any event line beyond the bookend markers. If false, `retireEmptyDigest` archives the digest to `.memoria/sessions/processed/` and removes its queue entry via `queueRemove`.

The guard is placed in `collectEnded` — the sole bottleneck that all three LLM callers (`detachProcess`, `processAll`, `generateProposal`) route through. One guard point, three callers, no duplication.

## When to revisit

If digests with no events become valuable (e.g., for metadata about sessions that crashed at startup), the guard can be made configurable. Until then, empty digests are noise.

Related: [[gotchas/empty-digests-poison-queue]] (the failure mode), [[concepts/consolidation-pipeline]] (the architecture).