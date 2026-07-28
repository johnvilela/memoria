# memoria

## What

Go CLI that gives code agents long-term memory and a per-project wiki, built from their chat sessions. Knowledge (decisions, rules, strange bugs) is captured from agent conversations, consolidated into markdown files inside the project folder, and read later by humans or by future agent sessions.

Deeply inspired by Fabio Akita's ai-memory and Andrej Karpathy's LLM wiki idea.

## How it works

- **Hooks**: code-agent hooks (e.g. Claude Code hooks) trigger knowledge capture at the right moments.
- **Cronjobs**: periodic consolidation of captured knowledge into curated markdown knowledge files.
- **Markdown in the repo**: no database, no embeddings — plain `.md` files living in the project folder. Per-project memory in the MVP.

Differentiator vs. existing solutions: hooks + cronjobs + markdown files, human-readable and agent-readable, versioned with the project.

## Stack

- Go, entrypoint in `cmd/memoria/` (no Go files in repo root); build with `go build -o memoria ./cmd/memoria`
- CLI dispatch: stdlib `os.Args` switch — no cobra/urfave
- TUI/styling: charmbracelet/lipgloss + bubbletea/bubbles (interactive selects and inputs live in `tui.go`)
- Module: `github.com/jv77/memoria`

## Conventions

- **TDD**: write tests first for every feature, confirm red, then implement to green.
- Docs, help text, and code in English.
- Help screen lists only real commands; planned commands are tagged "coming soon".

## Scripts

- `scripts/build.sh` — host binary at `./memoria`; `scripts/build.sh all` → `dist/` for linux/darwin × amd64/arm64
- `scripts/install.sh` — installs to `$BIN_DIR` (default `~/.local/bin`); in-repo builds local checkout, standalone go-installs `@latest` (curl-able from github later)
- `scripts/test.sh` — `go vet` + `go test -race`, extra args pass through (`scripts/test.sh -v -run TestX`)
- `scripts/dev.sh` — dev loop: build the local checkout and install to `$BIN_DIR` (wraps `install.sh`)

## Commands

| Command | Status | Description |
|---------|--------|-------------|
| `help` / `--help` / `-h` | done | ASCII art + command list |
| `init [<client>] [--client ...] [--processor ...] [--notification]` | done | Install memoria hooks for an agent (claude-code \| codex) and choose the session processor (claude-code \| codex \| ollama \| gemini). Flags are scriptable; omitted ones prompt via bubbles TUI (TTY only). Processor is saved to config and verified warn-only (CLI on PATH; gemini key against the models endpoint). Gemini key: `GEMINI_API_KEY` env, existing config, or masked prompt — saved as `gemini_api_key`. Ollama is a placeholder (auto-install coming soon). `--notification` (bool, default disabled; `--notification=false` disables) saves `notifications:` — desktop ping when background processing finishes; omitted flag outside a TTY leaves the config untouched, notify-send checked warn-only. Last step (TTY only, processor configured, cwd has git commits — otherwise silently skipped): offers to auto-generate the wiki from the git log, file tree and README via the processor, warning it may take a while; runs in the foreground with a spinner, same page validation as `process --apply`, failures never fail init |
| `bootstrap` | done | Register the current folder (name + path) as a tracked project in config.yaml; gitignores `.memoria/` and creates the wiki folder |
| `process [--apply] [--foreground]` | done | Consolidate ended pending sessions into the project wiki via the configured processor. Detaches by default (LLM calls take minutes and must not block an active agent); `--foreground` runs inline. Two-step: writes `.memoria/proposal.json` for review, `--apply` creates the files |
| `lint [--review] [--apply] [--deny "why"] [--foreground]` | done | Audit the wiki for contradictions/stale/duplicate pages via the configured processor. Detaches by default (same status.yaml entry as process — one background job per project). Prompt = `~/.config/memoria/lint-prompt.md` (user-editable, materialized from embed) + previews (path + first 400 runes) of every wiki page in one call + past denials + Go-appended JSON contract. Findings (kind: contradiction\|stale\|duplicate, severity: warning\|info, message, pages) are validated (known enums, cited pages must exist) and overwrite the single report `.memoria/lint.jsonl`, one finding per line; a clean run deletes any stale report. `--review` prints the report. `--apply` runs a second processor pass with the findings + full page contents, validates the returned pages (wiki paths only; `delete` allowed but confined to pages cited by findings), writes/deletes in the wiki, and consumes the report. `--deny "reason"` rejects the report: findings + reason append to `.memoria/lint-denied.jsonl`, which future lint prompts include as do-not-re-report context |
| `status` | done | Show background processing state per project from `~/.config/memoria/status.yaml` (running with pid liveness check / done / error) |
| `hook <name>` | done (internal) | Called by agent hooks; appends events to the session digest |

## How hooks flow

