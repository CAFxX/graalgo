#!/usr/bin/env python3
"""Regression tests for R20B-05: gate1 GOOS/GOARCH build-tag filtering.

gate1 used to inline every ``*_test.go`` file it found, including files
whose ``//go:build`` constraints exclude the target platform. The live
trigger: ``bytes/bytes_js_wasm_test.go`` (``//go:build js && wasm``,
``TestIssue65571``, allocates 2 GiB + 1) was synthesized into a
linux/amd64 driver and run there.

These tests pin the fix, not the bug: on the pristine scripts they FAIL
(T1/T3 error on the missing ``target_goos`` key, T4 fails 18 vs 17
tests); on the fixed scripts all four pass. T2 is the control that
passes on both (a ``//go:build linux`` file IS included on linux/amd64).

Run from anywhere: ``python3 tools/gate1/test_buildtags.py``.
"""
import json
import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
GOROOT = os.environ.get("GATE1_GOROOT", os.path.expanduser("~/tools/go"))
SHIM = os.path.join(HERE, "shim", "shim.go")


def run_discover(pkg, *extra):
    p = subprocess.run(
        [sys.executable, os.path.join(HERE, "discover.py"),
         pkg, "--goroot", GOROOT, *extra],
        capture_output=True, text=True, timeout=300)
    if p.returncode != 0:
        raise AssertionError(f"discover.py {pkg} failed: {p.stderr.strip()[:500]}")
    return json.loads(p.stdout)


def by_basename(files):
    return {os.path.basename(f["path"]): f for f in files}


class BuildTagsTest(unittest.TestCase):
    def test_discover_excludes_js_wasm_on_linux_amd64(self):
        d = run_discover("bytes")
        self.assertEqual((d["target_goos"], d["target_goarch"]),
                         ("linux", "amd64"))
        self.assertEqual(d["total_tests"], 95)
        files = by_basename(d["files"])
        self.assertNotIn("bytes_js_wasm_test.go", files)
        self.assertNotIn("TestIssue65571",
                         [t for f in d["files"] for t in f["tests"]])
        skipped = by_basename(d["skipped_files"])
        self.assertIn("bytes_js_wasm_test.go", skipped)
        self.assertEqual(skipped["bytes_js_wasm_test.go"]["tests"],
                         ["TestIssue65571"])
        self.assertIn("js && wasm",
                      skipped["bytes_js_wasm_test.go"]["skip_reason"])

    def test_discover_includes_linux_gated_file(self):
        d = run_discover("bytes")
        files = by_basename(d["files"])
        self.assertIn("boundary_test.go", files)
        self.assertIn("//go:build linux",
                      files["boundary_test.go"]["build_tags"])

    def test_positive_control_js_wasm_included(self):
        d = run_discover("bytes", "--goos", "js", "--goarch", "wasm")
        self.assertEqual((d["target_goos"], d["target_goarch"]),
                         ("js", "wasm"))
        # 96 pristine tests minus the 4 linux-gated boundary tests.
        self.assertEqual(d["total_tests"], 92)
        files = by_basename(d["files"])
        self.assertIn("bytes_js_wasm_test.go", files)
        self.assertIn("TestIssue65571",
                      files["bytes_js_wasm_test.go"]["tests"])
        skipped = by_basename(d["skipped_files"])
        self.assertIn("boundary_test.go", skipped)

    def test_synthesize_excludes_js_wasm(self):
        d = run_discover("bytes")
        with tempfile.TemporaryDirectory(prefix="gate1-r20b05-") as td:
            disc = os.path.join(td, "discover.json")
            driver = os.path.join(td, "driver.go")
            with open(disc, "w") as f:
                json.dump(d, f)
            q = subprocess.run(
                [sys.executable, os.path.join(HERE, "synthesize.py"),
                 disc, "--out", driver, "--shim", SHIM],
                capture_output=True, text=True, timeout=300)
            self.assertEqual(q.returncode, 0, q.stderr)
            self.assertIn("synthesized 17 tests", q.stderr)
            with open(driver) as f:
                src = f.read()
            self.assertEqual(src.count("TestIssue65571"), 0)


if __name__ == "__main__":
    unittest.main()
