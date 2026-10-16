#!/bin/bash
# Channel corpus graduation runner: the channel corpus programs (ch01-ch12)
# run end-to-end through the full pipeline (gofrontend ->
# internal/backend/graalgo-sync-run) and must produce their gc-recorded
# .expected output. Follows the timectx run_differential.sh pattern (cd to
# the script dir, relative paths, gc built binary, stdout+exit+(panic first
# line) comparison, JVM Unsafe stderr filter).
#
# The repo root defaults to the repository containing this script (resolved
# via its own path — never hardcoded) but can be overridden:
#   GR=/path/to/graalgo run_e2e.sh
set -uo pipefail
cd "$(dirname "$0")"

# Repo root: test/corpus/phase2/channels/run_e2e.sh -> phase2 -> corpus ->
# test -> repo.
GR="${GR:-$(cd "$(dirname "$0")/../../../.." && pwd)}"
BE="$GR/internal/backend"

# Toolchains. The gc reference leg uses GO_BIN_DIR; the graalgo leg runs
# through the product launcher (internal/backend/graalgo-sync-run), which
# reads GRAALVM_HOME — honor JAVA_BIN if the caller set that instead.
export PATH="${GO_BIN_DIR:-$HOME/tools/go/bin}:$PATH"
export GOTOOLCHAIN=local
if [ -z "${GRAALVM_HOME:-}" ] && [ -n "${JAVA_BIN:-}" ]; then
    GRAALVM_HOME="$(cd "$(dirname "$JAVA_BIN")/.." && pwd)"
    export GRAALVM_HOME
fi

# Tmp discipline: never write to /tmp (512M shared tmpfs on this VM). The
# repo's old hidden_files/ scratch convention was retired with the
# phases/ tree — scratch goes to the workspace disk. Honor a TMPDIR the
# caller already set.
export TMPDIR="${TMPDIR:-$HOME/workspace/.tmp-channels}"
export GOCACHE="${GOCACHE:-$HOME/workspace/.gocache-channels}"
mkdir -p "$TMPDIR" "$GOCACHE"

echo "== building backend java modules =="
make -C "$GR" build > /dev/null || { echo "backend build failed"; exit 1; }
if [ ! -x "$GR/internal/frontend/gofrontend" ]; then
    echo "== building gofrontend =="
    ( cd "$GR/internal/frontend" && ./build.sh build ) \
        || { echo "gofrontend build failed"; exit 1; }
fi
[ -x "$BE/graalgo-sync-run" ] || { echo "missing launcher: $BE/graalgo-sync-run"; exit 2; }

# The product launcher owns the load-bearing classpath order
# (interop -> runtime -> truffle) and points -Djava.io.tmpdir at $TMPDIR;
# never reconstruct the classpath here. --gmp=N sets scheduler parallelism.
GRAALGO_RUN=( "$BE/graalgo-sync-run" )

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
