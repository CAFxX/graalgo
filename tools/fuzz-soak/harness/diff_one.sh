#!/bin/bash
# Single-program differential: gc 1.27.1 vs graalgo P3SyncMain.
# Usage: diff_one.sh <go-file> <workdir> <gmp>
# Emits a JSON result record to stdout.
#
# Backend: the product launcher internal/backend/graalgo-sync-run
# (P3SyncMain), which owns the load-bearing classpath order
# (interop, then runtime, then truffle classes, then Truffle jars).
# This script deliberately does NOT hand-roll a classpath: the old
# phases/phase{1,2,3} classpath was stale and in the wrong order.
# Requires `make build` at the repo root first (classes are build
# outputs, never committed); a missing launcher/classes dir is reported
# as HARNESS_ERROR, never misclassified as a graalgo CRASH.
set -uo pipefail

GO_FILE="$1"
WORKDIR="$2"
GMP="${3:-1}"

# Repo root: this script lives at tools/fuzz-soak/harness/diff_one.sh;
# GR may be overridden (run_soak.sh exports it). Never default to a
# stale worktree name.
if [ -z "${GR:-}" ]; then
  GR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)"
fi
BACKEND="$GR/internal/backend"
export PATH="${GO_BIN_DIR:-$HOME/tools/go/bin}:$PATH"
# The launcher locates java via GRAALVM_HOME; the soak workflow exports
# JAVA_BIN instead, so derive GRAALVM_HOME from it when needed.
if [ -z "${GRAALVM_HOME:-}" ] && [ -n "${JAVA_BIN:-}" ]; then
  export GRAALVM_HOME="$(dirname -- "$(dirname -- "$JAVA_BIN")")"
fi
# JVM temp: FUZZ_TMPDIR (run_soak.sh) > caller's TMPDIR > a tmp dir
# inside this program's workdir (inside --out, never a hidden_files/
# dir under the repo — see repo AGENTS.md §5).
export TMPDIR="${FUZZ_TMPDIR:-${TMPDIR:-$WORKDIR/tmp}}"
mkdir -p "$TMPDIR" "$WORKDIR"

LAUNCHER="$BACKEND/graalgo-sync-run"
if [ ! -x "$LAUNCHER" ] || [ ! -d "$BACKEND/interop/out/classes" ]; then
  base="$(basename "$GO_FILE" .go)"
  printf '{"file": "%s", "result": "HARNESS_ERROR", "detail": "graalgo backend not built; run make build at repo root"}\n' "$base"
  exit 0
fi

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

# 3. graalgo run (120s timeout; JVM startup dominates). The product
# launcher owns the classpath and the java binary (via GRAALVM_HOME);
# TMPDIR above is honored by the launcher for -Djava.io.tmpdir.
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
