#!/bin/bash
# Differential runner for the P3-M9 wave-3 corpus (see README.md): each
# program runs under stock gc 1.27.1 (built binary, run directly — `go run`
# masks the exit code) and under graalgo-p3sync-run. Compares stdout bytes,
# exit codes, and — for panic programs — the first stderr line.
#
# Known JVM Unsafe warnings on stderr are filtered (harness noise, cf.
# p3frontend/DESIGN.md §7). Timing-sensitive programs (marked with an
# empty <base>.timing file) print fixed strings only (never measured
# durations) and run with gmp=1 only; all other programs run with gmp=1
# and gmp=4 (the timectx convention).
#
# Scope-limited programs (marked with a <base>.scopelimited file naming
# the reason and a retest: trigger) can never pass the frontend for honest
# scope reasons. They are NOT run under graalgo and are counted separately
# (neither passing nor failing); the gc leg still builds and runs them so
# a program that is not even valid Go surfaces as FAIL, not scope-limited.
# A .scopelimited file without a retest: trigger is a harness FAIL.
#
# Normalization (wave-3 delta): a package dir may ship a .normalize.sed
# file (sed -E script) applied to BOTH legs' stdout before cmp. This
# exists because testing/ programs print values the corpus must not pin
# byte-for-byte: gc's "file:line: " log prefixes (absent under graalgo —
# no runtime.Caller), test durations, the benchmark -GOMAXPROCS name
# suffix (the frozen runtime has no GOMAXPROCS), and ns/op values. Each
# rule cites the scope-limit it encodes; the script lives next to the
# programs it applies to (testing/.normalize.sed).
#
# Programs with a <base>.args file get its whitespace-separated contents
# as argv (after the program). Both legs receive the same args; the
# graalgo leg passes them after `--` per the launcher argv convention.
#
# Usage: run_wave3.sh [pkg|all] — run one package subdir, or every wave3
# package dir when omitted (nested dirs like testing/quick are included).
# Underscore-prefixed dirs (_smoke, infra) run only when named explicitly.
#
# The repo root defaults to $HOME/workspace/graalgo but can be overridden:
#   GR=~/workspace/graalgo-m9 run_wave3.sh strings
set -uo pipefail
cd "$(dirname "$0")"

GR="${GR:-$HOME/workspace/graalgo-m9}"
P3="$GR/phases/phase3"
P2="$GR/phases/phase2"
P1="$GR/phases/phase1"
export PATH="${GO_BIN_DIR:-$HOME/tools/go/bin}:$PATH"
JAVA="${JAVA_BIN:-$HOME/tools/graalvm-jdk-25/bin/java}"

# Tmp discipline (corpus/wave3/README.md): never write to /tmp.
export TMPDIR="$P3/hidden_files/m9-tmp/wave3"
export GOCACHE="$HOME/workspace/.gocache-m9-wave3"
mkdir -p "$TMPDIR"

case "${1-all}" in
    all)
        dirs=()
        for d in */; do
            case "$d" in _*/) continue ;; esac
            dirs+=("${d%/}")
        done
        for d in */*/; do
            case "$d" in _*/*) continue ;; esac
            # Only nested dirs that actually hold corpus programs.
            # Enumerate *.go, not just numbered files: the old numbered
            # glob silently dropped crypto/ecdh, crypto/elliptic and
            # crypto/hkdf, which hold only main.go (R1-33).
            if compgen -G "${d%/}/*.go" > /dev/null; then
                dirs+=("${d%/}")
            fi
        done
        ;;
    *) dirs=("$1") ;;
esac
for d in "${dirs[@]}"; do
    [ -d "$d" ] || { echo "unknown package dir: $d"; exit 2; }
done

echo "== building phase-3 java module =="
make -C "$P3" main > /dev/null || { echo "phase3 build failed"; exit 1; }

