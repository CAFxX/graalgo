#!/usr/bin/env python3
"""Positive/negative controls for check_snapshot_rebaseline.py (G-2).

Builds fixture git repos in tmp dirs and runs the guard against them:

  1. snapshot change in a NON-TIP commit without a trailer must FAIL
     (the G-2 hole, mirroring G-1: behavior change + re-record in one
     push passes snapshot-check green with no declared intent)
  2. snapshot change WITH a Snapshot-Rebaseline: trailer must PASS
  3. commits not touching the snapshots must PASS
  4. a bare `Snapshot-Rebaseline:` (empty value) must FAIL
  5. uncommitted snapshot changes must FAIL loudly
  6. no determinable pushed range must FAIL loudly (never pass vacuously)

Run: python3 check_snapshot_rebaseline_test.py
Exit code 0 = all controls behave as specified.
"""

import os
import shutil
import subprocess
import sys
import tempfile
import unittest

SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                      "check_snapshot_rebaseline.py")

SNAP = "test/snapshots/go1.27.1.json"
SNAP_V1 = '{"format": "graalgo-behavior-snapshot/v1", "programs": []}\n'
SNAP_V2 = '{"format": "graalgo-behavior-snapshot/v1", "programs": [{"name": "01_hello"}]}\n'
CODE = 'package foo\n\nfunc Foo() int { return 1 }\n'
TRAILER = "Snapshot-Rebaseline: deliberate output change in 01_hello after fix"


def git(cwd, *args):
    e = dict(os.environ)
    e.update({
        "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t",
        "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
    })
    return subprocess.run(["git"] + list(args), cwd=cwd, env=e,
                          capture_output=True, text=True, timeout=30)


def make_repo():
    d = tempfile.mkdtemp(prefix="sr-guard-test-")
    r = git(d, "init", "-q")
    assert r.returncode == 0, r.stderr
    return d


def commit(d, parts, files):
    """parts: one message string or a list of -m paragraphs."""
    if isinstance(parts, str):
        parts = [parts]
    for path, content in files.items():
        full = os.path.join(d, path)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w") as f:
            f.write(content)
    r = git(d, "add", "-A")
    assert r.returncode == 0, r.stderr
    args = []
    for p in parts:
        args += ["-m", p]
    r = git(d, "commit", "-q", *args)
    assert r.returncode == 0, r.stderr
    r = git(d, "rev-parse", "HEAD")
    assert r.returncode == 0, r.stderr
    return r.stdout.strip()


def run_guard(repo_dir, push_before=None):
    env = {}
    if push_before is not None:
        env["GITHUB_PUSH_BEFORE"] = push_before
    e = dict(os.environ)
    e.update(env)
    r = subprocess.run([sys.executable, SCRIPT], cwd=repo_dir, env=e,
                       capture_output=True, text=True, timeout=60)
    return r.returncode, r.stdout + r.stderr


class SnapshotRebaselineGuardTest(unittest.TestCase):
    def setUp(self):
        self.repos = []

    def tearDown(self):
        for d in self.repos:
            shutil.rmtree(d, ignore_errors=True)

    def fixture(self, commits):
        d = make_repo()
        self.repos.append(d)
        shas = [commit(d, msg, files) for msg, files in commits]
        return d, shas

    def test_snapshot_change_without_trailer_in_non_tip_commit_fails(self):
        # THE G-2 positive control: the re-record rides in the middle commit
        # of a 3-commit push with no declared intent. Must fail, naming it.
        d, shas = self.fixture([
            ("base", {"a.go": CODE, SNAP: SNAP_V1}),
            ("re-record baseline (non-tip)", {SNAP: SNAP_V2}),
            ("clean tip", {"b.go": CODE}),
        ])
        rc, out = run_guard(d, push_before=shas[0])
        self.assertEqual(rc, 1, f"guard must fail on untrailered re-record:\n{out}")
        self.assertIn(shas[1][:12], out)
        self.assertIn("Snapshot-Rebaseline", out)

    def test_snapshot_change_with_trailer_passes(self):
        d, shas = self.fixture([
            ("base", {"a.go": CODE, SNAP: SNAP_V1}),
            (["re-record baseline (non-tip)", TRAILER], {SNAP: SNAP_V2}),
            ("clean tip", {"b.go": CODE}),
        ])
        rc, out = run_guard(d, push_before=shas[0])
        self.assertEqual(rc, 0, f"guard must pass on trailered re-record:\n{out}")
        self.assertIn("passed", out)

    def test_unrelated_commits_pass(self):
        d, shas = self.fixture([
            ("base", {"a.go": CODE, SNAP: SNAP_V1}),
            ("more code", {"b.go": CODE}),
            ("tip", {"c.go": CODE}),
        ])
        rc, out = run_guard(d, push_before=shas[0])
        self.assertEqual(rc, 0, f"guard must pass without snapshot changes:\n{out}")

    def test_empty_trailer_value_fails(self):
        d, shas = self.fixture([
            ("base", {"a.go": CODE, SNAP: SNAP_V1}),
            (["re-record baseline", "Snapshot-Rebaseline:"], {SNAP: SNAP_V2}),
        ])
        rc, out = run_guard(d, push_before=shas[0])
        self.assertEqual(rc, 1, f"guard must reject empty trailer value:\n{out}")

    def test_dirty_snapshot_tree_fails(self):
        d, shas = self.fixture([
            ("base", {"a.go": CODE, SNAP: SNAP_V1}),
        ])
        with open(os.path.join(d, SNAP), "w") as f:
            f.write(SNAP_V2)
        rc, out = run_guard(d, push_before=shas[0])
        self.assertEqual(rc, 1, f"guard must fail on dirty snapshot tree:\n{out}")
        self.assertIn("uncommitted", out)

    def test_dirty_readme_alone_passes(self):
        # Only the baseline JSON is guarded: a dirty README.md next to it
        # cannot launder a behavior change and must not trip the guard.
        d, shas = self.fixture([
            ("base", {"a.go": CODE, SNAP: SNAP_V1,
                      "test/snapshots/README.md": "# snapshots\n"}),
        ])
        with open(os.path.join(d, "test/snapshots/README.md"), "a") as f:
            f.write("\nmore docs\n")
        rc, out = run_guard(d, push_before=shas[0])
        self.assertEqual(rc, 0, f"guard must ignore dirty docs:\n{out}")

    def test_no_range_fails_loudly(self):
        # Single-commit repo, no before ref, no origin: no range can be
        # determined. The guard must fail, never pass vacuously.
        d, _ = self.fixture([("only", {"a.go": CODE})])
        rc, out = run_guard(d, push_before=None)
        self.assertEqual(rc, 1, f"guard must fail with no range:\n{out}")
        self.assertIn("vacuously", out)


if __name__ == "__main__":
    unittest.main(verbosity=2)
