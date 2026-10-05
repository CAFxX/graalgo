#!/bin/bash
# P2-M8 GOMAXPROCS matrix: GOMAXPROCS x workload, differential vs gc.
#
# Matrix: GOMAXPROCS in {1, 2, 4, N} x workload in {compute, mixed}.
# (N = host CPUs; 4 on a 2-CPU VM exercises the oversubscription clamp.)
# Each cell: run under graalgo (--gmp=N) and under gc (GOMAXPROCS=N),
# compare stdout byte-for-byte. Any systematic divergence is a
# kill-criterion trigger (PLAN.md risk: "in-flight-cap economics").
#
# Channel-heavy cells are Java-level (TortureBattery F1); Go channel ops
# are WS E/F (not in Go source scope).
set -u
cd "$(dirname "$0")/../.." || exit 1

GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
GO="$HOME/tools/go/bin/go"
CP="out/test-classes:out/classes:../phase1/out/classes:../phase1/lib/collections-25.0.4.1.1.jar:../phase1/lib/graal-sdk-25.0.4.1.1.jar:../phase1/lib/nativeimage-25.0.4.1.1.jar:../phase1/lib/polyglot-25.0.4.1.1.jar:../phase1/lib/truffle-api-25.0.4.1.1.jar:../phase1/lib/truffle-dsl-processor-25.0.4.1.1.jar:../phase1/lib/truffle-runtime-25.0.4.1.1.jar"
CORPUS="hidden_files/torture/corpus"
OUTDIR="hidden_files/torture/gmp-matrix"
mkdir -p "$OUTDIR"

NPROC=$(nproc)
PASS=0
FAIL=0
FAILED_CELLS=""

for gmp in 1 2 4 "$NPROC"; do
  for wl in compute mixed; do
    prog="$CORPUS/gmp_$wl.go"
    # graalgo
    timeout 180 "$GRAALVM_HOME/bin/java" -cp "$CP" \
      graalgo.sched.launcher.SchedMain --gmp="$gmp" "$prog" \
      > "$OUTDIR/${wl}_gmp${gmp}.graalgo.out" 2> "$OUTDIR/${wl}_gmp${gmp}.graalgo.err"
    grc=$?
    # gc
    GOMAXPROCS="$gmp" timeout 120 "$GO" run "$prog" \
      > "$OUTDIR/${wl}_gmp${gmp}.gc.out" 2> "$OUTDIR/${wl}_gmp${gmp}.gc.err"
    gcrc=$?
    # Compare: both must exit 0 and stdout must match.
    # (rt_finalizer_storm's count is timing-dependent; it is excluded from
    #  byte-compare — checked separately for clean completion.)
    if [ $grc -eq 0 ] && [ $gcrc -eq 0 ] && \
       cmp -s "$OUTDIR/${wl}_gmp${gmp}.graalgo.out" "$OUTDIR/${wl}_gmp${gmp}.gc.out"; then
      echo "PASS wl=$wl gmp=$gmp"
      PASS=$((PASS + 1))
    else
      echo "FAIL wl=$wl gmp=$gmp (graalgo_exit=$grc gc_exit=$gcrc)"
      FAILED_CELLS="$FAILED_CELLS $wl/gmp$gmp"
      FAIL=$((FAIL + 1))
    fi
  done
done

echo "---"
echo "matrix: $PASS pass, $FAIL fail"
if [ -n "$FAILED_CELLS" ]; then
  echo "failed cells:$FAILED_CELLS"
fi
echo "RESULT=$([ $FAIL -eq 0 ] && echo PASS || echo FAIL)"
exit $([ $FAIL -eq 0 ] && echo 0 || echo 2)
