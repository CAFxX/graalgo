#!/usr/bin/env python3
"""Tests for harness/run_batch.py exit-code semantics (C04-P1-2).

The batch is a gate: any divergence or harness error must fail it.
Run from the harness directory:
  python3 -m unittest test_run_batch -v
  (or: python3 test_run_batch.py)
"""
import json
import os
import sys
import tempfile
import unittest
from collections import Counter

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import run_batch


class DecideExitCodeTest(unittest.TestCase):
    def test_all_pass_exits_zero(self):
        self.assertEqual(run_batch.decide_exit_code(Counter({"PASS": 5}))[0], 0)

    def test_pass_and_scope_exit_zero(self):
        self.assertEqual(
            run_batch.decide_exit_code(Counter({"PASS": 3, "SCOPE": 2}))[0], 0)

    def test_empty_counts_exit_zero(self):
        # Zero results = nothing tested. That hole is the carried R-01
        # defect (dead build path), owned by that lane — not this fix.
        self.assertEqual(run_batch.decide_exit_code(Counter())[0], 0)

    def test_gc_build_fail_still_exits_one(self):
        # Pre-existing behaviour, unchanged by the fix.
        self.assertEqual(
            run_batch.decide_exit_code(
                Counter({"PASS": 1, "GC_BUILD_FAIL": 1}))[0], 1)

    def test_each_diverged_class_exits_one(self):
        for result in ("WRONG_OUTPUT", "EXIT_MISMATCH",
                       "GO_PANIC_MISMATCH", "CRASH", "HANG"):
            with self.subTest(result=result):
                code, reason = run_batch.decide_exit_code(
                    Counter({"PASS": 4, result: 1}))
                self.assertEqual(code, 1)
                self.assertIn("diverged", reason)

    def test_all_harness_error_exits_one(self):
        code, reason = run_batch.decide_exit_code(
            Counter({"HARNESS_ERROR": 3}))
        self.assertEqual(code, 1)
        self.assertIn("harness", reason)

    def test_gc_hang_exits_one(self):
        # gc itself hung: a generator bug, not a graalgo divergence —
        # but the batch tested nothing meaningful for that seed, and the
        # overnight summary counts it as diverged, so the batch must
        # fail the same way.
        self.assertEqual(
            run_batch.decide_exit_code(Counter({"PASS": 1, "GC_HANG": 1}))[0], 1)

    def test_unknown_result_class_fails_loud(self):
        # A gate must fail on a verdict it does not recognise, never pass.
        self.assertEqual(
            run_batch.decide_exit_code(
                Counter({"PASS": 1, "SOMETHING_NEW": 1}))[0], 1)

    def test_reason_distinguishes_diverged_from_harness(self):
        _, div_reason = run_batch.decide_exit_code(
            Counter({"WRONG_OUTPUT": 1}))
        _, he_reason = run_batch.decide_exit_code(
            Counter({"HARNESS_ERROR": 1}))
        self.assertIn("diverged", div_reason)
        self.assertNotIn("harness", div_reason)
        self.assertIn("harness", he_reason)
        self.assertNotIn("diverged", he_reason)


def write_results_jsonl(path, results):
    with open(path, "w") as f:
        for i, result in enumerate(results):
            f.write(json.dumps({"file": f"fuzz_{i:05d}", "result": result,
                                "detail": "", "seed": i}) + "\n")


class ResultsFileTest(unittest.TestCase):
    """The decision as main() computes it: counts read from results.jsonl."""

    def decide_for_file(self, results):
        with tempfile.TemporaryDirectory() as d:
            p = os.path.join(d, "results.jsonl")
            write_results_jsonl(p, results)
            with open(p) as fh:
                counts = Counter(json.loads(line)["result"]
                                 for line in fh if line.strip())
            return run_batch.decide_exit_code(counts)[0]

    def test_one_wrong_output_in_file_exits_one(self):
        self.assertEqual(self.decide_for_file(["PASS", "WRONG_OUTPUT"]), 1)

    def test_all_harness_error_in_file_exits_one(self):
        self.assertEqual(self.decide_for_file(["HARNESS_ERROR"] * 3), 1)

    def test_all_pass_in_file_exits_zero(self):
        self.assertEqual(self.decide_for_file(["PASS", "PASS"]), 0)


if __name__ == "__main__":
    unittest.main()
