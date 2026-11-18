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
# Environment (set by caller or defaults; see test/lib/infra.sh):
#   GO                gc toolchain (default ~/tools/go/bin/go)
#   GRAALGO_SYNC_RUN  graalgo launcher (default: this repo's product launcher)
#   GRAALVM_HOME      JDK for the launcher (default ~/tools/graalvm-jdk-25)
#   TMPDIR            scratch (default <probe-dir>/.tmp, created)
#
# Exit: 0 every case passes every leg; 1 a divergence; 3 infrastructure
# error (launcher/backend/toolchain missing, JVM class-load failure).
# Prints a per-case table.
set -uo pipefail

PROBE_DIR="${1:?usage: run_module.sh <probe-dir> [case...]}"
shift
# shellcheck source=../lib/infra.sh
. "$(dirname "$0")/../lib/infra.sh"
infra_setup_go
infra_setup_graalgo
TMPDIR="${TMPDIR:-$PROBE_DIR/.tmp}"
export TMPDIR
mkdir -p "$TMPDIR"

filter_stderr() { grep -v '^WARNING: ' || true; }

if [ $# -gt 0 ]; then
    cases=("$@")
else
    cases=()
    for d in "$PROBE_DIR"/case*/; do
        [ -f "$d/main.go" ] && cases+=("$(basename "$d")")
    done
fi

# Zero cases is never a pass (a wrong probe-dir would otherwise exit 0).
if [ "${#cases[@]}" -eq 0 ]; then
    echo "INFRA: no case*/main.go under $PROBE_DIR" >&2
    exit 3
fi

pass=0; fail=0; skip=0
declare -a rows
for c in "${cases[@]}"; do
    casedir="$PROBE_DIR/$c"
    [ -f "$casedir/main.go" ] || { echo "SKIP $c: no main.go"; skip=$((skip+1)); continue; }
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
        eval "infra_check_run \"$c@gmp$gmp\" \"\$gr${gmp}_exit\" \"$TMPDIR/$c.gr$gmp.err\""
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
echo "== $pass passed, $fail failed, $skip skipped =="
[ "$pass" -gt 0 ] || { echo "INFRA: no case ran" >&2; exit 3; }
[ "$fail" = 0 ] && [ "$skip" = 0 ]
