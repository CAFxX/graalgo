#!/bin/bash
# Differential runner for the P3-M8 wave-2 corpus (see README.md): each
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
# Usage: run_wave.sh [pkg|all] — run one package subdir, or every wave-2
# package dir when omitted. Underscore-prefixed dirs (_smoke, infra) run
# only when named explicitly.
#
# The repo root defaults to the repository containing this script (resolved
# via its own path — never hardcoded) but can be overridden:
#   GR=~/workspace/graalgo-m8-pipe run_wave.sh strings
set -uo pipefail
cd "$(dirname "$0")"

GR="${GR:-$(cd "$(dirname "$0")/../../../.." && pwd)}"
P3="$GR/phases/phase3"
P2="$GR/phases/phase2"
P1="$GR/phases/phase1"
export PATH="$HOME/tools/go/bin:$PATH"
JAVA="$HOME/tools/graalvm-jdk-25/bin/java"

# Tmp discipline (corpus/wave2/README.md): never write to /tmp.
export TMPDIR="$P3/hidden_files/m8-tmp/wave2"
export GOCACHE="$HOME/workspace/.gocache-m8-wave2"
mkdir -p "$TMPDIR"

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

echo "== building phase-3 java module =="
make -C "$P3" main > /dev/null || { echo "phase3 build failed"; exit 1; }

CP="$P3/out/classes:$P2/out/classes:$P1/out/classes:$(ls "$P1"/lib/*.jar | tr '\n' ':')"
# P3-M8b: point java.io.tmpdir at $TMPDIR (never /tmp): the JVM's shared-
# memory files warn "Insufficient space" on the 512M /tmp tmpfs, which is
# harness noise in every corpus run's stderr.
GRAALGO_RUN=( "$JAVA" --enable-native-access=ALL-UNNAMED -Xss32m "-Djava.io.tmpdir=$TMPDIR" -cp "$CP"
    graalgo.p3sync.launcher.P3SyncMain )

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0; fail=0; scopelimited=0
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
        gopath="$(python3 "$(dirname "$0")/extract_synth_ref.py" \
            "$P1/frontend" "$go" "$refdir")" \
            || { echo "FAIL $base: synthetic-ref extraction failed"; return 1; }
        GO111MODULE=off GOPATH="$gopath" go build -o "$gcbin" \
            "$gopath/src/m8prog/main.go" \
            || { echo "FAIL $base: gc build failed (synthetic ref)"; return 1; }
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
        build_gc "$go" "$gcbin" || { fail=$((fail+1)); return; }
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

    build_gc "$go" "$gcbin" || { fail=$((fail+1)); return; }
    # Programs with a <base>.args file get its whitespace-separated
    # contents as argv (after the program). Both legs receive the same
    # args; the graalgo leg passes them after `--` per the launcher
    # argv convention (R6).
    local args=()
    if [ -f "$base.args" ]; then
        # shellcheck disable=SC2207
        args=($(cat "$base.args"))
    fi
    "$gcbin" "${args[@]}" > "$TMP/gc.out" 2> "$TMP/gc.err"; local gc_exit=$?
    "${GRAALGO_RUN[@]}" --gmp="$gmp" -- "$go" "${args[@]}" > "$TMP/gr.out" 2> "$TMP/gr.err"; local gr_exit=$?

    local ok=1
    cmp -s "$TMP/gc.out" "$TMP/gr.out" || { ok=0; echo "FAIL $base (gmp=$gmp): stdout differs"; }
    # Divergent programs (marked <base>.divergent): an INTENTIONAL,
    # documented behavioral divergence where graalgo fail-fasts and gc
    # does not (e.g. unsafe.Add traps under graalgo, computes under gc).
    # The exit codes are EXPECTED to differ, so the exit comparison is
    # skipped — but stdout must still match and the .panic pin still
    # applies. A .divergent marker WITHOUT a .panic file is a harness
    # FAIL (divergence must be a pinned fail-fast, never silent).
    if [ -f "$base.divergent" ]; then
        if [ ! -f "$base.panic" ]; then
            ok=0; echo "FAIL $base (gmp=$gmp): .divergent without .panic (divergence must be a pinned fail-fast)"
        fi
        # exit comparison skipped by design; stdout + .panic still checked
    elif [ "$gc_exit" != "$gr_exit" ]; then
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

echo "== differential: $pass passed, $scopelimited scope-limited, $fail failed =="
[ "$fail" = 0 ]
