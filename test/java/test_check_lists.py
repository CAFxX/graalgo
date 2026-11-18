#!/usr/bin/env python3
"""Tests for check_lists.py and quarantine_verdict.py: an unregistered
*Test.java, a class in neither list, a quarantined fast class, a failure
outside the quarantine, a quarantined class that now passes, and a class
that never completed must each fail.

Run: python3 test/java/test_check_lists.py
"""

import atexit
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import check_lists as cl  # noqa: E402
import quarantine_verdict as qv  # noqa: E402

RUNNER = """package graalgo.test;
public final class Runner { static { TESTS.add(ATest.class); TESTS.add(graalgo.x.BTest.class); } }
"""


def module(extra=()):
    d = Path(tempfile.mkdtemp())
    atexit.register(shutil.rmtree, d, True)
    root = d / "src/test/java"
    files = {"graalgo/test/Runner.java": RUNNER,
             "graalgo/test/ATest.java": "package graalgo.test;\n",
             "graalgo/x/BTest.java": "package graalgo.x;\n"}
    for rel in extra:
        files[rel] = "package " + ".".join(rel.split("/")[:-1]) + ";\n"
    for rel, content in files.items():
        p = root / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(content)
    return root / "graalgo/test/Runner.java"


LISTED = ["graalgo.test.ATest", "graalgo.x.BTest"]


class Lists(unittest.TestCase):
    def test_consistent(self):
        self.assertEqual(cl.check([(module(), LISTED)]), [])

    def test_unregistered_source_fails(self):
        bad = cl.check([(module(["graalgo/p3reflect/GcMaskTest.java"]), LISTED)])
        self.assertTrue(any("GcMaskTest is registered in no runner" in b for b in bad), bad)

    def test_unlisted_registered_fails(self):
        bad = cl.check([(module(), LISTED[:1])])
        self.assertTrue(any("BTest appears 0 times" in b for b in bad), bad)

    def test_quarantine_must_be_slow(self):
        q = [["graalgo.test.ATest", "r", "o", "M3"]]
        self.assertTrue(cl.check([(module(), LISTED)], q, frozenset()))
        self.assertEqual(cl.check([(module(), LISTED)], q, frozenset(["graalgo.test.ATest"])), [])

    def test_quarantine_format(self):
        d = Path(tempfile.mkdtemp())
        atexit.register(shutil.rmtree, d, True)
        (d / "q.txt").write_text("graalgo.A | reason only\n")
        with self.assertRaises(ValueError):
            cl.read_quarantine(d / "q.txt")


    def test_quarantine_owner_and_expiry(self):
        rec = "Tracks: T1 builds the path; WP-MUT; milestones M3 and M1a."
        today = __import__("datetime").date(2026, 11, 18)
        ok = [["graalgo.A", "r", "T1", "M3"], ["graalgo.B", "r", "lane:m1a-closeout", "2026-12-01"],
              ["graalgo.C", "r", "WP-MUT", "M1a"]]
        self.assertEqual(cl.quarantine_problems(ok, rec, today), [])
        for entry, want in [
                (["graalgo.D", "r", "toolexec M3 cutover (v1 deletion)", "M3"], "not a track/work-package id"),
                (["graalgo.E", "r", "W9", "M3"], "not defined in RECOVERY.md"),
                (["graalgo.F", "r", "T1", "2026-11-01"], "expired on 2026-11-01"),
                (["graalgo.G", "r", "T1", "M9"], "M9 is not in RECOVERY.md"),
                (["graalgo.H", "r", "T1", "someday"], "neither YYYY-MM-DD nor a milestone"),
                (["graalgo.I", "r", "T1", "2026-13-45"], "not a valid date")]:
            bad = cl.quarantine_problems([entry], rec, today)
            self.assertTrue(any(want in b for b in bad), (entry, bad))


LOG = """PASS ATest.testOne (1 ms)
CLASS graalgo.test.ATest 3 ms
FAIL BTest.testTwo (2 ms): java.lang.AssertionError: x
PASS BTest.testThree (1 ms)
CLASS graalgo.x.BTest 5 ms
"""
Q = [["graalgo.x.BTest", "v1 fixture rot", "backend", "M3"]]


class Verdict(unittest.TestCase):
    def test_green_with_quarantined_failure(self):
        bad, notes = qv.verdict(Q, LISTED, [LOG])
        self.assertEqual(bad, [])
        self.assertEqual(len(notes), 1)

    def test_failure_outside_quarantine(self):
        bad, _ = qv.verdict([], LISTED, [LOG])
        self.assertTrue(any("outside the quarantine" in b for b in bad), bad)

    def test_quarantined_now_passing(self):
        bad, _ = qv.verdict(Q, LISTED, [LOG.replace("FAIL BTest.testTwo (2 ms): java.lang.AssertionError: x",
                                                    "PASS BTest.testTwo (2 ms)")])
        self.assertTrue(any("now passes" in b for b in bad), bad)

    def test_class_never_completed(self):
        bad, _ = qv.verdict(Q, LISTED + ["graalgo.y.CTest"], [LOG])
        self.assertTrue(any("CTest: did not complete" in b for b in bad), bad)

    def test_test_output_infra_lines_are_not_runner_infra(self):
        # A test's own output (the typeparam adapter prints "INFRA  x.go")
        # is not a ClassListRunner infrastructure failure.
        bad, _ = qv.verdict(Q, LISTED, ["INFRA     tparam1.go\n" + LOG])
        self.assertEqual(bad, [])

    def test_infra_line_fails(self):
        bad, _ = qv.verdict(Q, LISTED, [LOG + "INFRA cannot load test class graalgo.z.D\n"])
        self.assertTrue(any("infrastructure" in b for b in bad), bad)


if __name__ == "__main__":
    unittest.main(verbosity=1)
