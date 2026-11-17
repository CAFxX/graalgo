#!/bin/bash
# Differential runner for the lifecycle corpus (T0.A): runs every lc*.go
# under graalgo (internal/backend/graalgo-sync-run; `make build` first)
# with GOMAXPROCS=1 and stdin at /dev/null, and compares stdout, the
# normalized stderr (normalize.py) and the exit code with the gc-recorded
# .expected file (regen.sh). One JVM at a time; a hang is caught by the
# per-program timeout (DH_TIMEOUT, default 120 s) and fails the program.
#
#   ./run_differential.sh            # whole corpus
#   ./run_differential.sh lc02 lc12  # selected programs (name prefixes)
set -uo pipefail
cd "$(dirname "$0")"
RUN="${GRAALGO_SYNC_RUN:-../../../internal/backend/graalgo-sync-run}"
[ -x "$RUN" ] || { echo "missing $RUN"; exit 2; }
TIMEOUT="${DH_TIMEOUT:-120}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

progs=()
if [ $# -eq 0 ]; then
    progs=(lc*.go)
else
    for p in "$@"; do progs+=("$p"*.go); done
fi

pass=0; fail=0
for src in "${progs[@]}"; do
    base="${src%.go}"
    GOMAXPROCS=1 timeout "$TIMEOUT" nice -n 19 "$RUN" "$PWD/$src" \
        < /dev/null > "$TMP/out" 2> "$TMP/err"
    rc=$?
    {
        echo "-- stdout --"
        cat "$TMP/out"
        echo
        echo "-- stderr (normalized) --"
        grep -v '^WARNING: \|^NOTE: Picked up' "$TMP/err" | ./normalize.py
        echo "-- exit $rc --"
    } > "$TMP/got"
    if cmp -s "$base.expected" "$TMP/got"; then
        pass=$((pass+1))
        echo "ok   $base"
    else
        fail=$((fail+1))
        echo "FAIL $base"
        diff "$base.expected" "$TMP/got" | head -20
    fi
done
echo "== lifecycle differential: $pass passed, $fail failed =="
[ "$fail" = 0 ]
