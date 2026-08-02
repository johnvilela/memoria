---
tags: [research, ai-memory, memoria, comparison, reliability, privacy, concurrency]
---

# How ai-memory handles the flaws found in Memoria

## Research basis

This comparison starts from [[research/memoria-evaluation]] and maps each flaw in that evaluation to the current implementation of [`akitaonrails/ai-memory`](https://github.com/akitaonrails/ai-memory).

Research date: **2026-08-01**.

Upstream source inspected: default branch commit [`e714d992993b5ee76a76e09e64ac5cb441e14aed`](https://github.com/akitaonrails/ai-memory/commit/e714d992993b5ee76a76e09e64ac5cb441e14aed), whose changelog identifies release **1.22.0**. All source links below are pinned to that commit so the findings remain reproducible.

This is newer than the ai-memory 1.19.2 comparison in [[research/ai-memory-workstream-comparison]]. Several security, retrieval, lifecycle, and managed-workstream changes landed after that earlier research.

## Executive conclusion

ai-memory deals with most of Memoria's engineering flaws more comprehensively because it has a persistent server, SQLite transactions, a single writer actor, stable UUIDs, a sanitization boundary, and a richer job/audit model. Those mechanisms give it stronger concurrency, recovery, retrieval, and privacy properties.

It does **not** eliminate every flaw:

- Default direct-hook routing still derives the project from `basename(cwd)`, so same-named repositories can collide unless a marker defines a workspace/project or repo-root strategy.
- Its LLM lint pass still reads only the first 400 characters of at most 20 pages.
- Ordinary multi-page consolidation writes directly through the atomic batch writer; it does not get the stage-time conflict detection used by auto-improvement proposals.
- Some structured-output limits, notably tag count and the maximum number of batch updates, are stated in prompts but not represented as hard collection-size constraints in the Rust types.
- Sanitization and prompt boundaries reduce privacy and prompt-injection risk but explicitly do not claim complete DLP or perfect semantic injection prevention.

The architectural price is significant: ai-memory is a server application with SQLite, FTS, optional embeddings, network authentication, migrations, background schedulers, and harness-specific native transcript adapters. Memoria remains much easier to inspect and operate.

## Status summary

| Memoria flaw | ai-memory status | Short answer |
|---|---|---|
| Same-basename project collision | **Partially addressed** | Storage uses workspace/project UUIDs, but default routing still begins from `basename(cwd)`; `.ai-memory.toml` is the explicit collision escape hatch. |
| Stale proposals overwrite newer edits | **Addressed for auto-improvement; partial elsewhere** | Staged proposals snapshot the target ID/hash/time and become `conflict` if it changed. Direct consolidation still writes without that proposal snapshot. |
| Starting one session ends other live sessions | **Addressed** | Sessions close on a real SessionEnd or explicit finalize action, not merely because another session started. |
| Non-atomic background-job claiming | **Addressed** | Writer transactions, idempotency keys, per-key gates, non-overlapping scheduler ticks, and managed-workstream leases replace check-then-spawn state files. |
| Weak privacy controls | **Substantially mitigated** | Typed sanitization boundary, capture exclusions, bounded bodies, private spool permissions, double opt-in assistant capture, and explicit trust boundaries. Not complete DLP. |
| Entire wiki sent on every consolidation | **Addressed** | Consolidation uses bounded session observations and limited current-page/slot context; recall uses indexed retrieval. |
| Lossy hook capture and weak handoff evidence | **Partially addressed** | Direct capture stays deliberately narrow; managed workstreams import visible native transcript events with cursors and deterministic IDs. |
| Structural-only LLM validation | **Substantially improved, not complete** | JSON Schema, typed enums, validated paths, sanitized writes, canonical frontmatter, and atomic batches; some cardinality/tag limits remain advisory. |
| Lint false confidence from 400-character previews | **Still present in the LLM layer** | Rule-based lint is much richer, but the contradiction pass still sees 400-character previews and only 20 selected pages. |
| Scheduler cannot find agent CLI on PATH | **Architecturally avoided** | Maintenance runs inside the server through configured LLM APIs; it does not start `claude` or `codex` from each scheduler firing. |
| MCP polling automatically repeats paid failures | **Addressed** | Provider work is durably queued with backoff/recovery or explicitly rerun; scheduled auto-improvement records claims so failures do not retry forever. |
| Weak provenance and stale-memory handling | **Substantially improved** | Supersession chains, page hashes, audit events, source evidence, feedback, TTL, git checkpoints, and restore commands. Semantic truth can still become stale. |

## Detailed comparison

### 1. Project identity and same-name collisions

ai-memory's storage identity is much stronger after scope resolution:

- Workspaces and projects receive UUID primary keys.
- Every page is identified by `(workspace_id, project_id, path)`.
- On-disk directories use UUIDs, so renaming a project changes a database column rather than moving all page paths.
- SQLite enforces one project name per workspace with `UNIQUE (workspace_id, name)`.

These invariants are visible in the initial [database schema](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/crates/ai-memory-store/migrations/V01__init.sql#L1-L48) and the [lifecycle documentation](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/docs/lifecycle-ops.md#L28-L65).

However, the routing input is not automatically collision-proof. The default is still `workspace = "default"` and `project = basename(cwd)`. Therefore `/clients/a/api` and `/clients/b/api` can still resolve to the same logical `default/api` project. Stable UUID storage prevents accidental filesystem overlap between *distinct resolved projects*; it cannot distinguish two paths that were resolved to the same logical project in the first place.

ai-memory addresses this through its [`.ai-memory.toml` marker](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/docs/marker-file.md#L1-L22): users can set an explicit workspace, explicit project, or `project_strategy = "repo-root"`. The marker documentation explicitly names multi-client consultancies, monorepos, and worktrees as the motivating cases.

**Verdict:** better internal identity, but the original collision is only eliminated when routing is configured correctly. Memoria should adopt stable internal project IDs *and* reject or warn about duplicate display names instead of relying on users to notice them.

### 2. Stale proposals and destructive overwrites

ai-memory's auto-improvement proposal path directly implements the conflict protection recommended for Memoria:

- A proposal stores the target page ID, body SHA-256, and `updated_at` observed at staging time.
- Approval reloads the latest target inside a transaction.
- A create conflicts if a page now exists.
- An update conflicts if the page ID, body hash, or timestamp changed.
- Pinned pages are refused by the same apply-time enforcement point.
- Every staged/approved/rejected/failed/conflict transition is appended to an event history.

The module describes this contract at the top of [`auto_improve.rs`](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/crates/ai-memory-store/src/auto_improve.rs#L3-L16), and the transaction performs the snapshot comparison in [`approve_proposal`](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/crates/ai-memory-store/src/auto_improve.rs#L922-L1059).

The filesystem/database write path is also stronger. `Wiki::apply_batch` stages temporary files, installs the files with rollback snapshots, commits page versions in one SQL batch, and restores prior files if the SQL write fails. See [`Wiki::apply_batch`](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/crates/ai-memory-wiki/src/wiki.rs#L1192-L1350). Git checkpoints and `restore-page` provide another recovery layer.

There are two important qualifications:

1. Auto-improvement is auto-approved by default unless `[auto_improve] require_approval = true`. This is more aggressive than Memoria's manual-review default, although ai-memory still stages, audits, validates, and conflict-checks the proposal first.
2. `memory_consolidate` multi-page output goes directly to `Wiki::apply_batch`. The prompt contains bounded session observations and slot context, not the full current bodies of arbitrary target pages. That path benefits from atomicity, supersession, and Git recovery, but not the auto-improvement proposal's stage-time semantic conflict check.

**Verdict:** ai-memory solves stale reviewed-proposal clobbering very well in its proposal pipeline, but direct consolidation can still replace an existing page without proving that it incorporated the latest body.

### 3. Concurrent and abandoned sessions

ai-memory does not use “a new session ends every other session in the project.” Its canonical close occurs on a real SessionEnd event. Clients without a reliable true SessionEnd, including direct Codex launches, use `ai-memory finalize-session`. A repeated SessionEnd is processed again only when the persisted observation generation advanced, so retries and resumed sessions converge without treating an unrelated new session as proof that the old one ended. See the [steady-state session flow](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/docs/ARCHITECTURE.md#L45-L95).

Managed workstreams deliberately allow only one launcher to mutate a logical workstream at a time. They use a renewable 90-second lease over a stable repository/worktree fingerprint; two terminals cannot silently race native-session pointers or delivery cursors. Normal failures cancel the lease, `kill -9` is recovered by expiry, and later imports resume from committed cursors. See [managed-workstream synchronization](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/docs/managed-workstreams.md#L136-L173) and [lease recovery](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/docs/managed-workstreams.md#L341-L363).

**Verdict:** addressed. The tradeoff is that clients missing SessionEnd require an explicit finalize step rather than an unsafe inference.

### 4. Atomic job claiming, idempotency, and recovery

ai-memory replaces Memoria's status-file check-then-spawn sequence with several coordinated mechanisms:

- One server-side writer actor serializes store mutations.
- Native hook spool entries have stable idempotency keys.
- The key and observation commit together.
- Overlapping retries of the same project/key are serialized by a bounded gate.
- Scheduler ticks do not overlap; a long pass delays the next tick.
- Managed runs use transactional leases and delivery/source cursors.
- SessionEnd inserts the handoff, ends the session, and records the covered observation generation in one transaction.
- Wiki batches stage files and use rollback around their single SQL batch.

The complete lifecycle is documented in [ARCHITECTURE.md](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/docs/ARCHITECTURE.md#L23-L95). The changelog also records that spool retries reuse a stable idempotency key and that overlapping deliveries are serialized ([1.18.0](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/CHANGELOG.md#L638-L690)).

The design is explicitly at-least-once for downstream effects until an idempotency key is marked complete. A crash may repeat an already-applied effect, but the system prefers a recoverable duplicate over silently dropping the remaining work.

**Verdict:** addressed much more robustly than Memoria's PID/status YAML slot.

### 5. Privacy, secret capture, and prompt injection

ai-memory has a substantially stronger privacy boundary:

- The writer accepts only `Sanitized<NewObservation>`, making sanitization a type-level persistence requirement.
- Built-in credential patterns run before storage; operators can add `extra_patterns` and an `allowlist`.
- Native capture is bounded. User prompts and compaction summaries retain at most 16 KiB; tool excerpts are much smaller, with a durable observation backstop.
- Per-repository `ignore_paths` rules drop recognized file-tool events before spool, network transport, logs, or storage.
- The local hook spool is created `0700` and files are created `0600` on Unix ([hook spool implementation](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/crates/ai-memory-cli/src/commands/hook_spool.rs#L138-L188)).
- Assistant final-turn capture is off by default and requires a client/server double opt-in. When enabled, it is sanitized on both sides and capped.
- Stored prompts, wiki pages, tool output, handoffs, and workstream packets are explicitly labeled untrusted historical data rather than instructions ([1.20.1 security change](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/CHANGELOG.md#L339-L355)).

Capture exclusions are useful but intentionally narrow: they match recognized file-tool paths, do not resolve every alias/symlink case, and do not parse shell commands, prompts, quoted content, or arbitrary patches. The [marker documentation](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/docs/marker-file.md#L130-L170) calls this a capture boundary rather than complete DLP.

The upstream [threat model](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/SECURITY.md#L12-L125) also acknowledges remaining limitations:

- No encryption at rest.
- All authenticated users belong to one trust domain; there are no private per-user memories.
- A cloud LLM still receives the bounded material used for consolidation or optional reranking.
- Manually edited wiki files may contain unsanitized data.
- Sanitization is best effort, and no text filter can prove that every prompt injection will be ignored.

**Verdict:** substantially mitigated and considerably safer by default, but not solved in the absolute sense.

### 6. Prompt and wiki growth

ai-memory avoids sending the complete wiki on every consolidation.

The consolidation prompt projects only the target session's observations with hard bounds:

- At most 256 projected observations.
- At most 3,000 characters per observation body.
- A 400,000-character total observation budget.
- At most 20,000 characters from the current single-page body.
- Project-specific consolidation instructions capped at 2,000 characters.
- Bounded slot snapshots rather than the whole wiki.

These limits are defined in the [consolidator prompt builder](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/crates/ai-memory-consolidate/src/consolidator.rs#L745-L970). The upper bound is still large, but it is fixed; it does not grow linearly with every page ever written.

Recall uses local FTS5, entity matching, graph-neighbor RRF, authority adjustment, and optional vector search. Only bounded hits/snippets enter responses or the optional LLM reranker. The retrieval pipeline is described in [ARCHITECTURE.md](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/docs/ARCHITECTURE.md#L96-L130).

Auto-improvement separately enforces limits for input tokens, patchable pages, edits, changed characters, output body size, and proposals per run; the documented defaults include a 24,000-token input and five proposals per run ([auto-improvement limits](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/docs/auto-improvement-loop.md#L296-L320)).

**Verdict:** addressed. This is one of the clearest advantages of ai-memory's derived index over Memoria's “send every Markdown page” approach.

### 7. Capture fidelity and cross-harness continuity

Direct ai-memory hook capture remains deliberately lossy, like Memoria's. It stores a narrow normalized event projection, not a complete transcript. PreToolUse does not retain commands, paths, arguments, input bodies, or arbitrary tool names; assistant Stop content is absent unless explicitly enabled. The exact direct-capture contract is documented in [marker-file.md](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/docs/marker-file.md#L203-L225).

Managed workstreams add the missing higher-fidelity layer for supported harnesses:

- Read the native transcript store without modifying it.
- Import visible user/assistant messages, tool calls/results, and compaction summaries where the harness exposes them.
- Exclude hidden reasoning, system prompts, subagent/private trajectories, and injected packets.
- Use deterministic event IDs, immutable sanitized JSONL segments, source cursors, delivery cursors, and prefix hashes for stores that can be rewritten.
- Deliver only the unseen bounded delta and expose full-ledger search for older events.

The managed protocol is summarized in [managed-workstreams.md](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/docs/managed-workstreams.md#L136-L173).

This is stronger than Memoria's hook-only evidence, but it costs ai-memory a growing set of adapters for private and changeable native formats. Some harnesses remain hook-ledger-only because their transcript representation is private or undocumented.

**Verdict:** partially addressed in direct mode and substantially addressed in opt-in managed mode.

### 8. LLM output validation

ai-memory improves the trust boundary through:

- JSON-Schema structured completion.
- Rust enum types for page tier/kind/slot kind.
- `PagePath` validation before filesystem resolution.
- Entity normalization capped at 10 names and 64 characters per name.
- Sanitization at the final wiki write boundary, including webhook-mutated content.
- Canonical frontmatter generation.
- Atomic multi-page application with rollback.
- Admission webhooks and operator-specific slot namespace checks.

The structured types are defined in [`types.rs`](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/crates/ai-memory-consolidate/src/types.rs#L1-L170), and the batch writer performs sanitization/canonicalization before installing files in [`wiki.rs`](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/crates/ai-memory-wiki/src/wiki.rs#L1200-L1350).

Some limits remain prompt-level rather than hard type constraints:

- `tags` is a plain `Vec<String>` described as “up to ~5,” without an evident collection-size validator in the type.
- `ConsolidatedBatch.updates` is a plain vector even though the prompt asks for 1-5 updates.
- Empty titles/bodies and duplicate target paths need to be rejected or normalized somewhere outside these public types; the schema alone does not encode every semantic invariant.

**Verdict:** much stronger than Memoria's path-and-empty-body checks, but still worth auditing for prompt-only limits.

### 9. Lint coverage and stale-memory detection

ai-memory's deterministic lint layer is significantly broader than Memoria's:

- Stale unused episodic pages.
- Empty bodies.
- Duplicate titles across paths.
- Broken cross-project wikilinks.
- Explicit `stale`/`wrong` feedback from users or agents.
- Durable-rule suggestions.
- Git-tracked lint reports.

However, its LLM contradiction layer preserves the exact blind spot identified in Memoria. It chooses at most 20 high-access semantic/procedural pages and sends only the first 400 characters of each page. See [`contradiction_pass`](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/crates/ai-memory-consolidate/src/lint.rs#L295-L343).

This means the rule-based results can be useful and globally derived, but “no LLM contradictions found” still does not establish that the entire wiki is consistent. Low-access pages and claims below the first 400 characters remain invisible to that pass.

**Verdict:** partially addressed; deterministic lint is stronger, while semantic contradiction coverage remains shallow.

### 10. Scheduler PATH and provider execution

Memoria's scheduler starts `memoria process --all`, which later looks up `claude` or `codex` from the scheduler's PATH. ai-memory avoids this coupling: its persistent server owns maintenance scheduling and invokes configured LLM/embedding providers through their provider integrations. Scheduler ticks operate inside the already-running process and are explicitly non-overlapping.

The agent harness executable is relevant to interactive `ai-memory run`, not to a background wiki-maintenance timer. The server still needs its own configuration and credentials, but it is not dependent on a shell startup file making an agent CLI visible at every scheduled run.

**Verdict:** architecturally avoided rather than patched.

### 11. Paid retries and error recovery

ai-memory distinguishes retryable transport/provider work from explicit user decisions:

- Native hook spool retries reuse an idempotency key.
- Opt-in SessionEnd provider work is durably queued, retried with backoff, and recovered after restart.
- Scheduled auto-improvement claims a session before LLM work so a failed scheduled review does not retry forever.
- Explicit CLI/admin/MCP calls remain the catch-up/rerun path.
- Managed-workstream heartbeats have bounded timeouts and a fixed lease-safe cadence rather than being triggered by arbitrary polling frequency.

The user-facing behavior is summarized in [usage.md](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/docs/usage.md#L85-L95), while scheduler claim behavior appears in the [architecture flow](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/docs/ARCHITECTURE.md#L75-L115).

**Verdict:** addressed. Polling status does not itself become an unlimited paid retry button.

### 12. Provenance, stale knowledge, and recovery

ai-memory has more provenance and recovery structure than Memoria:

- Page versions form a supersession chain in SQLite.
- Page rows carry SHA-256 hashes and timestamps.
- Markdown is Git-versioned and remains the source of truth.
- Auto-improvement stores provider/model, evidence, before/after snapshots, rejected candidates, proposal actor, approver, and append-only status events.
- Automated proposals cannot rewrite pinned pages.
- `memory_feedback` records helpful/not-helpful/stale/wrong signals.
- Page TTL (`expires_at`) removes time-bounded knowledge from normal retrieval.
- Retrieval uses positive and negative authority tags such as `canonical`, `source-of-truth`, `superseded`, and `historical`.
- `checkpoints` and `restore-page` provide targeted recovery from a bad write ([restore workflow](https://github.com/akitaonrails/ai-memory/blob/e714d992993b5ee76a76e09e64ac5cb441e14aed/docs/lifecycle-ops.md#L356-L395)).

These mechanisms make stale or incorrect knowledge detectable and recoverable, but they do not prove semantic truth. A wrong page may remain current if nobody flags it, no newer evidence contradicts it, and lint does not see the conflicting passage.

**Verdict:** substantially improved provenance and operational recovery, with the normal irreducible limit that semantic freshness still needs evidence and review.

## What Memoria can adopt without becoming ai-memory

Several of ai-memory's strongest ideas fit Memoria's simpler one-binary, Markdown-first architecture:

1. **Stable project IDs.** Generate a UUID during bootstrap, persist it in config, and key queue/status/log state by it. Keep project basename only for display.
2. **Proposal target snapshots.** Store each target page's SHA-256 in `proposal.json`; refuse apply when the current hash differs.
3. **Atomic project job lease.** Hold a per-project flock through claim/spawn ownership, or write a lease record with an owner token and expiry under the lock.
4. **Atomic batch writes with rollback snapshots.** Stage every output in the target directory, preserve old bytes, install all, and restore on failure.
5. **Strict output normalization.** Enforce 1-5 unique paths, 0-5 kebab-case tags, bounded title/body lengths, and duplicate-target rejection in Go.
6. **Private raw storage.** Use `0700` directories and `0600` raw digest/proposal/log files.
7. **Sanitization boundary.** Redact common credential formats before a hook event reaches disk, with configurable extra patterns and explicit allowlists.
8. **Capture exclusions.** Add project-local ignore patterns for recognized file events while clearly documenting that shell text and free-form prompts are not complete DLP.
9. **Bounded relevant context.** Stop sending every wiki page. Select candidates with existing text/tag search plus wikilinks and page hashes; embeddings are optional.
10. **Deterministic lint checks.** Broken links, duplicate titles/paths, missing headings/frontmatter, expired pages, and stale source references can be checked without an LLM.
11. **Durable retry manifests.** Record attempt count, next-attempt time, terminal state, and explicit manual retry instead of retrying whenever MCP polls.
12. **Page provenance.** Add source session IDs, source commit, generation time, processor/model, and prior body hash to generated frontmatter or an adjacent audit manifest.

## What probably should not be copied yet

The following ai-memory mechanisms solve real problems but would change Memoria's core product substantially:

- A permanently running HTTP server.
- SQLite as the authoritative operational state and derived search index.
- FTS5/vector/entity/graph fusion from day one.
- Multi-user authentication and network deployment modes.
- Native transcript parsers for every supported harness.
- Per-harness delivery workarounds and native-store cursors.
- A large migration and recovery subsystem.

Memoria's differentiator is that a user can understand the system as hooks + cron + Markdown in the project. Stable IDs, conflict hashes, privacy filtering, bounded context, and deterministic lint preserve that differentiator. A server, database, and native transcript adapter matrix would not.

## Final assessment

ai-memory demonstrates that the highest-risk flaws in Memoria are solvable:

- Use stable internal identity rather than display names.
- Treat background work as leased/idempotent state, not a PID convention.
- Snapshot proposal inputs and refuse stale applies.
- Put sanitization and size limits before persistence.
- Retrieve bounded relevant context instead of replaying the whole wiki.
- Preserve an audit trail and make bad writes recoverable.

The most useful lesson is not “turn Memoria into ai-memory.” It is to transplant the small invariants that give ai-memory its safety: UUID identity, target hashes, atomic leases, private permissions, bounded input, typed validation, and explicit provenance.

The main unresolved shared weakness is semantic maintenance. Both tools still use shallow page previews for contradiction lint, and neither can guarantee that model-generated historical text remains true without stronger provenance, repository verification, and human feedback.
