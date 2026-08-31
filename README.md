# memoria

Long-term memory and a per-project wiki for code agents, built from their chat sessions.

memoria captures knowledge (decisions, rules, strange bugs) from agent conversations via hooks, digests it into markdown files inside your project, and makes it readable later by humans or by future agent sessions — through the CLI or the built-in MCP server. No database, no embeddings — plain `.md` files, versioned with the project.

Inspired by Fabio Akita's ai-memory and Andrej Karpathy's LLM wiki idea.

## Install

```sh
curl -sS https://raw.githubusercontent.com/johnvilela/memoria/main/scripts/install.sh | sh
```

The script downloads the latest release binary for your platform (checksum-verified, no Go needed) to `~/.local/bin`, then runs `memoria init` — which itself adapts to the platform (systemd timer on Linux, launchd agent on macOS) and to the agents it finds installed (Claude Code, Codex). Later, `memoria update` upgrades it in place.

Or from a local checkout (needs Go):

```sh
scripts/build.sh        # builds ./memoria
scripts/install.sh      # installs to ~/.local/bin + runs init
```

## Quick start

```sh
# 1. Install hooks + MCP server, choose the session processor (once per machine)
memoria init               # interactive (bubbles select)
# or scriptable: memoria init --client claude-code --processor claude-code --cron

# 2. Opt a project in (run inside the project)
cd ~/dev/my-project
memoria bootstrap          # registers the project, writes recall instructions
                           # into AGENTS.md, offers to seed wiki/ from git history

# 3. Work with your agent as usual — sessions are digested automatically
#    into .memoria/sessions/pending/<session_id>.md

# 4. Consolidate finished sessions into the project wiki
memoria process            # background; LLM proposes pages -> .memoria/proposal.json
memoria status             # running / done / error per project
memoria process --apply    # after review: writes wiki/, archives sessions
# (or let the cron timer + the agent's MCP tools do both)

# 5. Read it back
memoria search "#queue"    # find wiki pages by text or tag
memoria run codex          # launch an agent, pick a session to continue
```

## Commands

