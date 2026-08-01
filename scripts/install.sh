#!/bin/sh
# Install memoria to $BIN_DIR (default ~/.local/bin), then run `memoria init`.
#   curl -sS https://raw.githubusercontent.com/johnvilela/memoria/main/scripts/install.sh | sh
# Inside the repo: builds and installs the local checkout.
# Requires Go. ponytail: swap the go-install path for a release-binary download once github releases exist.
set -eu

BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"

os=$(uname -s)
case "$os" in
    Linux|Darwin) ;;
    *) echo "error: unsupported OS: $os (linux and macos only)" >&2; exit 1 ;;
esac

command -v go >/dev/null 2>&1 || { echo "error: go is required (https://go.dev/dl)" >&2; exit 1; }

mkdir -p "$BIN_DIR"
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." 2>/dev/null && pwd)
if [ -f "$repo_root/go.mod" ]; then
    go build -C "$repo_root" -trimpath -o "$BIN_DIR/memoria" ./cmd/memoria
else
    GOBIN="$BIN_DIR" go install github.com/johnvilela/memoria/cmd/memoria@latest
fi
echo "installed $BIN_DIR/memoria"

# Detect installed agents for --client; memoria init adapts the rest to the
# environment itself: PATH repair for the user's shell rc, systemd timer on
# linux, launchd agent on macos.
clients=""
command -v claude >/dev/null 2>&1 && clients="claude-code"
command -v codex >/dev/null 2>&1 && clients="${clients:+$clients,}codex"

set -- init
[ -n "$clients" ] && set -- "$@" --client "$clients"
if [ -t 0 ]; then
    "$BIN_DIR/memoria" "$@"
elif (exec </dev/tty) 2>/dev/null; then
    # curl | sh: stdin is the script — borrow the terminal so init stays interactive
    "$BIN_DIR/memoria" "$@" </dev/tty
else
    echo "next: run 'memoria init' to install hooks, fix PATH and choose a processor"
fi
