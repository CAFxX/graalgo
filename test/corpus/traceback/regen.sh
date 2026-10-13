#!/bin/bash
# Regenerates .expected files for the P3-M4 traceback corpus using stock
# gc 1.27.1, normalized via normalize.py (see README). Review the diff
# before committing: a changed .expected means a semantic change.
set -euo pipefail
cd "$(dirname "$0")"
export PATH="$HOME/tools/go-1.27.1/bin:$PATH"

for go in tb*.go; do
    base="${go%.go}"
    go build -o "/tmp/$base.gc" "$go" || { echo "FAIL: $go gc build failed"; exit 1; }
    "/tmp/$base.gc" > /tmp/tb-regen.out 2> /tmp/tb-regen.err || true
    cat /tmp/tb-regen.out /tmp/tb-regen.err | ./normalize.py > "$base.expected"
    echo "$base: ok"
done
