#!/bin/sh
# Install memoria to $BIN_DIR (default ~/.local/bin).
# Inside the repo: builds and installs the local checkout.
# Anywhere else (e.g. curl | sh from github): go-installs the latest published version.
# Requires Go. ponytail: swap the go-install path for a release-binary download once github releases exist.
set -eu

BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
mkdir -p "$BIN_DIR"

command -v go >/dev/null 2>&1 || { echo "error: go is required" >&2; exit 1; }

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." 2>/dev/null && pwd)
if [ -f "$repo_root/go.mod" ]; then
    go build -C "$repo_root" -trimpath -o "$BIN_DIR/memoria" ./cmd/memoria
else
    GOBIN="$BIN_DIR" go install github.com/jv77/memoria/cmd/memoria@latest
fi

echo "installed $BIN_DIR/memoria"
case ":$PATH:" in
    *":$BIN_DIR:"*) ;;
    *) echo "note: $BIN_DIR is not on your PATH" ;;
esac
