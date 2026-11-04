#!/bin/bash
# Differential runner for the P3-M7 math corpus (ADR-0007 D5): each program
# runs under stock gc 1.27.1 and under graalgo. Compares stdout bytes
# (programs print with %x / %.17g to catch single-bit differences).
#
# GR defaults to the checkout containing this script; override
# with GR or GRAALGO_HOME for worktree testing.
set -euo pipefail
cd "$(dirname "$0")"

SCRIPT_DIR="$(pwd)"
GR="${GR:-${GRAALGO_HOME:-$SCRIPT_DIR}}"
while [ "$GR" != "/" ] && [ ! -d "$GR/internal/backend" ]; do GR="$(dirname "$GR")"; done
[ -d "$GR/internal/backend" ] || { echo "$(basename "$0"): cannot locate repo root (no internal/backend under $SCRIPT_DIR); set GR to a graalgo checkout" >&2; exit 2; }
GRAALGO_SYNC_RUN="$GR/internal/backend/graalgo-sync-run"
[ -x "$GRAALGO_SYNC_RUN" ] || { echo "$(basename "$0"): $GRAALGO_SYNC_RUN missing/not executable; run 'make generate build' at the repo root first" >&2; exit 2; }
[ -d "$GR/internal/backend/interop/out/classes" ] || { echo "$(basename "$0"): backend classes not built; run 'make generate build' at the repo root first" >&2; exit 2; }
GRAALGO_RUN=( "$GRAALGO_SYNC_RUN" )
export PATH="$HOME/tools/go/bin:$PATH"
export TMPDIR="${TMPDIR:-$GR/test/corpus/wave1/.tmp}"
export GOCACHE="${GOCACHE:-$HOME/workspace/.gocache-m7-math}"
mkdir -p "$TMPDIR"

echo "== graalgo backend: $GRAALGO_SYNC_RUN =="


TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0; fail=0
filter_stderr() { grep -v '^WARNING: ' || true; }

run_one() { # $1 = go file
    local go="$1" base="${1%.go}"
    local gcbin="$TMP/$base.gc"

    go build -o "$gcbin" "$go" || { echo "FAIL $base: gc build failed"; fail=$((fail+1)); return; }
    local gc_exit
    if "$gcbin" > "$TMP/gc.out" 2> "$TMP/gc.err"; then gc_exit=0; else gc_exit=$?; fi
    local gr_exit
    if "${GRAALGO_RUN[@]}" --gmp=1 "$go" > "$TMP/gr.out" 2> "$TMP/gr.err"; then gr_exit=0; else gr_exit=$?; fi

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
