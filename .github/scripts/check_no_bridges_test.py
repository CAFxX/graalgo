#!/usr/bin/env python3
"""Positive/negative controls for check_no_bridges.py (G-1, 2026-10-19).

Builds fixture git repos in tmp dirs and runs the gate against them:

  1. bridge in a NON-TIP commit of a multi-commit push must FAIL
     (the G-1 hole: pre-fix, the gate inspected only HEAD~1...HEAD, so a
     bridge in any earlier pushed commit passed unseen)
  2. bridge in the tip commit must FAIL (pre-existing behavior, unchanged)
  3. clean multi-commit push must PASS (no false positives)
  4. no determinable diff must FAIL loudly (never pass vacuously)
  5. an all-zeros push-before ref falls back to the tip-only source

Run: python3 check_no_bridges_test.py
Exit code 0 = all controls behave as specified.
"""

import os
import shutil
import subprocess
import sys
import tempfile
import unittest

SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                      "check_no_bridges.py")

BRIDGE_FILE = "internal/frontend/synth_evil.go"
BRIDGE_CONTENT = 'package frontend\n\n// synthetic stdlib package: the banned bridge\n'
CLEAN_GO = 'package foo\n\nfunc Foo() int { return 1 }\n'


def git(cwd, *args, env=None):
    e = dict(os.environ)
    e.update({
        "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t",
        "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
    })
    if env:
        e.update(env)
    return subprocess.run(["git"] + list(args), cwd=cwd, env=e,
                          capture_output=True, text=True, timeout=30)


def make_repo():
    d = tempfile.mkdtemp(prefix="nb-gate-test-")
    r = git(d, "init", "-q")
    assert r.returncode == 0, r.stderr
    return d


def commit(d, message, files):
    for path, content in files.items():
        full = os.path.join(d, path)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w") as f:
            f.write(content)
    r = git(d, "add", "-A")
    assert r.returncode == 0, r.stderr
    r = git(d, "commit", "-qm", message)
    assert r.returncode == 0, r.stderr
    r = git(d, "rev-parse", "HEAD")
    assert r.returncode == 0, r.stderr
    return r.stdout.strip()


def run_gate(repo_dir, push_before=None):
    env = {}
    if push_before is not None:
        env["GITHUB_PUSH_BEFORE"] = push_before
    e = dict(os.environ)
    e.update(env)
    # The fixture has no origin/master: that candidate fails and is skipped,
    # exactly like the real master-push path where HEAD == origin/master.
    r = subprocess.run([sys.executable, SCRIPT], cwd=repo_dir, env=e,
                       capture_output=True, text=True, timeout=60)
    return r.returncode, r.stdout + r.stderr


class NoBridgesGateTest(unittest.TestCase):
    def setUp(self):
        self.repos = []

    def tearDown(self):
        for d in self.repos:
            shutil.rmtree(d, ignore_errors=True)

    def fixture(self, commits):
        """commits: list of (message, {path: content}). Returns (dir, shas)."""
        d = make_repo()
        self.repos.append(d)
        shas = [commit(d, msg, files) for msg, files in commits]
        return d, shas

    def test_bridge_in_non_tip_commit_fails(self):
        # THE G-1 positive control: the bridge rides in the middle commit of
        # a 3-commit push. Pre-fix the gate saw only the tip commit and
        # passed; post-fix it must fail on the full pushed range.
        d, shas = self.fixture([
            ("base", {"a.go": CLEAN_GO}),
            ("add synthetic bridge (non-tip)", {BRIDGE_FILE: BRIDGE_CONTENT}),
            ("clean tip", {"b.go": CLEAN_GO}),
        ])
        rc, out = run_gate(d, push_before=shas[0])
        self.assertEqual(rc, 1, f"gate must fail on non-tip bridge:\n{out}")
        self.assertIn("full range", out)
        self.assertIn("NEW SYNTHETIC FILE", out)

    def test_bridge_in_tip_commit_fails(self):
        d, shas = self.fixture([
            ("base", {"a.go": CLEAN_GO}),
            ("clean middle", {"b.go": CLEAN_GO}),
            ("add synthetic bridge (tip)", {BRIDGE_FILE: BRIDGE_CONTENT}),
        ])
        rc, out = run_gate(d, push_before=shas[0])
        self.assertEqual(rc, 1, f"gate must fail on tip bridge:\n{out}")
        self.assertIn("NEW SYNTHETIC FILE", out)

    def test_clean_multi_commit_push_passes(self):
        d, shas = self.fixture([
            ("base", {"a.go": CLEAN_GO}),
            ("more", {"b.go": CLEAN_GO}),
            ("tip", {"c.go": CLEAN_GO}),
        ])
        rc, out = run_gate(d, push_before=shas[0])
        self.assertEqual(rc, 0, f"gate must pass on clean push:\n{out}")
        self.assertIn("passed", out)

    def test_no_diff_fails_loudly(self):
        # Single-commit repo, no before ref, no origin: every diff source is
        # empty. The gate must fail, never pass vacuously.
        d, _ = self.fixture([("only", {"a.go": CLEAN_GO})])
        rc, out = run_gate(d, push_before=None)
        self.assertEqual(rc, 1, f"gate must fail with no diff:\n{out}")
        self.assertIn("vacuously", out)

    def test_zero_before_falls_back_to_tip_only(self):
        # All-zeros before (first push of a new ref) carries no range info:
        # documented fallback is the tip-only source.
        d, shas = self.fixture([
            ("base", {"a.go": CLEAN_GO}),
            ("add synthetic bridge (tip)", {BRIDGE_FILE: BRIDGE_CONTENT}),
        ])
        rc, out = run_gate(d, push_before="0" * 40)
        self.assertEqual(rc, 1, f"gate must fail via tip fallback:\n{out}")
        self.assertIn("tip only", out)


if __name__ == "__main__":
    unittest.main(verbosity=2)
