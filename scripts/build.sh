#!/bin/sh
# build.sh          -> ./memoria for the host platform
# build.sh all      -> dist/memoria_<os>_<arch> for release platforms
set -eu
cd "$(dirname "$0")/.."

if [ "${1:-}" = "all" ]; then
    mkdir -p dist
    # ponytail: windows/* excluded — flock.go/process.go/status.go use unix syscalls; add targets after a windows port
    for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
        os=${target%/*} arch=${target#*/}
        out="dist/memoria_${os}_${arch}"
        GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -o "$out" ./cmd/memoria
        echo "built $out"
    done
else
    go build -trimpath -o memoria ./cmd/memoria
    echo "built ./memoria"
fi
