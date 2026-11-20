#!/usr/bin/env bash
# test/m1d/mutants.sh — the M1d gate can fail (AGENTS.md §5): plants each
# mutant of test/m1d/mutants.py in the backend's Java source, rebuilds, runs
# its test/m1d programs through test/m1d/run.sh and requires a FAIL; restores
# the source afterwards (also on interrupt) and rebuilds. A mutant whose
# anchor text no longer occurs exactly once is an error (the list went stale).
#
# Usage: test/m1d/mutants.sh [MUTANT-NAME]...   (heavy: one rebuild per mutant)
# Env:   as test/m1d/run.sh.
set -uo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd -- "$HERE/../.." && pwd)"
cd "$ROOT" || exit 3

backup="" file=""
restore() {
    if [ -n "$backup" ]; then
        cp "$backup" "$file" && rm -f "$backup"
        backup=""
        nice -n 10 make build >/dev/null 2>&1
    fi
}
trap 'restore; exit 3' INT TERM

failed=0 ran=0
while IFS=$'\t' read -r name programs; do
    if [ $# -gt 0 ] && [[ " $* " != *" $name "* ]]; then
        continue
    fi
    ran=$((ran + 1))
    backup="$(mktemp)"
    file="$(python3 "$HERE/mutants.py" list >/dev/null; python3 -c '
import sys; sys.path.insert(0, sys.argv[1]); import mutants
print([m[1] for m in mutants.MUTANTS if m[0] == sys.argv[2]][0])' "$HERE" "$name")"
    cp "$file" "$backup"
    if ! python3 "$HERE/mutants.py" apply "$name" >/dev/null; then
        echo "STALE    $name"
        failed=$((failed + 1)); rm -f "$backup"; backup=""; continue
    fi
    if ! nice -n 10 make build >/dev/null 2>&1; then
        echo "KILLED   $name (does not compile)"; restore; continue
    fi
    # shellcheck disable=SC2086
    if M1D_TIMEOUT="${M1D_TIMEOUT:-60}" "$HERE/run.sh" $programs >/dev/null 2>&1; then
        echo "SURVIVED $name: $programs still pass"
        failed=$((failed + 1))
    else
        echo "KILLED   $name"
    fi
    restore
done < <(python3 "$HERE/mutants.py" list)
echo "m1d mutants: $((ran - failed)) killed of $ran"
[ "$ran" -gt 0 ] && [ "$failed" -eq 0 ]
