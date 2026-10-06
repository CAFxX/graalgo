#!/bin/bash
# Regenerates .expected files for the P3-M4 atomic corpus using stock gc
# 1.27.1. Review the diff before committing: a changed .expected means a
# semantic change.
set -euo pipefail
cd "$(dirname "$0")"
export PATH="$HOME/tools/go-1.27.1/bin:$PATH"

for go in at*.go; do
    base="${go%.go}"
    go run "$go" > "$base.expected" 2> /tmp/at-regen.stderr || {
        echo "FAIL: $go exited nonzero"; cat /tmp/at-regen.stderr; exit 1
    }
    if [ -s /tmp/at-regen.stderr ]; then
        echo "WARN: $go wrote to stderr:"; cat /tmp/at-regen.stderr
    fi
    echo "$base: ok"
done
