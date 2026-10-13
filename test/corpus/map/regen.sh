#!/bin/bash
# Regenerates .expected (stdout) and .panic.gc (gc's full normalized
# stderr) files for the P3-M4 map corpus using stock gc 1.27.1. Review
# the diff before committing: a changed .expected means a semantic
# change.
#
# Panic programs (mp03, mp04): gc's full stderr, normalized with the
# traceback corpus's normalize.py, goes to .panic.gc; the graalgo leg's
# expected normalized stderr goes to .panic (hand-maintained — here
# identical to gc's normalized output). The differential runner compares
# gc-normalized vs graalgo-normalized live, plus graalgo-normalized vs
# the checked-in .panic anchor.
set -euo pipefail
cd "$(dirname "$0")"
export PATH="$HOME/tools/go-1.27.1/bin:$PATH"

NORM="../traceback/normalize.py"

for go in mp*.go; do
    base="${go%.go}"
    case "$base" in
        mp03_*|mp04_*)
            go run "$go" > "$base.expected" 2> /tmp/mp-regen.stderr || true
            grep -v '^WARNING: ' /tmp/mp-regen.stderr | "$NORM" > "$base.panic.gc"
            echo "$base: gc panic -> $(head -1 "$base.panic.gc")"
            ;;
        *)
            go run "$go" > "$base.expected" 2> /tmp/mp-regen.stderr || {
                echo "FAIL: $go exited nonzero"; cat /tmp/mp-regen.stderr; exit 1
            }
            echo "$base: ok"
            ;;
    esac
done
