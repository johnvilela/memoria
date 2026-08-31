---
tags: [research, ai-memory, workstreams, handoff, cross-agent]
---

# ai-memory workstreams compared with memoria agent handoff

Research conducted on July 29, 2026. The upstream evaluation is pinned to
ai-memory `v1.19.2`, commit
[`2f8cb7e`](https://github.com/akitaonrails/ai-memory/commit/2f8cb7e85cfbc92e111b54597671efc4df96863e).

## Bottom line

ai-memory's workstream is not merely a better session summary. It is a
cross-harness synchronization protocol.

memoria currently provides:

- Native continuation when returning to the same harness.
- A pointer to a lossy session digest when changing harnesses.
- Separate, asynchronous LLM consolidation for durable knowledge.

ai-memory provides:

- A persistent logical workstream independent of any agent session.
- One linked native session per harness.
- A deterministic ledger of visible conversation and tool history.
- Automatic delivery of only the unseen delta before the receiving agent's
  first turn.

That architectural separation explains most of the speed and naturalness
difference.

## ai-memory has two handoff systems

The ordinary/direct-launch path is:

1. Hooks capture bounded observations.
2. On a true `SessionEnd`, the server immediately creates a rule-based session
   page and a short single-use handoff.
3. The next agent's `SessionStart` hook fetches and injects that handoff.
4. LLM consolidation happens separately and optionally.

The managed path, invoked through `ai-memory run`, adds the workstream ledger.
This is the mechanism responsible for its highest-quality cross-agent
continuation. The upstream
[architecture document](https://github.com/akitaonrails/ai-memory/blob/2f8cb7e85cfbc92e111b54597671efc4df96863e/docs/ARCHITECTURE.md)
describes both paths.

One limitation is that direct Codex launches lack a reliable true session-end
hook, so ai-memory documents `finalize-session` for that path. Managed runs do
not depend on this for continuity because the launcher imports the native
transcript when the child exits.

## How the managed workstream works

### 1. Select the logical workstream

Repository and worktree fingerprints identify the checkout. The most recently
selected workstream is used, with `default` created on first use. `--new NAME`
creates another line of work; `--workstream NAME` returns to one.

### 2. Acquire ownership

The server opens a renewable 90-second lease. Only one launcher can change a
workstream's session pointers and delivery cursors at a time. A killed launcher
eventually releases ownership by lease expiry.

### 3. Resolve the native session

Each workstream remembers its current native session separately for Claude,
Codex, OpenCode, Pi, Crush, Kimi, OMP, and Grok. Returning to a harness uses
that harness's native resume command. A newly joining harness starts fresh and
receives portable history.

An empty workstream may adopt one of up to eight existing same-checkout native
sessions. Adoption is deliberately a one-time bootstrap; established
workstreams do not accidentally attach unrelated newer sessions.

### 4. Inject the unseen delta

`AI_MEMORY_RUN_ID` tells lifecycle hooks this is a managed run. At
`SessionStart`, the server links the observed native session and injects the
events that this particular harness session has not seen.

The rendered packet:

- Is capped at 30,000 characters.
- Caps each event at 6,000 characters.
- Labels foreign tool calls and results as completed historical evidence.
- Includes source-harness provenance and a Git checkpoint.
- Tells the agent to continue from the current checkout.
- Points to `workstream-search` when older omitted history is needed.

See the upstream
[packet renderer](https://github.com/akitaonrails/ai-memory/blob/2f8cb7e85cfbc92e111b54597671efc4df96863e/crates/ai-memory-hooks/src/router.rs#L1423-L1490).

### 5. Run the agent normally

Native arguments pass through. Claude resumes with `--resume`, Codex with
`resume`, and so on. Harnesses without usable SessionStart output have
specialized delivery paths: Crush gets a temporary context file, Kimi uses
`UserPromptSubmit`, and Grok gets `--rules`.

### 6. Import the transcript on exit

The launcher briefly waits for the native transcript to flush, then reads the
harness's store without modifying it. Adapters extract visible user and
assistant messages, completed tool calls and results, and compaction summaries.
Hidden reasoning, private records, and unsupported data are excluded and
recorded as extraction-loss annotations.

See the upstream
[transcript adapters](https://github.com/akitaonrails/ai-memory/blob/2f8cb7e85cfbc92e111b54597671efc4df96863e/crates/ai-memory-workstream/src/transcript.rs#L72-L357).

### 7. Commit cursors atomically

Deterministic event hashes prevent duplicates. Source cursors record how much
native history was imported; delivery cursors record how much each receiving
session has seen. The events enter an append-only SQLite/FTS ledger plus
immutable sanitized JSONL segments.

The upstream
[store implementation](https://github.com/akitaonrails/ai-memory/blob/2f8cb7e85cfbc92e111b54597671efc4df96863e/crates/ai-memory-store/src/workstream.rs#L165-L746)
handles leases, linkage, deduplication, and cursor updates transactionally.

The complete documented behavior is in
[Managed cross-harness workstreams](https://github.com/akitaonrails/ai-memory/blob/2f8cb7e85cfbc92e111b54597671efc4df96863e/docs/managed-workstreams.md).

## Comparison with memoria

| Area | memoria | ai-memory managed workstream |
|---|---|---|
| Continuity identity | Individual session ID | Persistent logical workstream |
| Same-harness return | Native resume for Claude/Codex | Native resume per linked harness |
| Cross-harness return | New session with a digest-path prompt | New or resumed session with unseen events injected |
| Captured source | memoria hook projection | Harness's native transcript |
| Fidelity | Prompts, limited tool metadata, final stop text | Visible conversation, tool calls/results, compactions |
| Delivery | Agent must read and interpret a file | Context already present before first turn |
| Incrementality | Read the selected digest | Per-session delivery cursor |
| Repository state | Inferred from commands and files | Explicit Git checkpoint |
| Task branching | Recent-session picker | Named workstreams per repository/worktree |
| LLM dependency | Durable synthesis requires processor | Operational continuity requires no LLM |
| Supported native adapters | Claude and Codex | Eight harnesses |

Our same-harness continuation is already comparable:
[`run.go`](../../cmd/run.go) calls the harness's native resume command.

The large gap begins at cross-harness continuation. memoria sends this generic
instruction:

> Read the session digest at ... then continue that work.

The receiving agent must open the file and reconstruct the work from memoria's
shadow log. That log deliberately excludes Read, Grep, and most tools;
Write/Edit retain primarily the path, Bash retains the command, and ordinary
successful tool output is generally absent. See
[`hook.go`](../../cmd/hook.go).

There is also no logical task link connecting the old Claude session, new Codex
session, and later Claude return. They are separate sessions selected from an
append-only recent list.

## Why ai-memory feels much faster and more natural

### 1. It separates continuity from knowledge consolidation

Operational continuation is deterministic. LLM work improves the durable wiki
later. memoria has a cheap but weak raw digest and a slow but higher-quality
LLM/wiki pipeline, without a strong deterministic layer between them.

### 2. It pushes context instead of asking the agent to pull it

The receiving agent begins its first turn already informed. With memoria, the
agent spends its first turn reading, interpreting, and frequently re-inspecting
the repository.

### 3. It captures substantially better evidence

Native transcripts contain the actual conversation and completed tool results.
memoria's hooks produce a compact but lossy event projection.

### 4. It sends a bounded delta

Each native session receives only what it has not seen. memoria points at an
entire digest, which can contain unbounded-length lines, and a resumed
incarnation may require following `continues_from` links manually.

### 5. Its continuity follows the work, not the chat

Switching Claude to Codex and then back to Claude stays inside one workstream.
The final Claude launch resumes Claude's linked native session and receives
only Codex's new contribution.

### 6. Its packet tells the model how to interpret it

The packet explicitly marks historical tools as completed, identifies
provenance, makes the latest checkout authoritative, and provides a search
fallback. memoria's generic prompt leaves reconstruction policy to the
receiving model.

### 7. It moves work to the previous agent's exit

Transcript extraction and import happen when the managed child finishes. The
next startup mostly performs bounded SQLite reads and context rendering.

## Important correction about LLM latency

memoria's cross-harness `run` does not itself wait for an LLM. The problem is
subtler: it has no fast, high-quality handoff artifact.

Its high-quality LLM processing can take minutes and reads the current wiki plus
ended digests, but `memoria run` does not consume the resulting wiki page. It
still points at the raw digest. See [`process.go`](../../cmd/process.go).

Therefore, the perceived delay is not primarily launcher latency. It is
time-to-competence: the receiving model must reconstruct intent, decisions,
failures, repository state, and next steps from incomplete evidence.

## Evaluation and tradeoffs

ai-memory's workstream implementation is convincingly engineered:

- Explicit renewable leases.
- Transactional delivery claims.
- Deterministic event IDs.
- Incremental source and delivery cursors.
- Recovery after incomplete launches.
- Read-only access to native stores.
- Packet-origin markers to prevent imported context from recursively entering
  the ledger again.
- Extensive deterministic adapter and state-machine tests.

Its real-harness acceptance suite is opt-in rather than ordinary CI because it
requires installed agents, credentials, and model calls. No independent latency
benchmarks were found, so "faster" is an architectural conclusion rather than a
measured benchmark claim.

The price is substantial:

- A continuously running server.
- SQLite, FTS, and immutable raw storage.
- Eight parsers for private, changeable native transcript formats.
- Harness-specific injection workarounds.
- More installation, upgrade, privacy, and recovery surface.

memoria is dramatically simpler: one Go binary, repository-local markdown, no
daemon, and no native-store dependencies. That simplicity is valuable, but it
currently optimizes durable project knowledge while ai-memory has invested in a
separate operational-continuity substrate.

## Central conclusion

ai-memory's natural handoff comes from treating cross-agent continuation as
deterministic state synchronization.

memoria currently treats cross-agent continuation as giving the new model a
summary-shaped file and asking it to catch up.

The transferable architectural insight is to keep two different jobs separate:

1. **Operational continuity:** immediate, deterministic, bounded, incremental,
   and injected before the next turn.
2. **Durable knowledge consolidation:** slower, LLM-driven, curated, and allowed
   to run asynchronously.
