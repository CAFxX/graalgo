#!/bin/bash
# Differential runner for the wave-1 corpus (see README.md): each program
# runs under stock gc 1.27.1 (built binary, run directly — `go run` masks
# the exit code) and under internal/backend/graalgo-sync-run (P3SyncMain).
# Compares stdout bytes, exit codes, and stderr against the live gc leg:
# for panic programs (non-zero gc exit) graalgo's first filtered stderr
# line must match gc's live first line; for all other programs the full
# filtered stderr must match. A <base>.panic anchor, when present,
# additionally pins gc's own panic message, so a toolchain message change
# fails loudly instead of silently pinning stale text.
#
# Known JVM Unsafe warnings on stderr are filtered (harness noise, cf.
# internal/frontend/p3frontend/DESIGN.md §7). Timing-sensitive programs
# (marked with an empty <base>.timing file) print fixed strings only
# (never measured durations) and run with gmp=1 only; all other programs
# run with gmp=1 and gmp=4 (the timectx convention).
#
# Scope-limited programs (marked with a <base>.scopelimited file naming
# the reason and a retest: trigger) can never pass the frontend for honest
# scope reasons. They are NOT run under graalgo and are counted separately
# (neither passing nor failing); the gc leg still builds and runs them so
# a program that is not even valid Go surfaces as FAIL, not scope-limited.
# A .scopelimited file without a retest: trigger is a harness FAIL.
#
# Synthetic-reference programs (marked with an empty <base>.synthetic file)
# use M7-extension constructors (csv.NewReaderString, gob.NewBufferEncoder,
# ...) absent from real std, so the gc leg cannot build them against GOROOT
# (a GOPATH cannot shadow the standard library). For these, the gc leg
# builds the program against the extracted synthetic sources
# (extract_synth_ref.py, reading internal/frontend/synth_*.go — the same
# sources validated as gc-faithful) with imports rewritten to a temp GOPATH.
#
# Usage: run_wave.sh [pkg|all] — run one package subdir, or every wave-1
# package dir when omitted. Underscore-prefixed dirs (_smoke, infra) run
# only when named explicitly.
#
# The repo root defaults to the repository containing this script (resolved
# via its own path — never hardcoded) but can be overridden:
#   GR=/path/to/graalgo run_wave.sh strings
set -uo pipefail
# Resolve the script's own directory to an absolute path BEFORE cd'ing: a
# relative $0 would otherwise make the GR resolution below resolve against
# the new cwd instead of the invocation cwd (breaking e.g. the _smoke
# run_differential.sh wrapper when invoked via a relative path).
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# Repo root: test/corpus/wave1/run_wave.sh -> corpus -> test -> repo.
# The caller may export GR (tools/run_stdlib_gate.sh does); otherwise
# resolve it from this script's location.
GR="${GR:-$(cd "$SCRIPT_DIR/../../.." && pwd)}"
BE="$GR/internal/backend"

# Toolchains. The gc reference leg uses GO_BIN_DIR (the stdlib-gate CI job
# exports it); the graalgo leg runs through the product launcher
# (internal/backend/graalgo-sync-run), which reads GRAALVM_HOME — honor
# JAVA_BIN if the caller set that instead.
export PATH="${GO_BIN_DIR:-$HOME/tools/go/bin}:$PATH"
export GOTOOLCHAIN=local
if [ -z "${GRAALVM_HOME:-}" ] && [ -n "${JAVA_BIN:-}" ]; then
    GRAALVM_HOME="$(cd "$(dirname "$JAVA_BIN")/.." && pwd)"
    export GRAALVM_HOME
fi

# Tmp discipline (corpus/wave1/README.md): never write to /tmp (512M shared
# tmpfs on this VM). The repo's old hidden_files/ scratch convention was
# retired with the phases/ tree — scratch goes to the workspace disk.
# Honor a TMPDIR the caller already set (tools/run_stdlib_gate.sh exports
# its own per-run scratch dir).
export TMPDIR="${TMPDIR:-$HOME/workspace/.tmp-wave1}"
export GOCACHE="${GOCACHE:-$HOME/workspace/.gocache-wave1}"
mkdir -p "$TMPDIR" "$GOCACHE"

case "${1-all}" in
    all)
        dirs=()
        for d in */; do
            case "$d" in _*/) continue ;; esac
            dirs+=("${d%/}")
        done
        ;;
    *) dirs=("$1") ;;
esac
for d in "${dirs[@]}"; do
    [ -d "$d" ] || { echo "unknown package dir: $d"; exit 2; }
done

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

pass=0; fail=0; scopelimited=0
# filter_stderr: drop the known JVM Unsafe warning lines.
filter_stderr() { grep -v '^WARNING: ' || true; }

