#!/usr/bin/env bash
# Layer-4 fuzz soak runner (tools/fuzz-soak/DESIGN.md, GG-0037):
# generative differential fuzzing, overnight per release.
#
# Runs the seeded program generator + differential harness
# (harness/run_batch.py) in time-bounded batches until HOURS elapse, then
# minimizes any divergences (harness/triage.py) and archives the findings.
# Minimized divergences become permanent regressions (findings are filed
# under tools/fuzz-soak/findings/ with a minimized reproducer).
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
# Repo root: tools/fuzz-soak/run_soak.sh -> tools -> repo.
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
  run_batch "$SEEDS" "$OUT/batch-0"
  minimize_findings "$OUT/batch-0"
  echo "batch done: $OUT/batch-0"
  exit 0
fi

[ -n "$HOURS" ] || { echo "need --hours or --seeds" >&2; exit 2; }

# Overnight soak: batches of BATCH seeds until the deadline.
seed=0
batch=0
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
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
