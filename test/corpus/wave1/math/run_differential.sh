#!/bin/bash
# Differential runner for the P3-M7 math corpus (ADR-0007 D5): each program
# runs under stock gc 1.27.1 and under graalgo. Compares stdout bytes
# (programs print with %x / %.17g to catch single-bit differences).
#
# GR defaults to $HOME/workspace/graalgo (the integrated tree); override
# with GRAALGO_HOME for worktree testing.
set -uo pipefail
cd "$(dirname "$0")"

GR="${GRAALGO_HOME:-$HOME/workspace/graalgo}"
P3="$GR/phases/phase3"
P2="$GR/phases/phase2"
P1="$GR/phases/phase1"
export PATH="$HOME/tools/go/bin:$PATH"
export TMPDIR="$P3/hidden_files/m7-tmp/math"
export GOCACHE="$HOME/workspace/.gocache-m7-math"
mkdir -p "$TMPDIR"
JAVA="$HOME/tools/graalvm-jdk-25/bin/java"

echo "== building phase-3 java module =="
make -C "$P3" main > /dev/null || { echo "phase3 build failed"; exit 1; }

CP="$P3/out/classes:$P2/out/classes:$P1/out/classes:$(ls "$P1"/lib/*.jar | tr '\n' ':')"
# P3-M7R2: -Djava.io.tmpdir keeps the JVM off /tmp (a full /tmp silently
# yields empty output with exit 0 — methodology review finding).
GRAALGO_RUN=( "$JAVA" --enable-native-access=ALL-UNNAMED -Xss32m \
    -Djava.io.tmpdir="$TMPDIR" -cp "$CP" \
    graalgo.p3sync.launcher.P3SyncMain )

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0; fail=0
filter_stderr() { grep -v '^WARNING: ' || true; }

run_one() { # $1 = go file
    local go="$1" base="${1%.go}"
    local gcbin="$TMP/$base.gc"

    go build -o "$gcbin" "$go" || { echo "FAIL $base: gc build failed"; fail=$((fail+1)); return; }
    "$gcbin" > "$TMP/gc.out" 2> "$TMP/gc.err"; local gc_exit=$?
    "${GRAALGO_RUN[@]}" --gmp=1 "$go" > "$TMP/gr.out" 2> "$TMP/gr.err"; local gr_exit=$?

    local ok=1
    cmp -s "$TMP/gc.out" "$TMP/gr.out" || { ok=0; echo "FAIL $base: stdout differs"; diff "$TMP/gc.out" "$TMP/gr.out" | head -5; }
    if [ "$gc_exit" != "$gr_exit" ]; then
        ok=0; echo "FAIL $base: exit gc=$gc_exit graalgo=$gr_exit"
    fi
    if [ "$ok" = 1 ]; then
        echo "PASS $base"; pass=$((pass+1))
    else
        fail=$((fail+1))
    fi
}

for f in m00_bits.go m01_transcendentals.go m02_binary.go m03_special.go m04_bessel.go; do
    run_one "$f"
done

echo "== $pass passed, $fail failed =="
[ "$fail" = 0 ]
