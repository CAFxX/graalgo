#!/usr/bin/env bash
# Regression tests for the fuzz-soak gates (review21-fuzzgates).
#
# Covers, without needing the graalgo JVM backend:
#  1. run_batch.py gate verdict (unit tests, every divergence class fails).
#  2. check_soak_report.py verdict (planted reports + missing report).
#  3. run_soak.sh single-batch end to end with a stub backend tree:
#     a planted WRONG_OUTPUT divergence makes the soak exit 1, and an
#     all-PASS batch exits 0. The stub backend replaces only the
#     generator/diff_one legs (via GR); run_soak.sh, run_batch.py and
#     the minimize plumbing are the real scripts.
#
# Usage: bash tools/fuzz-soak/harness/gate_regression_test.sh
# Exit non-zero on any failure. Leaves nothing behind (mktemp + trap).
set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
SOAK_DIR="$(cd "$HERE/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
export TMPDIR="$TMP/tmp"
mkdir -p "$TMPDIR"
export GOCACHE="$TMP/gocache"
mkdir -p "$GOCACHE"

fail() { echo "FAIL: $*" >&2; exit 1; }

echo "== [1] run_batch gate verdict =="
python3 "$HERE/run_batch_test.py" 2>&1 | tail -3

echo "== [2] check_soak_report verdict =="
check() { # $1 = report file (or MISSING), $2 = want rc, $3 = label
  local rc=0
  if [ "$1" = "MISSING" ]; then
    python3 "$HERE/check_soak_report.py" "$TMP/no-such-report.json" >/dev/null 2>&1 || rc=$?
  else
    python3 "$HERE/check_soak_report.py" "$1" >/dev/null 2>&1 || rc=$?
  fi
  [ "$rc" -eq "$2" ] || fail "check_soak_report $3: want rc $2, got $rc"
  echo "  ok: $3 -> rc $rc"
}
printf '%s' '{"batches": 2, "programs": 100, "diverged_total": 0, "minimize_failed": false}' > "$TMP/clean.json"
printf '%s' '{"batches": 2, "programs": 100, "diverged_total": 3, "minimize_failed": false}' > "$TMP/div.json"
printf '%s' '{"batches": 2, "programs": 100, "diverged_total": 0, "minimize_failed": true}' > "$TMP/minfail.json"
printf '%s' '{"batches": 0, "programs": 0, "diverged_total": 0, "minimize_failed": false}' > "$TMP/zero.json"
printf '%s' '{"batches": 2, "minimize_failed": false}' > "$TMP/nofield.json"
printf '%s' '{"batches": 3, "programs": 0, "diverged_total": 0, "minimize_failed": false}' > "$TMP/noprograms.json"
printf '%s' '{"batches": 2, "programs": 100, "minimize_failed": false}' > "$TMP/nodiv.json"
check "$TMP/clean.json" 0 "clean soak passes"
check "$TMP/div.json" 1 "planted divergences fail"
check "$TMP/minfail.json" 1 "failed minimization fails"
check "$TMP/zero.json" 1 "zero batches fail"
check "$TMP/nofield.json" 1 "report without programs field fails"
check "$TMP/noprograms.json" 1 "batches exist but zero programs sampled fail (all batches failed at build)"
check "$TMP/nodiv.json" 1 "malformed report fails"
check MISSING 1 "missing report fails"

echo "== [3] run_soak single-batch with stub backend =="
# Stub backend tree honouring run_batch.py's GR-derived layout:
#   $GR/Makefile (build target), $GR/tools/fuzz-soak/gen/generator.py,
#   $GR/tools/fuzz-soak/harness/diff_one.sh, and a stub gofrontend
#   binary (so run_batch.py's gofrontend check passes without building).
#   The diff_one result class is switched via $STUB_RESULT so the same
#   tree serves both directions.
mkdir -p "$TMP/fake-gr/tools/fuzz-soak/gen" "$TMP/fake-gr/tools/fuzz-soak/harness" "$TMP/fake-gr/internal/frontend"
printf 'build:\n\t@true\n' > "$TMP/fake-gr/Makefile"
: > "$TMP/fake-gr/internal/frontend/gofrontend"
chmod +x "$TMP/fake-gr/internal/frontend/gofrontend"
cat > "$TMP/fake-gr/tools/fuzz-soak/gen/generator.py" <<'EOF'
import argparse
ap = argparse.ArgumentParser()
ap.add_argument("--seed", required=True)
ap.add_argument("--out", required=True)
a = ap.parse_args()
with open(a.out, "w") as f:
    f.write('package main\n\nfunc main() {\n\tprintln("x")\n}\n')
EOF
cat > "$TMP/fake-gr/tools/fuzz-soak/harness/diff_one.sh" <<'EOF'
#!/bin/bash
# $STUB_RESULT is the planted class; $1 is the .go file, $2 the workdir.
base="$(basename "$1" .go)"
printf '{"file": "%s", "result": "%s", "detail": "planted"}\n' "$base" "${STUB_RESULT:-PASS}"
EOF
chmod +x "$TMP/fake-gr/tools/fuzz-soak/harness/diff_one.sh"

export GR="$TMP/fake-gr"
export GO_BIN_DIR="$HOME/tools/go/bin"

export STUB_RESULT=WRONG_OUTPUT
rc=0
"$SOAK_DIR/run_soak.sh" --seeds 0:1 --out "$TMP/soak-div" > "$TMP/soak-div.log" 2>&1 || rc=$?
[ "$rc" -ne 0 ] || fail "planted WRONG_OUTPUT soak exited 0"
grep -q "WRONG_OUTPUT" "$TMP/soak-div/batch-0/results.jsonl" || fail "planted record missing from results.jsonl"
echo "  ok: planted WRONG_OUTPUT soak exits $rc"

export STUB_RESULT=PASS
rc=0
"$SOAK_DIR/run_soak.sh" --seeds 0:1 --out "$TMP/soak-pass" > "$TMP/soak-pass.log" 2>&1 || rc=$?
[ "$rc" -eq 0 ] || { echo "--- soak-pass log ---"; cat "$TMP/soak-pass.log"; fail "all-PASS soak exited $rc"; }
echo "  ok: all-PASS soak exits 0"

echo "ALL SOAK GATE REGRESSION TESTS PASSED"
