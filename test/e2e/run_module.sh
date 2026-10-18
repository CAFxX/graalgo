#!/bin/bash
# Module probe: differential runner for published Go modules via graalgo.
#
# Usage: run_module.sh <probe-dir> [case...]
#   probe-dir: contains go.mod (for the gc leg) and one subdir per case,
#              each a package-main program (caseNN/main.go).
#   Compares `go build`+run (gc 1.27.1) vs graalgo-sync-run --gmp=1 and
#   --gmp=4 (the product launcher P3SyncMain): byte-identical stdout and
#   equal exit codes. Panic programs: the first stderr line is compared
#   (wave3 convention).
#
# Backend: [internal/backend/graalgo-sync-run](../../internal/backend/graalgo-sync-run)
# (P3SyncMain) — requires `make build` at the repo root first (it never
# builds). Third-party imports resolve from the local module cache via the
# frontend disk importer; `go mod download` below primes the cache before
# the loop so both legs agree on the pinned module versions.
#
# Environment (set by caller or defaults):
#   GR         repo root (default: derived from this script's location)
#   GO         gc toolchain (default ~/tools/go/bin/go)
#   GRAALVM_HOME GraalVM JDK (default ~/tools/graalvm-jdk-25;
#              honored by graalgo-sync-run)
#   TMPDIR     scratch (default <probe-dir>/.tmp, created; also sets the
#              JVM's java.io.tmpdir via graalgo-sync-run)
#   GR_TIMEOUT per-case graalgo timeout, seconds (default 300). A hung
#              backend is killed and the case fails as TIMEOUT — no
#              in-runner watchdog is needed beyond coreutils `timeout`.
#
# Exit: 0 iff every case passes every leg. Prints a per-case table.
set -uo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
PROBE_DIR="${1:?usage: run_module.sh <probe-dir> [case...]}"
shift
GR="${GR:-$(cd -- "$HERE/../.." && pwd)}"
GO="${GO:-$HOME/tools/go/bin/go}"
GRAALGO_RUN="$GR/internal/backend/graalgo-sync-run"
TMPDIR="${TMPDIR:-$PROBE_DIR/.tmp}"
export TMPDIR
mkdir -p "$TMPDIR"
export GOCACHE="${GOCACHE:-$HOME/workspace/.gocache-modprobe}"
GR_TIMEOUT="${GR_TIMEOUT:-300}"
[[ "$GR_TIMEOUT" =~ ^[0-9]+$ ]] || { echo "run_module.sh: GR_TIMEOUT must be a positive integer (got '$GR_TIMEOUT')"; exit 2; }

[ -x "$GRAALGO_RUN" ] || { echo "run_module.sh: backend launcher missing: $GRAALGO_RUN (run 'make build' at the repo root)"; exit 2; }
[ -x "$GO" ] || { echo "run_module.sh: gc toolchain missing: $GO"; exit 2; }
# Fail fast when the backend was never built: without the compiled classes
# the JVM legs die with confusing ClassNotFound errors instead of a clear
# message. (.built markers are the Makefiles' freshness sentinels.)
for _m in truffle runtime interop; do
    [ -f "$GR/internal/backend/$_m/out/classes/.built" ] || {
        echo "run_module.sh: backend not built: $GR/internal/backend/$_m/out/classes/.built missing (run 'make build' at the repo root)"; exit 2
    }
done
unset _m

filter_stderr() { grep -v '^WARNING: ' || true; }

if [ $# -gt 0 ]; then
    cases=("$@")
else
    cases=()
    for d in "$PROBE_DIR"/case*/; do
        [ -f "$d/main.go" ] && cases+=("$(basename "$d")")
    done
fi
[ "${#cases[@]}" -gt 0 ] || { echo "run_module.sh: no cases in $PROBE_DIR"; exit 2; }

# Prime the module cache once: the gc leg builds from it, and the frontend
# disk importer resolves third-party imports from it. Network needed only
# here (and only when the pinned modules are not cached yet).
( cd "$PROBE_DIR" && GOFLAGS=-mod=mod "$GO" mod download ) \
    || { echo "run_module.sh: 'go mod download' failed in $PROBE_DIR"; exit 2; }

pass=0; fail=0
declare -a rows
for c in "${cases[@]}"; do
    casedir="$PROBE_DIR/$c"
    [ -f "$casedir/main.go" ] || { rows+=("SKIP $c [no main.go]"); continue; }
    # gc leg (module-aware; module cache supplies deps, no per-case network)
    if ! ( cd "$PROBE_DIR" && GOFLAGS=-mod=mod timeout 120 "$GO" build -o "$TMPDIR/$c.gcbin" "./$c" ); then
        rows+=("FAIL $c [gc build failed]"); fail=$((fail+1)); continue
    fi
    timeout 120 "$TMPDIR/$c.gcbin" > "$TMPDIR/$c.gc.out" 2> "$TMPDIR/$c.gc.err"
    gc_exit=$?
    # graalgo legs, each with its own timeout (hang => exit 124 => TIMEOUT)
    timed_out=""
    for gmp in 1 4; do
        timeout "$GR_TIMEOUT" "$GRAALGO_RUN" --gmp="$gmp" "$casedir/main.go" \
            > "$TMPDIR/$c.gr$gmp.out" 2> "$TMPDIR/$c.gr$gmp.err"
        code=$?
        eval "gr${gmp}_exit=$code"
        [ "$code" = 124 ] && timed_out="$timed_out gmp$gmp"
    done
    ok=1; why=""
    [ -n "$timed_out" ] && { ok=0; why="TIMEOUT($timed_out);"; }
    for gmp in 1 4; do
        eval "ge=\$gr${gmp}_exit"
        if ! cmp -s "$TMPDIR/$c.gc.out" "$TMPDIR/$c.gr$gmp.out"; then
            ok=0; why="$why stdout@gmp$gmp;"
        fi
        if [ "$ge" != "$gc_exit" ]; then
            ok=0; why="$why exit$gmp($ge!=$gc_exit);"
        fi
    done
    # panic programs: compare first stderr line (wave3 convention)
    if [ "$gc_exit" -ne 0 ]; then
        gc_line="$(filter_stderr < "$TMPDIR/$c.gc.err" | head -1)"
        for gmp in 1 4; do
            gr_line="$(filter_stderr < "$TMPDIR/$c.gr$gmp.err" | head -1)"
            if [ "$gc_line" != "$gr_line" ]; then
                ok=0; why="$why stderr$gmp;"
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
