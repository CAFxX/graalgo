#!/bin/bash
# Differential runner for the P3-M3 interface-dispatch corpus (ADR-0003):
# each program runs under stock gc 1.27.1 (built binary, run directly —
# `go run` masks the exit code) and under graalgo (P3SyncMain, which
# registers the "ifacemethod" wire kind and installs the method tables).
# Compares stdout bytes, exit codes, and — for panic programs — the first
# stderr line.
#
# Known JVM Unsafe warnings on stderr are filtered (harness noise, cf.
# p3frontend/DESIGN.md §7). Programs are single-goroutine; one --gmp=1
# run each (the sync corpus's gmp sweep covers the scheduler).
#
# Exit status: 0 pass, 1 divergence, 3 infrastructure error (launcher,
# java, backend build or gc toolchain missing; JVM class-load failure) —
# see test/lib/infra.sh. Needs `make build` + `make generate` first.
set -uo pipefail
cd "$(dirname "$0")"

# shellcheck source=../../lib/infra.sh
. "../../lib/infra.sh"
infra_setup_go
infra_setup_graalgo
infra_mktemp corpus-iface

pass=0; fail=0
# filter_stderr: drop the known JVM Unsafe warning lines.
filter_stderr() { grep -v '^WARNING: ' || true; }

run_one() { # $1 = go file
    local go="$1" base="${1%.go}"
    local gcbin="$TMP/$base.gc"

    go build -o "$gcbin" "$go" || { echo "FAIL $base: gc build failed"; fail=$((fail+1)); return; }
    "$gcbin" > "$TMP/gc.out" 2> "$TMP/gc.err"; local gc_exit=$?
    "${GRAALGO_RUN[@]}" --gmp=1 "$go" > "$TMP/gr.out" 2> "$TMP/gr.err"; local gr_exit=$?
    infra_check_run "$base" "$gr_exit" "$TMP/gr.err"

    local ok=1
    cmp -s "$TMP/gc.out" "$TMP/gr.out" || { ok=0; echo "FAIL $base: stdout differs"; }
    if [ "$gc_exit" != "$gr_exit" ]; then
        ok=0; echo "FAIL $base: exit gc=$gc_exit graalgo=$gr_exit"
    fi
    if [ -f "$base.panic" ]; then
        local gr_first
        gr_first="$(filter_stderr < "$TMP/gr.err" | head -1)"
        local want
        want="$(cat "$base.panic")"
        if [ "$gr_first" != "$want" ]; then
            ok=0; echo "FAIL $base: stderr first line: got [$gr_first] want [$want]"
        fi
    fi
    if [ "$ok" = 1 ]; then
        pass=$((pass+1)); # echo "ok $base"
    else
        fail=$((fail+1))
        echo "--- $base gc stdout:"; cat "$TMP/gc.out"
        echo "--- $base graalgo stdout:"; cat "$TMP/gr.out"
        echo "--- $base graalgo stderr (filtered):"; filter_stderr < "$TMP/gr.err" | head -3
    fi
}

for go in if*.go; do
    run_one "$go"
done

echo "== differential: $pass passed, $fail failed =="
[ "$fail" = 0 ]
