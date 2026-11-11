#!/usr/bin/env python3
"""Batch differential fuzzing driver.

1. Builds graalgo Java modules once.
2. Generates N programs (seed range).
3. Runs diff_one.sh in parallel.
4. Classifies, dedups, writes results.jsonl + summary.

Exits non-zero when any result is a divergence, a harness error, or any
other non-PASS/SCOPE verdict (C04-P1-2): the soak is a gate, so its exit
code must express its finding.

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

# Result classes that fail a batch (C04-P1-2). The soak is a gate, so its
# exit code must express its finding: before this fix the ONLY failing
# signal was GC_BUILD_FAIL, and a batch full of genuine divergences — or
# of total harness failure — exited 0.
# DIVERGED: genuine graalgo divergences — the exact signal the soak exists
# to produce (emitted by harness/diff_one.sh).
DIVERGED_RESULTS = frozenset({
    "WRONG_OUTPUT", "EXIT_MISMATCH", "GO_PANIC_MISMATCH", "CRASH", "HANG",
})
# Results that pass a batch. Everything else fails it: besides DIVERGED
# and HARNESS_ERROR this covers GC_BUILD_FAIL (unchanged behaviour) and
# GC_HANG (gc itself hung — a generator bug, not a graalgo divergence,
# but the batch tested nothing meaningful for that seed), plus any future
# result class. A gate must fail loud on a verdict it does not
# recognise, never pass.
OK_RESULTS = frozenset({"PASS", "SCOPE"})


def decide_exit_code(counts):
    """Batch exit code for a mapping of result -> count.

    Returns (code, reason): 0 only when every result is PASS or SCOPE;
    1 otherwise, with the reason naming which class fired so the failure
    is diagnosable (divergences vs harness errors vs other failures).
    A batch with zero results (nothing tested) returns 0 here — the
    zero-program case is the carried R-01 defect's mechanism and is
    owned by that lane, not by this function.
    """
    diverged = sum(counts.get(r, 0) for r in DIVERGED_RESULTS)
    harness = counts.get("HARNESS_ERROR", 0)
    other = {k: v for k, v in counts.items()
             if k not in OK_RESULTS and k not in DIVERGED_RESULTS
             and k != "HARNESS_ERROR"}
    if diverged == 0 and harness == 0 and not other:
        return 0, "all PASS/SCOPE"
    parts = []
    if diverged:
        parts.append(f"{diverged} diverged")
    if harness:
        parts.append(f"{harness} harness error(s)")
    if other:
        parts.append("other failure(s): " + ", ".join(
            f"{k}={v}" for k, v in sorted(other.items())))
    return 1, "; ".join(parts)


def sh(cmd, **kw):
    return subprocess.run(cmd, shell=True, capture_output=True, text=True, **kw)


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

    code, reason = decide_exit_code(counts)
    if code != 0:
        print(f"\nBATCH FAILED: {reason}")
    else:
        print("\nbatch clean: all PASS/SCOPE")
    return code


if __name__ == "__main__":
    sys.exit(main())
