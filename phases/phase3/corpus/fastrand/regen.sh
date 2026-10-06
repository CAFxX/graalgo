#!/bin/bash
# Regenerates .expected files for the P3-M4 fastrand corpus using stock gc
# 1.27.1. Review the diff before committing: a changed .expected means a
# semantic change.
set -euo pipefail
cd "$(dirname "$0")"
export PATH="$HOME/tools/go-1.27.1/bin:$PATH"

for go in fr*.go; do
    base="${go%.go}"
    go run "$go" > "$base.expected" 2> /tmp/fa-regen.stderr || {
        echo "FAIL: $go exited nonzero"; cat /tmp/fa-regen.stderr; exit 1
    }
    echo "$base: ok"
done
