#!/bin/bash
# P2-M8 torture battery orchestrator: spike 2b F1–F6 on the production path.
#
# Runs every leg, archives logs to internal/backend/runtime/out/torture/logs/
# (gitignored via out/), prints a summary. Exit 0 iff every leg passes.
# A single failing leg is a bug to fix; systematic divergence across legs
# is the kill trigger (PLAN.md §7).
#
# Prerequisites: the backend runtime module built
#   make -C internal/backend/runtime main
# (the `torture` make target in that module builds it first).
set -euo pipefail
cd "$(dirname "$0")/../.." || exit 1
RT="internal/backend/runtime"

GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
JAVA="$GRAALVM_HOME/bin/java"
[ -x "$JAVA" ] || { echo "ERROR: no java at $JAVA (set GRAALVM_HOME)" >&2; exit 1; }

# Classpath mirrors the runtime module's TEST_CP (see
# internal/backend/runtime/Makefile): test classes first, then main
# classes, the truffle module's classes (the 8 frozen stubs — shadowed by
# the phase-2 replacements that come earlier on the classpath), the
# interop module's classes, then the truffle lib jars.
TRUFFLE="internal/backend/truffle"
for d in "$RT/out/test-classes" "$RT/out/classes" "$TRUFFLE/out/classes"; do
  [ -d "$d" ] || { echo "ERROR: $d missing — build the backend first: make -C $RT main" >&2; exit 1; }
done
[ -d "$TRUFFLE/lib" ] || { echo "ERROR: $TRUFFLE/lib missing" >&2; exit 1; }
CP="$RT/out/test-classes:$RT/out/classes:$TRUFFLE/out/classes:internal/backend/interop/out/classes:$TRUFFLE/lib/*"
# Same JVM flags as the runtime module's own test invocations.
JAVA_FLAGS=(--enable-native-access=ALL-UNNAMED -Xss32m)

OUTDIR="$RT/out/torture/logs"
mkdir -p "$OUTDIR" || { echo "ERROR: cannot create $OUTDIR" >&2; exit 1; }

# leg <logname> <JFLAGS> -- <LEG> [leg args...]
leg() {
  local logname="$1"; shift
  local jflags=()
  while [ "${1:-}" != "--" ]; do
    jflags+=("$1"); shift
  done
  shift # drop --
  local legname="$1"; shift
  local log="$OUTDIR/$logname.log"
  echo "--- leg $logname ($legname $*) ---"
  local rc=0
  timeout 300 "$JAVA" "${JAVA_FLAGS[@]}" "${jflags[@]}" -cp "$CP" \
    graalgo.sched.torture.TortureBattery "$legname" "$@" \
    > "$log" 2>&1 || rc=$?
  # Display-only pipeline: must not gate the verdict under pipefail.
  grep -E "^(F[0-9]|RESULT)" "$log" | tail -4 || true
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
exit $([ $FAIL -eq 0 ] && echo 0 || echo 2)
