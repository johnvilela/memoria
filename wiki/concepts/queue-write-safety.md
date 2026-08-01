---
tags: [queue, concurrency, locking, design]
---

The pending work queue (`pending.yaml`) and project status (`status.yaml`) are written by concurrent processes: hooks fire in parallel when multiple agent sessions run, and `memoria process --all` (cron timer) reads and modifies them independently. Without synchronization, concurrent load→modify→save operations on the same file lose updates.

## The race condition

Two writers overlap in the unprotected window:

```
Writer A: load pending.yaml → memory
Writer B: load pending.yaml → memory  (reads stale data)
Writer A: add item, save pending.yaml
Writer B: add item, save pending.yaml  (overwrites A's add)
```

Result: one add is lost. The digest for that session never gets queued for processing, and the session is silently lost forever.

## The solution: exclusive file locking with atomic writes

Commit `0f961a7` ([[sessions/16f159dc-d22a-413f-a3e4-c02ceb22b9cc]]) added file-locking primitives in `flock.go`:

- **`withFlock(path, fn)`**: takes an exclusive `syscall.Flock` on a sidecar `.lock` file before calling the function. Only one writer at a time can hold the lock.
- **Atomic writes**: `writeFileAtomic` writes to a temp file first, then atomically renames it into place. A process crash mid-write leaves only the temp file behind; the original is never partially written.

All queue and status writers are protected:

- `queueAdd` — adds a session to pending
- `queueRemove` — removes a session when processed
- `queueMarkEnded` — marks a session ended
- `queueEndOthers` — ends all other sessions when a new one starts
- `statusSet` — updates a project's processing status

## Why readers don't need locks

Readers (`loadQueue`, `runStatus`) stay lock-free. Atomic file rename has a key property: a reader always sees either the old complete file or the new complete file, never a partial write. The only downside is that a digest added mid-run sits in the queue until the next cron cycle — acceptable, since cron runs frequently ([[concepts/consolidation-pipeline]]).

## Concurrency test

`TestQueueConcurrentAdds` spins up 20 goroutines that add to the queue simultaneously. Without the lock, updates are lost. With it, the test passes reliably across multiple runs.

Related: [[concepts/consolidation-pipeline]] (how cron reads the queue), [[decisions/0007-queue-safety-via-file-locking]] (why file locking over append-only redesign).