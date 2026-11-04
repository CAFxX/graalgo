#!/bin/bash
# Differential runner for the P3-M4 slice corpus (GG-0020): each program
# runs under stock gc 1.27.1 (built binary) and under graalgo-sync-run.
# Compares stdout bytes and exit codes. JVM Unsafe warnings filtered.
set -euo pipefail
cd "$(dirname "$0")"

SCRIPT_DIR="$(pwd)"
GR="${GR:-$SCRIPT_DIR}"
while [ "$GR" != "/" ] && [ ! -d "$GR/internal/backend" ]; do GR="$(dirname "$GR")"; done
[ -d "$GR/internal/backend" ] || { echo "$(basename "$0"): cannot locate repo root (no internal/backend under $SCRIPT_DIR); set GR to a graalgo checkout" >&2; exit 2; }
GRAALGO_SYNC_RUN="$GR/internal/backend/graalgo-sync-run"
[ -x "$GRAALGO_SYNC_RUN" ] || { echo "$(basename "$0"): $GRAALGO_SYNC_RUN missing/not executable; run 'make generate build' at the repo root first" >&2; exit 2; }
[ -d "$GR/internal/backend/interop/out/classes" ] || { echo "$(basename "$0"): backend classes not built; run 'make generate build' at the repo root first" >&2; exit 2; }
GRAALGO_RUN=( "$GRAALGO_SYNC_RUN" )
export PATH="$HOME/tools/go-1.27.1/bin:$PATH"

echo "== graalgo backend: $GRAALGO_SYNC_RUN =="


TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0; fail=0
filter_stderr() { grep -v '^WARNING: ' || true; }

run_one() {
    local go="$1" gmp="$2" base="${1%.go}"
    local gcbin="$TMP/$base.gc"
    go build -o "$gcbin" "$go" || { echo "FAIL $base: gc build failed"; fail=$((fail+1)); return; }
    local gc_exit
    if "$gcbin" > "$TMP/gc.out" 2> "$TMP/gc.err"; then gc_exit=0; else gc_exit=$?; fi
    local gr_exit
    if "${GRAALGO_RUN[@]}" --gmp="$gmp" "$go" > "$TMP/gr.out" 2> "$TMP/gr.err"; then gr_exit=0; else gr_exit=$?; fi
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

for go in sl*.go; do
    run_one "$go" 1; run_one "$go" 4
done

echo "== differential: $pass passed, $fail failed =="
[ "$fail" = 0 ]
