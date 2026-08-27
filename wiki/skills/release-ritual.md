---
tags: [release, versioning, workflow, ci]
---

# Release ritual

> **Partially superseded**: steps 4–6 below and `gh release create` are now automated by `.github/workflows/release.yml`, and can't be done by hand anyway — `main` is protected by a ruleset that rejects direct pushes (see [[decisions/0012-ci-cd-release-pipeline]]). You still pick the number and bump the const by hand, but the bump now happens inside a feature branch/PR: CI's `version-check` job blocks merging Go changes without one. On merge to main, `release.yml` runs the tests, cross-compiles, tags `v<version>`, and publishes the GitHub release. Mechanics: [[concepts/ci-release-pipeline]].

Semver, pre-1.0: minor bump per feature batch, patch bump per fix-only or tooling-only release. Version lives in **one place**: `const version` in `cmd/memoria/main.go` — the MCP handshake (`cmd/memoria/mcp.go`) and the `version` subcommand both read it.

Current steps, in order, inside a feature branch (direct pushes to `main` are rejected by the `protect-main` ruleset):

1. Pick the number: new features since last tag → bump minor (`0.8.0` → `0.9.0`); fixes or tooling-only changes (e.g. a `go.mod` change) → bump patch (`0.8.0` → `0.8.1`).
2. Edit `const version = "X.Y.Z"` in `cmd/memoria/main.go`, in the same PR as the change that needs it.
3. Verify: `scripts/test.sh` (now vitest-style output via gotestsum's `testdox` format — see [[concepts/ci-release-pipeline]]), then `go run ./cmd/memoria version` prints the new number.
4. Push the branch and open a PR — CI's `test` and `version-check` jobs must pass; `version-check` fails the PR if Go files changed with no bump, or if the target tag already exists.
5. Merge (squash) — `release.yml` picks up the new tag automatically: cross-compiles for linux/darwin × amd64/arm64 (no Windows yet — [[decisions/0012-ci-cd-release-pipeline]]), generates sha256 checksums, and creates the tag + GitHub release with generated notes. Nothing left to run by hand.

Gotcha: tag and const must move together — a tag without the const bump makes `memoria version` lie (the MCP handshake said `0.1` for the whole pre-tag era). That's now enforced by CI instead of by discipline.

Releases so far:

- **v0.7.0** — commit `5a01725`, the first release, fully manual (pre-automation).
- **v0.8.0** — the const was bumped by hand first (commit `8d32d45`, closing drift left by `8e495d5`/global capture landing with no version bump), then the PR that shipped the CI/CD pipeline itself produced the actual `v0.8.0` tag and release automatically on merge.
- **v0.8.1** — a follow-up PR (gotestsum test-output tooling) bumped `go.mod`, so `version-check` forced a patch bump; the release published automatically on merge, same as v0.8.0.