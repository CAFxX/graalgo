#!/usr/bin/env python3
"""Tests for check_ratchets.py: a planted TODO or "unimplemented" stub over
budget must fail, a paid-down count must fail until the budget is lowered,
and --update must refuse to raise a budget.

Run: python3 .github/scripts/test_check_ratchets.py
"""

import atexit
import json
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
        b.write_text(json.dumps(budget))
    subprocess.run(["git", "-C", str(d), "add", "-A"], check=True)
    return d


class Gate(unittest.TestCase):
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

    def test_paid_down_requires_budget_update(self):
        d = repo({"tools/a.py": "ok\n"}, {"placeholder": {"tools/a.py": 2}})
        self.assertEqual(g.main(["x", str(d)]), 1)
        self.assertEqual(g.main(["x", "--update", str(d)]), 0)
        self.assertEqual(g.main(["x", str(d)]), 0)

    def test_update_refuses_raise(self):
        d = repo({"tools/a.py": f"# {TODO} {TODO}\n"}, {"placeholder": {"tools/a.py": 1}})
        self.assertEqual(g.main(["x", "--update", str(d)]), 1)


if __name__ == "__main__":
    unittest.main(verbosity=1)
