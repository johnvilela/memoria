---
tags: [adr, ci, release, github, branch-protection]
---

# ADR 0012: Automate release on merge to main, protect main with a GitHub ruleset

**Decision** (commit `8d32d45` + PR #2, [[sessions/e7535e5e-c210-4029-8496-3ab3c84ea9dd]]): releasing memoria is no longer a manual `git tag` + `gh release create` ritual. Two GitHub Actions workflows now own it end to end — `ci.yml` blocks a PR from merging if it touches Go code without bumping `const version`, and `release.yml` tags and publishes a GitHub release automatically the moment a bumped version lands on `main`. `main` itself is protected by a `protect-main` ruleset (created via `gh api`, not the web UI): PRs required, both CI checks required, no force-push, no branch delete, and **no bypass actors at all** — not even the repo owner can skip it.

**Rationale**: explicit user request — release whenever a PR merges to main, an AI rule to keep the version consistent, and main blocked so all changes go through a branch + PR.

**Windows dropped from the release build matrix**: `flock.go`, `process.go`, and `status.go` use unix-only syscalls (`syscall.Flock`, `Setsid`, `Kill`), so Windows binaries can't be cross-compiled today. Rather than ship a broken target, Windows was left out of `scripts/build.sh all`'s matrix (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64 only), with a `ponytail:` comment tracking that a real port with `_windows.go` build-tagged variants is the prerequisite.

**Consequence**: memoria's own opt-in wiki auto-commit ([[concepts/wiki-auto-commit]]) would have its push to `main` rejected by the same ruleset if enabled — wiki updates now need a branch + PR like any other change, or auto-commit can simply stay off (already the default per [[decisions/0010-wiki-auto-commit-is-opt-in]]).

**Verified live**: PR #2 merged and automatically published **v0.8.0** (4 binaries + sha256 checksums); the downloaded `memoria_linux_amd64` binary printed `memoria 0.8.0`; a direct push probe to `main` was rejected by the ruleset. PR #3 (gotestsum test output, [[concepts/ci-release-pipeline]]) then exercised the same pipeline for a patch release, publishing **v0.8.1**.

Related: [[concepts/ci-release-pipeline]] (workflow mechanics), [[skills/release-ritual]] (the updated release steps).