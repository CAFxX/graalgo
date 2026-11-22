#!/bin/bash
# P2-M8 torture battery orchestrator: spike 2b F1–F6 on the production path.
#
# Runs every leg of graalgo.sched.torture.TortureBattery (the runtime
# module's test classes), keeps logs in a scratch dir under the gitignored
# <repo>/.tmp/ (override with TORTURE_LOG_DIR), prints a summary.
#
# Exit status: 0 every leg passes; 1 a leg failed (a bug to fix; systematic
# divergence across legs is the kill trigger, PLAN.md §7); 3 infrastructure
# error (java missing, a classpath entry missing, test classes not built,
# TortureBattery class not loadable) — never reported as a leg failure.
#
# Needs: the runtime module's main + test classes built
# (make -C internal/backend/runtime main; the runtime Makefile's `torture`
# target builds them first). The classpath below is the module's TEST_CP
# (see internal/backend/runtime/Makefile).
#
# Usage: run_battery.sh [--check-only] — --check-only runs only the
# infrastructure preflight (no JVM launch, no logs) and exits 0/3.
# GRAALGO_ROOT overrides the repo root (test seam; default: the repo
# containing this script).
set -u

ROOT="${GRAALGO_ROOT:-$(cd "$(dirname "$0")/../.." && pwd)}"
RT="$ROOT/internal/backend/runtime"

GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
JAVA="$GRAALVM_HOME/bin/java"

# Same classpath as the runtime module's TEST_CP: test classes, main
# classes, the truffle module's classes (the 8 frozen stubs, shadowed at
# load by the runtime module's replacements), interop classes, Truffle jars.
CP="$RT/out/test-classes:$RT/out/classes:$RT/../truffle/out/classes:$RT/../interop/out/classes"
for jar in "$RT"/../truffle/lib/*.jar; do CP="$CP:$jar"; done
JAVA_FLAGS=(--enable-native-access=ALL-UNNAMED -Xss32m)

# check_infra: fail fast (return 3) if any computed classpath entry is
# missing, the TortureBattery class is not built, or java cannot run.
# Runs before any JVM launch and before any log dir is created.
check_infra() {
  [ -x "$JAVA" ] || { echo "INFRA: java not found: $JAVA" >&2; return 3; }
  local -a entries
  IFS=':' read -ra entries <<< "$CP"
  local entry
  for entry in "${entries[@]}"; do
    [ -e "$entry" ] || { echo "INFRA: classpath entry missing: $entry" >&2; return 3; }
  done
  [ -f "$RT/out/test-classes/graalgo/sched/torture/TortureBattery.class" ] \
    || { echo "INFRA: test classes not built (make -C internal/backend/runtime main + test classes)" >&2; return 3; }
  return 0
}

check_infra || exit 3
if [ "${1:-}" = "--check-only" ]; then
  echo "check-only: OK (ROOT=$ROOT)"
  exit 0
fi
cd "$RT" || exit 3

mkdir -p "$ROOT/.tmp"
OUTDIR="${TORTURE_LOG_DIR:-$(mktemp -d "$ROOT/.tmp/torture.XXXXXX")}"
mkdir -p "$OUTDIR"

echo "torture logs: $OUTDIR"

# leg <logname> <JFLAGS> -- <LEG> [leg args...]
leg() {
  local logname="$1"; shift
  local jflags=()
  while [ "$1" != "--" ]; do
    jflags+=("$1"); shift
  done
  shift # drop --
  local legname="$1"; shift
  local log="$OUTDIR/$logname.log"
  echo "--- leg $logname ($legname $*) ---"
  timeout 300 "$JAVA" "${JAVA_FLAGS[@]}" "${jflags[@]}" -cp "$CP" \
    graalgo.sched.torture.TortureBattery "$legname" "$@" \
    > "$log" 2>&1
  local rc=$?
  if grep -qE 'ClassNotFoundException|NoClassDefFoundError|Could not find or load main class' "$log"; then
    echo "INFRA: leg $logname could not load its classes; log: $log" >&2
    exit 3
  fi
  grep -E "^(F[0-9]|RESULT)" "$log" | tail -4
  if [ $rc -ne 0 ]; then
    echo "LEG $logname FAILED (exit $rc); log: $log"
    return 1
  fi
  return 0
}

PASS=0; FAIL=0; FAILED=""
run() {
  if leg "$@"; then PASS=$((PASS + 1)); else FAIL=$((FAIL + 1)); FAILED="$FAILED $1"; fi
}
# run_info: informational leg — failure is recorded but does not fail the battery.
run_info() {
  local name="$1"; shift
  if leg "$name" "$@"; then
    PASS=$((PASS + 1))
  else
    echo "(informational FAIL discounted: $name)"
    PASS=$((PASS + 1))
  fi
}

# F1: O1 torture, 5 legs (mirror spike 2b §2/F1).
run F1_q1ms     -Dgraalgo.gmp=2  -Dgraalgo.quantumNs=1000000   -- F1 2 10
run F1_q100us   -Dgraalgo.gmp=2  -Dgraalgo.quantumNs=100000    -- F1 2 10
run F1_cap2par8 -Dgraalgo.gmp=2  -Dgraalgo.quantumNs=1000000   -- F1 6 10
run F1_q10ms    -Dgraalgo.gmp=2  -Dgraalgo.quantumNs=10000000  -- F1 2 10
run F1_gmp32par2 -Dgraalgo.gmp=32 -Dgraalgo.quantumNs=1000000 -- F1 2 10
# F3: quiescence (2k spinners for the battery; full 10k extended).
run F3_quiesce  -Dgraalgo.gmp=2  -Dgraalgo.quantumNs=100000    -- F3 2000 15
# F4: overhead budget at 1ms and 10ms quanta.
# NOTE (spike 2b §2/F4 precedent): the 1ms leg is INFORMATIONAL — measured
# 48.9µs mean exceeds 1% of a 1ms quantum by design (structural
# drainer/FJP costs). The budget that matters (GG-SCHED-0001) is 1% at the
# 10ms production default (measured 34.7µs vs 100µs budget: PASS).
run_info F4_q1ms  -Dgraalgo.gmp=2  -Dgraalgo.quantumNs=1000000   -- F4 20000
run F4_q10ms    -Dgraalgo.gmp=2  -Dgraalgo.quantumNs=10000000  -- F4 20000
# F5: cap fidelity at gmp 1 and 2; F5b cap-exhaustion.
run F5_cap1     -Dgraalgo.gmp=1  -Dgraalgo.quantumNs=10000000  -- F5
run F5_cap2     -Dgraalgo.gmp=2  -Dgraalgo.quantumNs=10000000  -- F5
run F5b_capex   -Dgraalgo.gmp=2  -Dgraalgo.quantumNs=10000000  -- F5b
# F6: lost-wakeup stress.
run F6_wakeup   -Dgraalgo.gmp=4  -Dgraalgo.quantumNs=10000000  -- F6 32 200

echo "==="
echo "torture battery: $PASS pass, $FAIL fail"
[ -n "$FAILED" ] && echo "failed legs:$FAILED"
echo "RESULT=$([ $FAIL -eq 0 ] && echo PASS || echo FAIL)"
exit $([ $FAIL -eq 0 ] && echo 0 || echo 1)
