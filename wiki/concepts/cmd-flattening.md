---
tags: [refactor, cmd, cli, layout, ci]
---

# Repo layout: `cmd/memoria/` flattened into `cmd/`

Shipped on branch `refactor/flatten-cmd`, PR #11 (open, CI green at session close) — [[sessions/d67d4ba2-5135-4985-b81d-4ef7666d0232]]. Version bumped **0.12.0 → 0.12.1** (tooling-only patch, per [[skills/release-ritual]]).

## Motivation

The project's main package lived at `cmd/memoria/` — redundant nesting, since the repo itself is already called "memoria". Request: move everything up to `cmd/` directly, with the CLI's "root command" registering from `cmd/` itself rather than a subfolder.

## Recon before the move

An Explore agent mapped the ground first:

- **No CLI framework** — the router is a hand-rolled `switch` on `os.Args[1]` in `main.go` plus one stdlib `flag.NewFlagSet` per subcommand (already documented in [[concepts/recall-and-run]]: "manual switch dispatch in `main.go` plus one stdlib FlagSet per subcommand"). There is no framework object to register a "root command" on — the closest equivalent is the `""`/`help` case in the switch.
- `cmd/` held only the `memoria/` subfolder — around 61 Go files (all `package main`) at recon time, plus a `_test.go` sibling for nearly every source file, and `cmd/memoria/prompts/` (`digest-prompt.md`, `lint-prompt.md`, `seed-prompt.md`, `wiki-prompt.md`).
- The router (`main.go`'s `switch`) and the help text (`help.go`'s `commands` slice) are two separate registries, not derived from each other — adding or renaming a command means touching both.
- Nothing breaks in Go itself from the move: no package-name collision (everything was already `package main`), no import cycle possible (a `main` package can't be imported), and the four `//go:embed prompts/<name>` directives are file-relative so they keep working as long as `prompts/` moves with the source files.
- **The real breakage is the binary name.** Go derives an installed binary's name from the last path element of the main package's directory: `cmd/memoria` → `memoria`, but `cmd` → literally **`cmd`**. `go install` has no `-o` flag to override this. The Explore agent flagged that moving the package to the **repo root** instead would have kept `go install github.com/johnvilela/memoria@latest` working (the module name wins there) — but the user's explicit request was to flatten into `cmd/`, accepting that trade-off.

## What moved

Pure `git mv` — the PR body states 65 Go files plus `prompts/` moved from `cmd/memoria/` to `cmd/` as 100%-detected renames; no Go code changed.

Path references updated via `sed` across:
- `scripts/build.sh`, `scripts/install.sh` — build target `./cmd`
- `.github/workflows/ci.yml`'s `version-check` job and `.github/workflows/release.yml` — both read `const version` from `cmd/main.go` now
- `AGENTS.md`, `README.md`
- Live wiki reference docs: `wiki/skills/release-ritual.md`, `wiki/concepts/ci-release-pipeline.md`, `wiki/concepts/consolidation-pipeline.md`, `wiki/concepts/recall-and-run.md`, `wiki/concepts/session-capture.md`, `wiki/concepts/architecture-overview.md`, `wiki/concepts/session-decay.md`, `wiki/concepts/self-update-command.md`, `wiki/concepts/multirepo-parent-project.md`, `wiki/gotchas/prompt-over-stdin-argv-limit.md`, `wiki/gotchas/stale-prompt-overrides.md`, `wiki/decisions/0004-embedded-prompts-with-file-override.md`, `wiki/decisions/0011-deterministic-json-repair-over-retry.md`, and the relative links in `wiki/research/ai-memory-workstream-comparison.md` / `wiki/research/memoria-evaluation.md`

Historical wiki `sessions/*.md` pages were deliberately left referencing the old `cmd/memoria/` path — they're episodic records of when that path was real.

## The README go install line was dropped

Since a main package in a directory named `cmd` installs as a binary literally named `cmd`, the README's `go install github.com/johnvilela/memoria/cmd/memoria@latest` line no longer has a working equivalent and was removed outright. Manual installs now go through `scripts/install.sh` only.

## CI edge case on this PR specifically

`version-check`'s base-ref comparison (`git show origin/<base>:cmd/memoria/main.go`, per [[decisions/0012-ci-cd-release-pipeline]]) reads the version const from both HEAD and the base ref. On this specific PR, the base (`main`) still has the file at the old `cmd/memoria/main.go` path, so that read finds nothing, `base_version` comes back empty, and the bump comparison passes trivially rather than actually comparing versions. No dual-path handling was added for this — it self-heals automatically once the PR merges and `main` has the file at its new `cmd/main.go` path.

## Verification

`scripts/test.sh` (vet + `-race`): 381 tests green. Built binary and `go run` both confirmed: `./memoria version` and `go run ./cmd version` each print `memoria 0.12.1`. `gh pr checks 11 --watch` reported CI green before the session ended.

## Left out of the commit on purpose

The initial commit's `git add -A` also staged `wiki/concepts/session-decay.md`, `wiki/decisions/0013-deterministic-decay-over-salience-model.md`, and `wiki/sessions/acbf5d0d-d82b-4852-98fc-97cfbfb5da35.md` — leftover consolidation output from a prior session, unrelated to this refactor. They were unstaged (`git rm --cached`) and the commit amended before pushing. `wiki/index.md` was excluded from the start via `git reset -q wiki/index.md`.

Related: [[concepts/architecture-overview]] (the pipeline description this refactor's path updates now match), [[decisions/0012-ci-cd-release-pipeline]] (the `version-check` mechanics), [[skills/release-ritual]] (where the version const lives).