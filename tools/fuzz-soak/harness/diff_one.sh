#!/bin/bash
# Single-program differential: gc 1.27.1 vs graalgo (product launcher
# internal/backend/graalgo-sync-run, P3SyncMain).
# Usage: diff_one.sh <go-file> <workdir> <gmp>
# Emits ONE JSON result record to stdout.
#
# Results: PASS, GC_BUILD_FAIL, GC_HANG, SCOPE, HANG, CRASH, WRONG_OUTPUT,
# EXIT_MISMATCH, GO_PANIC_MISMATCH — and INFRA when no verdict is possible
# (launcher/java/backend build missing, JVM class-load failure, corrupted
# frontend export). An infrastructure failure is never reported as a
# divergence (it used to surface as a bogus WRONG_OUTPUT when the launcher
# classpath pointed at the deleted pre-restructure tree).
# Exit status: 0 a verdict was emitted (whatever it is), 3 INFRA.
set -uo pipefail

GO_FILE="$1"
WORKDIR="$2"
GMP="${3:-1}"
base="$(basename "$GO_FILE" .go)"

infra() {
    printf '{"file": "%s", "result": "INFRA", "detail": "%s"}\n' "$base" "$1"
    exit 3
}

# shellcheck source=../../../test/lib/infra.sh
. "$(dirname "$0")/../../../test/lib/infra.sh"
( infra_setup_go && infra_setup_graalgo ) > "$WORKDIR/infra.err" 2>&1 \
    || infra "$(head -c 200 "$WORKDIR/infra.err" | tr '"\n' "' ")"
infra_setup_go
infra_setup_graalgo
export TMPDIR="${FUZZ_TMPDIR:-$WORKDIR}"

gcbin="$WORKDIR/$base.gcbin"

# 1. gc build
if ! go build -o "$gcbin" "$GO_FILE" 2> "$WORKDIR/gc_build.err"; then
    printf '{"file": "%s", "result": "GC_BUILD_FAIL", "detail": "gc build failed"}\n' "$base"
    exit 0
fi

# 2. gc run (10s timeout)
timeout 10 "$gcbin" > "$WORKDIR/gc.out" 2> "$WORKDIR/gc.err"
gc_exit=$?

# 3. graalgo run (120s timeout; JVM startup dominates)
timeout 120 "${GRAALGO_RUN[@]}" --gmp="$GMP" "$GO_FILE" \
    > "$WORKDIR/gr.out" 2> "$WORKDIR/gr.err"
gr_exit=$?

# 4. classify — infrastructure first: no verdict is possible.
if infra_is_failure "$gr_exit" "$WORKDIR/gr.err" \
        || grep -qE 'JSON parse error|JSON encoding failed' "$WORKDIR/gr.err" 2>/dev/null; then
    infra "$(grep -m1 -E 'Error|Exception|JSON|not found' "$WORKDIR/gr.err" | head -c 200 | tr '"' "'")"
fi

# Scope rejection?
if grep -q "not supported in frontend/v1 scope" "$WORKDIR/gr.err" 2>/dev/null; then
    reason="$(grep -m1 -o "not supported in frontend/v1 scope.*" "$WORKDIR/gr.err" | head -c 120)"
    printf '{"file": "%s", "result": "SCOPE", "detail": "%s"}\n' "$base" "$reason"
    exit 0
fi

# Hang?
if [ "$gr_exit" = 124 ]; then
    printf '{"file": "%s", "result": "HANG", "detail": "graalgo timeout 120s"}\n' "$base"
    exit 0
fi
if [ "$gc_exit" = 124 ]; then
    printf '{"file": "%s", "result": "GC_HANG", "detail": "gc timeout 10s (generator bug?)"}\n' "$base"
    exit 0
fi

# JVM crash (not a Go panic)?
if grep -qE "Exception in thread|ClassCastException|FrameSlotTypeException|at graalgo\.|at com\.oracle\.truffle" "$WORKDIR/gr.err" 2>/dev/null; then
    first="$(grep -m1 -E "Exception|Error" "$WORKDIR/gr.err" | head -c 200 | tr '"' "'")"
    printf '{"file": "%s", "result": "CRASH", "detail": "%s"}\n' "$base" "$first"
    exit 0
fi

# stdout compare
if ! cmp -s "$WORKDIR/gc.out" "$WORKDIR/gr.out"; then
    printf '{"file": "%s", "result": "WRONG_OUTPUT", "detail": "stdout differs"}\n' "$base"
    exit 0
fi

# exit code compare
if [ "$gc_exit" != "$gr_exit" ]; then
    printf '{"file": "%s", "result": "EXIT_MISMATCH", "detail": "gc=%s graalgo=%s"}\n' "$base" "$gc_exit" "$gr_exit"
    exit 0
fi

# Go panic message compare (both should panic identically, or neither)
if [ "$gc_exit" != 0 ]; then
    gc_panic="$(head -1 "$WORKDIR/gc.err" | tr '"' "'")"
    gr_panic="$(grep -v "^WARNING: " "$WORKDIR/gr.err" 2>/dev/null | head -1 | tr '"' "'")"
    if [ "$gc_panic" != "$gr_panic" ]; then
        printf '{"file": "%s", "result": "GO_PANIC_MISMATCH", "detail": "gc=[%s] gr=[%s]"}\n' "$base" "$gc_panic" "$gr_panic"
        exit 0
    fi
fi

printf '{"file": "%s", "result": "PASS", "detail": ""}\n' "$base"
