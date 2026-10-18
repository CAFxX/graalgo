#!/bin/bash
# Single-program differential: gc 1.27.1 vs graalgo (product launcher).
# Usage: diff_one.sh <go-file> <workdir> <gmp>
# Emits a JSON result record to stdout.
#
# The graalgo leg runs through the supported product launcher wrapper
# (internal/backend/graalgo-sync-run -> graalgo.p3sync.launcher.P3SyncMain),
# the same backend the per-push snapshot gate records against (Makefile:
# backends-p3.json; the phase-1 graalgo-run has no channel/scheduler
# support). The wrapper assembles its own classpath; this script never
# hand-builds one. It needs `make build` outputs
# (internal/backend/{runtime,interop}/out/classes) to exist — the batch
# driver builds them once per batch before invoking this script.
set -uo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# Repo root: harness/diff_one.sh -> tools/fuzz-soak -> tools -> repo.
GR="${GR:-$(cd "$HERE/../../.." && pwd)}"
RUNNER="$GR/internal/backend/graalgo-sync-run"

GO_FILE="$1"
WORKDIR="$2"
GMP="${3:-1}"

export PATH="${GO_BIN_DIR:-$HOME/tools/go/bin}:$PATH"
export TMPDIR="${FUZZ_TMPDIR:-$WORKDIR/tmp}"
mkdir -p "$TMPDIR"

base="$(basename "$GO_FILE" .go)"
gcbin="$WORKDIR/$base.gcbin"

harness_error() {
    printf '{"file": "%s", "result": "HARNESS_ERROR", "detail": "%s"}\n' \
        "$base" "$1"
    exit 0
}

# 0. launcher sanity: a missing/unbuilt backend is a harness problem, not a
#    program verdict — report it as HARNESS_ERROR, never as a crash.
if [ ! -x "$RUNNER" ]; then
    harness_error "launcher missing: $RUNNER"
fi
if [ ! -d "$GR/internal/backend/runtime/out/classes" ] || \
   [ ! -d "$GR/internal/backend/interop/out/classes" ]; then
    harness_error "backend not built: run 'make build' at the repo root first"
fi

# 1. gc build
if ! go build -o "$gcbin" "$GO_FILE" 2> "$WORKDIR/gc_build.err"; then
    printf '{"file": "%s", "result": "GC_BUILD_FAIL", "detail": "gc build failed"}\n' "$base"
    exit 0
fi

# 2. gc run (10s timeout)
timeout 10 "$gcbin" > "$WORKDIR/gc.out" 2> "$WORKDIR/gc.err"
gc_exit=$?

# 3. graalgo run (120s timeout; JVM startup dominates)
timeout 120 "$RUNNER" --gmp="$GMP" "$GO_FILE" \
    > "$WORKDIR/gr.out" 2> "$WORKDIR/gr.err"
gr_exit=$?

# 4. classify
# Scope rejection? (P3SyncMain reports it as "internal error building AST"
# with the frontend/v1 scope message; it is the program's fault, not a bug.)
if grep -qE "not supported in frontend/v1 scope|not supported in v1" "$WORKDIR/gr.err" 2>/dev/null; then
    reason="$(grep -m1 -oE "not supported in (frontend/)?v1 scope.*" "$WORKDIR/gr.err" | head -c 120)"
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

# JVM crash or launcher-internal failure (never the program's fault)?
# (Checked AFTER the scope rejection above: P3SyncMain reports scope
# rejections through the same "internal error building AST" line.)
if grep -qE "Exception in thread|ClassCastException|FrameSlotTypeException|at graalgo\.|at com\.oracle\.truffle|^graalgo-sync-run: internal error|^graalgo-sync-run: execution failed" "$WORKDIR/gr.err" 2>/dev/null; then
    first="$(grep -m1 -E "Exception|Error|graalgo-sync-run: " "$WORKDIR/gr.err" | head -c 200)"
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
