---
tags: [gotcha, queue, auto-apply, race-condition]
---

A digest can be read by the processor, have new events appended to it by a still-running agent, and then be archived before those new events are processed. The appended events are silently lost.

## The window

```
collectEnded (locked read of digest contents)
  ↓
invokeProcessor (LLM call, 1-10 min)
  ↓
applyProposal (archive & queueRemove)
```

Events appended during the LLM call were never in the prompt, but the digest is archived unread.

## When it happens

Two agents in the same project:

1. Agent A is running in the project
2. Agent B starts in the same project → `queueEndOthers` flags A's session `ended` (per [[gotchas/implicit-session-end]])
3. B ends → `auto_apply` spawns consolidation → batch includes A's live digest
4. Processor runs for 1-10 minutes; A keeps working and appending
5. `applyProposal` archives A's digest and removes its queue entry

A's final work is gone. Not the whole session — `resolveDigestPath` assigns the next session a `-2` incarnation, so new events self-heal going forward. Bounded, but silent data loss.

With `auto_apply: false`, human review of the proposal would catch it. With `auto_apply: true`, the loss is unattended.

## The fix

Digests are append-only, so byte-length is a bulletproof change detector — no clock, no mtime granularity. Commit `ad5d206` adds:

- `proposal.Sizes map[string]int64` (`session_sizes,omitempty`) — digest byte length captured when the prompt was built
- `digestConsumed(path, sizes)` — checks if byte-length matches; missing entry → trusted (pre-upgrade proposals work)
- `applyProposal` skips both the rename and the `queueRemove` when a digest grew; logs it; keeps it queued

Cost: one re-consolidation of that digest next pass. The full content including the appended events is now in the prompt, so it gets a fresh wiki page every pass until it ends — self-correcting, not lossy.

## Related

[[gotchas/implicit-session-end]] (how queueEndOthers works), [[concepts/consolidation-pipeline]] (the architecture), [[gotchas/auto-apply-rewrites-wiki-mid-session]] (another auto-apply surprise).