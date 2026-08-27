#!/bin/sh
# Install memoria to $BIN_DIR (default ~/.local/bin), then run `memoria init`.
#   curl -sS https://raw.githubusercontent.com/johnvilela/memoria/main/scripts/install.sh | sh
# Inside the repo: builds and installs the local checkout (requires Go).
# Standalone: downloads the latest release binary, checksum-verified.
set -eu

BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"

case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) echo "error: unsupported OS: $(uname -s) (linux and macos only)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) echo "error: unsupported architecture: $(uname -m) (amd64 and arm64 only)" >&2; exit 1 ;;
esac

mkdir -p "$BIN_DIR"
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." 2>/dev/null && pwd)
if [ -f "$repo_root/go.mod" ]; then
    command -v go >/dev/null 2>&1 || { echo "error: go is required (https://go.dev/dl)" >&2; exit 1; }
    go build -C "$repo_root" -trimpath -o "$BIN_DIR/memoria" ./cmd/memoria
else
    command -v sha256sum >/dev/null 2>&1 || sha256sum() { shasum -a 256 "$@"; }
    url="https://github.com/johnvilela/memoria/releases/latest/download"
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    curl -fsSL -o "$tmp/memoria_${os}_${arch}" "$url/memoria_${os}_${arch}"
    curl -fsSL -o "$tmp/checksums.txt" "$url/checksums.txt"
    (cd "$tmp" && grep " memoria_${os}_${arch}\$" checksums.txt | sha256sum -c - >/dev/null)
    install -m 755 "$tmp/memoria_${os}_${arch}" "$BIN_DIR/memoria"
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
