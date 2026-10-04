#!/usr/bin/env bash
# CI entry point for the diffharness smoke layer (graalgo spike 7).
#
# Spike kill triggers (see spikes/07-harness/REPORT.md):
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

# graalgo backend (Phase 1, M1+): PHASE1_DIR points at the phase-1 tree holding
# the graalgo-run launcher wrapper. Defaults to this repo's phases/phase1 so a
# plain ./run.sh works from a clean checkout; override to point at another tree.
if [ -z "${PHASE1_DIR:-}" ]; then
  export PHASE1_DIR="$PWD/../phases/phase1"
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

echo "== [4/5] green run: unmutated corpus, gc-a vs gc-b =="
go run ./cmd/diffharness run \
  -config backends.json -corpus corpus \
  -out "$OUTDIR/green.json" 2>&1 | tee "$OUTDIR/green.log"

echo "== [5/5] oracle validation: rerun control + mutation red-green =="
rm -rf "$OUTDIR/mutants" && mkdir -p "$OUTDIR/mutants"
go run ./cmd/diffharness validate \
  -config backends.json -corpus corpus \
  -mutations mutations.txt -workdir "$OUTDIR/mutants" \
  -out "$OUTDIR/validate.json" 2>&1 | tee "$OUTDIR/validate.log"

echo "== checksums (measurement honesty: harness sources archived with logs) =="
{
  echo "# sha256 of harness sources at validation time: $(date -u +%FT%TZ)"
  find . -name '*.go' -not -path './testdata/_broken/*' -not -path './test-results/*' | sort | xargs sha256sum
  sha256sum backends.json mutations.txt run.sh
} | tee "$OUTDIR/checksums.txt"

echo
echo "ALL GREEN: harness operational, oracles mutation-validated."
