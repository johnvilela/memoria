---
tags: [adr, queue, concurrency]
---

**Decision** (commit pending, [[sessions/1ac019b3-5e9e-468e-a503-db42b7fa7ffd]]): when a digest grows between when the processor reads it and when `applyProposal` archives it, defer archiving and re-queue for the next processing pass.

**Rationale**: Digests are append-only. A digest read for the LLM prompt can have new events appended by the still-running agent while the processor runs (1-10 minutes). Archiving it before those appended events are processed means they are silently lost [[gotchas/digest-grows-during-processing]].

Byte-length is a bulletproof change detector — no clock granularity, no mtime races — because the write is atomic via `flock` ([[decisions/0007-queue-safety-via-file-locking]]).

**Implementation**: `proposal` gains `Sizes map[string]int64` (`session_sizes,omitempty`) stamped from the digests map at prompt-build time. `applyProposal` calls `digestConsumed(path, sizes)` before archiving; if the check fails (digest grew or entry missing), skip both the rename and `queueRemove`, log it, and continue.

Missing entry (pre-upgrade proposals) → trusted, so existing `.memoria/proposal.json` files don't stall forever.

**Cost**: a digest that grows re-consolidates on the next pass, producing a fresh wiki page with all the content including the appended events. It defers archive until the session ends or stops emitting. Self-correcting, never lossy.

**When to revisit**: if digests routinely grow faster than the consolidation interval (cron runs hourly by default), consider skipping the batch until it's quiet, or widening the interval.

Related: [[decisions/0007-queue-safety-via-file-locking]] (the flock infrastructure this depends on), [[gotchas/digest-grows-during-processing]] (the failure mode).