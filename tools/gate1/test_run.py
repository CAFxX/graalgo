#!/usr/bin/env python3
"""Regression tests for tools/gate1/run.py verdict logic.

Covers review 2026-10-30 P1-9 (both drivers failing must never report
PASS) and the PASS-requires-evidence rule: a PASS verdict requires both
legs to exit 0, at least one parsed test result, and no divergences.

Run: python3 -m unittest discover -s tools/gate1 -p 'test_run.py' -v
from the repo root, or: python3 tools/gate1/test_run.py
"""

import importlib.util
import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
RUN_PY = os.path.join(HERE, "run.py")


def load_run_module():
    spec = importlib.util.spec_from_file_location("gate1_run", RUN_PY)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


run_mod = load_run_module()
decide_verdict = run_mod.decide_verdict


class TestDecideVerdict(unittest.TestCase):
    def test_both_drivers_fail_is_error_not_pass(self):
        # P1-9 regression: two crashed legs, empty outputs, no results.
        verdict, divs = decide_verdict(1, 1, {}, {})
        self.assertEqual(verdict, "ERROR")
        self.assertEqual(divs, [])

    def test_both_drivers_fail_with_results_is_error(self):
        # Even partial results cannot rescue a double crash: gc did not
        # pass, so this is not a divergence and not a pass.
        verdict, _ = decide_verdict(1, 1, {"TestA": "PASS"}, {"TestA": "PASS"})
        self.assertEqual(verdict, "ERROR")

    def test_both_drivers_timeout_is_error(self):
        # run_cmd reports timeouts as rc=-1 on both legs.
        verdict, _ = decide_verdict(-1, -1, {}, {})
        self.assertEqual(verdict, "ERROR")

    def test_both_exit_zero_without_results_is_error(self):
        # Nothing was compared, so nothing passed — not a vacuous PASS.
        verdict, _ = decide_verdict(0, 0, {}, {})
        self.assertEqual(verdict, "ERROR")

    def test_agreeing_results_pass(self):
        verdict, divs = decide_verdict(
            0, 0, {"TestA": "PASS", "TestB": "FAIL"},
            {"TestA": "PASS", "TestB": "FAIL"})
        self.assertEqual(verdict, "PASS")
        self.assertEqual(divs, [])

    def test_disagreeing_results_diverge(self):
        verdict, divs = decide_verdict(
            0, 0, {"TestA": "PASS"}, {"TestA": "FAIL"})
        self.assertEqual(verdict, "DIVERGE")
        self.assertEqual(divs, [("TestA", "PASS", "FAIL")])

    def test_missing_result_diverges(self):
        verdict, divs = decide_verdict(0, 0, {"TestA": "PASS"}, {})
        self.assertEqual(verdict, "DIVERGE")
        self.assertEqual(divs, [("TestA", "PASS", "MISSING")])

    def test_gc_failure_alone_is_skip(self):
        verdict, _ = decide_verdict(1, 0, {}, {"TestA": "PASS"})
        self.assertEqual(verdict, "SKIP")

    def test_graalgo_failure_alone_is_diverge(self):
        verdict, _ = decide_verdict(0, 1, {"TestA": "PASS"}, {})
        self.assertEqual(verdict, "DIVERGE")


class TestRunPyEndToEndDoubleCrash(unittest.TestCase):
    """Both driver legs exit non-zero with empty output -> ERROR, exit 1.

    Builds a fake GOROOT (one external test package), a fake `go` that
    exits 1 (the gc leg runs `<goroot>/bin/go run driver.go`), and a
    fake graalgo-run that exits 1, then runs the real run.py end to end.
    """

    def test_double_crash_is_error_exit_nonzero(self):
        with tempfile.TemporaryDirectory(prefix="gate1-doublecrash-") as tmp:
            goroot = os.path.join(tmp, "goroot")
            pkgdir = os.path.join(goroot, "src", "demopkg")
            bindir = os.path.join(goroot, "bin")
            os.makedirs(pkgdir)
            os.makedirs(bindir)
            with open(os.path.join(pkgdir, "demopkg_test.go"), "w") as f:
                f.write("package demopkg_test\n\nimport \"testing\"\n\n"
                        "func TestDemo(t *testing.T) {}\n")
            for path in (os.path.join(bindir, "go"),
                         os.path.join(tmp, "fake-graalgo-run")):
                with open(path, "w") as f:
                    f.write("#!/bin/sh\nexit 1\n")
                os.chmod(path, 0o755)
            outdir = os.path.join(tmp, "out")
            proc = subprocess.run(
                [sys.executable, RUN_PY, "demopkg",
                 "--goroot", goroot,
                 "--graalgo-run", os.path.join(tmp, "fake-graalgo-run"),
                 "--out", outdir],
                capture_output=True, text=True, timeout=120)
            self.assertNotEqual(proc.returncode, 0,
                                f"run.py exited 0 on double crash:\n{proc.stdout}")
            self.assertIn("VERDICT demopkg: ERROR", proc.stdout)
            with open(os.path.join(outdir, "verdict.txt")) as f:
                self.assertEqual(f.read().strip(), "ERROR")


if __name__ == "__main__":
    unittest.main()
