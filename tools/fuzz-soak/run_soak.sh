#!/usr/bin/env bash
# Layer-4 fuzz soak runner (tools/fuzz-soak/DESIGN.md layer 4):
# generative differential fuzzing, overnight per release.
#
# Runs the seeded program generator + differential harness
# (harness/run_batch.py) in time-bounded batches until HOURS elapse, then
# minimizes any divergences (harness/triage.py) and archives the findings.
# Minimized divergences become permanent regressions (filed under
# tools/fuzz-soak/findings/ with a minimized reproducer).
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
  # recorded, never silently dropped. This function EXITS NON-ZERO if any
  # minimization fails, so callers must not swallow it with `|| true`:
  # a failed minimization means a divergence has no reproducer on file.
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
workdir = os.path.join(batchdir, "triage-work")
failed = 0
for r in diverged:
    prog = os.path.join(batchdir, "programs", r["file"] + ".go")
    if not os.path.exists(prog):
        print(f"missing program for {r['file']}; minimization failed")
        failed += 1
        continue
    p = subprocess.run(
        ["python3", f"{here}/harness/triage.py",
         "--prog", prog, "--out", os.path.join(out, r["file"] + ".go"),
         "--workdir", workdir],
        capture_output=True, text=True, timeout=1200)
    print(f"{r['file']}: triage rc={p.returncode}")
    if p.returncode != 0:
        print(p.stderr[-500:])
        failed += 1
if failed:
    print(f"minimization failed for {failed}/{len(diverged)} diverged programs")
    sys.exit(1)
EOF
}

if [ -n "$SEEDS" ]; then
  # Single bounded batch: the continuous-track slot. Minimize first, then
  # fail: a diverged batch must not exit 0, and a failed minimization must
  # not be hidden behind a batch verdict either way.
  echo "== soak batch: seeds $SEEDS =="
  batch_rc=0
  run_batch "$SEEDS" "$OUT/batch-0" || batch_rc=$?
  minimize_rc=0
  minimize_findings "$OUT/batch-0" || minimize_rc=$?
  echo "batch done: $OUT/batch-0 (batch_rc=$batch_rc minimize_rc=$minimize_rc)"
  if [ "$minimize_rc" -ne 0 ]; then
    echo "SOAK RED: minimization failed" >&2
    exit 1
  fi
  if [ "$batch_rc" -ne 0 ]; then
    echo "SOAK RED: divergences in batch (see $OUT/batch-0/results.jsonl)" >&2
    exit 1
  fi
  exit 0
fi

[ -n "$HOURS" ] || { echo "need --hours or --seeds" >&2; exit 2; }

# Overnight soak: batches of BATCH seeds until the deadline.
# A failed batch does not stop the soak (keep sampling), but it is
# remembered: the final verdict below fails on any diverged program,
# any failed minimization, or zero programs sampled. The programs count
# is what closes the all-batches-failed-at-build hole: a build failure
# leaves a batch dir but no results.jsonl, so batch-count alone would
# have stayed green over a night that sampled nothing.
seed=0
batch=0
minimize_failed=0
failed_batches=0
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
  echo "== soak batch $batch: seeds $seed:$((seed + BATCH)) =="
  if ! run_batch "$seed:$((seed + BATCH))" "$OUT/batch-$batch"; then
    echo "batch $batch failed; continuing with next batch"
    failed_batches=$((failed_batches + 1))
  fi
  if ! minimize_findings "$OUT/batch-$batch"; then
    echo "batch $batch minimization failed; continuing with next batch"
    minimize_failed=1
  fi
  seed=$((seed + BATCH))
  batch=$((batch + 1))
done

echo "== soak complete: $batch batches, $seed programs attempted =="
python3 - "$OUT" "$minimize_failed" "$failed_batches" <<'EOF'
import glob, json, os, sys
out, minimize_failed, failed_batches = sys.argv[1], sys.argv[2] == "1", int(sys.argv[3])
total_div = 0
total_programs = 0
for res in glob.glob(os.path.join(out, "batch-*", "results.jsonl")):
    for line in open(res):
        if not line.strip():
            continue
        total_programs += 1
        r = json.loads(line)
        if r.get("result") not in ("PASS", "SCOPE", "GC_BUILD_FAIL"):
            total_div += 1
rep = {"batches": len(glob.glob(os.path.join(out, "batch-*"))),
       "programs": total_programs,
       "failed_batches": failed_batches,
       "diverged_total": total_div,
       "minimize_failed": minimize_failed}
json.dump(rep, open(os.path.join(out, "soak-report.json"), "w"), indent=2)
print(f"soak report: {rep}")
EOF
diverged_total=$(python3 -c "import json; print(json.load(open('$OUT/soak-report.json'))['diverged_total'])")
programs_total=$(python3 -c "import json; print(json.load(open('$OUT/soak-report.json'))['programs'])")
echo "soak verdict: $diverged_total diverged, $programs_total programs, minimize_failed=$minimize_failed, failed_batches=$failed_batches, batches=$batch"
# Single source of truth for the verdict: harness/check_soak_report.py
# (also used by the workflow verdict step). It fails on zero batches,
# zero programs sampled, divergences, or failed minimization, and on a
# missing/malformed report.
if ! python3 "$HERE/harness/check_soak_report.py" "$OUT/soak-report.json"; then
  exit 1
fi
echo "SOAK GREEN: zero divergences across $batch batches ($programs_total programs)"
