#!/usr/bin/env bash
# Smoke test for run_soak.sh minimize_findings (REVIEW-2026-11-08-15 SEG-C-05,
# R-1108-145).
#
# Extracts the REAL minimize_findings function from run_soak.sh and drives it
# against a stub triage.py that enforces the REAL triage.py interface
# (--prog/--out are required flags; --out is a file, opened for writing).
#
# The test fails on the pre-fix code in two independent ways:
#   1. the old call passes the program positionally, so the stub exits 2
#      (argparse) and the recorded argv lacks --prog;
#   2. the old code swallows a non-zero triage rc and returns 0.
#
# Usage:
#   bash tools/fuzz-soak/tests/test_minimize_findings.sh
#   RUN_SOAK=/path/to/run_soak.sh bash tools/fuzz-soak/tests/test_minimize_findings.sh
#       (run against a different run_soak.sh, e.g. the pre-fix version for
#       a RED check)
set -u

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)"
RUN_SOAK="${RUN_SOAK:-$ROOT/tools/fuzz-soak/run_soak.sh}"
FAILURES=0

fail() { echo "FAIL: $*"; FAILURES=$((FAILURES + 1)); }
pass() { echo "ok: $*"; }

# Build a fixture: stub triage.py + a batch dir with one diverged program.
# Sets globals: TMP, HERE, TRIAGE_ARGV_LOG.
setup_fixture() {
  TMP="$(mktemp -d)"
  mkdir -p "$TMP/harness" "$TMP/batch/programs"
  cat > "$TMP/harness/triage.py" <<'PYEOF'
import argparse, os, sys
ap = argparse.ArgumentParser(prog="triage.py")
ap.add_argument("--prog", required=True)
ap.add_argument("--out", required=True)
ap.add_argument("--workdir", default="/tmp/triage")
args = ap.parse_args()
with open(os.environ["TRIAGE_ARGV_LOG"], "a") as f:
    f.write(" ".join(sys.argv[1:]) + "\n")
# --out is a FILE (real triage.py opens it for writing); a directory fails.
with open(args.out, "w") as f:
    f.write("minimized\n")
sys.exit(int(os.environ.get("TRIAGE_STUB_RC", "0")))
PYEOF
  # Extract the REAL minimize_findings from run_soak.sh (never a copy).
  sed -n '/^minimize_findings() {/,/^}/p' "$RUN_SOAK" > "$TMP/minimize_findings.sh"
  # shellcheck disable=SC1091
  source "$TMP/minimize_findings.sh"
  HERE="$TMP"
  export TRIAGE_ARGV_LOG="$TMP/argv.log"
  : > "$TRIAGE_ARGV_LOG"
}

teardown_fixture() { rm -rf "$TMP"; }

write_batch() { # $1 = results.jsonl content lines, $2 = "prog" to create programs/fuzz_00001.go
  printf '%s\n' "$1" > "$TMP/batch/results.jsonl"
  if [ "$2" = "prog" ]; then
    printf 'package main\n\nfunc main() {}\n' > "$TMP/batch/programs/fuzz_00001.go"
  fi
}

# 1. Happy path: triage succeeds; call shape is --prog/--out file; rc 0.
setup_fixture
write_batch '{"file": "fuzz_00001", "result": "WRONG_OUTPUT"}
{"file": "fuzz_00002", "result": "PASS"}' prog
TRIAGE_STUB_RC=0 minimize_findings "$TMP/batch"
rc=$?
[ "$rc" -eq 0 ] || fail "success case: minimize_findings rc=$rc, want 0"
[ -f "$TMP/batch/minimized/fuzz_00001.go" ] || fail "success case: minimized/fuzz_00001.go not created"
grep -q -- "--prog $TMP/batch/programs/fuzz_00001.go --out $TMP/batch/minimized/fuzz_00001.go" "$TRIAGE_ARGV_LOG" \
  || fail "success case: argv lacks '--prog <prog> --out <minimized>/fuzz_00001.go': $(cat "$TRIAGE_ARGV_LOG")"
[ "$FAILURES" -eq 0 ] && pass "success case: --prog/--out shape, minimized file written, rc 0"
teardown_fixture

# 2. Triage failure propagates: stub rc 1 -> minimize_findings rc non-zero.
setup_fixture
write_batch '{"file": "fuzz_00001", "result": "CRASH"}' prog
TRIAGE_STUB_RC=1 minimize_findings "$TMP/batch"
rc=$?
[ "$rc" -ne 0 ] || fail "propagation case: minimize_findings rc=0 with triage rc=1 (failure swallowed)"
[ "$FAILURES" -eq 0 ] && pass "propagation case: triage rc=1 -> minimize_findings rc=$rc"
teardown_fixture

# 3. No results.jsonl -> rc 0, nothing to triage.
setup_fixture
minimize_findings "$TMP/batch"
rc=$?
[ "$rc" -eq 0 ] || fail "no-results case: rc=$rc, want 0"
[ ! -s "$TRIAGE_ARGV_LOG" ] || fail "no-results case: triage invoked with no results"
[ "$FAILURES" -eq 0 ] && pass "no-results case: rc 0, triage not invoked"
teardown_fixture

# 4. All PASS -> rc 0, triage not invoked.
setup_fixture
write_batch '{"file": "fuzz_00001", "result": "PASS"}' prog
TRIAGE_STUB_RC=0 minimize_findings "$TMP/batch"
rc=$?
[ "$rc" -eq 0 ] || fail "all-pass case: rc=$rc, want 0"
[ ! -s "$TRIAGE_ARGV_LOG" ] || fail "all-pass case: triage invoked with no divergences"
[ "$FAILURES" -eq 0 ] && pass "all-pass case: rc 0, triage not invoked"
teardown_fixture

# 5. Missing program file -> skipped, rc 0 (documented behavior, not a failure).
setup_fixture
write_batch '{"file": "fuzz_00001", "result": "WRONG_OUTPUT"}' noprog
TRIAGE_STUB_RC=0 minimize_findings "$TMP/batch"
rc=$?
[ "$rc" -eq 0 ] || fail "missing-program case: rc=$rc, want 0"
[ ! -s "$TRIAGE_ARGV_LOG" ] || fail "missing-program case: triage invoked for a missing program"
[ "$FAILURES" -eq 0 ] && pass "missing-program case: skipped, rc 0"
teardown_fixture

if [ "$FAILURES" -ne 0 ]; then
  echo "test_minimize_findings: $FAILURES failure(s)"
  exit 1
fi
echo "test_minimize_findings: all 5 cases pass"
exit 0
