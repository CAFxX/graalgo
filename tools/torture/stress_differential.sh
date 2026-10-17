#!/bin/bash
# P2-M8 stress-corpus differential: runtime/context-pattern Go programs vs gc.
#
# Each program runs under graalgo (--gmp=2) and under gc; outputs compared.
# rt_finalizer_storm's finalizer count is timing-dependent (informational);
# the gate is clean completion (exit 0, no crash/hang) on both.
#
# Logs go to internal/backend/runtime/out/torture/stress-differential/
# (gitignored via out/). The Go corpus lives in tools/torture/corpus/
# (committed).
#
# Prerequisites: the backend runtime module and the gofrontend binary built:
#   make -C internal/backend/runtime main
#   (see internal/frontend/build.sh for gofrontend)
set -euo pipefail
cd "$(dirname "$0")/../.." || exit 1
GR="$PWD"
RT="internal/backend/runtime"

GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
JAVA="$GRAALVM_HOME/bin/java"
[ -x "$JAVA" ] || { echo "ERROR: no java at $JAVA (set GRAALVM_HOME)" >&2; exit 1; }
GO="${GO:-$HOME/tools/go/bin/go}"
[ -x "$GO" ] || { echo "ERROR: no go at $GO (set GO)" >&2; exit 1; }
# gofrontend resolution mirrors graalgo.launcher.ToolPaths: an explicit
# GRAALGO_GOFRONTEND wins if executable, else the in-tree build location.
GOFRONTEND="${GRAALGO_GOFRONTEND:-$GR/internal/frontend/gofrontend}"
[ -x "$GOFRONTEND" ] || { echo "ERROR: gofrontend not executable at $GOFRONTEND — build it (see internal/frontend/build.sh)" >&2; exit 1; }
export GRAALGO_GOFRONTEND="$GOFRONTEND"

# Classpath mirrors the runtime module's TEST_CP (see
# internal/backend/runtime/Makefile).
TRUFFLE="internal/backend/truffle"
for d in "$RT/out/test-classes" "$RT/out/classes" "$TRUFFLE/out/classes"; do
  [ -d "$d" ] || { echo "ERROR: $d missing — build the backend first: make -C $RT main" >&2; exit 1; }
done
[ -d "$TRUFFLE/lib" ] || { echo "ERROR: $TRUFFLE/lib missing" >&2; exit 1; }
CP="$RT/out/test-classes:$RT/out/classes:$TRUFFLE/out/classes:internal/backend/interop/out/classes:$TRUFFLE/lib/*"
JAVA_FLAGS=(--enable-native-access=ALL-UNNAMED -Xss32m)

CORPUS="tools/torture/corpus"
OUTDIR="$RT/out/torture/stress-differential"
mkdir -p "$OUTDIR" || { echo "ERROR: cannot create $OUTDIR" >&2; exit 1; }

PASS=0; FAIL=0; FAILED=""

# differential <name> <mode: exact|completion>
differential() {
  local name="$1" mode="$2"
  local prog="$CORPUS/$name.go"
  [ -f "$prog" ] || { echo "ERROR: corpus program missing: $prog" >&2; exit 1; }
  local grc=0 gcrc=0
  timeout 180 "$JAVA" "${JAVA_FLAGS[@]}" -cp "$CP" \
    graalgo.sched.launcher.SchedMain --gmp=2 "$prog" \
    > "$OUTDIR/$name.graalgo.out" 2> "$OUTDIR/$name.graalgo.err" || grc=$?
  timeout 120 "$GO" run "$prog" \
    > "$OUTDIR/$name.gc.out" 2> "$OUTDIR/$name.gc.err" || gcrc=$?
  local ok=1
  if [ "$mode" = "exact" ]; then
    cmp -s "$OUTDIR/$name.graalgo.out" "$OUTDIR/$name.gc.out" || ok=0
  fi
  [ $grc -eq 0 ] || ok=0
  [ $gcrc -eq 0 ] || ok=0
  if [ $ok -eq 1 ]; then
    echo "PASS $name ($mode)"
    PASS=$((PASS + 1))
  else
    echo "FAIL $name ($mode; graalgo_exit=$grc gc_exit=$gcrc)"
    FAILED="$FAILED $name"; FAIL=$((FAIL + 1))
  fi
}

differential gmp_compute exact
differential gmp_mixed exact
differential ctx_cancel exact
differential rt_finalizer_storm completion

echo "---"
echo "stress differential: $PASS pass, $FAIL fail"
[ -n "$FAILED" ] && echo "failed:$FAILED"
echo "RESULT=$([ $FAIL -eq 0 ] && echo PASS || echo FAIL)"
exit $([ $FAIL -eq 0 ] && echo 0 || echo 2)
