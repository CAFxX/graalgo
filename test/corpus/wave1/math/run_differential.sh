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
REPO_ROOT="$GR"
export PATH="$HOME/tools/go/bin:$PATH"
export TMPDIR="${TMPDIR:-$HOME/workspace/.tmp/m7-math}"
export GOCACHE="$HOME/workspace/.gocache-m7-math"
mkdir -p "$TMPDIR"

echo "== checking backend build =="
[ -x "$REPO_ROOT/internal/backend/graalgo-sync-run" ] || { echo "graalgo-sync-run missing: run 'make build' at $REPO_ROOT first"; exit 1; }
[ -d "$REPO_ROOT/internal/backend/interop/out/classes" ] || { echo "backend classes not built: run 'make build' at $REPO_ROOT first"; exit 1; }

# The launcher wrapper sets -Djava.io.tmpdir from TMPDIR (P3-M7R2: keeps the
# JVM off /tmp — a full /tmp silently yields empty output with exit 0,
# methodology review finding).
GRAALGO_RUN=( "$REPO_ROOT/internal/backend/graalgo-sync-run" )

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
