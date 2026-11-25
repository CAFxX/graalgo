#!/usr/bin/env python3
"""Tests for check_doc_paths.py: a planted backticked path that does not
exist must fail; existing paths, "(planned)" / historical lines, archived
docs, fenced code and Go std package names must pass. In a live
requirement (requirements/*.yaml) a bare path in the statement, rationale or
verification plan that does not exist must fail; superseded/retired records
(history) and the history list are not checked.

Run: python3 .github/scripts/test_check_doc_paths.py
"""

import atexit
import contextlib
import io
import shutil
import subprocess
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

    REQ = ("id: GG-0001\ntitle: t\nstatement: 'x SHALL y; see test/a/ok.go.'\n"
           "rationale: plain text, see docs/r.md\nparent: \"\"\nstatus: %s\n"
           "verification_plan: 'Run test/m1r/gone.sh (make test-x), then tools/t/ok.py; ''quoted''.'\n"
           "history:\n    - '2026-01-01: the old test/m1r/older.sh'\n")

    def test_requirement_missing_path_fails(self):
        files = {"test/a/ok.go": "", "docs/r.md": "", "tools/t/ok.py": "",
                 "requirements/GG-0001.yaml": self.REQ % "active"}
        errs = scan(files, ["requirements/GG-0001.yaml"])
        self.assertEqual(len(errs), 1, errs)
        self.assertIn("verification_plan: test/m1r/gone.sh does not exist", errs[0])

    def test_requirement_continuation_lines_and_planned(self):
        req = ("id: GG-0002\nstatus: locked\nverification_plan: 'first part,\n"
               "    then test/m1r/later.sh. A test/new/x.go (planned).'\n")
        errs = scan({"requirements/GG-0002.yaml": req}, ["requirements/GG-0002.yaml"])
        self.assertEqual(len(errs), 1, errs)
        self.assertIn("test/m1r/later.sh", errs[0])

    def test_main_scans_tracked_requirements(self):
        d = tree({"requirements/GG-0001.yaml": self.REQ % "active", "test/a/ok.go": "",
                  "docs/r.md": "", "tools/t/ok.py": ""})
        subprocess.run(["git", "init", "-q", str(d)], check=True)
        subprocess.run(["git", "-C", str(d), "add", "-A"], check=True)
        with contextlib.redirect_stdout(io.StringIO()) as out:
            self.assertEqual(g.main(["x", str(d)]), 1)
        self.assertIn("test/m1r/gone.sh", out.getvalue())

    def test_dead_requirement_is_history(self):
        for status in ("superseded", "retired"):
            files = {"requirements/GG-0001.yaml": self.REQ % status}
            self.assertEqual(scan(files, ["requirements/GG-0001.yaml"]), [], status)


if __name__ == "__main__":
    unittest.main(verbosity=1)
