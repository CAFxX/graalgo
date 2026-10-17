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
# Go programs run through the product launcher internal/backend/graalgo-sync-run
# (P3SyncMain): it drives the same SchedulerCore under test and supports
# channels/select and the full stdlib surface. The raw scheduler entry point
# (graalgo.sched.launcher.SchedMain) cannot run real Go programs on this base
# (its Truffle NodeFactory lacks the ifacemethod/makechan hooks) — using it
# here would fail every cell for the wrong reason.
#
# Prerequisites: the backend modules built:
#   make -C internal/backend/truffle main     # runtime + truffle classes
#   make -C internal/backend/interop main     # product launcher classes
#   (see internal/frontend/build.sh for the gofrontend binary)
set -euo pipefail
cd "$(dirname "$0")/../.." || exit 1
GR="$PWD"

SYNC_RUN="internal/backend/graalgo-sync-run"
[ -x "$SYNC_RUN" ] || { echo "ERROR: $SYNC_RUN missing/not executable" >&2; exit 1; }
[ -d internal/backend/interop/out/classes ] || {
  echo "ERROR: interop classes missing — build first: make -C internal/backend/interop main" >&2; exit 1; }
[ -d internal/backend/runtime/out/classes ] || {
  echo "ERROR: runtime classes missing — build first: make -C internal/backend/truffle main" >&2; exit 1; }
GO="${GO:-$HOME/tools/go/bin/go}"
[ -x "$GO" ] || { echo "ERROR: no go at $GO (set GO)" >&2; exit 1; }
# gofrontend resolution mirrors graalgo.launcher.ToolPaths: an explicit
# GRAALGO_GOFRONTEND wins if executable, else the in-tree build location.
GOFRONTEND="${GRAALGO_GOFRONTEND:-$GR/internal/frontend/gofrontend}"
[ -x "$GOFRONTEND" ] || { echo "ERROR: gofrontend not executable at $GOFRONTEND — build it (see internal/frontend/build.sh)" >&2; exit 1; }
export GRAALGO_GOFRONTEND="$GOFRONTEND"

CORPUS="tools/torture/corpus"
OUTDIR="internal/backend/runtime/out/torture/gmp-matrix"
mkdir -p "$OUTDIR" || { echo "ERROR: cannot create $OUTDIR" >&2; exit 1; }
# Keep the JVM's temp files out of /tmp (small shared tmpfs); the launcher
# honors TMPDIR for -Djava.io.tmpdir.
export TMPDIR="$OUTDIR"
# Cap the per-cell JVM heap: the box is memory-tight and an uncapped
# truffle JVM risks the OOM killer (observed SIGKILL, exit 137).
export _JAVA_OPTIONS="-Xmx1g"

NPROC=$(nproc)
PASS=0
FAIL=0
FAILED_CELLS=""

cell() {
  local wl="$1" gmp="$2" tag="$3"
  local prog="$CORPUS/gmp_$wl.go"
  [ -f "$prog" ] || { echo "ERROR: corpus program missing: $prog" >&2; exit 1; }
  local grc=0 gcrc=0
  # graalgo (product launcher: same SchedulerCore, full stdlib surface).
  # The corpus prints via the println builtin, which writes to stderr on
  # both gc and graalgo — so the differential comparison is on stderr.
  # (JVM/launcher warnings would also land on stderr and fail the cell;
  # that is intended: toolchain noise is a signal, not to be filtered.)
  timeout 180 "$SYNC_RUN" --gmp="$gmp" "$GR/$prog" \
    > "$OUTDIR/${wl}_${tag}.graalgo.stdout" 2> "$OUTDIR/${wl}_${tag}.graalgo.stderr" || grc=$?
  # gc
  GOMAXPROCS="$gmp" timeout 120 "$GO" run "$prog" \
    > "$OUTDIR/${wl}_${tag}.gc.stdout" 2> "$OUTDIR/${wl}_${tag}.gc.stderr" || gcrc=$?
  if [ $grc -eq 0 ] && [ $gcrc -eq 0 ] && \
     cmp -s "$OUTDIR/${wl}_${tag}.graalgo.stderr" "$OUTDIR/${wl}_${tag}.gc.stderr"; then
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
