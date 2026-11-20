#!/usr/bin/env python3
"""Tests for check_ratchets.py: a planted TODO or "unimplemented" stub over
budget must fail (including the new path's `new Unsupported(` and
`.unsupportedf(` stubs), a paid-down count must fail until the budget is
lowered, --update must refuse to raise a budget, and a budget raised above
the git base's must fail.

Run: python3 .github/scripts/test_check_ratchets.py
"""

import atexit
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import check_ratchets as g  # noqa: E402

TODO = "TO" + "DO"


def repo(files, budget=None):
    d = Path(tempfile.mkdtemp())
    atexit.register(shutil.rmtree, d, True)
    subprocess.run(["git", "init", "-q", str(d)], check=True)
    for rel, content in files.items():
        p = d / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(content)
    if budget is not None:
        b = d / g.BUDGET
        b.parent.mkdir(parents=True, exist_ok=True)
        b.write_text(json.dumps(dict(budget, _definitions=g.definitions())))
    subprocess.run(["git", "-C", str(d), "add", "-A"], check=True)
    return d


def commit(d, msg):
    env = ["-c", "user.name=t", "-c", "user.email=t@example.invalid"]
    subprocess.run(["git", "-C", str(d), "add", "-A"], check=True)
    subprocess.run(["git", "-C", str(d)] + env + ["commit", "-q", "-m", msg], check=True)
    return subprocess.run(["git", "-C", str(d), "rev-parse", "HEAD"], check=True,
                          capture_output=True, text=True).stdout.strip()


