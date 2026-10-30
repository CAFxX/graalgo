#!/bin/bash
# P2-M2 corpus graduation runner (P3-M5, ADR-0005 Appendix C): the channel
# corpus programs (ch01-ch12) run end-to-end through the full pipeline
# (phase-1 gofrontend -> phase-3 P3SyncMain) and must produce their
# gc-recorded .expected output. Follows the timectx run_differential.sh
# pattern exactly (cd-to-script-dir, relative paths, gc built binary,
# stdout+exit+(panic first line) comparison, JVM Unsafe stderr filter).
set -uo pipefail
cd "$(dirname "$0")"

GR="$HOME/workspace/graalgo"
P3="$GR/phases/phase3"
P2="$GR/phases/phase2"
P1="$GR/phases/phase1"
export PATH="$HOME/tools/go/bin:$PATH"
JAVA="$HOME/tools/graalvm-jdk-25/bin/java"

echo "== building phase-3 java module =="
make -C "$P3" main > /dev/null || { echo "phase3 build failed"; exit 1; }

CP="$P3/out/classes:$P2/out/classes:$P1/out/classes:$(ls "$P1"/lib/*.jar | tr '\n' ':')"
GRAALGO_RUN=( "$JAVA" --enable-native-access=ALL-UNNAMED -Xss32m -cp "$CP"
    graalgo.p3sync.launcher.P3SyncMain )

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0; fail=0
# filter_stderr: drop the known JVM Unsafe warning lines.
filter_stderr() { grep -v '^WARNING: ' || true; }

run_one() { # $1 = go file, $2 = gmp
    local go="$1" gmp="$2" base="${1%.go}"
    local gcbin="$TMP/$base.gc"

    go build -o "$gcbin" "$go" || { echo "FAIL $base: gc build failed"; fail=$((fail+1)); return; }
    "$gcbin" > "$TMP/gc.out" 2> "$TMP/gc.err"; local gc_exit=$?
    timeout 120 "${GRAALGO_RUN[@]}" --gmp="$gmp" "$go" > "$TMP/gr.out" 2> "$TMP/gr.err"; local gr_exit=$?

    local ok=1
    # .expected pins gc's recorded stdout — this is the graduation
    # criterion. A fresh gc run is informational only (racy programs like
    # ch12 may select a panic case non-deterministically).
    if [ -f "$base.expected" ]; then
        cmp -s "$base.expected" "$TMP/gr.out" || { ok=0; echo "FAIL $base (gmp=$gmp): stdout differs from .expected"; }
    else
        cmp -s "$TMP/gc.out" "$TMP/gr.out" || { ok=0; echo "FAIL $base (gmp=$gmp): stdout differs gc-vs-graalgo"; }
    fi
    if [ "$gc_exit" != "$gr_exit" ]; then
        # Exit-code parity is mandatory. The only carve-out is a program
        # carrying an explicit empty <base>.racy marker (genuinely
        # nondeterministic exit, e.g. a racy select): for such programs
        # an exit-code diff is ignored. Keying the carve-out on
        # .expected existence (as before) silently passed deterministic
        # programs that exit nonzero under graalgo with identical stdout
        # (R1-31). No program in this corpus currently carries a .racy
        # marker; ch12's select is deterministic (a send on a closed
        # channel is never ready in a select), so it gets none.
        if [ -f "$base.racy" ]; then
            : # racy program (explicit marker); exit code diff ignored
        else
            ok=0; echo "FAIL $base (gmp=$gmp): exit gc=$gc_exit graalgo=$gr_exit"
        fi
    fi
    if [ -f "$base.panic" ]; then
        local gr_first
        gr_first="$(filter_stderr < "$TMP/gr.err" | head -1)"
        local want
        want="$(cat "$base.panic")"
        if [ "$gr_first" != "$want" ]; then
            ok=0; echo "FAIL $base (gmp=$gmp): stderr first line: got [$gr_first] want [$want]"
        fi
    fi
    if [ "$ok" = 1 ]; then
        pass=$((pass+1)); # echo "ok $base (gmp=$gmp)"
    else
        fail=$((fail+1))
        echo "--- $base gc stdout:"; cat "$TMP/gc.out"
        echo "--- $base graalgo stdout:"; cat "$TMP/gr.out"
        echo "--- $base graalgo stderr (filtered):"; filter_stderr < "$TMP/gr.err" | head -3
    fi
}

for go in ch*.go; do
    run_one "$go" 1
    run_one "$go" 4
done

echo "== e2e graduation: $pass passed, $fail failed =="
[ "$fail" = 0 ]
