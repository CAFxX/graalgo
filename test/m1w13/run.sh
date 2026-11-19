#!/usr/bin/env bash
# test/m1w13/run.sh — the pointer-model gate (`make test-m1w13`; toolexec work
# package W13, SPEC §3.4, GG-0061/GG-0062, ADR-0015): programs that use
# package unsafe, built by the stock `go build` through graalgo-tool with
# graalgo's fork applied and run on the node package, against gc.
#
#   layout      test/m1w13/layout_corpus.txt, the fixture of the backend's
#               leaf-table unit test, is still what gc prints for the program
#               `gen.py layout` generates (a stale fixture would let the unit
#               test pass against yesterday's corpus).
#   access      the generated program `gen.py access --types 60`: 1/2/4/8-byte
#               scalar loads and stores and pointer-word loads and stores at
#               every byte offset of every corpus type, per addressable kind
#               (escaping variable, package variable, struct field, array
#               element, slice element). Differential vs gc.
#   programs/   unsafe idioms of real code. Differential vs gc.
#   checkptr/   programs that gc's own -d=checkptr instrumentation stops:
#               differential vs gc built with -gcflags=all=-d=checkptr (same
#               fatal-error text, exit status 2).
#   restricted/ programs gc runs but the flat value model cannot (SPEC §3.4:
#               "never silent"; docs/arch/limitations.md): the first stderr
#               line after the program's own output must be the fatal error
#               of the .want file, the exit status 2 — and gc must still
#               accept the program, or it belongs in checkptr/.
#
# Compared per program: stdout (bytes), stderr (normalized by
# test/corpus/lifecycle/normalize.py) and the exit status.
#
# Usage: test/m1w13/run.sh [-v] [PROGRAM.go]...   (a path under programs/,
#        checkptr/ or restricted/; without arguments: everything)
# Env:   GO, GRAALVM_HOME, GRAALGO_TMP (see test/lib/infra.sh),
#        GRAALGO_JAVA_OPTS, M1W13_TIMEOUT (seconds per run, default 120)
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
infra_mktemp m1w13
TIMEOUT="${M1W13_TIMEOUT:-120}"
NORMALIZE="$ROOT/test/corpus/lifecycle/normalize.py"
toolexec_setup
export GODEBUG=cpu.all=off

pass=0
fail=0
n=0

failed() { # <name> <reason>
    fail=$((fail + 1))
    echo "FAIL $1: $2"
}

run_leg() { # <exe> <stdout> <stderr> -> exit status
    local status=0
    ( cd "$(dirname "$1")" && timeout "$TIMEOUT" "$1" >"$2" 2>"$3" </dev/null ) || status=$?
    return "$status"
}

