#!/usr/bin/env bash
# test/jit/run_pe_guard.sh — JIT regression guard (lane T0.B).
#
# Runs test/jit/peguard/main.go under graalgo with the Truffle compiler
# traced, in two modes:
#   hot     — default tier thresholds: the guard* functions compile after
#             warm-up with their inline caches filled (specialized code);
#             the low OSR threshold makes main's 3000-iteration loop OSR.
#   breadth — CompileImmediately: every call target the program executes
#             (including the fmt/strings/reflect/sync stdlib code it
#             drags in) is compiled on first call.
# Either mode fails the guard when
#   - any compilation fails (opt failed: e.g. PermanentBailoutException
#     "Too deep inlining" because partial evaluation followed a runtime
#     helper into JDK code, or a frame that escapes PE), or
#   - the program output differs from test/jit/peguard/expected.txt
#     (gc's output, recorded with `go run`);
# the hot mode also fails when a guard* function or an OSR loop never
# finishes compiling (a silent fallback to the interpreter is a
# regression too). Compilation is synchronous (BackgroundCompilation=
# false), so every compile is finished — and traced — before the program
# continues, independent of machine load.
#
# The guard also fails, with a message naming the cause rather than a
# misleading output diff, when
#   - the program crashes (non-zero exit: the tail of its stderr is
#     printed), or
#   - the JVM has no optimizing Truffle runtime (Truffle fell back to the
#     interpreter-only runtime: wrong JDK, JVMCI disabled, jars missing) —
#     nothing can compile there, so every other check would be noise.
#
# Heap: the frontend/v1 JSON IR for this program (fmt + strings closure)
# is ~120 MB and the AST build peaks at ~1 GB live, so the default heap is
# 2 GB. With -Xmx1g the build dies with "Java heap space" before main runs
# — the CI failure of 2026-11-18 (ubuntu-24.04 runners: a 1g cap OOMs on
# any core count). Override with PEGUARD_JAVA_OPTS.
#
# Requires `make build`. Runs in ~30 s.
set -euo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd -- "$HERE/../.." && pwd)"
RUN="${GRAALGO_RUN:-$ROOT/internal/backend/graalgo-sync-run}"
PROG="$HERE/peguard/main.go"
GUARDS="guardCall guardLoop guardIface guardStruct guardAppend guardMap guardDefer"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/graalgo-peguard.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

fail=0
# run_mode <name> <extra -D options...>
run_mode() {
  local mode="$1"; shift
  local out="$WORK/$mode.out" err="$WORK/$mode.err" status=0
  JAVA_TOOL_OPTIONS="${PEGUARD_JAVA_OPTS:--Xmx2g} \
 -Dpolyglot.engine.TraceCompilation=true \
 -Dpolyglot.engine.CompilationFailureAction=Print \
 -Dpolyglot.engine.BackgroundCompilation=false $*" \
    timeout 300 "$RUN" "$PROG" >"$out" 2>"$err" || status=$?
  if grep -q 'uses a fallback runtime that does not support runtime compilation' "$err"; then
    echo "peguard[$mode]: FAIL: the JVM has no optimizing Truffle runtime (fallback" >&2
    echo "  interpreter-only runtime), so nothing can be compiled. Use the pinned" >&2
    echo "  Oracle GraalVM JDK 25 (docs/dev/ENV.md) with JVMCI enabled. Truffle says:" >&2
    grep -m1 'The following cause was found' "$err" | sed 's/^/  /' >&2 || true
    exit 1
  fi
  if [ "$status" -ne 0 ]; then
    echo "peguard[$mode]: program exited $status; stderr tail:" >&2
    grep -v '^\[engine\] opt \|^Picked up JAVA_TOOL_OPTIONS' "$err" | tail -n 15 | cut -c1-300 | sed 's/^/  /' >&2 || true
    fail=1
  fi
  if ! cmp -s "$out" "$HERE/peguard/expected.txt"; then
    echo "peguard[$mode]: output differs from gc (expected.txt):" >&2
    diff "$HERE/peguard/expected.txt" "$out" | head -5 >&2 || true
    fail=1
  fi
  if grep -q '^\[engine\] opt failed' "$err"; then
    echo "peguard[$mode]: compilation failures:" >&2
    grep '^\[engine\] opt failed' "$err" | cut -c1-240 >&2
    # The first graalgo frames of each failure's inlining stack name the
    # node or helper that partial evaluation should not have entered.
    grep -A400 '^\[engine\] opt failed' "$err" | grep -m 12 '^graalgo\.\|at graalgo\.' >&2 || true
    fail=1
  fi
  echo "peguard[$mode]: $(grep -c '^\[engine\] opt done' "$err" || true) compilations"
}

run_mode hot -Dpolyglot.engine.OSRCompilationThreshold=1024
for g in $GUARDS; do
  if ! grep -q "^\[engine\] opt done .*GoFuncRootNode($g)" "$WORK/hot.err"; then
    echo "peguard[hot]: $g was never compiled" >&2
    fail=1
  fi
done
if ! grep -q '^\[engine\] opt done .*<OSR>' "$WORK/hot.err"; then
  echo "peguard[hot]: no loop was OSR-compiled" >&2
  fail=1
fi
run_mode breadth -Dpolyglot.engine.CompileImmediately=true

if [ "$fail" -ne 0 ]; then
  echo "peguard: FAIL" >&2
  exit 1
fi
echo "peguard: OK (no compilation failed)"
