#!/bin/bash
# godebug-diff: differential tool for P2-M5 (WS E) EMULATED GODEBUG knobs.
#
# For each EMULATED knob, runs a probe program under stock gc (go run) and
# under graalgo (the new path since m3-plan D3: the probe built through
# graalgo-tool with the standard-library fork, test/lib/toolexec.sh, and
# run on graalgo-bundle-run) with the same GODEBUG setting, then diffs the
# observable behavior with a per-knob comparator:
#
#   schedtrace   both emit periodic SCHED lines on stderr (format differs:
#                gc prints P/M fields, graalgo prints goroutines/cap/carriers;
#                documented in the truffle module's GoDebug sources)
#   scheddetail  graalgo additionally emits SCHED-DETAIL lines
#   gctrace      both emit a per-GC-cycle line on stderr (numbers are
#                JVM-heap sourced for graalgo)
#   panicnil     byte-identical stdout (recover() returns nil, like gc)
#
# Usage: ./godebug-diff.sh [--keep]   (run from this directory)
# Exit 0 iff every knob diffs as expected. Writes REPORT.md on success.
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
# shellcheck source=../../test/lib/infra.sh
. "$ROOT/test/lib/infra.sh"
# shellcheck source=../../test/lib/toolexec.sh
. "$ROOT/test/lib/toolexec.sh"
CORPUS="$ROOT/test/corpus/phase2/godebug"
RUNNER="$ROOT/internal/backend/graalgo-bundle-run"
KEEP="${1:-}"
infra_setup_go
GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
export GRAALVM_HOME
infra_mktemp godebug-diff
WORK="$TMP"

pass=0
fail=0
report=""

run_gc() { # $1=godebug $2=probe -> stdout/stderr/exit in $WORK/gc.{out,err,exit}
    local gd="$1" probe="$2"
    GODEBUG="$gd" "$GO" run "$CORPUS/$probe" >"$WORK/gc.out" 2>"$WORK/gc.err"
    echo "$?" >"$WORK/gc.exit"
}

run_gr() { # $1=godebug $2=probe (built once through graalgo-tool)
    local gd="$1" probe="$2" bin="$WORK/${2%.go}.gg"
    if [ ! -x "$bin" ]; then
        ( cd "$CORPUS" && toolexec_build_files "$bin" "$RUNNER" "$probe" ) >"$WORK/gr.build" 2>&1 \
            || infra_die "graalgo-tool build of $probe failed: $(head -3 "$WORK/gr.build")"
    fi
    GODEBUG="$gd" GOMAXPROCS=2 TMPDIR="$WORK" "$bin" >"$WORK/gr.out" 2>"$WORK/gr.err"
    echo "$?" >"$WORK/gr.exit"
    if grep -qE 'ClassNotFoundException|NoClassDefFoundError|Could not find or load main class' "$WORK/gr.err"; then
        echo "INFRA: graalgo launcher could not load its classes:" >&2
        head -3 "$WORK/gr.err" >&2
        exit 3
    fi
}

# Strip JVM/Truffle warnings from graalgo stderr (not program behavior).
clean_gr_err() {
    grep -v "^WARNING:" "$WORK/gr.err" | grep -v "^OpenJDK 64-Bit" || true
}

check() { # $1=knob $2=ok(0/1) $3=detail
    if [ "$2" = "0" ]; then
        pass=$((pass+1)); report="${report}PASS $1: $3
"
        echo "PASS $1"
    else
        fail=$((fail+1)); report="${report}FAIL $1: $3
"
        echo "FAIL $1: $3"
    fi
}

echo "== building the backend and graalgo-tool =="
make -C "$ROOT" build >/dev/null 2>&1 || infra_die "make build failed"
[ -f "$ROOT/internal/backend/truffle/out/classes/graalgo/launcher/BundleMain.class" ] \
    || infra_die "backend not built (BundleMain missing)"
toolexec_setup

# --- schedtrace ---
# Strengthened comparator: requires >=3 lines with STRICTLY INCREASING
# timestamps — a print-once-and-die daemon would fail this.
echo "== schedtrace =="
run_gc  "schedtrace=100" "db04_spin.go"
run_gr  "schedtrace=100" "db04_spin.go"
gc_lines=$(grep -c "^SCHED" "$WORK/gc.err" || true)
gr_lines=$(clean_gr_err | grep -c "^SCHED [0-9]*ms: goroutines=" || true)
gc_exit=$(cat "$WORK/gc.exit"); gr_exit=$(cat "$WORK/gr.exit")
ok=0; detail=""
[ "$gc_exit" = "0" ] || { ok=1; detail="gc exit $gc_exit"; }
[ "$gr_exit" = "0" ] || { ok=1; detail="graalgo exit $gr_exit"; }
[ "$gc_lines" -ge 1 ] || { ok=1; detail="gc emitted no SCHED lines"; }
[ "$gr_lines" -ge 3 ] || { ok=1; detail="graalgo emitted <3 SCHED lines ($gr_lines)"; }
# Timestamps must strictly increase (periodic daemon, not one-shot).
if [ "$ok" = "0" ]; then
    prev=-1; mono=1
    while read -r line; do
        ms=$(echo "$line" | sed -E 's/^SCHED ([0-9]+)ms:.*/\1/')
        [ "$ms" -gt "$prev" ] || { mono=0; break; }
        prev=$ms
    done < <(clean_gr_err | grep "^SCHED [0-9]*ms: goroutines=")
    [ "$mono" = "1" ] || { ok=1; detail="SCHED timestamps not strictly increasing"; }
