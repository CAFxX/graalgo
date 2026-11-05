#!/usr/bin/env python3
"""Tests for run.py (Gate 1 differential runner).

Two layers:
  - Unit tests for decide_verdict / parse_go_test_results / run_cmd.
  - End-to-end tests for main() with stub executables: a fake `go`
    and a fake graalgo launcher print canned `go test -v` output and
    exit with a chosen code, and a fake `graalgo` binary writes a
    canned driver to the -emit-driver path. No JVM, no real toolchain.
"""
import os
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import run  # noqa: E402


def canned(outcome, name="TestOne"):
    """Canned `go test -v` stdout for one test with the given outcome."""
    if outcome is None:
        return ""
    return (f"=== RUN   {name}\n"
            f"--- {outcome}: {name} (0.00s)\n"
            f"{outcome}\n")


class TestParseGoTestResults(unittest.TestCase):
    def test_parses_pass_fail_skip(self):
        out = ("=== RUN   TestA\n--- PASS: TestA (0.00s)\n"
               "=== RUN   TestB\n--- FAIL: TestB (0.01s)\n"
               "=== RUN   TestC\n--- SKIP: TestC (0.00s)\n"
               "FAIL\n")
        self.assertEqual(run.parse_go_test_results(out),
                         {"TestA": "PASS", "TestB": "FAIL", "TestC": "SKIP"})

    def test_ignores_non_result_lines(self):
        out = "=== RUN   TestA\nok  \tmypkg\t0.001s\nPASS\n"
        self.assertEqual(run.parse_go_test_results(out), {})

    def test_empty_output(self):
        self.assertEqual(run.parse_go_test_results(""), {})

    def test_subtest_name(self):
        out = "--- PASS: TestA/subcase (0.00s)\n"
        self.assertEqual(run.parse_go_test_results(out),
                         {"TestA/subcase": "PASS"})


class TestDecideVerdict(unittest.TestCase):
    def test_both_agree_pass(self):
        v, _ = run.decide_verdict(0, 0, {"TestOne": "PASS"},
                                  {"TestOne": "PASS"}, "", "")
        self.assertEqual(v, "PASS")

    def test_identical_failure_is_agreement(self):
        # The gate compares graalgo against gc; it does not require
        # the tests themselves to pass.
        v, _ = run.decide_verdict(1, 1, {"TestOne": "FAIL"},
                                  {"TestOne": "FAIL"}, "", "")
        self.assertEqual(v, "PASS")

    def test_both_skip_agrees(self):
        v, _ = run.decide_verdict(0, 0, {"TestOne": "SKIP"},
                                  {"TestOne": "SKIP"}, "", "")
        self.assertEqual(v, "PASS")

    def test_one_differs(self):
        v, lines = run.decide_verdict(0, 1, {"TestOne": "PASS"},
                                      {"TestOne": "FAIL"}, "", "")
        self.assertEqual(v, "DIVERGE")
        self.assertTrue(any("TestOne" in ln for ln in lines))

    def test_missing_on_one_side(self):
        v, _ = run.decide_verdict(0, 0, {"TestOne": "PASS", "TestTwo": "PASS"},
                                  {"TestOne": "PASS"}, "", "")
        self.assertEqual(v, "DIVERGE")

    def test_both_empty_both_fail_is_error(self):
        # C-P1-7 regression: two failing drivers must never certify PASS.
        v, lines = run.decide_verdict(1, 1, {}, {}, "gc boom", "gr boom")
        self.assertEqual(v, "ERROR")
        self.assertTrue(any("no test results" in ln for ln in lines))

    def test_both_empty_both_zero_is_error(self):
        v, _ = run.decide_verdict(0, 0, {}, {}, "", "")
        self.assertEqual(v, "ERROR")

    def test_gc_empty_failed_is_skip(self):
        v, _ = run.decide_verdict(1, 0, {}, {"TestOne": "PASS"},
                                  "gc boom", "")
        self.assertEqual(v, "SKIP")

    def test_gc_empty_zero_is_error(self):
        v, _ = run.decide_verdict(0, 0, {}, {"TestOne": "PASS"}, "", "")
        self.assertEqual(v, "ERROR")

    def test_graalgo_empty_failed_is_diverge(self):
        v, _ = run.decide_verdict(0, 1, {"TestOne": "PASS"}, {},
                                  "", "gr boom")
        self.assertEqual(v, "DIVERGE")

    def test_graalgo_empty_zero_is_error(self):
        v, _ = run.decide_verdict(0, 0, {"TestOne": "PASS"}, {}, "", "")
        self.assertEqual(v, "ERROR")


