#!/bin/bash
# Module probe: differential runner for published Go modules via graalgo.
#
# Usage: run_module.sh <probe-dir> [case...]
#   probe-dir: contains go.mod (for the gc leg) and one subdir per case,
#              each a package-main program (caseNN/main.go).
#   Compares `go build`+run (gc 1.27.1) vs the graalgo product launcher
#   (graalgo-sync-run, P3SyncMain) at --gmp=1 and --gmp=4: byte-identical
#   stdout and equal exit codes. Panic programs: the first stderr line is
#   compared (wave3 convention).
#
# Backend: internal/backend/graalgo-sync-run — the same product launcher
# the diffharness smoke corpus uses (test/differential/run.sh). The
# launcher never builds: `make build` at the repo root must have run
# first; the script fails fast with the fix.
#
# Environment (set by caller or defaults):
#   GR        repo root (default: the repository containing this script,
#             resolved via its own path — never hardcoded)
#   GO        gc toolchain (default ~/tools/go/bin/go)
#   TMPDIR    scratch (default <probe-dir>/.tmp, created)
#
# Exit: 0 iff every case passes every leg. Prints a per-case table.
set -uo pipefail

PROBE_DIR="${1:?usage: run_module.sh <probe-dir> [case...]}"
shift
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
GR="${GR:-$(cd "$SCRIPT_DIR/../.." && pwd)}"
GO="${GO:-$HOME/tools/go/bin/go}"
TMPDIR="${TMPDIR:-$PROBE_DIR/.tmp}"
export TMPDIR
mkdir -p "$TMPDIR"
export GOCACHE="${GOCACHE:-$HOME/workspace/.gocache-modprobe}"
mkdir -p "$GOCACHE"

if [ -z "${PHASE3_DIR:-}" ]; then
  PHASE3_DIR="$GR/internal/backend"
fi
if [ ! -x "$PHASE3_DIR/graalgo-sync-run" ]; then
  echo "run_module.sh: $PHASE3_DIR/graalgo-sync-run missing/not executable" >&2
  exit 2
fi
if [ ! -d "$PHASE3_DIR/interop/out/classes" ]; then
  echo "run_module.sh: backend classes not built; run 'make build' at the repo root first" >&2
  exit 2
fi
# -Djava.io.tmpdir is set inside graalgo-sync-run from $TMPDIR (never /tmp).
GRAALGO_RUN=( "$PHASE3_DIR/graalgo-sync-run" )

filter_stderr() { grep -v '^WARNING: ' || true; }

if [ $# -gt 0 ]; then
    cases=("$@")
else
    cases=()
    for d in "$PROBE_DIR"/case*/; do
        [ -f "$d/main.go" ] && cases+=("$(basename "$d")")
    done
fi

pass=0; fail=0
declare -a rows
for c in "${cases[@]}"; do
    casedir="$PROBE_DIR/$c"
    [ -f "$casedir/main.go" ] || { echo "SKIP $c: no main.go"; continue; }
    # gc leg (module-aware; module cache supplies deps, no network)
    ( cd "$PROBE_DIR" && GOFLAGS=-mod=mod "$GO" build -o "$TMPDIR/$c.gcbin" "./$c" ) \
        || { rows+=("FAIL $c [gc build failed]"); fail=$((fail+1)); continue; }
    "$TMPDIR/$c.gcbin" > "$TMPDIR/$c.gc.out" 2> "$TMPDIR/$c.gc.err"
    gc_exit=$?
    # graalgo legs
    for gmp in 1 4; do
        "${GRAALGO_RUN[@]}" --gmp="$gmp" "$casedir/main.go" \
            > "$TMPDIR/$c.gr$gmp.out" 2> "$TMPDIR/$c.gr$gmp.err"
        eval "gr${gmp}_exit=$?"
    done
    ok=1; why=""
    for gmp in 1 4; do
        eval "ge=\$gr${gmp}_exit"
        if ! cmp -s "$TMPDIR/$c.gc.out" "$TMPDIR/$c.gr$gmp.out"; then
            ok=0; why="$why stdout@gmp$gmp;"
        fi
        if [ "$ge" != "$gc_exit" ]; then
            ok=0; why="$why exit$gmp($ge!=$gc_exit);"
        fi
    done
    # panic programs: compare first stderr line (wave3 convention)
    if [ "$gc_exit" -ne 0 ]; then
        gc_line="$(filter_stderr < "$TMPDIR/$c.gc.err" | head -1)"
        for gmp in 1 4; do
            gr_line="$(filter_stderr < "$TMPDIR/$c.gr$gmp.err" | head -1)"
            if [ "$gc_line" != "$gr_line" ]; then
                ok=0; why="$why stderr$gmp;"
            fi
        done
    fi
    if [ "$ok" = 1 ]; then
        pass=$((pass+1)); rows+=("PASS $c")
    else
        fail=$((fail+1)); rows+=("FAIL $c [$why]")
    fi
done

printf '%s\n' "${rows[@]}"
echo "== $pass passed, $fail failed =="
[ "$fail" = 0 ]
