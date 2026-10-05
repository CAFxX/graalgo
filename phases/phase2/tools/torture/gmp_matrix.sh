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

# Note: on hosts where NPROC is 1, 2, or 4, the gmp=N run duplicates an
# earlier cell; we still run it (distinct evidence) but tag the files
# -n<N> to avoid overwriting.
for gmp in 1 2 4; do
  for wl in compute mixed; do
    tag="gmp${gmp}"
    prog="$CORPUS/gmp_$wl.go"
    # graalgo
    timeout 180 "$GRAALVM_HOME/bin/java" -cp "$CP" \
      graalgo.sched.launcher.SchedMain --gmp="$gmp" "$prog" \
      > "$OUTDIR/${wl}_${tag}.graalgo.out" 2> "$OUTDIR/${wl}_${tag}.graalgo.err"
    grc=$?
    # gc
    GOMAXPROCS="$gmp" timeout 120 "$GO" run "$prog" \
      > "$OUTDIR/${wl}_${tag}.gc.out" 2> "$OUTDIR/${wl}_${tag}.gc.err"
    gcrc=$?
    if [ $grc -eq 0 ] && [ $gcrc -eq 0 ] && \
       cmp -s "$OUTDIR/${wl}_${tag}.graalgo.out" "$OUTDIR/${wl}_${tag}.gc.out"; then
      echo "PASS wl=$wl gmp=$gmp"
      PASS=$((PASS + 1))
    else
      echo "FAIL wl=$wl gmp=$gmp (graalgo_exit=$grc gc_exit=$gcrc)"
      FAILED_CELLS="$FAILED_CELLS $wl/gmp$gmp"
      FAIL=$((FAIL + 1))
    fi
  done
done
# gmp=N (distinct cell even when N coincides numerically: separate files).
for wl in compute mixed; do
  prog="$CORPUS/gmp_$wl.go"
  timeout 180 "$GRAALVM_HOME/bin/java" -cp "$CP" \
    graalgo.sched.launcher.SchedMain --gmp="$NPROC" "$prog" \
    > "$OUTDIR/${wl}_gmpN.n$NPROC.graalgo.out" 2> "$OUTDIR/${wl}_gmpN.n$NPROC.graalgo.err"
  grc=$?
  GOMAXPROCS="$NPROC" timeout 120 "$GO" run "$prog" \
    > "$OUTDIR/${wl}_gmpN.n$NPROC.gc.out" 2> "$OUTDIR/${wl}_gmpN.n$NPROC.gc.err"
  gcrc=$?
  if [ $grc -eq 0 ] && [ $gcrc -eq 0 ] && \
     cmp -s "$OUTDIR/${wl}_gmpN.n$NPROC.graalgo.out" "$OUTDIR/${wl}_gmpN.n$NPROC.gc.out"; then
    echo "PASS wl=$wl gmp=N($NPROC)"
    PASS=$((PASS + 1))
  else
    echo "FAIL wl=$wl gmp=N($NPROC) (graalgo_exit=$grc gc_exit=$gcrc)"
    FAILED_CELLS="$FAILED_CELLS $wl/gmpN"
    FAIL=$((FAIL + 1))
  fi
done

echo "---"
echo "matrix: $PASS pass, $FAIL fail"
if [ -n "$FAILED_CELLS" ]; then
  echo "failed cells:$FAILED_CELLS"
fi
echo "RESULT=$([ $FAIL -eq 0 ] && echo PASS || echo FAIL)"
exit $([ $FAIL -eq 0 ] && echo 0 || echo 2)
