#!/usr/bin/env bash
# Layer-4 fuzz soak runner (phase-3 plan PLAN.md §5 layer 4, GG-0037):
# generative differential fuzzing, overnight per release.
#
# Runs the seeded program generator + differential harness
# (harness/run_batch.py) in time-bounded batches until HOURS elapse, then
# minimizes any divergences (harness/triage.py) and archives the findings.
# Minimized divergences become permanent regressions per SPEC §5.4 (filed
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
# Repo root: tools/fuzz-soak/run_soak.sh -> tools/fuzz-soak -> tools -> repo.
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
    # ${2:?...} guards: with set -u, a missing value must fail with a
    # clear message, not an "unbound variable" abort (T17).
    --hours) HOURS="${2:---hours requires a value}"; shift 2;;
    --seeds) SEEDS="${2:---seeds requires a value}"; shift 2;;
    --out) OUT="${2:---out requires a value}"; shift 2;;
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
  # triage.py minimizes each diverged program. A divergence that fails
  # to minimize is a LOUD batch failure (nonzero exit) — never silently
  # dropped (T4 fix: the old positional invocation always exited 2
  # "unrecognized arguments" and the failure was swallowed).
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
triage_work = os.path.join(batchdir, "triage-work")
os.makedirs(triage_work, exist_ok=True)
failed = []
for r in diverged:
    prog = os.path.join(batchdir, "programs", r["file"] + ".go")
    if not os.path.exists(prog):
        print(f"missing program for {r['file']}; skipping")
        failed.append(r["file"])
        continue
    p = subprocess.run(
        ["python3", f"{here}/harness/triage.py",
         "--prog", prog, "--out", os.path.join(out, r["file"] + ".min.go"),
         "--workdir", triage_work],
        capture_output=True, text=True, timeout=1200)
    print(f"{r['file']}: triage rc={p.returncode}")
    if p.returncode != 0:
        print(p.stdout[-500:])
        print(p.stderr[-500:])
        failed.append(r["file"])
if failed:
    print(f"MINIMIZATION FAILED for {len(failed)} diverged program(s): "
          + ", ".join(failed))
    sys.exit(1)
print("all divergences minimized")
EOF
}

if [ -n "$SEEDS" ]; then
  # Single bounded batch: the continuous-track slot.
  echo "== soak batch: seeds $SEEDS =="
  run_batch "$SEEDS" "$OUT/batch-0"
  if ! minimize_findings "$OUT/batch-0"; then
    echo "MINIMIZATION FAILED for batch-0 (see above)" >&2
    touch "$OUT/batch-0/MINIMIZE_FAILED"
    exit 1
  fi
  echo "batch done: $OUT/batch-0"
  exit 0
fi

[ -n "$HOURS" ] || { echo "need --hours or --seeds" >&2; exit 2; }

# Consecutive build-failure abort (T3 fix): run_batch.py exits 3 on
# build failure. N failing builds in a row means the harness is broken,
# not the code — abort instead of spinning failing batches to the deadline.
MAX_CONSEC_BUILD_FAILS="${SOAK_MAX_CONSEC_FAILS:-3}"

# Overnight soak: batches of BATCH seeds until the deadline.
seed=0
batch=0
consec_build_fails=0
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
  echo "== soak batch $batch: seeds $seed:$((seed + BATCH)) =="
  if ! run_batch "$seed:$((seed + BATCH))" "$OUT/batch-$batch"; then
    rc=$?
    if [ "$rc" -eq 3 ]; then
      consec_build_fails=$((consec_build_fails + 1))
      echo "batch $batch: BUILD FAILED ($consec_build_fails/$MAX_CONSEC_BUILD_FAILS consecutive)" >&2
      if [ "$consec_build_fails" -ge "$MAX_CONSEC_BUILD_FAILS" ]; then
        echo "ABORTING soak: $MAX_CONSEC_BUILD_FAILS consecutive build failures" >&2
        exit 1
      fi
    else
      consec_build_fails=0
      echo "batch $batch failed (rc=$rc); continuing with next batch"
    fi
  else
    consec_build_fails=0
  fi
  if ! minimize_findings "$OUT/batch-$batch"; then
    echo "MINIMIZATION FAILED for batch $batch (see above)" >&2
    touch "$OUT/batch-$batch/MINIMIZE_FAILED"
  fi
  seed=$((seed + BATCH))
  batch=$((batch + 1))
done

echo "== soak complete: $batch batches, $seed programs =="
python3 - "$OUT" <<'EOF'
import glob, json, os, sys
out = sys.argv[1]
total_div = 0
min_fail_batches = []
for res in glob.glob(os.path.join(out, "batch-*", "results.jsonl")):
    for line in open(res):
        r = json.loads(line)
        if r.get("result") not in ("PASS", "SCOPE", "GC_BUILD_FAIL"):
            total_div += 1
for marker in glob.glob(os.path.join(out, "batch-*", "MINIMIZE_FAILED")):
    min_fail_batches.append(os.path.basename(os.path.dirname(marker)))
rep = {"batches": len(glob.glob(os.path.join(out, "batch-*"))),
       "diverged_total": total_div,
       "minimize_failed_batches": sorted(min_fail_batches)}
json.dump(rep, open(os.path.join(out, "soak-report.json"), "w"), indent=2)
print(f"soak report: {rep}")
if min_fail_batches:
    print(f"WARNING: minimization failed in batches: {sorted(min_fail_batches)}")
    sys.exit(1)
EOF
