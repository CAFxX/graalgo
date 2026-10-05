#!/bin/bash
# P2-M8 torture battery orchestrator: spike 2b F1–F6 on the production path.
#
# Runs every leg, archives logs to hidden_files/torture/logs/, prints a
# summary. Exit 0 iff every leg passes. A single failing leg is a bug to
# fix; systematic divergence across legs is the kill trigger (PLAN.md §7).
set -u
cd "$(dirname "$0")/../.." || exit 1

GRAALVM_HOME="${GRAALVM_HOME:-$HOME/tools/graalvm-jdk-25}"
CP="out/test-classes:out/classes:../phase1/out/classes:../phase1/lib/collections-25.0.4.1.1.jar:../phase1/lib/graal-sdk-25.0.4.1.1.jar:../phase1/lib/nativeimage-25.0.4.1.1.jar:../phase1/lib/polyglot-25.0.4.1.1.jar:../phase1/lib/truffle-api-25.0.4.1.1.jar:../phase1/lib/truffle-dsl-processor-25.0.4.1.1.jar:../phase1/lib/truffle-runtime-25.0.4.1.1.jar"
OUTDIR="hidden_files/torture/logs"
mkdir -p "$OUTDIR"
JAVA="$GRAALVM_HOME/bin/java"

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
  timeout 300 "$JAVA" "${jflags[@]}" -cp "$CP" \
    graalgo.sched.torture.TortureBattery "$legname" "$@" \
    > "$log" 2>&1
  local rc=$?
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

# F1: O1 torture, 5 legs (mirror spike 2b §2/F1).
run F1_q1ms     -Dgraalgo.gmp=2  -Dgraalgo.quantumNs=1000000   -- F1 2 10
run F1_q100us   -Dgraalgo.gmp=2  -Dgraalgo.quantumNs=100000    -- F1 2 10
run F1_cap2par8 -Dgraalgo.gmp=2  -Dgraalgo.quantumNs=1000000   -- F1 6 10
run F1_q10ms    -Dgraalgo.gmp=2  -Dgraalgo.quantumNs=10000000  -- F1 2 10
run F1_gmp32par2 -Dgraalgo.gmp=32 -Dgraalgo.quantumNs=1000000 -- F1 2 10
# F3: quiescence (2k spinners for the battery; full 10k extended).
run F3_quiesce  -Dgraalgo.gmp=2  -Dgraalgo.quantumNs=100000    -- F3 2000 15
# F4: overhead budget at 1ms and 10ms quanta.
run F4_q1ms     -Dgraalgo.gmp=2  -Dgraalgo.quantumNs=1000000   -- F4 20000
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
