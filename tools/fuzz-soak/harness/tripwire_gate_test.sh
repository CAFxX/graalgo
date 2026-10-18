#!/usr/bin/env bash
# Honest-gate tripwire for tools/fuzz-soak/run_soak.sh.
#
# The P0 green-lie (every batch failing at a dead build step while the soak
# exited 0) regressed silently once because the gate property — "a soak that
# produced zero results.jsonl exits non-zero" — had no automated test. This
# script is that test: it runs the REAL run_soak.sh against a STUB batch
# driver (no backend, no JVM, no Go toolchain needed) and asserts:
#   scenario 1 (all batches fail): exit 2 + "SOAK FAILED" on stderr
#   scenario 2 (batch succeeds):   exit 0 + batch-0/results.jsonl present
#
# Determinism: run_soak.sh is time-bounded (--hours), so the test shims
# `date` (exported function) with a fake clock that yields exactly one
# batch, then jumps past the deadline. No sleeping, no flakes.
#
# Run directly: ./tools/fuzz-soak/harness/tripwire_gate_test.sh
# Also runs in per-push CI via `make check`.
set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
SOAK="$HERE/../run_soak.sh"
[ -f "$SOAK" ] || { echo "run_soak.sh not found at $SOAK" >&2; exit 1; }

fail() { echo "FAIL: $*" >&2; exit 1; }

SANDBOX="$(mktemp -d)"
trap 'rm -rf "$SANDBOX"' EXIT

# Sandbox mirrors the real layout so run_soak.sh needs no changes:
#   sandbox/tools/fuzz-soak/run_soak.sh            (copy of the real script)
#   sandbox/tools/fuzz-soak/harness/run_batch.py   (stub batch driver)
mkdir -p "$SANDBOX/tools/fuzz-soak/harness"
cp "$SOAK" "$SANDBOX/tools/fuzz-soak/run_soak.sh"
cat > "$SANDBOX/tools/fuzz-soak/harness/run_batch.py" <<'STUB_EOF'
#!/usr/bin/env python3
"""Stub batch driver for the honest-gate tripwire test (no backend needed).

STUB_MODE=fail    -> exit 1, writes nothing (batch produced no results).
STUB_MODE=success -> exit 0, writes one PASS results.jsonl.
"""
import json
import os
import sys

out = sys.argv[sys.argv.index("--out") + 1]
os.makedirs(out, exist_ok=True)
if os.environ.get("STUB_MODE") == "success":
    with open(os.path.join(out, "results.jsonl"), "w") as f:
        f.write(json.dumps({"file": "fuzz_00000", "result": "PASS",
                            "detail": "", "seed": 0}) + "\n")
    sys.exit(0)
sys.exit(1)
STUB_EOF
chmod +x "$SANDBOX/tools/fuzz-soak/harness/run_batch.py"

_FAKE_T0=1000000

# Run the real run_soak.sh for exactly one batch under a fake clock.
run_soak_one_batch() {
  local mode="$1" out="$2"
  # Exported: the shim below runs inside run_soak.sh's shell (set -u), so
  # everything it touches must cross the environment, not closures.
  export _DATE_NFILE="$SANDBOX/date_calls"
  export _FAKE_T0
  rm -f "$_DATE_NFILE"
  # Call 1: START_TS assignment -> T0.
  # Call 2: first loop check -> T0 (< deadline: run one batch).
  # Call 3+: deadline reached -> exit the loop.
  date() {
    local n
    n="$(cat "$_DATE_NFILE" 2>/dev/null || echo 0)"
    echo $((n + 1)) > "$_DATE_NFILE"
    if [ "$n" -lt 2 ]; then
      printf '%s\n' "$_FAKE_T0"
    else
      printf '%s\n' $((_FAKE_T0 + 7200))
    fi
  }
  export -f date
  STUB_MODE="$mode" \
  JAVA_BIN=/bin/true \
  JOBS=1 BATCH=1 \
    bash "$SANDBOX/tools/fuzz-soak/run_soak.sh" --hours 1 --out "$out"
}

echo "== scenario 1: all batches fail -> exit 2 with SOAK FAILED =="
rc=0
run_soak_one_batch fail "$SANDBOX/out-fail" >"$SANDBOX/fail.log" 2>&1 || rc=$?
[ "$rc" -eq 2 ] || fail "all-fail: expected exit 2, got $rc (see $SANDBOX/fail.log)"
grep -q "SOAK FAILED" "$SANDBOX/fail.log" \
  || fail "all-fail: 'SOAK FAILED' not in output"
echo "ok: exit 2 + SOAK FAILED"

echo "== scenario 2: batch succeeds -> exit 0 =="
rc=0
run_soak_one_batch success "$SANDBOX/out-ok" >"$SANDBOX/ok.log" 2>&1 || rc=$?
[ "$rc" -eq 0 ] || fail "success: expected exit 0, got $rc (see $SANDBOX/ok.log)"
[ -f "$SANDBOX/out-ok/batch-0/results.jsonl" ] \
  || fail "success: batch-0/results.jsonl missing"
echo "ok: exit 0 + results.jsonl present"

echo "TRIPWIRE PASS: honest-gate holds (fail->2, success->0)"
