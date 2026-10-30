#!/usr/bin/env python3
"""Unit tests for the run_batch.py gate verdict (batch_exit_code).

Run: python3 run_batch_test.py  (stdlib unittest, no third-party deps)

These tests are the regression net for the fuzz-soak gate: every
divergence class must fail the batch, and PASS/SCOPE-only batches (and
only those) pass. An empty batch fails.
"""
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from run_batch import batch_exit_code  # noqa: E402

DIVERGENCE_CLASSES = [
    "WRONG_OUTPUT",
    "CRASH",
    "HANG",
    "EXIT_MISMATCH",
    "GO_PANIC_MISMATCH",
    "GC_HANG",
    "GC_BUILD_FAIL",
    "HARNESS_ERROR",
]


class TestBatchExitCode(unittest.TestCase):
    def test_pass_only(self):
        self.assertEqual(batch_exit_code({"PASS": 10}, 10), 0)

    def test_pass_and_scope(self):
        self.assertEqual(batch_exit_code({"PASS": 8, "SCOPE": 2}, 10), 0)

    def test_empty_batch_fails(self):
        self.assertEqual(batch_exit_code({}, 0), 1)
        self.assertEqual(batch_exit_code({"PASS": 0}, 0), 1)

    def test_each_divergence_class_fails(self):
        for cls in DIVERGENCE_CLASSES:
            with self.subTest(cls=cls):
                self.assertEqual(
                    batch_exit_code({"PASS": 9, cls: 1}, 10), 1)

    def test_divergence_alone_fails(self):
        for cls in DIVERGENCE_CLASSES:
            with self.subTest(cls=cls):
                self.assertEqual(batch_exit_code({cls: 1}, 1), 1)

    def test_unknown_future_class_fails(self):
        # The gate is allowlist-based (PASS/SCOPE only), not a denylist
        # of known classes: a class added tomorrow must fail, not slip
        # through because nobody updated the exit-code logic.
        self.assertEqual(
            batch_exit_code({"PASS": 9, "SOME_NEW_CLASS": 1}, 10), 1)


if __name__ == "__main__":
    unittest.main()
