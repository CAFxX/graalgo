#!/bin/bash
# Regenerates .expected (stdout) and .panic.gc (gc's first stderr line)
# files for the P3-M4 map corpus using stock gc 1.27.1. Review the diff
# before committing: a changed .expected means a semantic change.
#
# Panic programs (mp03): gc's first stderr line goes to .panic.gc; the
# graalgo leg's expected first line goes to .panic (hand-maintained —
# here identical to gc's).
set -euo pipefail
cd "$(dirname "$0")"
export PATH="$HOME/tools/go-1.27.1/bin:$PATH"

for go in mp*.go; do
    base="${go%.go}"
    case "$base" in
        mp03_*)
            go run "$go" > "$base.expected" 2> /tmp/mp-regen.stderr || true
            head -1 /tmp/mp-regen.stderr > "$base.panic.gc"
            echo "$base: gc panic -> $(cat "$base.panic.gc")"
            ;;
        *)
            go run "$go" > "$base.expected" 2> /tmp/mp-regen.stderr || {
                echo "FAIL: $go exited nonzero"; cat /tmp/mp-regen.stderr; exit 1
            }
            echo "$base: ok"
            ;;
    esac
done
