#!/bin/sh
# Dev loop: build the local checkout and install it to $BIN_DIR.
# ponytail: install.sh already builds in-repo; this just names the workflow.
set -eu
exec "$(dirname "$0")/install.sh"
