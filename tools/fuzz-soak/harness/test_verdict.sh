#!/usr/bin/env bash
# Verdict tests for the fuzz soak (R-1108-144).
#
# Black-box tests: the scripts under test run against a STUB graalgo tree
# (fake `make main` target, fake generator, fake diff_one.sh) under /tmp,
# so no real JVM build or gc run is needed — only the verdict logic is
# exercised. The stub diff emits canned results controlled by env vars, so
# each case pins one verdict outcome.
#
#   STUB_RESULT        result for every program: PASS (default), SCOPE,
#                      WRONG_OUTPUT, GC_BUILD_FAIL, or NONJSON (prints
#                      garbage -> run_batch records HARNESS_ERROR).
#   STUB_DIVERGE_SEED  single seed number that diverges (WRONG_OUTPUT).
#   STUB_DIVERGE_BATCH single batch number whose programs diverge.
#
# Overnight cases use a fake `date` (PATH stub) that advances simulated
# time by $FAKE_DATE_STEP per `date +%s` call, so --hours 1 runs exactly
# two batches and stops.
#
# Usage: bash tools/fuzz-soak/harness/test_verdict.sh
# Exit 0 when every case passes, 1 otherwise. Prints PASS/FAIL per case.
set -uo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
RUN_BATCH="$HERE/run_batch.py"
RUN_SOAK="$HERE/../run_soak.sh"
CHECK="$HERE/check_soak_report.py"

T="$(mktemp -d /tmp/soak-verdict-test.XXXXXX)"
trap 'rm -rf "$T"' EXIT
mkdir -p "$T/home" "$T/tmp" "$T/gocache"

PASS_N=0; FAIL_N=0
ok()  { PASS_N=$((PASS_N + 1)); echo "PASS: $1"; }
bad() { FAIL_N=$((FAIL_N + 1)); echo "FAIL: $1"; }

# t <name> <expected-rc> <cmd...>: run cmd, compare its exit code.
t() {
  local name="$1" want="$2"; shift 2
  "$@" > "$T/last.log" 2>&1
  local rc=$?
  if [ "$rc" = "$want" ]; then
    ok "$name (rc=$rc)"
  else
    bad "$name (expected rc=$want, got rc=$rc)"
    tail -15 "$T/last.log" | sed 's/^/    /'
  fi
}

# expect_report <file> <key> <expected>: assert one soak-report.json field.
expect_report() {
  local got
  got="$(python3 -c "import json; print(json.load(open('$1')).get('$2'))")"
  if [ "$got" = "$3" ]; then
    ok "report $2=$got"
  else
    bad "report $2: expected $3, got $got"
  fi
}

build_stub_gr() {
  local gr="$T/stub-gr"
  mkdir -p "$gr/phases/phase3/fuzzing/gen" "$gr/phases/phase3/fuzzing/harness"
  printf 'main:\n\t@true\n' > "$gr/phases/phase3/Makefile"
  cat > "$gr/phases/phase3/fuzzing/gen/generator.py" <<'PYEOF'
import sys
out = None
args = sys.argv[1:]
for i, a in enumerate(args):
    if a == "--out":
        out = args[i + 1]
with open(out, "w") as f:
    f.write("package main\n\nfunc main() {}\n")
PYEOF
  cat > "$gr/phases/phase3/fuzzing/harness/diff_one.sh" <<'SHEOF'
#!/usr/bin/env bash
# Stub diff: emits one canned JSON record; no gc, no JVM.
gofile="$1"; workdir="$2"
base="$(basename "$gofile" .go)"
seed="${base#fuzz_}"
seed="$((10#$seed))"
batchdir="$(dirname "$(dirname "$workdir")")"
batch="${batchdir##*batch-}"
result="${STUB_RESULT:-PASS}"
if [ -n "${STUB_DIVERGE_SEED:-}" ] && [ "$seed" = "$STUB_DIVERGE_SEED" ]; then
  result="WRONG_OUTPUT"
elif [ -n "${STUB_DIVERGE_BATCH:-}" ] && [ "$batch" = "$STUB_DIVERGE_BATCH" ]; then
  result="WRONG_OUTPUT"
fi
if [ "$result" = "NONJSON" ]; then
  echo "stub diff error: not json"
  exit 0
fi
printf '{"file": "%s", "result": "%s", "detail": "stub"}\n' "$base" "$result"
SHEOF
  chmod +x "$gr/phases/phase3/fuzzing/harness/diff_one.sh"
}

build_fake_date() {
  mkdir -p "$T/bin"
  REAL_DATE="$(command -v date)"
  cat > "$T/bin/date" <<'EOF'
#!/usr/bin/env bash
# Fake date: each `date +%s` advances simulated time by FAKE_DATE_STEP.
if [ "${1:-}" = "+%s" ]; then
  n="$(cat "$FAKE_DATE_CTR" 2>/dev/null || echo 0)"
  echo $((FAKE_DATE_BASE + n * FAKE_DATE_STEP))
  echo $((n + 1)) > "$FAKE_DATE_CTR"
else
  exec "$REAL_DATE" "$@"
fi
EOF
  chmod +x "$T/bin/date"
}

build_stub_gr
build_fake_date

