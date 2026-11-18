# shellcheck shell=bash
# test/lib/infra.sh — shared verdict plumbing for graalgo's shell-level
# differential runners (test/corpus/*, test/e2e, tools/fuzz-soak,
# tools/torture, tools/godebug-diff). Source it; do not execute it.
#
# Exit-code contract (every script that sources this file follows it):
#   0  PASS        — every case matched the gc reference
#   1  DIVERGENCE  — at least one real behavior difference (or a corpus
#                    bug such as a program gc itself rejects)
#   2  USAGE       — bad command-line arguments
#   3  INFRA       — the harness could not produce a verdict: launcher,
#                    java, compiled backend classes, gofrontend or the gc
#                    toolchain missing, or the JVM failed to load a class
#                    (ClassNotFoundException & co). Never reported as a
#                    divergence and never as a pass.
#
# Layout (docs/REPO_STRUCTURE.md): product launcher
# internal/backend/graalgo-sync-run (P3SyncMain), backend classes under
# internal/backend/{truffle,interop}/out/classes (built by
# `make build`), frontend binary internal/frontend/gofrontend (built by
# `make generate`). This library never builds anything: a missing build
# is an INFRA error telling the user what to run.
#
# Overridable environment:
#   GRAALGO_ROOT       repo root (default: derived from this file's path)
#   GO                 gc reference toolchain (default ~/tools/go/bin/go)
#   GRAALGO_SYNC_RUN   graalgo launcher (default: the in-repo product launcher)
#   GRAALVM_HOME       JDK used by the launcher (default ~/tools/graalvm-jdk-25)
#   GRAALGO_TMP        scratch root (default <repo>/.tmp, gitignored); the
#                      JVM is kept off /tmp (a full /tmp tmpfs silently
#                      yields empty output with exit 0)

INFRA_EXIT=3
DIVERGENCE_EXIT=1
USAGE_EXIT=2

GRAALGO_ROOT="${GRAALGO_ROOT:-$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)}"

# infra_die <msg...>: report an infrastructure failure and exit 3.
infra_die() {
    echo "INFRA: $*" >&2
    exit "$INFRA_EXIT"
}

# infra_setup_go: locate the gc reference toolchain, put it first on PATH.
infra_setup_go() {
    GO="${GO:-$HOME/tools/go/bin/go}"
    [ -x "$GO" ] || infra_die "gc toolchain not found or not executable: $GO (set GO=)"
    PATH="$(dirname -- "$GO"):$PATH"
    export PATH GO
}

# infra_setup_graalgo: locate and sanity-check the product launcher and
# its build products; sets the GRAALGO_RUN array (prefix for every run).
infra_setup_graalgo() {
    local default_run="$GRAALGO_ROOT/internal/backend/graalgo-sync-run"
    GRAALGO_SYNC_RUN="${GRAALGO_SYNC_RUN:-$default_run}"
    [ -x "$GRAALGO_SYNC_RUN" ] || infra_die "graalgo launcher not found or not executable: $GRAALGO_SYNC_RUN"
    GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
    export GRAALVM_HOME
    [ -x "$GRAALVM_HOME/bin/java" ] || infra_die "java not found: $GRAALVM_HOME/bin/java (set GRAALVM_HOME=)"
    # The in-repo launcher needs the compiled backend; check the entry
    # class up front so a missing build is one clear INFRA line instead
    # of N ClassNotFoundExceptions.
    if [ "$GRAALGO_SYNC_RUN" = "$default_run" ]; then
        local b="$GRAALGO_ROOT/internal/backend"
        [ -f "$b/interop/out/classes/graalgo/p3sync/launcher/P3SyncMain.class" ] \
            || infra_die "backend not built ($b/interop/out/classes lacks P3SyncMain) — run \`make build\` at the repo root"
        [ -d "$b/truffle/out/classes" ] \
            || infra_die "backend not built (truffle/out/classes missing) — run \`make build\` at the repo root"
        [ -x "${GRAALGO_GOFRONTEND:-$GRAALGO_ROOT/internal/frontend/gofrontend}" ] \
            || infra_die "gofrontend not built — run \`make generate\` at the repo root"
    fi
    GRAALGO_RUN=( "$GRAALGO_SYNC_RUN" )
}

# infra_mktemp <tag>: create a private scratch dir under GRAALGO_TMP, export
# it as TMPDIR (so the launcher's -Djava.io.tmpdir follows), set $TMP and
# register cleanup on exit.
infra_mktemp() {
    local root="${GRAALGO_TMP:-$GRAALGO_ROOT/.tmp}"
    mkdir -p "$root" || infra_die "cannot create scratch root $root"
    TMP="$(mktemp -d "$root/${1:-run}.XXXXXX")" || infra_die "mktemp failed under $root"
    export TMPDIR="$TMP"
    # shellcheck disable=SC2064
    trap "rm -rf '$TMP'" EXIT
}

# infra_is_failure <exit> <stderr-file>: true when a graalgo run failed for
# an infrastructure reason rather than because of the Go program. Only
# JVM-level load/launch failures qualify; a guest panic or an internal
# graalgo exception (NPE, ClassCastException, ...) is a real failure.
infra_is_failure() {
    local rc="$1" err="$2"
    case "$rc" in 126|127) return 0 ;; esac
    [ "$rc" = 0 ] && return 1
    [ -f "$err" ] || return 1
    grep -q -E 'Could not find or load main class|java\.lang\.ClassNotFoundException|java\.lang\.NoClassDefFoundError|java\.lang\.UnsupportedClassVersionError|Error: Unable to initialize main class|Error occurred during initialization of VM|(java|graalgo-sync-run|graalgo-run): (command )?not found|Could not create the Java Virtual Machine' "$err"
}

# infra_check_run <label> <exit> <stderr-file>: exit 3 (with the evidence)
# if the run was an infrastructure failure; otherwise return normally.
infra_check_run() {
    if infra_is_failure "$2" "$3"; then
        echo "INFRA $1: graalgo run failed for an infrastructure reason (exit $2):" >&2
        grep -v '^WARNING: ' "$3" | head -5 | sed 's/^/    /' >&2
        exit "$INFRA_EXIT"
    fi
}
