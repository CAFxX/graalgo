#!/bin/bash
# Differential runner for the P3-M3 interface-dispatch corpus (ADR-0003):
# each program runs under stock gc 1.27.1 (built binary, run directly —
# `go run` masks the exit code) and under graalgo (P3SyncMain, which
# registers the "ifacemethod" wire kind and installs the method tables).
# Compares stdout bytes, exit codes, and — for panic programs — the first
# stderr line.
#
# Known JVM Unsafe warnings on stderr are filtered (harness noise, cf.
# p3frontend/DESIGN.md §7). Programs are single-goroutine; one --gmp=1
# run each (the sync corpus's gmp sweep covers the scheduler).
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
export PATH="$HOME/tools/go/bin:$PATH"

echo "== graalgo backend: $GRAALGO_SYNC_RUN =="


TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0; fail=0
# filter_stderr: drop the known JVM Unsafe warning lines.
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
    cmp -s "$TMP/gc.out" "$TMP/gr.out" || { ok=0; echo "FAIL $base: stdout differs"; }
    if [ "$gc_exit" != "$gr_exit" ]; then
        ok=0; echo "FAIL $base: exit gc=$gc_exit graalgo=$gr_exit"
    fi
    if [ -f "$base.panic" ]; then
        local gr_first
        gr_first="$(filter_stderr < "$TMP/gr.err" | head -1)"
        local want
        want="$(cat "$base.panic")"
        if [ "$gr_first" != "$want" ]; then
            ok=0; echo "FAIL $base: stderr first line: got [$gr_first] want [$want]"
        fi
    fi
    if [ "$ok" = 1 ]; then
        pass=$((pass+1)); # echo "ok $base"
    else
        fail=$((fail+1))
        echo "--- $base gc stdout:"; cat "$TMP/gc.out"
        echo "--- $base graalgo stdout:"; cat "$TMP/gr.out"
        echo "--- $base graalgo stderr (filtered):"; filter_stderr < "$TMP/gr.err" | head -3
    fi
}

for go in if*.go; do
    run_one "$go"
done

echo "== differential: $pass passed, $fail failed =="
[ "$fail" = 0 ]