# --- check_soak_report.py (unit cases) ---
printf '{"batches": 2, "programs_run": 100, "failed_batches": 0, "diverged_total": 0}' > "$T/rep-clean.json"
printf '{"batches": 2, "programs_run": 100, "failed_batches": 0, "diverged_total": 3}' > "$T/rep-div.json"
printf '{"batches": 0, "diverged_total": 0}' > "$T/rep-zero.json"
printf '{"batches": 2, "programs_run": 100, "failed_batches": 1, "diverged_total": 0}' > "$T/rep-failed.json"
printf '{"batches": 2, "programs_run": 0, "failed_batches": 0, "diverged_total": 0}' > "$T/rep-noprog.json"
printf '{"batches": 3, "diverged_total": 0}' > "$T/rep-old.json"
t "check: missing report -> rc 2" 2 python3 "$CHECK" "$T/rep-missing.json"
t "check: clean report -> 0" 0 python3 "$CHECK" "$T/rep-clean.json"
t "check: diverged report -> 1" 1 python3 "$CHECK" "$T/rep-div.json"
t "check: zero batches -> 1" 1 python3 "$CHECK" "$T/rep-zero.json"
t "check: failed batch -> 1" 1 python3 "$CHECK" "$T/rep-failed.json"
t "check: zero programs -> 1" 1 python3 "$CHECK" "$T/rep-noprog.json"
t "check: old-format clean report -> 0" 0 python3 "$CHECK" "$T/rep-old.json"

# --- run_batch.py (stub GR) ---
t "run_batch: all PASS -> 0" 0 \
  env GR="$T/stub-gr" python3 "$RUN_BATCH" \
  --seeds 0:3 --out "$T/rb-pass" --jobs 2 --gmp 1
t "run_batch: all SCOPE -> 0" 0 \
  env GR="$T/stub-gr" STUB_RESULT=SCOPE python3 "$RUN_BATCH" \
  --seeds 0:3 --out "$T/rb-scope" --jobs 2 --gmp 1
t "run_batch: one WRONG_OUTPUT -> 1" 1 \
  env GR="$T/stub-gr" STUB_DIVERGE_SEED=1 python3 "$RUN_BATCH" \
  --seeds 0:3 --out "$T/rb-div" --jobs 2 --gmp 1
t "run_batch: GC_BUILD_FAIL -> 1" 1 \
  env GR="$T/stub-gr" STUB_RESULT=GC_BUILD_FAIL python3 "$RUN_BATCH" \
  --seeds 0:3 --out "$T/rb-gcbf" --jobs 2 --gmp 1
t "run_batch: HARNESS_ERROR -> 1" 1 \
  env GR="$T/stub-gr" STUB_RESULT=NONJSON python3 "$RUN_BATCH" \
  --seeds 0:3 --out "$T/rb-herr" --jobs 2 --gmp 1

# --- run_soak.sh --seeds (stub GR) ---
t "run_soak --seeds: all PASS -> 0" 0 \
  env GR="$T/stub-gr" TMPDIR="$T/tmp" GOCACHE="$T/gocache" HOME="$T/home" \
  JOBS=2 BATCH=3 bash "$RUN_SOAK" --seeds 0:3 --out "$T/soak-pass"
t "run_soak --seeds: one divergence -> 1" 1 \
  env GR="$T/stub-gr" TMPDIR="$T/tmp" GOCACHE="$T/gocache" HOME="$T/home" \
  JOBS=2 BATCH=3 STUB_DIVERGE_SEED=1 bash "$RUN_SOAK" --seeds 0:3 --out "$T/soak-div"

# --- run_soak.sh --hours 0: the vacuous-report shape ---
t "run_soak --hours 0: zero batches -> 1" 1 \
  env GR="$T/stub-gr" TMPDIR="$T/tmp" GOCACHE="$T/gocache" HOME="$T/home" \
  JOBS=2 BATCH=3 bash "$RUN_SOAK" --hours 0 --out "$T/soak-zero"
expect_report "$T/soak-zero/soak-report.json" batches 0

# --- run_soak.sh overnight with fake date: exactly 2 batches ---
echo 0 > "$T/ctr"
t "run_soak overnight: all PASS -> 0" 0 \
  env GR="$T/stub-gr" TMPDIR="$T/tmp" GOCACHE="$T/gocache" HOME="$T/home" \
  JOBS=2 BATCH=3 PATH="$T/bin:$PATH" \
  FAKE_DATE_BASE=1794000000 FAKE_DATE_STEP=1200 FAKE_DATE_CTR="$T/ctr" \
  bash "$RUN_SOAK" --hours 1 --out "$T/soak-night-pass"
expect_report "$T/soak-night-pass/soak-report.json" batches 2
expect_report "$T/soak-night-pass/soak-report.json" programs_run 6
expect_report "$T/soak-night-pass/soak-report.json" failed_batches 0
expect_report "$T/soak-night-pass/soak-report.json" diverged_total 0

echo 0 > "$T/ctr"
t "run_soak overnight: batch 1 diverges -> 1" 1 \
  env GR="$T/stub-gr" TMPDIR="$T/tmp" GOCACHE="$T/gocache" HOME="$T/home" \
  JOBS=2 BATCH=3 STUB_DIVERGE_BATCH=1 PATH="$T/bin:$PATH" \
  FAKE_DATE_BASE=1794000000 FAKE_DATE_STEP=1200 FAKE_DATE_CTR="$T/ctr" \
  bash "$RUN_SOAK" --hours 1 --out "$T/soak-night-div"
expect_report "$T/soak-night-div/soak-report.json" batches 2
expect_report "$T/soak-night-div/soak-report.json" programs_run 6
expect_report "$T/soak-night-div/soak-report.json" failed_batches 1
expect_report "$T/soak-night-div/soak-report.json" diverged_total 3

echo
echo "verdict tests: $PASS_N passed, $FAIL_N failed"
[ "$FAIL_N" = 0 ]
