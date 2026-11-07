#!/usr/bin/env python3
"""Unit tests for the gate1 verdict logic (run.py: decide_verdict).

Regression tests for REVIEW-2026-11-07-07 R-02: a package whose driver
fails under BOTH gc and graalgo, or that executes zero tests under both
legs, must never be reported PASS.

Run from tools/gate1:  python3 -m unittest run_test -v
"""
import unittest

import run


class DecideVerdictTest(unittest.TestCase):
    def test_both_pass_agree(self):
        v, d = run.decide_verdict(0, 0, {"TestA": "PASS"}, {"TestA": "PASS"})
        self.assertEqual(v, "PASS")
        self.assertEqual(d, [])

    def test_gc_pass_gr_missing(self):
        v, d = run.decide_verdict(0, 0, {"TestA": "PASS"}, {})
        self.assertEqual(v, "DIVERGE")
        self.assertEqual(d, [("TestA", "PASS", "MISSING")])

    def test_result_mismatch(self):
        v, d = run.decide_verdict(0, 0, {"TestA": "PASS"}, {"TestA": "FAIL"})
        self.assertEqual(v, "DIVERGE")
        self.assertEqual(d, [("TestA", "PASS", "FAIL")])

    def test_gr_extra_test(self):
        v, d = run.decide_verdict(0, 0, {"TestA": "PASS"},
                                  {"TestA": "PASS", "TestB": "PASS"})
        self.assertEqual(v, "DIVERGE")
        self.assertEqual(d, [("TestB", "MISSING", "PASS")])

    def test_zero_tests_both_empty(self):
        # R-02, second half: both legs rc 0 but zero tests executed.
        v, d = run.decide_verdict(0, 0, {}, {})
        self.assertEqual(v, "ERROR")
        self.assertEqual(d, [])

    def test_gc_empty_gr_nonempty(self):
        v, d = run.decide_verdict(0, 0, {}, {"TestA": "PASS"})
        self.assertEqual(v, "DIVERGE")
        self.assertEqual(d, [("TestA", "MISSING", "PASS")])

    def test_gc_nonempty_gr_empty(self):
        v, d = run.decide_verdict(0, 0, {"TestA": "PASS"}, {})
        self.assertEqual(v, "DIVERGE")
        self.assertEqual(d, [("TestA", "PASS", "MISSING")])

    def test_gc_fails_alone(self):
        # Documented SKIP case: gc failed, graalgo ok.
        v, d = run.decide_verdict(1, 0, {}, {"TestA": "PASS"})
        self.assertEqual(v, "SKIP")
        self.assertEqual(d, [])

    def test_gr_fails_alone(self):
        v, d = run.decide_verdict(0, 1, {"TestA": "PASS"}, {})
        self.assertEqual(v, "DIVERGE")
        self.assertEqual(d, [])

    def test_both_fail(self):
        # R-02, first half: both legs fail -> ERROR, never PASS.
        v, d = run.decide_verdict(1, 1, {}, {})
        self.assertEqual(v, "ERROR")
        self.assertEqual(d, [])

    def test_both_timeout(self):
        # run_cmd returns -1 on timeout.
        v, d = run.decide_verdict(-1, -1, {}, {})
        self.assertEqual(v, "ERROR")
        self.assertEqual(d, [])

    def test_both_fail_with_partial_results(self):
        # Even identical partial results are untrustworthy when both legs
        # failed; the verdict must still be ERROR, never PASS.
        v, d = run.decide_verdict(1, 1, {"TestA": "PASS"}, {"TestA": "PASS"})
        self.assertEqual(v, "ERROR")
        self.assertEqual(d, [])

    def test_skip_results_agree(self):
        v, d = run.decide_verdict(0, 0, {"TestA": "SKIP"}, {"TestA": "SKIP"})
        self.assertEqual(v, "PASS")
        self.assertEqual(d, [])

    def test_skip_vs_pass_mismatch(self):
        v, d = run.decide_verdict(0, 0, {"TestA": "SKIP"}, {"TestA": "PASS"})
        self.assertEqual(v, "DIVERGE")
        self.assertEqual(d, [("TestA", "SKIP", "PASS")])

    def test_gc_fails_alone_empty_results(self):
        v, d = run.decide_verdict(2, 0, {}, {})
        self.assertEqual(v, "SKIP")
        self.assertEqual(d, [])

    def test_gr_fails_alone_empty_results(self):
        v, d = run.decide_verdict(0, 2, {}, {})
        self.assertEqual(v, "DIVERGE")
        self.assertEqual(d, [])

    def test_many_identical(self):
        gc = {f"Test{i}": "PASS" for i in range(50)}
        v, d = run.decide_verdict(0, 0, gc, dict(gc))
        self.assertEqual(v, "PASS")
        self.assertEqual(d, [])

    def test_agree_on_failure(self):
        # Both legs failing the same test is agreement, not divergence.
        v, d = run.decide_verdict(0, 0, {"TestA": "FAIL"}, {"TestA": "FAIL"})
        self.assertEqual(v, "PASS")
        self.assertEqual(d, [])


if __name__ == "__main__":
    unittest.main()
