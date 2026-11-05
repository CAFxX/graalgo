#!/bin/bash
# Differential runner for the P3-M4 map corpus (GG-0020): each program
# runs under stock gc 1.27.1 (built binary, run directly — `go run` masks
# the exit code) and under graalgo-sync-run. Compares stdout bytes, exit
# codes, and — for panic programs — the FULL normalized stderr (P3-M4
# remediation P2-4: first-line-only comparison could not catch a wrong
# innermost frame line, cf. P1-2). Panic stderr is normalized with the
# traceback corpus's normalize.py and compared both differentially
# (gc vs graalgo) and against the checked-in `$base.panic` anchor.
#
# Known JVM Unsafe warnings on stderr are filtered (harness noise, cf.
# p3frontend/DESIGN.md §7).
set -uo pipefail
cd "$(dirname "$0")"

GR="${GR:-$HOME/workspace/graalgo}"
BE="$GR/internal/backend"
export PATH="$HOME/tools/go-1.27.1/bin:$PATH"

# Build precondition: the product launcher (internal/backend/graalgo-sync-run)
# needs the backend classes (internal/backend/*/out/classes) and the
# gofrontend binary (internal/frontend/gofrontend). Build what's missing at
# the repo root; the stdlib gate already builds, so this is normally a
# no-op check. (The old phases/phase{1,2,3} layout was deleted 2026-10-14.)
if [ ! -d "$BE/interop/out/classes" ] || [ ! -d "$BE/runtime/out/classes" ] \
    || [ ! -d "$BE/truffle/out/classes" ]; then
    echo "== building backend (make build) =="
    make -C "$GR" build > /dev/null || { echo "backend build failed"; exit 1; }
fi
if [ ! -x "$GR/internal/frontend/gofrontend" ]; then
    echo "== building gofrontend =="
    ( cd "$GR/internal/frontend" && ./build.sh build > /dev/null ) \
        || { echo "gofrontend build failed"; exit 1; }
fi

GRAALGO_RUN=( "$BE/graalgo-sync-run" )

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# NOTE: the script already did `cd "$(dirname "$0")"` above, so a second
# `cd "$(dirname "$0")"` would resolve relative to the corpus dir and fail.
# Resolve against the current (already-changed) directory instead.
NORM="$(pwd)/../traceback/normalize.py"

pass=0; fail=0
filter_stderr() { grep -v '^WARNING: ' || true; }

run_one() { # $1 = go file, $2 = gmp
    local go="$1" gmp="$2" base="${1%.go}"
    local gcbin="$TMP/$base.gc"

    go build -o "$gcbin" "$go" || { echo "FAIL $base: gc build failed"; fail=$((fail+1)); return; }
    "$gcbin" > "$TMP/gc.out" 2> "$TMP/gc.err"; local gc_exit=$?
    "${GRAALGO_RUN[@]}" --gmp="$gmp" "$go" > "$TMP/gr.out" 2> "$TMP/gr.err"; local gr_exit=$?

    local ok=1
    cmp -s "$TMP/gc.out" "$TMP/gr.out" || { ok=0; echo "FAIL $base (gmp=$gmp): stdout differs"; }
    if [ "$gc_exit" != "$gr_exit" ]; then
        ok=0; echo "FAIL $base (gmp=$gmp): exit gc=$gc_exit graalgo=$gr_exit"
    fi
    if [ -f "$base.panic" ]; then
        # P3-M4 remediation (P1-2/P2-4): compare the FULL normalized
        # stderr, not just the first line — the innermost frame's
        # file:line is part of the contract now.
        filter_stderr < "$TMP/gc.err" | "$NORM" > "$TMP/gc.panic.norm"
        filter_stderr < "$TMP/gr.err" | "$NORM" > "$TMP/gr.panic.norm"
        cmp -s "$TMP/gc.panic.norm" "$TMP/gr.panic.norm" || {
            ok=0; echo "FAIL $base (gmp=$gmp): normalized panic stderr differs"
        }
        cmp -s "$base.panic" "$TMP/gr.panic.norm" || {
            ok=0; echo "FAIL $base (gmp=$gmp): differs from checked-in .panic anchor"
        }
    fi
    cmp -s "$base.expected" "$TMP/gr.out" || { ok=0; echo "FAIL $base (gmp=$gmp): differs from .expected"; }
    if [ "$ok" = 1 ]; then
        pass=$((pass+1))
    else
        fail=$((fail+1))
        echo "--- $base gc stdout:"; cat "$TMP/gc.out"
        echo "--- $base graalgo stdout:"; cat "$TMP/gr.out"
        echo "--- $base graalgo stderr (filtered):"; filter_stderr < "$TMP/gr.err" | head -3
    fi
}

for go in mp*.go; do
    case "${go%.go}" in
        mp03_*) run_one "$go" 1 ;;
        *) run_one "$go" 1; run_one "$go" 4 ;;
    esac
done

echo "== differential: $pass passed, $fail failed =="
[ "$fail" = 0 ]
