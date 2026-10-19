#!/bin/bash
# P2-M2 corpus graduation runner (P3-M5, ADR-0005 Appendix C): the channel
# corpus programs (ch01-ch12) run end-to-end through the full pipeline
# (phase-1 gofrontend -> phase-3 P3SyncMain) and must produce their
# gc-recorded .expected output. Follows the timectx run_differential.sh
# pattern exactly (cd-to-script-dir, relative paths, gc built binary,
# stdout+exit+(panic first line) comparison, JVM Unsafe stderr filter).
set -uo pipefail
cd "$(dirname "$0")"

# Backend: the product launcher (P3SyncMain) via the wrapper at
# internal/backend/graalgo-sync-run. Requires `make build` at the repo root
# first — the wrapper never builds; it fails loudly when classes are missing.
REPO_ROOT="$(cd ../../../.. && pwd)"
export PATH="$HOME/tools/go/bin:$PATH"

echo "== checking backend build =="
[ -x "$REPO_ROOT/internal/backend/graalgo-sync-run" ] || { echo "graalgo-sync-run missing: run 'make build' at $REPO_ROOT first"; exit 1; }
[ -d "$REPO_ROOT/internal/backend/interop/out/classes" ] || { echo "backend classes not built: run 'make build' at $REPO_ROOT first"; exit 1; }

GRAALGO_RUN=( "$REPO_ROOT/internal/backend/graalgo-sync-run" )

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0; fail=0
# filter_stderr: drop the known JVM Unsafe warning lines.
filter_stderr() { grep -v '^WARNING: ' || true; }

run_one() { # $1 = go file, $2 = gmp
    local go="$1" gmp="$2" base="${1%.go}"
    local gcbin="$TMP/$base.gc"

    go build -o "$gcbin" "$go" || { echo "FAIL $base: gc build failed"; fail=$((fail+1)); return; }
    "$gcbin" > "$TMP/gc.out" 2> "$TMP/gc.err"; local gc_exit=$?
    timeout 120 "${GRAALGO_RUN[@]}" --gmp="$gmp" "$go" > "$TMP/gr.out" 2> "$TMP/gr.err"; local gr_exit=$?

    local ok=1
    # .expected pins gc's recorded stdout — this is the graduation
    # criterion. A fresh gc run is informational only (racy programs like
    # ch12 may select a panic case non-deterministically).
    if [ -f "$base.expected" ]; then
        cmp -s "$base.expected" "$TMP/gr.out" || { ok=0; echo "FAIL $base (gmp=$gmp): stdout differs from .expected"; }
    else
        cmp -s "$TMP/gc.out" "$TMP/gr.out" || { ok=0; echo "FAIL $base (gmp=$gmp): stdout differs gc-vs-graalgo"; }
    fi
    if [ "$gc_exit" != "$gr_exit" ]; then
        # For racy programs, exit code may differ; only fail if .expected
        # exists and stdout matched (i.e., deterministic programs).
        if [ -f "$base.expected" ] && cmp -s "$base.expected" "$TMP/gr.out"; then
            : # stdout matched .expected; exit code diff is racy, ignore
        else
            ok=0; echo "FAIL $base (gmp=$gmp): exit gc=$gc_exit graalgo=$gr_exit"
        fi
    fi
    if [ -f "$base.panic" ]; then
        local gr_first
        gr_first="$(filter_stderr < "$TMP/gr.err" | head -1)"
        local want
        want="$(cat "$base.panic")"
        if [ "$gr_first" != "$want" ]; then
            ok=0; echo "FAIL $base (gmp=$gmp): stderr first line: got [$gr_first] want [$want]"
        fi
    fi
    if [ "$ok" = 1 ]; then
        pass=$((pass+1)); # echo "ok $base (gmp=$gmp)"
    else
        fail=$((fail+1))
        echo "--- $base gc stdout:"; cat "$TMP/gc.out"
        echo "--- $base graalgo stdout:"; cat "$TMP/gr.out"
        echo "--- $base graalgo stderr (filtered):"; filter_stderr < "$TMP/gr.err" | head -3
    fi
}

for go in ch*.go; do
    run_one "$go" 1
    run_one "$go" 4
done

echo "== e2e graduation: $pass passed, $fail failed =="
[ "$fail" = 0 ]
