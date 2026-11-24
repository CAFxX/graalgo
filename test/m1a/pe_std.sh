#!/usr/bin/env bash
# test/m1a/pe_std.sh — the partial-evaluation gate of the standard-library
# paths (`make pe-check-m1a`, after the test/bench workloads): test/m1a/pe/
# stdhot.go runs the json, regexp, flate, template, strings/maps/sort paths of
# test/bench/realistic hot under engine.CompilationFailureAction=ExitVM with
# synchronous compilation; any failed compilation (a function left in the
# interpreter) or a result other than gc's fails the gate. Then the planted
# violation: a hot generated function compiled under a graph-size limit it
# exceeds must make the same check fail (the gate can fail).
#
# Usage: pe_std.sh
# Env:   GO, GRAALVM_HOME, GRAALGO_TMP, GRAALGO_JAVA_OPTS (default -Xmx2g
#        -XX:ActiveProcessorCount=4), BENCH_TIMEOUT (seconds per run, 300).
# Exit:  the infra.sh contract.
set -uo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/infra.sh
. "$HERE/../lib/infra.sh"
ROOT="$GRAALGO_ROOT"

infra_setup_go
GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
export GRAALVM_HOME
RUNNER="$ROOT/internal/backend/graalgo-bundle-run"
[ -f "$ROOT/internal/backend/truffle/out/classes/graalgo/launcher/BundleMain.class" ] \
    || infra_die "backend not built (BundleMain missing) — run \`make build\` at the repo root"
infra_mktemp pe-std
# shellcheck source=../lib/toolexec.sh
. "$HERE/../lib/toolexec.sh"
toolexec_setup
TIMEOUT="${BENCH_TIMEOUT:-300}"
OPTS="${GRAALGO_JAVA_OPTS:--Xmx2g -XX:ActiveProcessorCount=4} -Dpolyglot.engine.TraceCompilation=true -Dpolyglot.engine.CompilationFailureAction=ExitVM -Dpolyglot.engine.BackgroundCompilation=false"

# run <name> <dir>: builds <dir> with gc and graalgo-tool, runs both; sets
# verdict (ok, or why not) and failed (the failed compilations).
run() {
    local name="$1" d="$2" status=0 want got
    ( cd "$d" && "$GO" build -o "$d/gc.bin" . ) || infra_die "gc build failed: $name"
    ( cd "$d" && toolexec_build "$d/gg.bin" "$RUNNER" ) || infra_die "graalgo-tool build failed: $name"
    "$d/gc.bin" >/dev/null 2>"$d/gc.err" || infra_die "gc run failed: $name"
    # In its own directory: a failed compilation under ExitVM writes graal_dumps/ to the cwd.
    ( cd "$d" && GRAALGO_JAVA_OPTS="$OPTS" TMPDIR="$TMP" nice -n 19 timeout "$TIMEOUT" "$d/gg.bin" >"$d/gg.out" 2>"$d/gg.err" ) || status=$?
    infra_check_run "$name" "$status" "$d/gg.err"
    want="$(head -1 "$d/gc.err")"
    got="$(grep -a -m1 -E '^-?[0-9]+$' "$d/gg.err")"
    failed="$(grep -ac '^\[engine\] opt failed' "$d/gg.err" || true)"
    local done_
    done_="$(grep -ac '^\[engine\] opt done' "$d/gg.err" || true)"
    verdict=ok
    if [ "$failed" -ne 0 ]; then
        verdict="FAIL($failed failed compilation(s))"
    elif [ "$status" -ne 0 ]; then
        verdict="FAIL(exit $status)"
    elif [ "$want" != "$got" ]; then
        verdict="FAIL(result '$got', gc '$want')"
    elif [ "$done_" -eq 0 ]; then
        verdict="FAIL(nothing compiled)"
    fi
    printf 'pe-check-std %-8s compilations=%-5s %s\n' "$name" "$done_" "$verdict"
}

module() {
    mkdir -p "$1"
    printf 'module pestd\n\ngo 1.27\n' >"$1/go.mod"
}

fail=0
d="$TMP/stdhot"
module "$d"
cp "$HERE/pe/stdhot.go" "$d/main.go"
run stdhot "$d"
if [ "$verdict" != ok ]; then
    grep -a '^\[engine\] opt failed' "$d/gg.err" | cut -c1-300 >&2
    fail=1
fi

# The planted violation: a hot function whose compiled graph exceeds the
# engine's graph-size limit, lowered for this run only (Go source cannot plant
# a failure on its own once every shape compiles; the IR caps nesting below
# partial evaluation's inlining depth). It proves the detection: a failed
# compilation under ExitVM must fail the gate.
d="$TMP/plant"
module "$d"
{
    printf 'package main\n\n//go:noinline\nfunc deep(x int) int {\n\treturn x'
    i=0
    while [ $i -lt 200 ]; do
        if [ $((i % 2)) -eq 0 ]; then printf ' ^ %d' $((i % 7)); else printf ' + x'; fi
        i=$((i + 1))
    done
    printf '\n}\n\nfunc main() {\n\ts := 0\n\tfor i := 0; i < 20000; i++ {\n\t\ts += deep(i)\n\t}\n\tprintln(s)\n}\n'
} >"$d/main.go"
OPTS="$OPTS -Dpolyglot.compiler.MaximumGraalGraphSize=100"
run plant "$d"
if [ "$failed" -eq 0 ]; then
    echo "pe-check-std: the planted violation (plant) did not fail a compilation — the gate cannot fail" >&2
    fail=1
fi

[ "$fail" -eq 0 ] || exit "$DIVERGENCE_EXIT"
echo "pe-check-std: ok (stdhot compiled without failures; the plant failed as it must)"
exit 0
