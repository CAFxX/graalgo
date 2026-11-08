#!/usr/bin/env python3
"""Regression tests for gate1 synthesize.py build-tag filtering (R-1108-141).

The gate1 synthesizer used to ignore the //go:build (and // +build)
constraints discover.py collects, so the bytes pilot scheduled
TestIssue65571 — a js && wasm test allocating a 2 GiB slice — for a
linux/amd64 run, where gc's own suite would never run it.

Run from the repo root:
    python3 -m unittest discover -s tools/gate1 -p 'test_*.py'
"""
import json
import os
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import synthesize

HERE = os.path.dirname(os.path.abspath(__file__))
GOROOT = os.path.expanduser("~/tools/go")


class TestBuildExprSatisfied(unittest.TestCase):
    """build_expr_satisfied mirrors srctest's satisfiable() (linux/amd64+gc)."""

    def test_satisfied_terms(self):
        for expr in (
            "linux",
            "amd64",
            "unix",
            "gc",
            "linux && amd64",
            "linux || windows",
            "(linux || darwin) && amd64",
            "!windows",
            "linux && !windows",
            "go1.21",
            "go1.27",
            "go1.27rc1",
        ):
            with self.subTest(expr=expr):
                self.assertTrue(synthesize.build_expr_satisfied(expr), expr)

    def test_unsatisfied_terms(self):
        for expr in (
            "js",
            "wasm",
            "js && wasm",
            "windows",
            "darwin",
            "cgo",
            "race",
            "ignore",
            "go1.28",
            "go1.99",
            "linux && windows",
            "!(linux)",
            "",
            "linux &&",
            "linux ||",
            "(linux",
        ):
            with self.subTest(expr=expr):
                self.assertFalse(synthesize.build_expr_satisfied(expr), expr)


class TestPlusBuildConversion(unittest.TestCase):
    """// +build lines convert to the equivalent //go:build expression."""

    def test_conversion(self):
        cases = {
            "// +build linux": "linux",
            "// +build linux,amd64": "(linux && amd64)",
            "// +build linux windows": "linux || windows",
            "// +build linux,amd64 darwin": "(linux && amd64) || darwin",
            "// +build !windows": "!windows",
            "// +build ignore": "ignore",
        }
        for line, want in cases.items():
            with self.subTest(line=line):
                self.assertEqual(synthesize.plus_build_to_expr(line), want)

    def test_tags_satisfied_plus_build(self):
        self.assertTrue(synthesize.tags_satisfied(["// +build linux"]))
        self.assertTrue(synthesize.tags_satisfied(["// +build linux,amd64"]))
        self.assertTrue(synthesize.tags_satisfied(["// +build darwin linux"]))
        self.assertFalse(synthesize.tags_satisfied(["// +build windows"]))
        self.assertFalse(synthesize.tags_satisfied(["// +build js,wasm"]))
        self.assertFalse(synthesize.tags_satisfied(["// +build ignore"]))


class TestTagsSatisfied(unittest.TestCase):
    """tags_satisfied ANDs every constraint line; no tags means included."""

    def test_no_tags_included(self):
        self.assertTrue(synthesize.tags_satisfied([]))
        self.assertTrue(synthesize.tags_satisfied(None))

    def test_go_build_lines_anded(self):
        self.assertTrue(
            synthesize.tags_satisfied(["//go:build linux", "//go:build amd64"]))
        self.assertFalse(
            synthesize.tags_satisfied(["//go:build linux", "//go:build js"]))

    def test_mixed_forms(self):
        self.assertTrue(
            synthesize.tags_satisfied(["//go:build linux", "// +build amd64"]))
        self.assertFalse(
            synthesize.tags_satisfied(["//go:build linux", "// +build windows"]))

    def test_unrecognized_line_fails_closed(self):
        self.assertFalse(synthesize.tags_satisfied(["// some random comment"]))


def _write_fixture(tmpdir, name, tags_header, test_name):
    path = os.path.join(tmpdir, name)
    with open(path, "w") as f:
        f.write(tags_header)
        f.write("""
package bytes_test

import (
\t"bytes"
\t"testing"
)

func %s(t *testing.T) {
\tif bytes.Compare([]byte("a"), []byte("b")) >= 0 {
\t\tt.Errorf("want negative")
\t}
}
""" % test_name)
    return path


