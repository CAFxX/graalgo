#!/usr/bin/env bash
# test/imgcheck/run.sh — the descriptor-image gate (`make test-imgcheck`;
# toolexec W9a, docs/arch/toolexec-design.md §3.4/§5.6, abi-spike §1.4):
# the type descriptors the node package builds
# (internal/backend/truffle/src/main/java/graalgo/go/types/Descriptors.java)
# against the ones gc's compiler emits, for a corpus of types.
#
# One Go program (main.go, corpus.go) walks every descriptor reachable from
# the corpus's root types the way internal/abi and internal/reflectlite do —
# unsafe conversions of an interface's type word, runtime-resolved name and
# type offsets — and prints every fact it reads. It runs twice:
#
#   gc leg       built by gc (-ldflags=-checklinkname=0: it pulls
#                internal/reflectlite's two resolvers), with oracle_gc.go,
#                which checks every fact the walker read against gc's own
#                package reflect (0 mismatches required);
#   graalgo leg  built by the stock `go build` through graalgo-tool with the
#                fork applied, run on the node package.
#
# The two outputs must be identical: the 24 scalar bytes of abi.Type
# (Size_, PtrBytes, Hash, TFlag, Align_, FieldAlign_, Kind_) byte for byte,
# everything else fact for fact. Not compared, because gc's values are
# code addresses or depend on what its linker kept: Method.Ifn/Tfn,
# MapType.Hasher, the type of an unexported method, PtrToThis of a type
# that is not defined (main.go says where).
#
# Then the mutants: the graalgo leg again with one deliberate builder error
# per run (-Dgraalgo.descriptors.mutate=kind|size|offset|nameoff|methods);
# every one must make the comparison fail, or the gate cannot.
#
# corpus.go is generated (gen.py); a stale file fails the gate.
#
# Usage: test/imgcheck/run.sh [-v] [--no-mutants]
#   -v            print the first differing lines
#   --no-mutants  skip the mutant runs
# Env:   GO, GRAALVM_HOME, GRAALGO_TMP (see test/lib/infra.sh),
#        GRAALGO_JAVA_OPTS (JVM flags of the graalgo leg),
#        IMGCHECK_TIMEOUT (seconds per run, default 300)
# Exit:  the infra.sh contract (0 pass, 1 divergence, 2 usage, 3 infra).
set -uo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/infra.sh
. "$HERE/../lib/infra.sh"
# shellcheck source=../lib/toolexec.sh
. "$HERE/../lib/toolexec.sh"
ROOT="$GRAALGO_ROOT"

verbose=0
mutants=1
for arg in "$@"; do
    case "$arg" in
        -v) verbose=1 ;;
        --no-mutants) mutants=0 ;;
        *) echo "usage: $0 [-v] [--no-mutants]" >&2; exit "$USAGE_EXIT" ;;
    esac
done

infra_setup_go
GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
export GRAALVM_HOME
RUNNER="$ROOT/internal/backend/graalgo-bundle-run"
[ -x "$RUNNER" ] || infra_die "runner not found: $RUNNER"
[ -f "$ROOT/internal/backend/truffle/out/classes/graalgo/launcher/BundleMain.class" ] \
    || infra_die "backend not built (BundleMain missing) — run \`make build\` at the repo root"
command -v python3 >/dev/null || infra_die "python3 not found"
infra_mktemp imgcheck
TIMEOUT="${IMGCHECK_TIMEOUT:-300}"
MIN_TYPES=300
toolexec_setup
export GODEBUG=cpu.all=off

fail() { echo "imgcheck: FAIL: $*"; exit "$DIVERGENCE_EXIT"; }

# The corpus is generated: the checked-in file must be the generator's output.
python3 "$HERE/gen.py" > "$TMP/corpus.raw" || infra_die "gen.py failed"
"$(dirname "$GO")/gofmt" < "$TMP/corpus.raw" > "$TMP/corpus.go" || infra_die "gofmt failed on gen.py's output"
cmp -s "$TMP/corpus.go" "$HERE/corpus.go" \
    || fail "test/imgcheck/corpus.go is stale: python3 test/imgcheck/gen.py | gofmt > test/imgcheck/corpus.go"

