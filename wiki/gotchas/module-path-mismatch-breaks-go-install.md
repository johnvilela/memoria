---
tags: [gotcha, release, go-module, install]
---

# Module path doesn't match the GitHub remote — go install breaks

Found while refreshing the README ([[sessions/85a0d12d-60b4-4442-b107-f40c097122d8]]): `go.mod` declares the module as `github.com/jv77/memoria`, but the git remote is `github.com/johnvilela/memoria`.

`go install ...@latest` — the install path the README documents — resolves through the **module path**, so it can't work until the two match. `install.sh` is affected the same way: it go-installs `@latest` and additionally needs a published tag to work standalone.

Release order for `v0.1.0`: fix the module path in `go.mod` first, then tag. Tag first and the release is broken on arrival.

Related release loose ends noted in the same session: the `.gitignore` change (`.memoria/` line) and the `wiki/` folder were still uncommitted, and the ollama processor remains a placeholder.