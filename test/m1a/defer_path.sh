#!/usr/bin/env bash
# test/m1a/defer_path.sh — the defer-mechanism checks of GG-0071 on the node
# package (part of `make test-m1a`):
#
# 1. Boundary at the open-coding limit L (NodeFactory.MAX_OPEN_DEFERS = 8):
#    test/m1a/programs/defer_limit.go is built through graalgo-tool and run with
#    -Dgraalgo.go.traceDeferPath=true; the trace must show the function with L
#    defer statements open-coded and the ones with L+1, or with a defer in a
#    loop or a range-over-func body, on the chain. A change of L, or of the
#    eligibility rule, turns this red.
# 2. The choice is unobservable: the defer programs of test/m1a/programs run
#    through test/m1a/run.sh (gc as oracle) with
#    -Dgraalgo.go.forceDeferChain=true, i.e. every deferring function on the
#    chain, and must still match gc.
#
# Env: as test/m1a/run.sh. Exit: the infra.sh contract.
set -uo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/infra.sh
. "$HERE/../lib/infra.sh"
ROOT="$GRAALGO_ROOT"

infra_setup_go
GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
export GRAALVM_HOME
RUNNER="$ROOT/internal/backend/graalgo-bundle-run"
[ -f "$ROOT/internal/backend/truffle/out/classes/graalgo/launcher/BundleMain.class" ] \
    || infra_die "backend not built (BundleMain missing) — run \`make build\` at the repo root"
infra_mktemp m1a-defer
TIMEOUT="${M1A_TIMEOUT:-60}"

mkdir -p "$TMP/bin" "$TMP/gotmp" "$TMP/mod"
export GOTMPDIR="$TMP/gotmp" GOFLAGS= CGO_ENABLED=0 GOTOOLCHAIN=local
STUB_ID="GRAALGO-STUB-BUILDID/GRAALGO-STUB-BUILDID/GRAALGO-STUB-BUILDID/GRAALGO-STUB-BUILDID"
( cd "$ROOT/internal/toolexec" \
    && "$GO" build -o "$TMP/bin/graalgo-tool" ./cmd/graalgo-tool \
    && "$GO" build -ldflags="-buildid=$STUB_ID" -o "$TMP/bin/graalgo-stub" ./cmd/graalgo-stub ) \
    || infra_die "cannot build graalgo-tool / graalgo-stub"

fail=0

# ---- 1. which path each function takes
cp "$HERE/programs/defer_limit.go" "$TMP/mod/main.go"
printf 'module m1a/deferpath\n\ngo 1.27\n' > "$TMP/mod/go.mod"
( cd "$TMP/mod" && GRAALGO_STUB="$TMP/bin/graalgo-stub" GRAALGO_RUNNER="$RUNNER" \
    "$GO" build -toolexec "$TMP/bin/graalgo-tool" -o "$TMP/gg.bin" . ) || infra_die "graalgo-tool build failed"
status=0
( cd "$TMP" && GRAALGO_JAVA_OPTS="${GRAALGO_JAVA_OPTS:-} -Dgraalgo.go.traceDeferPath=true" TMPDIR="$TMP" \
    timeout "$TIMEOUT" "$TMP/gg.bin" >/dev/null 2>"$TMP/trace" </dev/null ) || status=$?
infra_check_run defer_limit "$status" "$TMP/trace"
for want in "main.atLimit open-coded" "main.overLimit chain" "main.inLoop chain" \
        "main.outerOpen open-coded" "main.middleChain chain" "main.innerOpen open-coded"; do
    if grep -qx "graalgo: defer path $want" "$TMP/trace"; then
        echo "PASS defer path $want"
    else
        echo "FAIL defer path $want: got $(grep -a "defer path ${want%% *} " "$TMP/trace" || echo nothing)"
        fail=1
    fi
done
# main itself does not defer: no line for it.
if grep -q '^graalgo: defer path main.main ' "$TMP/trace"; then
    echo "FAIL defer path: main.main reported although it has no defer"
    fail=1
fi

# ---- 2. every deferring function forced onto the chain: still gc's output
progs=()
for f in "$HERE"/programs/*.go; do
    grep -q 'defer' "$f" && progs+=("test/m1a/programs/$(basename "$f")")
done
[ "${#progs[@]}" -gt 0 ] || infra_die "no defer programs under test/m1a/programs"
if ! GRAALGO_JAVA_OPTS="${GRAALGO_JAVA_OPTS:-} -Dgraalgo.go.forceDeferChain=true" "$HERE/run.sh" "${progs[@]}"; then
    echo "FAIL forced chain: output differs from gc"
    fail=1
fi

[ "$fail" -eq 0 ] || exit "$DIVERGENCE_EXIT"
echo "defer_path: all checks passed"
exit 0