mkdir -p "$TMP/mod"
cp "$HERE"/*.go "$TMP/mod/"
printf 'module imgcheck\n\ngo 1.27\n' > "$TMP/mod/go.mod"

# gc leg: the compiler's descriptors, and the walker checked against reflect.
( cd "$TMP/mod" && "$GO" build -tags imgcheck_oracle -ldflags=-checklinkname=0 -o "$TMP/gc.bin" . ) >"$TMP/gc.build" 2>&1 \
    || { cat "$TMP/gc.build" >&2; infra_die "gc cannot build the imgcheck program"; }
( cd "$TMP" && timeout "$TIMEOUT" "$TMP/gc.bin" >"$TMP/gc.out" 2>"$TMP/gc.err" </dev/null ) \
    || fail "the gc leg exited with status $?"
grep -q '^oracle: [0-9]* descriptors checked against reflect, 0 mismatches$' "$TMP/gc.out" \
    || { grep -a 'ORACLE MISMATCH' "$TMP/gc.err" | head -10; fail "the walker disagrees with gc's reflect: $(cat "$TMP/gc.out")"; }
summary="$(tail -1 "$TMP/gc.err")"
types="$(printf '%s\n' "$summary" | sed -n 's/^imgcheck: [0-9]* roots, \([0-9]*\) types, [0-9]* facts$/\1/p')"
[ -n "$types" ] || fail "the gc leg printed no summary line"
[ "$types" -ge "$MIN_TYPES" ] || fail "the corpus has $types types, the gate needs at least $MIN_TYPES"

# graalgo leg: the same program through graalgo-tool on the node package.
( cd "$TMP/mod" && toolexec_build "$TMP/gg.bin" "$RUNNER" ) >"$TMP/gg.build" 2>&1 \
    || { grep -v '^#' "$TMP/gg.build" | head -10 >&2; fail "graalgo-tool build failed"; }

# gg_leg <tag> [java option]: run the graalgo leg; its stderr without the
# JVM's own notices is $TMP/<tag>.err. Returns the exit status.
gg_leg() {
    local status=0
    ( cd "$TMP" && TMPDIR="$TMP" GRAALGO_JAVA_OPTS="${GRAALGO_JAVA_OPTS:-} ${2:-}" \
        timeout "$TIMEOUT" "$TMP/gg.bin" >"$TMP/$1.out" 2>"$TMP/$1.raw" </dev/null ) || status=$?
    grep -av '^WARNING: \|^NOTE: Picked up' "$TMP/$1.raw" > "$TMP/$1.err"
    return "$status"
}

gg_status=0
gg_leg gg || gg_status=$?
infra_check_run imgcheck "$gg_status" "$TMP/gg.raw"
if [ "$gg_status" -ne 0 ] || ! cmp -s "$TMP/gc.err" "$TMP/gg.err"; then
    n="$(diff "$TMP/gc.err" "$TMP/gg.err" | grep -c '^[<>]')"
    echo "imgcheck: graalgo's descriptors differ from gc's: exit status $gg_status, $n differing lines"
    [ "$verbose" -eq 1 ] && diff "$TMP/gc.err" "$TMP/gg.err" | head -40 | cut -c1-240 | sed 's/^/    /'
    exit "$DIVERGENCE_EXIT"
fi
echo "$summary — graalgo's images identical to gc's descriptors; $(cat "$TMP/gc.out")"

# Mutants: each deliberate builder error must be reported.
if [ "$mutants" -eq 1 ]; then
    killed=0
    for m in kind size offset nameoff methods; do
        st=0
        gg_leg "mut-$m" "-Dgraalgo.descriptors.mutate=$m" || st=$?
        if [ "$st" -eq 0 ] && cmp -s "$TMP/gc.err" "$TMP/mut-$m.err"; then
            fail "mutant '$m' was not detected: the gate cannot fail on that class of error"
        fi
        n="$(diff "$TMP/gc.err" "$TMP/mut-$m.err" | grep -c '^[<>]')"
        echo "imgcheck: mutant $m detected (exit status $st, $n differing lines)"
        killed=$((killed + 1))
    done
    echo "imgcheck: $killed of 5 mutants detected"
fi
exit 0
