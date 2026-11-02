#!/usr/bin/env bash
# Native differential fuzzing (GG-0037(i)): testing.F fuzzing of pure
# stdlib functions with identical corpora under gc and graalgo.
#
# 1. `go test -fuzz` grows the corpus per target in the fuzz cache
#    ($GOCACHE/fuzz/<import-path>/FuzzXxx/ — Go does NOT write newly
#    interesting inputs back to testdata/fuzz; verified with a fresh
#    GOCACHE: 61 new inputs landed in the cache, no testdata/ created).
#    Seeds live in fuzz_test.go (f.Add).
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
#   PHASE1_DIR   backend tree holding the graalgo-run launcher wrapper
#                (legacy name; now internal/backend, mirroring
#                ../run_conformance.sh). Override to point at another tree.
set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
DH_ROOT="$(cd "$HERE/.." && pwd)"
REPO_ROOT="$(cd "$DH_ROOT/../.." && pwd)"

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
export PHASE1_DIR="${PHASE1_DIR:-$REPO_ROOT/internal/backend}"
if [ ! -x "$PHASE1_DIR/graalgo-run" ]; then
  echo "run_fuzz_native: $PHASE1_DIR/graalgo-run missing/not executable" >&2
  exit 2
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
OUTDIR="${OUTDIR:-$DH_ROOT/test-results/fuzz-native}"
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
# Corpus sources: (a) the fuzz cache, where step 1 writes every newly
# interesting input; (b) any committed seed corpus under testdata/fuzz,
# if present. replay.py takes a single fuzzdir, so stage both sources
# under one dir first (seed files prefixed to avoid hash collisions
# with cache files of the same name).
STAGE_DIR="$OUTDIR/replay-stage"
rm -rf "$STAGE_DIR" && mkdir -p "$STAGE_DIR"
FUZZ_PKG="$(cd "$HERE" && go list 2>/dev/null || true)"
FUZZ_CACHE="$(go env GOCACHE)/fuzz/$FUZZ_PKG"
for src in "$FUZZ_CACHE" "$HERE/testdata/fuzz"; do
  [ -d "$src" ] || continue
  for targetdir in "$src"/Fuzz*; do
    [ -d "$targetdir" ] || continue
    target="$(basename "$targetdir")"
    mkdir -p "$STAGE_DIR/$target"
    for f in "$targetdir"/*; do
      [ -f "$f" ] || continue
      if [ "$src" = "$HERE/testdata/fuzz" ]; then
        cp "$f" "$STAGE_DIR/$target/seed-$(basename "$f")"
      else
        cp "$f" "$STAGE_DIR/$target/$(basename "$f")"
      fi
    done
  done
done
python3 "$HERE/replay.py" --fuzzdir "$STAGE_DIR" --out "$REPLAY_DIR" | tee "$OUTDIR/replay.log"

echo "== [3/4] differential: gc vs graalgo =="
cd "$DH_ROOT"
go build -o "$OUTDIR/diffharness" ./cmd/diffharness
PHASE1_DIR="$PHASE1_DIR" \
"$OUTDIR/diffharness" run \
  -config "$DH_ROOT/backends.json" \
  -corpus "$REPLAY_DIR" \
  -out "$OUTDIR/report.json" 2>&1 | tee "$OUTDIR/run.log"
rc=$?

echo "== [4/4] verdict =="
if [ $rc -eq 0 ]; then
  echo "FUZZ-NATIVE GREEN: zero divergences (see $OUTDIR/report.json)"
else
  echo "FUZZ-NATIVE RED: divergences above (see $OUTDIR/report.json)"
fi
exit $rc
