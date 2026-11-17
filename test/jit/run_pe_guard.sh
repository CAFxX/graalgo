#!/usr/bin/env bash
# test/jit/run_pe_guard.sh — JIT regression guard (lane T0.B).
#
# Runs test/jit/peguard/main.go under graalgo with the Truffle compiler
# traced and fails when
#   - any compilation fails (opt failed: e.g. PermanentBailoutException
#     "Too deep inlining" because partial evaluation followed a runtime
#     helper into JDK code, or a frame that escapes PE), or
#   - any guard* function, or an OSR loop, never finishes compiling
#     (a silent fallback to the interpreter is a regression too), or
#   - the program output differs from test/jit/peguard/expected.txt
#     (gc's output, recorded with `go run`).
# Compilation is synchronous (BackgroundCompilation=false) so every
# compile the program triggers is finished — and traced — before the
# next call, independent of machine load; the low OSR threshold makes
# main's 3000-iteration loop OSR-compile.
#
# Requires `make build`. Runs in ~10 s.
set -euo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd -- "$HERE/../.." && pwd)"
RUN="${GRAALGO_RUN:-$ROOT/internal/backend/graalgo-sync-run}"
PROG="$HERE/peguard/main.go"
GUARDS="guardCall guardLoop guardIface guardStruct guardAppend guardMap guardDefer"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/graalgo-peguard.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

status=0
JAVA_TOOL_OPTIONS="${PEGUARD_JAVA_OPTS:--Xmx1g} \
 -Dpolyglot.engine.TraceCompilation=true \
 -Dpolyglot.engine.CompilationFailureAction=Print \
 -Dpolyglot.engine.BackgroundCompilation=false \
 -Dpolyglot.engine.OSRCompilationThreshold=1024" \
  timeout 300 "$RUN" "$PROG" >"$WORK/out" 2>"$WORK/err" || status=$?

fail=0
if [ "$status" -ne 0 ]; then
  echo "peguard: program exited $status" >&2
  fail=1
fi
if ! cmp -s "$WORK/out" "$HERE/peguard/expected.txt"; then
  echo "peguard: output differs from gc (expected.txt):" >&2
  diff "$HERE/peguard/expected.txt" "$WORK/out" | head -5 >&2 || true
  fail=1
fi
if grep -q '^\[engine\] opt failed' "$WORK/err"; then
  echo "peguard: compilation failures:" >&2
  grep '^\[engine\] opt failed' "$WORK/err" | cut -c1-240 >&2
  # The first graalgo frames of each failure's inlining stack name the
  # node or helper that partial evaluation should not have entered.
  grep -A400 '^\[engine\] opt failed' "$WORK/err" | grep -m 12 '^graalgo\.\|at graalgo\.' >&2 || true
  fail=1
fi
for g in $GUARDS; do
  if ! grep -q "^\[engine\] opt done .*GoFuncRootNode($g)" "$WORK/err"; then
    echo "peguard: $g was never compiled" >&2
    fail=1
  fi
done
if ! grep -q '^\[engine\] opt done .*<OSR>' "$WORK/err"; then
  echo "peguard: no loop was OSR-compiled" >&2
  fail=1
fi
if [ "$fail" -ne 0 ]; then
  echo "peguard: FAIL" >&2
  exit 1
fi
echo "peguard: OK ($(grep -c '^\[engine\] opt done' "$WORK/err") compilations, none failed)"