CP="$P3/out/classes:$P2/out/classes:$P1/out/classes:$(ls "$P1"/lib/*.jar | tr '\n' ':')"
# P3-M7R2: -Djava.io.tmpdir keeps the JVM off /tmp (a full /tmp silently
# yields empty output with exit 0 — methodology review finding).
GRAALGO_RUN=( "$JAVA" --enable-native-access=ALL-UNNAMED -Xss32m \
    -Djava.io.tmpdir="$TMPDIR" -cp "$CP" \
    graalgo.p3sync.launcher.P3SyncMain )

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0; fail=0; scopelimited=0
# filter_stderr: drop the known JVM Unsafe warning lines.
filter_stderr() { grep -v '^WARNING: ' || true; }

# build_gc <go file> <gcbin>: stock-gc reference leg. Programs marked
# <base>.synthetic build against the extracted phase-1 synthetic sources;
# all others build against real std.
build_gc() {
    local go="$1" gcbin="$2" base="${1%.go}"
    if [ -f "$base.synthetic" ]; then
        local refdir="$TMP/synthref-$(basename "$base")"
        local gopath
        gopath="$(python3 "$(dirname "$0")/extract_synth_ref.py" \
            "$P1/frontend" "$go" "$refdir")" \
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
    # Programs with a <base>.args file get its whitespace-separated
    # contents as argv (after the program). Both legs receive the same
    # args; the graalgo leg passes them after `--` per the launcher
    # argv convention (R6).
    local args=()
    if [ -f "$base.args" ]; then
        # shellcheck disable=SC2207
        args=($(cat "$base.args"))
    fi
    "$gcbin" "${args[@]}" > "$TMP/gc.out" 2> "$TMP/gc.err"; local gc_exit=$?
    "${GRAALGO_RUN[@]}" --gmp="$gmp" -- "$go" "${args[@]}" > "$TMP/gr.out" 2> "$TMP/gr.err"; local gr_exit=$?

    # Wave-3 delta: per-dir .normalize.sed applied to BOTH legs before cmp.
    local dirn
    dirn="$(dirname "$go")"
    if [ -f "$dirn/.normalize.sed" ]; then
        sed -E -f "$dirn/.normalize.sed" "$TMP/gc.out" > "$TMP/gc.norm" && mv "$TMP/gc.norm" "$TMP/gc.out"
        sed -E -f "$dirn/.normalize.sed" "$TMP/gr.out" > "$TMP/gr.norm" && mv "$TMP/gr.norm" "$TMP/gr.out"
    fi

    local ok=1
    cmp -s "$TMP/gc.out" "$TMP/gr.out" || { ok=0; echo "FAIL $base (gmp=$gmp): stdout differs"; }
    if [ "$gc_exit" != "$gr_exit" ]; then
        ok=0; echo "FAIL $base (gmp=$gmp): exit gc=$gc_exit graalgo=$gr_exit"
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

# Loopback-only policy gate (03-corpus-design.md §1.6): no program may
# address a non-loopback host. Static grep over Dial/Listen call targets.
# net.Pipe programs and 127.0.0.1/::1/localhost are allowed.
echo "== loopback egress gate =="
if grep -rn --include='*.go' -E '\.(Dial|DialTimeout|Listen)\(' . | grep -v -E '127\.0\.0\.1|::1|localhost|net\.Pipe' ; then
    echo "FAIL: non-loopback network address in corpus program"
    exit 1
fi

for dir in "${dirs[@]}"; do
    # Enumerate every program in the dir, not just numbered ones (R1-33:
    # the old glob skipped unnumbered main.go programs). A dir that
    # contributes zero programs fails loudly — unless it is a container
    # dir whose programs live in subdirs (e.g. archive/, crypto/),
    # which are enumerated as their own dirs entries above.
    dir_programs=0
    for go in "$dir"/*.go; do
        [ -e "$go" ] || break
        dir_programs=$((dir_programs+1))
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
    if [ "$dir_programs" = 0 ]; then
        if compgen -G "$dir"/*/*.go > /dev/null; then
            echo "== $dir: container dir; programs live in subdirs =="
        else
            echo "FAIL $dir: no corpus programs (*.go) — dir contributes nothing to the gate"
            fail=$((fail+1))
        fi
    fi
done

echo "== differential: $pass passed, $scopelimited scope-limited, $fail failed =="
[ "$fail" = 0 ]
