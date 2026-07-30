---
tags: [init, setup, clients, hooks, mcp]
---

# init and setup: multi-agent install and the clients registry

As of commit `7128a3d` (`feat(init): install hooks and mcp for multiple agents in one run`, [[sessions/80f131bd-209e-4610-8bf3-07dac9154545]]), one memoria install can wire up several code agents at once, and the config remembers which ones.

## Installing

- `memoria init claude-code codex` (multiple positionals) or `memoria init --client claude-code,codex`. On a TTY with no client given, a checkbox multi-select (`selectMulti` in tui.go, a stubable var mirroring `selectOption`) replaces the old single-choice picker.
- Validation is fail-fast: every name is normalized and checked before any install, so `init good bad` installs nothing and names the bad client. `claude` is an accepted alias for `claude-code`, normalized before dedup — the config never stores `claude`.
- `memoria setup --client codex` adds an agent surgically: no prompts, existing installs untouched. This ends setup's previous "hooks stay init-only" rule. Interactive setup (zero flags) gains a first step: it prints `Capture hooks installed for: ...`, offers a multi-select of the missing agents, and skips silently when everything is installed.
- Shared helpers in init.go serve both commands: `splitClients`, `normalizeClient`, `installClients`, `recordClients`. The per-agent `installClientHooks` is untouched.

## The clients registry

`clients:` in `~/.config/memoria/config.yaml` (`Clients []string`) records which agents have capture hooks installed. Before this field, nothing could answer "which agents did the user install into?" without parsing the agents' own settings files. `recordClients` merge-dedups and saves best-effort — a save failure warns instead of failing, because the hooks are the real effect. `detectClients` (hooks.go) backfills pre-feature installs during interactive setup by scanning `~/.claude/settings.json` and `~/.codex/hooks.json` for the memoria hook command, always unioned with the stored list.

## Per-agent install surface

| Agent | Hooks | MCP registration |
|---|---|---|
| `claude-code` | `~/.claude/settings.json` | `~/.claude.json` → `mcpServers.memoria` |
| `codex` | `~/.codex/hooks.json` | `~/.codex/config.toml` → `[mcp_servers.memoria]` |

Both agents share the same JSON hook shape; `installHooks` itself is agent-agnostic (an events map plus a settings path). Hook install is idempotent: `findMemoriaHook` matches the ` hook <name>` command and re-points the existing entry in place, handling moved binaries and pre-`--client` installs without duplicating. Codex extras: run `/hooks` once inside Codex to trust the new hooks, and Codex has no Notification event, so that hook is skipped.

Two distinct agent lists are easy to conflate: hook/MCP **install targets** (claude-code, codex) versus **session processors** (claude-code | codex | ollama | gemini — [[concepts/consolidation-pipeline]]).

## Known limits

- Per-agent knowledge is still spread over several switch/map sites (init.go's picker and install switches, the hooks.go event maps, the two mcpinstall.go functions, run.go's `binClient`/`nativeResume`) — the recon estimate is ~6 places to edit when adding a third agent.
- There is no uninstall for hooks or MCP registration; removing memoria from an agent is manual settings editing. The only removal path that exists is the cron timer (`--cron off`).

Related: [[concepts/session-capture]] (what the hooks capture), [[concepts/mcp-server]] (what MCP registration enables), [[concepts/architecture-overview]] (the config file).