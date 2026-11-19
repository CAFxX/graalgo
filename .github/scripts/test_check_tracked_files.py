#!/usr/bin/env python3
"""Tests for check_tracked_files.py: planted oversized / ELF / .pyc files
must fail the gate; an allowlisted one must pass; allowlist entries need a
reason and must not be stale.

Run: python3 .github/scripts/test_check_tracked_files.py
"""

import atexit
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import check_tracked_files as g  # noqa: E402


def repo(files, allow=None):
    d = Path(tempfile.mkdtemp())
    atexit.register(shutil.rmtree, d, True)
    subprocess.run(["git", "init", "-q", str(d)], check=True)
    for rel, content in files.items():
        p = d / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_bytes(content)
    if allow is not None:
        a = d / g.ALLOWLIST
        a.parent.mkdir(parents=True, exist_ok=True)
        a.write_text(allow)
    subprocess.run(["git", "-C", str(d), "add", "-A"], check=True)
    return d


BIG = b"x" * (g.LIMIT + 1)


class Gate(unittest.TestCase):
    def test_clean(self):
        self.assertEqual(g.scan(repo({"a.go": b"package a\n"})), [])

    def test_big_file_fails(self):
        errs = g.scan(repo({"logs/run.log": BIG}))
        self.assertTrue(any("logs/run.log" in e for e in errs), errs)

    def test_elf_fails(self):
        errs = g.scan(repo({"tools/req/req": b"\x7fELF\x02\x01\x01" + b"\0" * 64}))
        self.assertTrue(any("ELF" in e for e in errs), errs)

    def test_pyc_and_so_fail(self):
        errs = g.scan(repo({"x/__pycache__/m.cpython-312.pyc": b"abc", "lib/a.so": b"abc"}))
        self.assertEqual(len(errs), 2, errs)

    def test_tree_log_cap(self):
        # Many small logs, each under the per-file limit, over the per-tree cap.
        chunk = b"x" * (200 * 1024)
        files = {f"docs/arch/spikes/s{i}/run{i}.log": chunk for i in range(8)}
        errs = g.scan(repo(files))
        self.assertTrue(any("docs/arch/spikes/: tracked logs total" in e for e in errs), errs)
        # The same bytes as non-log files, or split across two trees, pass.
        self.assertEqual(g.scan(repo({f"docs/arch/spikes/s{i}/run{i}.md": chunk for i in range(8)})), [])
        half = {f"docs/a/t{i}/r.log": chunk for i in range(4)}
        half.update({f"docs/b/t{i}/r.log": chunk for i in range(4)})
        self.assertEqual(g.scan(repo(half)), [])

    def test_allowlisted_passes(self):
        d = repo({"lib/big.jar": BIG}, allow="lib/*.jar  # pinned jars\n")
        self.assertEqual(g.scan(d), [])

    def test_allowlist_needs_reason(self):
        d = repo({"lib/big.jar": BIG}, allow="lib/*.jar\n")
        errs = g.scan(d)
        self.assertTrue(any("without a '# reason'" in e for e in errs), errs)

    def test_stale_allowlist_entry_fails(self):
        d = repo({"a.go": b"package a\n"}, allow="lib/*.jar  # pinned jars\n")
        errs = g.scan(d)
        self.assertTrue(any("stale entry" in e for e in errs), errs)

    def test_main_exit_code(self):
        self.assertEqual(g.main(["x", str(repo({"big.bin": BIG}))]), 1)


if __name__ == "__main__":
    unittest.main(verbosity=1)
