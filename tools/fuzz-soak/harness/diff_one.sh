#!/bin/bash
# Single-program differential: gc 1.27.1 vs graalgo-sync-run (P3SyncMain).
# Usage: diff_one.sh <go-file> <workdir> <gmp>
# Emits a JSON result record to stdout.
#
# Requires `make build` at the repo root first (run_batch.py builds once);
# this script never builds and never reconstructs a classpath: the
# differential leg goes through internal/backend/graalgo-sync-run, which
# owns the (load-bearing) classpath order. The old phases/phase1|2|3
# classpath layout was removed with the phases/ tree on 2026-10-14 and
# must not be reintroduced here.
set -uo pipefail

GO_FILE="$1"
WORKDIR="$2"
GMP="${3:-1}"

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# Repo root: tools/fuzz-soak/harness/diff_one.sh -> fuzz-soak -> tools -> repo.
if [ -n "${GR:-}" ]; then
  :
else
  GR="$(cd "$HERE/../../.." && pwd)"
fi
export PATH="${GO_BIN_DIR:-$HOME/tools/go/bin}:$PATH"
# graalgo-sync-run resolves java via GRAALVM_HOME; honor a JAVA_BIN
# override (set by the CI workflow and run_soak.sh) when GRAALVM_HOME
# is not already set.
if [ -n "${JAVA_BIN:-}" ] && [ -z "${GRAALVM_HOME:-}" ]; then
  export GRAALVM_HOME="$(cd -- "$(dirname -- "$JAVA_BIN")/.." && pwd)"
fi
# Keep JVM temp files out of /tmp (small shared tmpfs; a full /tmp
# silently yields empty output with exit 0) and out of any hidden_files/
# scratch dir: per-workdir tmp by default, shared batch tmp when set.
export TMPDIR="${FUZZ_TMPDIR:-$WORKDIR/tmp}"
mkdir -p "$TMPDIR"

LAUNCHER="$GR/internal/backend/graalgo-sync-run"
if [ ! -x "$LAUNCHER" ]; then
    printf '{"file": "%s", "result": "HARNESS_ERROR", "detail": "graalgo-sync-run missing/not executable; run make build first"}\n' "$(basename "$GO_FILE" .go)"
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

# 3. graalgo run (120s timeout; JVM startup dominates)
# P3SyncMain usage: graalgo-sync-run [--gmp=N] [--check] file.go [-- args...]
timeout 120 "$LAUNCHER" --gmp="$GMP" -- "$GO_FILE" \
    > "$WORKDIR/gr.out" 2> "$WORKDIR/gr.err"
gr_exit=$?

# 4. classify
# Scope rejection? (frontend/v1 scope errors are still worded this way;
# verified against internal/frontend scopeErrorf call sites.)
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
