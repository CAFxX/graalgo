#!/usr/bin/env bash
# test/m1r/cancel.sh — embedder cancellation of a running program in each
# goroutine mode (playbook §10; GG-0073), run by `make test-goroutine-modes`:
# builds cancel/probe.go through graalgo-tool and runs
# graalgo.launcher.CancelProbe on it, which waits for the program's ready file
# (goroutines parked on a channel and on timers, spinning) and then calls
# Context.close(true): the close must return within
# 2 s, the eval must end cancelled, and no runtime thread may stay alive.
#
# Usage: test/m1r/cancel.sh [MODES]   (default "carrier,thread")
# Env:   as test/m1r/run.sh. Exit: the infra.sh contract.
set -uo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/infra.sh
. "$HERE/../lib/infra.sh"
# shellcheck source=../lib/toolexec.sh
. "$HERE/../lib/toolexec.sh"
ROOT="$GRAALGO_ROOT"
MODES="${1:-carrier,thread}"

infra_setup_go
GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
BK="$ROOT/internal/backend"
[ -f "$BK/truffle/out/test-classes/graalgo/launcher/CancelProbe.class" ] \
    || infra_die "test classes not built (CancelProbe missing) — run \`make java-test-classes\`"
infra_mktemp m1r-cancel
toolexec_setup
mkdir -p "$TMP/mod"
cp "$HERE/cancel/probe.go" "$TMP/mod/main.go"
printf 'module m1r/cancel\n\ngo 1.27\n' > "$TMP/mod/go.mod"
( cd "$TMP/mod" && toolexec_build "$TMP/probe" "$BK/graalgo-bundle-run" ) >"$TMP/build.log" 2>&1 \
    || { cat "$TMP/build.log" >&2; infra_die "graalgo-tool cannot build cancel/probe.go"; }
CP="$BK/truffle/out/test-classes:$BK/truffle/out/classes"
for jar in "$BK"/truffle/lib/*.jar; do CP="$CP:$jar"; done
status=0
# shellcheck disable=SC2086
timeout 120 "$GRAALVM_HOME/bin/java" --enable-native-access=ALL-UNNAMED \
    --add-exports java.base/jdk.internal.vm=ALL-UNNAMED -Xss32m -XX:-UsePerfData \
    -XX:G1HeapRegionSize=32m -Djava.lang.invoke.MethodHandle.CUSTOMIZE_THRESHOLD=-1 \
    --sun-misc-unsafe-memory-access=allow ${GRAALGO_JAVA_OPTS:-} \
    -cp "$CP" graalgo.launcher.CancelProbe "$TMP/probe" "$MODES" </dev/null || status=$?
[ "$status" -eq 0 ] && echo "m1r cancel: pass ($MODES)" || echo "m1r cancel: FAIL ($MODES, exit $status)"
exit "$status"
