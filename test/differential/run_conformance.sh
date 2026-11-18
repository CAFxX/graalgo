#!/usr/bin/env bash
# Conformance layer runner (SPEC §5.2 layer 2, GG-0029/GG-0032):
# GOROOT/test language suite (356 files) differential vs stock gc, under
# the PRODUCT launcher (internal/backend/graalgo-sync-run, P3SyncMain, via
# backends-p3.json).
#
# The srctest adapter classifies every file (run / errorcheck / notrun) and
# runs every runnable one gc-a vs graalgo. There are no automatic
# exclusions: expected divergences are explicit, reason-coded entries in
# srctest-exclusions.yaml (SPEC §5.6); listed files still run.
#
# Exit status: 0 green; 1 an unlisted divergence (triage: fix graalgo, or
# list it with a reason code) or a STALE entry (listed but now matches gc:
# prune it); 2 usage/setup; 3 infrastructure failure (no verdict).
#
# Environment:
#   DH_GO        Go toolchain root (default: ~/tools/go, else PATH)
#   GRAALVM_HOME GraalVM root for the graalgo launcher
#                (default: ~/tools/graalvm-jdk-25)
#   GOROOT_TEST  GOROOT/test directory override (default: $(go env GOROOT)/test)
#   JOBS         parallel programs (default: 4)
#
# Runnable from a clean checkout by any agent/CI: needs a Go toolchain with
# a full GOROOT/test tree (actions/setup-go provides it; the VM's
# ~/tools/go is a stripped distribution without test/).
set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"

export TMPDIR="${TMPDIR:-$REPO_ROOT/.tmp}"
mkdir -p "$TMPDIR"

if [ -z "${DH_GO:-}" ]; then
  if [ -x "$HOME/tools/go/bin/go" ]; then
    export DH_GO="$HOME/tools/go"
  fi
fi
if [ -n "${DH_GO:-}" ]; then
  export PATH="$DH_GO/bin:$PATH"
fi
export GOTOOLCHAIN=local
export GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"

if [ -z "${GOROOT_TEST:-}" ]; then
  GOROOT_TEST="$(go env GOROOT)/test"
fi
if [ ! -d "$GOROOT_TEST" ]; then
  echo "conformance: no test/ dir at $GOROOT_TEST" >&2
  echo "conformance: need a full Go distribution (actions/setup-go has it;" >&2
  echo "conformance: the VM's ~/tools/go is stripped). Set GOROOT_TEST to override." >&2
  exit 2
fi

PHASE3_DIR="$REPO_ROOT/internal/backend"
if [ ! -x "$PHASE3_DIR/graalgo-sync-run" ]; then
  echo "conformance: $PHASE3_DIR/graalgo-sync-run missing/not executable" >&2
  exit 3
fi
if [ ! -d "$PHASE3_DIR/interop/out/classes" ]; then
  echo "conformance: backend classes not built; run 'make build' at the repo root first" >&2
  exit 3
fi

JOBS="${JOBS:-4}"
OUTDIR="${OUTDIR:-$HERE/.tmp/conformance}"
mkdir -p "$OUTDIR"

echo "== [1/3] build diffharness =="
cd "$HERE"
go build -o "$OUTDIR/diffharness" ./cmd/diffharness

echo "== [2/3] scan: classify GOROOT/test =="
"$OUTDIR/diffharness" srctest scan \
  -dir "$GOROOT_TEST" \
  -exclusions "$HERE/srctest-exclusions.yaml" \
  -out "$OUTDIR/scan.json" | tee "$OUTDIR/scan.log"

echo "== [3/3] differential: gc-a vs graalgo (product launcher) =="
# DH_TIMEOUT: per-program timeout override (seconds); backends.json default
# suits the VM, slower CI runners need more headroom.
TIMEOUT_ARGS=()
if [ -n "${DH_TIMEOUT:-}" ]; then
  TIMEOUT_ARGS=(-timeout "$DH_TIMEOUT")
fi
set +e
PHASE3_DIR="$PHASE3_DIR" \
"$OUTDIR/diffharness" srctest run \
  -dir "$GOROOT_TEST" \
  -exclusions "$HERE/srctest-exclusions.yaml" \
  -config "$HERE/backends-p3.json" \
  -jobs "$JOBS" \
  "${TIMEOUT_ARGS[@]}" \
  -out "$OUTDIR/report.json" 2>&1 | tee "$OUTDIR/run.log"
rc=${PIPESTATUS[0]}
set -e
if [ "$rc" != 0 ]; then
  echo "CONFORMANCE RED (exit $rc): see $OUTDIR/run.log" >&2
  exit "$rc"
fi
echo
echo "CONFORMANCE GREEN: zero unlisted divergences, zero stale entries (see $OUTDIR/report.json)"
