#!/bin/bash
# Module probe: differential runner for published Go modules via graalgo.
#
# Usage: run_module.sh <probe-dir> [case...]
#   probe-dir: contains go.mod (for the gc leg) and one subdir per case,
#              each a package-main program (caseNN/main.go).
#   Compares `go run` (gc 1.27.1) vs graalgo (internal/backend/graalgo-run,
#   the phase-1 launcher per test/e2e/README.md): byte-identical stdout
#   and equal exit codes. Panic programs: the FULL normalised stderr is
#   compared (not just the first line — first-line-only comparison masked
#   panic-trace divergences, the same fidelity gap excluded as
#   27_panic_trace in the snapshot corpus).
#
# Environment (set by caller or defaults):
#   GR        repo root (default: the checkout containing this script)
#   GO        gc toolchain (default ~/tools/go/bin/go)
#   TMPDIR    scratch (default <probe-dir>/.tmp, created)
#
# Exit: 0 iff every case passes. Prints a per-case table.
set -uo pipefail

PROBE_DIR="${1:?usage: run_module.sh <probe-dir> [case...]}"
shift
# Absolutise the probe dir up front: TMPDIR defaults under it and the gc
# leg cds into it, so a relative probe path would otherwise make TMPDIR
# resolve against the wrong directory inside the gc leg.
[ -d "$PROBE_DIR" ] || { echo "probe dir not found: $PROBE_DIR" >&2; exit 2; }
PROBE_DIR="$(cd -- "$PROBE_DIR" && pwd)"
# Resolve the script's own directory BEFORE any cd: BASH_SOURCE[0] is
# relative to the caller's cwd, so it must be absolutised first.
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# Repo root: this script lives at test/e2e/ in the checkout.
# (No stale-worktree default: the old m9-modprobe default pointed at a
# worktree that no longer exists.)
GR="${GR:-$(cd "$SCRIPT_DIR/../.." && pwd)}"
GO="${GO:-$HOME/tools/go/bin/go}"
TMPDIR="${TMPDIR:-$PROBE_DIR/.tmp}"
export TMPDIR
mkdir -p "$TMPDIR"
export GOCACHE="${GOCACHE:-$HOME/workspace/.gocache-modprobe}"

echo "== building backend java modules =="
make -C "$GR" build > /dev/null || { echo "backend build failed"; exit 1; }
# gofrontend is a gitignored build output (internal/frontend/gofrontend);
# the launcher resolves it via ToolPaths' walk-up.
if [ ! -x "$GR/internal/frontend/gofrontend" ]; then
    echo "== building gofrontend =="
    make -C "$GR" generate > /dev/null || { echo "gofrontend build failed"; exit 1; }
fi

# Product launcher per test/e2e/README.md: internal/backend/graalgo-run
# (the phase-1 launcher; classpath order lives in the wrapper, not
# here). NOTE: graalgo-run takes no --gmp flag, so there is a single
# graalgo leg per case, not the old --gmp=1/--gmp=4 pair (scheduler
# legs live in the wave/sync corpora via graalgo-sync-run).
GRAALGO_RUN=( "$GR/internal/backend/graalgo-run" )
# gofrontend: prefer the checkout build, else the launcher's walk-up/PATH.
if [ -x "$GR/internal/frontend/gofrontend" ]; then
    export GRAALGO_GOFRONTEND="$GR/internal/frontend/gofrontend"
fi

filter_stderr() { grep -v '^WARNING: ' || true; }
# Full-stderr comparison for panic programs: normalise both legs with
# the traceback corpus's normaliser (goroutine ids, frame offsets,
# absolute paths, "created by" lines) and compare the whole output.
# The normaliser path is resolved from this script's checkout, not from
# the caller's cwd.
NORM="$GR/test/corpus/traceback/normalize.py"
normalize_stderr() { filter_stderr | python3 "$NORM"; }

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
    # graalgo leg (single leg: graalgo-run takes no --gmp flag).
    "${GRAALGO_RUN[@]}" "$casedir/main.go" \
        > "$TMPDIR/$c.gr.out" 2> "$TMPDIR/$c.gr.err"
    gr_exit=$?
    ok=1; why=""
    if ! cmp -s "$TMPDIR/$c.gc.out" "$TMPDIR/$c.gr.out"; then
        ok=0; why="$why stdout;"
    fi
    if [ "$gr_exit" != "$gc_exit" ]; then
        ok=0; why="$why exit($gr_exit!=$gc_exit);"
    fi
    # Panic programs: compare the FULL normalised stderr, not just the
    # first line. A first-line match with divergent frames underneath
    # used to pass silently; the normaliser removes only incidental
    # details (ids, offsets, paths), so a mismatch here is real.
    if [ "$gc_exit" -ne 0 ]; then
        if ! cmp -s <(normalize_stderr < "$TMPDIR/$c.gc.err") \
                      <(normalize_stderr < "$TMPDIR/$c.gr.err"); then
            ok=0; why="$why stderr;"
        fi
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
