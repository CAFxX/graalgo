#!/usr/bin/env bash
# test/m1d/run.sh — differential gate of `os`, `reflect` and `fmt` through the
# -toolexec path (`make test-m1d`; toolexec milestones M1c/M1d, work packages
# W8b and W9b of docs/arch/toolexec-design.md): programs that print with the
# real fmt, use files, pipes, arguments, the environment and exit codes
# through upstream's os / internal/poll / syscall, and reflect on their own
# values, built by the stock `go build` through graalgo-tool with graalgo's
# fork applied (stdlib/) and run on the node package, against the same
# program built and run by gc.
#
# Nothing of the standard library is reimplemented for these programs: a
# function executes upstream's Go body or the registry's intrinsic for a
# function upstream gives no body (internal/toolexec/intrinsics/registry.txt);
# system calls go through the role table (internal/toolexec/syscalls/table.txt).
#
# Compared per program: stdout (bytes), stderr (normalized by
# test/corpus/lifecycle/normalize.py) and the exit status. Both executables
# are named `prog` and run as `./prog` from their own directory, so os.Args[0]
# is compared too (BUG-0029).
#
# A program's leading comment may carry directives, one per line:
#   // args: A B C        the program's arguments (split on blanks)
#   // env: K=V           an environment variable of both runs (repeatable)
#   // stdin: TEXT        the program's standard input, a newline appended
#                         (\n in TEXT is a newline); /dev/null otherwise
#
# Usage: test/m1d/run.sh [-v] [PROGRAM.go]...
#   -v   print the diff of every failing program
# Env:   GO, GRAALVM_HOME, GRAALGO_TMP (see test/lib/infra.sh),
#        GRAALGO_JAVA_OPTS (JVM flags of the graalgo leg),
#        M1D_TIMEOUT (seconds per run, default 120),
#        M1D_REPORT (file: one "PASS|FAIL <program> [reason]" line per program)
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
infra_mktemp m1d
TIMEOUT="${M1D_TIMEOUT:-120}"
NORMALIZE="$ROOT/test/corpus/lifecycle/normalize.py"
toolexec_setup
export GODEBUG=cpu.all=off

if [ $# -eq 0 ]; then
    mapfile -t programs < <(cd "$ROOT" && ls test/m1d/programs/*.go | sort)
else
    programs=("$@")
fi

directive() { # <source> <name> -> the values, one per line
    sed -n "s|^// $2: \(.*\)\$|\1|p" "$1"
}

run_leg() { # <dir> <stdout> <stderr> <stdin> <src> -> exit status
    local status=0 args=() envs=() a e
    read -r -a args <<< "$(directive "$5" args | head -1)"
    while IFS= read -r e; do [ -n "$e" ] && envs+=("$e"); done < <(directive "$5" env)
    ( cd "$1" && env "${envs[@]}" TMPDIR="$TMP" timeout "$TIMEOUT" ./prog "${args[@]}" >"$2" 2>"$3" <"$4" ) || status=$?
    return "$status"
}

# shellcheck source=../lib/gcoracle.sh
. "$HERE/../lib/gcoracle.sh"
gc_live() {
    if ( cd "$w/mod" && "$GO" build -o "$w/gc/prog" . ) >"$w/gc.build" 2>&1; then
        local st=0
        run_leg "$w/gc" "$w/gc.out" "$w/gc.err" "$stdin" "$src" || st=$?
        echo "$st" > "$w/gc.status"
    else
        echo build > "$w/gc.status"
    fi
}

pass=0
fail=0
report="${M1D_REPORT:-}"
[ -n "$report" ] && : > "$report"
n=0
for prog in "${programs[@]}"; do
    n=$((n + 1))
    case "$prog" in /*) src="$prog" ;; *) src="$ROOT/$prog" ;; esac
    [ -f "$src" ] || { echo "run.sh: no such program: $prog" >&2; exit "$USAGE_EXIT"; }
    w="$TMP/w/$n"
    mkdir -p "$w/mod" "$w/gc" "$w/gg"
    cp "$src" "$w/mod/main.go"
    printf 'module m1d/p\n\ngo 1.27\n' > "$w/mod/go.mod"
    stdin=/dev/null
    if grep -q '^// stdin: ' "$src"; then
        stdin="$w/stdin"
        printf '%b\n' "$(directive "$src" stdin | head -1)" > "$stdin"
    fi
    reason=""
    # The gc leg, replayed from test/lib/gcoracle.sh's cache when unchanged
    # (its args, env and stdin are directives of the source: in the key).
    gc_oracle "$w" "$w/mod" gc_live m1d "$TIMEOUT"
    gc_status=$(cat "$w/gc.status")
    if [ "$gc_status" = build ]; then
        reason="gc rejects the program (corpus bug)"
    elif ! ( cd "$w/mod" && toolexec_build "$w/gg/prog" "$RUNNER" ) >"$w/gg.build" 2>&1; then
        reason="graalgo-tool build failed: $(grep -v '^#' "$w/gg.build" | head -1)"
    else
        gg_status=0
        run_leg "$w/gg" "$w/gg.out" "$w/gg.err" "$stdin" "$src" || gg_status=$?
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

echo "m1d: $pass passed, $fail failed of $((pass + fail))"
[ "$fail" -eq 0 ] || exit "$DIVERGENCE_EXIT"
exit 0
