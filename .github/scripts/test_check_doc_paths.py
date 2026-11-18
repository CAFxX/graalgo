#!/usr/bin/env python3
"""Tests for check_doc_paths.py: a planted backticked path that does not
exist must fail; existing paths, "(planned)" / historical lines, archived
docs, fenced code and Go std package names must pass.

Run: python3 .github/scripts/test_check_doc_paths.py
"""

import atexit
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import check_doc_paths as g  # noqa: E402


def tree(files):
    d = Path(tempfile.mkdtemp())
    atexit.register(shutil.rmtree, d, True)
    for rel, content in files.items():
        p = d / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(content)
    return d


def scan(files, docs):
    return g.scan(tree(files), docs)


class Gate(unittest.TestCase):
    def test_existing_path_passes(self):
        self.assertEqual(scan({"internal/toolexec/x.go": "", "docs/a.md": "see `internal/toolexec/x.go`\n"},
                              ["docs/a.md"]), [])

    def test_missing_path_fails(self):
        errs = scan({"docs/a.md": "the IR lives in `internal/ir/`\n"}, ["docs/a.md"])
        self.assertEqual(len(errs), 1, errs)
        self.assertIn("internal/ir/", errs[0])

    def test_doc_relative_path(self):
        files = {"docs/b.md": "", "docs/a.md": "`./b.md` and `../docs/missing.md`\n"}
        errs = scan(files, ["docs/a.md"])
        self.assertEqual(len(errs), 1, errs)
        self.assertIn("missing.md", errs[0])

    def test_planned_and_historical_opt_out(self):
        files = {"docs/a.md": "`internal/new/` (planned)\n`tools/diffharness/` was moved\n"}
        self.assertEqual(scan(files, ["docs/a.md"]), [])

    def test_fenced_code_skipped(self):
        files = {"docs/a.md": "```\n`internal/nope/`\n```\n"}
        self.assertEqual(scan(files, ["docs/a.md"]), [])

    def test_non_repo_tokens_skipped(self):
        files = {"docs/a.md": "`net/http` `go build ./...` `$HOME/x` `<pkg>/y` `a.b`\n"}
        self.assertEqual(scan(files, ["docs/a.md"]), [])

    def test_std_names_skipped_when_goroot_known(self):
        goroot = tree({"internal/abi/type.go": "", "cmd/go/main.go": ""})
        old = g.GOROOT_SRC
        g.GOROOT_SRC = goroot
        try:
            files = {"docs/a.md": "`internal/abi` `internal/abi.TypeOf` `cmd/go` `internal/nope`\n"}
            errs = scan(files, ["docs/a.md"])
        finally:
            g.GOROOT_SRC = old
        self.assertEqual(len(errs), 1, errs)
        self.assertIn("internal/nope", errs[0])


if __name__ == "__main__":
    unittest.main(verbosity=1)
