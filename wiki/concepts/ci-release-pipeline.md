---
tags: [ci, release, github-actions, gotestsum, testing]
---

# CI/CD: automated release pipeline and gotestsum test output

Shipped in one session ([[sessions/e7535e5e-c210-4029-8496-3ab3c84ea9dd]]) as PR #2 (the pipeline itself, releasing **v0.8.0**) and PR #3 (test output, releasing **v0.8.1**). Decision record: [[decisions/0012-ci-cd-release-pipeline]].

## `.github/workflows/ci.yml` — runs on every PR to `main`

- **`test`** job: runs `scripts/test.sh`.
- **`version-check`** job: fails the PR if `*.go` or `go.mod` changed without a matching bump to `const version` in `cmd/memoria/main.go`, or if the tag for the new version already exists. A docs-only PR (no Go files touched) passes automatically.

## `.github/workflows/release.yml` — runs on push to `main`

Reruns the tests, then checks whether tag `v<version>` (read from the const) already exists. If it's new: cross-compiles with `scripts/build.sh all`, generates sha256 checksums, and runs `gh release create --generate-notes` to publish the tag and release together. A merge that didn't bump the version skips the release step quietly — no error, just no new release.

## Branch protection: the `protect-main` ruleset

Created via `gh api repos/johnvilela/memoria/rulesets -X POST` with a hand-built JSON payload, not the GitHub web UI. Requires PRs to merge (0 required approvals — solo repo), requires both the `test` and `version-check` checks to pass, blocks force-push and branch deletion, and sets **no bypass actors** — the strictest option, an explicit choice over allowing the repo owner to skip it. Verified live by pushing an empty commit straight to `main` (rejected) and cleaning up with `git reset --hard origin/main`.

## Windows is not in the release matrix

`scripts/build.sh all` cross-compiles linux/amd64, linux/arm64, darwin/amd64, darwin/arm64 — no Windows. `flock.go`, `process.go`, and `status.go` call unix-only syscalls (`syscall.Flock`, `Setsid`, `Kill`), which don't compile on Windows, so a Windows binary would need a real port with `_windows.go` build-tagged variants first. A `ponytail:` comment in `build.sh` tracks this as a known ceiling.

## Version bump moved into the feature PR

The version const still lives in exactly one place — `cmd/memoria/main.go` (see [[skills/release-ritual]]) — but the bump now happens as part of the PR that needs it, not as a separate manual release commit afterward. `version-check` is what enforces this; tagging and publishing are fully automatic on merge.

## Test output: gotestsum in `testdox` format

Requested to make `scripts/test.sh` "more visual like vitest" — showing which scenario ran and whether it passed or failed. Implemented with `gotest.tools/gotestsum`, added via `go get -tool gotest.tools/gotestsum@latest` — the Go 1.25 `tool` directive pins it in `go.mod` rather than requiring a global install, so CI and any dev machine get the identical binary for free. `scripts/test.sh` now runs it in `testdox` format: each test prints as a humanized `✓`/`✗` line with duration (e.g. `✓ Bootstrap global idempotent (0.08s)`), failures are expanded at the bottom, and the run ends with a summary line (`DONE 323 tests, 6 skipped`). Arg pass-through was preserved and verified (`scripts/test.sh -run TestX` still filters correctly). Since this change touched `go.mod`, `version-check` required the patch bump to 0.8.1.

## Releases shipped so far

- **v0.8.0** — the const was bumped by hand first (commit `8d32d45`, closing drift left by `8e495d5`/global capture landing with no bump), then PR #2 shipped the pipeline itself and produced the actual tag + release automatically on merge.
- **v0.8.1** — PR #3, the gotestsum tooling change, a patch bump forced by `version-check`.

Related: [[decisions/0012-ci-cd-release-pipeline]], [[skills/release-ritual]].