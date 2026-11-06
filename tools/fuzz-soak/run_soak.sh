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
# TIME BUDGET: the caller (the nightly GHA job, whose 360 min timeout is
# the GitHub-hosted ceiling) reserves time around the soak for setup,
# Layer 4b and upload, so HOURS must already fit that budget — the
# workflow validates it. Within the soak, a new batch is started only if
# at least SOAK_BATCH_MARGIN_SECONDS remain before the deadline: a batch
# started at the deadline would run to completion past it (batch +
# minimize), overrunning into whatever follows the soak. The margin
# bounds that overrun to one batch duration; it does not guarantee the
# batch finishes inside it, but the common case (a batch of minutes)
# does. Do not raise HOURS to compensate for the margin.
#
# This is the OVERNIGHT slot. The CONTINUOUS adversarial track reserves the
# same interface: `run_soak.sh --seeds A:B` runs one bounded batch (used by
# the continuous track's scheduler); the scheduled GHA workflow
# (.github/workflows/fuzz-soak.yml) runs the overnight soak per release.
#
# Usage:
#   run_soak.sh --hours H --out DIR     overnight soak, batches until H
#                                       hours elapse (no new batch starts
#                                       within SOAK_BATCH_MARGIN_SECONDS
#                                       of the deadline)
#   run_soak.sh --seeds A:B --out DIR   single bounded batch (continuous slot)
#
# Environment:
#   GR           repo root (default: derived from this file's location)
#   GO_BIN_DIR   Go toolchain bin dir (default: $HOME/tools/go/bin)
#   JAVA_BIN     java binary (default: $HOME/tools/graalvm-jdk-25/bin/java)
#   JOBS         parallel diffs per batch (default: 4)
#   BATCH        seeds per batch (default: 50)
#   SOAK_BATCH_MARGIN_SECONDS
#                stop starting new batches this many seconds before the
#                deadline so the last batch + minimize finishes inside
#                the soak window (default: 1200 = 20 min; keep in sync
#                with the SOAK_RESERVE_MINUTES in
#                .github/workflows/fuzz-soak.yml)
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
# No new batch starts within this of the deadline (see header).
BATCH_MARGIN_SECONDS="${SOAK_BATCH_MARGIN_SECONDS:-1200}"

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
# Fail fast on non-integer budgets, before DEADLINE arithmetic turns a
# typo into an obscure "unbound variable" error.
case "$HOURS" in
  '') :;;
  *[!0-9]*) echo "--hours must be a non-negative integer, got '$HOURS'" >&2; exit 2;;
esac
case "$BATCH_MARGIN_SECONDS" in
  ''|*[!0-9]*) echo "SOAK_BATCH_MARGIN_SECONDS must be a non-negative integer, got '$BATCH_MARGIN_SECONDS'" >&2; exit 2;;
esac

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
  run_batch "$SEEDS" "$OUT/batch-0"
  minimize_findings "$OUT/batch-0"
  echo "batch done: $OUT/batch-0"
  exit 0
fi

[ -n "$HOURS" ] || { echo "need --hours or --seeds" >&2; exit 2; }

# Overnight soak: batches of BATCH seeds until the deadline. A batch is
# started only if at least BATCH_MARGIN_SECONDS remain before the
# deadline, so the final batch + minimize has room to finish inside the
# soak window instead of overrunning into the next job stage.
seed=0
batch=0
while [ "$(( $(date +%s) + BATCH_MARGIN_SECONDS ))" -lt "$DEADLINE" ]; do
  echo "== soak batch $batch: seeds $seed:$((seed + BATCH)) =="
  if ! run_batch "$seed:$((seed + BATCH))" "$OUT/batch-$batch"; then
    echo "batch $batch failed; continuing with next batch"
  fi
  minimize_findings "$OUT/batch-$batch" || true
  seed=$((seed + BATCH))
  batch=$((batch + 1))
done

echo "== soak complete: $batch batches, $seed programs =="
python3 - "$OUT" <<'EOF'
import glob, json, os, sys
out = sys.argv[1]
total_div = 0
for res in glob.glob(os.path.join(out, "batch-*", "results.jsonl")):
    for line in open(res):
        r = json.loads(line)
        if r.get("result") not in ("PASS", "SCOPE", "GC_BUILD_FAIL"):
            total_div += 1
rep = {"batches": len(glob.glob(os.path.join(out, "batch-*"))),
       "diverged_total": total_div}
json.dump(rep, open(os.path.join(out, "soak-report.json"), "w"), indent=2)
print(f"soak report: {rep}")
EOF
