#!/bin/sh
# Run the test suite. Extra args go to `go test`, e.g.:
#   scripts/test.sh -v -run TestCaptureHook
#   scripts/test.sh -cover
set -eu
cd "$(dirname "$0")/.."

go vet ./...
go test -race "$@" ./...
