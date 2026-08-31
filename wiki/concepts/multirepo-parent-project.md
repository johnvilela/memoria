---
tags: [multirepo, monorepo, config, git, matching]
---

# Multirepo parent folders: what works, what breaks

Assessed 2026-07-30 ([[sessions/e112664f-9954-4d8e-8fd2-a11e13d66bc0]]) for the eparts layout: many separate microservice repos under one parent folder, worked as a monorepo "without the git features and package manager features". The three adaptations in the verdict landed later the same morning as commit `31b7251` (details at the end).

## How project matching behaves

- `matchProject` (config.go:76-89) is longest-prefix over registered paths: with only the parent registered, a session in `eparts/service-a` is captured and attributed to the parent — digests land in `eparts/.memoria/`. With parent and child both registered, the longest match (the child) wins. There is no symlink resolution, so a symlinked cwd fails to prefix-match.
- Registry entries in `~/.config/memoria/config.yaml` are `{name, path, wiki?}`; bootstrap dedupes on Path only, and Name is the folder basename.

## Git degrades silently outside a repo

As assessed (pre-`31b7251`), every git shell-out uses the project root as its `-C` dir, and a non-repo root never errors:

- Seeding is gated on `hasCommits` (`git rev-parse --verify HEAD`) — a non-repo silently skips seeding with no message (`seed.go:31`).
- `gitCheckpoint` returns empty → handoff packets omit their Git checkpoint section.
- Consolidation and lint read no git at all — digests plus the wiki are the only inputs.
- Bootstrap's `addGitignoreEntry` is unconditional (it creates a `.gitignore` even in a non-repo folder); `init`'s variant already gates on `.git` existing.

## Parent-registration breakpoints

1. No wiki seed, silently — the wiki starts empty with no way to seed from the children's histories.
2. No git checkpoint in handoff packets, even though the work happened inside a child repo.
3. `eparts/wiki/` and the parent `.gitignore` are versioned by nothing.
4. Concurrent sessions across services sabotage each other: `queueEndOthers` marks every other pending session of the same project ended the moment a new one starts ([[gotchas/implicit-session-end]]).
5. One background job slot per project: a session ending in service-a skips auto-consolidation while service-b's job runs.
6. `memoria run` sets cwd to the project root, so it launches the agent at `eparts` rather than the service you meant.
7. The AGENTS.md recall block exists only at the parent, with a relative `wiki/` path — whether a child-cwd agent sees it depends on the harness's ancestor walk (a walk that stops at the child's `.git` never finds it), and the relative path resolves wrong when it does. The MCP tools are cwd-based and stay correct.

## Per-child-registration breakpoints

Git, seed, checkpoint, run, and AGENTS.md all work per repo, but: N separate wikis with no cross-service memory, plus a basename-collision hazard — the queue (`pending.yaml`), status (`status.yaml`), and run log are keyed by project *name*, so two repos named `api` in different folders share a queue bucket and job slot, and a foreign proposal passes the name-based owner check while its sessions fail the path-prefix guard, leaving queue entries that reprocess forever.

## The hidden lever: `wiki:` is never validated

A hand-edited `wiki: ../shared-wiki` resolves consistently through `wikiRootFor` and its eight open-coded copies, giving per-child registration a shared wiki. Limits: `bootstrap --wiki ../shared-wiki` on the second child dies on the "already exists" guard before registering anything, and an absolute path breaks (`filepath.Join` mangles it).

## Verdict for the eparts workflow

User constraints: one logical project, sessions at the parent root one at a time, unversioned wiki acceptable if warned. Under those constraints, parent registration works today — capture, wiki, recall, run, and consolidation all function. Three small adaptations were agreed:

1. Warn on non-repo bootstrap ("wiki won't be versioned, seeding from history skipped") and gate the `.gitignore` write on `.git` existing.
2. Best-effort git checkpoint in `buildHandoff`: map file paths from the session's `@post-tool-use` events to their owning child repos and checkpoint the distinct repos touched (capped ~3).
3. Make the seed skip visible with a warning line.

**Shipped**: commit **`31b7251`** — `feat(cmd): support multirepo parent projects with warnings and per-repo checkpoints` — landed in incarnation 2 of the assessment session ([[sessions/e112664f-9954-4d8e-8fd2-a11e13d66bc0]]), staging `cmd/bootstrap.go`, `bootstrap_test.go`, `run.go`, `run_test.go`, `seed.go`. The file set matches the three adaptations one-to-one (bootstrap warning → bootstrap.go, per-repo checkpoints → run.go, seed-skip warning → seed.go); the commit is the only trace in capture, so implementation detail beyond the commit message and file list is not recorded.

Explicitly skipped as unneeded for this workflow: the concurrency rework, per-child AGENTS.md blocks, shared-wiki config support, and the name-collision fix.

Related: [[concepts/recall-and-run]] (the handoff packet the checkpoint feeds), [[concepts/architecture-overview]] (config and registration), [[gotchas/hooks-global-capture-opt-in]] (why untracked siblings stay invisible).