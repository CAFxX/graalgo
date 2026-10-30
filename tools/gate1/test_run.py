#!/usr/bin/env python3
"""Regression tests for tools/gate1/run.py + synthesize.py verdict logic.

Covers the Gate 1 correctness fixes (review 2026-10-30):
- P0-1: both drivers crashing must NOT pass (HARNESS_ERROR, exit != 0),
  and a comparison with zero parsed results on both sides must NOT pass.
- P1-4: (Makefile-level; covered by inspection — no `|| true` remains.)
- P1-5: synthesised count is compared to discover.json total_tests;
  a passing subset below MIN_COVERAGE becomes PARTIAL, and the skip
  list lands in verdict.txt.
- P1-6: a failed synthesis exits non-zero and never runs a stale
  driver from a reused --out dir.

The end-to-end tests use a fake GOROOT (test sources only) and stub
`go` / graalgo-run executables that print canned driver output instead
of compiling anything, so they run without the JVM or a real toolchain.

Run: python3 tools/gate1/test_run.py
"""
import json
import os
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import run as gate1_run  # noqa: E402

EXT_TEST = '''package mypkg_test

import "testing"

func TestOne(t *testing.T) {}
'''

INTERNAL_TEST = '''package mypkg

import "testing"

func TestIntA(t *testing.T) {}
func TestIntB(t *testing.T) {}
'''


def write_stub(path, log_name, mode_var, pass_output):
    """Shell stub: logs its invocation, then crashes or prints canned output."""
    with open(path, "w") as f:
        f.write("#!/bin/sh\n")
        f.write(f'echo invoked >> "$STUB_LOG_DIR/{log_name}"\n')
        f.write(f'if [ "${mode_var}" = crash ]; then echo "stub boom" >&2; exit 1; fi\n')
        f.write(f"printf '%b\\n' {pass_output!r}\n")
    os.chmod(path, os.stat(path).st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)


class DecideVerdictTest(unittest.TestCase):
    def test_both_crash_is_harness_error(self):
        verdict, _ = gate1_run.decide_verdict(1, 1, {}, {})
        self.assertEqual(verdict, "HARNESS_ERROR")

    def test_both_crash_with_results_still_harness_error(self):
        # Exit codes dominate: partial output from two crashed drivers
        # is not a measurement either.
        verdict, _ = gate1_run.decide_verdict(1, 1, {"T": "PASS"}, {})
        self.assertEqual(verdict, "HARNESS_ERROR")

    def test_both_zero_with_no_results_is_harness_error(self):
        # The old fall-through: empty comparison read as PASS.
        verdict, _ = gate1_run.decide_verdict(0, 0, {}, {})
        self.assertEqual(verdict, "HARNESS_ERROR")

    def test_gc_only_failure_is_skip(self):
        verdict, _ = gate1_run.decide_verdict(1, 0, {}, {"T": "PASS"})
        self.assertEqual(verdict, "SKIP")

    def test_graalgo_only_failure_is_diverge(self):
        verdict, _ = gate1_run.decide_verdict(0, 1, {"T": "PASS"}, {})
        self.assertEqual(verdict, "DIVERGE")

    def test_matching_results_pass(self):
        verdict, div = gate1_run.decide_verdict(0, 0, {"T": "PASS"}, {"T": "PASS"})
        self.assertEqual((verdict, div), ("PASS", []))

    def test_mismatch_diverges(self):
        verdict, div = gate1_run.decide_verdict(0, 0, {"T": "PASS"}, {"T": "FAIL"})
        self.assertEqual(verdict, "DIVERGE")
        self.assertEqual(div, [("T", "PASS", "FAIL")])

    def test_one_sided_result_diverges_as_missing(self):
        verdict, _ = gate1_run.decide_verdict(0, 0, {"T": "PASS"}, {})
        self.assertEqual(verdict, "DIVERGE")


