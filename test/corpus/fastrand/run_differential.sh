#!/bin/bash
# Differential runner for the P3-M4 fastrand corpus (GG-0020): each program
# runs under stock gc 1.27.1 (built binary) and under graalgo-sync-run.
# Compares stdout bytes and exit codes. JVM Unsafe warnings filtered.
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
filter_stderr() { grep -v '^WARNING: ' || true; }

run_one() {
    local go="$1" gmp="$2" base="${1%.go}"
    local gcbin="$TMP/$base.gc"
    go build -o "$gcbin" "$go" || { echo "FAIL $base: gc build failed"; fail=$((fail+1)); return; }
    "$gcbin" > "$TMP/gc.out" 2> "$TMP/gc.err"; local gc_exit=$?
    "${GRAALGO_RUN[@]}" --gmp="$gmp" "$go" > "$TMP/gr.out" 2> "$TMP/gr.err"; local gr_exit=$?
    local ok=1
    cmp -s "$TMP/gc.out" "$TMP/gr.out" || { ok=0; echo "FAIL $base (gmp=$gmp): stdout differs"; }
    if [ "$gc_exit" != "$gr_exit" ]; then
        ok=0; echo "FAIL $base (gmp=$gmp): exit gc=$gc_exit graalgo=$gr_exit"
    fi
    cmp -s "$base.expected" "$TMP/gr.out" || { ok=0; echo "FAIL $base (gmp=$gmp): differs from .expected"; }
    if [ "$ok" = 1 ]; then pass=$((pass+1)); else
        fail=$((fail+1))
        echo "--- $base gc stdout:"; cat "$TMP/gc.out"
        echo "--- $base graalgo stdout:"; cat "$TMP/gr.out"
    fi
}

for go in fr*.go; do
    run_one "$go" 1; run_one "$go" 4
done

echo "== differential: $pass passed, $fail failed =="
[ "$fail" = 0 ]
