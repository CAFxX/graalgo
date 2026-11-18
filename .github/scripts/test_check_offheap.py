#!/usr/bin/env python3
"""Tests for check_offheap.py: NIO/FFM/off-heap use outside graalgo.syscall
must fail; inside it must pass; grandfathered entries must not go stale.

Run: python3 .github/scripts/test_check_offheap.py
"""

import atexit
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import check_offheap as g  # noqa: E402

GF = sorted(g.GRANDFATHERED)


def tree(files, grandfathered=True):
    d = Path(tempfile.mkdtemp())
    atexit.register(shutil.rmtree, d, True)
    if grandfathered:
        for rel in GF:
            files.setdefault(rel, "import java.nio.channels.FileChannel;\n")
    for rel, content in files.items():
        p = d / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(content)
    return d


class Gate(unittest.TestCase):
    def test_clean(self):
        self.assertEqual(g.scan(tree({"internal/backend/truffle/src/main/java/graalgo/A.java": "class A {}\n"})), [])

    def test_syscall_package_allowed(self):
        d = tree({"internal/backend/truffle/src/main/java/graalgo/syscall/Sys.java":
                  "import java.lang.foreign.Arena;\nMemorySegment s;\n"})
        self.assertEqual(g.scan(d), [])

    def test_memorysegment_elsewhere_fails(self):
        d = tree({"internal/backend/truffle/src/main/java/graalgo/runtime/X.java": "MemorySegment m;\n"})
        self.assertEqual(len(g.scan(d)), 1)

    def test_nio_channels_in_interop_fails(self):
        d = tree({"internal/backend/interop/src/main/java/graalgo/p3x/Y.java":
                  "import java.nio.channels.SocketChannel;\n"})
        self.assertEqual(len(g.scan(d)), 1)

    def test_lookalike_package_fails(self):
        d = tree({"internal/backend/truffle/src/main/java/graalgo/syscallx/Z.java": "Arena a;\n"})
        self.assertEqual(len(g.scan(d)), 1)

    def test_stale_grandfather_fails(self):
        d = tree({GF[0]: "class Clean {}\n"})
        errs = g.scan(d)
        self.assertTrue(any("stale" in e for e in errs), errs)


if __name__ == "__main__":
    unittest.main(verbosity=1)