class Gate1EndToEndTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="gate1-test-")
        self.addCleanup(shutil.rmtree, self.tmp, True)
        self.goroot = os.path.join(self.tmp, "goroot")
        self.pkgdir = os.path.join(self.goroot, "src", "mypkg")
        self.bindir = os.path.join(self.goroot, "bin")
        os.makedirs(self.pkgdir)
        os.makedirs(self.bindir)
        self.logdir = os.path.join(self.tmp, "logs")
        os.makedirs(self.logdir)
        self.outdir = os.path.join(self.tmp, "out")
        self.go_stub = os.path.join(self.bindir, "go")
        self.gr_stub = os.path.join(self.tmp, "graalgo-run")
        write_stub(self.go_stub, "go.log", "GC_MODE",
                   "PASS TestOne\nSUMMARY pass=1 fail=0 skip=0")
        write_stub(self.gr_stub, "gr.log", "GR_MODE",
                   "PASS TestOne\nSUMMARY pass=1 fail=0 skip=0")

    def write_pkg(self, internal=False):
        with open(os.path.join(self.pkgdir, "ext_test.go"), "w") as f:
            f.write(EXT_TEST)
        if internal:
            with open(os.path.join(self.pkgdir, "internal_test.go"), "w") as f:
                f.write(INTERNAL_TEST)

    def run_gate1(self, gc_mode="pass", gr_mode="pass", script=None):
        env = dict(os.environ)
        env["STUB_LOG_DIR"] = self.logdir
        env["GC_MODE"] = gc_mode
        env["GR_MODE"] = gr_mode
        script = script or os.path.join(HERE, "run.py")
        return subprocess.run(
            [sys.executable, script, "mypkg", "--goroot", self.goroot,
             "--graalgo-run", self.gr_stub, "--out", self.outdir],
            capture_output=True, text=True, env=env)

    def verdict_lines(self):
        with open(os.path.join(self.outdir, "verdict.txt")) as f:
            return f.read().splitlines()

    def test_both_crash_is_not_pass(self):
        # P0-1 regression: previously this fell through to PASS with
        # "0 tests agree".
        self.write_pkg()
        proc = self.run_gate1(gc_mode="crash", gr_mode="crash")
        self.assertNotEqual(proc.returncode, 0)
        self.assertEqual(self.verdict_lines()[0], "HARNESS_ERROR")
        self.assertIn("HARNESS_ERROR", proc.stdout)

    def test_passing_drivers_still_pass(self):
        self.write_pkg()
        proc = self.run_gate1()
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        lines = self.verdict_lines()
        self.assertEqual(lines[0], "PASS")
        self.assertIn("COVERAGE synthesised=1 total=1", lines[1])

    def test_thin_coverage_becomes_partial_with_skip_list(self):
        # P1-5: 1 of 3 discovered tests synthesised (internal file
        # skipped) -> coverage 0.33 < MIN_COVERAGE, so PARTIAL, and the
        # skipped file is named in verdict.txt.
        self.write_pkg(internal=True)
        proc = self.run_gate1()
        self.assertNotEqual(proc.returncode, 0)
        lines = self.verdict_lines()
        self.assertEqual(lines[0], "PARTIAL")
        self.assertIn("COVERAGE synthesised=1 total=3", lines[1])
        self.assertTrue(any("internal_test.go" in ln for ln in lines),
                        lines)

    def test_gc_crash_alone_stays_skip(self):
        self.write_pkg()
        proc = self.run_gate1(gc_mode="crash")
        self.assertNotEqual(proc.returncode, 0)
        self.assertEqual(self.verdict_lines()[0], "SKIP")


