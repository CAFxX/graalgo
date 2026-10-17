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
# Go programs run through the product launcher internal/backend/graalgo-sync-run
# (P3SyncMain): it drives the same SchedulerCore under test and supports
# channels/select and the full stdlib surface. The raw scheduler entry point
# (graalgo.sched.launcher.SchedMain) cannot run real Go programs on this base
# (its Truffle NodeFactory lacks the ifacemethod/makechan hooks) — using it
# here would fail every program for the wrong reason.
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
OUTDIR="internal/backend/runtime/out/torture/stress-differential"
mkdir -p "$OUTDIR" || { echo "ERROR: cannot create $OUTDIR" >&2; exit 1; }
# Keep the JVM's temp files out of /tmp (small shared tmpfs); the launcher
# honors TMPDIR for -Djava.io.tmpdir.
export TMPDIR="$OUTDIR"
# Cap the per-program JVM heap: the box is memory-tight and an uncapped
# truffle JVM risks the OOM killer (observed SIGKILL, exit 137).
export _JAVA_OPTIONS="-Xmx1g"

PASS=0; FAIL=0; FAILED=""

# differential <name> <mode: exact|completion>
differential() {
  local name="$1" mode="$2"
  local prog="$CORPUS/$name.go"
  [ -f "$prog" ] || { echo "ERROR: corpus program missing: $prog" >&2; exit 1; }
  local grc=0 gcrc=0
  timeout 180 "$SYNC_RUN" --gmp=2 "$GR/$prog" \
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
