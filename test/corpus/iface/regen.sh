#!/bin/bash
# Regenerates .expected (stdout) and .panic.gc (gc's first stderr line)
# files for the P3-M3 interface-dispatch corpus using stock gc 1.27.1.
# Review the diff before committing: a changed .expected means a semantic
# change.
#
# .panic files (the graalgo leg's expected first stderr line) are NOT
# written by this script — they are hand-maintained (for if06 the message
# is identical to gc's by the nil-panic parity, but the file stays
# hand-owned so a divergence is a loud diff, not a silent regen).
#
# Panic programs (if06): gc's first stderr line goes to .panic.gc; the
# graalgo leg's expected first line goes to .panic. .expected holds the
# stdout printed BEFORE the panic (gc and graalgo must agree byte for
# byte — the differential runner compares it).
set -euo pipefail
cd "$(dirname "$0")"
export PATH="$HOME/tools/go/bin:$PATH"

for go in if*.go; do
    base="${go%.go}"
    case "$base" in
        if06_*)
            go run "$go" > "$base.expected" 2> /tmp/if-regen.stderr || true
            head -1 /tmp/if-regen.stderr > "$base.panic.gc"
            echo "$base: gc panic -> $(cat "$base.panic.gc")"
            ;;
        *)
            go run "$go" > "$base.expected" 2> /tmp/if-regen.stderr || {
                echo "FAIL: $go exited nonzero"; cat /tmp/if-regen.stderr; exit 1
            }
            echo "$base: $(wc -l < "$base.expected") stdout lines"
            ;;
    esac
done
echo "regen complete — review git diff before committing"
