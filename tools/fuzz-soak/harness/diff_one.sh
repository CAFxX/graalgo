#!/bin/bash
# Single-program differential: gc 1.27.1 vs graalgo P3SyncMain.
# Usage: diff_one.sh <go-file> <workdir> <gmp>
# Emits a JSON result record to stdout.
# Env: GR (repo root; default: derived from this script's location),
#   GO_BIN_DIR (Go toolchain bin dir), JAVA_BIN or GRAALVM_HOME (JVM),
#   FUZZ_TMPDIR (JVM temp dir override; default: <workdir>/tmp).
# Requires `make build` at the repo root first (backend classes).
set -uo pipefail

GO_FILE="$1"
WORKDIR="$2"
GMP="${3:-1}"

# Repo root: harness/ -> fuzz-soak/ -> tools/ -> repo. $GR overrides
# (run_soak.sh exports it; the GHA image layout derives it the same way).
if [ -z "${GR:-}" ]; then
  GR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)"
fi
export PATH="${GO_BIN_DIR:-$HOME/tools/go/bin}:$PATH"
# The graalgo run goes through internal/backend/graalgo-sync-run, which
# owns the load-bearing classpath (interop/out/classes, then
# runtime/out/classes, then truffle/out/classes, then truffle/lib/*.jar)
# and the JVM flags — diff_one.sh must not duplicate it. JAVA_BIN (a java
# binary path) is translated to GRAALVM_HOME for the wrapper.
if [ -z "${GRAALVM_HOME:-}" ] && [ -n "${JAVA_BIN:-}" ]; then
  GRAALVM_HOME="$(dirname "$(dirname "$JAVA_BIN")")"
fi
export GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
# JVM temp dir: per-workdir, never /tmp (a full /tmp silently yields
# empty output with exit 0) and never a hidden_files/ scratch dir.
export TMPDIR="${FUZZ_TMPDIR:-$WORKDIR/tmp}"
mkdir -p "$TMPDIR"

WRAPPER="$GR/internal/backend/graalgo-sync-run"

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

# 3. graalgo run (120s timeout; JVM startup dominates). Fail fast as
# HARNESS_ERROR if the backend was never built: without classes, java
# would die with NoClassDefFoundError and misclassify as CRASH.
if [ ! -x "$WRAPPER" ] || [ ! -d "$GR/internal/backend/interop/out/classes" ]; then
    printf '{"file": "%s", "result": "HARNESS_ERROR", "detail": "backend not built: run make build at the repo root first"}\n' "$base"
    exit 0
fi
timeout 120 "$WRAPPER" --gmp="$GMP" -- "$GO_FILE" \
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
