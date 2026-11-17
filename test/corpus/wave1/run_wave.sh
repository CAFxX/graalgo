#!/bin/bash
# Differential runner for the P3-M7 wave-1 corpus (see README.md): each
# program runs under stock gc 1.27.1 (built binary, run directly — `go run`
# masks the exit code) and under graalgo-p3sync-run. Compares stdout bytes,
# exit codes, and — for panic programs — the first stderr line.
#
# Known JVM Unsafe warnings on stderr are filtered (harness noise, cf.
# p3frontend/DESIGN.md §7). Timing-sensitive programs (marked with an
# empty <base>.timing file) print fixed strings only (never measured
# durations) and run with gmp=1 only; all other programs run with gmp=1
# and gmp=4 (the timectx convention).
#
# Scope-limited programs (marked with a <base>.scopelimited file naming
# the reason and a retest: trigger) can never pass the frontend for honest
# scope reasons. They are NOT run under graalgo and are counted separately
# (neither passing nor failing); the gc leg still builds and runs them so
# a program that is not even valid Go surfaces as FAIL, not scope-limited.
# A .scopelimited file without a retest: trigger is a harness FAIL.
#
# Synthetic-reference programs (marked with an empty <base>.synthetic file)
# use M7-extension constructors (csv.NewReaderString, gob.NewBufferEncoder,
# ...) absent from real std, so the gc leg cannot build them against GOROOT
# (a GOPATH cannot shadow the standard library). For these, the gc leg
# builds the program against the extracted phase-1 synthetic sources
# (extract_synth_ref.py) — the same sources the P3-M7 review §11 validated
# as gc-faithful — with imports rewritten to a temp GOPATH.
#
# Usage: run_wave.sh [pkg|all] — run one package subdir, or every wave-1
# package dir when omitted. Underscore-prefixed dirs (_smoke, infra) run
# only when named explicitly.
#
# The repo root is the repository containing this script; override with
# GRAALGO_ROOT=<checkout>. Toolchains: GO= (gc), GRAALVM_HOME=, and
# GRAALGO_SYNC_RUN= (launcher) — see test/lib/infra.sh.
#
# Exit status: 0 pass, 1 divergence, 2 usage (unknown package dir),
# 3 infrastructure error (launcher, java, backend build or gc toolchain
# missing; JVM class-load failure). Needs `make build` + `make generate`.
set -uo pipefail
cd "$(dirname "$0")"

WAVE_DIR="$(pwd)"
# shellcheck source=../../lib/infra.sh
. "$WAVE_DIR/../../lib/infra.sh"
infra_setup_go

case "${1-all}" in
    all)
        dirs=()
        for d in */; do
            case "$d" in _*/) continue ;; esac
            dirs+=("${d%/}")
        done
        ;;
    *) dirs=("$1") ;;
esac
for d in "${dirs[@]}"; do
    [ -d "$d" ] || { echo "unknown package dir: $d"; exit 2; }
done

infra_setup_graalgo
# infra_mktemp exports TMPDIR, so the launcher's -Djava.io.tmpdir and
# go build both stay off /tmp (a full /tmp tmpfs silently yields empty
# output with exit 0 — methodology review finding).
infra_mktemp corpus-wave1

pass=0; fail=0; scopelimited=0; infra=0
# filter_stderr: drop the known JVM Unsafe warning lines.
filter_stderr() { grep -v '^WARNING: ' || true; }

# build_gc <go file> <gcbin>: stock-gc reference leg. Programs marked
# <base>.synthetic build against the extracted phase-1 synthetic sources;
# all others build against real std.
build_gc() {
    local go="$1" gcbin="$2" base="${1%.go}"
    if [ -f "$base.synthetic" ]; then
        local refdir="$TMP/synthref-$(basename "$base")"
        local gopath
        gopath="$(python3 "$WAVE_DIR/extract_synth_ref.py" \
            "$GRAALGO_ROOT/internal/frontend" "$go" "$refdir")" \
            || { echo "INFRA $base: synthetic-ref extraction failed"; infra=$((infra+1)); return 2; }
        GO111MODULE=off GOPATH="$gopath" go build -o "$gcbin" \
            "$gopath/src/m7prog/main.go" \
            || { echo "INFRA $base: gc build of the synthetic reference failed (no reference leg)"; infra=$((infra+1)); return 2; }
    else
        go build -o "$gcbin" "$go" \
            || { echo "FAIL $base: gc build failed"; return 1; }
    fi
}

