#!/usr/bin/env python3
"""GHA shard driver for graalgo-fuzz.

Shard contract: honors $SHARD_IDX / $SHARD_COUNT / $OUT_DIR.
Each shard generates N programs from a disjoint seed range, runs the
differential, and writes $OUT_DIR/results.jsonl.

Env:
  SHARD_IDX, SHARD_COUNT : shard identity
  OUT_DIR                : output directory
  SEEDS_PER_SHARD        : programs per shard (default 50)
  GRAALGO_HOME           : repo root in the image (/opt/graalgo default);
                           the fuzz harness lives at
                           $GRAALGO_HOME/tools/fuzz-soak (Phase-4 tree;
                           the pre-Phase-4 layout was deleted
                           2026-10-14 — the image tarball must be built
                           from the current tree, see ../Dockerfile
                           and ../README.md)

Golden canary: seed 999999 is a known-pass program; if it doesn't PASS,
the shard writes CANARY_FAIL and exits nonzero (batch quarantined).

Exit codes (same 0/1/2 contract as harness/run_batch.py):
  0  every program PASSed or was SCOPE-rejected (clean shard)
  1  the shard ran, but programs diverged (WRONG_OUTPUT, CRASH, HANG,
     EXIT_MISMATCH, GO_PANIC_MISMATCH, GC_BUILD_FAIL)
  2  the shard itself could not run correctly: canary failure,
     generator failure, any program HARNESS_ERROR, or zero programs
     ran (SEEDS_PER_SHARD <= 0). A shard that ran nothing must never
     report success — "0 divergences" is only meaningful with
     programs_run > 0 (see shard-summary.json).
"""
import json
import os
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor

GRAALGO_HOME = os.environ.get("GRAALGO_HOME", "/opt/graalgo")
FUZZ = os.path.join(GRAALGO_HOME, "tools", "fuzz-soak")
OUT_DIR = os.environ.get("OUT_DIR", "/out")
SHARD_IDX = int(os.environ.get("SHARD_IDX", "0"))
SHARD_COUNT = int(os.environ.get("SHARD_COUNT", "1"))
SEEDS_PER_SHARD = int(os.environ.get("SEEDS_PER_SHARD", "50"))

# Canary: must PASS. (A tiny fixed program exercising int arithmetic.)
CANARY_SEED = 999999


def sh(cmd, timeout=300):
    return subprocess.run(cmd, shell=True, capture_output=True, text=True,
                          timeout=timeout)


def diff_one(gofile, workdir, gmp=1):
    r = sh(f"bash {FUZZ}/harness/diff_one.sh {gofile} {workdir} {gmp}",
           timeout=300)
    try:
        return json.loads(r.stdout.strip().split("\n")[-1])
    except Exception:
        return {"file": os.path.basename(gofile), "result": "HARNESS_ERROR",
                "detail": (r.stdout + r.stderr)[-500:]}


