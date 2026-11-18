#!/usr/bin/env bash
# test/m1a/run.sh — differential gate of toolexec milestone M1a (`make test-m1a`):
# the corpus programs that import nothing, built by the stock `go build`
# through graalgo-tool (-toolexec: shadow compile, GIR, bundle link) and run
# on the new node package (graalgo.go) via internal/backend/graalgo-bundle-run,
# against the same program built and run by gc. Compared per program: stdout
# (bytes), stderr (normalized by test/corpus/lifecycle/normalize.py: goroutine
# ids, frame lines and hex addresses — stack-trace fidelity is not this gate's
# subject), and the exit status.
#
# Programs: every test/corpus/**/*.go without an import (the M1a set of
# docs/arch/toolexec-design.md §8), made observable by transform.py, the
# programs of test/m1a/programs/ (language areas the corpus is thin on: open-coded
# and chained defers, the flat value model, interfaces, generics, goroutines),
# the multi-package modules under test/m1a/multipkg/ (one directory = one module;
# its root package is main), and the programs of the reference GIR interpreter
# (internal/toolexec/girinterp/testdata/progs).
#
# Usage: test/m1a/run.sh [-v] [PROGRAM.go | MODULE_DIR]...
#   -v   print the diff of every failing program
# Env:   GO, GRAALVM_HOME, GRAALGO_TMP (see test/lib/infra.sh),
#        GRAALGO_JAVA_OPTS (JVM flags of the graalgo leg),
#        M1A_TIMEOUT (seconds per run, default 60),
#        M1A_REPORT (file: one "PASS|FAIL <program> [reason]" line per program)
# Exit:  the infra.sh contract (0 pass, 1 divergence, 2 usage, 3 infra).
set -uo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/infra.sh
. "$HERE/../lib/infra.sh"
ROOT="$GRAALGO_ROOT"

verbose=0
while getopts v opt; do
    case "$opt" in
        v) verbose=1 ;;
        *) echo "usage: $0 [-v] [PROGRAM.go | MODULE_DIR]..." >&2; exit "$USAGE_EXIT" ;;
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
infra_mktemp m1a
TIMEOUT="${M1A_TIMEOUT:-60}"
NORMALIZE="$ROOT/test/corpus/lifecycle/normalize.py"

# The toolexec tools, built by the tracked gc toolchain, and the
# standard-library fork they build against (test/lib/toolexec.sh).
# shellcheck source=../lib/toolexec.sh
. "$HERE/../lib/toolexec.sh"
toolexec_setup

if [ $# -eq 0 ]; then
    mapfile -t programs < <(cd "$ROOT" && grep -rLE '^[[:space:]]*import' --include='*.go' test/corpus test/m1a/programs | sort)
    for d in "$HERE"/multipkg/*/; do
        [ -f "$d/go.mod" ] && programs+=("test/m1a/multipkg/$(basename "$d")")
    done
    # The emitter lane's programs (the reference GIR interpreter's own set).
    INTERP="internal/toolexec/girinterp/testdata/progs"
    for f in "$ROOT/$INTERP"/*; do
        if [ -d "$f" ] && [ -f "$f/go.mod" ]; then
            programs+=("$INTERP/$(basename "$f")")
        elif [ -f "$f" ] && [ "${f%.go}" != "$f" ] && ! grep -qE '^[[:space:]]*import' "$f"; then
            programs+=("$INTERP/$(basename "$f")")
        fi
    done
else
    programs=("$@")
fi

pass=0
fail=0
declare -A failed_by_category
report="${M1A_REPORT:-}"
[ -n "$report" ] && : > "$report"

run_leg() { # <exe> <stdout> <stderr> -> exit status
    local status=0
    ( cd "$(dirname "$1")" && timeout "$TIMEOUT" "$1" >"$2" 2>"$3" </dev/null ) || status=$?
    return "$status"
}

n=0
for prog in "${programs[@]}"; do
    n=$((n + 1))
    case "$prog" in /*) src="$prog" ;; *) src="$ROOT/$prog" ;; esac
    w="$TMP/w/$n"
    mkdir -p "$w/mod"
    if [ -d "$src" ]; then
        cp -r "$src/." "$w/mod/"
        category="multipkg"
    elif [ -f "$src" ]; then
        python3 "$HERE/transform.py" "$src" "$w/mod/main.go" || infra_die "transform failed: $prog"
        printf 'module m1a/p%d\n\ngo 1.27\n' "$n" > "$w/mod/go.mod"
        category="$(basename "$(dirname "$src")")"
    else
        echo "run.sh: no such program: $prog" >&2
        exit "$USAGE_EXIT"
    fi
    reason=""
    if ! ( cd "$w/mod" && "$GO" build -o "$w/gc.bin" . ) >"$w/gc.build" 2>&1; then
        reason="gc rejects the program (corpus bug)"
    elif ! ( cd "$w/mod" && toolexec_build "$w/gg.bin" "$RUNNER" ) >"$w/gg.build" 2>&1; then
        reason="graalgo-tool build failed: $(head -1 "$w/gg.build")"
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
        fi
    fi
    if [ -z "$reason" ]; then
        pass=$((pass + 1))
        [ -n "$report" ] && echo "PASS $prog" >> "$report"
    else
        fail=$((fail + 1))
        failed_by_category[$category]=$(( ${failed_by_category[$category]:-0} + 1 ))
        echo "FAIL $prog: $reason"
        [ -n "$report" ] && echo "FAIL $prog: $reason" >> "$report"
        if [ "$verbose" -eq 1 ] && [ -f "$w/gg.nerr" ]; then
            diff "$w/gc.out" "$w/gg.out" | head -10 | sed 's/^/    out | /'
            diff "$w/gc.nerr" "$w/gg.nerr" | head -14 | sed 's/^/    err | /'
        fi
    fi
    rm -rf "$w"
done

echo "m1a: $pass passed, $fail failed of $((pass + fail))"
for c in "${!failed_by_category[@]}"; do
    echo "m1a: failing category $c: ${failed_by_category[$c]}"
done | sort
[ "$fail" -eq 0 ] || exit "$DIVERGENCE_EXIT"
exit 0