fi
diff -q "$WORK/gc.out" "$WORK/gr.out" >/dev/null || { ok=1; detail="stdout differs"; }
[ -z "$detail" ] && detail="gc SCHED lines=$gc_lines, graalgo SCHED lines=$gr_lines (increasing), stdout identical"
check "schedtrace" "$ok" "$detail"

# --- scheddetail ---
# Positive control (>=1 SCHED-DETAIL line with the knob) and NEGATIVE
# control (no SCHED-DETAIL lines without it).
echo "== scheddetail =="
run_gr "schedtrace=100,scheddetail=1" "db04_spin.go"
gr_detail=$(clean_gr_err | grep -c "^SCHED-DETAIL" || true)
gr_exit=$(cat "$WORK/gr.exit")
ok=0; detail=""
[ "$gr_exit" = "0" ] || { ok=1; detail="graalgo exit $gr_exit"; }
[ "$gr_detail" -ge 1 ] || { ok=1; detail="no SCHED-DETAIL lines"; }
run_gr "schedtrace=100" "db04_spin.go"
gr_nodetail=$(clean_gr_err | grep -c "^SCHED-DETAIL" || true)
[ "$gr_nodetail" = "0" ] || { ok=1; detail="SCHED-DETAIL lines without scheddetail=1 ($gr_nodetail)"; }
[ -z "$detail" ] && detail="SCHED-DETAIL lines=$gr_detail, negative control clean"
check "scheddetail" "$ok" "$detail"

# --- gctrace ---
# Per-cycle-ness: requires >=2 cycle lines with non-decreasing timestamps.
echo "== gctrace =="
run_gc "gctrace=1" "db03_alloc.go"
run_gr "gctrace=1" "db03_alloc.go"
gc_lines=$(grep -cE "^gc [0-9]+ @" "$WORK/gc.err" || true)
gr_lines=$(clean_gr_err | grep -cE "^gc [0-9]+ @" || true)
gc_exit=$(cat "$WORK/gc.exit"); gr_exit=$(cat "$WORK/gr.exit")
ok=0; detail=""
[ "$gc_exit" = "0" ] || { ok=1; detail="gc exit $gc_exit"; }
[ "$gr_exit" = "0" ] || { ok=1; detail="graalgo exit $gr_exit"; }
[ "$gc_lines" -ge 1 ] || { ok=1; detail="gc emitted no gc lines"; }
[ "$gr_lines" -ge 2 ] || { ok=1; detail="graalgo emitted <2 gc cycle lines ($gr_lines)"; }
diff -q "$WORK/gc.out" "$WORK/gr.out" >/dev/null || { ok=1; detail="stdout differs"; }
[ -z "$detail" ] && detail="gc cycle lines=$gc_lines, graalgo cycle lines=$gr_lines, stdout identical"
check "gctrace" "$ok" "$detail"

# --- panicnil ---
echo "== panicnil =="
run_gc "panicnil=1" "db02_panicnil.go"
run_gr "panicnil=1" "db02_panicnil.go"
gc_exit=$(cat "$WORK/gc.exit"); gr_exit=$(cat "$WORK/gr.exit")
ok=0; detail=""
[ "$gc_exit" = "$gr_exit" ] || { ok=1; detail="exit codes differ: gc=$gc_exit graalgo=$gr_exit"; }
if diff -q "$WORK/gc.out" "$WORK/gr.out" >/dev/null; then
    [ -z "$detail" ] && detail="stdout byte-identical: $(head -1 "$WORK/gr.out")"
else
    ok=1; detail="stdout differs: gc=[$(cat "$WORK/gc.out" | tr '\n' '|')] graalgo=[$(cat "$WORK/gr.out" | tr '\n' '|')]"
fi
check "panicnil" "$ok" "$detail"

echo ""
echo "== $pass passed, $fail failed =="

{
    echo "# godebug-diff report"
    echo ""
    echo "Generated: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "Probes: test/corpus/phase2/godebug (db02_panicnil.go, db03_alloc.go, db04_spin.go)"
    echo "Backends: stock gc go1.27.1 (go run) vs graalgo (graalgo-tool, graalgo-bundle-run)"
    echo ""
    echo '```'
    echo "$report"
    echo '```'
    echo ""
    echo "Result: $pass passed, $fail failed"
} > "$HERE/REPORT.md"
echo "wrote REPORT.md"

[ -n "$KEEP" ] && { trap - EXIT; echo "work dir kept: $WORK"; }

[ "$fail" = "0" ]