# build_gc <go file> <gcbin>: stock-gc reference leg. Programs marked
# <base>.synthetic build against the extracted synthetic sources; all
# others build against real std.
build_gc() {
    local go="$1" gcbin="$2" base="${1%.go}"
    if [ -f "$base.synthetic" ]; then
        local refdir="$TMP/synthref-$(basename "$base")"
        local gopath
        gopath="$(python3 "$(dirname "$0")/extract_synth_ref.py" \
            "$GR/internal/frontend" "$go" "$refdir")" \
            || { echo "FAIL $base: synthetic-ref extraction failed"; return 1; }
        GO111MODULE=off GOPATH="$gopath" go build -o "$gcbin" \
            "$gopath/src/m7prog/main.go" \
            || { echo "FAIL $base: gc build failed (synthetic ref)"; return 1; }
    else
        go build -o "$gcbin" "$go" \
            || { echo "FAIL $base: gc build failed"; return 1; }
    fi
}

run_scope_limited() { # $1 = go file
    local go="$1" base="${1%.go}"
    local reason retest nogc
    reason="$(sed -n 's/^reason: //p' "$base.scopelimited" | head -1)"
    retest="$(sed -n 's/^retest: //p' "$base.scopelimited" | head -1)"
    nogc="$(sed -n 's/^nogc: //p' "$base.scopelimited" | head -1)"
    if [ -z "${retest:-}" ]; then
        echo "FAIL $base: .scopelimited marker lacks a retest: trigger"
        fail=$((fail+1)); return
    fi
    if [ -z "${nogc:-}" ]; then
        # The program must still be valid runnable Go; otherwise it is a
        # corpus bug, not a scope limitation.
        local gcbin="$TMP/$base.gc"
        build_gc "$go" "$gcbin" || { fail=$((fail+1)); return; }
        "$gcbin" > /dev/null 2>&1
        local gc_exit=$?
        if [ "$gc_exit" != 0 ]; then
            echo "FAIL $base: scope-limited program exits $gc_exit under gc (corpus bug)"
            fail=$((fail+1)); return
        fi
    fi
    scopelimited=$((scopelimited+1))
    echo "SCOPE-LIMITED $base: ${reason:-<no reason given>} [retest: $retest]"
}

run_one() { # $1 = go file, $2 = gmp
    local go="$1" gmp="$2" base="${1%.go}"
    local gcbin="$TMP/$base.gc"

    build_gc "$go" "$gcbin" || { fail=$((fail+1)); return; }
    "$gcbin" > "$TMP/gc.out" 2> "$TMP/gc.err"; local gc_exit=$?
    "${GRAALGO_RUN[@]}" --gmp="$gmp" "$go" > "$TMP/gr.out" 2> "$TMP/gr.err"; local gr_exit=$?

    local ok=1
    cmp -s "$TMP/gc.out" "$TMP/gr.out" || { ok=0; echo "FAIL $base (gmp=$gmp): stdout differs"; }
    if [ "$gc_exit" != "$gr_exit" ]; then
        ok=0; echo "FAIL $base (gmp=$gmp): exit gc=$gc_exit graalgo=$gr_exit"
    fi
    # Stderr is compared against the LIVE gc leg — never a hand-written
    # anchor alone. Panic programs (non-zero gc exit): graalgo's first
    # filtered stderr line must equal gc's live first line, so a toolchain
    # message change moves both legs together instead of silently pinning
    # stale anchor text. All other programs: the full filtered stderr must
    # match, so stderr divergences in "passing" programs stay visible.
    if [ "$gc_exit" != 0 ]; then
        local gc_first gr_first
        gc_first="$(filter_stderr < "$TMP/gc.err" | head -1)"
        gr_first="$(filter_stderr < "$TMP/gr.err" | head -1)"
        if [ "$gr_first" != "$gc_first" ]; then
            ok=0; echo "FAIL $base (gmp=$gmp): stderr first line differs from live gc: got [$gr_first] gc [$gc_first]"
        fi
        # A .panic anchor additionally pins gc's own message: a toolchain
        # message change fails loudly here instead of leaving stale text.
        if [ -f "$base.panic" ]; then
            local want
            want="$(cat "$base.panic")"
            if [ "$gc_first" != "$want" ]; then
                ok=0; echo "FAIL $base (gmp=$gmp): gc stderr first line drifted from .panic anchor: gc [$gc_first] anchor [$want] (toolchain message changed?)"
            fi
        fi
    elif [ -f "$base.panic" ]; then
        ok=0; echo "FAIL $base (gmp=$gmp): .panic anchor present but gc exits 0 (stale anchor?)"
    elif ! cmp -s <(filter_stderr < "$TMP/gc.err") <(filter_stderr < "$TMP/gr.err"); then
        ok=0; echo "FAIL $base (gmp=$gmp): stderr differs from live gc"
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

for dir in "${dirs[@]}"; do
    for go in "$dir"/[0-9][0-9]_*.go; do
        [ -e "$go" ] || continue
        base="${go%.go}"
        # Scope-limited programs: gc leg only, counted separately.
        if [ -f "$base.scopelimited" ]; then
            run_scope_limited "$go"
            continue
        fi
        # Timing-sensitive programs (marked <base>.timing) run with gmp=1
        # only (single-threaded is enough; the multi-gmp leg adds
        # scheduling noise for sleeps).
        if [ -f "${go%.go}.timing" ]; then
            run_one "$go" 1
        else
            run_one "$go" 1; run_one "$go" 4
        fi
    done
done

echo "== differential: $pass passed, $scopelimited scope-limited, $fail failed =="
[ "$fail" = 0 ]
