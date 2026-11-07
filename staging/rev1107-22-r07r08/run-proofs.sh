#!/bin/sh
# RED->GREEN stub proofs for R-07 + R-08 (Makefile only, no real build).
# RED  = verify/red/Makefile  (original, HEAD 59f0a247)
# GREEN = verify/green/Makefile (FIX.patch applied)
# Stubs: ./stub-go stands in for $(GO); tools/gate1/run.py is a stub with the
# real run.py exit contract (0 iff PASS else 1); backend Makefiles are stubs.
# Every expectation is checked explicitly; the script exits non-zero if any
# expectation fails. Log: verify/proof.log
V=/tmp/rev1107-22-r07r08/verify
LOG=$V/proof.log
: > $LOG
fail=0
note() { echo "$*" | tee -a $LOG; }
check() { # $1=label $2=expected $3=actual
  if [ "$2" = "$3" ]; then note "PASS: $1 (expected=$2 actual=$3)";
  else note "FAIL: $1 (expected=$2 actual=$3)"; fail=1; fi
}

# --- R-07 RED: second `make graalgo` is a no-op (stale driver) ---
note "=== R-07 RED (original Makefile) ==="
cd $V/red && rm -f graalgo go.log run.log
make GO=./stub-go graalgo >> $LOG 2>&1
make GO=./stub-go graalgo >> $LOG 2>&1
# Count only recipe invocations: Makefile:264 runs `$(GO) version` at parse
# time on every make run, so go.log also gets one "stub-go version" line per
# make invocation; the recipe is the "stub-go build ..." line.
n=$(grep -c '^stub-go build' go.log)
check "R-07 RED: stub-go BUILD invocations across two makes" "0" "$n"

# --- R-07 GREEN: both makes rebuild ---
note "=== R-07 GREEN (patched Makefile) ==="
cd $V/green && rm -f graalgo go.log run.log
make GO=./stub-go graalgo >> $LOG 2>&1
make GO=./stub-go graalgo >> $LOG 2>&1
n=$(grep -c '^stub-go build' go.log)
check "R-07 GREEN: stub-go BUILD invocations across two makes" "2" "$n"

# --- R-07 clean RED: `make clean` leaves ./graalgo ---
note "=== clean RED ==="
cd $V/red && rm -f graalgo && touch graalgo
make clean >> $LOG 2>&1
if [ -f graalgo ]; then r=present; else r=absent; fi
check "clean RED: ./graalgo after make clean" "present" "$r"

# --- R-07 clean GREEN: `make clean` removes ./graalgo ---
note "=== clean GREEN ==="
cd $V/green && rm -f graalgo && touch graalgo
make clean >> $LOG 2>&1
if [ -f graalgo ]; then r=present; else r=absent; fi
check "clean GREEN: ./graalgo after make clean" "absent" "$r"

# --- R-08 RED: one diverging package, pilot still exits 0 ---
note "=== R-08 RED (one diverging package) ==="
cd $V/red && rm -f run.log && rm -rf tools/gate1/.tmp
FAIL_PKGS=errors make gate1-pilot >> $LOG 2>&1
rc=$?
n=$(wc -l < run.log)
check "R-08 RED: pilot exit code with 1/5 diverging" "0" "$rc"
check "R-08 RED: packages run" "5" "$n"

# --- R-08 GREEN: one diverging package, pilot fails, all 5 still run ---
note "=== R-08 GREEN (one diverging package) ==="
cd $V/green && rm -f run.log && rm -rf tools/gate1/.tmp
FAIL_PKGS=errors make gate1-pilot >> $LOG 2>&1
rc=$?
n=$(wc -l < run.log)
check "R-08 GREEN: pilot exit code with 1/5 diverging (non-zero)" "nonzero" "$([ "$rc" -ne 0 ] && echo nonzero || echo zero)"
check "R-08 GREEN: packages run" "5" "$n"
grep -q "Gate 1 pilot FAILED" $LOG && note "PASS: R-08 GREEN: FAILED banner printed" || { note "FAIL: R-08 GREEN: no FAILED banner"; fail=1; }

# --- R-08 GREEN all-pass: pilot exits 0, complete banner ---
note "=== R-08 GREEN (all pass) ==="
cd $V/green && rm -f run.log && rm -rf tools/gate1/.tmp
FAIL_PKGS= make gate1-pilot >> $LOG 2>&1
rc=$?
n=$(wc -l < run.log)
check "R-08 GREEN all-pass: pilot exit code" "0" "$rc"
check "R-08 GREEN all-pass: packages run" "5" "$n"

# --- R-08 GREEN all-diverge: fails, all 5 still run ---
note "=== R-08 GREEN (all diverge) ==="
cd $V/green && rm -f run.log && rm -rf tools/gate1/.tmp
FAIL_PKGS="strconv errors sort strings bytes" make gate1-pilot >> $LOG 2>&1
rc=$?
n=$(wc -l < run.log)
check "R-08 GREEN all-diverge: pilot exit code (non-zero)" "nonzero" "$([ "$rc" -ne 0 ] && echo nonzero || echo zero)"
check "R-08 GREEN all-diverge: packages run" "5" "$n"

note "=== SUMMARY ==="
if [ "$fail" -eq 0 ]; then note "ALL PROOFS PASSED"; else note "SOME PROOFS FAILED"; fi
exit $fail
