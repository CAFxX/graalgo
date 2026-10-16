#!/usr/bin/env python3
"""Regression test: gate1 run.py verdict logic (E1).

Fail-open defect: when both the gc leg and the graalgo leg failed, run.py
compared two empty result sets and reported PASS ("0 tests agree") with
exit 0. A gate that validated nothing reported green.

This test drives run.py with fake toolchains (no graalgo build needed):
  - a fake GOROOT holding one trivial external test package
  - a fake `go` binary and a fake `graalgo-run` launcher whose behavior
    is chosen per scenario

and asserts the verdict.

Scenarios:
  - both legs fail (exit 1)        -> verdict ERROR, exit != 0 (the E1 case)
  - gc leg exits 0 with no parsable results -> verdict ERROR, exit != 0
  - both legs exit 0 with identical results -> verdict PASS, exit 0
    (positive control: the fix must not break the honest-PASS path)
  - default graalgo-run launcher   -> <repo>/internal/backend/graalgo-run
    (T18: the old default pointed at the removed phases/phase1 tree)

Scratch goes under tools/gate1/.tmp/ (gitignored), or $GATE1_TEST_TMP if
set. Never /tmp.

Usage: python3 tools/gate1/test_run_verdict.py
"""
import importlib.util
import os
import shutil
import stat
import subprocess
import sys
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))

FAKE_TEST_SRC = '''package fakepkg_test

import "fmt"

func TestFoo(t *testing.T) {
	if fmt.Sprint(1) != "1" {
		t.Errorf("bad")
	}
}

func TestBar(t *testing.T) {}
'''

# Fake `go` / `graalgo-run` behaviors per scenario.
FAIL_SCRIPT = "#!/bin/sh\necho 'fake leg: simulated driver failure' >&2\nexit 1\n"
EMPTY_SCRIPT = "#!/bin/sh\necho 'fake leg: ran, emitted no test lines'\nexit 0\n"
PASS_SCRIPT = (
    "#!/bin/sh\n"
    "printf 'PASS TestFoo\\nPASS TestBar\\nSUMMARY total=2 pass=2 fail=0 skip=0\\n'\n"
    "exit 0\n"
)


def _write_executable(path, content):
    with open(path, "w") as f:
        f.write(content)
    os.chmod(path, os.stat(path).st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)


def _load_run_module():
    spec = importlib.util.spec_from_file_location(
        "gate1_run", os.path.join(HERE, "run.py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


class VerdictTest(unittest.TestCase):
    def setUp(self):
        base = os.environ.get("GATE1_TEST_TMP") or os.path.join(HERE, ".tmp")
        self.work = os.path.join(base, f"verdict-test-{os.getpid()}")
        shutil.rmtree(self.work, ignore_errors=True)
        os.makedirs(self.work)

        # Fake GOROOT: src/fakepkg/fakepkg_test.go + bin/go
        self.goroot = os.path.join(self.work, "goroot")
        pkgdir = os.path.join(self.goroot, "src", "fakepkg")
        os.makedirs(pkgdir)
        with open(os.path.join(pkgdir, "fakepkg_test.go"), "w") as f:
            f.write(FAKE_TEST_SRC)
        os.makedirs(os.path.join(self.goroot, "bin"))
        self.fake_go = os.path.join(self.goroot, "bin", "go")

    def tearDown(self):
        shutil.rmtree(self.work, ignore_errors=True)

    def _run_gate(self, go_script, gr_script):
        _write_executable(self.fake_go, go_script)
        fake_gr = os.path.join(self.work, "fake-graalgo-run")
        _write_executable(fake_gr, gr_script)
        outdir = os.path.join(self.work, "out")
        proc = subprocess.run(
            [sys.executable, os.path.join(HERE, "run.py"), "fakepkg",
             "--goroot", self.goroot,
             "--graalgo-run", fake_gr,
             "--out", outdir],
            capture_output=True, text=True, timeout=120,
            env={**os.environ, "TMPDIR": self.work, "GOTMPDIR": self.work},
        )
        return proc

    def test_both_legs_fail_is_error_not_pass(self):
        """E1: both legs failing must not report PASS."""
        proc = self._run_gate(FAIL_SCRIPT, FAIL_SCRIPT)
        self.assertIn("VERDICT fakepkg: ERROR", proc.stdout,
                      f"fail-open: both legs failed yet verdict was not ERROR.\n"
                      f"rc={proc.returncode}\nstdout:\n{proc.stdout}\n"
                      f"stderr:\n{proc.stderr}")
        self.assertNotEqual(proc.returncode, 0,
                            "fail-open: run.py exited 0 while both legs failed")

    def test_gc_leg_empty_results_is_error(self):
        """gc exiting 0 with zero parsable results is not a PASS."""
        proc = self._run_gate(EMPTY_SCRIPT, EMPTY_SCRIPT)
        self.assertIn("VERDICT fakepkg: ERROR", proc.stdout,
                      f"empty gc results reported as non-ERROR.\n"
                      f"rc={proc.returncode}\nstdout:\n{proc.stdout}")
        self.assertNotEqual(proc.returncode, 0)

    def test_agreeing_legs_still_pass(self):
        """Positive control: honest agreement still reports PASS."""
        proc = self._run_gate(PASS_SCRIPT, PASS_SCRIPT)
        self.assertIn("VERDICT fakepkg: PASS", proc.stdout,
                      f"honest PASS broken.\nrc={proc.returncode}\n"
                      f"stdout:\n{proc.stdout}\nstderr:\n{proc.stderr}")
        self.assertEqual(proc.returncode, 0)

    def test_default_graalgo_run_launcher(self):
        """T18: the default launcher must be the in-repo backend launcher."""
        run_mod = _load_run_module()
        default = run_mod.default_graalgo_run()
        want = os.path.join("internal", "backend", "graalgo-run")
        self.assertTrue(default.endswith(want),
                        f"default graalgo-run does not point at the backend "
                        f"launcher: {default}")
        self.assertNotIn("phases", default,
                         f"default still references the removed phases/ tree: "
                         f"{default}")
        self.assertTrue(os.path.isfile(default),
                        f"default graalgo-run path does not exist: {default}")


if __name__ == "__main__":
    unittest.main(verbosity=2)
