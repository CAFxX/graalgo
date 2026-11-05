#!/bin/bash
# Module probe: differential runner for published Go modules via graalgo.
#
# Usage: run_module.sh <probe-dir> [case...]
#   probe-dir: contains go.mod (for the gc leg) and one subdir per case,
#              each a package-main program (caseNN/main.go).
#   Compares `go run` (gc 1.27.1) vs graalgo P3SyncMain --gmp=1 and --gmp=4:
#   byte-identical stdout and equal exit codes. Panic programs: the first
#   stderr line is compared (wave3 convention).
#
# Environment (set by caller or defaults):
#   GR        repo root (default ~/workspace/graalgo)
#   GO        gc toolchain (default ~/tools/go/bin/go)
#   JAVA      GraalVM java (default ~/tools/graalvm-jdk-25/bin/java)
#   TMPDIR    scratch (default <probe-dir>/.tmp, created)
#
# Exit: 0 iff every case passes every leg. Prints a per-case table.
set -uo pipefail

PROBE_DIR="${1:?usage: run_module.sh <probe-dir> [case...]}"
shift
GR="${GR:-$HOME/workspace/graalgo}"
GO="${GO:-$HOME/tools/go/bin/go}"
JAVA="${JAVA:-$HOME/tools/graalvm-jdk-25/bin/java}"
# The product launcher resolves its JVM from GRAALVM_HOME; JAVA still
# overrides it when GRAALVM_HOME is unset.
if [ -z "${GRAALVM_HOME:-}" ] && [ -n "${JAVA:-}" ]; then
    export GRAALVM_HOME="$(dirname "$(dirname "$JAVA")")"
fi
BE="$GR/internal/backend"
TMPDIR="${TMPDIR:-$PROBE_DIR/.tmp}"
export TMPDIR
mkdir -p "$TMPDIR"
export GOCACHE="${GOCACHE:-$HOME/workspace/.gocache-modprobe}"

# Build precondition: the product launcher (internal/backend/graalgo-sync-run)
# needs the backend classes (internal/backend/*/out/classes) and the
# gofrontend binary (internal/frontend/gofrontend). Build what's missing at
# the repo root; the stdlib gate already builds, so this is normally a
# no-op check. (The old phases/phase{1,2,3} layout was deleted 2026-10-14.)
if [ ! -d "$BE/interop/out/classes" ] || [ ! -d "$BE/runtime/out/classes" ] \
    || [ ! -d "$BE/truffle/out/classes" ]; then
    echo "== building backend (make build) =="
    make -C "$GR" build > /dev/null || { echo "backend build failed"; exit 1; }
fi
if [ ! -x "$GR/internal/frontend/gofrontend" ]; then
    echo "== building gofrontend =="
    ( export PATH="$(dirname "$GO"):$PATH"
      cd "$GR/internal/frontend" && ./build.sh build > /dev/null ) \
        || { echo "gofrontend build failed"; exit 1; }
fi

# The product launcher inherits -Djava.io.tmpdir from $TMPDIR (exported above).
GRAALGO_RUN=( "$BE/graalgo-sync-run" )
# gofrontend: prefer the worktree build, else PATH.
if [ -x "$GR/internal/frontend/gofrontend" ]; then
    export GRAALGO_GOFRONTEND="$GR/internal/frontend/gofrontend"
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