1. `memoria init claude-code` merges command hooks into `~/.claude/settings.json` (Codex: `~/.codex/hooks.json`, same JSON shape; Codex lacks the Notification event and needs one-time `/hooks` trust in its TUI). Idempotent; preserves existing settings/hooks; uses the absolute binary path.
2. Each agent event runs `memoria hook <canonical-name>` with a JSON payload on stdin (`session_id`, `cwd`, `hook_event_name`, ...). Canonical names: `session-start user-prompt pre-tool-use post-tool-use pre-compact post-compact notification stop session-end subagent-start subagent-stop`; unknown names are ignored.
3. Hooks are global, but capture only happens for projects opted-in via `~/.config/memoria/config.yaml`. Run `memoria bootstrap` inside a project to register it (idempotent):
   ```yaml
   projects:
     - name: some-project
       path: /home/me/dev/some-project
   processor: claude-code       # set by memoria init; processes sessions into wiki/memories
   gemini_api_key: ...          # only when processor is gemini; config written 0600
   ```
   `cwd` is matched by longest path prefix; untracked projects are silently ignored. Bootstrap also appends `.memoria/` to the project's `.gitignore` (captures stay untracked) and creates `wiki/` with a `.gitkeep` (the curated wiki is meant to be versioned). An existing wiki folder is an error — pick another name with `--wiki <name>`, saved as `wiki:` on the project entry (empty = `wiki`).
4. Captured events append chronologically to the session digest at `<project>/.memoria/sessions/pending/<session_id>.md` as `@hook` annotated lines (see "How digests flow"). Events not worth digesting write nothing: `pre-tool-use`, `notification`, `subagent-start`, unknown hooks, and tools other than Write/Edit/NotebookEdit/Bash. Reopening a session whose digest was already processed starts a new incarnation (`resolveDigestPath` in `hook.go`): `<sid>-2.md`, `<sid>-3.md`, ... in pending/, frontmatter `continues_from: ../processed/<previous>.md`; the processed original is never touched.
5. `<project>/.memoria/sessions.md` indexes sessions as `DATETIME - SESSION_ID - NAME` — the name is the session's first user prompt (whitespace-collapsed, truncated to 80 runes). One entry per session id.
6. Creating a digest also registers its absolute path in `~/.config/memoria/pending.yaml` (`queue.go`) — the central worklist `memoria process` consumes, grouped by project name. An entry becomes `ended: true` when `session-end` fires OR when a new session starts in the same project (`queueEndOthers` — crashed/abandoned sessions don't stay pending forever). Append + dedupe only; `process --apply` removes entries:
   ```yaml
   memoria:
     - path: /home/me/dev/memoria/.memoria/sessions/pending/abc-123.md
       ended: true
   ```
7. `memoria hook` must NEVER block an agent: always exits 0, never writes stdout (some agents inject hook stdout as model context).

## How processing flows

1. `memoria process` (inside a tracked project) collects this project's `ended: true` queue entries whose digest file still exists. None → "Nothing to process". With work to do it detaches: re-execs itself as `process --foreground` (setsid, no stdio) and returns immediately, recording `state: running` + pid in `~/.config/memoria/status.yaml` (`status.go`). A second `process` while one is running for the project is refused (pid liveness check — a dead pid is respawned over). The child writes the final `done`/`error` state + detail; `memoria status` prints it, flagging a running entry whose pid died. With `notifications: true` the child also fires a notify-send desktop notification on success ("proposal ready") and failure (`notify.go`; Linux only, errors just logged).
2. Prompt = `~/.config/memoria/wiki-prompt.md` (user-editable; materialized from a go:embed default on first run — FAITHFULNESS first, category definitions, wikilinks, no negative ontologies) + full current wiki content + session digests + a Go-appended JSON output contract (kept out of the editable file so edits can't break parsing).
3. The configured processor runs it (`processor.go`): `claude-code` → `claude -p`, `codex` → `codex exec` (both with cwd = temp dir + `MEMORIA_NO_CAPTURE=1` as recursion guards, 10-min timeout), `gemini` → `generateContent` REST call, `ollama` → "coming soon" error.
4. The LLM returns `{"pages":[{"action","path","title","content"}]}` — it NEVER writes files. Go validates every page (path is `index.md` or under `concepts/ decisions/ gotchas/ rules/`, `.md` only, no traversal, nothing empty; any violation rejects the whole proposal) and writes `.memoria/proposal.json` with project/sessions metadata.
5. After human review, `memoria process --apply` re-validates, writes the pages under the wiki folder, moves the digests to `.memoria/sessions/processed/`, removes their queue entries, and deletes the proposal.

## How digests flow

1. Hooks write the digest directly — there is no separate digest command, raw log, or AI pass. The first captured event of a session creates `.memoria/sessions/pending/<sid>.md` with YAML frontmatter (`schema_version: 2`, `kind: session-digest`, `session_id`, `project`, `project_root`, `started_at`); every event then appends one `@hook` line (`renderEvent` in `hook.go`):
   ```
   @session-start source: startup
   @user-prompt 'Can you create something'
   @post-tool-use Write /path/file.go
   @post-tool-use Bash 'go build' error: 'exit status 1: undefined: foo'
   @stop 'Done. Created the file.'
   @session-end reason: exit
   ```
2. Lines are whitespace-collapsed but never truncated. Write → `Write /path`; Edit/NotebookEdit → `Edit /path`; Bash → full command; a `tool_response` with a non-empty `error` (or `is_error`) appends ` error: '...'`. Deleted files surface as Bash `rm` lines; recoveries are visible from event order.
3. `session-end` also inserts/updates `ended_at:` in the frontmatter (`setEndedAt`).
4. Recursion guard: `MEMORIA_NO_CAPTURE=1` makes `memoria hook` skip capture entirely (used by tooling/nested agent sessions).
