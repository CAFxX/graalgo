#!/usr/bin/env bash
# diffharness with the new path's toolchain (m3-plan D1):
#   newpath.sh <diffharness args...>
# builds graalgo-tool, the stub and the standard-library fork overlay once
# (test/lib/toolexec.sh's toolexec_setup, with the runner JVMs'
# class-data-sharing archive) and runs `diffharness <args>` in this
# directory; backends-newpath.json's graalgo backend builds each program
# with newpath_build.sh and runs the built programs in one JVM
# (graalgo-bundle-run --batch; GRAALGO_BATCH=off: one JVM per program).
# Exit status: diffharness's (3 = infrastructure).
set -euo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/infra.sh
. "$HERE/../lib/infra.sh"
# shellcheck source=../lib/toolexec.sh
. "$HERE/../lib/toolexec.sh"
infra_setup_go
GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
export GRAALVM_HOME GRAALGO_ROOT
[ -x "$GRAALGO_ROOT/internal/backend/graalgo-bundle-run" ] || infra_die "runner not found: internal/backend/graalgo-bundle-run"
[ -f "$GRAALGO_ROOT/internal/backend/truffle/out/classes/graalgo/launcher/BundleBatchMain.class" ] \
    || infra_die "backend not built (BundleBatchMain missing) — run \`make build\` at the repo root"
infra_mktemp dh-newpath
cgo="${CGO_ENABLED-}"
toolexec_setup
export TOOLEXEC_TOOL TOOLEXEC_STUB TOOLEXEC_OVERLAY
# toolexec_setup's build settings are the graalgo leg's (newpath_build.sh
# sets them again): the gc reference builds as `make snapshot` recorded it.
if [ -n "$cgo" ]; then export CGO_ENABLED="$cgo"; else unset CGO_ENABLED; fi
cd "$HERE"
"$GO" build -o "$TMP/bin/diffharness" ./cmd/diffharness || infra_die "cannot build diffharness"
st=0
"$TMP/bin/diffharness" "$@" || st=$?
exit "$st"
