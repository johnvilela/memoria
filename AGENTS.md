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
| `bootstrap` | done | Register the current folder (name + path) as a tracked project in config.yaml |
| `hook <name>` | done (internal) | Called by agent hooks; captures session data |

## How hooks flow

1. `memoria init claude-code` merges command hooks into `~/.claude/settings.json` (Codex: `~/.codex/hooks.json`, same JSON shape; Codex lacks the Notification event and needs one-time `/hooks` trust in its TUI). Idempotent; preserves existing settings/hooks; uses the absolute binary path.
2. Each agent event runs `memoria hook <canonical-name>` with a JSON payload on stdin (`session_id`, `cwd`, `hook_event_name`, ...). Canonical names: `session-start user-prompt pre-tool-use post-tool-use pre-compact post-compact notification stop session-end subagent-start subagent-stop`; unknown names log as `other`.
3. Hooks are global, but capture only happens for projects opted-in via `~/.config/memoria/config.yaml`. Run `memoria bootstrap` inside a project to register it (idempotent):
   ```yaml
   projects:
     - name: some-project
       path: /home/me/dev/some-project
   ```
   `cwd` is matched by longest path prefix; untracked projects are silently ignored.
4. Captured lines append chronologically to `<project>/.memoria/sessions/<session_id>.md` as `DATETIME - HOOK_NAME - DATA` (RFC3339 local time, full compact JSON payload).
5. `memoria hook` must NEVER block an agent: always exits 0, never writes stdout (some agents inject hook stdout as model context).