| Command | Description |
|---------|-------------|
| `help` | Show usage |
| `init [<client>] [--client ...] [--processor ...] [--notification] [--auto-apply] [--auto-commit] [--cron [expr]] [--cron-apply]` | Install memoria hooks for an agent (claude-code \| codex), register the memoria MCP server (claude-code: `~/.claude.json`, codex: `~/.codex/config.toml`), choose the AI provider that processes sessions (claude-code \| codex \| ollama \| gemini), opt into desktop notifications, autopilot (`--auto-apply`), wiki auto-commit (`--auto-commit`) and the periodic systemd timer; interactive when flags are omitted |
| `setup [--processor ...] [--notification] [--auto-apply] [--auto-commit] [--cron <expr\|off>] [--cron-apply]` | Reconfigure processor / notifications / auto-apply / auto-commit / schedule without touching hooks; no flags = interactive walk over current values |
| `bootstrap [--wiki <name>] [--background]` | Register the current folder as a tracked project; gitignores `.memoria/`, creates the wiki folder (an existing one is adopted as-is — handy after renaming a project folder), writes agent recall instructions into `AGENTS.md` (marker block, repaired on re-run) + a `CLAUDE.md` shim, then offers to seed the wiki from git history when it has no pages (`--background` detaches) |
| `remove` | Pick a registered project and remove it from memoria — config entry plus its pending/status state; files in the project folder are never touched |
| `process [--apply] [--foreground] [--inspect] [--all]` | Consolidate ended sessions into the wiki: detaches by default, writes a JSON proposal for review, `--apply` creates the files. `--inspect` follows a running job, `--all` sweeps every project (the timer's entrypoint) |
| `lint [--review] [--apply] [--deny "why"]` | Audit the wiki for contradictions / stale / duplicate pages in the background; `--review` prints the findings, `--apply` fixes them via a second LLM pass, `--deny` rejects them with a reason future runs remember |
| `run <agent-binary> [--new \| --session <id\|name>]` | Launch any agent on PATH inside the project, continuing a previous session: same harness → native resume (`claude --resume`, `codex resume`), different harness → a self-contained handoff packet: git checkpoint + inlined session history + session summary page, capped to fit one argv element. No flags → interactive picker: new session or one of the last 5 (sessions without a digest resume natively and may be slow) |
| `search [--trash] <text \| #tag>` | Find wiki pages by content substring or frontmatter tag and print the chosen one; trashed pages stay hidden unless `--trash`. Printing a session page refreshes its `lastUsed` date so it doesn't decay |
| `commit [-m "subject"]` | Commit the project's wiki folder — new and modified pages only, message in the same `docs(wiki): ...` shape, your other staged files untouched |
| `status` | Show background processing state per project (running / done / error) |
| `list` | List registered projects — name, wiki folder, and whether the path still exists |
| `update [-y]` | Check GitHub for a newer release; shows the version and changelog, asks to install, then replaces the binary in place (sha256-verified). `-y` skips the prompt (and is the non-interactive path) |
| `mcp` | Internal — stdio MCP server for agents (see below) |
| `digest <sid>` | Internal — compile one session's digest into its `sessions/<sid>.md` wiki page |
| `hook <name>` | Internal — called by agent hooks to capture session data |

## MCP server

`memoria init` registers `memoria mcp` with the agent, so sessions get seven tools out of the box:

| Tool | What it does |
|------|--------------|
| `memoria_search` | Search the wiki by text or `#tag` (`include_trash` to look inside `trash/`); results whose content is returned refresh the page's `lastUsed` |
| `memoria_recall` | Read-only record of a past session — git checkpoint, event log, last state. No LLM, no writes (refreshes the session page's `lastUsed`) |
| `memoria_digest` | Writes/overwrites `sessions/<sid>.md`: compiles the session's observation log into a clean page (background LLM job — call again to poll) |
| `memoria_consolidate` | Batch-consolidate ended sessions; when the proposal is ready the agent reviews the page list and calls again with `apply=true` |
| `memoria_lint` | Audit the wiki for contradictions and return the findings |
| `memoria_write_page` | Create or update a wiki page (path validated, tags frontmatter rendered by memoria) |
| `memoria_delete_page` | Move a page to `trash/`, tagged `deleted` and hidden from search |

The long-running tools (digest, consolidate, lint) never block the agent: the first call starts a detached job, later calls poll its state — the same one-job-per-project tracking `memoria status` shows.

## How it works

- **Hooks** — `memoria init` wires agent hooks (Claude Code, Codex) that run `memoria hook <event> --client <name>` on each session event. Hooks are global, but only projects registered with `memoria bootstrap` are captured; everything else is ignored. Hooks never block the agent.
- **Live digests** — each event appends an `@hook` annotated line to `<project>/.memoria/sessions/pending/<session_id>.md` (YAML frontmatter + chronological stream: full prompts, file writes/edits, Bash commands with errors, assistant stop messages). Sessions are indexed in `.memoria/sessions.md` by their first user prompt. Reopening an already-processed session starts a numbered incarnation (`<sid>-2.md`) linked to the archived one via `continues_from`.
- **Pending queue** — new digests are registered in `~/.config/memoria/pending.yaml`, grouped by project. A session counts as ended when `session-end` fires or when a new session starts in the same project (so crashed sessions don't linger); `memoria process` consumes this worklist.
- **Wiki** — `memoria process` detaches and returns immediately (the processor call can take minutes and must never block an active agent session); `memoria status` tracks the run, and with `notifications: true` the finished run pings the desktop via notify-send. The processor gets the ended sessions + current wiki and returns a JSON proposal: 1-5 pages under the suggested `concepts/`, `decisions/`, `gotchas/`, `rules/` and `sessions/` (plus `index.md` and any top-level folder you've created in the wiki yourself — `trash/`, `_global/` and dot-folders are reserved), connected by `[[wikilinks]]`, tags rendered as YAML frontmatter. The LLM never writes files: you (or the agent, via `memoria_consolidate`) review `.memoria/proposal.json`, then apply writes the pages and archives the sessions to `.memoria/sessions/processed/`. Prompts are built in — the batch one at [cmd/prompts/wiki-prompt.md](cmd/prompts/wiki-prompt.md), the per-session one at [cmd/prompts/digest-prompt.md](cmd/prompts/digest-prompt.md) — and replaceable by creating a file with the same name in `~/.config/memoria/`.
- **Cron** — `--cron` installs a systemd user timer running `process --all` on your schedule (`hourly`, `every 3 hours`, `8 times a day`, or 5-field cron); `--cron-apply` makes it apply proposals without review. Each pass also runs the decay sweep over every project.
- **Decay** — episodic memory fades when unused: `sessions/` pages carry a `lastUsed` date (managed by memoria only, never by the LLM) refreshed whenever their content is actually delivered — a search that prints the page, `memoria_recall`, a `memoria run` resume. The sweep moves pages unused for `decay_soft_days` (default 15) to `trash/` and permanently removes trashed ones unused for `decay_hard_days` (default 30). Pages without the field are stamped, never deleted, on first sight — a pre-existing wiki starts its clock on the first sweep. `concepts/`, `decisions/`, `rules/` and `gotchas/` never decay.
- **Autopilot** — `--auto-apply` (off by default) removes every manual step: ending a session triggers consolidation by itself, proposals are written straight to the wiki, and lint findings are fixed immediately. The review gate comes back the moment you turn it off.
- **Recall** — `bootstrap` writes a marker block into the project's `AGENTS.md` telling agents where the memory lives and to prefer the MCP tools; `memoria run` carries a finished session into the next one, natively when it's the same harness.
- **Markdown in the repo** — session digests stay untracked (`.memoria/` is gitignored); the curated `wiki/` folder is meant to be committed. `memoria commit` commits it for you — new and modified pages only, with a `docs(wiki): update — N page(s) (...)` message (`-m` overrides the subject) and a pathspec commit that leaves your own staged files alone. Auto-commit after every applied change (proposals, lint fixes, seed, session pages) is opt-in: `--auto-commit` on `init`/`setup`, or `wiki_auto_commit: true` in the config; `wiki_commit_message` customizes the pattern for both. Deleted pages land in `wiki/trash/` instead of vanishing.

Config lives at `~/.config/memoria/config.yaml` (`projects`, `processor`, `processor_model`, `processor_effort`, `wiki_commit_message`, `wiki_auto_commit`, `notifications`, `auto_apply`, `cron`, `decay_soft_days`, `decay_hard_days` and `gemini_api_key` when the gemini processor is chosen — the file is written 0600). The gemini key can also come from the `GEMINI_API_KEY` env var.

## Development

Go, stdlib CLI dispatch, entrypoint in `cmd/`.

```sh
scripts/test.sh          # go vet + go test -race
scripts/build.sh all     # cross-compile to dist/
```

See [AGENTS.md](AGENTS.md) for full internals and conventions.
