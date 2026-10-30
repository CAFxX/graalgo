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
#
# NOTE (post-restructure): the Go corpus this matrix ran
# (gmp_compute.go, gmp_mixed.go) was never committed on this branch, so
# the matrix cannot run until that corpus lands at tools/torture/corpus/.
# The script fails fast and loudly in that case instead of running
# against a stale classpath and printing a bogus RESULT line; the
# runtime Makefile's `gmp-matrix` target invokes this script, so a loud
# failure there is the correct signal until the corpus is restored.
set -u
cd "$(dirname "$0")/../.." || exit 1

REPO="$(pwd)"
GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
GO="${GO:-$HOME/tools/go/bin/go}"
# Classpath mirrors the runtime module's RUN_CP: runtime classes first
# (same-FQN replacements shadow the truffle stubs), then truffle, then
# interop, then the Truffle jars. SchedMain lives in the runtime module.
CP="$REPO/internal/backend/runtime/out/classes:$REPO/internal/backend/truffle/out/classes:$REPO/internal/backend/interop/out/classes:$(echo "$REPO"/internal/backend/truffle/lib/*.jar | tr ' ' ':')"
CORPUS="$REPO/tools/torture/corpus"
OUTDIR="${OUTDIR:-$REPO/.out/torture/gmp-matrix}"
mkdir -p "$OUTDIR"
if [ ! -d "$REPO/internal/backend/runtime/out/classes" ]; then
    echo "FAIL: runtime classes not built; run 'make build' at the repo root first" >&2
    exit 1
fi
if [ ! -d "$CORPUS" ]; then
    echo "FAIL: torture Go corpus missing: $CORPUS (gmp_compute.go, gmp_mixed.go were never committed on this branch)" >&2
    exit 1
fi
# SchedMain resolves gofrontend via ToolPaths' walk-up; pin the
# checkout build when present.
if [ -x "$REPO/internal/frontend/gofrontend" ]; then
    export GRAALGO_GOFRONTEND="$REPO/internal/frontend/gofrontend"
fi

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
