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
| `init [<client>] [--client ...] [--processor ...] [--notification]` | done | Install memoria hooks for an agent (claude-code \| codex) and choose the session processor (claude-code \| codex \| ollama \| gemini). Flags are scriptable; omitted ones prompt via bubbles TUI (TTY only). Processor is saved to config and verified warn-only (CLI on PATH; gemini key against the models endpoint). Gemini key: `GEMINI_API_KEY` env, existing config, or masked prompt — saved as `gemini_api_key`. Ollama is a placeholder (auto-install coming soon). After hooks install, appends `.memoria/` to cwd's `.gitignore` (created if missing) when cwd is a git repo — best-effort, dedupes, never fails init. `--notification` (bool, default disabled; `--notification=false` disables) saves `notifications:` — desktop ping when background processing finishes; omitted flag outside a TTY leaves the config untouched, notify-send checked warn-only. `--cron [<schedule>]` (bare flag = "8 times a day") + `--cron-apply` install the systemd user timer (see "How the cron flows"); `--cron off` removes it; omitted outside a TTY leaves scheduling untouched |
| `setup [--processor ...] [--notification] [--cron <expr\|preset\|off>] [--cron-apply]` | done | Reconfigure an existing install without touching hooks: same processor/notification/cron handling as init, but requires an existing config (else "run memoria init first"). Any flag given = change exactly that, keep the rest — the interactive walk (TTY only, each select led by "Keep current (...)") runs only when no flags are passed. `--cron-apply` alone re-installs the timer with the stored schedule in apply mode (error if none stored) |
| `bootstrap [--wiki <name>] [--background]` | done | Register the current folder (name + path) as a tracked project in config.yaml; gitignores `.memoria/` and creates the wiki folder. Then (processor configured + repo has commits, else silently skipped) offers to seed the wiki from git history: TTY yes/no prompt → foreground spinner warning it may take minutes, or `--background` (implies yes, works non-TTY) → detached child `bootstrap --seed-foreground` sharing the per-project status.yaml entry with process/lint, notify-send on finish/fail. Re-running on a registered project whose wiki has no `.md` pages offers seeding again; a wiki with pages just prints "already registered". Seed content is read from HEAD (`git log --oneline -n 300`, `git ls-tree -r --name-only HEAD`, `git show HEAD:README.md`) so a dirty working tree never leaks into the prompt. Prompt = embedded default, overridable by creating `~/.config/memoria/seed-prompt.md` (carries its own JSON contract: `{"pages":[{path,title,body_markdown,tags}],"rationale"}`). Pages validated like `process --apply` (paths under the wiki categories only); `tags` become YAML frontmatter; `rationale` is printed (foreground) or stored in the status detail (background) |
| `process [--apply] [--foreground] [--all]` | done | Consolidate ended pending sessions into the project wiki via the configured processor. Detaches by default (LLM calls take minutes and must not block an active agent); `--foreground` runs inline. Two-step: writes `.memoria/proposal.json` for review, `--apply` creates the files. `--all` (the timer's entrypoint) sweeps every tracked project sequentially from any cwd, skipping projects with nothing ended or already running; `--all --apply` also applies each successful proposal immediately |
| `lint [--review] [--apply] [--deny "why"] [--foreground]` | done | Audit the wiki for contradictions/stale/duplicate pages via the configured processor. Detaches by default (same status.yaml entry as process — one background job per project). Prompt = embedded default (overridable by creating `~/.config/memoria/lint-prompt.md`) + previews (path + first 400 runes) of every wiki page in one call + past denials + Go-appended JSON contract. Findings (kind: contradiction\|stale\|duplicate, severity: warning\|info, message, pages) are validated (known enums, cited pages must exist) and overwrite the single report `.memoria/lint.jsonl`, one finding per line; a clean run deletes any stale report. `--review` prints the report. `--apply` runs a second processor pass with the findings + full page contents, validates the returned pages (wiki paths only; `delete` allowed but confined to pages cited by findings), writes/deletes in the wiki, and consumes the report. `--deny "reason"` rejects the report: findings + reason append to `.memoria/lint-denied.jsonl`, which future lint prompts include as do-not-re-report context |
| `run <agent-binary> [--new \| --session <id\|name> \| --last-session]` | done | Launch any agent binary on PATH inside the current tracked project (cwd = project root, capture ON so the new session enters the loop). Continuation: same harness as the recorded session (digest frontmatter `client:` vs binary name; claude→`--resume <sid>`, codex→`resume <sid>`) → native resume; different/unknown harness or pre-`--client` session → digest handoff (one-sentence initial prompt pointing at the digest's absolute path — content never inlined). No flags + TTY → yes/no offer of the last session (No first; decline/esc/non-TTY/no-digest → fresh). `--session` matches sid prefix or name substring (case-insensitive) against sessions.md, multi-hit → selector. Agent exit code propagates |
| `search <text \| #tag>` | done | Find pages in the current project's wiki: plain text = case-insensitive content substring, `#tag` = whole-tag match against YAML frontmatter `tags:`. Multiple hits → bubbles selector, single hit prints straight away; the chosen page is printed raw to stdout. Human-only: TTY required (agent recall arrives later via MCP), no matches or esc = exit 1 |
| `status` | done | Show background processing state per project from `~/.config/memoria/status.yaml` (running with pid liveness check / done / error) |
| `hook <name>` | done (internal) | Called by agent hooks; appends events to the session digest |

## How hooks flow

1. `memoria init claude-code` merges command hooks into `~/.claude/settings.json` (Codex: `~/.codex/hooks.json`, same JSON shape; Codex lacks the Notification event and needs one-time `/hooks` trust in its TUI). Idempotent; preserves existing settings/hooks; uses the absolute binary path.
2. Each agent event runs `memoria hook <canonical-name> --client <claude-code|codex>` with a JSON payload on stdin — the client flag is baked in at install time (payloads don't name the agent) and lands as `client:` in digest frontmatter, which `memoria run` uses to pick native resume vs handoff. Re-running init re-points existing hook commands in place (covers moved binaries and pre-`--client` installs) (`session_id`, `cwd`, `hook_event_name`, ...). Canonical names: `session-start user-prompt pre-tool-use post-tool-use pre-compact post-compact notification stop session-end subagent-start subagent-stop`; unknown names are ignored.
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
2. Prompt = go:embed default (FAITHFULNESS first, category definitions, wikilinks, no negative ontologies), overridable by creating `~/.config/memoria/wiki-prompt.md` + full current wiki content + session digests + a Go-appended JSON output contract (kept out of the editable file so edits can't break parsing).
3. The configured processor runs it (`processor.go`): `claude-code` → `claude -p`, `codex` → `codex exec` (both with cwd = temp dir + `MEMORIA_NO_CAPTURE=1` as recursion guards, 10-min timeout), `gemini` → `generateContent` REST call, `ollama` → "coming soon" error.
4. The LLM returns `{"pages":[{"action","path","title","content"}]}` — it NEVER writes files. Go validates every page (path is `index.md` or under `concepts/ decisions/ gotchas/ rules/`, `.md` only, no traversal, nothing empty; any violation rejects the whole proposal) and writes `.memoria/proposal.json` with project/sessions metadata.
5. After human review, `memoria process --apply` re-validates, writes the pages under the wiki folder, moves the digests to `.memoria/sessions/processed/`, removes their queue entries, and deletes the proposal.

## How the cron flows

1. `memoria init --cron <schedule>` (or `memoria setup --cron ...`) writes two units to `~/.config/systemd/user/`: `memoria-process.service` (`Type=oneshot`, `ExecStart="<abs memoria>" process --all`, plus ` --apply` when `--cron-apply`) and `memoria-process.timer` (`OnCalendar=...`, `Persistent=true`), then `systemctl --user daemon-reload` + `enable --now` — systemctl failures are warn-only, the units + config are the state.
2. Schedules accept systemd presets (`hourly`/`daily`/`weekly`), phrases (`every N hours`, `N times a day` — N must divide 24), or 5-field cron translated to OnCalendar (`0 */3 * * *` → `*-*-* 0/3:00:00`; dow numbers → `Sun..Sat`; ranges and names unsupported). The verbatim input is saved as `cron:` (+ `cron_apply:`) in config.yaml; `--cron off` disables the timer, removes the units and clears both fields.
3. Each firing runs `process --all`: a sequential sweep (the status/queue yaml files have no locking) that skips projects with no ended sessions or an already-running job, and continues past per-project failures (exit 1 at the end if any failed). Without `cron_apply` the sweep leaves proposals for review — with notifications on, notify-send announces each.

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
