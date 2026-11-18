#!/usr/bin/env python3
"""Tests for check_gg_ids.py: a planted citation of an id that is not in
requirements/ must fail; known ids (plain and namespaced) must pass.

Run: python3 .github/scripts/test_check_gg_ids.py
"""

import atexit
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import check_gg_ids as g  # noqa: E402

# Fake ids are assembled at run time so tools/req lint does not see them.
FAKE = "GG" + "-0777"
FAKE_NS = "GG" + "-WAKE-0042"


def tree(files):
    d = Path(tempfile.mkdtemp())
    atexit.register(shutil.rmtree, d, True)
    for rel, content in files.items():
        p = d / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(content)
    return d


REG = {"requirements/GG-0011.yaml": "id: GG-0011\n",
       "requirements/GG-SCHED-0001.yaml": "id: GG-SCHED-0001\n"}


class Gate(unittest.TestCase):
    def test_known_ids_pass(self):
        d = tree({**REG, "docs/a.md": "GG-0011 and GG-SCHED-0001\n"})
        self.assertEqual(g.scan(d, ["docs/a.md"]), [])

    def test_unknown_id_fails(self):
        d = tree({**REG, "internal/x.go": "// verifies: " + FAKE + "\n"})
        errs = g.scan(d, ["internal/x.go"])
        self.assertEqual(len(errs), 1, errs)
        self.assertIn(FAKE, errs[0])

    def test_unknown_namespaced_id_fails(self):
        d = tree({**REG, "docs/a.md": FAKE_NS + "\n"})
        self.assertEqual(len(g.scan(d, ["docs/a.md"])), 1)

    def test_binary_skipped(self):
        d = tree(REG)
        (d / "bin.dat").write_bytes(b"\0\0" + FAKE.encode())
        self.assertEqual(g.scan(d, ["bin.dat"]), [])


if __name__ == "__main__":
    unittest.main(verbosity=1)
