# memoria

Long-term memory and a per-project wiki for code agents, built from their chat sessions.

memoria captures knowledge (decisions, rules, strange bugs) from agent conversations via hooks, digests it into markdown files inside your project, and makes it readable later by humans or by future agent sessions. No database, no embeddings — plain `.md` files, versioned with the project.

Inspired by Fabio Akita's ai-memory and Andrej Karpathy's LLM wiki idea.

## Install

```sh
go install github.com/jv77/memoria/cmd/memoria@latest
```

Or from a local checkout:

```sh
scripts/build.sh        # builds ./memoria
scripts/install.sh      # installs to ~/.local/bin
```

## Quick start

```sh
# 1. Install hooks + choose the session processor (once per machine)
memoria init               # interactive (bubbles select)
# or scriptable: memoria init --client claude-code --processor claude-code

# 2. Opt a project in (run inside the project)
cd ~/dev/my-project
memoria bootstrap

# 3. Work with your agent as usual — sessions are digested automatically
#    into .memoria/sessions/pending/<session_id>.md

# 4. Consolidate finished sessions into the project wiki
memoria process            # runs in the background; LLM proposes pages -> .memoria/proposal.json
memoria status             # running / done / error per project
memoria process --apply    # after review: writes wiki/, archives sessions
```

## Commands

| Command | Description |
|---------|-------------|
| `help` | Show usage |
| `init [<client>] [--client ...] [--processor ...] [--notification]` | Install memoria hooks for an agent (claude-code \| codex), choose the AI provider that processes sessions (claude-code \| codex \| ollama \| gemini) and opt into desktop notifications; interactive when flags are omitted |
| `bootstrap` | Register the current folder as a tracked project; gitignores `.memoria/` and creates the wiki folder |
| `process [--apply] [--foreground]` | Consolidate ended pending sessions into the wiki: detaches by default (the LLM call can take minutes), writes a JSON proposal for review, `--apply` creates the files |
| `status` | Show background processing state per project (running / done / error) |
| `hook <name>` | Internal — called by agent hooks to capture session data |

## How it works

- **Hooks** — `memoria init` wires agent hooks (Claude Code, Codex) that run `memoria hook <event>` on each session event. Hooks are global, but only projects registered with `memoria bootstrap` are captured; everything else is ignored. Hooks never block the agent.
- **Live digests** — each event appends an `@hook` annotated line to `<project>/.memoria/sessions/pending/<session_id>.md` (YAML frontmatter + chronological stream: full prompts, file writes/edits, Bash commands with errors, assistant stop messages). Sessions are indexed in `.memoria/sessions.md` by their first user prompt. Reopening an already-processed session starts a numbered incarnation (`<sid>-2.md`) linked to the archived one via `continues_from`.
- **Pending queue** — new digests are registered in `~/.config/memoria/pending.yaml`, grouped by project. A session counts as ended when `session-end` fires or when a new session starts in the same project (so crashed sessions don't linger); `memoria process` consumes this worklist.
- **Wiki** — `memoria process` detaches and returns immediately (the processor call can take minutes and must never block an active agent session); `memoria status` tracks the run via `~/.config/memoria/status.yaml`, and with `notifications: true` the finished run pings the desktop via notify-send (opt-in through `memoria init --notification`, Linux). The processor gets the ended sessions + current wiki and returns a JSON proposal (`index.md` plus pages under `concepts/`, `decisions/`, `gotchas/`, `rules/`, connected by `[[wikilinks]]`). The LLM never writes files: you review `.memoria/proposal.json`, then `--apply` writes the pages and archives the sessions to `.memoria/sessions/processed/`. The consolidation prompt lives at `~/.config/memoria/wiki-prompt.md` (editable; FAITHFULNESS is its first rule).
- **Markdown in the repo** — session digests stay untracked (`.memoria/` is gitignored); the curated `wiki/` folder is meant to be committed.

Config lives at `~/.config/memoria/config.yaml` (`projects`, `processor`, `notifications` and `gemini_api_key` when the gemini processor is chosen — the file is written 0600). The gemini key can also come from the `GEMINI_API_KEY` env var.

## Development

Go, stdlib CLI dispatch, entrypoint in `cmd/memoria/`.

```sh
scripts/test.sh          # go vet + go test -race
scripts/build.sh all     # cross-compile to dist/
```

See [AGENTS.md](AGENTS.md) for full internals and conventions.
