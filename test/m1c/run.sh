#!/usr/bin/env bash
# test/m1c/run.sh — differential gate of the M1c slice (`make test-m1c`;
# docs/arch/toolexec-design.md §13, "M1c slice"): the parts of the standard
# library that do not need descriptor images — sync's semaphores and notify
# lists, internal/bytealg's intrinsics, time on the runtime timer heap and the
# syscall executor — built by the stock `go build` through graalgo-tool with
# graalgo's fork applied, run on the node package (graalgo.go) via
# internal/backend/graalgo-bundle-run, against the same program built and run
# by gc. Conventions as test/m1r/run.sh: upstream's Go bodies or the registry's
# intrinsics, nothing reimplemented; stdout compared byte for byte, stderr
# after test/corpus/lifecycle/normalize.py (goroutine ids, frame lines, hex
# addresses: the fatal and deadlock headers with gc's wait reasons are
# compared), and the exit status; both legs with GODEBUG=cpu.all=off.
#
# Programs: test/m1c/programs/*.go, one `package main` file each.
#
# Pending programs. A program whose first lines contain
#     //m1c:requires <probe>
# needs a piece of another lane (today: the descriptor images of W9a, which
# `errors` and everything importing it — `time` — stand on). The probe
# test/m1c/probes/<probe>.go is built and run through the same path first:
# while it does not match gc, the program is reported PENDING (with the
# probe's failure) and not run; once it does, the program runs and must pass.
# A probe is the smallest program that exercises the missing piece, so the
# gate turns itself on when that piece lands (a master merge) — nobody has to
# remember to enable it. M1C_STRICT=1 turns every PENDING into a failure.
#
# Link flags. A program whose first lines contain
#     //m1c:ldflags <flags>
# is built with -ldflags=<flags> on both legs (bytealg_kernels.go pulls
# internal/bytealg's assembly by //go:linkname and needs gc's
# -checklinkname=0).
#
# Environment. Each line
#     //m1c:env NAME=VALUE
# in a program's first lines is set for both legs' runs (TZ=Europe/Rome).
#
# Usage: test/m1c/run.sh [-v] [PROGRAM.go]...
#   -v   print the diff of every failing program
# Env:   GO, GRAALVM_HOME, GRAALGO_TMP (see test/lib/infra.sh),
#        GRAALGO_JAVA_OPTS (JVM flags of the graalgo leg),
#        M1C_TIMEOUT (seconds per run, default 120),
#        M1C_STRICT (1: a pending program fails),
#        M1C_REPORT (file: one "PASS|FAIL|PENDING <program> [reason]" line per program)
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
infra_mktemp m1c
TIMEOUT="${M1C_TIMEOUT:-120}"
NORMALIZE="$ROOT/test/corpus/lifecycle/normalize.py"
toolexec_setup
export GODEBUG=cpu.all=off
# shellcheck source=../lib/gcoracle.sh
. "$HERE/../lib/gcoracle.sh"

if [ $# -eq 0 ]; then
    mapfile -t programs < <(cd "$ROOT" && ls test/m1c/programs/*.go | sort)
else
    programs=("$@")
fi

run_leg() { # <exe> <stdout> <stderr> [NAME=VALUE...] -> exit status
    local status=0 exe="$1" out="$2" err="$3"
    shift 3
    ( cd "$(dirname "$exe")" && env "$@" timeout "$TIMEOUT" "$exe" >"$out" 2>"$err" </dev/null ) || status=$?
    return "$status"
}

# compare <src> <workdir> -> sets $reason ("" when graalgo matches gc)
compare() {
    local src="$1" w="$2" gc_status gg_status
    mkdir -p "$w/mod"
    cp "$src" "$w/mod/main.go"
    printf 'module m1c/p\n\ngo 1.27\n' > "$w/mod/go.mod"
    reason=""
    local ldflags goflags=""
    ldflags=$(head -20 "$src" | sed -n 's|^//m1c:ldflags \(.*\)$|\1|p' | head -1)
    [ -n "$ldflags" ] && goflags="-ldflags=$ldflags"
    local envs=()
    mapfile -t envs < <(head -20 "$src" | sed -n 's|^//m1c:env \([A-Za-z_][A-Za-z0-9_]*=.*\)$|\1|p')
    # The gc leg, replayed from test/lib/gcoracle.sh's cache when unchanged.
    gc_live() {
        if ( cd "$w/mod" && GOFLAGS="$goflags" "$GO" build -o "$w/gc.bin" . ) >"$w/gc.build" 2>&1; then
            local st=0
            run_leg "$w/gc.bin" "$w/gc.out" "$w/gc.err" "${envs[@]}" || st=$?
            echo "$st" > "$w/gc.status"
        else
            echo build > "$w/gc.status"
        fi
    }
    gc_oracle "$w" "$w/mod" gc_live m1c "$TIMEOUT" "goflags=$goflags" "${envs[@]}"
    gc_status=$(cat "$w/gc.status")
    if [ "$gc_status" = build ]; then
        reason="gc rejects the program (corpus bug)"
    elif ! ( cd "$w/mod" && GOFLAGS="$goflags" toolexec_build "$w/gg.bin" "$RUNNER" ) >"$w/gg.build" 2>&1; then
        reason="graalgo-tool build failed: $(grep -v '^#' "$w/gg.build" | head -1)"
    else
        gg_status=0
        TMPDIR="$TMP" run_leg "$w/gg.bin" "$w/gg.out" "$w/gg.err" "${envs[@]}" || gg_status=$?
        infra_check_run "$src" "$gg_status" "$w/gg.err"
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
}

declare -A probe_state # probe name -> "" (passes) or its failure
probe() { # <name> -> 0 when the probe matches gc; sets $probe_reason
    local name="$1"
    if [ -z "${probe_state[$name]+x}" ]; then
        local psrc="$ROOT/test/m1c/probes/$name.go"
        [ -f "$psrc" ] || { echo "run.sh: no such probe: $name" >&2; exit "$USAGE_EXIT"; }
        compare "$psrc" "$TMP/probe/$name"
        probe_state[$name]="$reason"
        rm -rf "$TMP/probe/$name"
    fi
    probe_reason="${probe_state[$name]}"
    [ -z "$probe_reason" ]
}

pass=0
fail=0
pending=0
report="${M1C_REPORT:-}"
[ -n "$report" ] && : > "$report"
n=0
for prog in "${programs[@]}"; do
    n=$((n + 1))
    case "$prog" in /*) src="$prog" ;; *) src="$ROOT/$prog" ;; esac
    [ -f "$src" ] || { echo "run.sh: no such program: $prog" >&2; exit "$USAGE_EXIT"; }
    req=$(head -20 "$src" | sed -n 's|^//m1c:requires \([A-Za-z0-9_]*\).*|\1|p' | head -1)
    if [ -n "$req" ] && ! probe "$req"; then
        if [ "${M1C_STRICT:-0}" = 1 ]; then
            fail=$((fail + 1))
            echo "FAIL $prog: pending on probe $req: $probe_reason"
            [ -n "$report" ] && echo "FAIL $prog: pending on probe $req: $probe_reason" >> "$report"
        else
            pending=$((pending + 1))
            echo "PENDING $prog: probe $req: $probe_reason"
            [ -n "$report" ] && echo "PENDING $prog: probe $req: $probe_reason" >> "$report"
        fi
        continue
    fi
    w="$TMP/w/$n"
    compare "$src" "$w"
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

echo "m1c: $pass passed, $fail failed, $pending pending of $((pass + fail + pending))"
[ "$fail" -eq 0 ] || exit "$DIVERGENCE_EXIT"
exit 0
