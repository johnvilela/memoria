---
tags: [update, release, install, checksum, build-size]
---

# Self-update command: `memoria update`

Shipped on branch `feat/update-command` (pushed, **no PR opened yet** — explicit instruction: "commit and push the branch, dont create the PR yet"), in the session that also trimmed the release binary size ([[sessions/6f2b7832-1db8-4e7a-a2c0-ca0cc207e4c8]]). Version bumped to **0.9.0**.

## What it does

`memoria update [-y]` (new `cmd/memoria/update.go`):

1. Fetches `https://api.github.com/repos/johnvilela/memoria/releases/latest` (the URL is a package var, `latestReleaseURL`, purely so tests can point it at an `httptest` server).
2. Compares the running `const version` against the release tag **numerically** — `parseVer` parses `x.y.z` into an int triple rather than doing a string comparison, so a dev build ahead of the last release (e.g. local `0.10.0` vs published `v0.9.0`) correctly reports "nothing to do" instead of offering a downgrade.
3. If a newer release exists: prints `New version available: vX.Y.Z (current ...)` plus the release's changelog body verbatim, then confirms via the same bubbletea `selectOption` picker `seed.go` uses for its yes/no prompt. `-y` skips the prompt. Without a TTY and without `-y`, it prints the info plus a `memoria update -y` hint and exits 0 rather than hanging.
4. Downloads the `memoria_<GOOS>_<GOARCH>` asset plus `checksums.txt`, verifies the binary's sha256 against the checksum file, and only then replaces the running binary: `os.Executable()` (behind a stubbable `executable` var) resolved through `filepath.EvalSymlinks`, written to a temp file in the same directory (so the final `os.Rename` never crosses devices), `chmod 0755`, then renamed over the running binary — safe on Unix since the running process keeps its old inode. Any failure before that final rename leaves the installed binary untouched.

Built via TDD: `update_test.go` was written and confirmed red first. 10 tests cover up-to-date, local-newer, non-TTY hint, decline, successful install, checksum mismatch, missing platform asset, and symlink-target resolution, all driven against an `httptest` server with `latestReleaseURL`/`isTTY`/`selectOption`/`executable` stubbed — the same stub pattern `init_test.go` already used for `checkGeminiKey`. Full suite green at 332 tests under `-race`.

## `scripts/install.sh` also rewritten

The standalone (non-in-repo) install path now downloads the checksum-verified release binary for the detected OS/arch directly, instead of `go install github.com/johnvilela/memoria/cmd/memoria@latest` — Go is only needed for in-repo/dev builds now. This resolves a `ponytail:` TODO the script had carried since before GitHub releases existed ("swap the go-install path for a release-binary download once github releases exist"). Both `install.sh` and `update` rely on the same release shape [[decisions/0012-ci-cd-release-pipeline]] and [[concepts/ci-release-pipeline]] already publish: exactly `checksums.txt` + 4 bare platform binaries (`memoria_<os>_<arch>`, no version in the filename), with changelog text available as the release's auto-generated `body`.

## Known ceilings (marked `ponytail:` in code)

- `parseVer` only understands plain `x.y.z` tags — no pre-release suffixes. Upgrade path if tags ever grow them: `golang.org/x/mod/semver`.
- The release binary is downloaded fully into memory before writing, not streamed. Fine at the current ~10-15 MB; would need streaming if binaries grow to tens of MB.

## Follow-up: release binary size, 15 MB → ~10 MB

Right after `update` shipped, the user asked why the binary was 15 MB. Breakdown via `go tool nm -size`: roughly 2 MB baseline Go runtime/stdlib, ~1.5 MB crypto+net (the full TLS stack, needed for HTTPS calls to the Gemini and now GitHub APIs), the MCP SDK + jsonschema-go, the full charm TUI stack (bubbletea/lipgloss/bubbles plus Unicode segmentation tables), encoding/json, yaml.v3, regexp — and about 5 MB of symbol table + DWARF debug info.

That last 5 MB is free to drop: `-ldflags="-s -w"` strips it with no functional loss, because Go keeps a separate, always-present `pclntab` table that the runtime itself needs — panic stack traces still show real function names and line numbers even on a fully stripped binary.

**Fix**: added `-ldflags="-s -w"` to `scripts/build.sh`'s release path only — the local/dev build path is untouched, so `delve` still attaches to local builds. Verified all four cross-compile targets (linux/darwin × amd64/arm64) rebuild at roughly 9-11 MB, down from ~15 MB (about a third smaller). Committed separately from the `update` command, on the same branch, as `build: strip symbol table and DWARF from release binaries`.

Related: [[concepts/ci-release-pipeline]] (the release workflow this build/install path serves), [[decisions/0012-ci-cd-release-pipeline]] (why the release publishes exactly these assets), [[gotchas/module-path-mismatch-breaks-go-install]] (the earlier install-path problem the standalone script now supersedes).