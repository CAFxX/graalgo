#!/usr/bin/env bash
# Conformance layer runner (PLAN.md §5 layer 2, GG-0029/GG-0032):
# GOROOT/test language suite (356 files) differential vs stock gc.
#
# The srctest adapter classifies every file (run / errorcheck / structural
# exclusions) and runs the included ones gc-a vs graalgo through the shared
# diffharness backends. Exclusions are versioned and reason-coded
# (srctest-exclusions.yaml, GG-0033).
#
# Exit status is non-zero on any unexplained divergence (a divergence not
# covered by the exclusion list). New divergences are triaged into
# srctest-exclusions.yaml with a reason code (four-way triage per PLAN §5:
# graalgo bug / upstream change / backend-specific+reason-code /
# nondeterminism-normalization) or fixed in the frontend.
#
# Environment:
#   DH_GO        Go toolchain root (default: ~/tools/go, else PATH)
#   GRAALVM_HOME GraalVM root for the graalgo-run backend
#                (default: ~/tools/graalvm-jdk-25)
#   GOROOT_TEST  GOROOT/test directory override (default: $(go env GOROOT)/test)
#   JOBS         parallel programs (default: 4)
#   RECHECK_EXCLUDED
#                when 1, run the exclusion-list recheck after the
#                differential (srctest run --recheck-excluded): runs only the
#                YAML-excluded files, fails on unexpected passes (XPASS).
#                Intended cadence: weekly (scheduled job), not per-commit.
#
# Exclusion-list hygiene (GG-0033): every run verifies the exclusion list
# against the scan (srctest scan --verify-exclusions: no stale entries,
# valid reason codes, non-empty details, no shadowed entries). The recheck
# step additionally catches entries overtaken by fixes. See
# test/differential/README.md "Exclusion-list hygiene".
#
# Runnable from a clean checkout by any agent/CI: needs a Go toolchain with
# a full GOROOT/test tree (actions/setup-go provides it; the VM's
# ~/tools/go is a stripped distribution without test/).
set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"

export TMPDIR="${TMPDIR:-$HOME/workspace/.tmp}"
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

PHASE1_DIR="$REPO_ROOT/internal/backend"
if [ ! -x "$PHASE1_DIR/graalgo-run" ]; then
  echo "conformance: $PHASE1_DIR/graalgo-run missing/not executable" >&2
  exit 2
fi
if [ ! -d "$PHASE1_DIR/runtime/out/classes" ]; then
  echo "conformance: backend classes not built; run 'make build' at the repo root first" >&2
  exit 2
fi

JOBS="${JOBS:-4}"
OUTDIR="${OUTDIR:-$HERE/../test-results/conformance}"
mkdir -p "$OUTDIR"

echo "== [1/4] build diffharness =="
cd "$HERE"
go build -o "$OUTDIR/diffharness" ./cmd/diffharness

echo "== [2/4] scan: classify GOROOT/test + verify exclusion list =="
"$OUTDIR/diffharness" srctest scan \
  -dir "$GOROOT_TEST" \
  -exclusions "$HERE/srctest-exclusions.yaml" \
  -verify-exclusions \
  -out "$OUTDIR/scan.json" | tee "$OUTDIR/scan.log"

echo "== [3/4] differential: gc-a vs graalgo =="
# DH_TIMEOUT: per-program timeout override (seconds); backends.json default
# (30s) suits the VM, slower CI runners need more headroom.
TIMEOUT_ARGS=()
if [ -n "${DH_TIMEOUT:-}" ]; then
  TIMEOUT_ARGS=(-timeout "$DH_TIMEOUT")
fi
PHASE1_DIR="$PHASE1_DIR" \
"$OUTDIR/diffharness" srctest run \
  -dir "$GOROOT_TEST" \
  -exclusions "$HERE/srctest-exclusions.yaml" \
  -config "$HERE/backends.json" \
  -jobs "$JOBS" \
  "${TIMEOUT_ARGS[@]}" \
  -out "$OUTDIR/report.json" 2>&1 | tee "$OUTDIR/run.log"

echo
echo "CONFORMANCE GREEN: zero unexplained divergences (see $OUTDIR/report.json)"

# == [4/4] exclusion-list recheck (opt-in) ==
# The exclusion list is write-once: nothing ever re-runs an excluded file,
# so a fix that overtakes an exclusion rots silently. Recheck mode runs
# ONLY the YAML-excluded files and fails on any unexpected pass (XPASS) —
# a stale entry needing re-triage. Run weekly (RECHECK_EXCLUDED=1), or in a
# scheduled conformance job; not per-commit (it costs extra JVM batches).
if [ "${RECHECK_EXCLUDED:-0}" = "1" ]; then
  echo "== [4/4] recheck: YAML-excluded files only =="
  PHASE1_DIR="$PHASE1_DIR" \
  "$OUTDIR/diffharness" srctest run \
    -dir "$GOROOT_TEST" \
    -exclusions "$HERE/srctest-exclusions.yaml" \
    -config "$HERE/backends.json" \
    -jobs "$JOBS" \
    "${TIMEOUT_ARGS[@]}" \
    -recheck-excluded \
    -out "$OUTDIR/recheck.json" 2>&1 | tee "$OUTDIR/recheck.log"
  echo "RECHECK GREEN: no unexpected passes (see $OUTDIR/recheck.json)"
fi
