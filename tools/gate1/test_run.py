#!/usr/bin/env python3
"""Regression tests for the gate1 harness (tools/gate1/run.py + Makefile).

Covers the three P1 defects filed by the 2026-11-06 independent review:
  R-P1-9  (C1): both backends failing must never report PASS.
  R-P1-10 (C2): `make gate1-pilot` must fail when any package fails.
  R-P1-11 (C3): direct `run.py <pkg>` must resolve the current launcher.

The end-to-end tests use a fixture GOROOT (one trivial external test
package) plus fake `go` / `graalgo-run` backends that never compile
anything — they just emit a driver summary or fail on demand via
FAKE_GC_FAIL / FAKE_GR_FAIL. No GraalVM, no network, no real toolchain.

Run: python3 tools/gate1/test_run.py   (from the repo root, or anywhere;
paths are derived from this file's location).
"""
import os
import shutil
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, HERE)

import run  # noqa: E402  (tools/gate1/run.py under test)

FAKE_GO = """#!/bin/sh
# Fake `go run`: prints a passing driver summary, or fails on demand.
# Never compiles anything.
if [ -n "$FAKE_GC_FAIL" ]; then echo "fake gc: driver failed" >&2; exit 1; fi
printf 'PASS TestOne\\nSUMMARY pass=1 fail=0 skip=0\\n'
exit 0
"""

FAKE_GRAALGO_RUN = """#!/bin/sh
# Fake graalgo-run: prints a passing driver summary, or fails on demand.
# Never runs anything.
if [ -n "$FAKE_GR_FAIL" ]; then echo "fake graalgo: driver failed" >&2; exit 1; fi
printf 'PASS TestOne\\nSUMMARY pass=1 fail=0 skip=0\\n'
exit 0
"""

FAKE_TEST_GO = """package fakepkg_test

import "testing"

func TestOne(t *testing.T) {}
"""


def write_executable(path, content):
    with open(path, "w") as f:
        f.write(content)
    os.chmod(path, 0o755)


class Gate1TestBase(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="gate1-test-")
        self.addCleanup(shutil.rmtree, self.tmp, True)

    def make_fixture_goroot(self):
        """A minimal GOROOT with one external test package + fake `go`."""
        goroot = os.path.join(self.tmp, "goroot")
        pkgdir = os.path.join(goroot, "src", "fakepkg")
        bindir = os.path.join(goroot, "bin")
        os.makedirs(pkgdir)
        os.makedirs(bindir)
        with open(os.path.join(pkgdir, "fakepkg_test.go"), "w") as f:
            f.write(FAKE_TEST_GO)
        write_executable(os.path.join(bindir, "go"), FAKE_GO)
        return goroot

    def make_fake_graalgo_run(self, name="fake-graalgo-run"):
        path = os.path.join(self.tmp, name)
        write_executable(path, FAKE_GRAALGO_RUN)
        return path

    def run_gate1(self, goroot, graalgo_run=None, extra_env=None):
        """Run run.py against the fixture package; return CompletedProcess."""
        outdir = os.path.join(self.tmp, "out")
        cmd = [sys.executable, os.path.join(HERE, "run.py"),
               "fakepkg", "--goroot", goroot, "--out", outdir]
        if graalgo_run is not None:
            cmd += ["--graalgo-run", graalgo_run]
        env = dict(os.environ)
        env.pop("FAKE_GC_FAIL", None)
        env.pop("FAKE_GR_FAIL", None)
        if extra_env:
            env.update(extra_env)
        return subprocess.run(cmd, capture_output=True, text=True,
                              env=env, timeout=120)


