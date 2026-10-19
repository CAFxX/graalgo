#!/usr/bin/env bash
# CI entry point for the diffharness smoke layer (graalgo spike 7).
#
# Spike kill triggers (see docs/arch/spikes/07-harness/REPORT.md):
#   K1: any mutation class the harness cannot detect -> FAIL
#   K2: systematic false positives on the unmutated corpus -> FAIL
#   K3: harness flakiness (rerun control diverges) -> FAIL
#
# Exits non-zero on any failure. Runnable from a clean checkout by any
# agent/CI: only needs a Go toolchain (DH_GO override, else ~/tools/go,
# else PATH).
set -euo pipefail
cd "$(dirname "$0")"

# Scratch space: /tmp on this VM is a small tmpfs shared with other work;
# keep all temp build/run dirs on the persistent disk.
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

# graalgo backend (interop product launcher): PHASE3_DIR points at the
# internal/backend tree holding the graalgo-sync-run launcher wrapper
# (P3SyncMain). Defaults to this repo's internal/backend so a plain ./run.sh
# works from a clean checkout; override to point at another tree. The
# product launcher (not the truffle graalgo-run) is the smoke backend so the
# full corpus — including the channel/goroutine programs — runs without
# exclusions.
if [ -z "${PHASE3_DIR:-}" ]; then
  export PHASE3_DIR="$PWD/../../internal/backend"
fi
# The P3SyncMain backend needs truffle+runtime+interop classes built.
if [ ! -x "$PHASE3_DIR/graalgo-sync-run" ]; then
  echo "run.sh: $PHASE3_DIR/graalgo-sync-run missing/not executable" >&2
  exit 2
fi
if [ ! -d "$PHASE3_DIR/interop/out/classes" ]; then
  echo "run.sh: interop classes not built; run 'make build' at the repo root first" >&2
  exit 2
fi

OUTDIR="${OUTDIR:-./test-results}"
mkdir -p "$OUTDIR"

echo "== [0/5] toolchain =="
go version
command -v go

echo "== [1/5] build =="
# NOTE: test-results/ holds generated mutants and reports; it is not source
# and is deliberately excluded from the build/vet/test patterns below.
go build ./cmd/... ./internal/... ./corpus/...

echo "== [2/5] vet + gofmt =="
go vet ./cmd/... ./internal/... ./corpus/...
gofmt -l $(find . -name '*.go' -not -path './testdata/_broken/*') | tee "$OUTDIR/gofmt.txt"
test ! -s "$OUTDIR/gofmt.txt"

echo "== [3/5] unit tests =="
go test -count=1 ./internal/... 2>&1 | tee "$OUTDIR/unit-tests.log"
# (go test exits non-zero on failure; set -e stops here)

# DH_TIMEOUT: per-program timeout override (seconds). The backends-p3.json
# default (30s) suits the VM; slower CI runners need more headroom.
TIMEOUT_ARGS=()
if [ -n "${DH_TIMEOUT:-}" ]; then
  TIMEOUT_ARGS=(-timeout "$DH_TIMEOUT")
fi

echo "== [4/5] green run: unmutated corpus, gc-a vs gc-b =="
go run ./cmd/diffharness run \
  -config backends-p3.json -corpus corpus \
  "${TIMEOUT_ARGS[@]}" \
  -out "$OUTDIR/green.json" 2>&1 | tee "$OUTDIR/green.log"

echo "== [5/5] oracle validation: rerun control + mutation red-green =="
rm -rf "$OUTDIR/mutants" && mkdir -p "$OUTDIR/mutants"
go run ./cmd/diffharness validate \
  -config backends-p3.json -corpus corpus \
  "${TIMEOUT_ARGS[@]}" \
  -mutations mutations.txt -workdir "$OUTDIR/mutants" \
  -out "$OUTDIR/validate.json" 2>&1 | tee "$OUTDIR/validate.log"

echo "== checksums (measurement honesty: harness sources archived with logs) =="
{
  echo "# sha256 of harness sources at validation time: $(date -u +%FT%TZ)"
  find . -name '*.go' -not -path './testdata/_broken/*' -not -path './test-results/*' | sort | xargs sha256sum
  sha256sum backends.json backends-p3.json mutations.txt run.sh
} | tee "$OUTDIR/checksums.txt"

echo
echo "ALL GREEN: harness operational, oracles mutation-validated."
