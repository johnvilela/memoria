---
tags: [decay, sessions, wiki, config]
---

# Session-page decay: deterministic `lastUsed` stamps

Shipped on branch `feat/session-decay`, commit `82490e0`, PR #10 (open against main, deliberately left unapproved — "commit and open the PR, do not approve it"). Version bumped to **0.12.0**; merging the PR will auto-tag and release it via the existing pipeline ([[concepts/ci-release-pipeline]]). Design rationale and rejected alternatives (SQLite, FTS5, link-neighbor ranking): [[decisions/0013-deterministic-decay-over-salience-model]]. Session: [[sessions/acbf5d0d-d82b-4852-98fc-97cfbfb5da35]].

## Motivation

Two stated goals: stop `sessions/` pages accumulating forever in the wiki, and make memoria more token-efficient for an AI agent to search and retrieve from. Modeled on ai-memory's decay/salience concept but reduced to a deterministic mechanism with no daemon, no counters, no exponential-decay math.

## The field

`lastUsed: 2026-08-31` — a plain, date-only (`2006-01-02`) frontmatter line, written only on `sessions/` pages. It is managed exclusively by deterministic Go code; an LLM is never allowed to author, copy, or edit it — the JSON contract, the digest prompt, and the lint-fix prompt were all updated to say so, and any LLM-smuggled `lastUsed` line gets overwritten on write.

## New file: `cmd/decay.go`

- `var now = time.Now` — the **first clock seam in the codebase**, matching the existing `var commitWiki`/`var isTTY` stub idiom (stubbed in tests as `stubNow`).
- `today()` — `now().Format("2006-01-02")`.
- `upsertFrontLine(content, key, line)` — insert-or-replace a frontmatter key, synthesizing the `---` block when the page has none. Extracted from the existing `addDeletedTag` pattern (mcp.go); `addDeletedTag` itself was refactored down to call this, net code deletion.
- `pageLastUsed(content)` — reads the stored date via the existing `splitFrontmatter`/`frontKey` parsers (run.go), returns `""` when absent.
- `touchLastUsed(wikiRoot, relPath)` — stamps today on a delivered `sessions/` page. No-op for non-sessions paths (including anything under `trash/...`), missing files, or a page already stamped today (bounds writes to at most one per page per day, which also bounds git-diff noise). Never auto-commits — touches stay outside `wiki_auto_commit`, consistent with [[decisions/0010-wiki-auto-commit-is-opt-in]] ("the user owns the commit").
- `stampSessions(wikiRoot, relPath, content)` — returns content carrying the deterministic `lastUsed` line: the existing page's date when it has one, else today. Non-sessions paths pass through untouched.
- `writeWikiPage(wikiRoot, relPath, tags, body)` — `MkdirAll` + `WriteFile(stampSessions(renderPage(...)))`, the single new chokepoint that replaced repeated hand-rolled MkdirAll/WriteFile pairs at every writer.
- `decaySweep(cfg, wikiRoot, out)` — the sweep itself (below).

## Preservation across every write site

Rule: before overwriting a page, read the old file's `lastUsed`; keep it, or stamp today if absent.

- `applyProposal`, `digestForeground`, `mcpWritePage`, `seedWiki` — all four now route through `writeWikiPage`, a one-line swap at each site replacing the old MkdirAll+WriteFile(renderPage(...)) pattern.
- `lintApply` — the one writer that bypasses `renderPage` entirely (it writes a full LLM-authored file verbatim). Gets a separate `stampSessions(wikiRoot, pg.Path, pg.Content)` call before `WriteFile`, so a lint fix can't forge or drop the date.

## Touch sites — content delivery only, never a bare listing

One `touchLastUsed(...)` call each:

