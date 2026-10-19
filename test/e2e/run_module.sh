#!/bin/bash
# Module probe: differential runner for published Go modules via graalgo.
#
# Usage: run_module.sh <probe-dir> [case...]
#   probe-dir: contains go.mod (for the gc leg) and one subdir per case,
#              each a package-main program (caseNN/main.go).
#   Compares `go run` (gc 1.27.1) vs graalgo-sync-run --gmp=1 and --gmp=4:
#   byte-identical stdout and equal exit codes. Panic programs: the first
#   stderr line is compared (wave3 convention).
#
# Environment (set by caller or defaults):
#   GR            repo root (default: derived from this file's location)
#   GO            gc toolchain (default ~/tools/go/bin/go)
#   GRAALVM_HOME  GraalVM root (default ~/tools/graalvm-jdk-25)
#   GRAALGO_RUN   product launcher (default $GR/internal/backend/graalgo-sync-run)
#   TMPDIR        scratch (default <probe-dir>/.tmp, created)
#
# Requires `make generate build` to have been run at the repo root first
# (backend classes + the gofrontend binary).
#
# Exit: 0 iff every case passes every leg. Prints a per-case table.
set -uo pipefail

PROBE_DIR="${1:?usage: run_module.sh <probe-dir> [case...]}"
shift
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# Repo root: test/e2e/run_module.sh -> test/e2e -> test -> repo.
GR="${GR:-$(cd "$HERE/../.." && pwd)}"
GO="${GO:-$HOME/tools/go/bin/go}"
export GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
GRAALGO_RUN="${GRAALGO_RUN:-$GR/internal/backend/graalgo-sync-run}"
TMPDIR="${TMPDIR:-$PROBE_DIR/.tmp}"
export TMPDIR
mkdir -p "$TMPDIR"
export GOCACHE="${GOCACHE:-$HOME/workspace/.gocache-modprobe}"

if [ ! -x "$GRAALGO_RUN" ]; then
    echo "run_module.sh: $GRAALGO_RUN missing/not executable" >&2
    exit 2
fi
if [ ! -d "$GR/internal/backend/interop/out/classes" ]; then
    echo "run_module.sh: backend classes not built; run 'make build' at the repo root first" >&2
    exit 2
fi
if [ ! -x "$GR/internal/frontend/gofrontend" ]; then
    echo "run_module.sh: gofrontend not built; run 'make generate' at the repo root first" >&2
    exit 2
fi

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
    ( cd "$PROBE_DIR" && GOFLAGS=-mod=mod GOTOOLCHAIN=local "$GO" build -o "$TMPDIR/$c.gcbin" "./$c" ) \
        || { rows+=("FAIL $c [gc build failed]"); fail=$((fail+1)); continue; }
    "$TMPDIR/$c.gcbin" > "$TMPDIR/$c.gc.out" 2> "$TMPDIR/$c.gc.err"
    gc_exit=$?
    # graalgo legs (product launcher; --gmp varies the scheduler cap)
    for gmp in 1 4; do
        "$GRAALGO_RUN" --gmp="$gmp" -- "$casedir/main.go" \
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
