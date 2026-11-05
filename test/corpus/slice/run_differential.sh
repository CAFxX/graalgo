#!/bin/bash
# Differential runner for the P3-M4 slice corpus (GG-0020): each program
# runs under stock gc 1.27.1 (built binary) and under graalgo-sync-run.
# Compares stdout bytes and exit codes. JVM Unsafe warnings filtered.
set -uo pipefail
cd "$(dirname "$0")"

GR="${GR:-$HOME/workspace/graalgo}"
BE="$GR/internal/backend"
export PATH="$HOME/tools/go-1.27.1/bin:$PATH"

# Build precondition: the product launcher (internal/backend/graalgo-sync-run)
# needs the backend classes (internal/backend/*/out/classes) and the
# gofrontend binary (internal/frontend/gofrontend). Build what's missing at
# the repo root; the stdlib gate already builds, so this is normally a
# no-op check. (The old phases/phase{1,2,3} layout was deleted 2026-10-14.)
if [ ! -d "$BE/interop/out/classes" ] || [ ! -d "$BE/runtime/out/classes" ] \
    || [ ! -d "$BE/truffle/out/classes" ]; then
    echo "== building backend (make build) =="
    make -C "$GR" build > /dev/null || { echo "backend build failed"; exit 1; }
fi
if [ ! -x "$GR/internal/frontend/gofrontend" ]; then
    echo "== building gofrontend =="
    ( cd "$GR/internal/frontend" && ./build.sh build > /dev/null ) \
        || { echo "gofrontend build failed"; exit 1; }
fi

GRAALGO_RUN=( "$BE/graalgo-sync-run" )

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

for go in sl*.go; do
    run_one "$go" 1; run_one "$go" 4
done

echo "== differential: $pass passed, $fail failed =="
[ "$fail" = 0 ]
