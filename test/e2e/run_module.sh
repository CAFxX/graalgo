#!/bin/bash
# Module probe: differential runner for published Go modules via graalgo.
#
# Usage: run_module.sh [probe-dir] [case...]
#   probe-dir: contains go.mod (for the gc leg) and one subdir per case,
#              each a package-main program (caseNN/main.go).
#              Default: <repo>/test/e2e/uuid (github.com/google/uuid probe).
#   Compares the gc leg (`go build` + run, go1.27.1) against the graalgo leg
#   (internal/backend launcher): byte-identical stdout and equal exit codes.
#   Panic programs: the first stderr line is compared (wave3 convention).
#
#   The graalgo leg defaults to graalgo-sync-run (the product launcher,
#   graalgo.p3sync.launcher.P3SyncMain), which supports channels/select and
#   the full stdlib surface, so the module corpus runs without exclusions.
#   It is invoked at --gmp=1 and --gmp=4 to cover both scheduler shapes.
#   Set BACKEND=graalgo-run to use the truffle-only launcher instead
#   (single leg — that launcher exposes no --gmp surface).
#
# Third-party module sources are NEVER patched: they resolve unmodified from
# the local Go module cache via the frontend's disk import path. A graalgo
# failure on unmodified sources is a graalgo gap — log it in BUGS.md with a
# reproducer, never patch around it.
#
# Prerequisites (checked by this script, never built by it):
#   make build     at the repo root  (internal/backend/*/out/classes)
#   make generate  at the repo root  (internal/frontend/gofrontend)
#
# Environment (set by caller or defaults):
#   GR           repo root (default: derived from this script's location)
#   BACKEND      graalgo launcher: graalgo-sync-run (default) | graalgo-run
#   GO           gc toolchain (default ~/tools/go/bin/go)
#   GRAALVM_HOME GraalVM JDK (default ~/tools/graalvm-jdk-25)
#   TMPDIR       scratch (default ~/.tmp/e2e-run, created); exported so the
#                launcher's java.io.tmpdir follows it
#
# Hermeticity: GOFLAGS=-mod=mod, GOPROXY=off, GOTOOLCHAIN=local — no network
# is touched. The probe module must already sit in the local module cache;
# if it does not, run `go mod download` in the probe dir once (the one
# network step), then re-run hermetically.
#
# Exit: 0 iff every case passes every leg. Prints a per-case table.
set -uo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
GR="${GR:-$SCRIPT_DIR/../..}"
GR="$(cd -- "$GR" && pwd)"

PROBE_DIR="${1:-$GR/test/e2e/uuid}"
shift || true
[ -d "$PROBE_DIR" ] || { echo "run_module.sh: probe dir not found: $PROBE_DIR" >&2; exit 2; }
PROBE_DIR="$(cd -- "$PROBE_DIR" && pwd)"

BACKEND="${BACKEND:-graalgo-sync-run}"
LAUNCHER="$GR/internal/backend/$BACKEND"
GOFRONTEND="$GR/internal/frontend/gofrontend"
GO="${GO:-$HOME/tools/go/bin/go}"
GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
TMPDIR="${TMPDIR:-$HOME/workspace/.tmp/e2e-run}"
mkdir -p "$TMPDIR"
export TMPDIR

# Hermetic Go environment for both legs (defaults; caller may override).
export GOFLAGS="${GOFLAGS:--mod=mod}"
export GOPROXY="${GOPROXY:-off}"
export GOTOOLCHAIN="${GOTOOLCHAIN:-local}"

# --- prerequisites -------------------------------------------------------
preflight_fail=0
need() { # need <path> <how-to-fix>
    local p="$1"; shift
    if [ ! -e "$p" ]; then
        echo "run_module.sh: missing prerequisite: $p" >&2
        echo "  fix: $*" >&2
        preflight_fail=1
    fi
}
need "$LAUNCHER" "unknown BACKEND=$BACKEND (expected graalgo-sync-run or graalgo-run)"
for mod in truffle runtime interop; do
    need "$GR/internal/backend/$mod/out/classes" "run 'make build' at the repo root ($GR)"
done
need "$GOFRONTEND" "run 'make generate' at the repo root ($GR)"
need "$GO" "install the gc toolchain or set GO=<path-to-go>"
need "$GRAALVM_HOME/bin/java" "install GraalVM or set GRAALVM_HOME"
[ "$preflight_fail" = 0 ] || exit 2

