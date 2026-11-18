#!/usr/bin/env bash
# test/m1a/bench.sh — the test/bench workloads on the new node package
# (`make bench-m1a`, `make pe-check-m1a`).
#
# Each benchmark of test/bench that needs no standard-library package for its
# workload is rewritten by bench.py (harness only: runtime.nanotime through
# go:linkname, println instead of fmt), built by gc and through graalgo-tool,
# and run both ways. The result line (stderr line 1) must be identical.
#
#   bench.sh [name...]             table: best-round ms under gc and graalgo
#                                  and the ratio (steady state)
#   bench.sh --pe-check [name...]  partial-evaluation gate: the graalgo leg
#                                  runs with engine.CompilationFailureAction=
#                                  ExitVM, synchronous compilation and
#                                  compilation tracing; fails on any failed
#                                  compilation, a wrong result or nothing
#                                  compiled; prints the bailout total (must
#                                  be 0)
# Default names: fib loop slicesum structs iface maps.
# Env: GO, GRAALVM_HOME, GRAALGO_TMP, GRAALGO_JAVA_OPTS (base JVM flags,
#      default -Xmx2g), BENCH_TIMEOUT (seconds per run, default 600).
# Exit: the infra.sh contract.
set -uo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/infra.sh
. "$HERE/../lib/infra.sh"
ROOT="$GRAALGO_ROOT"

pecheck=0
if [ "${1:-}" = "--pe-check" ]; then
    pecheck=1
    shift
fi
[ $# -gt 0 ] || set -- fib loop slicesum structs iface maps

infra_setup_go
GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
export GRAALVM_HOME
RUNNER="$ROOT/internal/backend/graalgo-bundle-run"
[ -f "$ROOT/internal/backend/truffle/out/classes/graalgo/launcher/BundleMain.class" ] \
    || infra_die "backend not built (BundleMain missing) — run \`make build\` at the repo root"
infra_mktemp m1a-bench
TIMEOUT="${BENCH_TIMEOUT:-600}"
BASE_OPTS="${GRAALGO_JAVA_OPTS:--Xmx2g}"

mkdir -p "$TMP/bin" "$TMP/gotmp"
export GOTMPDIR="$TMP/gotmp" GOFLAGS= CGO_ENABLED=0 GOTOOLCHAIN=local
STUB_ID="GRAALGO-STUB-BUILDID/GRAALGO-STUB-BUILDID/GRAALGO-STUB-BUILDID/GRAALGO-STUB-BUILDID"
( cd "$ROOT/internal/toolexec" \
    && "$GO" build -o "$TMP/bin/graalgo-tool" ./cmd/graalgo-tool \
    && "$GO" build -ldflags="-buildid=$STUB_ID" -o "$TMP/bin/graalgo-stub" ./cmd/graalgo-stub ) \
    || infra_die "cannot build graalgo-tool / graalgo-stub"

bench_ns() { sed -n "s/^BENCH $2 \([0-9][0-9]*\)\$/\1/p" "$1" | tail -1; }

fail=0
bailouts=0
if [ "$pecheck" -eq 1 ]; then
    OPTS="$BASE_OPTS -Dpolyglot.engine.TraceCompilation=true -Dpolyglot.engine.CompilationFailureAction=ExitVM -Dpolyglot.engine.BackgroundCompilation=false"
else
    OPTS="$BASE_OPTS"
    printf '%-10s %12s %12s %9s  %s\n' benchmark gc_ms graalgo_ms ratio result
fi
for name in "$@"; do
    src="$ROOT/test/bench/$name/main.go"
    [ -f "$src" ] || { echo "bench.sh: no such benchmark: $name" >&2; exit "$USAGE_EXIT"; }
    w="$TMP/$name"
    python3 "$HERE/bench.py" "$src" "$w/mod" || exit "$USAGE_EXIT"
    ( cd "$w/mod" && "$GO" build -o "$w/gc.bin" . ) || infra_die "gc build failed: $name"
    ( cd "$w/mod" && GRAALGO_STUB="$TMP/bin/graalgo-stub" GRAALGO_RUNNER="$RUNNER" \
        "$GO" build -toolexec "$TMP/bin/graalgo-tool" -o "$w/gg.bin" . ) || infra_die "graalgo-tool build failed: $name"
    "$w/gc.bin" >/dev/null 2>"$w/gc.err"
    status=0
    GRAALGO_JAVA_OPTS="$OPTS" TMPDIR="$TMP" nice -n 19 timeout "$TIMEOUT" "$w/gg.bin" >"$w/gg.out" 2>"$w/gg.err" || status=$?
    infra_check_run "$name" "$status" "$w/gg.err"
    want="$(grep -av '^BENCH ' "$w/gc.err" | head -1)"
    # The result is the first line that is a bare integer (the engine's trace
    # and log-redirection notes share stderr in --pe-check mode).
    got="$(grep -a -m1 -E '^-?[0-9]+$' "$w/gg.err")"
    gcns="$(bench_ns "$w/gc.err" "$name")"
    ggns="$(bench_ns "$w/gg.err" "$name")"
    verdict=ok
    if [ "$status" -ne 0 ]; then
        verdict="FAIL(exit $status)"
        grep -av '^\[engine\] opt \(done\|queued\|start\)' "$w/gg.err" | head -12 | cut -c1-300 >&2
    elif [ "$want" != "$got" ]; then
        verdict="FAIL(result '$got', gc '$want')"
    elif [ -z "$gcns" ] || [ -z "$ggns" ]; then
        verdict="FAIL(missing BENCH line)"
    fi
    if [ "$pecheck" -eq 1 ]; then
        failed="$(grep -ac '^\[engine\] opt failed' "$w/gg.err" || true)"
        done_="$(grep -ac '^\[engine\] opt done' "$w/gg.err" || true)"
        bailouts=$((bailouts + failed))
        if [ "$failed" -ne 0 ]; then
            verdict="FAIL($failed failed compilation(s))"
            grep -a '^\[engine\] opt failed' "$w/gg.err" | cut -c1-240 >&2
        elif [ "$verdict" = ok ] && [ "$done_" -eq 0 ]; then
            verdict="FAIL(nothing compiled)"
        fi
        printf 'pe-check-m1a %-10s compilations=%-5s %s\n' "$name" "$done_" "$verdict"
    elif [ -n "$gcns" ] && [ -n "$ggns" ]; then
        awk -v n="$name" -v a="$gcns" -v b="$ggns" -v v="$verdict" \
            'BEGIN { printf "%-10s %12.3f %12.3f %8.1fx  %s\n", n, a/1e6, b/1e6, b/a, v }'
    else
        printf '%-10s %12s %12s %9s  %s\n' "$name" "${gcns:--}" "${ggns:--}" - "$verdict"
    fi
    [ "$verdict" = ok ] || fail=1
done
[ "$pecheck" -eq 1 ] && echo "pe-check-m1a: $bailouts failed compilation(s)"
[ "$fail" -eq 0 ] || exit "$DIVERGENCE_EXIT"
exit 0
