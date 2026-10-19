#!/bin/bash
# Regenerates .expected (stdout) and .panic.gc (gc's first stderr line)
# files for the P3-M2b sync corpus using stock gc 1.27.1. Review the diff
# before committing: a changed .expected means a semantic change.
#
# .panic files (the graalgo leg's expected first stderr line) are NOT
# written by this script — they are hand-maintained, since for the D4
# scoped-divergence programs they intentionally differ from gc.
#
# Panic programs (sy03, sy05, sy08, sy12, sy13, sy14, sy15): gc's first
# stderr line goes to .panic.gc; the graalgo leg's expected first line
# goes to .panic (these differ for sy03/sy05 by the ADR-0002 D4 scoped
# divergence: gc fatal-errors, graalgo panics — same message text, same
# exit code).
set -euo pipefail
cd "$(dirname "$0")"
export PATH="$HOME/tools/go-1.27.1/bin:$PATH"

for go in sy*.go; do
    base="${go%.go}"
    case "$base" in
        sy16_*)
            # sy16 asserts GOMAXPROCS(0) == 2: pin GOMAXPROCS so the recorded
            # .expected ("ok") is host-independent.
            GOMAXPROCS=2 go run "$go" > "$base.expected" 2> /tmp/sy-regen.stderr || {
                echo "FAIL: $go exited nonzero"; cat /tmp/sy-regen.stderr; exit 1
            }
            echo "$base: $(wc -l < "$base.expected") stdout lines"
            ;;
        sy03_*|sy05_*|sy08_*|sy12_*|sy13_*|sy14_*|sy15_*)
            go run "$go" > /dev/null 2> /tmp/sy-regen.stderr || true
            head -1 /tmp/sy-regen.stderr > "$base.panic.gc"
            : > "$base.expected"  # panic programs: empty stdout
            echo "$base: gc panic -> $(cat "$base.panic.gc")"
            ;;
        *)
            go run "$go" > "$base.expected" 2> /tmp/sy-regen.stderr || {
                echo "FAIL: $go exited nonzero"; cat /tmp/sy-regen.stderr; exit 1
            }
            echo "$base: $(wc -l < "$base.expected") stdout lines"
            ;;
    esac
done
echo "regen complete — review git diff before committing"
