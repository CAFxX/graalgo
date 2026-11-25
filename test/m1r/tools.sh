#!/usr/bin/env bash
# test/m1r/tools.sh — Truffle tools on a running Go program (WP-TR-AUDIT-6,
# playbook §12): builds tools/probe.go through graalgo-tool and runs
# graalgo.launcher.ToolsProbe on it (its Javadoc lists the checks: a
# statement/root ExecutionListener counts main.fib's calls and statements at
# their lines, a collecting CPUSampler records Go names and main.go
# sections — both goroutine modes). Run by `make
# test-goroutine-modes` in both modes.
#
# Usage: test/m1r/tools.sh [MODES]   (default "carrier")
# Env:   as test/m1r/run.sh. Exit: the infra.sh contract.
set -uo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/infra.sh
. "$HERE/../lib/infra.sh"
# shellcheck source=../lib/toolexec.sh
. "$HERE/../lib/toolexec.sh"
ROOT="$GRAALGO_ROOT"
MODES="${1:-carrier}"

infra_setup_go
GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
BK="$ROOT/internal/backend"
[ -f "$BK/truffle/out/test-classes/graalgo/launcher/ToolsProbe.class" ] \
    || infra_die "test classes not built (ToolsProbe missing) — run \`make java-test-classes\`"
infra_mktemp m1r-tools
toolexec_setup
mkdir -p "$TMP/mod"
cp "$HERE/tools/probe.go" "$TMP/mod/main.go"
printf 'module m1r/tools\n\ngo 1.27\n' > "$TMP/mod/go.mod"
( cd "$TMP/mod" && toolexec_build "$TMP/probe" "$BK/graalgo-bundle-run" ) >"$TMP/build.log" 2>&1 \
    || { cat "$TMP/build.log" >&2; infra_die "graalgo-tool cannot build tools/probe.go"; }
CP="$BK/truffle/out/test-classes:$BK/truffle/out/classes"
for jar in "$BK"/truffle/lib/*.jar; do CP="$CP:$jar"; done
status=0
# shellcheck disable=SC2086
timeout 300 "$GRAALVM_HOME/bin/java" --enable-native-access=ALL-UNNAMED \
    --add-exports java.base/jdk.internal.vm=ALL-UNNAMED -Xss32m -XX:-UsePerfData \
    -XX:G1HeapRegionSize=32m -Djava.lang.invoke.MethodHandle.CUSTOMIZE_THRESHOLD=-1 \
    --sun-misc-unsafe-memory-access=allow ${GRAALGO_JAVA_OPTS:-} \
    -cp "$CP" graalgo.launcher.ToolsProbe "$TMP/probe" "$MODES" </dev/null || status=$?
[ "$status" -eq 0 ] && echo "m1r tools: pass ($MODES)" || echo "m1r tools: FAIL ($MODES, exit $status)"
exit "$status"
