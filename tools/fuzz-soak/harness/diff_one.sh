#!/bin/bash
# Single-program differential: gc 1.27.1 vs graalgo P3SyncMain.
# Usage: diff_one.sh <go-file> <workdir> <gmp>
# Emits a JSON result record to stdout.
set -uo pipefail

GO_FILE="$1"
WORKDIR="$2"
GMP="${3:-1}"

# Repo root: this script lives at tools/fuzz-soak/harness/ in the checkout.
GR="${GR:-$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)}"
export PATH="${GO_BIN_DIR:-$HOME/tools/go/bin}:$PATH"
# graalgo-sync-run resolves its JVM from GRAALVM_HOME (default
# $HOME/tools/graalvm-jdk-25); a JAVA_BIN override maps onto it so a
# caller-set java keeps working.
if [ -z "${GRAALVM_HOME:-}" ] && [ -n "${JAVA_BIN:-}" ]; then
    export GRAALVM_HOME="$(cd -- "$(dirname -- "$JAVA_BIN")/.." && pwd)"
fi
# Tmp discipline: never write to /tmp. run_soak.sh presets FUZZ_TMPDIR
# to its --out dir; otherwise default under the repo's .out/ scratch root.
export TMPDIR="${FUZZ_TMPDIR:-$GR/.out/fuzz-soak-tmp}"
mkdir -p "$TMPDIR"

GRAALGO_SYNC_RUN="$GR/internal/backend/graalgo-sync-run"

base="$(basename "$GO_FILE" .go)"
gcbin="$WORKDIR/$base.gcbin"

# The wrapper never builds: fail fast (as HARNESS_ERROR, not CRASH) when
# the backend classes are missing instead of misclassifying a JVM
# classpath failure as a graalgo crash. run_batch.py builds first, so
# this only fires for standalone diff_one.sh runs.
if [ ! -d "$GR/internal/backend/interop/out/classes" ]; then
    printf '{"file": "%s", "result": "HARNESS_ERROR", "detail": "backend classes not built; run make build at the repo root"}\n' "$base"
    exit 0
fi

# 1. gc build
if ! go build -o "$gcbin" "$GO_FILE" 2> "$WORKDIR/gc_build.err"; then
    printf '{"file": "%s", "result": "GC_BUILD_FAIL", "detail": "gc build failed"}\n' "$base"
    exit 0
fi

# 2. gc run (10s timeout)
timeout 10 "$gcbin" > "$WORKDIR/gc.out" 2> "$WORKDIR/gc.err"
gc_exit=$?

# 3. graalgo run (120s timeout; JVM startup dominates) via the product
# launcher wrapper (P3SyncMain; classpath order lives in the wrapper).
# TMPDIR reaches the JVM as java.io.tmpdir through the wrapper.
timeout 120 "$GRAALGO_SYNC_RUN" --gmp="$GMP" -- "$GO_FILE" \
    > "$WORKDIR/gr.out" 2> "$WORKDIR/gr.err"
gr_exit=$?

# 4. classify
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
    first="$(grep -m1 -E "Exception|Error" "$WORKDIR/gr.err" | head -c 200)"
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
    gc_panic="$(head -1 "$WORKDIR/gc.err")"
    gr_panic="$(grep -v "^WARNING: " "$WORKDIR/gr.err" 2>/dev/null | head -1)"
    if [ "$gc_panic" != "$gr_panic" ]; then
        printf '{"file": "%s", "result": "GO_PANIC_MISMATCH", "detail": "gc=[%s] gr=[%s]"}\n' "$base" "$gc_panic" "$gr_panic"
        exit 0
    fi
fi

printf '{"file": "%s", "result": "PASS", "detail": ""}\n' "$base"
