#!/usr/bin/env python3
"""Self-test of test/lib/gates.py: plants a failing gate, a gate whose CPU
time blows through its baseline, and parallel gates whose output must not
interleave, and checks gates.py reports each (run by `make check`)."""
import contextlib
import importlib.util
from typing import Any
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
GATES = os.path.join(HERE, "gates.py")

def _load(name) -> Any:
    """Import test/lib/<name>.py by path (test/lib is not a package)."""
    spec = importlib.util.spec_from_file_location(name, os.path.join(HERE, name + ".py"))
    assert spec and spec.loader
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


gates = _load("gates")

MAKEFILE = """\
all: generate build ok1 \\
\tok2 burn
generate build graalgo java-test-classes:
\t@echo setup $@
ok1:
\t@echo ok1-a; sleep 0.3; echo ok1-b
ok2:
\t@echo ok2-a; sleep 0.3; echo ok2-b
burn:
\t@python3 -c 'import time; t=time.process_time(); any(time.process_time()-t>0.6 for _ in iter(int, 1))'
\t@echo burned
bad:
\t@echo about to fail; exit 7
"""


def jload(path):
    with open(path) as f:
        return json.load(f)


def jdump(path, v):
    with open(path, "w") as f:
        json.dump(v, f)


class GatesTest(unittest.TestCase):
    def setUp(self):
        self.d = tempfile.mkdtemp()
        with open(os.path.join(self.d, "Makefile"), "w") as f:
            f.write(MAKEFILE)

    def tearDown(self):
        subprocess.call(["rm", "-rf", self.d])

    def run_gates(self, *args):
        cmd = [sys.executable, GATES, "run", "--dir", self.d, "--logdir", self.d] + list(args)
        p = subprocess.run(cmd, capture_output=True, text=True)
        return p.returncode, p.stdout + p.stderr

    def test_all_targets_follows_continuations(self):
        self.assertEqual(gates.all_targets(os.path.join(self.d, "Makefile")),
                         ["generate", "build", "ok1", "ok2", "burn"])

    def test_parallel_pass_and_cpu_measured(self):
        out_json = os.path.join(self.d, "r.json")
        rc, out = self.run_gates("-j", "3", "--json", out_json, "--ratchet", "off")
        self.assertEqual(rc, 0, out)
        res = jload(out_json)["targets"]
        self.assertEqual(set(res), {"generate", "build", "graalgo", "java-test-classes", "ok1", "ok2", "burn"})
        self.assertGreaterEqual(res["burn"]["cpu"], 0.5)
        # each gate's block is contiguous: parallel output never interleaves
        for g in ("ok1", "ok2"):
            self.assertRegex(out, r"== %s\n%s-a\n%s-b\n== PASS %s" % (g, g, g, g))

    def test_failing_gate_fails_the_run(self):
        rc, out = self.run_gates("ok1", "bad", "ok2")
        self.assertEqual(rc, 1, out)
        self.assertIn("FAIL bad", out)
        self.assertIn("about to fail", out)
        self.assertIn("gates: FAILED: bad", out)
        self.assertIn("not run (an earlier failure): ok2", out)
        rc, out = self.run_gates("all", "bad")
        self.assertEqual(rc, 1, out)
        self.assertIn("PASS burn", out)
        rc, out = self.run_gates("-k", "ok1", "bad", "ok2")
        self.assertEqual(rc, 1, out)
        self.assertIn("PASS ok2", out)

    def test_ratchet(self):
        base = os.path.join(self.d, "base.json")
        jdump(base, {"cpu": {"burn": 0.01, "ok1": 100}})
        # the slack (30 s) keeps a sub-second gate from failing: shrink it
        old = gates.FAIL_SLACK
        try:
            self.assertEqual(gates.verdict(0.6, 0.01, "fail", 2.0), "OK")  # within slack
            gates.FAIL_SLACK = 0.0
            self.assertEqual(gates.verdict(0.6, 0.01, "fail", 2.0), "FAIL")
            self.assertEqual(gates.verdict(0.6, 0.01, "warn", 2.0), "OK")  # warn slack 5 s
            self.assertEqual(gates.verdict(30.0, 10.0, "warn", 2.0), "SLOWER")
            self.assertEqual(gates.verdict(30.0, 10.0, "fail", 2.0), "FAIL")
            self.assertEqual(gates.verdict(19.0, 10.0, "fail", 2.0), "SLOWER")
            self.assertEqual(gates.verdict(9.0, None, "fail", 2.0), "NEW")
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertTrue(gates.report({"burn": {"rc": 0, "wall": 1, "cpu": 0.6}},
                                             {"burn": 0.01}, "fail", 2.0, []))
                self.assertFalse(gates.report({"burn": {"rc": 0, "wall": 1, "cpu": 0.6}},
                                              {"burn": 0.01}, "warn", 2.0, []))
        finally:
            gates.FAIL_SLACK = old
        rc, out = self.run_gates("--baseline", base, "--ratchet", "warn", "burn", "ok1")
        self.assertEqual(rc, 0, out)
        self.assertRegex(out, r"burn\s+0\s+[\d.]+\s+[\d.]+\s+0\.0\s+OK")
        self.assertRegex(out, r"generate .* NEW")

    def test_serial_groups(self):
        self.assertTrue(gates.conflicts("check", {"test-ci": None}))
        self.assertFalse(gates.conflicts("check", {"test-m1a": None}))
        self.assertFalse(gates.conflicts("test-m1a", {"check": None}))

    def test_docs_tier(self):
        full = ["snapshot-check", "check", "test-toolexec", "test-java-fast", "test-m1a"]
        self.assertEqual(gates.tier_targets(["README.md", "docs/arch/x.md"], full), ["check"])
        self.assertEqual(gates.tier_targets(["docs/dev/bugs.md"], full), ["check", "test-toolexec"])
        # code, a doc inside a code tree, an unknown or empty change: full
        for changed in (["docs/a.md", "Makefile"], ["test/m1a/README.md"], ["stdlib/x/README.md"],
                        [".github/workflows/ci.yml"], [], None):
            self.assertEqual(gates.tier_targets(changed, full), full, changed)

    def test_record(self):
        rj, base = os.path.join(self.d, "r.json"), os.path.join(self.d, "b.json")
        jdump(rj, {"targets": {"a": {"rc": 0, "wall": 3, "cpu": 2}, "b": {"rc": 1, "wall": 1, "cpu": 1}}})
        jdump(base, {"cpu": {"c": 5}})
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(gates.main(["record", rj, "--baseline", base]), 0)
        self.assertEqual(jload(base)["cpu"], {"a": 2, "c": 5})


if __name__ == "__main__":
    unittest.main(verbosity=1)
