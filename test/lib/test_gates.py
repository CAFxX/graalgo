#!/usr/bin/env python3
"""Self-test of test/lib/gates.py: plants a failing gate, a gate whose CPU
time blows through its baseline, and parallel gates whose output must not
interleave, and checks gates.py reports each (run by `make check`).

The runs pin their environment (hermetic_env): gates.py reads
GITHUB_ACTIONS (output framing), GATE_* (jobs, cache state, tier base) and
make reads MAKEFLAGS — a self-test that inherits CI's or an enclosing
make's values tests that environment instead of the code (c35a2888 went
red exactly so: under GitHub Actions the output was ::group:: framed)."""
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


# Inherited variables that change what gates.py or the inner make do.
LEAKY_ENV = ("GITHUB_ACTIONS", "CI", "GATE_JOBS", "GATE_CACHE", "GATE_TIER_BASE",
             "MAKE", "MAKEFLAGS", "MFLAGS", "MAKELEVEL", "MAKEOVERRIDES")


def hermetic_env(**extra):
    env = {k: v for k, v in os.environ.items() if k not in LEAKY_ENV}
    env.update(extra)
    return env


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

    def run_gates(self, *args, env=None):
        cmd = [sys.executable, GATES, "run", "--dir", self.d, "--logdir", self.d] + list(args)
        p = subprocess.run(cmd, capture_output=True, text=True, env=env or hermetic_env())
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
        self.assertNotIn("::group::", out)

    def test_output_format(self):
        # github framing: auto under GITHUB_ACTIONS=true, or forced; each
        # gate's block stays contiguous inside its group
        group = r"::group::PASS ok1 \(wall \d+s, cpu \d+s\)\nok1-a\nok1-b\n::endgroup::\nPASS ok1"
        for args, env in ((["--format", "auto"], hermetic_env(GITHUB_ACTIONS="true")),
                          (["--format", "github"], hermetic_env())):
            rc, out = self.run_gates(*args, "-j", "2", "ok1", "ok2", env=env)
            self.assertEqual(rc, 0, out)
            self.assertRegex(out, group)
            self.assertNotIn("== ok1\n", out)
        # plain framing: forced even under GitHub Actions
        rc, out = self.run_gates("--format", "plain", "ok1", env=hermetic_env(GITHUB_ACTIONS="true"))
        self.assertEqual(rc, 0, out)
        self.assertRegex(out, r"== ok1\nok1-a\nok1-b\n== PASS ok1")
        self.assertNotIn("::group::", out)

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
                                             {"burn": (0.01, False)}, "fail", 2.0, []))
                self.assertFalse(gates.report({"burn": {"rc": 0, "wall": 1, "cpu": 0.6}},
                                              {"burn": (0.01, False)}, "warn", 2.0, []))
        finally:
            gates.FAIL_SLACK = old
        rc, out = self.run_gates("--baseline", base, "--ratchet", "warn", "burn", "ok1")
        self.assertEqual(rc, 0, out)
        self.assertRegex(out, r"burn\s+0\s+[\d.]+\s+[\d.]+\s+0\.0\s+OK")
        self.assertRegex(out, r"generate .* NEW")

    def test_ratchet_compares_like_with_like(self):
        # an environment's baseline keeps warm- and cold-cache numbers apart
        base = os.path.join(self.d, "base.json")
        jdump(base, {"cpu": {"w": 10.0, "both": 10.0}, "cpu_cold": {"c": 80.0, "both": 80.0}})
        warm, cold = gates.load_baseline(base, "warm"), gates.load_baseline(base, "cold")
        # warm run: warm numbers, falling back to the (never lower) cold one
        self.assertEqual(warm, {"w": (10.0, False), "both": (10.0, False), "c": (80.0, True)})
        # cold run: cold numbers only; a warm-only target is NEW, not failed
        self.assertEqual(cold, {"c": (80.0, True), "both": (80.0, True)})
        # the planted CI incident: a cold run (cpu 60) against a warm-only
        # baseline (10) failed; against the cold section it passes, while a
        # real doubling of a cold run (200 > 2x80+30) still fails
        with contextlib.redirect_stdout(io.StringIO()) as buf:
            self.assertTrue(gates.report({"both": {"rc": 0, "wall": 1, "cpu": 60.0}},
                                         {"both": (10.0, False)}, "fail", 2.0, []))
            self.assertFalse(gates.report({"both": {"rc": 0, "wall": 1, "cpu": 60.0},
                                           "w": {"rc": 0, "wall": 1, "cpu": 999.0}},
                                          cold, "fail", 2.0, []))
            self.assertTrue(gates.report({"both": {"rc": 0, "wall": 1, "cpu": 200.0}},
                                         cold, "fail", 2.0, []))
            # a warm run that doubles is caught against the warm number
            self.assertTrue(gates.report({"both": {"rc": 0, "wall": 1, "cpu": 51.0}},
                                         warm, "fail", 2.0, []))
        self.assertRegex(buf.getvalue(), r"w\s+0\s+1\.0\s+999\.0\s+-\s+NEW")
        self.assertRegex(buf.getvalue(), r"both .*80\.0c\s+FAIL")
        # end to end: --cache (or GATE_CACHE) picks the section
        jdump(base, {"cpu": {"burn": 0.01}, "cpu_cold": {}})
        rc, out = self.run_gates("--baseline", base, "--ratchet", "fail", "--fail-ratio", "1",
                                 "--no-setup", "burn", env=hermetic_env(GATE_CACHE="cold"))
        self.assertEqual(rc, 0, out)
        self.assertIn("gates: cold caches", out)
        self.assertRegex(out, r"burn .* NEW")
        rc, out = self.run_gates("--cache", "bogus", "burn")
        self.assertEqual(rc, 2, out)
        rc, out = self.run_gates("--no-setup", "burn", env=hermetic_env(GATE_CACHE="bogus"))
        self.assertEqual(rc, 2, out)

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

    def test_shards_partition(self):
        # LPT: longest first, each to the lightest shard; a gate the
        # baseline does not know weighs SHARD_DEFAULT_WALL
        walls = {"a": 100, "b": 60, "c": 50, "d": 10}
        shards, load = gates.shard_targets(["d", "c", "b", "a", "new"], 2, walls)
        # a(100) -> 0; b(60) -> 1; c(50) -> 1 (60 < 100); new(30) -> 0
        # (100 < 110); d(10) -> 1 (110 < 130)
        self.assertEqual(shards, [["a", "new"], ["b", "c", "d"]])
        self.assertEqual(load, [100 + gates.SHARD_DEFAULT_WALL, 120])
        # more shards than gates: the extra ones are empty, never a gate twice
        self.assertEqual(gates.shard_targets(["a"], 3, walls)[0], [["a"], [], []])
        # the real list: for every N and both environments' baselines the
        # shards are disjoint and cover every gate of the per-push set
        root = os.path.dirname(os.path.dirname(HERE))
        full = [t for t in gates.all_targets(os.path.join(root, "Makefile")) + ["test-diffharness"]
                if t not in gates.SETUP]
        for base in ("duration-baseline.json", "duration-baseline-ci.json"):
            for cache in ("warm", "cold"):
                walls = gates.load_walls(os.path.join(root, "test", base), cache)
                for n in range(1, 7):
                    got = gates.shard_targets(full, n, walls)[0]
                    flat = [t for s in got for t in s]
                    self.assertEqual(sorted(flat), sorted(full), (base, cache, n))
        for bad in ("0/2", "3/2", "2", "a/b", ""):
            with self.assertRaises(ValueError):
                gates.parse_shard(bad)

    def test_shard_run(self):
        base = os.path.join(self.d, "base.json")
        jdump(base, {"wall": {"ok1": 50, "ok2": 40, "burn": 30}})
        # shard 1/2 = ok1 alone; shard 2/2 = ok2 + burn (40 + 30 > 50, but
        # burn goes to the lighter shard 2 at 40 < 50); each with SETUP
        rc, out = self.run_gates("--baseline", base, "--shard", "1/2", "--ratchet", "off")
        self.assertEqual(rc, 0, out)
        self.assertIn("PASS ok1", out)
        self.assertIn("PASS generate", out)
        self.assertNotIn("PASS ok2", out)
        self.assertNotIn("PASS burn", out)
        rc, out = self.run_gates("--baseline", base, "--ratchet", "off", env=hermetic_env(GATE_SHARD="2/2"))
        self.assertEqual(rc, 0, out)
        self.assertIn("PASS ok2", out)
        self.assertIn("PASS burn", out)
        self.assertNotIn("PASS ok1", out)
        # a planted failure in a shard fails that shard
        rc, out = self.run_gates("--baseline", base, "--shard", "1/1", "ok1", "bad")
        self.assertEqual(rc, 1, out)
        self.assertIn("FAIL bad", out)
        # an empty shard runs nothing, not even SETUP, and passes
        rc, out = self.run_gates("--baseline", base, "--shard", "3/3", "ok1", "ok2")
        self.assertEqual(rc, 0, out)
        self.assertNotIn("PASS", out)
        rc, out = self.run_gates("--shard", "4/3", "ok1")
        self.assertEqual(rc, 2, out)

    def test_record(self):
        rj, base = os.path.join(self.d, "r.json"), os.path.join(self.d, "b.json")
        jdump(rj, {"targets": {"a": {"rc": 0, "wall": 3, "cpu": 2}, "b": {"rc": 1, "wall": 1, "cpu": 1}}})
        jdump(base, {"cpu": {"c": 5}})
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(gates.main(["record", rj, "--baseline", base]), 0)
        self.assertEqual(jload(base)["cpu"], {"a": 2, "c": 5})
        # a cold run is filed under the cold sections, the warm ones untouched
        jdump(rj, {"cache": "cold", "targets": {"a": {"rc": 0, "wall": 9, "cpu": 7}}})
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(gates.main(["record", rj, "--baseline", base]), 0)
        b = jload(base)
        self.assertEqual((b["cpu"], b["cpu_cold"], b["wall_cold"]), ({"a": 2, "c": 5}, {"a": 7}, {"a": 9}))


if __name__ == "__main__":
    unittest.main(verbosity=1)
