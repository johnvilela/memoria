---
tags: [adr, decay, ai-memory, comparison]
---

# ADR 0013: Reject SQLite/FTS5/link-neighbor from ai-memory; adopt only a deterministic decay stamp

**Decision** (Q&A session preceding [[concepts/session-decay]], [[sessions/acbf5d0d-d82b-4852-98fc-97cfbfb5da35]]): of four ai-memory retrieval/decay mechanisms discussed, three were explicitly rejected for memoria and one was adopted in a deliberately reduced form.

## Rejected: SQLite as memoria's storage/index

ai-memory's SQLite need comes from its daemon architecture: transactional atomic writes for kill-9 safety, dedup via content-hash event IDs, and FTS5 full-text search over a corpus far too large to grep. Memoria's workload doesn't have that shape — corpus is tiny (1-5 pages per consolidation), write-concurrency is already solved with file locking ([[decisions/0007-queue-safety-via-file-locking]]), and search is substring/tag matching over a handful of files. SQLite would cost the human-readable, git-diffable wiki that is the product's differentiator ([[decisions/0001-plain-markdown-no-db]]) for no benefit measurable today. If the wiki ever grows to thousands of pages and search gets slow, the fallback considered (not built) is a *disposable* FTS index built from the markdown — markdown stays the source of truth, the index is a deletable cache.

## Reaffirmed, not changed: capture/handoff are already LLM-decoupled

A direct question — "can we make this more decoupled from LLM processing, like ai-memory does?" — surfaced that memoria already matches the shape ai-memory pioneered: capture (hooks writing digest lines) and cross-harness handoff (the `buildHandoff` packet, commit `ad5d206` — [[concepts/recall-and-run]]) are both already zero-LLM and deterministic. Only wiki consolidation and lint are LLM-driven, and that stage is async, detached, and entirely skippable — configure no cron, never run `process`, and capture/handoff still work. No code changed here; this was a confirmation that the existing architecture ([[concepts/handoff-vs-ai-memory]]) already achieved the separation ai-memory's design demonstrates.

## Rejected: FTS5 over the current linear scan

`searchWiki`'s current approach (read every page, lowercase, `strings.Contains`) stays. FTS5 buys query language (multi-term AND/OR/phrase/prefix), BM25 relevance ranking, word-aware stemmed matching, and snippets — but costs index-sync machinery and the SQLite dependency already rejected above. At the wiki's current size (dozens of pages) the scan runs in single-digit milliseconds, so the tradeoff isn't worth it. The cheaper next rung, if ever needed, is multi-word AND matching in `searchWiki` (~5 lines, no index) — discussed but **not built** this session.

## Rejected: link-neighbor RRF ranking

ai-memory expands search hits with pages linked *from* the matches (using its `links` table) and merges multiple retrieval streams via Reciprocal Rank Fusion. Memoria already has the graph — every consolidated page is required to carry `[[wikilinks]]` — but exploits it at *read time* (a human or agent follows a link after reading the page) rather than at *rank time*. RRF earns its keep merging thousands of candidates across multiple retrieval streams; memoria has one stream and a screenful of results with no ranking problem for it to solve.

## Adopted, reduced: decay as a deterministic date stamp

ai-memory's decay is a salience score — `salience · exp(−λΔt) + σ · log(1+access_count) · exp(−μ · days_since_access)` — with per-access reinforcement bookkeeping that needs the daemon's state. Memoria adopted only the underlying insight (episodic pages fade when unused, semantic/procedural pages don't) and implemented it as a plain `lastUsed` date per `sessions/` page: no counters, no exponential math, no access-count tracking — "ai-memory's tier rule with zero counters," made safer by routing every deletion through `trash/` (recoverable via git history, same as every other memoria deletion). Full mechanism: [[concepts/session-decay]].

Related: [[research/ai-memory-handling-of-memoria-flaws]] and [[concepts/handoff-vs-ai-memory]] (the earlier research this Q&A built on), [[decisions/0003-never-block-the-agent]] (why the sweep lives only in `processAll`).