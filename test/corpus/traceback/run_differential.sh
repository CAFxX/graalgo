#!/bin/bash
# Differential runner for the P3-M4 traceback corpus (GG-0020): each
# program panics; gc's and graalgo's tracebacks are normalized via
# normalize.py and compared. Exit codes are compared too (gc: 2).
#
# Known JVM Unsafe warnings on stderr are filtered (harness noise, cf.
# p3frontend/DESIGN.md §7).
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
filter_noise() { grep -v '^WARNING: ' | grep -v '^/home/hatch/tools' | grep -v '^make:' || true; }

run_one() { # $1 = go file
    local go="$1" base="${1%.go}"
    local gcbin="$TMP/$base.gc"

    go build -o "$gcbin" "$go" || { echo "FAIL $base: gc build failed"; fail=$((fail+1)); return; }
    local gc_exit
    if "$gcbin" > "$TMP/gc.out" 2> "$TMP/gc.err"; then gc_exit=0; else gc_exit=$?; fi
    local gr_exit
    if "${GRAALGO_RUN[@]}" --gmp=1 "$go" > "$TMP/gr.out" 2> "$TMP/gr.err"; then gr_exit=0; else gr_exit=$?; fi

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