class TestDecideVerdict(Gate1TestBase):
    def test_both_drivers_fail_is_never_pass(self):
        # R-P1-9: the old code compared two empty result sets, found no
        # divergences, and reported PASS with exit 0.
        verdict, divergences, note = run.decide_verdict(1, 1, {}, {})
        self.assertEqual(verdict, "DIVERGE")
        self.assertIn("both drivers failed", note)

    def test_both_drivers_fail_nonzero_codes(self):
        # Timeouts (-1) and distinct codes are still "both failed".
        verdict, _, note = run.decide_verdict(-1, 2, {}, {})
        self.assertEqual(verdict, "DIVERGE")
        self.assertIn("both drivers failed", note)

    def test_both_drivers_fail_despite_partial_results(self):
        # Even if a failing driver printed some lines, there is no valid
        # comparison to make.
        verdict, _, _ = run.decide_verdict(1, 1, {"TestOne": "PASS"}, {})
        self.assertEqual(verdict, "DIVERGE")

    def test_gc_fail_only_is_skip(self):
        verdict, _, note = run.decide_verdict(1, 0, {}, {"TestOne": "PASS"})
        self.assertEqual(verdict, "SKIP")
        self.assertIn("gc driver failed", note)

    def test_graalgo_fail_only_is_diverge(self):
        verdict, _, note = run.decide_verdict(0, 1, {"TestOne": "PASS"}, {})
        self.assertEqual(verdict, "DIVERGE")
        self.assertIn("graalgo driver failed", note)

    def test_both_pass_agreeing_is_pass(self):
        verdict, divergences, _ = run.decide_verdict(
            0, 0, {"TestOne": "PASS"}, {"TestOne": "PASS"})
        self.assertEqual(verdict, "PASS")
        self.assertEqual(divergences, [])

    def test_both_pass_disagreeing_is_diverge(self):
        verdict, divergences, _ = run.decide_verdict(
            0, 0, {"TestOne": "PASS"}, {"TestOne": "FAIL"})
        self.assertEqual(verdict, "DIVERGE")
        self.assertEqual(divergences, [("TestOne", "PASS", "FAIL")])

    def test_missing_test_is_diverge(self):
        verdict, divergences, _ = run.decide_verdict(
            0, 0, {"TestOne": "PASS", "TestTwo": "PASS"}, {"TestOne": "PASS"})
        self.assertEqual(verdict, "DIVERGE")
        self.assertEqual(divergences, [("TestTwo", "PASS", "MISSING")])


class TestDefaultGraalgoRun(Gate1TestBase):
    def test_default_is_current_launcher(self):
        # R-P1-11: the default must be the live launcher, not the deleted
        # phases/phase1 tree.
        path = run.default_graalgo_run()
        self.assertEqual(
            path,
            os.path.join(REPO, "internal", "backend", "graalgo-run"))
        self.assertNotIn("phases", path)

    def test_default_launcher_exists_and_is_executable(self):
        path = run.default_graalgo_run()
        self.assertTrue(os.path.isfile(path), f"missing: {path}")
        self.assertTrue(os.access(path, os.X_OK), f"not executable: {path}")


class TestRunPyEndToEnd(Gate1TestBase):
    def test_both_backends_fail_reports_diverge_not_pass(self):
        # R-P1-9 end to end: exit code must be non-zero and the verdict
        # must not be PASS.
        goroot = self.make_fixture_goroot()
        fake_gr = self.make_fake_graalgo_run()
        proc = self.run_gate1(goroot, fake_gr,
                              {"FAKE_GC_FAIL": "1", "FAKE_GR_FAIL": "1"})
        self.assertNotEqual(proc.returncode, 0)
        self.assertIn("VERDICT fakepkg: DIVERGE", proc.stdout)
        self.assertIn("both drivers failed", proc.stdout)
        self.assertNotIn("VERDICT fakepkg: PASS", proc.stdout)

    def test_happy_path_still_passes(self):
        goroot = self.make_fixture_goroot()
        fake_gr = self.make_fake_graalgo_run()
        proc = self.run_gate1(goroot, fake_gr)
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("VERDICT fakepkg: PASS", proc.stdout)

    def test_gc_fail_only_skips(self):
        goroot = self.make_fixture_goroot()
        fake_gr = self.make_fake_graalgo_run()
        proc = self.run_gate1(goroot, fake_gr, {"FAKE_GC_FAIL": "1"})
        self.assertNotEqual(proc.returncode, 0)
        self.assertIn("VERDICT fakepkg: SKIP", proc.stdout)

    def test_graalgo_fail_only_diverges(self):
        goroot = self.make_fixture_goroot()
        fake_gr = self.make_fake_graalgo_run()
        proc = self.run_gate1(goroot, fake_gr, {"FAKE_GR_FAIL": "1"})
        self.assertNotEqual(proc.returncode, 0)
        self.assertIn("VERDICT fakepkg: DIVERGE", proc.stdout)

    def test_default_launcher_resolves_without_flag(self):
        # R-P1-11 end to end: copy tools/gate1 into a scratch repo layout
        # with a fake launcher at internal/backend/graalgo-run, then run
        # the copy WITHOUT --graalgo-run. The old default
        # <repo>/phases/phase1/graalgo-run does not exist there, so the
        # pre-fix code crashed with FileNotFoundError.
        scratch = os.path.join(self.tmp, "repo")
        shutil.copytree(HERE, os.path.join(scratch, "tools", "gate1"))
        launcher_dir = os.path.join(scratch, "internal", "backend")
        os.makedirs(launcher_dir)
        write_executable(os.path.join(launcher_dir, "graalgo-run"),
                         FAKE_GRAALGO_RUN)
        goroot = self.make_fixture_goroot()
        outdir = os.path.join(self.tmp, "out-default")
        cmd = [sys.executable, os.path.join(scratch, "tools", "gate1", "run.py"),
               "fakepkg", "--goroot", goroot, "--out", outdir]
        env = dict(os.environ)
        env.pop("FAKE_GC_FAIL", None)
        env.pop("FAKE_GR_FAIL", None)
        proc = subprocess.run(cmd, capture_output=True, text=True,
                              env=env, timeout=120)
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("VERDICT fakepkg: PASS", proc.stdout)

    def test_missing_launcher_fails_loudly(self):
        # A bad --graalgo-run must fail with a clear message naming the
        # path, not an uncaught traceback from deep in the run.
        goroot = self.make_fixture_goroot()
        missing = os.path.join(self.tmp, "no-such-launcher")
        proc = self.run_gate1(goroot, missing)
        self.assertNotEqual(proc.returncode, 0)
        self.assertIn(missing, proc.stderr)
        self.assertIn("graalgo-run not found", proc.stderr)
        self.assertNotIn("Traceback", proc.stderr)


