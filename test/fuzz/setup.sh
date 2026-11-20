#!/usr/bin/env bash
# test/fuzz/setup.sh <dir> — build the -toolexec toolchain (graalgo-tool,
# graalgo-stub, the stdlib-fork overlay; test/lib/toolexec.sh) into <dir>
# for test/fuzz/run.py, and print the environment the runner builds with
# as KEY=VALUE lines. <dir> is not removed (the runner owns it).
set -uo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/infra.sh
. "$HERE/../lib/infra.sh"
# shellcheck source=../lib/toolexec.sh
. "$HERE/../lib/toolexec.sh"
[ $# -eq 1 ] || { echo "usage: $0 <dir>" >&2; exit "$USAGE_EXIT"; }
infra_setup_go
TMP="$1"
mkdir -p "$TMP"
toolexec_setup >&2
echo "GO=$GO"
echo "TOOLEXEC_TOOL=$TOOLEXEC_TOOL"
echo "TOOLEXEC_STUB=$TOOLEXEC_STUB"
echo "TOOLEXEC_OVERLAY=$TOOLEXEC_OVERLAY"
echo "GOTMPDIR=$GOTMPDIR"
echo "RUNNER=$GRAALGO_ROOT/internal/backend/graalgo-bundle-run"
echo "NORMALIZE=$GRAALGO_ROOT/test/corpus/lifecycle/normalize.py"
