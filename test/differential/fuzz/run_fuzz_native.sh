#!/usr/bin/env bash
# Native differential fuzzing (GG-0037(i)): testing.F fuzzing of pure
# stdlib functions with identical corpora under gc and graalgo.
#
# 1. `go test -fuzz` grows the corpus per target in testdata/fuzz/FuzzXxx/
#    (fuzzer mutations supply input diversity; seeds in fuzz_test.go).
# 2. replay.py materializes every corpus entry as a single-file program.
# 3. `diffharness run` executes the materialized corpus differential
#    gc vs graalgo through the shared oracle (GG-0030).
#
# A divergence is a graalgo bug: the functions are pure and the inputs
# are identical on both legs.
#
# Usage: run_fuzz_native.sh [--fuzztime SEC] [--out DIR]
#
# Environment:
#   DH_GO        Go toolchain root (default: ~/tools/go, else PATH)
#   GRAALVM_HOME GraalVM root for the graalgo backend
#   PHASE3_DIR   dir holding the product launcher graalgo-sync-run
#                (default: <repo>/internal/backend; needs `make build`)
#
# Exit status: 0 no divergence, 1 divergence, 2 usage, 3 infrastructure
# failure (launcher/backend missing, no verdict for some program).
set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
DH_ROOT="$(cd "$HERE/.." && pwd)"
REPO_ROOT="$(cd "$DH_ROOT/../.." && pwd)"

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
export PHASE3_DIR="${PHASE3_DIR:-$REPO_ROOT/internal/backend}"
if [ ! -x "$PHASE3_DIR/graalgo-sync-run" ] || [ ! -d "$PHASE3_DIR/interop/out/classes" ]; then
  echo "INFRA: product launcher/backend not built under $PHASE3_DIR (run make generate build)" >&2
  exit 3
fi

FUZZTIME="30"
OUTDIR=""
while [ $# -gt 0 ]; do
  case "$1" in
    --fuzztime) FUZZTIME="$2"; shift 2;;
    --out) OUTDIR="$2"; shift 2;;
    *) echo "unknown arg: $1" >&2; exit 2;;
  esac
done
OUTDIR="${OUTDIR:-$DH_ROOT/.tmp/fuzz-native}"
mkdir -p "$OUTDIR"

echo "== [1/4] native fuzz: grow corpora (${FUZZTIME}s per target) =="
cd "$HERE"
for target in FuzzStringsToUpper FuzzStringsToLower FuzzStringsTrimSpace \
    FuzzStringsCount FuzzStringsReplaceAll FuzzStrconvAtoi FuzzStrconvItoa \
    FuzzBytesToUpper; do
  echo "--- $target ---"
  go test -run=NONE -fuzz="$target" -fuzztime="${FUZZTIME}s" . 2>&1 | tail -2
done

echo "== [2/4] replay: materialize differential corpus =="
REPLAY_DIR="$OUTDIR/replay-corpus"
rm -rf "$REPLAY_DIR" && mkdir -p "$REPLAY_DIR"
python3 "$HERE/replay.py" --fuzzdir "$HERE/testdata/fuzz" --out "$REPLAY_DIR" | tee "$OUTDIR/replay.log"

echo "== [3/4] differential: gc vs graalgo =="
cd "$DH_ROOT"
go build -o "$OUTDIR/diffharness" ./cmd/diffharness
set +e
PHASE3_DIR="$PHASE3_DIR" \
"$OUTDIR/diffharness" run \
  -config "$DH_ROOT/backends-p3.json" \
  -corpus "$REPLAY_DIR" \
  -out "$OUTDIR/report.json" 2>&1 | tee "$OUTDIR/run.log"
rc=${PIPESTATUS[0]}
set -e

echo "== [4/4] verdict =="
if [ $rc -eq 0 ]; then
  echo "FUZZ-NATIVE GREEN: zero divergences (see $OUTDIR/report.json)"
else
  echo "FUZZ-NATIVE RED: divergences above (see $OUTDIR/report.json)"
fi
exit $rc