def extract_gate1_pilot_recipe(makefile_path):
    """Return the gate1-pilot target (header + recipe lines) verbatim."""
    with open(makefile_path) as f:
        lines = f.read().splitlines()
    start = next(i for i, line in enumerate(lines)
                 if line.startswith("gate1-pilot:"))
    recipe = [lines[start]]
    for line in lines[start + 1:]:
        if line.startswith("\t"):
            recipe.append(line)
        else:
            break
    return "\n".join(recipe) + "\n"


class TestGate1PilotMakefile(Gate1TestBase):
    def run_pilot_recipe(self, stub_body):
        """Run the REAL gate1-pilot recipe from the repo Makefile in a
        sandbox, with a stub `python3` on PATH. Returns (proc, log)."""
        recipe = extract_gate1_pilot_recipe(os.path.join(REPO, "Makefile"))
        self.assertNotIn("|| true", recipe,
                         "gate1-pilot still swallows failures (R-P1-10)")
        bindir = os.path.join(self.tmp, "bin")
        os.makedirs(bindir)
        log = os.path.join(self.tmp, "stub.log")
        stub = ("#!/bin/sh\n"
                f'echo "$@" >> "{log}"\n' + stub_body)
        write_executable(os.path.join(bindir, "python3"), stub)
        pilot_mk = os.path.join(self.tmp, "pilot.mk")
        with open(pilot_mk, "w") as f:
            f.write("GATE1_PKGS := pkgfail pkgok\n")
            f.write(recipe)
            f.write("build:\n\t@true\n")
        env = dict(os.environ)
        env["PATH"] = bindir + ":" + env["PATH"]
        proc = subprocess.run(["make", "-f", pilot_mk, "gate1-pilot"],
                              capture_output=True, text=True, env=env,
                              cwd=self.tmp, timeout=120)
        invoked = []
        if os.path.exists(log):
            with open(log) as f:
                invoked = [line.split()[1] for line in f if line.split()]
        return proc, invoked

    def test_pilot_fails_when_first_package_fails(self):
        # R-P1-10: the failure must fail the target even when it is NOT
        # the last package — a bare removal of `|| true` would not fix
        # this, because a for-loop's exit status is its last iteration's.
        stub_body = ('for a in "$@"; do '
                     'if [ "$a" = "pkgfail" ]; then exit 1; fi; done\n'
                     'exit 0\n')
        proc, invoked = self.run_pilot_recipe(stub_body)
        self.assertEqual(invoked, ["pkgfail", "pkgok"],
                         "pilot must still run every package after a failure")
        self.assertNotEqual(proc.returncode, 0,
                            "gate1-pilot must fail when a package fails:\n"
                            + proc.stdout + proc.stderr)
        self.assertNotIn("Gate 1 pilot complete", proc.stdout)

    def test_pilot_succeeds_when_all_packages_pass(self):
        stub_body = 'exit 0\n'
        proc, invoked = self.run_pilot_recipe(stub_body)
        self.assertEqual(invoked, ["pkgfail", "pkgok"])
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("Gate 1 pilot complete", proc.stdout)


if __name__ == "__main__":
    unittest.main()
