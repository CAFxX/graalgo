#!/bin/bash
# Differential runner for the P3-M4 traceback corpus (GG-0020): each
# program panics; gc's and graalgo's tracebacks are normalized via
# normalize.py and compared. Exit codes are compared too (gc: 2).
#
# Known JVM Unsafe warnings on stderr are filtered (harness noise, cf.
# p3frontend/DESIGN.md §7).
set -uo pipefail
cd "$(dirname "$0")"

# Backend: the interop product launcher (P3SyncMain) via
# internal/backend/graalgo-sync-run — the same backend the diffharness
# smoke corpus uses (test/differential/run.sh). PHASE3_DIR points at the
# internal/backend tree; defaults to this repo's tree so a plain
# ./run_differential.sh works from a clean checkout. The launcher never
# builds: `make build` at the repo root must have run first; fail fast
# with the fix instead of a confusing java classpath error.
if [ -z "${PHASE3_DIR:-}" ]; then
  PHASE3_DIR="$(cd ../../../internal/backend && pwd)"
fi
export PATH="${GO_BIN_DIR:-$HOME/tools/go/bin}:$PATH"
if [ ! -x "$PHASE3_DIR/graalgo-sync-run" ]; then
  echo "run_differential.sh: $PHASE3_DIR/graalgo-sync-run missing/not executable" >&2
  exit 2
fi
if [ ! -d "$PHASE3_DIR/interop/out/classes" ]; then
  echo "run_differential.sh: backend classes not built; run 'make build' at the repo root first" >&2
  exit 2
fi
GRAALGO_RUN=( "$PHASE3_DIR/graalgo-sync-run" )

# Tmp discipline: never write to /tmp (512M shared tmpfs). Honor a
# caller-supplied TMPDIR; otherwise use the repo-local scratch dir (.tmp/
# is gitignored).
REPO_ROOT="$(cd ../../.. && pwd)"
export TMPDIR="${TMPDIR:-$REPO_ROOT/.tmp/corpus-traceback}"
mkdir -p "$TMPDIR"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0; fail=0
filter_noise() { grep -v '^WARNING: ' | grep -v '^/home/hatch/tools' | grep -v '^make:' || true; }

run_one() { # $1 = go file
    local go="$1" base="${1%.go}"
    local gcbin="$TMP/$base.gc"

    go build -o "$gcbin" "$go" || { echo "FAIL $base: gc build failed"; fail=$((fail+1)); return; }
    "$gcbin" > "$TMP/gc.out" 2> "$TMP/gc.err"; local gc_exit=$?
    "${GRAALGO_RUN[@]}" --gmp=1 "$go" > "$TMP/gr.out" 2> "$TMP/gr.err"; local gr_exit=$?

    cat "$TMP/gc.out" "$TMP/gc.err" | ./normalize.py > "$TMP/gc.norm"
    cat "$TMP/gr.out" "$TMP/gr.err" | filter_noise | ./normalize.py > "$TMP/gr.norm"

    local ok=1
    cmp -s "$TMP/gc.norm" "$TMP/gr.norm" || { ok=0; echo "FAIL $base: normalized traceback differs"; }
    if [ "$gc_exit" != "$gr_exit" ]; then
        ok=0; echo "FAIL $base: exit gc=$gc_exit graalgo=$gr_exit"
    fi
    cmp -s "$base.expected" "$TMP/gr.norm" || { ok=0; echo "FAIL $base: differs from .expected"; }
    if [ "$ok" = 1 ]; then
        pass=$((pass+1))
    else
        fail=$((fail+1))
        echo "--- $base gc (normalized):"; cat "$TMP/gc.norm"
        echo "--- $base graalgo (normalized):"; cat "$TMP/gr.norm"
    fi
}

for go in tb*.go; do
    run_one "$go"
done

echo "== differential: $pass passed, $fail failed =="
[ "$fail" = 0 ]
