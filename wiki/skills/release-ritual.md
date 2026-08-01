---
tags: [release, versioning, workflow]
---

# Release ritual

Semver, pre-1.0: minor bump per feature batch, patch bump per fix-only release. Version lives in **one place**: `const version` in `cmd/memoria/main.go` — the MCP handshake (`cmd/memoria/mcp.go`) and the `version` subcommand both read it.

Steps, in order, one commit:

1. Pick the number: new features since last tag → bump minor (`0.7.0` → `0.8.0`); only fixes → bump patch (`0.7.0` → `0.7.1`).
2. Edit `const version = "X.Y.Z"` in `cmd/memoria/main.go`.
3. Verify: `go build ./... && go test ./cmd/memoria/` then `go run ./cmd/memoria version` prints the new number.
4. Commit: `git commit -m "chore(release): vX.Y.Z"` (include the version bump only, nothing else rides along).
5. Tag the same commit: `git tag vX.Y.Z`.
6. Push both: `git push && git push origin vX.Y.Z`.

Optional, when a changelog page is wanted: `gh release create vX.Y.Z --generate-notes`.

Gotcha: tag and const must move together — a tag without the const bump makes `memoria version` lie (the MCP handshake said `0.1` for the whole pre-tag era). First release under this ritual: v0.7.0, commit `5a01725`.
