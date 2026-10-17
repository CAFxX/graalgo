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
# Logs go to internal/backend/runtime/out/torture/gmp-matrix/ (gitignored
# via out/). The Go corpus lives in tools/torture/corpus/ (committed).
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
OUTDIR="$RT/out/torture/gmp-matrix"
mkdir -p "$OUTDIR" || { echo "ERROR: cannot create $OUTDIR" >&2; exit 1; }

NPROC=$(nproc)
PASS=0
FAIL=0
FAILED_CELLS=""

cell() {
  local wl="$1" gmp="$2" tag="$3"
  local prog="$CORPUS/gmp_$wl.go"
  [ -f "$prog" ] || { echo "ERROR: corpus program missing: $prog" >&2; exit 1; }
  local grc=0 gcrc=0
  # graalgo
  timeout 180 "$JAVA" "${JAVA_FLAGS[@]}" -cp "$CP" \
    graalgo.sched.launcher.SchedMain --gmp="$gmp" "$prog" \
    > "$OUTDIR/${wl}_${tag}.graalgo.out" 2> "$OUTDIR/${wl}_${tag}.graalgo.err" || grc=$?
  # gc
  GOMAXPROCS="$gmp" timeout 120 "$GO" run "$prog" \
    > "$OUTDIR/${wl}_${tag}.gc.out" 2> "$OUTDIR/${wl}_${tag}.gc.err" || gcrc=$?
  if [ $grc -eq 0 ] && [ $gcrc -eq 0 ] && \
     cmp -s "$OUTDIR/${wl}_${tag}.graalgo.out" "$OUTDIR/${wl}_${tag}.gc.out"; then
    echo "PASS wl=$wl gmp=$gmp"
    PASS=$((PASS + 1))
  else
    echo "FAIL wl=$wl gmp=$gmp (graalgo_exit=$grc gc_exit=$gcrc)"
    FAILED_CELLS="$FAILED_CELLS $wl/gmp$gmp"
    FAIL=$((FAIL + 1))
  fi
}

for gmp in 1 2 4; do
  for wl in compute mixed; do
    cell "$wl" "$gmp" "gmp${gmp}"
  done
done
# gmp=N (distinct cell even when N coincides numerically: separate files).
# Note: on hosts where NPROC is 1, 2, or 4, the gmp=N run duplicates an
# earlier cell; we still run it (distinct evidence) but tag the files
# -n<N> to avoid overwriting.
for wl in compute mixed; do
  cell "$wl" "$NPROC" "gmpN.n$NPROC"
done

echo "---"
echo "matrix: $PASS pass, $FAIL fail"
if [ -n "$FAILED_CELLS" ]; then
  echo "failed cells:$FAILED_CELLS"
fi
echo "RESULT=$([ $FAIL -eq 0 ] && echo PASS || echo FAIL)"
exit $([ $FAIL -eq 0 ] && echo 0 || echo 2)
