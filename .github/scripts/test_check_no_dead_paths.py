#!/usr/bin/env python3
"""Tests for check_no_dead_paths.py: the gate must fail on a planted
executable reference and pass on a clean tree (and on comment-only or
opted-out mentions).

Run: python3 .github/scripts/test_check_no_dead_paths.py
"""

import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import check_no_dead_paths as g  # noqa: E402

DEAD = "phases" + "/phase1/graalgo-run"
BANNED = "hidden" + "_files/torture/logs"


def tree(files):
    d = Path(tempfile.mkdtemp())
    for rel, content in files.items():
        p = d / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(content)
    return d


class Gate(unittest.TestCase):
    def test_clean_tree_passes(self):
        d = tree({"tools/x/run.sh": "#!/bin/bash\nexec internal/backend/graalgo-sync-run \"$@\"\n",
                  "Makefile": "all:\n\techo ok\n"})
        self.assertEqual(g.scan(d), [])
        self.assertEqual(g.main(["x", str(d)]), 0)

    def test_planted_shell_reference_fails(self):
        d = tree({"tools/x/run.sh": f"#!/bin/bash\nRUN=$HOME/{DEAD}\n"})
        hits = g.scan(d)
        self.assertEqual(len(hits), 1)
        self.assertIn("tools/x/run.sh:2", hits[0])
        self.assertEqual(g.main(["x", str(d)]), 1)

    def test_banned_scratch_dir_in_go_and_python_fails(self):
        d = tree({"tools/y/main.go": f'package main\nvar p = "{BANNED}"\n',
                  "test/z/run.py": f'OUT = "{BANNED}"\n',
                  ".github/workflows/w.yml": f"run: make -C {DEAD}\n"})
        self.assertEqual(len(g.scan(d)), 3)

    def test_shebang_file_without_extension_is_scanned(self):
        d = tree({"internal/backend/launcher": f"#!/usr/bin/env bash\nexec {DEAD}\n"})
        self.assertEqual(len(g.scan(d)), 1)

    def test_comments_docs_data_and_optout_are_ignored(self):
        d = tree({"tools/x/run.sh": f"#!/bin/bash\n# moved from {DEAD}\n  // {DEAD}\n",
                  "tools/x/README.md": f"see {DEAD}\n",
                  "tools/x/inv.txt": f"by {DEAD}\n",
                  "test/x/testdata/fixture.sh": f"{DEAD}\n",
                  "tools/x/check.py": f'BAN = "{BANNED}"  # dead-path-ok\n'})
        self.assertEqual(g.scan(d), [])


    def test_widened_scope_java_json_frontend_cmd(self):
        # G9: .java/.json files and the internal/frontend, cmd trees were
        # not scanned; javadoc stays a comment.
        for rel, content in {
            "internal/frontend/x.go": f'const p = "{DEAD}"\n',
            "cmd/graalgo/main.go": f'var p = "{BANNED}"\n',
            "internal/backend/truffle/src/main/java/A.java": f'String p = "{DEAD}";\n',
            "test/differential/backends.json": f'{{"run": "{DEAD}"}}\n',
        }.items():
            d = tree({rel: content})
            self.assertEqual(len(g.scan(d)), 1, rel)
        d = tree({"internal/backend/truffle/src/main/java/B.java": f"/**\n * see {DEAD}\n */\nclass B {{}}\n",
                  "internal/frontend/go/types/api.go": f'const p = "{DEAD}"\n'})
        self.assertEqual(g.scan(d), [])

if __name__ == "__main__":
    unittest.main()