# one <name> <source.go> <mode>: mode gc | checkptr | restricted
one() {
    local name="$1" src="$2" mode="$3" w reason="" gc_status=0 gg_status=0 gcflags=()
    n=$((n + 1))
    w="$TMP/w/$n"
    mkdir -p "$w/mod"
    cp "$src" "$w/mod/main.go"
    printf 'module m1w13/p%d\n\ngo 1.27\n' "$n" > "$w/mod/go.mod"
    [ "$mode" = checkptr ] && gcflags=(-gcflags=all=-d=checkptr)
    if ! ( cd "$w/mod" && "$GO" build "${gcflags[@]}" -o "$w/gc.bin" . ) >"$w/gc.build" 2>&1; then
        reason="gc rejects the program (corpus bug)"
    elif ! ( cd "$w/mod" && toolexec_build "$w/gg.bin" "$RUNNER" ) >"$w/gg.build" 2>&1; then
        reason="graalgo-tool build failed: $(grep -v '^#' "$w/gg.build" | head -1)"
    else
        run_leg "$w/gc.bin" "$w/gc.out" "$w/gc.err" || gc_status=$?
        TMPDIR="$TMP" run_leg "$w/gg.bin" "$w/gg.out" "$w/gg.err" || gg_status=$?
        infra_check_run "$name" "$gg_status" "$w/gg.err"
        python3 "$NORMALIZE" < "$w/gc.err" > "$w/gc.nerr"
        grep -av '^WARNING: \|^NOTE: Picked up' "$w/gg.err" | python3 "$NORMALIZE" > "$w/gg.nerr"
        if [ "$mode" = restricted ]; then
            # gc runs it; graalgo prints the same output up to the fatal error of the .want file.
            local want="${src%.go}.want"
            if [ "$gc_status" -ne 0 ]; then
                reason="gc exits $gc_status: the program belongs in checkptr/, not restricted/"
            elif [ "$gg_status" -ne 2 ]; then
                reason="exit status $gg_status, want 2 (a fatal error)"
            elif ! reason="$(python3 - "$want" "$w/gg.nerr" "$w/gc.nerr" <<'PY'
import sys
want = open(sys.argv[1]).read().strip()
gg = open(sys.argv[2]).read().splitlines()
gc = open(sys.argv[3]).read().splitlines()
if want not in gg:
    print("stderr lacks the fatal error: " + want)
    sys.exit(1)
before = gg[:gg.index(want)]
if before != gc[:len(before)] or not before:
    print("the output before the fatal error is not the start of gc's output")
    sys.exit(1)
PY
            )"; then
                :
            fi
        elif [ "$gc_status" -ne "$gg_status" ]; then
            reason="exit status $gg_status, gc $gc_status"
        elif ! cmp -s "$w/gc.out" "$w/gg.out"; then
            reason="stdout differs"
        elif ! cmp -s "$w/gc.nerr" "$w/gg.nerr"; then
            reason="stderr differs"
        elif [ ! -s "$w/gc.nerr" ] && [ ! -s "$w/gc.out" ]; then
            reason="the program prints nothing (corpus bug: nothing was compared)"
        elif [ "$mode" = checkptr ] && { [ "$gc_status" -ne 2 ] || ! grep -q '^fatal error: checkptr: ' "$w/gc.nerr"; }; then
            reason="gc -d=checkptr does not stop the program (corpus bug: not a checkptr case)"
        fi
    fi
    if [ -z "$reason" ]; then
        pass=$((pass + 1))
    else
        failed "$name" "$reason"
        if [ "$verbose" -eq 1 ] && [ -f "$w/gg.nerr" ]; then
            diff "$w/gc.out" "$w/gg.out" | head -10 | sed 's/^/    out | /'
            diff "$w/gc.nerr" "$w/gg.nerr" | head -14 | sed 's/^/    err | /'
        fi
    fi
    rm -rf "$w"
}

mode_of() {
    case "$1" in
        */checkptr/*) echo checkptr ;;
        */restricted/*) echo restricted ;;
        *) echo gc ;;
    esac
}

if [ $# -gt 0 ]; then
    for prog in "$@"; do
        case "$prog" in /*) src="$prog" ;; *) src="$ROOT/$prog" ;; esac
        [ -f "$src" ] || { echo "run.sh: no such program: $prog" >&2; exit "$USAGE_EXIT"; }
        one "$prog" "$src" "$(mode_of "$src")"
    done
else
    # layout: the committed fixture is gc's output for today's generator.
    mkdir -p "$TMP/layout"
    python3 "$HERE/gen.py" layout > "$TMP/layout/main.go" || infra_die "gen.py layout failed"
    printf 'module m1w13/layout\n\ngo 1.27\n' > "$TMP/layout/go.mod"
    if ( cd "$TMP/layout" && "$GO" run . ) 2>"$TMP/layout/out.txt" >/dev/null \
            && cmp -s "$TMP/layout/out.txt" "$HERE/layout_corpus.txt"; then
        pass=$((pass + 1))
    else
        failed "layout_corpus.txt" "not gc's output for \`gen.py layout\` (regenerate: see gen.py)"
    fi
    # access: the generated load/store program.
    python3 "$HERE/gen.py" access --types 60 > "$TMP/access.go" || infra_die "gen.py access failed"
    one "gen.py access" "$TMP/access.go" gc
    for src in "$HERE"/programs/*.go "$HERE"/checkptr/*.go "$HERE"/restricted/*.go; do
        one "${src#"$ROOT"/}" "$src" "$(mode_of "$src")"
    done
fi

echo "m1w13: $pass passed, $fail failed of $((pass + fail))"
if [ $((pass + fail)) -eq 0 ]; then
    echo "m1w13: no program ran (zero cases is not a pass)" >&2
    exit "$INFRA_EXIT"
fi
[ "$fail" -eq 0 ] || exit "$DIVERGENCE_EXIT"
exit 0
