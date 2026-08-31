---
tags: [research, evaluation, architecture, reliability, privacy]
---

# Memoria evaluation

## Overall assessment

Memoria has a strong product idea and a surprisingly mature implementation for v0.7. Its biggest advantage is that memory remains visible, reviewable, and version-controlled rather than disappearing into an opaque vector database.

I would consider it good for careful, single-developer use with manual review enabled. I would not yet trust unattended `auto_apply`/`cron_apply` on sensitive or heavily concurrent projects. The main remaining problems concern concurrency, stale overwrites, privacy, and long-term scalability.

## What is good

- **Excellent storage model.** Plain Markdown provides portability, Git history, human editing, easy backups, and no database or embedding lock-in.
- **Good separation of raw and curated memory.** Raw session observations remain under `.memoria/`, while only consolidated knowledge enters the wiki.
- **The LLM has a constrained role.** It proposes structured JSON; Go validates paths and performs the writes. Auto-apply and auto-commit are off by default.
- **Strong agent integration.** Hooks capture work automatically, `AGENTS.md` tells future agents how to recall it, and the MCP server provides search, recall, consolidation, linting, writing, and soft deletion.
- **Thoughtful continuation behavior.** Same-harness sessions use native resume; cross-harness continuation gets a bounded packet with source provenance, Git state, recent events, and an explicit instruction not to repeat completed work. See the [handoff implementation](../../cmd/run.go#L198).
- **Good failure containment.** Hooks never block the coding agent, processor calls have a timeout and recursion guard, and queue/status writes use locking and atomic replacement.
- **Strong test culture.** I found 270 tests, with more test code than production code. `go vet ./...` and `go test -race ./...` pass; statement coverage is 75.3%.

## Important flaws

| Priority | Finding | Why it matters |
|---|---|---|
| High | **Projects are globally keyed by basename.** Bootstrap names every project with `filepath.Base(cwd)`, while queues, status, and logs use that name as their key. Two repositories such as `client-a/api` and `client-b/api` can share sessions and status accidentally. | This could contaminate one project's wiki with another project's observations. See [bootstrap.go](../../cmd/bootstrap.go#L76) and [queue.go](../../cmd/queue.go#L10). |
| High | **Proposal application can overwrite newer human edits.** The proposal stores digest sizes but no hash of the wiki pages it was generated from. If a user edits a page during review, `process --apply` overwrites it without detecting the conflict. Writes are sequential rather than transactional, and a failed digest `Rename` is ignored before removing its queue entry. | Reviewed or manually maintained knowledge can be lost. See [proposal generation and application](../../cmd/process.go#L388). |
| High | **Concurrent sessions are treated as abandoned sessions.** Starting session B marks every other pending session in the project as ended—even if session A is still active. Digest-growth detection prevents some archival loss, but the system can repeatedly consolidate incomplete sessions and temporarily publish stale summaries. | Parallel agents or terminals can cause premature and repeated consolidation. See [queueEndOthers](../../cmd/queue.go#L59). |
| High | **Background job claiming is not atomic.** The code checks status, spawns a process, and only then records the PID. Two simultaneous callers can both pass the check and write the same proposal/log/status files. Similar logic exists for process, lint, digest, seed, auto-apply, and MCP jobs. | Concurrent jobs can overwrite artifacts and make status unreliable. |
| High | **Privacy controls are too weak for automatic capture.** Full user prompts, Bash commands, errors, and final assistant messages are stored. Bash commands often contain tokens or credentials. Raw digests and proposals are written `0644`, and consolidation sends the entire material to the configured AI provider. There is no redaction, exclusion pattern, or secret scanner. | Sensitive data can be retained or sent to an external processor unintentionally. See [hook capture](../../cmd/hook.go#L29). |
| Medium | **Prompt size grows with the entire wiki.** Every consolidation sends every non-trash Markdown page plus all selected digests. This repository already has 60 active pages totaling about 171 KB before adding pending sessions. | Cost and context pressure grow linearly forever. See [buildPrompt](../../cmd/process.go#L664). |
| Medium | **Capture fidelity is intentionally lossy.** Read/Grep activity and successful tool results are excluded; Write/Edit generally retain only a path. | This keeps logs compact but removes much of the evidence needed to understand why a decision was made. Cross-harness handoff is good framing around incomplete evidence. |
| Medium | **LLM validation is mostly structural.** Paths and empty bodies are checked, but tag count/format, duplicate target paths, page-count limits, and frontmatter-safe values are not enforced. | A malformed or adversarial tag could corrupt the YAML frontmatter. See [validatePages](../../cmd/process.go#L549). |
| Medium | **Lint can produce false confidence.** It sees only the first 400 runes of each page, yet a clean result is described as the whole wiki being internally consistent. | Contradictions deeper in a page are invisible. See [lint prompt construction](../../cmd/lint.go#L405). |
| Medium | **Scheduled processors may not be on PATH.** The systemd and launchd artifacts use an absolute path for Memoria, but Memoria later resolves `claude` or `codex` from the scheduler's restricted environment. Shell PATH repair does not normally affect systemd or launchd. | Scheduled processing can fail even when interactive processing works. See [cron installation](../../cmd/cron.go#L345). |
| Medium | **Failed MCP calls automatically retry paid work.** Polling after an error immediately launches another processor job, with no retry limit or explicit confirmation. | Repeated polling can create unexpected processor cost. See [mcpJob](../../cmd/mcp.go#L98). |

There is also an instructive dogfooding result: the current wiki says agents have “six MCP tools” in the [architecture page](../concepts/architecture-overview.md#L13), while the current implementation has seven. Another page points to the obsolete `cmd/memoria/lint-prompt.md` location in [consolidation-pipeline.md](../concepts/consolidation-pipeline.md#L13). That demonstrates the real difficulty: consolidation works, but provenance and stale-knowledge detection are not yet strong enough.

## Recommended improvement order

1. **Introduce a stable project ID.** Use a UUID or hash of the canonical path for queue/status/log keys. Keep the basename only as a display name, and add a migration for existing YAML state.
2. **Make jobs and application conflict-safe.** Add an atomic job lease/CAS, proposal IDs, source-page hashes, expected-content hashes for MCP writes, atomic temporary writes, and error checking for every archive move.
3. **Fix multi-session semantics.** Do not end all other sessions on every start. Use explicit session-end, heartbeat/TTL, or per-session leases.
4. **Add privacy controls.** Default raw files to `0600`; support ignored commands/files, secret redaction, maximum event sizes, local-only processor mode, and a pre-apply secret scan.
5. **Add provenance to every generated page.** Frontmatter could record source session IDs, source commit, generation time, processor/model, and previous-page hash. This would make stale detection and auditing much stronger.
6. **Scale without abandoning Markdown.** Select relevant pages using tags, wikilinks, lexical search, changed-page history, and page summaries. Embeddings are not required. Keep episodic session pages out of every routine consolidation prompt.
7. **Strengthen lint and validation.** Validate tag syntax/count, duplicate paths and output count; check broken wikilinks; retrieve full relevant sections for lint; and avoid claiming global consistency from 400-character previews.
8. **Add production evaluations.** Create a fixed corpus of real sessions and measure factual retention, hallucination rate, stale-page preservation, secret leakage, cross-harness time-to-competence, token cost, and behavior after hundreds or thousands of pages.

## Practical recommendation

Use Memoria with manual proposal review and manual wiki commits. Avoid auto-apply for concurrent or sensitive work until project identity, conflict detection, and redaction are addressed.

This evaluation was read-only. At the time of evaluation, `go vet ./...` and `go test -race ./...` passed, statement coverage was 75.3%, and the repository worktree was clean.
