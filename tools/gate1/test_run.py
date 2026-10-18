#!/usr/bin/env python3
"""Unit tests for the gate1 verdict logic in run.py.

Covers compute_verdict() over every (gc leg, graalgo leg) outcome
combination plus the per-test comparison paths. Pure in-memory: no
drivers are built or run.

Run:  python3 -B tools/gate1/test_run.py
(-B inhibits .pyc writes so the test works on read-only checkouts.)
"""
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import run


class ComputeVerdictTest(unittest.TestCase):
    def test_both_crash_is_skip_not_pass(self):
        # The P1: a simultaneous driver crash is the strongest failure
        # signal; blessing it PASS hid total harness/driver failure.
        verdict, divs = run.compute_verdict(1, 1, {}, {})
        self.assertEqual(verdict, "SKIP")
        self.assertEqual(divs, [])

    def test_both_crash_skips_even_with_partial_results(self):
        # A crash invalidates any partial per-test output parsed before it.
        verdict, _ = run.compute_verdict(
            1, 1, {"TestA": "PASS"}, {"TestA": "PASS"})
        self.assertEqual(verdict, "SKIP")

    def test_both_timeout_is_skip(self):
        verdict, _ = run.compute_verdict(-1, -1, {}, {})
        self.assertEqual(verdict, "SKIP")

    def test_gc_only_crash_is_skip(self):
        # gc failure is a harness problem, not a graalgo result.
        verdict, _ = run.compute_verdict(1, 0, {}, {"TestA": "PASS"})
        self.assertEqual(verdict, "SKIP")

    def test_gc_timeout_is_skip(self):
        verdict, _ = run.compute_verdict(-1, 0, {}, {"TestA": "PASS"})
        self.assertEqual(verdict, "SKIP")

    def test_graalgo_only_crash_is_diverge(self):
        verdict, _ = run.compute_verdict(0, 1, {"TestA": "PASS"}, {})
        self.assertEqual(verdict, "DIVERGE")

    def test_all_agree_is_pass(self):
        res = {"TestA": "PASS", "TestB": "FAIL", "TestC": "SKIP"}
        verdict, divs = run.compute_verdict(0, 0, res, dict(res))
        self.assertEqual(verdict, "PASS")
        self.assertEqual(divs, [])

    def test_per_test_mismatch_is_diverge(self):
        verdict, divs = run.compute_verdict(
            0, 0, {"TestA": "PASS"}, {"TestA": "FAIL"})
        self.assertEqual(verdict, "DIVERGE")
        self.assertEqual(divs, [("TestA", "PASS", "FAIL")])

    def test_missing_test_is_diverge(self):
        verdict, divs = run.compute_verdict(
            0, 0, {"TestA": "PASS", "TestB": "PASS"}, {"TestA": "PASS"})
        self.assertEqual(verdict, "DIVERGE")
        self.assertEqual(divs, [("TestB", "PASS", "MISSING")])

    def test_empty_but_clean_run_is_pass(self):
        # Zero discovered tests with both legs green: vacuously PASS
        # (distinct from the both-crash SKIP above).
        verdict, divs = run.compute_verdict(0, 0, {}, {})
        self.assertEqual(verdict, "PASS")
        self.assertEqual(divs, [])


if __name__ == "__main__":
    unittest.main(verbosity=2)
