---
tags: [handoff, ai-memory, research, comparison]
---

# Handoff between agents: memoria vs ai-memory

Findings from a research-only comparison of memoria's agent handoff with Fabio Akita's [ai-memory](https://github.com/akitaonrails/ai-memory) ([[sessions/0075f9e5-98eb-4d2e-bd20-3e8f5c52b069]]). These are recorded analysis and candidate ideas — none of it is implemented in memoria.

## How memoria hands off today

Same harness → native resume (`claude --resume`, `codex resume`); different harness → a one-sentence handoff prompt pointing at the session digest (`run.go:111`), with the digest content never inlined ([[concepts/recall-and-run]]). The digest itself is memoria's own capture: `@hook` annotated one-liners — prompts, Write/Edit/Bash, assistant text only at `stop` ([[concepts/session-capture]]). A high-quality handoff artifact requires an LLM digest/consolidate pass, which runs detached and takes minutes ([[decisions/0003-never-block-the-agent]]).

## How ai-memory works

- **Workstream** is the unit of continuity: a persistent line of work tied to repo+worktree, independent of any harness. Default one per repo; branch via `--new NAME`, return via `--workstream NAME`. Single-writer via a 90-second renewable lease — kill -9 safe: the lease expires and the next run resumes from the committed cursor.
- **Linked sessions**: each harness's native session gets a linked id tracked by the workstream, per harness. Same harness again → plain native resume of the linked session.
- **Cross-harness handoff**: an adapter reads the prior harness's own native transcript store, read-only (`~/.claude/projects/**/*.jsonl`, codex `rollout-*.jsonl`, opencode.db, etc. — 8 harnesses supported), extracts a bounded "portable history" (visible user/assistant messages, completed tool calls, compaction summaries — never the full transcript), and **injects the packet into the new harness at the SessionStart hook, before the first turn** — the model wakes up already caught up. The packet carries a versioned origin marker so the Read tool excludes marked content and nothing re-imports in a loop.
- **Ledger**: append-only JSONL immutable segments per workstream (visible history, tool results, compaction summaries, git checkpoints, extraction-loss annotations) with deterministic event IDs (content hashes) and incremental per-adapter cursors, so retries never duplicate; searchable via `workstream-search`.
- **Adoption**: the first managed run in an empty workstream offers up to 8 recent native sessions from cwd to adopt — bootstrap only; once established, links rule.

## The comparison, side by side

| Aspect | memoria | ai-memory |
|---|---|---|
| Unit of continuity | session digest file | workstream (first-class, cross-session, cross-harness) |
| Capture source | own hooks, lossy one-liners | harness's native transcript store, full visible fidelity |
| Cross-harness handoff | one-sentence prompt pointing at the digest | context packet injected at SessionStart before first turn |
| LLM needed for good handoff | yes (detached digest pass, minutes) | no — deterministic extraction |
| Session linkage | none — new session reads about old one | linked ids + cursors, thread continues, dedup guaranteed |
| Harness resume support | 2 (claude, codex) | 8 |
| Infra | Go binary, markdown, no daemon | Rust server daemon, SQLite+FTS5, per-harness store parsers |

## Why theirs is faster — three root causes

1. **Zero-LLM handoff path.** Deterministic extraction from native stores takes milliseconds. memoria's good handoff artifact needs a `claude -p`/`codex exec` pass (detached, minutes, 10-min timeout) — if the user switches agents right after a session ends, the digest is still raw hook lines.
2. **Push, not pull.** They prepend context so the model's first turn is already informed. memoria hands a path and burns the model's first turn on Read plus reconstruction from skeletal `@hook` lines (`@post-tool-use Bash 'go build'` says nothing about *why* or *what next*).
3. **Capture fidelity.** They read the harness's complete transcript after the fact; memoria records its own lossy shadow of the session (whitespace-collapsed one-liners, only Write/Edit/Bash, assistant text only at `stop`).

## Trade-off and the key insight

Their speed costs a daemon, SQLite, and parsers for 8 undocumented private store formats — fragile when harnesses update (they admit rename-project can't fix native stores). memoria stays daemon-free: plain markdown in the repo, human-readable, git-versioned ([[decisions/0001-plain-markdown-no-db]]). Their wiki consolidation is also LLM-driven and optional — the transferable insight is that ai-memory separates **handoff (deterministic, instant)** from **knowledge distillation (LLM, slow, async)**, while memoria currently routes both through the same slow LLM pipeline.

## Candidate ideas (recorded, unimplemented)

- Build the handoff packet by reading `~/.claude/projects/**/*.jsonl` / codex rollouts instead of memoria's digest.
- Inject context via a SessionStart hook instead of a "read this file" prompt.
- Add a workstream-style link id to `.memoria/sessions.md`.