class SynthesizeFailureTest(unittest.TestCase):
    """P1-6: run.py must fail closed on synthesis failure and must never
    execute a stale driver left in a reused --out dir."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="gate1-synthfail-")
        self.addCleanup(shutil.rmtree, self.tmp, True)
        # Copy of the gate1 scripts so synthesize.py can be replaced by
        # a failing stub without touching the real tree.
        self.scripts = os.path.join(self.tmp, "scripts")
        os.makedirs(self.scripts)
        for name in ("run.py", "discover.py"):
            shutil.copy(os.path.join(HERE, name), os.path.join(self.scripts, name))
        shutil.copytree(os.path.join(HERE, "shim"), os.path.join(self.scripts, "shim"))
        goroot = os.path.join(self.tmp, "goroot")
        pkgdir = os.path.join(goroot, "src", "mypkg")
        bindir = os.path.join(goroot, "bin")
        os.makedirs(pkgdir)
        os.makedirs(bindir)
        with open(os.path.join(pkgdir, "ext_test.go"), "w") as f:
            f.write(EXT_TEST)
        self.logdir = os.path.join(self.tmp, "logs")
        os.makedirs(self.logdir)
        self.go_stub = os.path.join(bindir, "go")
        self.gr_stub = os.path.join(self.tmp, "graalgo-run")
        write_stub(self.go_stub, "go.log", "GC_MODE",
                   "PASS TestOne\nSUMMARY pass=1 fail=0 skip=0")
        write_stub(self.gr_stub, "gr.log", "GR_MODE",
                   "PASS TestOne\nSUMMARY pass=1 fail=0 skip=0")
        self.outdir = os.path.join(self.tmp, "out")
        os.makedirs(self.outdir)
        # Stale artefacts from a "previous run".
        with open(os.path.join(self.outdir, "driver.go"), "w") as f:
            f.write("// STALE driver from a previous run\n")
        with open(os.path.join(self.outdir, "verdict.txt"), "w") as f:
            f.write("PASS\n")
        self.goroot = goroot

    def run_with_synth_stub(self, stub_body):
        with open(os.path.join(self.scripts, "synthesize.py"), "w") as f:
            f.write(stub_body)
        env = dict(os.environ)
        env["STUB_LOG_DIR"] = self.logdir
        env["GC_MODE"] = "pass"
        env["GR_MODE"] = "pass"
        return subprocess.run(
            [sys.executable, os.path.join(self.scripts, "run.py"),
             "mypkg", "--goroot", self.goroot,
             "--graalgo-run", self.gr_stub, "--out", self.outdir],
            capture_output=True, text=True, env=env)

    def test_failing_synthesis_exits_nonzero_and_runs_nothing(self):
        proc = self.run_with_synth_stub(
            "#!/usr/bin/env python3\nimport sys\n"
            "print('boom', file=sys.stderr)\nsys.exit(3)\n")
        self.assertNotEqual(proc.returncode, 0)
        self.assertIn("synthesize failed", proc.stderr + proc.stdout)
        self.assertFalse(os.path.exists(os.path.join(self.outdir, "driver.go")),
                         "stale driver must be deleted, not reused")
        self.assertFalse(os.path.exists(os.path.join(self.logdir, "go.log")))
        self.assertFalse(os.path.exists(os.path.join(self.logdir, "gr.log")))

    def test_silent_synthesis_never_runs_stale_driver(self):
        # Synthesize "succeeds" but writes no driver (stale dir reuse).
        proc = self.run_with_synth_stub(
            "#!/usr/bin/env python3\nimport sys\nsys.exit(0)\n")
        self.assertNotEqual(proc.returncode, 0)
        self.assertFalse(os.path.exists(os.path.join(self.outdir, "driver.go")))
        self.assertFalse(os.path.exists(os.path.join(self.logdir, "go.log")))
        self.assertFalse(os.path.exists(os.path.join(self.logdir, "gr.log")))


class SynthesizeSidecarTest(unittest.TestCase):
    def test_sidecar_reports_coverage_and_skips(self):
        tmp = tempfile.mkdtemp(prefix="gate1-sidecar-")
        self.addCleanup(shutil.rmtree, tmp, True)
        ext = os.path.join(tmp, "ext_test.go")
        internal = os.path.join(tmp, "internal_test.go")
        with open(ext, "w") as f:
            f.write(EXT_TEST)
        with open(internal, "w") as f:
            f.write(INTERNAL_TEST)
        disc = {"package": "mypkg", "goroot": "/nonexistent",
                "total_tests": 3,
                "files": [{"path": ext}, {"path": internal}]}
        disc_path = os.path.join(tmp, "discover.json")
        with open(disc_path, "w") as f:
            json.dump(disc, f)
        out = os.path.join(tmp, "driver.go")
        proc = subprocess.run(
            [sys.executable, os.path.join(HERE, "synthesize.py"),
             disc_path, "--out", out,
             "--shim", os.path.join(HERE, "shim", "shim.go")],
            capture_output=True, text=True)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        with open(out + ".synth.json") as f:
            sidecar = json.load(f)
        self.assertEqual(sidecar["total_tests"], 3)
        self.assertEqual(sidecar["synthesized_tests"], 1)
        self.assertAlmostEqual(sidecar["skip_ratio"], 2 / 3)
        self.assertEqual(len(sidecar["skipped"]), 1)
        self.assertIn("internal_test.go", sidecar["skipped"][0]["file"])


if __name__ == "__main__":
    unittest.main()
