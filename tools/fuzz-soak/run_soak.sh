#!/usr/bin/env bash
# Layer-4 fuzz soak runner (GG-0037): generative differential fuzzing,
# overnight per release.
#
# Runs the seeded program generator + differential harness
# (harness/run_batch.py) in time-bounded batches until HOURS elapse, then
# minimizes any divergences (harness/triage.py) and archives the findings.
# Minimized divergences become permanent regressions per SPEC §5.4 (filed
# under tools/fuzz-soak/findings/ with a minimized reproducer).
#
# HONEST-GATE CONTRACT: the soak exits NON-ZERO when zero batches produced
# a results.jsonl — "the harness ran nothing" must never look like
# "the harness found nothing". Divergences themselves are findings, not
# gate failures (exit 0); only an empty soak fails.
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
#   GRAALVM_HOME GraalVM root (default: derived from JAVA_BIN); the
#                graalgo-sync-run launcher wrapper uses this to pick its JVM
#   JOBS         parallel diffs per batch (default: 4)
#   BATCH        seeds per batch (default: 50)
set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# Repo root: tools/fuzz-soak/run_soak.sh -> tools -> repo.
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
export GR="${GR:-$REPO_ROOT}"
export GO_BIN_DIR="${GO_BIN_DIR:-$HOME/tools/go/bin}"
JAVA_BIN="${JAVA_BIN:-$HOME/tools/graalvm-jdk-25/bin/java}"
export JAVA_BIN
if [ -z "${GRAALVM_HOME:-}" ]; then
  export GRAALVM_HOME="$(cd "$(dirname "$JAVA_BIN")/.." && pwd)"
fi
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
    min_out = os.path.join(out, r["file"] + ".min.go")
    p = subprocess.run(
        ["python3", f"{here}/harness/triage.py", "--prog", prog,
         "--out", min_out],
        capture_output=True, text=True, timeout=1200)
    print(f"{r['file']}: triage rc={p.returncode} -> {min_out}")
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
# Counters for the honest-gate contract: a soak that produced zero batch
# results must exit non-zero (see header). Assignment-form arithmetic only:
# ((n++)) under `set -e` kills the script when the old value is 0.
seed=0
batch=0
batches_failed=0
batches_with_results=0
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
  echo "== soak batch $batch: seeds $seed:$((seed + BATCH)) =="
  if ! run_batch "$seed:$((seed + BATCH))" "$OUT/batch-$batch"; then
    echo "batch $batch failed; continuing with next batch"
    batches_failed=$((batches_failed + 1))
  elif [ -f "$OUT/batch-$batch/results.jsonl" ]; then
    batches_with_results=$((batches_with_results + 1))
  fi
  minimize_findings "$OUT/batch-$batch" || true
  seed=$((seed + BATCH))
  batch=$((batch + 1))
done

echo "== soak complete: $batch batches attempted, $batches_failed failed, $batches_with_results with results, $seed programs =="
python3 - "$OUT" "$batch" "$batches_failed" "$batches_with_results" <<'EOF'
import glob, json, os, sys
out, attempted, failed, with_results = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), int(sys.argv[4])
total_div = 0
for res in glob.glob(os.path.join(out, "batch-*", "results.jsonl")):
    for line in open(res):
        r = json.loads(line)
        if r.get("result") not in ("PASS", "SCOPE", "GC_BUILD_FAIL"):
            total_div += 1
rep = {"batches": attempted,
       "batches_attempted": attempted,
       "batches_failed": failed,
       "batches_with_results": with_results,
       "diverged_total": total_div}
json.dump(rep, open(os.path.join(out, "soak-report.json"), "w"), indent=2)
print(f"soak report: {rep}")
EOF

# Dead-gate tripwire: zero batches produced results.jsonl means the harness
# ran nothing — that is a gate failure, never a green soak.
if [ "$batches_with_results" -eq 0 ]; then
  echo "SOAK FAILED: 0/$batch batches produced results.jsonl (see $OUT/soak-report.json)" >&2
  exit 2
fi
echo "soak done: $batches_with_results/$batch batches produced results"
