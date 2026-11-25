#!/usr/bin/env bash
# test/m1r/leak.sh — the context lifecycle leak gate (playbook §10, §15;
# BUG-0052), run by `make test-goroutine-modes`: builds leak/cycle.go
# through graalgo-tool and runs graalgo.launcher.ContextLeakProbe on it,
# which runs it in 12 successive contexts of one JVM per goroutine mode and
# fails on a wrong result, on a runtime thread outliving its context, or on
# live-heap growth above 2 MB per cycle (a retained context is ~15 MB; its
# Javadoc has the contract).
#
# Usage: test/m1r/leak.sh [MODES]   (default "carrier,thread")
# Env:   as test/m1r/run.sh; LEAKPROBE_JAVA: extra probe JVM flags (e.g.
#        -Dleakprobe.verbose=true prints every cycle's live heap and timing).
# Exit:  the infra.sh contract.
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
[ -f "$BK/truffle/out/test-classes/graalgo/launcher/ContextLeakProbe.class" ] \
    || infra_die "test classes not built (ContextLeakProbe missing) — run \`make java-test-classes\`"
infra_mktemp m1r-leak
toolexec_setup
mkdir -p "$TMP/mod"
cp "$HERE/leak/cycle.go" "$TMP/mod/main.go"
printf 'module m1r/leak\n\ngo 1.27\n' > "$TMP/mod/go.mod"
( cd "$TMP/mod" && toolexec_build "$TMP/cycle" "$BK/graalgo-bundle-run" ) >"$TMP/build.log" 2>&1 \
    || { cat "$TMP/build.log" >&2; infra_die "graalgo-tool cannot build leak/cycle.go"; }
CP="$BK/truffle/out/test-classes:$BK/truffle/out/classes"
for jar in "$BK"/truffle/lib/*.jar; do CP="$CP:$jar"; done
status=0
# shellcheck disable=SC2086
timeout 90 "$GRAALVM_HOME/bin/java" --enable-native-access=ALL-UNNAMED \
    --add-exports java.base/jdk.internal.vm=ALL-UNNAMED -Xss32m -XX:-UsePerfData \
    -XX:G1HeapRegionSize=32m -Djava.lang.invoke.MethodHandle.CUSTOMIZE_THRESHOLD=-1 \
    --sun-misc-unsafe-memory-access=allow -Xmx1g ${GRAALGO_JAVA_OPTS:-} \
    -cp "$CP" ${LEAKPROBE_JAVA:-} graalgo.launcher.ContextLeakProbe "$TMP/cycle" "$MODES" </dev/null || status=$?
[ "$status" -eq 0 ] && echo "m1r leak: pass ($MODES)" || echo "m1r leak: FAIL ($MODES, exit $status)"
exit "$status"
