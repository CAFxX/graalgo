#!/usr/bin/env python3
"""Tests for harness/soak_report.py (C04-P1-2).

The overnight soak writes soak-report.json and must exit non-zero when
it found divergences or when a batch failed outright.
Run from the harness directory:
  python3 -m unittest test_soak_report -v
  (or: python3 test_soak_report.py)
"""
import json
import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
SOAK_REPORT = os.path.join(HERE, "soak_report.py")


def make_batch(outdir, name, results):
    bdir = os.path.join(outdir, name)
    os.makedirs(bdir)
    with open(os.path.join(bdir, "results.jsonl"), "w") as f:
        for i, result in enumerate(results):
            f.write(json.dumps({"file": f"fuzz_{i:05d}", "result": result,
                                "detail": "", "seed": i}) + "\n")


def run_report(outdir, failed_batches=0):
    return subprocess.run(
        [sys.executable, SOAK_REPORT, "--out", outdir,
         "--failed-batches", str(failed_batches)],
        capture_output=True, text=True)


def load_report(outdir):
    with open(os.path.join(outdir, "soak-report.json")) as fh:
        return json.load(fh)


class SoakReportTest(unittest.TestCase):
    def test_clean_soak_exits_zero(self):
        with tempfile.TemporaryDirectory() as d:
            make_batch(d, "batch-0", ["PASS", "SCOPE"])
            make_batch(d, "batch-1", ["PASS"])
            p = run_report(d)
            self.assertEqual(p.returncode, 0, p.stdout + p.stderr)
            rep = load_report(d)
            self.assertEqual(rep, {"batches": 2, "batches_failed": 0,
                                   "diverged_total": 0,
                                   "harness_error_total": 0})

    def test_diverged_soak_exits_one(self):
        with tempfile.TemporaryDirectory() as d:
            make_batch(d, "batch-0", ["PASS", "WRONG_OUTPUT"])
            p = run_report(d)
            self.assertEqual(p.returncode, 1, p.stdout + p.stderr)
            rep = load_report(d)
            self.assertEqual(rep["diverged_total"], 1)

    def test_all_harness_error_exits_one(self):
        with tempfile.TemporaryDirectory() as d:
            make_batch(d, "batch-0", ["HARNESS_ERROR", "HARNESS_ERROR"])
            p = run_report(d)
            self.assertEqual(p.returncode, 1, p.stdout + p.stderr)
            rep = load_report(d)
            self.assertEqual(rep["diverged_total"], 2)
            self.assertEqual(rep["harness_error_total"], 2)

    def test_failed_batch_with_no_results_exits_one(self):
        # A batch that died before writing results.jsonl contributes no
        # rows; the --failed-batches count is what fails the soak.
        with tempfile.TemporaryDirectory() as d:
            make_batch(d, "batch-0", ["PASS"])
            os.makedirs(os.path.join(d, "batch-1"))  # no results.jsonl
            p = run_report(d, failed_batches=1)
            self.assertEqual(p.returncode, 1, p.stdout + p.stderr)
            rep = load_report(d)
            self.assertEqual(rep["batches_failed"], 1)
            self.assertEqual(rep["diverged_total"], 0)

    def test_gc_build_fail_not_counted_as_diverged(self):
        # GC_BUILD_FAIL is a generator bug, not a graalgo divergence:
        # it fails its batch (run_batch.py) but is not a soak divergence.
        # (In a real soak that batch's non-zero exit also raises
        # batches_failed, which fails the soak via that count instead.)
        with tempfile.TemporaryDirectory() as d:
            make_batch(d, "batch-0", ["PASS", "GC_BUILD_FAIL"])
            p = run_report(d)
            self.assertEqual(p.returncode, 0, p.stdout + p.stderr)
            rep = load_report(d)
            self.assertEqual(rep["diverged_total"], 0)


if __name__ == "__main__":
    unittest.main()
