#!/bin/bash
# Regenerates the .expected files of the lifecycle corpus from stock gc
# (go1.27.1 at ~/tools/go). Each .expected holds gc's stdout, its
# normalized stderr (normalize.py) and its exit code. Review the diff
# before committing: a changed .expected means gc's behavior changed.
set -euo pipefail
cd "$(dirname "$0")"
GO="${GO:-$HOME/tools/go/bin/go}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
for src in lc*.go; do
    base="${src%.go}"
    "$GO" build -o "$TMP/$base" "$src"
    set +e
    GOMAXPROCS=1 timeout 60 "$TMP/$base" < /dev/null > "$TMP/out" 2> "$TMP/err"
    rc=$?
    set -e
    {
        echo "-- stdout --"
        cat "$TMP/out"
        echo
        echo "-- stderr (normalized) --"
        ../../lib/normalize.py < "$TMP/err"
        echo "-- exit $rc --"
    } > "$base.expected"
    echo "$base: exit $rc"
done
