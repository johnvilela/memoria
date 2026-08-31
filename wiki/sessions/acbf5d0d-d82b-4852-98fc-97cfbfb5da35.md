---
tags: [session, decay, ai-memory, pr]
lastUsed: 2026-08-31
---

# Session: PR #9 resolved as redundant, then `lastUsed` decay shipped — PR #10

**2026-08-31 13:03–16:56 · project memoria · claude-code · commit `82490e0` · branch `feat/session-decay` · PR #10 (open, not approved)**

## Part 1: PR #9 conflict investigation — closed as redundant

Request: open a PR for a branch holding one commit (a search fix) so the user could switch to `main` without losing it. `gh pr create` opened **PR #9**. The user then reported main had conflicts, likely from a fix pushed from another machine days earlier — asked whether the merge was actually necessary. Investigation: `git fetch` + `git cherry origin/main f4d504e` plus a direct byte-diff between the branch's `f4d504e` and main's `8c0432e` confirmed the two patches were **byte-identical** — the fix had already landed on main via a different PR (#7) from another machine. Verdict: PR #9 is unnecessary; the branch's only unique content was a wiki docs commit (`d7d9239`, some of it already stale — it referenced PR #7 as "still open, unmerged"). The user chose to switch to `main` and pull rather than merge or cherry-pick: fast-forwarded to `5514afb`, which already included the search fix (released as v0.10.2) plus new `list`/`remove` commands (v0.11.0, from unrelated prior work). PR #9 and its branch were left open/undeleted at the user's choice.

## Part 2: ai-memory comparison Q&A — no code, four questions answered

Extended back-and-forth (each answer read the actual codebase — `mcp.go` tool descriptions, `search.go`, `run.go`, existing wiki research pages — before answering) working through which ai-memory ideas memoria should adopt:

- **Why does ai-memory use SQLite, and does memoria need it?** No — memoria's tiny corpus and already-solved write concurrency ([[decisions/0007-queue-safety-via-file-locking]]) don't justify it; it would cost the plain-markdown differentiator ([[decisions/0001-plain-markdown-no-db]]).
- **Can memoria be more decoupled from LLM processing, like ai-memory?** It already is — capture and cross-harness handoff are zero-LLM; only consolidation/lint use an LLM, and that stage is optional and async.
- **How does memoria's search differ from FTS5, and is FTS5 worth adding?** Explained the mechanical difference (linear substring scan vs. inverted index); not worth adding at current wiki scale, multi-word AND matching flagged as the cheaper next step if ever needed (not built).
- **What is link-neighbor ranking and decay in ai-memory, and are they good fits?** Link-neighbor: not adopted, memoria's wikilink graph is already used at read time. Decay: adopted, in simplified deterministic form.

Design captured in [[decisions/0013-deterministic-decay-over-salience-model]].

## Part 3: `lastUsed` decay implemented and shipped

User sharpened the decay ask mid-conversation: date-only `lastUsed` frontmatter on `sessions/` pages, updated deterministically — never by an LLM digest — only when the CLI/MCP delivers a session on resume or search; plus a cron job every 3 hours (background, never affecting a running agent) that soft-deletes stale pages after 15 days and hard-deletes after 30, both user-configurable. Full TDD build on branch `feat/session-decay`: two background Explore/Plan subagents first mapped every wiki write/read touchpoint across `process.go`, `digest.go`, `mcp.go`, `search.go`, `run.go`, `lint.go`, `seed.go`, `bootstrap.go`, `wikigit.go`, and existing test fixtures/stub idioms, before any code was written. Full implementation, test coverage, config, prompt/docs updates, and the sweep-scheduling/locking reasoning are recorded in [[concepts/session-decay]] — not repeated here.

Verification: 381 tests green under `-race`; a real compiled binary was smoke-tested end to end (sweep trash/purge/adopt behavior, idempotence within a day, single-hit piped search stamping a page vs. multi-hit listings not stamping). Smoke-testing caught one real bug before commit — a page past both decay thresholds being trashed *and* purged in the same run, plus mid-day clock reads aging pages early — fixed by comparing dates day-to-day and running the hard pass before the soft pass, so a freshly-trashed page always survives at least one sweep interval.

Committed as **`82490e0`** — `feat(decay): age out unused session pages via lastUsed stamps` — pushed, and **PR #10** opened against `main`, deliberately left unapproved per explicit instruction ("do not approve it"). Version bumped to **0.12.0**; merging will auto-tag and publish the release automatically via the existing CI pipeline ([[concepts/ci-release-pipeline]]). The old `fix/search-global-scope` branch and PR #9 were noted as still around, left for the user to clean up later.