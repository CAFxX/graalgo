#!/usr/bin/env bash
# The graalgo backend's build step in backends-newpath.json:
#   newpath_build.sh <bin> <src>
# builds <src> as a command-line-arguments package (what gc's
# `go build -o <bin> <src>` builds, so traceback file paths are the
# source's own) through graalgo-tool with the standard-library fork applied
# (test/lib/toolexec.sh's toolexec_build_files), linked to run on
# graalgo-bundle-run. Needs the toolchain newpath.sh sets up (TOOLEXEC_*).
set -euo pipefail
bin=$1 src=$2
: "${TOOLEXEC_TOOL:?run through test/differential/newpath.sh}"
# shellcheck source=../lib/toolexec.sh
. "$GRAALGO_ROOT/test/lib/toolexec.sh"
CGO_ENABLED=0 GOFLAGS= toolexec_build_files "$bin" "$GRAALGO_ROOT/internal/backend/graalgo-bundle-run" "$src"
