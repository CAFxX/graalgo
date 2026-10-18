#!/bin/bash
# Differential runner for the P3-M7 math corpus (ADR-0007 D5): each program
# runs under stock gc 1.27.1 and under graalgo. Compares stdout bytes
# (programs print with %x / %.17g to catch single-bit differences).
#
# The repo root defaults to the repository containing this script (resolved
# via its own path — never hardcoded) but can be overridden with GR or
# GRAALGO_HOME for worktree testing.
set -uo pipefail
cd "$(dirname "$0")"

GR="${GR:-${GRAALGO_HOME:-$(cd "../../../.." && pwd)}}"
# Post-phase-4 layout (migration e5e6dbd9 + fbe5b563: the phases/ tree is
# gone; derive every module path from the repo root, never hardcode):
#   INTEROP <- phases/phase3 (stdlib bindings; the module this runner builds)
#   RUNTIME <- phases/phase2 + phases/phase1 runtime/ (sched/jfr/runtime/...)
#   TRUFFLE <- phases/phase1 nodes/values/frontend (Truffle AST impl) + stubs
# Classpath order (INTEROP:RUNTIME:TRUFFLE:jars) mirrors
# internal/backend/interop/Makefile's RUN_CP: the runtime module's
# same-FQN replacements shadow the truffle module's phase-1 stubs.
INTEROP="$GR/internal/backend/interop"
RUNTIME="$GR/internal/backend/runtime"
TRUFFLE="$GR/internal/backend/truffle"
export PATH="${GO_BIN_DIR:-$HOME/tools/go/bin}:$PATH"
# Tmp discipline (corpus/wave1/README.md; repo AGENTS.md §5 no-debris):
# never write to /tmp, and never re-create hidden_files/ under the deleted
# phases/ path. Inherit TMPDIR from the environment; standalone runs default
# to the gitignored .tmp/ tree.
export TMPDIR="${TMPDIR:-$GR/.tmp/wave1/math}"
export GOCACHE="$HOME/workspace/.gocache-m7-math"
mkdir -p "$TMPDIR"
JAVA="${JAVA_BIN:-$HOME/tools/graalvm-jdk-25/bin/java}"

echo "== building backend java modules (interop) =="
make -C "$INTEROP" main > /dev/null || { echo "interop build failed"; exit 1; }

CP="$INTEROP/out/classes:$RUNTIME/out/classes:$TRUFFLE/out/classes:$(ls "$TRUFFLE"/lib/*.jar | tr '\n' ':')"
# P3-M7R2: -Djava.io.tmpdir keeps the JVM off /tmp (a full /tmp silently
# yields empty output with exit 0 — methodology review finding).
GRAALGO_RUN=( "$JAVA" --enable-native-access=ALL-UNNAMED -Xss32m \
    -Djava.io.tmpdir="$TMPDIR" -cp "$CP" \
    graalgo.p3sync.launcher.P3SyncMain )

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0; fail=0
filter_stderr() { grep -v '^WARNING: ' || true; }

run_one() { # $1 = go file
    local go="$1" base="${1%.go}"
    local gcbin="$TMP/$base.gc"

    go build -o "$gcbin" "$go" || { echo "FAIL $base: gc build failed"; fail=$((fail+1)); return; }
    "$gcbin" > "$TMP/gc.out" 2> "$TMP/gc.err"; local gc_exit=$?
    "${GRAALGO_RUN[@]}" --gmp=1 "$go" > "$TMP/gr.out" 2> "$TMP/gr.err"; local gr_exit=$?

    local ok=1
    cmp -s "$TMP/gc.out" "$TMP/gr.out" || { ok=0; echo "FAIL $base: stdout differs"; diff "$TMP/gc.out" "$TMP/gr.out" | head -5; }
    if [ "$gc_exit" != "$gr_exit" ]; then
        ok=0; echo "FAIL $base: exit gc=$gc_exit graalgo=$gr_exit"
    fi
    if [ "$ok" = 1 ]; then
        echo "PASS $base"; pass=$((pass+1))
    else
        fail=$((fail+1))
    fi
}

for f in m00_bits.go m01_transcendentals.go m02_binary.go m03_special.go m04_bessel.go; do
    run_one "$f"
done

echo "== $pass passed, $fail failed =="
[ "$fail" = 0 ]
