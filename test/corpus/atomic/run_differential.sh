#!/bin/bash
# Differential runner for the P3-M4 atomic corpus (ADR-0004, GG-0020):
# each program runs under stock gc 1.27.1 (built binary, run directly)
# and under graalgo-sync-run. Compares stdout bytes and exit codes.
#
# Known JVM Unsafe warnings on stderr are filtered (harness noise, cf.
# p3frontend/DESIGN.md §7).
set -uo pipefail
# Resolve the script's own directory BEFORE any cd: BASH_SOURCE[0] is
# relative to the caller's cwd, so it must be absolutised first.
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Repo root: this script lives at test/corpus/atomic/ in the checkout.
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
filter_stderr() { grep -v '^WARNING: ' || true; }

run_one() { # $1 = go file, $2 = gmp
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
    # Also compare against the checked-in .expected (guards against
    # both implementations drifting together).
    cmp -s "$base.expected" "$TMP/gr.out" || { ok=0; echo "FAIL $base (gmp=$gmp): differs from .expected"; }
    if [ "$ok" = 1 ]; then
        pass=$((pass+1))
    else
        fail=$((fail+1))
        echo "--- $base gc stdout:"; cat "$TMP/gc.out"
        echo "--- $base graalgo stdout:"; cat "$TMP/gr.out"
        echo "--- $base graalgo stderr (filtered):"; filter_stderr < "$TMP/gr.err" | head -3
    fi
}

for go in at*.go; do
    run_one "$go" 1; run_one "$go" 4
done

echo "== differential: $pass passed, $fail failed =="
[ "$fail" = 0 ]
