#!/bin/bash
# Single-program differential: gc 1.27.1 vs graalgo P3SyncMain.
# Usage: diff_one.sh <go-file> <workdir> <gmp>
# Emits a JSON result record to stdout.
set -uo pipefail

GO_FILE="$1"
WORKDIR="$2"
GMP="${3:-1}"

GR="${GR:-$(cd "$HERE/../.." && pwd)}"
BACKEND="$GR/internal/backend"
# Classpath order is load-bearing: interop FIRST, then runtime (so the
# runtime module's same-FQN replacements shadow the truffle module's frozen
# stubs), then truffle, then the Truffle jars — same order as the interop
# module Makefile's RUN_CP and internal/backend/graalgo-sync-run.
CP="$BACKEND/interop/out/classes:$BACKEND/runtime/out/classes:$BACKEND/truffle/out/classes"
for jar in "$BACKEND"/truffle/lib/*.jar; do
    CP="$CP:$jar"
done
export PATH="${GO_BIN_DIR:-$HOME/tools/go/bin}:$PATH"
JAVA="${JAVA_BIN:-$HOME/tools/graalvm-jdk-25/bin/java}"
# Scratch: explicit FUZZ_TMPDIR (run_soak.sh sets it under the out dir),
# else inherited TMPDIR, else the program workdir — never /tmp or the
# banned hidden_files/ tree.
export TMPDIR="${FUZZ_TMPDIR:-${TMPDIR:-$WORKDIR/tmp}}"
mkdir -p "$TMPDIR" || { echo "diff_one.sh: cannot create TMPDIR=$TMPDIR" >&2; exit 2; }

base="$(basename "$GO_FILE" .go)"
gcbin="$WORKDIR/$base.gcbin"

# json_emit <file> <result> <detail>: emits one JSON result record to
# stdout. Details are JSON-escaped via python3 (T8 fix): raw printf
# interpolation produced invalid JSON whenever a log excerpt contained
# a quote or backslash, turning real CRASH findings into HARNESS_ERROR
# noise downstream.
json_emit() {
    python3 -c 'import json,sys; print(json.dumps({"file": sys.argv[1], "result": sys.argv[2], "detail": sys.argv[3]}))' \
        "$1" "$2" "$3"
}

# 1. gc build
if ! go build -o "$gcbin" "$GO_FILE" 2> "$WORKDIR/gc_build.err"; then
    json_emit "$base" "GC_BUILD_FAIL" "gc build failed"
    exit 0
fi

# 2. gc run (10s timeout)
timeout 10 "$gcbin" > "$WORKDIR/gc.out" 2> "$WORKDIR/gc.err"
gc_exit=$?

# 3. graalgo run (120s timeout; JVM startup dominates)
timeout 120 "$JAVA" --enable-native-access=ALL-UNNAMED -Xss32m \
    -Djava.io.tmpdir="$TMPDIR" -cp "$CP" \
    graalgo.p3sync.launcher.P3SyncMain --gmp="$GMP" -- "$GO_FILE" \
    > "$WORKDIR/gr.out" 2> "$WORKDIR/gr.err"
gr_exit=$?

# 4. classify
# Scope rejection?
if grep -q "not supported in frontend/v1 scope" "$WORKDIR/gr.err" 2>/dev/null; then
    reason="$(grep -m1 -o "not supported in frontend/v1 scope.*" "$WORKDIR/gr.err" | head -c 120)"
    json_emit "$base" "SCOPE" "$reason"
    exit 0
fi

# Hang?
if [ "$gr_exit" = 124 ]; then
    json_emit "$base" "HANG" "graalgo timeout 120s"
    exit 0
fi
if [ "$gc_exit" = 124 ]; then
    json_emit "$base" "GC_HANG" "gc timeout 10s (generator bug?)"
    exit 0
fi

# JVM crash (not a Go panic)?
if grep -qE "Exception in thread|ClassCastException|FrameSlotTypeException|at graalgo\.|at com\.oracle\.truffle" "$WORKDIR/gr.err" 2>/dev/null; then
    first="$(grep -m1 -E "Exception|Error" "$WORKDIR/gr.err" | head -c 200)"
    json_emit "$base" "CRASH" "$first"
    exit 0
fi

# stdout compare
if ! cmp -s "$WORKDIR/gc.out" "$WORKDIR/gr.out"; then
    json_emit "$base" "WRONG_OUTPUT" "stdout differs"
    exit 0
fi

# exit code compare
if [ "$gc_exit" != "$gr_exit" ]; then
    json_emit "$base" "EXIT_MISMATCH" "gc=$gc_exit graalgo=$gr_exit"
    exit 0
fi

# Go panic message compare (both should panic identically, or neither)
if [ "$gc_exit" != 0 ]; then
    gc_panic="$(head -1 "$WORKDIR/gc.err")"
    gr_panic="$(grep -v "^WARNING: " "$WORKDIR/gr.err" 2>/dev/null | head -1)"
    if [ "$gc_panic" != "$gr_panic" ]; then
        json_emit "$base" "GO_PANIC_MISMATCH" "gc=[$gc_panic] gr=[$gr_panic]"
        exit 0
    fi
fi

json_emit "$base" "PASS" ""
