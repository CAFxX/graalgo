#!/usr/bin/env python3
"""Overnight-soak summary: write soak-report.json and fail the soak on
divergences (C04-P1-2).

Reads every batch-*/results.jsonl under --out, counts diverged programs
(any result other than PASS, SCOPE, or GC_BUILD_FAIL — the same rule
run_soak.sh's minimize step uses), and writes {"batches",
"batches_failed", "diverged_total", "harness_error_total"} to
soak-report.json.

Exit code: 1 when diverged_total > 0 or any batch failed outright
(--failed-batches: a batch that died before writing results, e.g. at
the build step, contributes no rows, so it must fail the soak
explicitly); 0 otherwise. A gate that cannot fail is not a gate.

Usage:
  python3 soak_report.py --out DIR [--failed-batches N]
"""
import argparse
import glob
import json
import os
import sys

OK_RESULTS = ("PASS", "SCOPE", "GC_BUILD_FAIL")


def summarize(outdir):
    """Count diverged programs across all batch results files."""
    diverged = 0
    harness_errors = 0
    for res in glob.glob(os.path.join(outdir, "batch-*", "results.jsonl")):
        with open(res) as fh:
            for line in fh:
                if not line.strip():
                    continue
                r = json.loads(line)
                if r.get("result") not in OK_RESULTS:
                    diverged += 1
                    if r.get("result") == "HARNESS_ERROR":
                        harness_errors += 1
    batches = len(glob.glob(os.path.join(outdir, "batch-*")))
    return {"batches": batches, "diverged_total": diverged,
            "harness_error_total": harness_errors}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True)
    ap.add_argument("--failed-batches", type=int, default=0,
                    help="batches whose driver exited non-zero")
    args = ap.parse_args()
    rep = summarize(args.out)
    rep = {"batches": rep["batches"],
           "batches_failed": args.failed_batches,
           "diverged_total": rep["diverged_total"],
           "harness_error_total": rep["harness_error_total"]}
    with open(os.path.join(args.out, "soak-report.json"), "w") as f:
        json.dump(rep, f, indent=2)
    print(f"soak report: {rep}")
    if rep["diverged_total"] > 0 or rep["batches_failed"] > 0:
        print(f"SOAK FAILED: diverged_total={rep['diverged_total']} "
              f"(harness errors: {rep['harness_error_total']}), "
              f"batches_failed={rep['batches_failed']}")
        return 1
    print("soak clean: no divergences, no failed batches")
    return 0


if __name__ == "__main__":
    sys.exit(main())
