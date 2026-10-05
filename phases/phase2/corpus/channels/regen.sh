#!/bin/bash
# Regenerates .expected (stdout) and .panic (first stderr line) files for
# the P2-M2 channel corpus using stock gc. Review the diff before
# committing: a changed .expected means a semantic change.
set -euo pipefail
cd "$(dirname "$0")"
export PATH="$HOME/tools/go/bin:$PATH"
for go in ch*.go; do
    base="${go%.go}"
    # Skip panic programs for stdout; they get .panic from stderr.
    if [[ "$base" == ch09* || "$base" == ch10* || "$base" == ch11* ]]; then
        go run "$go" 2>&1 >/dev/null | head -1 > "$base.panic"
        : > "$base.expected"  # panic programs: empty stdout
        echo "$base: panic -> $(cat "$base.panic")"
    else
        go run "$go" > "$base.expected" 2>/tmp/regen.stderr || {
            echo "FAIL: $go exited nonzero"; cat /tmp/regen.stderr; exit 1
        }
        echo "$base: $(wc -l < "$base.expected") stdout lines"
    fi
done
echo "regen complete — review git diff before committing"
