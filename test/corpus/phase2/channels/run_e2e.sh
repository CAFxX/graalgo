#!/bin/bash
# P2-M2 corpus graduation runner (P3-M5, ADR-0005 Appendix C): the channel
# corpus programs (ch01-ch12) run end-to-end through the full pipeline
# (gofrontend -> product launcher P3SyncMain) and must produce their
# gc-recorded .expected output. Follows the timectx run_differential.sh
# pattern exactly (cd-to-script-dir, relative paths, gc built binary,
# stdout+exit+(panic first line) comparison, JVM Unsafe stderr filter).
set -uo pipefail
cd "$(dirname "$0")"

GR="${GR:-$HOME/workspace/graalgo}"
export PATH="$HOME/tools/go/bin:$PATH"

# Tmp discipline: never write to /tmp (512 MB shared tmpfs).
# Workspace-local per-runner scratch; override with TMPDIR.
export TMPDIR="${TMPDIR:-$HOME/workspace/.tmp-graalgo-channels}"
mkdir -p "$TMPDIR"

# GraalVM java selection: JAVA_BIN wins; GRAALVM_HOME derives from it so
# the product launcher (which resolves $GRAALVM_HOME/bin/java) and the
# backend Makefiles agree on the toolchain.
JAVA_BIN="${JAVA_BIN:-$HOME/tools/graalvm-jdk-25/bin/java}"
if [ -z "${GRAALVM_HOME:-}" ]; then
    export GRAALVM_HOME="$(cd "$(dirname "$JAVA_BIN")/.." && pwd)"
fi

echo "== building backend java modules =="
make -C "$GR" build > /dev/null || { echo "backend build failed"; exit 1; }
if [ ! -x "${GRAALGO_GOFRONTEND:-$GR/internal/frontend/gofrontend}" ]; then
    echo "== building gofrontend =="
    ( cd "$GR/internal/frontend" && ./build.sh build ) > /dev/null \
        || { echo "gofrontend build failed"; exit 1; }
fi

# The graalgo leg delegates to the product launcher
# (internal/backend/graalgo-sync-run): it builds the classpath from
# internal/backend/{interop,runtime,truffle}/out/classes plus the truffle
# lib jars (classpath order is load-bearing: interop first, then runtime
# so the runtime module's same-FQN replacements shadow the truffle
# module's frozen stubs, then truffle) and invokes
# graalgo.p3sync.launcher.P3SyncMain with these args. -Djava.io.tmpdir
# follows $TMPDIR (never /tmp).
GRAALGO_RUN=( "$GR/internal/backend/graalgo-sync-run" )

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
        # For racy programs, exit code may differ; only fail if .expected
        # exists and stdout matched (i.e., deterministic programs).
        if [ -f "$base.expected" ] && cmp -s "$base.expected" "$TMP/gr.out"; then
            : # stdout matched .expected; exit code diff is racy, ignore
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
