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
# A superseded id of the fixture registry (assembled for the same reason:
# tools/req lint rule (b) rejects live references to superseded ids).
OLD = "GG" + "-0012"


def tree(files):
    d = Path(tempfile.mkdtemp())
    atexit.register(shutil.rmtree, d, True)
    for rel, content in files.items():
        p = d / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(content)
    return d


REG = {"requirements/GG-0011.yaml": "id: GG-0011\nstatus: active\n",
       "requirements/GG-SCHED-0001.yaml": "id: GG-SCHED-0001\n",
       f"requirements/{OLD}.yaml": f"id: {OLD}\nstatus: superseded\nsuperseded_by: GG-0011\n"}


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

    def test_superseded_id_cited_live_fails(self):
        d = tree({**REG, "docs/a.md": f"intro\n\nThe frontend (`{OLD}`) is verbatim.\n\nend\n",
                  "internal/x.go": f"// verifies: {OLD}\n"})
        errs = g.scan(d, ["docs/a.md", "internal/x.go"])
        self.assertEqual(len(errs), 2, errs)
        self.assertTrue(all("superseded/retired" in e for e in errs), errs)

    def test_superseded_id_marked_as_history_passes(self):
        d = tree({**REG,
                  "docs/a.md": f"{OLD} was superseded by GG-0011.\n",
                  "docs/b.md": f"The old rule\n(`{OLD}`)\n— history only.\n",
                  "docs/dev/adr/ADR-1.md": f"Decision: {OLD} stands.\n",
                  "requirements/GG-0011.yaml": f"id: GG-0011\nstatus: active\nhistory: [{OLD}]\n"})
        self.assertEqual(g.scan(d, ["docs/a.md", "docs/b.md", "docs/dev/adr/ADR-1.md",
                                    "requirements/GG-0011.yaml"]), [])

    def test_binary_skipped(self):
        d = tree(REG)
        (d / "bin.dat").write_bytes(b"\0\0" + FAKE.encode())
        self.assertEqual(g.scan(d, ["bin.dat"]), [])


if __name__ == "__main__":
    unittest.main(verbosity=1)
