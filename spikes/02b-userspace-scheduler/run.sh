#!/bin/bash
# Spike 2b battery. Each workload logs to logs/<name>.log; a non-zero exit
# anywhere is a FAIL signal for that falsifier.
set -uo pipefail
JH=~/tools/graalvm-jdk-25
S=~/workspace/graalgo/spikes/02b-userspace-scheduler
CP="$S/classes"
LOG="$S/logs"
mkdir -p "$LOG"

run() { # name, parallelism, gomaxprocs, quantumNs, class, args...
    local name="$1" par="$2" gmp="$3" qns="$4"; shift 4
    local log="$LOG/$name.log"
    echo "=== $name (par=$par gmp=$gmp quantumNs=$qns) ===" | tee "$log"
    "$JH/bin/java" \
        -Djdk.virtualThreadScheduler.parallelism="$par" \
        -Dusched.gomaxprocs="$gmp" \
        -Dusched.quantumNs="$qns" \
        -cp "$CP" "$@" >>"$log" 2>&1
    local rc=$?
    echo "exit=$rc" | tee -a "$log"
    return $rc
}

FAILED=0
fail() { echo "BATTERY: $1 FAILED"; FAILED=1; }

# Environment noise probe (measurement honesty: thread-wakeup latency, the
# dominant overhead-hop cost, degrades under CPU contention — record it).
{ echo "date=$(date -u +%FT%TZ)"; echo "nproc=$(nproc)"; uptime; "$JH/bin/java" -version 2>&1 | head -1; } > "$LOG/env.log" 2>&1
cat "$LOG/env.log"

# F1: O1 torture (binary gate) — 2 spinners + latency pair, 5 s
run o1_q1ms 2 2 1000000 work.O1Torture 2 5 || fail "o1_q1ms"
# F1b: O1 with 100 us quanta
run o1_q100us 2 2 100000 work.O1Torture 2 5 || fail "o1_q100us"
# F1c: O1 with 8 carriers but cap=2 (cap independent of carriers)
run o1_cap2_par8 8 2 1000000 work.O1Torture 2 5 || fail "o1_cap2_par8"
# F1d (P1-3): O1 at the 10 ms default quantum — the [DECIDED] default must be
# measured, not just arithmetically projected
run o1_q10ms 2 2 10000000 work.O1Torture 2 5 || fail "o1_q10ms"
# F1e (P1-1): GOMAXPROCS above carrier parallelism — liveness under the
# effective-cap clamp (req=32, par=2 -> effective cap=2)
run o1_gmp32_par2 2 32 1000000 work.O1Torture 2 5 || fail "o1_gmp32_par2"
# F2: O2/O3/O4/O5 correctness
run o2_chan 2 2 1000000 work.O2Chan || fail "o2_chan"
run o3_select 2 2 1000000 work.O3Select || fail "o3_select"
run o4_timer 2 2 1000000 work.O4Timer || fail "o4_timer"
run o5_scale_cap1 2 1 1000000 work.O5Scale || fail "o5_scale_cap1"
run o5_scale_cap2 2 2 1000000 work.O5Scale || fail "o5_scale_cap2"
# F3: quiescence — 10k spinners, 100 us quanta, 20 s
run quiescence_10k 2 2 100000 work.Quiescence 10000 20 || fail "quiescence_10k"
# F4: overhead budget. NOTE: the 1 ms leg is EXPECTED to fail on the mean
# (the documented fired kill trigger: 1% of 1 ms = 10 us); the 10 ms leg
# carries the scoped fix's verdict.
run overhead 2 2 1000000 work.Overhead 200000 || fail "overhead"
run overhead_q10ms 2 2 10000000 work.Overhead 200000 || fail "overhead_q10ms"
# F5: GOMAXPROCS fidelity
run gomaxprocs_cap1 2 1 1000000 work.Gomaxprocs par || fail "gomaxprocs_cap1"
run gomaxprocs_cap2 2 2 1000000 work.Gomaxprocs par || fail "gomaxprocs_cap2"
run gomaxprocs_mixed 2 2 1000000 work.Gomaxprocs mixed || fail "gomaxprocs_mixed"
# pure-compute baseline for the mixed case (2 spinners, no sleeper)
run gomaxprocs_baseline 2 2 1000000 work.Gomaxprocs mixed_nosleeper || fail "gomaxprocs_baseline"
# F5b (P0-2): cap-exhaustion starvation probe — 2 indefinitely I/O-blocked
# goroutines at cap=2 must not starve the spinner (slot-releasing ioEnter)
run capexhaustion 2 2 1000000 work.CapExhaustion 5 || fail "capexhaustion"
# F6: lost-wakeup stress
run stress 2 2 1000000 work.LostWakeupStress || fail "stress"

echo "BATTERY_DONE failed=$FAILED"
exit $FAILED