- **CLI `search`** — only when a single page's content is actually printed. The non-TTY multi-hit path (prints a sorted path list, no content — see [[concepts/global-capture-mode]]'s headless search) does not touch anything, and neither does a `--trash` read.
- **MCP `memoria_search`** — only the `≤3 hits` branch where pages are inlined ([[concepts/mcp-server]]); a `>3`-hit path-only listing touches nothing.
- **`memoria_recall`** — touches the session's page on every call; a missing page is a no-op, recall still succeeds.
- **`memoria run` resume** — touches on *both* branches: same-harness native resume (`claude --resume`/`codex resume`) and the cross-harness handoff packet ([[concepts/recall-and-run]]). This closes a real gap: native resume previously never read the wiki page at all, so it had no existing touch point.
- **`memoria_digest`'s done-poll** — the page's full content is returned to the agent when the background compile job finishes, which counts as delivery even if the stored date is old.

## The sweep

Wired into `processAll` — the same function `process --all` and the cron timer's `process --all` firing both call — so it runs for **every** registered project (`_global` included) on each pass, even a project with nothing pending to consolidate, but is still skipped for a project whose background job slot is currently `running` (the existing status check).

- **Soft pass**: `sessions/*.md` pages unused for `decay_soft_days` (default 15) move to `trash/` via the existing `trashPage` helper (collision-suffixed, tagged `deleted`).
- **Hard pass**: `trash/sessions/*.md` pages unused for `decay_hard_days` (default 30) are permanently removed with `os.Remove`.
- **Adoption, not deletion**: a page with no `lastUsed` at all (every pre-existing wiki, on first sweep) is stamped today rather than trashed — the clock starts from the first sweep, so upgrading never mass-deletes a pre-existing wiki.
- One `commitWiki(...)` call at the end when anything changed, gated on `wiki_auto_commit` as always.

**Bug caught by smoke-testing, fixed before commit**: a page past both thresholds was trashed *and* purged in the very same sweep run, and a mid-day clock read aged pages roughly half a day early. Fix: compare day-to-day (not raw duration) and run the **hard pass before the soft pass**, so a page the current run just trashed always survives at least one full sweep interval — a built-in recovery window.

## Config

```yaml
decay_soft_days: 15 # unused sessions/ pages -> trash/
decay_hard_days: 30 # trashed sessions/ pages -> permanently removed
```

Both optional, hand-editable in `config.yaml` (no init/setup TUI wiring, matching how `processor_model`/`processor_effort` already work).

## Never-block and concurrency reasoning

- **Never blocks an agent** ([[decisions/0003-never-block-the-agent]]): the sweep only runs inside `processAll`, never on any MCP tool call path — the detached `--foreground` children (digest/process/lint) don't sweep, so no agent-facing poll pays for it. It's file stats in milliseconds, not the multi-minute LLM calls that ADR exists for.
- **No new file locking added** for wiki page writes. Reasoning: `withFlock` ([[decisions/0007-queue-safety-via-file-locking]]) exists for the shared queue/status YAML files; wiki page writes have always been lock-free/last-writer-wins (applyProposal, digestForeground, mcpWritePage already race each other today). The sweep's two new race pairs are judged benign: a background writer racing the sweep is closed by the existing running-job-slot skip; a live agent's synchronous write racing the sweep is near-impossible because any page in active use was touched today and so isn't sweep-eligible (15-day margin) — worst case is a benign self-healing rebirth (old copy still in `trash/`).

## Docs and prompt updates in the same change

JSON contract (process.go), digest prompt, lint-fix prompt — all forbid authoring `lastUsed`. MCP tool descriptions (`memoria_search`, `memoria_recall`, `memoria_write_page`, `memoria_delete_page`) explain the decay behavior to agents. The AGENTS.md recall block template gained decay wording — existing projects pick it up on their next `memoria bootstrap` re-run (the block is repaired in place). README updated.

## Verification

381 tests green under `go vet` + `go test -race`. Real-binary end-to-end smoke test: sweep correctly trashes/purges/adopts and is idempotent within a day; piped single-hit search stamps the delivered page; a multi-hit listing stamps nothing.