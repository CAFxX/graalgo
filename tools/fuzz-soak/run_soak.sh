#!/usr/bin/env bash
# Layer-4 fuzz soak runner (phases/phase3/PLAN.md §5 layer 4, GG-0037):
# generative differential fuzzing, overnight per release.
#
# Runs the seeded program generator + differential harness
# (harness/run_batch.py) in time-bounded batches until HOURS elapse, then
# minimizes any divergences (harness/triage.py) and archives the findings.
# Minimized divergences become permanent regressions per SPEC §5.4 (filed
# under phases/phase3/fuzzing/findings/ with a minimized reproducer).
#
# This is the OVERNIGHT slot. The CONTINUOUS adversarial track reserves the
# same interface: `run_soak.sh --seeds A:B` runs one bounded batch (used by
# the continuous track's scheduler); the scheduled GHA workflow
# (.github/workflows/fuzz-soak.yml) runs the overnight soak per release.
#
# Usage:
#   run_soak.sh --hours H --out DIR     overnight soak, ~H hours of batches
#   run_soak.sh --seeds A:B --out DIR   single bounded batch (continuous slot)
#
# Environment:
#   GR           repo root (default: derived from this file's location)
#   GO_BIN_DIR   Go toolchain bin dir (default: $HOME/tools/go/bin)
#   JAVA_BIN     java binary (default: $HOME/tools/graalvm-jdk-25/bin/java)
#   JOBS         parallel diffs per batch (default: 4)
#   BATCH        seeds per batch (default: 50)
set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# Repo root: fuzzing/run_soak.sh -> phase3 -> phases -> repo.
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
export GR="${GR:-$REPO_ROOT}"
export GO_BIN_DIR="${GO_BIN_DIR:-$HOME/tools/go/bin}"
export JAVA_BIN="${JAVA_BIN:-$HOME/tools/graalvm-jdk-25/bin/java}"
export PATH="$GO_BIN_DIR:$PATH"
JOBS="${JOBS:-4}"
BATCH="${BATCH:-50}"

HOURS=""; SEEDS=""; OUT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --hours) HOURS="$2"; shift 2;;
    --seeds) SEEDS="$2"; shift 2;;
    --out) OUT="$2"; shift 2;;
    *) echo "unknown arg: $1" >&2; exit 2;;
  esac
done
[ -n "$OUT" ] || { echo "--out is required" >&2; exit 2; }

mkdir -p "$OUT"
export TMPDIR="${TMPDIR:-$OUT/tmp}"
mkdir -p "$TMPDIR"
export GOCACHE="${GOCACHE:-$HOME/workspace/.gocache-fuzz-soak}"
mkdir -p "$GOCACHE"
export FUZZ_TMPDIR="$TMPDIR"

START_TS=$(date +%s)
DEADLINE=$((START_TS + ${HOURS:-0} * 3600))

run_batch() {
  local seeds="$1" batchdir="$2"
  python3 "$HERE/harness/run_batch.py" \
    --seeds "$seeds" --out "$batchdir" --jobs "$JOBS" --gmp 1
}

minimize_findings() {
  local batchdir="$1"
  # triage.py minimizes each diverged program; failures to minimize are
  # recorded, never silently dropped.
  python3 - "$HERE" "$batchdir" <<'EOF'
import json, os, subprocess, sys
here, batchdir = sys.argv[1], sys.argv[2]
res = os.path.join(batchdir, "results.jsonl")
if not os.path.exists(res):
    print("no results.jsonl; nothing to triage")
    sys.exit(0)
diverged = []
for line in open(res):
    r = json.loads(line)
    if r.get("result") not in ("PASS", "SCOPE", "GC_BUILD_FAIL"):
        diverged.append(r)
print(f"{len(diverged)} diverged programs to minimize")
out = os.path.join(batchdir, "minimized")
os.makedirs(out, exist_ok=True)
for r in diverged:
    prog = os.path.join(batchdir, "programs", r["file"] + ".go")
    if not os.path.exists(prog):
        print(f"missing program for {r['file']}; skipping")
        continue
    p = subprocess.run(
        ["python3", f"{here}/harness/triage.py", prog, "--out", out],
        capture_output=True, text=True, timeout=1200)
    print(f"{r['file']}: triage rc={p.returncode}")
    if p.returncode != 0:
        print(p.stderr[-500:])
EOF
}

if [ -n "$SEEDS" ]; then
  # Single bounded batch: the continuous-track slot.
  echo "== soak batch: seeds $SEEDS =="
  # Verdict (R-1108-144): the batch's exit code IS the verdict — a batch
  # that found divergences (or failed outright) must not report success
  # to the continuous track's scheduler. Minimize first (archive the
  # findings), then propagate the batch's code.
  batch_rc=0
  run_batch "$SEEDS" "$OUT/batch-0" || batch_rc=$?
  minimize_findings "$OUT/batch-0"
  echo "batch done: $OUT/batch-0 (run_batch rc=$batch_rc)"
  exit "$batch_rc"
fi

[ -n "$HOURS" ] || { echo "need --hours or --seeds" >&2; exit 2; }

# Overnight soak: batches of BATCH seeds until the deadline.
seed=0
batch=0
failed_batches=0
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
  echo "== soak batch $batch: seeds $seed:$((seed + BATCH)) =="
  # Verdict (R-1108-144): a failed batch is RECORDED (failed_batches),
  # never swallowed — but the loop continues so later batches still run
  # and the final report covers the whole soak. The failure surfaces at
  # the end via check_soak_report.py, not here.
  batch_failed=0
  if ! run_batch "$seed:$((seed + BATCH))" "$OUT/batch-$batch"; then
    echo "batch $batch failed; continuing with next batch"
    batch_failed=1
  fi
  failed_batches=$((failed_batches + batch_failed))
  minimize_findings "$OUT/batch-$batch" || true
  seed=$((seed + BATCH))
  batch=$((batch + 1))
done

echo "== soak complete: $batch batches, $seed programs, $failed_batches failed =="
python3 - "$OUT" "$failed_batches" <<'EOF'
import glob, json, os, sys
out = sys.argv[1]
failed_batches = int(sys.argv[2])
total_div = 0
total_programs = 0
for res in glob.glob(os.path.join(out, "batch-*", "results.jsonl")):
    for line in open(res):
        r = json.loads(line)
        total_programs += 1
        if r.get("result") not in ("PASS", "SCOPE", "GC_BUILD_FAIL"):
            total_div += 1
rep = {"batches": len(glob.glob(os.path.join(out, "batch-*"))),
       "programs_run": total_programs,
       "failed_batches": failed_batches,
       "diverged_total": total_div}
json.dump(rep, open(os.path.join(out, "soak-report.json"), "w"), indent=2)
print(f"soak report: {rep}")
EOF
# Verdict (R-1108-144): the single verdict owner for the overnight soak.
# check_soak_report.py exits 0 only for a clean soak; anything else —
# zero batches, zero programs, failed batches, divergences, or a missing
# report — fails the script, and the workflow gates on the same script.
# (Boundary with R-01: the zero-programs fail-loudly is shared with the
# R-01 lane's staged fix; if R-01's 7 files are applied after this,
# re-derive this finale instead of blind-copying — see lane README.)
python3 "$HERE/harness/check_soak_report.py" "$OUT/soak-report.json"
