#!/bin/bash
# Single-program differential: gc 1.27.1 vs graalgo P3SyncMain
# (internal/backend/graalgo-sync-run).
# Usage: diff_one.sh <go-file> <workdir> <gmp>
# Emits a JSON result record to stdout.
#
# Prerequisites (backend classes + gofrontend) are built once by the
# caller (harness/run_batch.py step 1); this script never builds. Missing
# prerequisites surface as HARNESS_ERROR, never as a diff verdict.
set -uo pipefail

GO_FILE="$1"
WORKDIR="$2"
GMP="${3:-1}"

# Repo root: harness/diff_one.sh -> fuzz-soak -> tools -> repo.
GR="${GR:-$(cd "$(dirname "$0")/../../.." && pwd)}"
BE="$GR/internal/backend"
export PATH="${GO_BIN_DIR:-$HOME/tools/go/bin}:$PATH"
export GOTOOLCHAIN=local
if [ -z "${GRAALVM_HOME:-}" ] && [ -n "${JAVA_BIN:-}" ]; then
    GRAALVM_HOME="$(cd "$(dirname "$JAVA_BIN")/.." && pwd)"
    export GRAALVM_HOME
fi
# Tmp discipline: never write to /tmp (512M shared tmpfs on this VM), and
# the repo's old hidden_files/ scratch convention was retired with the
# phases/ tree. FUZZ_TMPDIR (exported by run_soak.sh) wins, then a
# caller-set TMPDIR, then the workspace-disk default.
export TMPDIR="${FUZZ_TMPDIR:-${TMPDIR:-$HOME/workspace/.tmp-fuzz-soak}}"
mkdir -p "$TMPDIR"

base="$(basename "$GO_FILE" .go)"
gcbin="$WORKDIR/$base.gcbin"

# 0. prerequisites (built once by the caller)
if [ ! -x "$BE/graalgo-sync-run" ]; then
    printf '{"file": "%s", "result": "HARNESS_ERROR", "detail": "missing launcher %s/graalgo-sync-run"}\n' "$base" "$BE"
    exit 0
fi
if [ ! -x "$GR/internal/frontend/gofrontend" ]; then
    printf '{"file": "%s", "result": "HARNESS_ERROR", "detail": "missing gofrontend binary (internal/frontend/gofrontend not built)"}\n' "$base"
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

# 3. graalgo run (120s timeout; JVM startup dominates). The product
# launcher owns the load-bearing classpath order (interop -> runtime ->
# truffle) and points -Djava.io.tmpdir at $TMPDIR.
timeout 120 "$BE/graalgo-sync-run" --gmp="$GMP" -- "$GO_FILE" \
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
