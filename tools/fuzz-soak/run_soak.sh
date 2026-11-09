#!/usr/bin/env bash
# Layer-4 fuzz soak runner (layer 4, GG-0037; see DESIGN.md):
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
#
# FAILURE SEMANTICS (R-P1-13): a batch failure is never swallowed. A
# batch verdict comes from run_batch.py: 0 clean, 1 divergences found,
# 2 the batch could not run (build/generator/harness failure). The
# overnight loop records every non-clean batch and keeps going to
# gather signal, but the soak ends LOUDLY: a SOAK_FAILED marker file
# in $OUT, a summary on stdout, and a nonzero exit code (2 if any
# batch failed to run, else 1 if any batch found divergences). The
# nightly GHA workflow step fails on that exit code, so a red soak
# cannot pass as green. The single-batch slot (--seeds) propagates
# the batch verdict as its own exit code after minimizing.
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
  # recorded, never silently dropped. Exits nonzero if any program
  # could not be minimized (or its program file is missing), so the
  # caller can record the triage failure in the soak report.
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
failed = 0
for r in diverged:
    prog = os.path.join(batchdir, "programs", r["file"] + ".go")
    if not os.path.exists(prog):
        print(f"missing program for {r['file']}; skipping")
        failed += 1
        continue
    p = subprocess.run(
        ["python3", f"{here}/harness/triage.py", prog, "--out", out],
        capture_output=True, text=True, timeout=1200)
    print(f"{r['file']}: triage rc={p.returncode}")
    if p.returncode != 0:
        print(p.stderr[-500:])
        failed += 1
sys.exit(1 if failed else 0)
EOF
}

if [ -n "$SEEDS" ]; then
  # Single bounded batch: the continuous-track slot.
  echo "== soak batch: seeds $SEEDS =="
  # Capture run_batch's verdict (0 clean, 1 findings, 2 infra failure)
  # instead of letting `set -e` abort before minimize_findings runs:
  # a divergent batch must still be minimized, and the verdict must
  # still propagate as this script's exit code.
  batch_rc=0
  run_batch "$SEEDS" "$OUT/batch-0" || batch_rc=$?
  minimize_rc=0
  minimize_findings "$OUT/batch-0" || minimize_rc=$?
  if [ "$batch_rc" -ne 0 ]; then
    echo "batch verdict rc=$batch_rc (triage rc=$minimize_rc): $OUT/batch-0"
    exit "$batch_rc"
  fi
  if [ "$minimize_rc" -ne 0 ]; then
    echo "batch clean but minimization failed (triage rc=$minimize_rc)" >&2
    exit "$minimize_rc"
  fi
  echo "batch done: $OUT/batch-0"
  exit 0
fi

[ -n "$HOURS" ] || { echo "need --hours or --seeds" >&2; exit 2; }

# Overnight soak: batches of BATCH seeds until the deadline. A failed
# batch never ends the soak silently: infrastructure failures (rc=2)
# and divergence findings (rc=1) are recorded per batch, the soak
# continues to gather signal, and the final verdict is loud — a
# SOAK_FAILED marker, a summary on stdout, and a nonzero exit code.
seed=0
batch=0
failed_batches=""
divergent_batches=""
triage_failed_batches=""
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
  echo "== soak batch $batch: seeds $seed:$((seed + BATCH)) =="
  batch_rc=0
  run_batch "$seed:$((seed + BATCH))" "$OUT/batch-$batch" || batch_rc=$?
  case "$batch_rc" in
    0) ;;
    1)
      divergent_batches="$divergent_batches $batch"
      echo "LOUD: batch $batch found divergences (run_batch rc=1); continuing with next batch"
      ;;
    *)
      failed_batches="$failed_batches $batch"
      echo "LOUD: batch $batch FAILED to run (run_batch rc=$batch_rc); continuing with next batch"
      ;;
  esac
  if ! minimize_findings "$OUT/batch-$batch"; then
    triage_failed_batches="$triage_failed_batches $batch"
    echo "LOUD: batch $batch minimization failed; continuing with next batch"
  fi
  seed=$((seed + BATCH))
  batch=$((batch + 1))
done

echo "== soak complete: $batch batches, $seed programs =="
python3 - "$OUT" "$failed_batches" "$divergent_batches" "$triage_failed_batches" <<'EOF'
import glob, json, os, sys
out = sys.argv[1]
failed = sys.argv[2].split()
divergent = sys.argv[3].split()
triage_failed = sys.argv[4].split()
total_div = 0
programs_run = 0
for res in glob.glob(os.path.join(out, "batch-*", "results.jsonl")):
    for line in open(res):
        r = json.loads(line)
        programs_run += 1
        if r.get("result") not in ("PASS", "SCOPE", "GC_BUILD_FAIL"):
            total_div += 1
# programs_run / batches_failed make "0 divergences" distinguishable
# from "0 programs ran": a clean soak reports diverged_total 0 with
# programs_run > 0; a soak that ran nothing reports programs_run 0
# (and its batches land in failed_batches with exit 2, never 0).
rep = {"batches": len(glob.glob(os.path.join(out, "batch-*"))),
       "programs_run": programs_run,
       "diverged_total": total_div,
       "batches_failed": len(failed),
       "failed_batches": [int(b) for b in failed],
       "divergent_batches": [int(b) for b in divergent],
       "triage_failed_batches": [int(b) for b in triage_failed]}
json.dump(rep, open(os.path.join(out, "soak-report.json"), "w"), indent=2)
print(f"soak report: {rep}")
# Fail loudly: a SOAK_FAILED marker file plus a nonzero exit code.
# The GHA workflow step fails on the exit code; the marker survives
# in the uploaded artifact for the `if: always()` upload step.
if failed:
    with open(os.path.join(out, "SOAK_FAILED"), "w") as f:
        f.write("infrastructure failure in batches:"
                f" {' '.join(failed)}\n")
    print(f"SOAK FAILED: batches {failed} failed to run (see SOAK_FAILED)")
    sys.exit(2)
if divergent:
    with open(os.path.join(out, "SOAK_FAILED"), "w") as f:
        f.write(f"divergences found in batches: {' '.join(divergent)}\n")
    print(f"SOAK FAILED: batches {divergent} found divergences"
          " (see SOAK_FAILED)")
    sys.exit(1)
if triage_failed:
    print(f"WARNING: minimization failed in batches {triage_failed}"
          " (findings kept in batch dirs)")
print("SOAK CLEAN: no failed or divergent batches")
EOF
