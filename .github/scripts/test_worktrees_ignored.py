#!/usr/bin/env python3
"""The lane worktrees live INSIDE the integration checkout, at
<repo>/.worktrees/<lane> (AGENTS.md §4): no repo gate may scan them, and
git must not see them.

One throwaway repo carries the real .gitignore and a real nested worktree
(`git worktree add .worktrees/planted`) whose branch commits a planted
violation for every gate that enumerates the repo from its root: a broken
markdown link, a backticked doc path that does not exist, an unknown GG id,
a dead-path reference, an oversized file and a .so. The outer checkout must
stay `git status`-clean and every gate green; the control repo plants the
same files at its own root and every gate must fail on them (so a gate that
went blind would fail this test too). The req CLI's scan is covered by
tools/req's TestScan_SkipsRootWorktrees; the gates that only walk fixed
subtrees (duplicate-FQN, layering, off-heap) never reach the root.

Run: python3 .github/scripts/test_worktrees_ignored.py
"""

import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

import check_doc_paths  # noqa: E402
import check_gg_ids  # noqa: E402
import check_no_dead_paths  # noqa: E402
import check_tracked_files  # noqa: E402

GITIGNORE = HERE.parents[1] / ".gitignore"
GIT = ["git", "-c", "user.name=t", "-c", "user.email=t@t", "-c", "init.defaultBranch=master",
       "-c", "commit.gpgsign=false"]
# Built from parts so this file trips neither the dead-path nor the GG-id gate.
DEAD = "pha" + "ses/"
UNKNOWN_ID = "GG-" + "9999"

PLANTS = {
    "docs/bad.md": (f"[broken](missing.md)\n\nSee `docs/no-such-file.md` and {UNKNOWN_ID}.\n").encode(),
    "tools/x.sh": f"#!/bin/sh\ncat {DEAD}p1/out.txt\n".encode(),
    "logs/big.bin": b"x" * (check_tracked_files.LIMIT + 1),
    "lib/a.so": b"abc",
}


def git(root, *args):
    return subprocess.run([*GIT, "-C", str(root), *args], check=True,
                          capture_output=True, text=True).stdout


def plant(root):
    for rel, data in PLANTS.items():
        p = root / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_bytes(data)


def new_repo(base, name):
    root = base / name
    root.mkdir()
    git(root, "init", "-q")
    shutil.copyfile(GITIGNORE, root / ".gitignore")
    (root / "requirements").mkdir()
    (root / "requirements/GG-0001.yaml").write_text("id: GG-0001\nstatus: active\n")
    (root / "README.md").write_text("# r\n\n[ok](requirements/GG-0001.yaml)\n")
    git(root, "add", ".gitignore", "README.md", "requirements")
    git(root, "commit", "-q", "-m", "base")
    return root


def gate_errors(root):
    """{gate: errors} for every root-enumerating gate."""
    md = subprocess.run([sys.executable, str(HERE / "check_markdown_links.py"), "--skip-external", str(root)],
                        capture_output=True, text=True)
    return {
        "markdown-links": [md.stdout] if md.returncode else [],
        "doc-paths": check_doc_paths.scan(root),
        "gg-ids": check_gg_ids.scan(root),
        "dead-paths": check_no_dead_paths.scan(root),
        "tracked-files": check_tracked_files.scan(root),
    }


class WorktreesIgnored(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.base = Path(tempfile.mkdtemp())
        # The repo under test: violations committed on a lane worktree's branch.
        cls.repo = new_repo(cls.base, "repo")
        lane = cls.repo / ".worktrees/planted"
        git(cls.repo, "worktree", "add", "-q", "-b", "wt/test/planted", str(lane))
        plant(lane)
        git(lane, "add", "-f", *PLANTS)
        git(lane, "commit", "-q", "-m", "planted")
        # The control: the same violations tracked at the root.
        cls.control = new_repo(cls.base, "control")
        plant(cls.control)
        git(cls.control, "add", "-f", *PLANTS)

    @classmethod
    def tearDownClass(cls):
        shutil.rmtree(cls.base, ignore_errors=True)

    def test_git_status_clean(self):
        self.assertEqual(git(self.repo, "status", "--porcelain", "--untracked-files=all"), "")

    def test_ignore_is_root_anchored(self):
        self.assertEqual(git(self.repo, "check-ignore", ".worktrees/x").strip(), ".worktrees/x")
        r = subprocess.run([*GIT, "-C", str(self.repo), "check-ignore", "-q", "docs/.worktrees/x"])
        self.assertEqual(r.returncode, 1, "only <repo>/.worktrees is ignored")

    def test_gates_ignore_worktrees(self):
        for gate, errs in gate_errors(self.repo).items():
            self.assertEqual(errs, [], f"{gate} scanned .worktrees/: {errs}")

    def test_same_violations_outside_fail(self):
        for gate, errs in gate_errors(self.control).items():
            self.assertTrue(errs, f"{gate} missed the planted violation at the root")


if __name__ == "__main__":
    unittest.main()
