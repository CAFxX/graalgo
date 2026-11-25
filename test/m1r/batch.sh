#!/usr/bin/env bash
# test/m1r/batch.sh — the planted failures of the gates' batch runner
# (test/lib/batchrun.sh, graalgo.launcher.BundleBatchMain), run by `make
# test-goroutine-modes`: one batch of the programs of batch/, each hazard
# followed by a plain program, must give every program what its own process
# would — its arguments, stdin, directory, stdout and stderr apart
# (stdio.go); a goroutine leaked past main's return stopped with its
# context, nothing in the next program's output (leak.go); a hang ended by
# its timeout with status 124 and the batch going on (hang.go); a hard exit
# from a goroutine with its status (exit.go); main is goroutine 1 however
# many programs ran before it (goid.go); a JVM killed under a program
# (crash.go: SIGKILL, as a crash or the OOM killer) reruns it and every
# later program in their own processes — and a program whose output differs from gc's still
# fails m1r/run.sh in batch mode (an os.Executable() print).
#
# Usage: test/m1r/batch.sh
# Env:   as test/m1r/run.sh. Exit: the infra.sh contract.
set -uo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/infra.sh
. "$HERE/../lib/infra.sh"
# shellcheck source=../lib/toolexec.sh
. "$HERE/../lib/toolexec.sh"
# shellcheck source=../lib/batchrun.sh
. "$HERE/../lib/batchrun.sh"
ROOT="$GRAALGO_ROOT"
RUNNER="$ROOT/internal/backend/graalgo-bundle-run"

infra_setup_go
GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
export GRAALVM_HOME
[ -f "$ROOT/internal/backend/truffle/out/classes/graalgo/launcher/BundleBatchMain.class" ] \
    || infra_die "backend not built (BundleBatchMain missing) — run \`make build\` at the repo root"
infra_mktemp m1r-batch
toolexec_setup

for name in stdio leak hang exit goid crash hello; do
    mkdir -p "$TMP/p/$name"
    cp "$HERE/batch/$name.go" "$TMP/p/$name/main.go"
    printf 'module m1r/batch\n\ngo 1.27\n' > "$TMP/p/$name/go.mod"
    ( cd "$TMP/p/$name" && toolexec_build "$TMP/p/$name/prog" "$RUNNER" ) >"$TMP/build.log" 2>&1 \
        || { cat "$TMP/build.log" >&2; infra_die "graalgo-tool cannot build batch/$name.go"; }
done
printf 'input text\n' > "$TMP/stdin"

# id -> program, args, stdin, timeout
declare -A PROG=() ARGS=() IN=() TO=()
order=(1 2 3 4 5 6 7 8 9 10)
set_case() { PROG[$1]=$2; ARGS[$1]=$3; IN[$1]=$4; TO[$1]=$5; }
set_case 1 stdio "a b" "$TMP/stdin" 60
set_case 2 leak "" /dev/null 60
set_case 3 hello "" /dev/null 60
set_case 4 hang "" /dev/null 2
set_case 5 hello "" /dev/null 60
set_case 6 exit "" /dev/null 60
set_case 7 hello "" /dev/null 60
set_case 8 goid "" /dev/null 60
set_case 9 crash "" /dev/null 60
set_case 10 hello "" /dev/null 60

: > "$TMP/fallback.log"
proc() { # <id>: the per-process leg
    local id="$1" d="$TMP/p/${PROG[$1]}" st=0 args=()
    read -r -a args <<< "${ARGS[$id]}"
    echo "$id" >> "$TMP/fallback.log"
    ( cd "$d" && timeout "${TO[$id]}" ./prog "${args[@]}" >"$TMP/$id.out" 2>"$TMP/$id.err" <"${IN[$id]}" ) || st=$?
    return "$st"
}

batch_init
for id in "${order[@]}"; do
    read -r -a args <<< "${ARGS[$id]}"
    batch_add "$id" "$TMP/p/${PROG[$id]}" ./prog "${IN[$id]}" "$TMP/$id.out" "$TMP/$id.err" "${TO[$id]}" "${args[@]}"
done
# crash.go's killer: SIGKILL the process that runs it (the batch JVM), once.
(
    d="$TMP/p/crash"
    for _ in $(seq 600); do [ -s "$d/pid" ] && break; sleep 0.1; done
    [ -s "$d/pid" ] && touch "$d/killed" && kill -KILL "$(cat "$d/pid")"
) &
killer=$!
batch_run "$RUNNER" proc 2>"$TMP/batch.msg"
wait "$killer"

fail=0
check() { # <what> <condition...>
    local what="$1"
    shift
    if ! "$@"; then
        echo "FAIL $what" >&2
        fail=$((fail + 1))
    fi
}
is() { [ "$(cat "$1")" = "$2" ]; }
want_status() { [ "${BATCH_STATUS[$1]}" = "$2" ] || { echo "    program $1 (${PROG[$1]}): status ${BATCH_STATUS[$1]}, want $2" >&2; false; }; }

for st in 1:0 2:0 3:0 4:124 5:0 6:7 7:0 8:2 9:0 10:0; do
    check "status of program ${st%%:*}" want_status "${st%%:*}" "${st#*:}"
done
check "stdio: args, stdin and directory" is "$TMP/1.out" $'args a,b\nstdin input text\ndir stdio'
check "stdio: stderr apart" is "$TMP/1.err" "to stderr"
check "leak: main's line" grep -q '^main returns$' "$TMP/2.err"
for id in 3 5 7 10; do
    check "hello (program $id): its own output only" is "$TMP/$id.err" "hello"
    check "hello (program $id): empty stdout" is "$TMP/$id.out" ""
done
check "hang: ran until its timeout" grep -q '^spinning$' "$TMP/4.err"
check "exit: nothing after os.Exit" is "$TMP/6.err" "exiting"
check "goid: main is goroutine 1 in a JVM that ran 7 programs" grep -qx 'goroutine 1 \[running\]:' "$TMP/8.err"
check "crash: killed in the batch, rerun in its own process" is "$TMP/9.err" "rerun in its own process"
check "programs 1-8 ran in the batch, 9 and 10 in their own processes" is "$TMP/fallback.log" $'9\n10'
check "the batch reports its fallback" grep -q 'ran the other 2 in their own processes' "$TMP/batch.msg"
check "the batch JVM died on program 9" grep -qx 'START 9' "$BATCH_DIR/results"

# A divergence still fails the gate in batch mode.
cat > "$TMP/wrong.go" <<'EOF'
package main

import (
	"os"
	"path/filepath"
)

func main() {
	exe, _ := os.Executable()
	println("running", filepath.Base(exe)) // gc: the program; graalgo: the JVM
}
EOF
st=0
GRAALGO_BATCH=on "$HERE/run.sh" "$TMP/wrong.go" >"$TMP/wrong.log" 2>&1 || st=$?
check "a divergent program fails m1r/run.sh in batch mode (exit $st)" [ "$st" -eq "$DIVERGENCE_EXIT" ]
check "  ... as a stderr difference" grep -q 'FAIL .*wrong.go: stderr differs' "$TMP/wrong.log"
check "  ... from the batch JVM" grep -q '^batch: 1 programs in one JVM' "$TMP/wrong.log"

if [ "$fail" -ne 0 ]; then
    echo "m1r batch: FAIL ($fail checks)"
    exit "$DIVERGENCE_EXIT"
fi
echo "m1r batch: pass (10 programs, every hazard; a divergence still fails)"
exit 0
