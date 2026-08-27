---
tags: [global-capture, bootstrap, setup, config, git]
---

# Global capture mode: `--global` and `--global-path`

Shipped in commit `8e495d5` ([[sessions/266a3d10-ad0c-44f1-86f7-379941908fbf]]), following recon in [[sessions/19c76dfd-1284-48fa-bbba-486b6f7d66f0]]. Lets memoria capture sessions in folders that were never registered with `memoria bootstrap` — a separate, explicit opt-in layered on top of the per-project model described in [[gotchas/hooks-global-capture-opt-in]].

## Enabling

- `memoria bootstrap --global` — turns on global capture. `--global --wiki` and `--global --background` are rejected (a global wiki has no per-run name, and there is nothing to seed).
- `memoria bootstrap --global --global-path <folder>` — same, but writes to `<folder>` instead of the default root. Relative paths are resolved and stored absolute. The folder does not need to be a git repo.
- `memoria setup --global` / `--global=false` — enable or disable without going through bootstrap directly. Disabling keeps `global_path` in the config so a later re-enable finds the same root.
- `memoria setup --global-path <folder>` — move the root (requires global already on, or `--global` alongside); an empty value resets to the default root.
- `--global=false --global-path X` together is rejected.

Config keys: `global: bool`, `global_path: string` (empty = default root). The flag was named `--path` for part of the session and renamed to `--global-path` on both `bootstrap` and `setup` at explicit request before the commit landed — the old `--path` now errors as unknown.

## Routing: registered projects still win

`resolveProject` tries the existing longest-prefix `matchProject` first; only when that fails and `cfg.Global` is set does a session fall through to a `_global` pseudo-project. A folder that is already `bootstrap`-registered behaves exactly as before — global mode only catches what was previously silently ignored.

## Where it writes

- **Default root**: `~/.config/memoria` (the config directory). Its `wiki/` subfolder gets its own `git init` and an init commit — but **the config directory itself is never turned into a git repo**, since `config.yaml` can hold a `gemini_api_key` and is kept 0600. Every applied write to the default-root wiki auto-commits regardless of the `wiki_auto_commit` setting.
- **`--global-path <folder>`**: memoria never touches git there at all — no init, no auto-commit. It is the user's own folder to manage.

This forced-commit / never-commit split is implemented by `globalCommitCfg`, which flips `WikiAutoCommit` on a copy of the config at three call sites (`runProcess`, `processAll`, `runLint`) rather than threading extra state through `commitWiki`.

## What gets captured, and where it lands

A session in an unregistered folder produces a digest exactly like a normal project session, except the frontmatter's `project:`/`project_root:` keys — previously written but read by no code — now record the real source folder's basename and path. Nothing is ever written into the source folder itself. All global digests, queue entries, and status share one `_global` slot (queue, status, job, `_global.run.log`) — the same pooling as multiple sessions inside one project.

The global wiki mixes the usual shared root categories (`concepts/`, `decisions/`, etc.) with per-source-folder namespaces (`<folder>/concepts/...`), so knowledge from unrelated source folders does not collide. This is taught to the LLM via a Go-appended prompt addendum — not a separate prompt file, so the existing FAITHFULNESS/wikilinks rules stay shared — and enforced by a new `global bool` parameter on `validPagePath`/`validatePages`: in global mode any non-reserved top-level namespace is a valid write target, not just the five suggested categories.

## Which commands are global-aware

Touched: `captureHook` (the capture gate), `process` (including `--all`, so the cron sweeps `_global` for free), `lint`. Left project-scoped on purpose: `search`, `run`, `mcp`, `commit`, `digest` — these are resume/recall machinery or already error per-call in an untracked cwd, and did not need a global variant.

## Known ceilings

- **Root-change stranding**: changing `--global-path` can leave old-root pending queue entries pointing at the old location (`applyProposal` only archives digests under the resolved project's path). Bootstrap/setup warn when pending `_global` entries still reference a stale root; there is no automatic migration.
- **Same-basename source folders**: two different source folders with the same basename share one wiki namespace; the `project_root` frontmatter lets the LLM disambiguate but nothing enforces it.
- **`queueEndOthers` pools globally**: a new session starting in any unregistered folder marks every other pending `_global` session ended — same semantics as concurrent sessions inside one project ([[gotchas/implicit-session-end]]).
- No disable-and-forget: `setup --global=false` deliberately keeps `global_path` around rather than clearing it, so re-enabling restores the same root.