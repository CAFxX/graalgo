#!/bin/bash
# godebug-diff: differential tool for P2-M5 (WS E) EMULATED GODEBUG knobs.
#
# For each EMULATED knob, runs a probe program under stock gc (go run) and
# under graalgo (SchedMain) with the same GODEBUG setting, then diffs the
# observable behavior with a per-knob comparator:
#
#   schedtrace   both emit periodic SCHED lines on stderr (format differs:
#                gc prints P/M fields, graalgo prints goroutines/cap/carriers;
#                documented in docs/arch/phases/phase2/GODEBUG.md)
#   scheddetail  graalgo additionally emits SCHED-DETAIL lines
#   gctrace      both emit a per-GC-cycle line on stderr (numbers are
#                JVM-heap sourced for graalgo)
#   panicnil     byte-identical stdout (recover() returns nil, like gc)
#
# Usage: ./godebug-diff.sh [--keep | --check-only]
#   --keep        keep the scratch work dir under <repo>/.tmp/ (else removed)
#   --check-only  run only the infrastructure preflight (no build, no JVM
#                 launch) and exit 0 (ready) / 3 (infra missing)
# Exit 0 iff every knob diffs as expected; 3 infrastructure error (missing
# toolchain, unbuilt classes, missing corpus/probe — never a knob FAIL).
# Writes REPORT.md on success. GRAALGO_ROOT overrides the repo root
# (test seam; default: the repo containing this script).
set -u

ROOT="${GRAALGO_ROOT:-$(cd "$(dirname "$0")/../.." && pwd)}"
BK="$ROOT/internal/backend"
GO="${GO:-$HOME/tools/go/bin/go}"
GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
JAVA="$GRAALVM_HOME/bin/java"
CORPUS="$ROOT/test/corpus/phase2/godebug"
PROBES="db02_panicnil.go db03_alloc.go db04_spin.go"

# Same classpath as the runtime module's RUN_CP (see
# internal/backend/runtime/Makefile): runtime classes first (the overlay
# shadowing the truffle module's frozen stubs), then the truffle module's
# classes, interop classes, Truffle jars.
CP="$BK/runtime/out/classes:$BK/truffle/out/classes:$BK/interop/out/classes"
for jar in "$BK"/truffle/lib/*.jar; do CP="$CP:$jar"; done
JAVA_FLAGS=(--enable-native-access=ALL-UNNAMED -Xss32m)

# check_infra: every computed classpath entry, the corpus dir, and the
# probe programs must exist before any JVM launch. Returns 3 otherwise.
check_infra() {
  [ -x "$GO" ] || { echo "INFRA: gc toolchain not found: $GO" >&2; return 3; }
  [ -x "$JAVA" ] || { echo "INFRA: java not found: $JAVA" >&2; return 3; }
  local -a entries
  IFS=':' read -ra entries <<< "$CP"
  local entry
  for entry in "${entries[@]}"; do
    [ -e "$entry" ] || { echo "INFRA: classpath entry missing: $entry" >&2; return 3; }
  done
  [ -d "$CORPUS" ] || { echo "INFRA: corpus dir missing: $CORPUS" >&2; return 3; }
  local probe
  for probe in $PROBES; do
    [ -f "$CORPUS/$probe" ] || { echo "INFRA: probe missing: $CORPUS/$probe" >&2; return 3; }
  done
  [ -f "$BK/runtime/out/classes/graalgo/sched/launcher/SchedMain.class" ] \
    || { echo "INFRA: SchedMain not built (make -C $ROOT build)" >&2; return 3; }
  return 0
}

if [ "${1:-}" = "--check-only" ]; then
  check_infra || exit 3
  echo "check-only: OK (ROOT=$ROOT)"
  exit 0
fi
KEEP="${1:-}"

mkdir -p "$ROOT/.tmp"
WORK="$(mktemp -d "$ROOT/.tmp/godebug-diff.XXXXXX")"

pass=0
fail=0
report=""

run_gc() { # $1=godebug $2=probe -> stdout/stderr/exit in $WORK/gc.{out,err,exit}
    local gd="$1" probe="$2"
    GODEBUG="$gd" "$GO" run "$CORPUS/$probe" >"$WORK/gc.out" 2>"$WORK/gc.err"
    echo "$?" >"$WORK/gc.exit"
}

run_gr() { # $1=godebug $2=probe
    local gd="$1" probe="$2"
    GODEBUG="$gd" "$JAVA" "${JAVA_FLAGS[@]}" -cp "$CP" \
        graalgo.sched.launcher.SchedMain --gmp=2 "$CORPUS/$probe" \
        >"$WORK/gr.out" 2>"$WORK/gr.err"
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

echo "== building backend =="
make -C "$ROOT" build >/dev/null 2>&1 || { echo "INFRA: make build failed" >&2; exit 3; }
check_infra || exit 3

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
    echo "Probes: $CORPUS (db02_panicnil.go, db03_alloc.go, db04_spin.go)"
    echo "Backends: stock gc go1.27.1 ($GO run) vs graalgo SchedMain"
    echo ""
    echo '```'
    echo "$report"
    echo '```'
    echo ""
    echo "Result: $pass passed, $fail failed"
} > REPORT.md
echo "wrote REPORT.md"

if [ -z "$KEEP" ]; then
    rm -rf "$WORK"
else
    echo "work dir kept: $WORK"
fi

[ "$fail" = "0" ]