# The launcher resolves its gofrontend via GRAALGO_GOFRONTEND (or its own
# ToolPaths walk-up); pin it explicitly to the tree under test.
export GRAALGO_GOFRONTEND="$GOFRONTEND"

# Module preflight: probe dependencies must resolve from the local module
# cache with no network (GOPROXY=off). Fails fast with the one-time fix.
( cd "$PROBE_DIR" && "$GO" list -m all >/dev/null 2>&1 ) || {
    echo "run_module.sh: probe module deps not in the local module cache (hermetic run, GOPROXY=off)" >&2
    echo "  one-time network step: ( cd $PROBE_DIR && go mod download )" >&2
    echo "  then re-run this script" >&2
    exit 2
}

filter_stderr() { grep -v '^WARNING: ' || true; }

# --- case selection ------------------------------------------------------
if [ $# -gt 0 ]; then
    cases=("$@")
else
    cases=()
    for d in "$PROBE_DIR"/case*/; do
        [ -f "$d/main.go" ] && cases+=("$(basename "$d")")
    done
fi
[ "${#cases[@]}" -gt 0 ] || { echo "run_module.sh: no cases in $PROBE_DIR" >&2; exit 2; }

# --- legs ----------------------------------------------------------------
# graalgo-sync-run (P3SyncMain) exposes --gmp; the truffle-only launcher does not.
declare -a gmps
case "$BACKEND" in
    graalgo-sync-run) gmps=(1 4) ;;
    graalgo-run)      gmps=(0) ;;   # 0 = no --gmp flag
    *) echo "run_module.sh: unknown BACKEND=$BACKEND" >&2; exit 2 ;;
esac
tag_of() { # tag_of <gmp> -> leg tag used in artifact names
    [ "$1" = 0 ] && echo "def" || echo "$1"
}

pass=0; fail=0
declare -a rows
for c in "${cases[@]}"; do
    casedir="$PROBE_DIR/$c"
    [ -f "$casedir/main.go" ] || { rows+=("SKIP $c [no main.go]"); continue; }
    # gc leg (module-aware; module cache supplies deps, no network)
    ( cd "$PROBE_DIR" && "$GO" build -o "$TMPDIR/$c.gcbin" "./$c" ) \
        || { rows+=("FAIL $c [gc build failed]"); fail=$((fail+1)); continue; }
    "$TMPDIR/$c.gcbin" > "$TMPDIR/$c.gc.out" 2> "$TMPDIR/$c.gc.err"
    gc_exit=$?
    # graalgo legs
    for gmp in "${gmps[@]}"; do
        tag="$(tag_of "$gmp")"
        args=()
        [ "$gmp" != 0 ] && args+=( "--gmp=$gmp" )
        "$LAUNCHER" "${args[@]}" "$casedir/main.go" \
            > "$TMPDIR/$c.gr$tag.out" 2> "$TMPDIR/$c.gr$tag.err"
        eval "gr${tag}_exit=$?"
    done
    ok=1; why=""
    for gmp in "${gmps[@]}"; do
        tag="$(tag_of "$gmp")"
        eval "ge=\$gr${tag}_exit"
        if ! cmp -s "$TMPDIR/$c.gc.out" "$TMPDIR/$c.gr$tag.out"; then
            ok=0; why="$why stdout@$tag;"
        fi
        if [ "$ge" != "$gc_exit" ]; then
            ok=0; why="$why exit$tag($ge!=$gc_exit);"
        fi
    done
    # panic programs: compare first stderr line (wave3 convention)
    if [ "$gc_exit" -ne 0 ]; then
        gc_line="$(filter_stderr < "$TMPDIR/$c.gc.err" | head -1)"
        for gmp in "${gmps[@]}"; do
            tag="$(tag_of "$gmp")"
            gr_line="$(filter_stderr < "$TMPDIR/$c.gr$tag.err" | head -1)"
            if [ "$gc_line" != "$gr_line" ]; then
                ok=0; why="$why stderr$tag;"
            fi
        done
    fi
    if [ "$ok" = 1 ]; then
        pass=$((pass+1)); rows+=("PASS $c")
    else
        fail=$((fail+1)); rows+=("FAIL $c [$why]")
    fi
done

printf '%s\n' "${rows[@]}"
echo "== $pass passed, $fail failed =="
[ "$fail" = 0 ]
