#!/bin/bash
# Single-program differential: gc 1.27.1 vs graalgo P3SyncMain.
# Usage: diff_one.sh <go-file> <workdir> <gmp>
# Emits a JSON result record to stdout.
set -uo pipefail

GO_FILE="$1"
WORKDIR="$2"
GMP="${3:-1}"

GR="${GR:-$HOME/workspace/graalgo-m9}"
export PATH="${GO_BIN_DIR:-$HOME/tools/go/bin}:$PATH"
# Workspace-local scratch (the soak driver passes FUZZ_TMPDIR); never /tmp.
export TMPDIR="${FUZZ_TMPDIR:-$HOME/workspace/.tmp-graalgo-fuzz}"
JAVA_BIN="${JAVA_BIN:-$HOME/tools/graalvm-jdk-25/bin/java}"

# The graalgo leg delegates to the product launcher
# (internal/backend/graalgo-sync-run): it builds the classpath from
# internal/backend/{interop,runtime,truffle}/out/classes plus the truffle
# lib jars (classpath order is load-bearing: interop first, then runtime
# so the runtime module's same-FQN replacements shadow the truffle
# module's frozen stubs, then truffle) and invokes
# graalgo.p3sync.launcher.P3SyncMain with these args. -Djava.io.tmpdir
# follows $TMPDIR (never /tmp).
if [ -z "${GRAALVM_HOME:-}" ]; then
    export GRAALVM_HOME="$(cd "$(dirname "$JAVA_BIN")/.." && pwd)"
fi
LAUNCHER="$GR/internal/backend/graalgo-sync-run"

base="$(basename "$GO_FILE" .go)"
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
timeout 120 "$LAUNCHER" --gmp="$GMP" -- "$GO_FILE" \
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
