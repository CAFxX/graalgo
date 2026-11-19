#!/usr/bin/env bash
# test/bench/suite.sh — the benchmark suite (`make bench-suite`): the programs
# of test/bench/suite/ under gc (optionally gc+PGO) and graalgo (graalgo-tool,
# node package), results checked against gc, warmup curves and steady-state
# ratios. Method and current numbers: docs/arch/performance.md.
#
# Usage: suite.sh [--reps K] [--pgo] [--trace] [--json FILE] [--no-graalgo] [name...]
#        (all options: test/bench/suite.py --help)
# Env:   GO, GRAALVM_HOME, GRAALGO_TMP, GRAALGO_JAVA_OPTS (default
#        -Xmx2g -XX:ActiveProcessorCount=4), BENCH_TIMEOUT (seconds per run).
# Exit:  the infra.sh contract (divergence or crash: DIVERGENCE_EXIT).
set -uo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/infra.sh
. "$HERE/../lib/infra.sh"
ROOT="$GRAALGO_ROOT"

infra_setup_go
GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
export GRAALVM_HOME
[ -f "$ROOT/internal/backend/truffle/out/classes/graalgo/launcher/BundleMain.class" ] \
    || infra_die "backend not built (BundleMain missing) — run \`make build\` at the repo root"
infra_mktemp bench-suite
# shellcheck source=../lib/toolexec.sh
. "$HERE/../lib/toolexec.sh"
toolexec_setup
export GO TMP TOOLEXEC_TOOL TOOLEXEC_STUB TOOLEXEC_OVERLAY
export GRAALGO_RUNNER="$ROOT/internal/backend/graalgo-bundle-run"
export GRAALGO_JAVA_OPTS="${GRAALGO_JAVA_OPTS:--Xmx2g -XX:ActiveProcessorCount=4}"
python3 "$HERE/suite.py" "$@" || exit "$DIVERGENCE_EXIT"
