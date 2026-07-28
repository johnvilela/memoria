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
- TUI/styling: charmbracelet/lipgloss (bubbles will be added when an interactive TUI feature lands)
- Module: `github.com/jv77/memoria`

## Conventions

- **TDD**: write tests first for every feature, confirm red, then implement to green.
- Docs, help text, and code in English.
- Help screen lists only real commands; planned commands are tagged "coming soon".

## Scripts

- `scripts/build.sh` — host binary at `./memoria`; `scripts/build.sh all` → `dist/` for linux/darwin × amd64/arm64
- `scripts/install.sh` — installs to `$BIN_DIR` (default `~/.local/bin`); in-repo builds local checkout, standalone go-installs `@latest` (curl-able from github later)
- `scripts/test.sh` — `go vet` + `go test -race`, extra args pass through (`scripts/test.sh -v -run TestX`)

## Commands

| Command | Status | Description |
|---------|--------|-------------|
| `help` / `--help` / `-h` | done | ASCII art + command list |
| `init <claude-code\|codex>` | done | Install memoria hooks globally for the chosen agent |
| `bootstrap` | done | Register the current folder (name + path) as a tracked project in config.yaml; gitignores `.memoria/` and creates the wiki folder |
| `hook <name>` | done (internal) | Called by agent hooks; appends events to the session digest |

## How hooks flow

1. `memoria init claude-code` merges command hooks into `~/.claude/settings.json` (Codex: `~/.codex/hooks.json`, same JSON shape; Codex lacks the Notification event and needs one-time `/hooks` trust in its TUI). Idempotent; preserves existing settings/hooks; uses the absolute binary path.
2. Each agent event runs `memoria hook <canonical-name>` with a JSON payload on stdin (`session_id`, `cwd`, `hook_event_name`, ...). Canonical names: `session-start user-prompt pre-tool-use post-tool-use pre-compact post-compact notification stop session-end subagent-start subagent-stop`; unknown names are ignored.
3. Hooks are global, but capture only happens for projects opted-in via `~/.config/memoria/config.yaml`. Run `memoria bootstrap` inside a project to register it (idempotent):
   ```yaml
   projects:
     - name: some-project
       path: /home/me/dev/some-project
   ```
   `cwd` is matched by longest path prefix; untracked projects are silently ignored. Bootstrap also appends `.memoria/` to the project's `.gitignore` (captures stay untracked) and creates `wiki/` with a `.gitkeep` (the curated wiki is meant to be versioned). An existing wiki folder is an error — pick another name with `--wiki <name>`, saved as `wiki:` on the project entry (empty = `wiki`).
4. Captured events append chronologically to the session digest at `<project>/.memoria/sessions/pending/<session_id>.md` as `@hook` annotated lines (see "How digests flow"). Events not worth digesting write nothing: `pre-tool-use`, `notification`, `subagent-start`, unknown hooks, and tools other than Write/Edit/NotebookEdit/Bash.
5. `<project>/.memoria/sessions.md` indexes sessions as `DATETIME - SESSION_ID - NAME` — the name is the session's first user prompt (whitespace-collapsed, truncated to 80 runes). One entry per session id.
6. `memoria hook` must NEVER block an agent: always exits 0, never writes stdout (some agents inject hook stdout as model context).

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