class TestRunCmd(unittest.TestCase):
    def test_missing_binary_is_spawn_failure(self):
        rc, out, err = run.run_cmd(["/nonexistent-binary-xyz"])
        self.assertEqual(rc, run.SPAWN_FAILED_RC)
        self.assertIn("SPAWN FAILED", err)
        self.assertEqual(out, "")

    def test_successful_command(self):
        rc, out, err = run.run_cmd(["echo", "hi"])
        self.assertEqual(rc, 0)
        self.assertEqual(out.strip(), "hi")


class StubHarness(unittest.TestCase):
    """End-to-end main() tests with stub executables."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = self.tmp.name
        self.pkg = "mypkg"
        self.pkg_dir = os.path.join(self.root, "goroot", "src", self.pkg)
        os.makedirs(self.pkg_dir)
        self.log = os.path.join(self.root, "argv.log")
        self.outdir = os.path.join(self.root, "out")
        self.env = dict(os.environ, STUB_LOG=self.log)

    def tearDown(self):
        self.tmp.cleanup()

    def _stub(self, name, body):
        path = os.path.join(self.root, name)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w") as f:
            f.write(body)
        os.chmod(path, 0o755)
        return path

    def _printer_stub(self, name, stdout, rc, stderr=""):
        # Prints canned stdout/stderr, logs argv, exits rc.
        body = ("#!/usr/bin/env python3\n"
                "import os, sys\n"
                "open(os.environ['STUB_LOG'], 'a').write(' '.join(sys.argv[1:]) + '\\n')\n"
                f"sys.stdout.write({stdout!r})\n"
                f"sys.stderr.write({stderr!r})\n"
                f"sys.exit({rc})\n")
        return self._stub(name, body)

    def _graalgo_stub(self):
        # Writes a canned driver to the -emit-driver path, logs argv.
        body = ("#!/usr/bin/env python3\n"
                "import os, sys\n"
                "open(os.environ['STUB_LOG'], 'a').write(' '.join(sys.argv[1:]) + '\\n')\n"
                "args = sys.argv[1:]\n"
                "out = args[args.index('-emit-driver') + 1]\n"
                "open(out, 'w').write('package main\\nfunc main() {}\\n')\n")
        return self._stub("fake-graalgo", body)

    def _run_main(self, go_out, go_rc, gr_out, gr_rc,
                  go_err="", gr_err=""):
        # run.py derives the go binary from --goroot, so the fake go
        # lives at <goroot>/bin/go.
        go = self._printer_stub(os.path.join("goroot", "bin", "go"),
                                go_out, go_rc, go_err)
        gr = self._printer_stub("fake-graalgo-run", gr_out, gr_rc, gr_err)
        graalgo = self._graalgo_stub()
        argv = ["run.py", self.pkg,
                "--goroot", os.path.join(self.root, "goroot"),
                "--graalgo", graalgo,
                "--graalgo-run", gr,
                "--out", self.outdir]
        with mock.patch.object(sys, "argv", argv):
            with mock.patch.dict(os.environ, {"STUB_LOG": self.log}):
                rc = run.main()
        with open(os.path.join(self.outdir, "verdict.txt")) as f:
            verdict = f.read().strip()
        return rc, verdict

    def test_both_fail_is_error(self):
        rc, verdict = self._run_main("", 1, "", 1,
                                     go_err="gc boom", gr_err="gr boom")
        self.assertEqual(verdict, "ERROR")
        self.assertEqual(rc, 1)

    def test_both_pass_agree(self):
        rc, verdict = self._run_main(canned("PASS"), 0, canned("PASS"), 0)
        self.assertEqual(verdict, "PASS")
        self.assertEqual(rc, 0)

    def test_identical_fail_agrees(self):
        rc, verdict = self._run_main(canned("FAIL"), 1, canned("FAIL"), 1)
        self.assertEqual(verdict, "PASS")
        self.assertEqual(rc, 0)

    def test_gc_fail_only_is_skip(self):
        rc, verdict = self._run_main("", 1, canned("PASS"), 0,
                                     go_err="gc boom")
        self.assertEqual(verdict, "SKIP")
        self.assertEqual(rc, 1)

    def test_graalgo_fail_only_is_diverge(self):
        rc, verdict = self._run_main(canned("PASS"), 0, "", 1,
                                     gr_err="gr boom")
        self.assertEqual(verdict, "DIVERGE")
        self.assertEqual(rc, 1)

    def test_argv_passed_to_stubs(self):
        self._run_main(canned("PASS"), 0, canned("PASS"), 0)
        with open(self.log) as f:
            logged = f.read()
        self.assertIn("test -emit-driver", logged)
        self.assertIn(self.pkg_dir, logged)
        self.assertIn("test -v -count=1 " + self.pkg_dir, logged)
        self.assertIn("--test-package-dir=" + self.pkg_dir, logged)


class TestMainValidation(unittest.TestCase):
    def test_missing_graalgo_binary(self):
        tmp = tempfile.mkdtemp()
        argv = ["run.py", "mypkg", "--goroot", tmp,
                "--graalgo", os.path.join(tmp, "no-such-graalgo"),
                "--graalgo-run", "/bin/true", "--out", tmp]
        with mock.patch.object(sys, "argv", argv):
            with self.assertRaises(SystemExit) as cm:
                run.main()
        self.assertIn("no-such-graalgo", str(cm.exception))

    def test_missing_launcher(self):
        tmp = tempfile.mkdtemp()
        argv = ["run.py", "mypkg", "--goroot", tmp,
                "--graalgo", "/bin/true",
                "--graalgo-run", os.path.join(tmp, "no-such-launcher"),
                "--out", tmp]
        with mock.patch.object(sys, "argv", argv):
            with self.assertRaises(SystemExit) as cm:
                run.main()
        self.assertIn("no-such-launcher", str(cm.exception))

    def test_default_launcher_prefers_sync_run(self):
        got = run._default_graalgo_run()
        sync_run = os.path.join(run.REPO, "internal", "backend",
                                "graalgo-sync-run")
        plain = os.path.join(run.REPO, "internal", "backend", "graalgo-run")
        if os.path.isfile(sync_run):
            self.assertEqual(got, sync_run)
        else:
            self.assertEqual(got, plain)


class TestBothFailSubprocess(unittest.TestCase):
    """The headline C-P1-7 case through the real CLI entry point."""

    def test_both_fail_subprocess(self):
        harness = StubHarness("test_both_fail_is_error")
        harness.setUp()
        try:
            go = harness._printer_stub(
                os.path.join("goroot", "bin", "go"), "", 1, "gc boom")
            gr = harness._printer_stub("fake-graalgo-run", "", 1, "gr boom")
            graalgo = harness._graalgo_stub()
            cmd = [sys.executable,
                   os.path.join(os.path.dirname(os.path.abspath(__file__)),
                                "run.py"),
                   "mypkg",
                   "--goroot", os.path.join(harness.root, "goroot"),
                   "--graalgo", graalgo,
                   "--graalgo-run", gr,
                   "--out", harness.outdir]
            env = dict(os.environ, STUB_LOG=harness.log)
            p = subprocess.run(cmd, capture_output=True, text=True,
                               env=env, timeout=60)
            self.assertEqual(p.returncode, 1)
            self.assertIn("VERDICT mypkg: ERROR", p.stdout)
            self.assertNotIn("0 tests agree", p.stdout)
            with open(os.path.join(harness.outdir, "verdict.txt")) as f:
                self.assertEqual(f.read().strip(), "ERROR")
        finally:
            harness.tearDown()


if __name__ == "__main__":
    unittest.main()
