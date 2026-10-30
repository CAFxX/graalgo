#!/usr/bin/env python3
"""Batch differential fuzzing driver.

1. Builds graalgo Java modules once.
2. Generates N programs (seed range).
3. Runs diff_one.sh in parallel.
4. Classifies, dedups, writes results.jsonl + summary.

Usage: run_batch.py --seeds START:END --out DIR [--jobs J] [--gmp N]
"""
import argparse
import hashlib
import json
import os
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor

# Repo root: harness/run_batch.py -> fuzzing -> phase3 -> phases -> repo.
GR = os.environ.get("GR") or os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))
FUZZ = os.path.join(GR, "phases/phase3/fuzzing")


def sh(cmd, **kw):
    return subprocess.run(cmd, shell=True, capture_output=True, text=True, **kw)


def batch_exit_code(counts, n_results):
    """Gate verdict for a batch: 0 only if every result is PASS/SCOPE.

    ANY other result class fails the batch — GC_BUILD_FAIL, WRONG_OUTPUT,
    CRASH, HANG, EXIT_MISMATCH, HARNESS_ERROR, and any future divergence
    class. An empty batch (zero programs ran) also fails: a green verdict
    over zero programs proves nothing.
    """
    if n_results == 0:
        return 1
    return 0 if all(k in ("PASS", "SCOPE") for k in counts) else 1


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--seeds", required=True, help="START:END (e.g. 0:100)")
    ap.add_argument("--out", required=True)
    ap.add_argument("--jobs", type=int, default=4)
    ap.add_argument("--gmp", type=int, default=1)
    args = ap.parse_args()

    start, end = map(int, args.seeds.split(":"))
    outdir = os.path.abspath(args.out)
    os.makedirs(outdir, exist_ok=True)
    progdir = os.path.join(outdir, "programs")
    os.makedirs(progdir, exist_ok=True)

    # 1. Build Java modules once.
    print("== building graalgo java modules ==", flush=True)
    r = sh(f"make -C {GR}/phases/phase3 main", timeout=600)
    if r.returncode != 0:
        print("BUILD FAILED")
        print(r.stdout[-2000:])
        print(r.stderr[-2000:])
        sys.exit(1)
    print("build ok", flush=True)

    # 2. Generate programs.
    seeds = list(range(start, end))
    print(f"== generating {len(seeds)} programs ==", flush=True)
    for s in seeds:
        out = os.path.join(progdir, f"fuzz_{s:05d}.go")
        r = sh(f"python3 {FUZZ}/gen/generator.py --seed {s} --out {out}")
        if r.returncode != 0:
            print(f"generator failed for seed {s}: {r.stderr[:500]}")
            sys.exit(1)

    # 3. Differential runs in parallel.
    print(f"== differential: {len(seeds)} programs x {args.jobs} jobs ==",
          flush=True)
    results = []
    results_path = os.path.join(outdir, "results.jsonl")
    # Incremental writes: append as each completes (kill-resilient).
    rf = open(results_path, "w")

    def run_one(s):
        workdir = os.path.join(outdir, "work", f"seed_{s:05d}")
        os.makedirs(workdir, exist_ok=True)
        gofile = os.path.join(progdir, f"fuzz_{s:05d}.go")
        r = sh(f"bash {FUZZ}/harness/diff_one.sh {gofile} {workdir} {args.gmp}",
               timeout=300)
        try:
            rec = json.loads(r.stdout.strip().split("\n")[-1])
        except Exception:
            rec = {"file": f"fuzz_{s:05d}", "result": "HARNESS_ERROR",
                   "detail": r.stdout[-500:] + r.stderr[-500:]}
        rec["seed"] = s
        return rec

    with ThreadPoolExecutor(max_workers=args.jobs) as ex:
        for i, rec in enumerate(ex.map(run_one, seeds)):
            results.append(rec)
            rf.write(json.dumps(rec) + "\n")
            rf.flush()
            if (i + 1) % 10 == 0:
                print(f"  {i+1}/{len(seeds)}", flush=True)
    rf.close()

    # Dedup: group non-PASS by (result, signature).
    from collections import Counter, defaultdict
    counts = Counter(r["result"] for r in results)
    groups = defaultdict(list)
    for r in results:
        if r["result"] in ("PASS", "SCOPE"):
            continue
        sig = (r["result"], r["detail"][:100])
        groups[sig].append(r["seed"])

    print("\n== summary ==")
    for k, v in sorted(counts.items()):
        print(f"  {k}: {v}")
    if groups:
        print("\n== failure groups (deduped) ==")
        for (res, sig), seeds_g in sorted(groups.items()):
            print(f"  {res} x{len(seeds_g)}: {sig[:80]} (e.g. seed {seeds_g[0]})")

    # Scope-hit rate (generator tuning signal).
    scope_rate = counts.get("SCOPE", 0) / max(1, len(results))
    print(f"\nscope-hit rate: {scope_rate:.1%}")
    if scope_rate > 0.2:
        print("WARNING: scope-hit rate >20% — tune generator before scaling")

    # Exit code is the gate: ANY non-PASS/SCOPE result fails the batch.
    # GC_BUILD_FAIL, WRONG_OUTPUT, CRASH, HANG, EXIT_MISMATCH, HARNESS_ERROR
    # and any future divergence class are all failures. (Previously only
    # GC_BUILD_FAIL affected the exit code, so a night full of divergences
    # exited 0 and the soak stayed green.)
    if not results:
        print("ERROR: no programs ran (empty seed range?) — refusing green on an empty batch")
    non_pass = {k: v for k, v in counts.items() if k not in ("PASS", "SCOPE")}
    if non_pass:
        detail = ", ".join(f"{k}={v}" for k, v in sorted(non_pass.items()))
        print(f"\nERROR: batch failed: {detail}")
    return batch_exit_code(counts, len(results))


if __name__ == "__main__":
    sys.exit(main())
