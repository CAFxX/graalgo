#!/bin/bash
# Module probe: differential runner for published Go modules via graalgo.
#
# Usage: run_module.sh <probe-dir> [case...]
#   probe-dir: contains go.mod (for the gc leg) and one subdir per case,
#              each a package-main program (caseNN/main.go).
#   Compares `go run` (gc 1.27.1) vs graalgo P3SyncMain
#   (internal/backend/graalgo-sync-run) --gmp=1 and --gmp=4:
#   byte-identical stdout and equal exit codes. Panic programs: the first
#   stderr line is compared (wave3 convention).
#
# Environment (set by caller or defaults):
#   GR         repo root (default: the repository containing this script,
#              resolved via its own path — never hardcoded)
#   GO         gc toolchain (default: $GO_BIN_DIR/go, else
#              ~/tools/go/bin/go)
#   JAVA_BIN   GraalVM java (default ~/tools/graalvm-jdk-25/bin/java);
#              translated to GRAALVM_HOME for the launcher
#   TMPDIR     scratch (default $HOME/workspace/.tmp-modprobe, created;
#              never /tmp — 512M shared tmpfs)
#
# Exit: 0 iff every case passes every leg. Prints a per-case table.
set -uo pipefail

PROBE_DIR="${1:?usage: run_module.sh <probe-dir> [case...]}"
shift
# Repo root: test/e2e/run_module.sh -> test -> repo.
GR="${GR:-$(cd "$(dirname "$0")/../.." && pwd)}"
BE="$GR/internal/backend"
GO="${GO:-${GO_BIN_DIR:-$HOME/tools/go/bin}/go}"
if [ -z "${GRAALVM_HOME:-}" ]; then
    JAVA_BIN="${JAVA_BIN:-$HOME/tools/graalvm-jdk-25/bin/java}"
    GRAALVM_HOME="$(cd "$(dirname "$JAVA_BIN")/.." && pwd)"
    export GRAALVM_HOME
fi
TMPDIR="${TMPDIR:-$HOME/workspace/.tmp-modprobe}"
export TMPDIR
mkdir -p "$TMPDIR"
export GOCACHE="${GOCACHE:-$HOME/workspace/.gocache-modprobe}"
mkdir -p "$GOCACHE"
export GOTOOLCHAIN=local

echo "== building backend java modules =="
make -C "$GR" build > /dev/null || { echo "backend build failed"; exit 1; }
if [ ! -x "$GR/internal/frontend/gofrontend" ]; then
    echo "== building gofrontend =="
    ( cd "$GR/internal/frontend" && PATH="$(dirname "$GO"):$PATH" ./build.sh build ) \
        || { echo "gofrontend build failed"; exit 1; }
fi
[ -x "$BE/graalgo-sync-run" ] || { echo "missing launcher: $BE/graalgo-sync-run"; exit 2; }

# The product launcher owns the load-bearing classpath order
# (interop -> runtime -> truffle) and points -Djava.io.tmpdir at $TMPDIR;
# never reconstruct the classpath here. --gmp=N sets scheduler parallelism.
GRAALGO_RUN=( "$BE/graalgo-sync-run" )
# gofrontend: prefer the explicit env var, else the repo build.
export GRAALGO_GOFRONTEND="${GRAALGO_GOFRONTEND:-$GR/internal/frontend/gofrontend}"

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
