#!/usr/bin/env bash
# Native differential fuzzing (GG-0037(i)): testing.F fuzzing of pure
# stdlib functions with identical corpora under gc and graalgo.
#
# 1. `go test -fuzz` grows the corpus per target in
#    $GOCACHE/fuzz/<import path>/FuzzXxx/ (NOT testdata/fuzz/: go writes
#    only failing inputs there; fuzzer mutations supply input diversity;
#    seeds in fuzz_test.go).
# 2. replay.py materializes every corpus entry as a single-file program.
# 3. test/fuzz/run.py (the new path's differential runner, `make
#    fuzz-newpath`) builds and runs every replay program under gc and
#    under `go build -toolexec graalgo-tool` and compares them. (Until
#    2026-11-23 this step ran diffharness on the v1 product launcher
#    graalgo-sync-run, which M3 deletes.)
#
# A divergence is a graalgo bug: the functions are pure and the inputs
# are identical on both legs.
#
# Usage: run_fuzz_native.sh [--fuzztime SEC] [--max-per-target N] [--out DIR]
#   --max-per-target bounds the replay differential (0: every entry).
#
# Environment:
#   DH_GO        Go toolchain root (default: ~/tools/go, else PATH)
#   GO           the gc oracle (default: go on PATH); needs `make build`
#   GRAALVM_HOME GraalVM root for the graalgo backend
#
# Exit status (run.py's): 0 no divergence, 1 new divergence, 2 usage,
# 3 infrastructure failure (toolchain setup failed, empty replay).
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
export GO="${GO:-$(command -v go)}"

FUZZTIME="30"
MAXPER="0"
OUTDIR=""
while [ $# -gt 0 ]; do
  case "$1" in
    --fuzztime) FUZZTIME="$2"; shift 2;;
    --max-per-target) MAXPER="$2"; shift 2;;
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
FUZZCACHE="$(go env GOCACHE)/fuzz/$(go list -f '{{.ImportPath}}' .)"
python3 "$HERE/replay.py" --fuzzdir "$HERE/testdata/fuzz" --fuzzdir "$FUZZCACHE" \
  --max-per-target "$MAXPER" --out "$REPLAY_DIR" | tee "$OUTDIR/replay.log"
if [ -z "$(ls -A "$REPLAY_DIR")" ]; then
  # An empty replay is a broken pipeline (wrong corpus location), never a
  # divergence verdict: report it as the infrastructure failure it is.
  echo "INFRA: no replay programs materialized (corpus dirs: $HERE/testdata/fuzz $FUZZCACHE)" >&2
  exit 3
fi

echo "== [3/4] differential: gc vs graalgo (-toolexec path) =="
set +e
GRAALGO_TMP="$TMPDIR" python3 "$REPO_ROOT/test/fuzz/run.py" \
  --report "$OUTDIR/report.jsonl" --keep "$OUTDIR/failing" \
  $(find "$REPLAY_DIR" -name main.go | sort) 2>&1 | tee "$OUTDIR/run.log"
rc=${PIPESTATUS[0]}
set -e

echo "== [4/4] verdict =="
if [ $rc -eq 0 ]; then
  echo "FUZZ-NATIVE GREEN: zero divergences (see $OUTDIR/report.jsonl)"
elif [ $rc -eq 3 ]; then
  echo "FUZZ-NATIVE INFRA: no verdict for some program (see $OUTDIR/run.log)"
else
  echo "FUZZ-NATIVE RED: divergences above (see $OUTDIR/report.jsonl)"
fi
exit $rc
