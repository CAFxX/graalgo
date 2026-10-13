#!/bin/bash
# Differential runner for the P3-M4 traceback corpus (GG-0020): each
# program panics; gc's and graalgo's tracebacks are normalized via
# normalize.py and compared. Exit codes are compared too (gc: 2).
#
# Known JVM Unsafe warnings on stderr are filtered (harness noise, cf.
# p3frontend/DESIGN.md §7).
set -uo pipefail
cd "$(dirname "$0")"

GR="$HOME/workspace/graalgo"
P3="$GR/phases/phase3"
P1="$GR/phases/phase1"
P2="$GR/phases/phase2"
export PATH="$HOME/tools/go-1.27.1/bin:$PATH"
JAVA="$HOME/tools/graalvm-jdk-25/bin/java"

echo "== building phase-3 java module =="
make -C "$P3" main > /dev/null || { echo "phase3 build failed"; exit 1; }

CP="$P3/out/classes:$P2/out/classes:$P1/out/classes:$(ls "$P1"/lib/*.jar | tr '\n' ':')"
GRAALGO_RUN=( "$JAVA" --enable-native-access=ALL-UNNAMED -Xss32m -cp "$CP"
    graalgo.p3sync.launcher.P3SyncMain )

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0; fail=0
filter_noise() { grep -v '^WARNING: ' | grep -v '^/home/hatch/tools' | grep -v '^make:' || true; }

run_one() { # $1 = go file
    local go="$1" base="${1%.go}"
    local gcbin="$TMP/$base.gc"

    go build -o "$gcbin" "$go" || { echo "FAIL $base: gc build failed"; fail=$((fail+1)); return; }
    "$gcbin" > "$TMP/gc.out" 2> "$TMP/gc.err"; local gc_exit=$?
    "${GRAALGO_RUN[@]}" --gmp=1 "$go" > "$TMP/gr.out" 2> "$TMP/gr.err"; local gr_exit=$?

    cat "$TMP/gc.out" "$TMP/gc.err" | ./normalize.py > "$TMP/gc.norm"
    cat "$TMP/gr.out" "$TMP/gr.err" | filter_noise | ./normalize.py > "$TMP/gr.norm"

    local ok=1
    cmp -s "$TMP/gc.norm" "$TMP/gr.norm" || { ok=0; echo "FAIL $base: normalized traceback differs"; }
    if [ "$gc_exit" != "$gr_exit" ]; then
        ok=0; echo "FAIL $base: exit gc=$gc_exit graalgo=$gr_exit"
    fi
    cmp -s "$base.expected" "$TMP/gr.norm" || { ok=0; echo "FAIL $base: differs from .expected"; }
    if [ "$ok" = 1 ]; then
        pass=$((pass+1))
    else
        fail=$((fail+1))
        echo "--- $base gc (normalized):"; cat "$TMP/gc.norm"
        echo "--- $base graalgo (normalized):"; cat "$TMP/gr.norm"
    fi
}

for go in tb*.go; do
    run_one "$go"
done

echo "== differential: $pass passed, $fail failed =="
[ "$fail" = 0 ]
