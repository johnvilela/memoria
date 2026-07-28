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
# 1. Install hooks globally for your agent (once per machine)
memoria init claude-code   # or: memoria init codex

# 2. Opt a project in (run inside the project)
cd ~/dev/my-project
memoria bootstrap

# 3. Work with your agent as usual — sessions are digested automatically
#    into .memoria/sessions/pending/<session_id>.md
```

## Commands

| Command | Description |
|---------|-------------|
| `help` | Show usage |
| `init <claude-code\|codex>` | Install memoria hooks globally for the chosen agent |
| `bootstrap` | Register the current folder as a tracked project; gitignores `.memoria/` and creates the wiki folder |
| `hook <name>` | Internal — called by agent hooks to capture session data |

## How it works

- **Hooks** — `memoria init` wires agent hooks (Claude Code, Codex) that run `memoria hook <event>` on each session event. Hooks are global, but only projects registered with `memoria bootstrap` are captured; everything else is ignored. Hooks never block the agent.
- **Live digests** — each event appends an `@hook` annotated line to `<project>/.memoria/sessions/pending/<session_id>.md` (YAML frontmatter + chronological stream: full prompts, file writes/edits, Bash commands with errors, assistant stop messages). Sessions are indexed in `.memoria/sessions.md` by their first user prompt.
- **Markdown in the repo** — session digests stay untracked (`.memoria/` is gitignored); the curated `wiki/` folder is meant to be committed.

Config lives at `~/.config/memoria/config.yaml`.

## Development

Go, stdlib CLI dispatch, entrypoint in `cmd/memoria/`.

```sh
scripts/test.sh          # go vet + go test -race
scripts/build.sh all     # cross-compile to dist/
```

See [AGENTS.md](AGENTS.md) for full internals and conventions.
