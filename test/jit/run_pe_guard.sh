#!/usr/bin/env bash
# test/jit/run_pe_guard.sh — JIT regression guard (lane T0.B; on the new
# path since m3-plan D3).
#
# Builds test/jit/peguard/main.go through graalgo-tool with the
# standard-library fork (test/lib/toolexec.sh) and runs the linked bundle on
# the new node package with the Truffle compiler traced, in two modes:
#   hot     — default tier thresholds: the guard* functions compile after
#             warm-up with their inline caches filled (specialized code);
#             the low OSR threshold makes main's 3000-iteration loop OSR.
#   breadth — CompileImmediately: every call target the program executes
#             (including the upstream fmt/strings/reflect/sync code it
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
# continues, independent of machine load. pe-check-m1a covers the benchmark
# set and the std paths under ExitVM; this guard adds the named-function and
# OSR checks and the CompileImmediately breadth over the whole program.
#
# The guard also fails, with a message naming the cause rather than a
# misleading output diff, when
#   - the program crashes (non-zero exit: the tail of its stderr is
#     printed), or
#   - the JVM has no optimizing Truffle runtime (Truffle fell back to the
#     interpreter-only runtime: wrong JDK, JVMCI disabled, jars missing) —
#     nothing can compile there, so every other check would be noise.
#
# Env: GO, GRAALVM_HOME, PEGUARD_JAVA_OPTS (default -Xmx2g
# -XX:ActiveProcessorCount=4). Exit: the infra.sh contract.
# Requires `make build`.
set -euo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/infra.sh
. "$HERE/../lib/infra.sh"
ROOT="$GRAALGO_ROOT"
GUARDS="guardCall guardLoop guardIface guardStruct guardAppend guardMap guardDefer"

infra_setup_go
GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
export GRAALVM_HOME
RUNNER="$ROOT/internal/backend/graalgo-bundle-run"
[ -f "$ROOT/internal/backend/truffle/out/classes/graalgo/launcher/BundleMain.class" ] \
  || infra_die "backend not built (BundleMain missing) — run \`make build\` at the repo root"
infra_mktemp peguard
WORK="$TMP"
# shellcheck source=../lib/toolexec.sh
. "$ROOT/test/lib/toolexec.sh"
toolexec_setup
PROG="$WORK/peguard.bin"
( cd "$HERE/peguard" && toolexec_build_files "$PROG" "$RUNNER" main.go ) \
  || infra_die "graalgo-tool build of peguard failed"

fail=0
# run_mode <name> <extra -D options...>
run_mode() {
  local mode="$1"; shift
  local out="$WORK/$mode.out" err="$WORK/$mode.err" status=0
  GRAALGO_JAVA_OPTS="${PEGUARD_JAVA_OPTS:--Xmx2g -XX:ActiveProcessorCount=4} \
 -Dpolyglot.engine.TraceCompilation=true \
 -Dpolyglot.engine.CompilationFailureAction=Print \
 -Dpolyglot.engine.BackgroundCompilation=false $*" TMPDIR="$WORK" \
    timeout 300 "$PROG" >"$out" 2>"$err" || status=$?
  if grep -q 'uses a fallback runtime that does not support runtime compilation' "$err"; then
    echo "peguard[$mode]: FAIL: the JVM has no optimizing Truffle runtime (fallback" >&2
    echo "  interpreter-only runtime), so nothing can be compiled. Use the pinned" >&2
    echo "  Oracle GraalVM JDK 25 (docs/dev/ENV.md) with JVMCI enabled. Truffle says:" >&2
    grep -m1 'The following cause was found' "$err" | sed 's/^/  /' >&2 || true
    exit "$INFRA_EXIT"
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
  nfailed="$(grep -c '^\[engine\] opt failed' "$err" || true)"
  echo "peguard[$mode]: $(grep -c '^\[engine\] opt done' "$err" || true) compilations, $nfailed failed"
}

run_mode hot -Dpolyglot.engine.OSRCompilationThreshold=1024
for g in $GUARDS; do
  if ! grep -q "^\[engine\] opt done .*main\.$g[[:space:]]" "$WORK/hot.err"; then
    echo "peguard[hot]: $g was never compiled" >&2
    fail=1
  fi
done
if ! grep -q '^\[engine\] opt done .*<OSR>' "$WORK/hot.err"; then
  echo "peguard[hot]: no loop was OSR-compiled" >&2
  fail=1
fi
# The package variable initializers (`<pkg>.init·vars`: unicode's tables
# are thousands of composite literals) run once and never compile under
# normal tiering; compiled eagerly, unicode's alone takes > 100 s of partial
# evaluation. They are excluded from the breadth run (CompileOnly `~`
# exclusion), every other executed call target compiles.
run_mode breadth -Dpolyglot.engine.CompileImmediately=true '-Dpolyglot.engine.CompileOnly=~init·vars'

# The planted violation (the guard can fail): the hot run under a graph-size
# limit every guard exceeds must report failed compilations.
if [ "$fail" -eq 0 ]; then
  run_mode plant -Dpolyglot.engine.OSRCompilationThreshold=1024 -Dpolyglot.compiler.MaximumGraalGraphSize=100 2>/dev/null
  if [ "$nfailed" -eq 0 ]; then
    echo "peguard: the planted violation (MaximumGraalGraphSize=100) failed no compilation — the guard cannot fail" >&2
    fail=1
  else
    fail=0
  fi
fi

if [ "$fail" -ne 0 ]; then
  echo "peguard: FAIL" >&2
  exit "$DIVERGENCE_EXIT"
fi
echo "peguard: OK (no compilation failed)"
