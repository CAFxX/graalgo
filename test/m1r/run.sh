#!/usr/bin/env bash
# test/m1r/run.sh — differential gate of the standard library through the
# -toolexec path (`make test-m1r`; toolexec milestones M1r/M1b, work packages
# W5 and W6 of docs/arch/toolexec-design.md): programs that import standard-
# library packages, built by the stock `go build` through graalgo-tool with
# graalgo's fork applied (stdlib/: the facade runtime replaces upstream's,
# every other package is upstream's source, compiled as it is), run on the
# node package (graalgo.go) via internal/backend/graalgo-bundle-run, against
# the same program built and run by gc.
#
# Nothing of the standard library is reimplemented for these programs: a
# function executes upstream's Go body, or — when upstream has none (assembly,
# runtime-provided) or gc's compiler replaces it — the Java intrinsic the
# registry (internal/toolexec/intrinsics/registry.txt) names for its symbol.
# The exceptions are the registry's TEMPORARY entries (BUG-0037:
# reflectlite.Swapper and pkgPath), each tied to an open bug.
#
# Compared per program: stdout (bytes), stderr (normalized by
# test/corpus/lifecycle/normalize.py: goroutine ids, frame lines, hex
# addresses) and the exit status. Programs report with println.
#
# Both legs run with GODEBUG=cpu.all=off: under graalgo, internal/cpu reports
# no optional CPU feature (docs/arch/limitations.md), so the reference is gc
# on the same feature set — it matters where upstream's assembly has an FMA
# path (math.Exp).
#
# Programs: test/m1r/programs/*.go, one `package main` file each.
#
# Usage: test/m1r/run.sh [-v] [PROGRAM.go]...
#   -v   print the diff of every failing program
# Env:   GO, GRAALVM_HOME, GRAALGO_TMP (see test/lib/infra.sh),
#        GRAALGO_JAVA_OPTS (JVM flags of the graalgo leg),
#        M1R_TIMEOUT (seconds per run, default 120),
#        M1R_REPORT (file: one "PASS|FAIL <program> [reason]" line per program)
# Exit:  the infra.sh contract (0 pass, 1 divergence, 2 usage, 3 infra).
set -uo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/infra.sh
. "$HERE/../lib/infra.sh"
# shellcheck source=../lib/toolexec.sh
. "$HERE/../lib/toolexec.sh"
ROOT="$GRAALGO_ROOT"

verbose=0
while getopts v opt; do
    case "$opt" in
        v) verbose=1 ;;
        *) echo "usage: $0 [-v] [PROGRAM.go]..." >&2; exit "$USAGE_EXIT" ;;
    esac
done
shift $((OPTIND - 1))

infra_setup_go
GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
export GRAALVM_HOME
RUNNER="$ROOT/internal/backend/graalgo-bundle-run"
[ -x "$RUNNER" ] || infra_die "runner not found: $RUNNER"
[ -f "$ROOT/internal/backend/truffle/out/classes/graalgo/launcher/BundleMain.class" ] \
    || infra_die "backend not built (BundleMain missing) — run \`make build\` at the repo root"
command -v python3 >/dev/null || infra_die "python3 not found"
infra_mktemp m1r
TIMEOUT="${M1R_TIMEOUT:-120}"
NORMALIZE="$ROOT/test/corpus/lifecycle/normalize.py"
toolexec_setup
export GODEBUG=cpu.all=off

if [ $# -eq 0 ]; then
    mapfile -t programs < <(cd "$ROOT" && ls test/m1r/programs/*.go | sort)
else
    programs=("$@")
fi

run_leg() { # <exe> <stdout> <stderr> -> exit status
    local status=0
    ( cd "$(dirname "$1")" && timeout "$TIMEOUT" "$1" >"$2" 2>"$3" </dev/null ) || status=$?
    return "$status"
}

pass=0
fail=0
report="${M1R_REPORT:-}"
[ -n "$report" ] && : > "$report"
n=0
for prog in "${programs[@]}"; do
    n=$((n + 1))
    case "$prog" in /*) src="$prog" ;; *) src="$ROOT/$prog" ;; esac
    [ -f "$src" ] || { echo "run.sh: no such program: $prog" >&2; exit "$USAGE_EXIT"; }
    w="$TMP/w/$n"
    mkdir -p "$w/mod"
    cp "$src" "$w/mod/main.go"
    printf 'module m1r/p%d\n\ngo 1.27\n' "$n" > "$w/mod/go.mod"
    reason=""
    if ! ( cd "$w/mod" && "$GO" build -o "$w/gc.bin" . ) >"$w/gc.build" 2>&1; then
        reason="gc rejects the program (corpus bug)"
    elif ! ( cd "$w/mod" && toolexec_build "$w/gg.bin" "$RUNNER" ) >"$w/gg.build" 2>&1; then
        reason="graalgo-tool build failed: $(grep -v '^#' "$w/gg.build" | head -1)"
    else
        gc_status=0
        run_leg "$w/gc.bin" "$w/gc.out" "$w/gc.err" || gc_status=$?
        gg_status=0
        TMPDIR="$TMP" run_leg "$w/gg.bin" "$w/gg.out" "$w/gg.err" || gg_status=$?
        infra_check_run "$prog" "$gg_status" "$w/gg.err"
        python3 "$NORMALIZE" < "$w/gc.err" > "$w/gc.nerr"
        grep -av '^WARNING: \|^NOTE: Picked up' "$w/gg.err" | python3 "$NORMALIZE" > "$w/gg.nerr"
        if [ "$gc_status" -ne "$gg_status" ]; then
            reason="exit status $gg_status, gc $gc_status"
        elif ! cmp -s "$w/gc.out" "$w/gg.out"; then
            reason="stdout differs"
        elif ! cmp -s "$w/gc.nerr" "$w/gg.nerr"; then
            reason="stderr differs"
        elif [ ! -s "$w/gc.nerr" ] && [ ! -s "$w/gc.out" ]; then
            reason="the program prints nothing (corpus bug: nothing was compared)"
        fi
    fi
    if [ -z "$reason" ]; then
        pass=$((pass + 1))
        [ -n "$report" ] && echo "PASS $prog" >> "$report"
    else
        fail=$((fail + 1))
        echo "FAIL $prog: $reason"
        [ -n "$report" ] && echo "FAIL $prog: $reason" >> "$report"
        if [ "$verbose" -eq 1 ] && [ -f "$w/gg.nerr" ]; then
            diff "$w/gc.out" "$w/gg.out" | head -10 | sed 's/^/    out | /'
            diff "$w/gc.nerr" "$w/gg.nerr" | head -14 | sed 's/^/    err | /'
        fi
    fi
    rm -rf "$w"
done

echo "m1r: $pass passed, $fail failed of $((pass + fail))"
[ "$fail" -eq 0 ] || exit "$DIVERGENCE_EXIT"
exit 0
