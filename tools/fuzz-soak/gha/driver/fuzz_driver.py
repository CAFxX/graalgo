#!/usr/bin/env python3
"""GHA shard driver for graalgo-fuzz.

Shard contract: honors $SHARD_IDX / $SHARD_COUNT / $OUT_DIR.
Each shard generates N programs from a disjoint seed range, runs the
differential, and writes $OUT_DIR/results.jsonl.

Env:
  SHARD_IDX, SHARD_COUNT : shard identity
  OUT_DIR                : output directory
  SEEDS_PER_SHARD        : programs per shard (default 50)
  GRAALGO_HOME           : /opt/graalgo (image default)

Golden canary: seed 999999 is a known-pass program; if it doesn't PASS,
the shard writes CANARY_FAIL and exits nonzero (batch quarantined).
"""
import json
import os
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor

GRAALGO_HOME = os.environ.get("GRAALGO_HOME", "/opt/graalgo")
FUZZ = os.path.join(GRAALGO_HOME, "phases/phase3/fuzzing")
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


def main():
    os.makedirs(OUT_DIR, exist_ok=True)
    # Shard i owns seeds [i*N, (i+1)*N) offset by a base to avoid overlap
    # with local runs (local uses 0..; GHA uses 1_000_000..).
    base = 1_000_000 + SHARD_IDX * SEEDS_PER_SHARD
    seeds = list(range(base, base + SEEDS_PER_SHARD))
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
            sys.exit(1)

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

    print("shard done", flush=True)


if __name__ == "__main__":
    main()