run_scope_limited() { # $1 = go file
    local go="$1" base="${1%.go}"
    local reason retest nogc
    reason="$(sed -n 's/^reason: //p' "$base.scopelimited" | head -1)"
    retest="$(sed -n 's/^retest: //p' "$base.scopelimited" | head -1)"
    nogc="$(sed -n 's/^nogc: //p' "$base.scopelimited" | head -1)"
    if [ -z "${retest:-}" ]; then
        echo "FAIL $base: .scopelimited marker lacks a retest: trigger"
        fail=$((fail+1)); return
    fi
    if [ -z "${nogc:-}" ]; then
        # The program must still be valid runnable Go; otherwise it is a
        # corpus bug, not a scope limitation.
        local gcbin="$TMP/$base.gc"
        build_gc "$go" "$gcbin" || { [ $? = 2 ] || fail=$((fail+1)); return; }
        "$gcbin" > /dev/null 2>&1
        local gc_exit=$?
        if [ "$gc_exit" != 0 ]; then
            echo "FAIL $base: scope-limited program exits $gc_exit under gc (corpus bug)"
            fail=$((fail+1)); return
        fi
    fi
    scopelimited=$((scopelimited+1))
    echo "SCOPE-LIMITED $base: ${reason:-<no reason given>} [retest: $retest]"
}

run_one() { # $1 = go file, $2 = gmp
    local go="$1" gmp="$2" base="${1%.go}"
    local gcbin="$TMP/$base.gc"

    build_gc "$go" "$gcbin" || { [ $? = 2 ] || fail=$((fail+1)); return; }
    "$gcbin" > "$TMP/gc.out" 2> "$TMP/gc.err"; local gc_exit=$?
    "${GRAALGO_RUN[@]}" --gmp="$gmp" "$go" > "$TMP/gr.out" 2> "$TMP/gr.err"; local gr_exit=$?
    infra_check_run "$base" "$gr_exit" "$TMP/gr.err"

    local ok=1
    cmp -s "$TMP/gc.out" "$TMP/gr.out" || { ok=0; echo "FAIL $base (gmp=$gmp): stdout differs"; }
    if [ "$gc_exit" != "$gr_exit" ]; then
        ok=0; echo "FAIL $base (gmp=$gmp): exit gc=$gc_exit graalgo=$gr_exit"
    fi
    if [ -f "$base.panic" ]; then
        local gr_first
        gr_first="$(filter_stderr < "$TMP/gr.err" | head -1)"
        local want
        want="$(cat "$base.panic")"
        if [ "$gr_first" != "$want" ]; then
            ok=0; echo "FAIL $base (gmp=$gmp): stderr first line: got [$gr_first] want [$want]"
        fi
    fi
    if [ "$ok" = 1 ]; then
        pass=$((pass+1)); # echo "ok $base (gmp=$gmp)"
    else
        fail=$((fail+1))
        echo "--- $base gc stdout:"; cat "$TMP/gc.out"
        echo "--- $base graalgo stdout:"; cat "$TMP/gr.out"
        echo "--- $base graalgo stderr (filtered):"; filter_stderr < "$TMP/gr.err" | head -3
    fi
}

for dir in "${dirs[@]}"; do
    for go in "$dir"/[0-9][0-9]_*.go; do
        [ -e "$go" ] || continue
        base="${go%.go}"
        # Scope-limited programs: gc leg only, counted separately.
        if [ -f "$base.scopelimited" ]; then
            run_scope_limited "$go"
            continue
        fi
        # Timing-sensitive programs (marked <base>.timing) run with gmp=1
        # only (single-threaded is enough; the multi-gmp leg adds
        # scheduling noise for sleeps).
        if [ -f "${go%.go}.timing" ]; then
            run_one "$go" 1
        else
            run_one "$go" 1; run_one "$go" 4
        fi
    done
done

echo "== differential: $pass passed, $scopelimited scope-limited, $fail failed, $infra infra-error =="
# An infra error means part of the corpus produced no verdict: exit 3
# (never a pass, never a divergence) — the counts above say what ran.
[ "$infra" = 0 ] || exit "$INFRA_EXIT"
[ "$fail" = 0 ]