class Gate(unittest.TestCase):
    def setUp(self):
        saved = os.environ.pop("GATE_BASE", None)
        if saved is not None:
            self.addCleanup(os.environ.__setitem__, "GATE_BASE", saved)

    def test_within_budget(self):
        d = repo({"tools/a.py": f"# {TODO} x\n"}, {"placeholder": {"tools/a.py": 1}})
        self.assertEqual(g.main(["x", str(d)]), 0)

    def test_new_placeholder_fails(self):
        d = repo({"tools/a.sh": f"# {TODO} x\n", "tools/b.go": f"// FIX" + "ME\n"}, {})
        errs = g.compare(g.count(d, g.tracked(d)), {})
        self.assertEqual(len(errs), 2, errs)
        self.assertEqual(g.main(["x", str(d)]), 1)

    def test_unimplemented_stub_fails_tests_exempt(self):
        d = repo({"internal/x/A.java": 'throw new RuntimeException("not implemented: foo");\n',
                  "internal/x/src/test/B.java": '"unimplemented"\n',
                  "internal/y/a_test.go": '"unimplemented"\n'}, {})
        errs = g.compare(g.count(d, g.tracked(d)), {})
        self.assertEqual(errs, ["unimplemented: internal/x/A.java: 1 > budget 0 (debt may only go down)"])

    def test_timing_entries_count_and_shrink_only(self):
        reg = "test/differential/srctest-exclusions.yaml"
        text = "- file: a.go\n  timing: true\n- file: b.go\n  timing: true\n- file: c.go\n"
        d = repo({reg: text, "docs/x.yaml": "timing: true\n"}, {})
        errs = g.compare(g.count(d, g.tracked(d)), {})
        self.assertEqual(errs, [f"timing-excl: {reg}: 2 > budget 0 (debt may only go down)"])
        self.assertEqual(g.compare(g.count(d, g.tracked(d)), {"timing-excl": {reg: 2}}), [])
        (d / reg).write_text("- file: a.go\n  timing: true\n")
        errs = g.compare(g.count(d, g.tracked(d)), {"timing-excl": {reg: 2}})
        self.assertEqual(len(errs), 1, errs)
        self.assertIn("paid down", errs[0])

    def test_paid_down_requires_budget_update(self):
        d = repo({"tools/a.py": "ok\n"}, {"placeholder": {"tools/a.py": 2}})
        self.assertEqual(g.main(["x", str(d)]), 1)
        self.assertEqual(g.main(["x", "--update", str(d)]), 0)
        self.assertEqual(g.main(["x", str(d)]), 0)

    def test_new_path_stubs_count(self):
        d = repo({"internal/backend/x/NodeFactory.java":
                  'throw new Unsupported("range over func");\nthrow new Unsupported("x");\n',
                  "internal/toolexec/emit/expr.go": 'e.unsupportedf(pos, "conversion")\n',
                  "internal/toolexec/emit/expr_test.go": 'e.unsupportedf(pos, "t")\n'}, {})
        errs = g.compare(g.count(d, g.tracked(d)), {})
        self.assertEqual(sorted(errs), [
            "unimplemented: internal/backend/x/NodeFactory.java: 2 > budget 0 (debt may only go down)",
            "unimplemented: internal/toolexec/emit/expr.go: 1 > budget 0 (debt may only go down)"])

    def test_definition_change_requires_rebaseline(self):
        d = repo({"tools/a.py": "ok\n"}, {})
        b = d / g.BUDGET
        b.write_text(json.dumps({"_definitions": {"placeholder": "old", "unimplemented": "old"}}))
        self.assertEqual(g.main(["x", str(d)]), 1)
        self.assertEqual(g.main(["x", "--update", str(d)]), 0)
        self.assertEqual(g.main(["x", str(d)]), 0)

    def test_budget_raised_above_base_fails(self):
        d = repo({"tools/a.py": f"# {TODO}\n"}, {"placeholder": {"tools/a.py": 1}})
        base = commit(d, "base")
        subprocess.run(["git", "-C", str(d), "checkout", "-q", "-b", "lane"], check=True)
        # Plant a second TODO AND raise the budget by hand: the tree-vs-budget
        # check is satisfied, only the base comparison catches it.
        (d / "tools/a.py").write_text(f"# {TODO}\n# {TODO}\n")
        (d / g.BUDGET).write_text(json.dumps(
            {"placeholder": {"tools/a.py": 2}, "_definitions": g.definitions()}))
        self.assertEqual(g.compare(g.count(d, g.tracked(d)), json.loads((d / g.BUDGET).read_text())), [])
        self.assertEqual(g.main(["x", str(d)]), 1)
        # A new file's budget entry is a raise from 0 as well.
        (d / "tools/a.py").write_text(f"# {TODO}\n")
        (d / "tools/b.py").write_text(f"# {TODO}\n")
        subprocess.run(["git", "-C", str(d), "add", "-A"], check=True)
        (d / g.BUDGET).write_text(json.dumps(
            {"placeholder": {"tools/a.py": 1, "tools/b.py": 1}, "_definitions": g.definitions()}))
        self.assertEqual(g.main(["x", str(d)]), 1)
        # GATE_BASE overrides the merge-base: committed on master itself.
        subprocess.run(["git", "-C", str(d), "checkout", "-q", "master"], check=True)
        commit(d, "raise on master")
        self.assertEqual(g.main(["x", str(d)]), 0)  # control: merge-base == HEAD
        os.environ["GATE_BASE"] = base
        self.addCleanup(os.environ.pop, "GATE_BASE", None)
        self.assertEqual(g.main(["x", str(d)]), 1)

    def test_lowered_budget_passes_against_base(self):
        d = repo({"tools/a.py": f"# {TODO}\n# {TODO}\n"}, {"placeholder": {"tools/a.py": 2}})
        commit(d, "base")
        (d / "tools/a.py").write_text(f"# {TODO}\n")
        self.assertEqual(g.main(["x", "--update", str(d)]), 0)
        self.assertEqual(g.main(["x", str(d)]), 0)

    def test_update_refuses_raise(self):
        d = repo({"tools/a.py": f"# {TODO} {TODO}\n"}, {"placeholder": {"tools/a.py": 1}})
        self.assertEqual(g.main(["x", "--update", str(d)]), 1)


if __name__ == "__main__":
    unittest.main(verbosity=1)
