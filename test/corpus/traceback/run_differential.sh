#!/bin/bash
# Differential runner for the P3-M4 traceback corpus (GG-0020): each
# program panics; gc's and graalgo's tracebacks are normalized via
# normalize.py and compared. Exit codes are compared too (gc: 2).
#
# Known JVM Unsafe warnings on stderr are filtered (harness noise, cf.
# p3frontend/DESIGN.md §7).
set -uo pipefail
# Resolve the script's own directory BEFORE any cd: BASH_SOURCE[0] is
# relative to the caller's cwd, so it must be absolutised first.
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Repo root: this script lives at test/corpus/traceback/ in the checkout.
GR="${GR:-$(cd "$SCRIPT_DIR/../../.." && pwd)}"
export PATH="${GO_BIN_DIR:-$HOME/tools/go/bin}:$PATH"

echo "== building backend java modules =="
make -C "$GR" build > /dev/null || { echo "backend build failed"; exit 1; }
# gofrontend is a gitignored build output (internal/frontend/gofrontend);
# the product launcher resolves it via ToolPaths' walk-up.
if [ ! -x "$GR/internal/frontend/gofrontend" ]; then
    echo "== building gofrontend =="
    make -C "$GR" generate > /dev/null || { echo "gofrontend build failed"; exit 1; }
fi

# Product launcher (P3SyncMain) via its wrapper: classpath order lives
# in the wrapper, not here.
GRAALGO_RUN=( "$GR/internal/backend/graalgo-sync-run" )

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
