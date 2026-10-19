#!/usr/bin/env bash
# Conformance layer runner (docs/arch/phases/phase3/PLAN.md §5 layer 2, GG-0029/GG-0032):
# GOROOT/test language suite (356 files) differential vs stock gc.
#
# The srctest adapter classifies every file (run / errorcheck / structural
# exclusions) and runs the included ones gc-a vs graalgo through the shared
# diffharness backends. Exclusions are versioned and reason-coded
# (srctest-exclusions.yaml, GG-0033).
#
# Exit status is non-zero on any unexplained divergence (a divergence not
# covered by the exclusion list). New divergences are triaged into
# srctest-exclusions.yaml with a reason code (four-way triage per
# docs/arch/phases/phase3/PLAN.md §5:
# graalgo bug / upstream change / backend-specific+reason-code /
# nondeterminism-normalization) or fixed in the frontend.
#
# Environment:
#   DH_GO        Go toolchain root (default: ~/tools/go, else PATH)
#   GRAALVM_HOME GraalVM root for the graalgo-run backend
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

echo "== [1/3] build diffharness =="
cd "$HERE"
go build -o "$OUTDIR/diffharness" ./cmd/diffharness

echo "== [2/3] scan: classify GOROOT/test =="
"$OUTDIR/diffharness" srctest scan \
  -dir "$GOROOT_TEST" \
  -exclusions "$HERE/srctest-exclusions.yaml" \
  -out "$OUTDIR/scan.json" | tee "$OUTDIR/scan.log"

echo "== [3/3] differential: gc-a vs graalgo =="
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