class TestSynthesizeSkipsUnsatisfiableTags(unittest.TestCase):
    """End-to-end: synthesize drops the js/wasm file, keeps the rest."""

    def test_fixture_driver(self):
        with tempfile.TemporaryDirectory(prefix="gate1-tags-") as tmpdir:
            js_path = _write_fixture(
                tmpdir, "bytes_js_wasm_test.go", "//go:build js && wasm\n",
                "TestIssue65571")
            linux_path = _write_fixture(
                tmpdir, "boundary_test.go", "//go:build linux\n",
                "TestLinuxOnly")
            plain_path = _write_fixture(
                tmpdir, "plain_test.go", "", "TestPlain")
            disc = {
                "package": "bytes",
                "goroot": tmpdir,
                "files": [
                    {"path": js_path, "tests": ["TestIssue65571"],
                     "imports": ["bytes", "testing"],
                     "build_tags": ["//go:build js && wasm"]},
                    {"path": linux_path, "tests": ["TestLinuxOnly"],
                     "imports": ["bytes", "testing"],
                     "build_tags": ["//go:build linux"]},
                    {"path": plain_path, "tests": ["TestPlain"],
                     "imports": ["bytes", "testing"],
                     "build_tags": []},
                ],
            }
            disc_path = os.path.join(tmpdir, "discover.json")
            with open(disc_path, "w") as f:
                json.dump(disc, f)
            driver = os.path.join(tmpdir, "driver.go")
            proc = subprocess.run(
                [sys.executable, os.path.join(HERE, "synthesize.py"),
                 disc_path, "--out", driver,
                 "--shim", os.path.join(HERE, "shim", "shim.go")],
                capture_output=True, text=True, timeout=60)
            self.assertEqual(proc.returncode, 0, proc.stderr)
            self.assertIn("SKIP file bytes_js_wasm_test.go", proc.stderr)
            self.assertIn("unsatisfiable build tags", proc.stderr)
            with open(driver) as f:
                src = f.read()
            # The js/wasm test must not be scheduled on linux/amd64.
            self.assertNotIn("TestIssue65571", src)
            self.assertNotIn("bytes_js_wasm_test.go", src)
            # The satisfiable files must be kept.
            self.assertIn("TestLinuxOnly", src)
            self.assertIn("TestPlain", src)


@unittest.skipUnless(
    os.path.isdir(os.path.join(GOROOT, "src", "bytes")),
    "pinned go1.27.1 toolchain not present at ~/tools/go")
class TestRealBytesPilot(unittest.TestCase):
    """Pin the exact review case on the real stdlib tree (no test is run)."""

    def test_bytes_js_wasm_skipped_and_boundary_kept_by_tags(self):
        with tempfile.TemporaryDirectory(prefix="gate1-bytes-") as tmpdir:
            disc_path = os.path.join(tmpdir, "discover.json")
            proc = subprocess.run(
                [sys.executable, os.path.join(HERE, "discover.py"),
                 "bytes", "--goroot", GOROOT],
                capture_output=True, text=True, timeout=60)
            self.assertEqual(proc.returncode, 0, proc.stderr)
            with open(disc_path, "w") as f:
                f.write(proc.stdout)
            disc = json.loads(proc.stdout)
            by_name = {os.path.basename(f["path"]): f for f in disc["files"]}
            self.assertIn("bytes_js_wasm_test.go", by_name)
            self.assertIn("boundary_test.go", by_name)
            js_tags = by_name["bytes_js_wasm_test.go"]["build_tags"]
            boundary_tags = by_name["boundary_test.go"]["build_tags"]
            # The tag decision itself: js/wasm is unsatisfiable here,
            # linux is satisfiable. (boundary_test.go is separately skipped
            # by synthesize for its syscall import; that is unrelated to
            # the tag decision pinned here.)
            self.assertFalse(synthesize.tags_satisfied(js_tags), js_tags)
            self.assertTrue(synthesize.tags_satisfied(boundary_tags),
                            boundary_tags)
            driver = os.path.join(tmpdir, "driver.go")
            proc = subprocess.run(
                [sys.executable, os.path.join(HERE, "synthesize.py"),
                 disc_path, "--out", driver,
                 "--shim", os.path.join(HERE, "shim", "shim.go")],
                capture_output=True, text=True, timeout=60)
            self.assertEqual(proc.returncode, 0, proc.stderr)
            self.assertIn("SKIP file bytes_js_wasm_test.go", proc.stderr)
            self.assertIn("unsatisfiable build tags", proc.stderr)
            with open(driver) as f:
                src = f.read()
            self.assertNotIn("TestIssue65571", src)


if __name__ == "__main__":
    unittest.main()
