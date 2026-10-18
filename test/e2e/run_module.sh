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
# The graalgo leg invokes internal/backend/graalgo-sync-run directly: the
# product launcher (P3SyncMain, the same backend the `graalgo` driver
# routes to). It preserves the raw program exit code (panic = 2, like gc),
# so the differential is byte-identical against the gc binary. The launcher
# self-resolves gofrontend (via ToolPaths) and GRAALVM_HOME; no classpath
# or environment surgery is needed here.
#
# Requires `make build` (backend classes) and `make generate` (gofrontend)
# to have been run first at the repo root; this script never builds.
#
# Environment (set by caller or defaults):
#   GR              graalgo repo root (default: derived from this script's
#                   location; the old hardcoded worktree path is gone)
#   GO              gc toolchain (default ~/tools/go/bin/go)
#   GRAALGO_SYNC_RUN
#                   launcher override (default $GR/internal/backend/graalgo-sync-run)
#   TMPDIR          scratch (default <probe-dir>/.tmp, created)
#   GRAALGO_TIMEOUT per-leg timeout in seconds (default 300). Hang
#                   protection: a hung program is killed here and fails the
#                   case instead of stalling the suite (timers do not fire
#                   under graalgo, so no in-test watchdog is possible).
#
# Exit: 0 iff every case passes every leg. Prints a per-case table.
set -uo pipefail

PROBE_DIR="${1:?usage: run_module.sh <probe-dir> [case...]}"
shift
# Absolute early: the gc leg cd's into PROBE_DIR, so a relative TMPDIR
# would stop resolving there (go: "creating work dir: stat ...").
PROBE_DIR="$(cd -- "$PROBE_DIR" && pwd)"
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
GR="${GR:-$(cd "$SCRIPT_DIR/../.." && pwd)}"
GO="${GO:-$HOME/tools/go/bin/go}"
GRAALGO_SYNC_RUN="${GRAALGO_SYNC_RUN:-$GR/internal/backend/graalgo-sync-run}"
GRAALGO_TIMEOUT="${GRAALGO_TIMEOUT:-300}"
TMPDIR="${TMPDIR:-$PROBE_DIR/.tmp}"
export TMPDIR
mkdir -p "$TMPDIR"
export GOCACHE="${GOCACHE:-$HOME/workspace/.gocache-modprobe}"

[ -x "$GRAALGO_SYNC_RUN" ] || { echo "error: launcher not executable: $GRAALGO_SYNC_RUN (run 'make build' first)" >&2; exit 2; }

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
        timeout -k 30 "$GRAALGO_TIMEOUT" "$GRAALGO_SYNC_RUN" --gmp="$gmp" "$casedir/main.go" \
            > "$TMPDIR/$c.gr$gmp.out" 2> "$TMPDIR/$c.gr$gmp.err"
        eval "gr${gmp}_exit=$?"
        eval "gr${gmp}_timeout=0"
        eval "[ \$gr${gmp}_exit = 124 ] && gr${gmp}_timeout=1"
    done
    ok=1; why=""
    for gmp in 1 4; do
        eval "ge=\$gr${gmp}_exit"; eval "gt=\$gr${gmp}_timeout"
        if [ "$gt" = 1 ]; then
            ok=0; why="$why hang@gmp$gmp;"
            continue
        fi
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
            eval "gt=\$gr${gmp}_timeout"
            [ "$gt" = 1 ] && continue
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
