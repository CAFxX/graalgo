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
export TMPDIR="${TMPDIR:-$PWD/.tmp}"
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

# Only gc runs here (gc-a vs gc-b: the comparator's self-test); graalgo is
# never built or run, so nothing of the backend is a precondition (m3-plan
# D1 dropped the old graalgo-sync-run/interop checks).
OUTDIR="${OUTDIR:-./.tmp/test-results}"
mkdir -p "$OUTDIR"

echo "== [0/5] toolchain =="
go version
command -v go

echo "== [1/4] build =="
# NOTE: $OUTDIR (.tmp/) holds generated mutants and reports; it is not source
# and is deliberately excluded from the build/vet/test patterns below.
go build ./cmd/... ./internal/... ./corpus/...

echo "== [2/4] vet + gofmt =="
go vet ./cmd/... ./internal/... ./corpus/...
gofmt -l $(find . -name '*.go' -not -path './testdata/_broken/*' -not -path './.tmp/*') | tee "$OUTDIR/gofmt.txt"
test ! -s "$OUTDIR/gofmt.txt"

echo "== [3/4] unit tests =="
go test -count=1 ./internal/... 2>&1 | tee "$OUTDIR/unit-tests.log"
# (go test exits non-zero on failure; set -e stops here)

# DH_TIMEOUT: per-program timeout override (seconds). The backends-newpath.json
# default (30s) suits the VM; slower CI runners need more headroom.
TIMEOUT_ARGS=()
if [ -n "${DH_TIMEOUT:-}" ]; then
  TIMEOUT_ARGS=(-timeout "$DH_TIMEOUT")
fi

# graalgo-vs-gc is NOT compared here: that is `make snapshot-check`
# (gc snapshot + reviewed expected divergences). This step self-tests the
# comparator on gc-vs-gc only: no false positives on the unmutated corpus
# (gc-a vs gc-b), no flakiness (gc-a rerun), and sensitivity to mutated
# corpus programs. Mutation of graalgo itself (SPEC 5.7) is tools/mutation.
# (A separate `diffharness run` green pass used to precede this one: it
# repeated validate's step 1 exactly — 2 of the 6 builds+runs per program.)
# DH_JOBS: programs built and run concurrently (default 1).
echo "== [4/4] comparator self-test: green run + rerun control + corpus-mutation red-green (gc-vs-gc) =="
rm -rf "$OUTDIR/mutants" && mkdir -p "$OUTDIR/mutants"
go run ./cmd/diffharness validate \
  -config backends-newpath.json -corpus corpus -backends gc-a,gc-b \
  "${TIMEOUT_ARGS[@]}" -jobs "${DH_JOBS:-1}" \
  -mutations mutations.txt -workdir "$OUTDIR/mutants" \
  -out "$OUTDIR/validate.json" 2>&1 | tee "$OUTDIR/validate.log"

echo "== checksums (measurement honesty: harness sources archived with logs) =="
{
  echo "# sha256 of harness sources at validation time: $(date -u +%FT%TZ)"
  find . -name '*.go' -not -path './testdata/_broken/*' -not -path './.tmp/*' | sort | xargs sha256sum
  sha256sum backends.json backends-p3.json backends-newpath.json mutations.txt run.sh newpath.sh newpath_build.sh
} | tee "$OUTDIR/checksums.txt"

echo
echo "ALL GREEN: harness comparator self-test passed (gc-vs-gc). graalgo-vs-gc: make snapshot-check; graalgo mutation: make mutation."
