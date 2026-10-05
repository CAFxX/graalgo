#!/bin/bash
# Regenerates .expected files for the P2-M3 timer corpus using stock gc.
# Programs are timing-sensitive: only event ORDER is significant, never
# wall-clock values. Margins are generous (50ms+ separations); if a
# program becomes flaky, widen the sleeps, never the comparison.
# Review the diff before committing: a changed .expected means a semantic change.
set -euo pipefail
cd "$(dirname "$0")"
export PATH="$HOME/tools/go/bin:$PATH"
for go in tm*.go; do
    base="${go%.go}"
    if ! go run "$go" > "$base.expected" 2>/tmp/regen.stderr; then
        echo "FAIL: $go exited nonzero"; cat /tmp/regen.stderr; exit 1
    fi
    echo "$base: $(cat "$base.expected" | tr '\n' ' ')"
done
echo "regen complete — review git diff before committing"
