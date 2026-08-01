---
tags: [install, path, macos, deleted]
---

# install.sh: ask where to put the binary + fix PATH

> Resolved: `memoria init` now detects `$SHELL` and appends the PATH export to
> the right rc file itself (`ensurePathEnv` in `cmd/memoria/init.go`). The
> "ask where" part was skipped — `BIN_DIR` env override covers it.

Reported on macOS: `zsh: command not found: memoria` after install. `scripts/install.sh` installs to `~/.local/bin`, which macOS zsh does not have on PATH by default — the script only prints a warning (`note: $BIN_DIR is not on your PATH`) and never fixes it. init/bootstrap kept working because hooks/MCP/launchd record the absolute binary path; only bare `memoria` in a new shell fails.

Plan: rework install.sh to ask the user where they want the binary referred (install dir / PATH), and append the PATH export to the right shell rc (`~/.zshrc` on macOS, `~/.profile`/`~/.bashrc` on Linux) instead of warn-only.

Workaround until then:

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
exec zsh
```
