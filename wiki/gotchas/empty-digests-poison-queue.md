---
tags: [gotcha, queue, sessions, filter]
---

A session digest that contains only bookend events — `@session-start` and `@session-end` with nothing in between — is sent to the LLM for consolidation. The LLM correctly returns `{"pages":[]}`, which the system treats as a hard failure. The queue entry is never removed, and the same digest re-fails on every subsequent `session-end` hook and cron run, poisoning any batch it rides in.

Hit on 2026-08-01: two such digests (`c589857b-...-3.md` at 397 bytes, `55eed586` at 335 bytes) lasted 3 hours in the queue, each blocking batch consolidation until manually retired.

## Why it happens

Sessions that start and exit immediately (e.g., `@session-start source: resume` followed by `@session-end reason: prompt_input_exit` with no captures between) produce valid but valueless digests. The hook capture records the bookends correctly — nothing to blame there. But `collectEnded` passes everything to `generateProposal`, which sends it to the LLM, which sees no content and returns an empty proposal. `applyProposal` then fails at "no pages to write," and the queue is never cleaned.

## The fix

Filter before the LLM: `digestHasContent(body)` checks if a digest has any event lines beyond `@session-start` and `@session-end`. If false, `retireEmptyDigest` archives it to `processed/` and removes the queue entry. All three LLM callers (`detachProcess`, `processAll`, `generateProposal`) route through `collectEnded`, so one guard point stops the problem everywhere. Empty sessions never reach the processor.

## Related

[[decisions/0008-filter-empty-digests-before-processor]] (the design), [[concepts/consolidation-pipeline]] (where it fits).