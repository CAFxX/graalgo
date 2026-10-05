#!/bin/bash
# P2-M8 stress-corpus differential: runtime/context-pattern Go programs vs gc.
#
# Each program runs under graalgo (--gmp=2) and under gc; outputs compared.
# rt_finalizer_storm's finalizer count is timing-dependent (informational);
# the gate is clean completion (exit 0, no crash/hang) on both.
set -u
cd "$(dirname "$0")/../.." || exit 1

GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
GO="$HOME/tools/go/bin/go"
CP="out/test-classes:out/classes:../phase1/out/classes:../phase1/lib/collections-25.0.4.1.1.jar:../phase1/lib/graal-sdk-25.0.4.1.1.jar:../phase1/lib/nativeimage-25.0.4.1.1.jar:../phase1/lib/polyglot-25.0.4.1.1.jar:../phase1/lib/truffle-api-25.0.4.1.1.jar:../phase1/lib/truffle-dsl-processor-25.0.4.1.1.jar:../phase1/lib/truffle-runtime-25.0.4.1.1.jar"
CORPUS="hidden_files/torture/corpus"
OUTDIR="hidden_files/torture/stress-differential"
mkdir -p "$OUTDIR"

PASS=0; FAIL=0; FAILED=""

# differential <name> <mode: exact|completion>
differential() {
  local name="$1" mode="$2"
  local prog="$CORPUS/$name.go"
  timeout 180 "$GRAALVM_HOME/bin/java" -cp "$CP" \
    graalgo.sched.launcher.SchedMain --gmp=2 "$prog" \
    > "$OUTDIR/$name.graalgo.out" 2> "$OUTDIR/$name.graalgo.err"
  local grc=$?
  timeout 120 "$GO" run "$prog" \
    > "$OUTDIR/$name.gc.out" 2> "$OUTDIR/$name.gc.err"
  local gcrc=$?
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
