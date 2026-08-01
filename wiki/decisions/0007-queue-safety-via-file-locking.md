---
tags: [adr, queue, concurrency, simplicity]
---

**Decision** (commit `0f961a7`, [[sessions/16f159dc-d22a-413f-a3e4-c02ceb22b9cc]]): queue and status writes are serialized via exclusive file locks and atomic file operations, not via a complete redesign to append-only or transactional storage.

## Trade-off evaluated

- **File locking + atomic writes** (~30 lines): `flock.go` with `syscall.Flock` and temp-file-then-rename. Readers stay lock-free and fast. Portable across POSIX systems.
- **Append-only per-project queue**: would eliminate the race entirely but requires ledger entries, cursors, deduplication logic, and careful restart semantics — orders of magnitude larger, adds SQLite or similar for durable cursors.

**Rationale**: lock contention is unmeasurable at memoria's write rate (hooks fire on every prompt, cron runs hourly or less frequently). The flock solution kills the race for minimal code and stays true to [[decisions/0001-plain-markdown-no-db]] (no database, plain files).

**When to revisit**: if measuring the queue reveals lock contention causing palpable delays, or if write rate scales beyond the current ~1 per second per project, then the append-only design becomes worth the complexity.