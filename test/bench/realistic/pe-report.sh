#!/usr/bin/env bash
# test/bench/realistic/pe-report.sh — the compilation report of the realistic
# programs (`make pe-report`, not a gate): each program is built through
# graalgo-tool and run once with Truffle compilation tracing; the report
# lists, per program, the compilations done, failed, deoptimized and
# invalidated, then every failed compilation (root, reason) and the roots
# deoptimized or invalidated most often. A failed compilation leaves its
# function in the AST interpreter for the rest of the run, so the target is
# zero failures (docs/arch/performance.md §8).
#
# Usage: pe-report.sh [--sync] [--keep DIR] [name...]
#   --sync      BackgroundCompilation=false (deterministic; slower)
#   --keep DIR  keep the binaries and the raw traces in DIR
# Default names: every program of test/bench/realistic except goroutines.
# Env: GO, GRAALVM_HOME, GRAALGO_TMP, GRAALGO_JAVA_OPTS (default -Xmx2g
#      -XX:ActiveProcessorCount=4), BENCH_TIMEOUT (seconds per run, default
#      600), PE_REPORT_OPTS (more JVM flags, e.g. engine.TraceCompilationDetails).
# Exit: the infra.sh contract; DIVERGENCE_EXIT when a run fails or any
#       compilation failed.
set -uo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../lib/infra.sh
. "$HERE/../../lib/infra.sh"
ROOT="$GRAALGO_ROOT"

sync=0
keep=
while [ $# -gt 0 ]; do
    case "$1" in
        --sync) sync=1; shift ;;
        --keep) keep="$2"; shift 2 ;;
        -*) echo "pe-report.sh: unknown option $1" >&2; exit "$USAGE_EXIT" ;;
        *) break ;;
    esac
done
[ $# -gt 0 ] || set -- jsonrt regexscan textproc tmplrender reqpipe deflate allocmix

infra_setup_go
GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
export GRAALVM_HOME
RUNNER="$ROOT/internal/backend/graalgo-bundle-run"
[ -f "$ROOT/internal/backend/truffle/out/classes/graalgo/launcher/BundleMain.class" ] \
    || infra_die "backend not built (BundleMain missing) — run \`make build\` at the repo root"
infra_mktemp pe-report
# shellcheck source=../../lib/toolexec.sh
. "$HERE/../../lib/toolexec.sh"
toolexec_setup
out="${keep:-$TMP/out}"
mkdir -p "$out"

OPTS="${GRAALGO_JAVA_OPTS:--Xmx2g -XX:ActiveProcessorCount=4} -Dpolyglot.engine.TraceCompilation=true -Dpolyglot.engine.TraceDeoptimizeFrame=false ${PE_REPORT_OPTS:-}"
[ "$sync" -eq 1 ] && OPTS="$OPTS -Dpolyglot.engine.BackgroundCompilation=false"

fail=0
for name in "$@"; do
    [ -f "$HERE/$name/main.go" ] || { echo "pe-report.sh: no such program: $name" >&2; exit "$USAGE_EXIT"; }
    ( cd "$HERE" && GRAALGO_STUB="$TOOLEXEC_STUB" GRAALGO_RUNNER="$RUNNER" \
        "$GO" build -overlay "$TOOLEXEC_OVERLAY" -toolexec "$TOOLEXEC_TOOL" -o "$out/$name.gg" "./$name" ) \
        || infra_die "graalgo-tool build failed: $name"
    status=0
    GRAALGO_JAVA_OPTS="$OPTS" TMPDIR="$TMP" nice -n 19 timeout "${BENCH_TIMEOUT:-600}" \
        "$out/$name.gg" >"$out/$name.out" 2>"$out/$name.trace" || status=$?
    [ "$status" -eq 0 ] || { echo "pe-report.sh: $name exited $status" >&2; fail=1; }
    python3 "$HERE/pe_report.py" "$name" "$out/$name.trace" || fail=1
done
[ "$fail" -eq 0 ] || exit "$DIVERGENCE_EXIT"
exit 0
