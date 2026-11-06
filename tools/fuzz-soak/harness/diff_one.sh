#!/bin/bash
# Single-program differential: gc 1.27.1 vs graalgo P3SyncMain (product
# launcher internal/backend/graalgo-sync-run; requires `make build` at the
# repo root first — this script never builds).
# Usage: diff_one.sh <go-file> <workdir> <gmp>
# Emits a JSON result record to stdout.
set -uo pipefail

GO_FILE="$1"
WORKDIR="$2"
GMP="${3:-1}"

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# Repo root: harness/diff_one.sh -> fuzz-soak -> tools -> repo.
GR="${GR:-$(cd "$HERE/../../.." && pwd)}"
LAUNCHER="$GR/internal/backend/graalgo-sync-run"
export PATH="${GO_BIN_DIR:-$HOME/tools/go/bin}:$PATH"
# The launcher resolves java via GRAALVM_HOME; honor the legacy JAVA_BIN
# override (set by the fuzz-soak workflow) by deriving GRAALVM_HOME from it.
if [ -n "${JAVA_BIN:-}" ] && [ -z "${GRAALVM_HOME:-}" ]; then
  GRAALVM_HOME="$(cd -- "$(dirname -- "$JAVA_BIN")/.." && pwd)"
  export GRAALVM_HOME
fi
mkdir -p "$WORKDIR"
# JVM temp dir: per-program workdir, never a shared repo path.
export TMPDIR="${FUZZ_TMPDIR:-$WORKDIR/gr-tmp}"
mkdir -p "$TMPDIR"

# JSON-escape one string (including the surrounding quotes) via python3.
json_escape() { printf '%s' "$1" | python3 -c 'import json,sys; sys.stdout.write(json.dumps(sys.stdin.read()))'; }

base="$(basename "$GO_FILE" .go)"

# Emit one JSON result record; detail strings are JSON-escaped so a quote
# or backslash in a panic message can never corrupt the record (run_batch.py
# parses the last stdout line as JSON).
emit_result() { # emit_result RESULT DETAIL
  printf '{"file": %s, "result": "%s", "detail": %s}\n' \
    "$(json_escape "$base")" "$1" "$(json_escape "$2")"
}

# Fail fast: a missing launcher/classes must never masquerade as
# WRONG_OUTPUT — a broken classpath yields empty graalgo stdout, which the
# stdout comparison below would report as a divergence. Emit HARNESS_ERROR
# and exit non-zero instead.
missing=""
[ -x "$LAUNCHER" ] || missing="$missing launcher:$LAUNCHER"
for d in "$GR/internal/backend/interop/out/classes" \
         "$GR/internal/backend/runtime/out/classes" \
         "$GR/internal/backend/truffle/out/classes"; do
  [ -d "$d" ] || missing="$missing classes:$d"
done
if ! compgen -G "$GR/internal/backend/truffle/lib/*.jar" > /dev/null; then
  missing="$missing jars:$GR/internal/backend/truffle/lib/*.jar"
fi
[ -x "$GR/internal/frontend/gofrontend" ] || missing="$missing gofrontend:$GR/internal/frontend/gofrontend"
if [ -n "$missing" ]; then
  printf '{"file": %s, "result": "HARNESS_ERROR", "detail": %s}\n' \
    "$(json_escape "$base")" \
    "$(json_escape "backend not built:$missing run 'make build' at the repo root first")"
  exit 1
fi

gcbin="$WORKDIR/$base.gcbin"

# 1. gc build
if ! go build -o "$gcbin" "$GO_FILE" 2> "$WORKDIR/gc_build.err"; then
    emit_result "GC_BUILD_FAIL" "gc build failed"
    exit 0
fi

# 2. gc run (10s timeout)
timeout 10 "$gcbin" > "$WORKDIR/gc.out" 2> "$WORKDIR/gc.err"
gc_exit=$?

# 3. graalgo run (120s timeout; JVM startup dominates). The launcher
# supplies the classpath, JVM flags and tmpdir (from $TMPDIR above).
timeout 120 "$LAUNCHER" --gmp="$GMP" -- "$GO_FILE" \
    > "$WORKDIR/gr.out" 2> "$WORKDIR/gr.err"
gr_exit=$?

# 4. classify
# Scope rejection?
if grep -q "not supported in frontend/v1 scope" "$WORKDIR/gr.err" 2>/dev/null; then
    reason="$(grep -m1 -o "not supported in frontend/v1 scope.*" "$WORKDIR/gr.err" | head -c 120)"
    emit_result "SCOPE" "$reason"
    exit 0
fi

# Hang?
if [ "$gr_exit" = 124 ]; then
    emit_result "HANG" "graalgo timeout 120s"
    exit 0
fi
if [ "$gc_exit" = 124 ]; then
    emit_result "GC_HANG" "gc timeout 10s (generator bug?)"
    exit 0
fi

# JVM crash (not a Go panic)?
if grep -qE "Exception in thread|ClassCastException|FrameSlotTypeException|at graalgo\.|at com\.oracle\.truffle" "$WORKDIR/gr.err" 2>/dev/null; then
    first="$(grep -m1 -E "Exception|Error" "$WORKDIR/gr.err" | head -c 200)"
    emit_result "CRASH" "$first"
    exit 0
fi

# stdout compare
if ! cmp -s "$WORKDIR/gc.out" "$WORKDIR/gr.out"; then
    emit_result "WRONG_OUTPUT" "stdout differs"
    exit 0
fi

# exit code compare
if [ "$gc_exit" != "$gr_exit" ]; then
    emit_result "EXIT_MISMATCH" "gc=$gc_exit graalgo=$gr_exit"
    exit 0
fi

# Go panic message compare (both should panic identically, or neither)
if [ "$gc_exit" != 0 ]; then
    gc_panic="$(head -1 "$WORKDIR/gc.err")"
    gr_panic="$(grep -v "^WARNING: " "$WORKDIR/gr.err" 2>/dev/null | head -1)"
    if [ "$gc_panic" != "$gr_panic" ]; then
        emit_result "GO_PANIC_MISMATCH" "gc=[$gc_panic] gr=[$gr_panic]"
        exit 0
    fi
fi

emit_result "PASS" ""