def summarize_results(results_path):
    """Count results.jsonl records by result.

    Returns (counts, programs_run). A missing file counts as zero
    programs ran (the caller fails loudly on that); a malformed line
    counts as HARNESS_ERROR — an unusable record is a harness failure,
    not a clean program.
    """
    from collections import Counter
    counts = Counter()
    if not os.path.isfile(results_path):
        return counts, 0
    with open(results_path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                counts[json.loads(line).get("result", "HARNESS_ERROR")] += 1
            except Exception:
                counts["HARNESS_ERROR"] += 1
    return counts, sum(counts.values())


def main():
    # Fail fast on an image built from the old layout: without the
    # generator and diff_one.sh every seed would fail identically, and
    # the per-seed errors would hide the real cause (a stale tarball).
    for rel in ("gen/generator.py", "harness/diff_one.sh"):
        if not os.path.isfile(os.path.join(FUZZ, rel)):
            print(f"FUZZ layout mismatch: {os.path.join(FUZZ, rel)} "
                  "missing — rebuild the graalgo-slim tarball from the "
                  "current tree (see ../README.md)")
            sys.exit(2)
    os.makedirs(OUT_DIR, exist_ok=True)
    # Shard i owns seeds [i*N, (i+1)*N) offset by a base to avoid overlap
    # with local runs (local uses 0..; GHA uses 1_000_000..).
    base = 1_000_000 + SHARD_IDX * SEEDS_PER_SHARD
    seeds = list(range(base, base + SEEDS_PER_SHARD))
    if not seeds:
        # Zero programs is a shard that could not run, not a clean
        # shard: SEEDS_PER_SHARD <= 0 would otherwise run the canary
        # only, compare nothing, and exit 0 — a vacuous pass in the
        # layer-4 gate (same family as the run_batch.py empty-range
        # guard). Fail loudly with exit 2 before generating anything.
        print(f"ERROR: SEEDS_PER_SHARD={SEEDS_PER_SHARD}: no programs "
              "to run", flush=True)
        sys.exit(2)
    progdir = os.path.join(OUT_DIR, "programs")
    os.makedirs(progdir, exist_ok=True)

    print(f"shard {SHARD_IDX}/{SHARD_COUNT}: seeds {base}..{base + SEEDS_PER_SHARD - 1}",
          flush=True)

    # Canary first.
    canary_go = os.path.join(progdir, "canary.go")
    r = sh(f"python3 {FUZZ}/gen/generator.py --seed {CANARY_SEED} --out {canary_go}")
    if r.returncode != 0:
        print("CANARY_FAIL: generator error")
        sys.exit(2)
    crec = diff_one(canary_go, os.path.join(OUT_DIR, "work_canary"))
    print(f"canary: {crec}", flush=True)
    if crec["result"] != "PASS":
        with open(os.path.join(OUT_DIR, "CANARY_FAIL"), "w") as f:
            f.write(json.dumps(crec))
        print("CANARY_FAIL: known-pass program did not PASS — quarantining shard")
        sys.exit(2)

    # Main batch.
    for s in seeds:
        out = os.path.join(progdir, f"fuzz_{s}.go")
        r = sh(f"python3 {FUZZ}/gen/generator.py --seed {s} --out {out}")
        if r.returncode != 0:
            print(f"generator failed for seed {s}")
            # Exit 2: infrastructure failure, not a divergence finding
            # (same contract as run_batch.py; the canary generator
            # failure above already exits 2).
            sys.exit(2)

    results_path = os.path.join(OUT_DIR, "results.jsonl")
    with open(results_path, "w") as rf:
        def run_one(s):
            workdir = os.path.join(OUT_DIR, "work", f"seed_{s}")
            os.makedirs(workdir, exist_ok=True)
            gofile = os.path.join(progdir, f"fuzz_{s}.go")
            rec = diff_one(gofile, workdir)
            rec["seed"] = s
            rec["shard"] = SHARD_IDX
            return rec

        with ThreadPoolExecutor(max_workers=4) as ex:
            for i, rec in enumerate(ex.map(run_one, seeds)):
                rf.write(json.dumps(rec) + "\n")
                rf.flush()
                if (i + 1) % 10 == 0:
                    print(f"  {i+1}/{len(seeds)}", flush=True)

    # Verdict: a shard that found divergences must fail, not print
    # "shard done" and exit 0. HARNESS_ERROR is infrastructure failure
    # (exit 2), not a finding (exit 1); zero programs ran is likewise
    # exit 2. shard-summary.json records programs_run so "0
    # divergences" is distinguishable from "0 programs ran".
    counts, programs_run = summarize_results(results_path)
    diverged = sum(v for k, v in counts.items()
                   if k not in ("PASS", "SCOPE"))
    summary = {"shard": SHARD_IDX,
               "programs_run": programs_run,
               "counts": dict(sorted(counts.items())),
               "diverged": diverged,
               "harness_errors": counts.get("HARNESS_ERROR", 0)}
    with open(os.path.join(OUT_DIR, "shard-summary.json"), "w") as f:
        json.dump(summary, f, indent=2)
    print(f"shard summary: {summary}", flush=True)
    if programs_run == 0:
        print("SHARD FAILED: zero programs ran — see shard-summary.json",
              flush=True)
        sys.exit(2)
    if counts.get("HARNESS_ERROR", 0):
        print("SHARD FAILED: HARNESS_ERROR present — shard "
              "infrastructure failed", flush=True)
        sys.exit(2)
    if diverged:
        print(f"SHARD DIVERGENT: {diverged} diverged program(s) — "
              "see shard-summary.json", flush=True)
        sys.exit(1)

    print("shard done", flush=True)


if __name__ == "__main__":
    main()
