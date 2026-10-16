#!/usr/bin/env python3
"""
Self-tests for check_no_bridges.py (INFRA-2, 2026-10-16).

Run: python3 .github/scripts/test_check_no_bridges.py
(covers the gate's own logic with throwaway git repos in a temp dir —
no network, no repo state touched).

The central regression test is test_full_range_flags_bridge_in_earlier_commit:
it builds a two-commit push whose FIRST commit adds a bridge and whose LAST
commit is benign, then shows that (a) the old auto-detect path (HEAD~1...HEAD
only) passes — the hole — while (b) the --range full-push path flags it.
"""

import os
import subprocess
import sys
import tempfile
import unittest

SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                      "check_no_bridges.py")


def git(cwd, *args):
    subprocess.run(["git", *args], cwd=cwd, check=True,
                   capture_output=True, text=True)


def rev_parse(cwd, rev):
    return subprocess.run(["git", "rev-parse", rev], cwd=cwd, check=True,
                          capture_output=True, text=True).stdout.strip()


def init_repo():
    d = tempfile.mkdtemp(prefix="nbtest-")
    git(d, "init", "-q")
    git(d, "config", "user.email", "test@example.com")
    git(d, "config", "user.name", "test")
    git(d, "config", "commit.gpgsign", "false")
    return d


def commit(d, files, msg):
    for name, content in files.items():
        p = os.path.join(d, name)
        os.makedirs(os.path.dirname(p), exist_ok=True)
        with open(p, "w") as f:
            f.write(content)
    git(d, "add", "-A")
    git(d, "commit", "-qm", msg)
    return rev_parse(d, "HEAD")


def run_gate(cwd, *args):
    return subprocess.run([sys.executable, SCRIPT, *args], cwd=cwd,
                          capture_output=True, text=True, timeout=120)


class TestCheckNoBridges(unittest.TestCase):
    def test_full_range_flags_bridge_in_earlier_commit(self):
        """INFRA-2: a bridge in an earlier commit of a multi-commit push is
        flagged when the full push range is inspected."""
        d = init_repo()
        base = commit(d, {"main.go": 'package main\n\nfunc main() {}\n'},
                      "base")
        # Commit 1 of the push: introduces a bridge (new synthetic file).
        commit(d, {"internal/frontend/synth_evil.go":
                   'package frontend\n\n// synthetic bridge\n'},
               "bridge in earlier commit")
        # Commit 2 of the push: benign.
        commit(d, {"main.go":
                   'package main\n\nfunc main() { println("hi") }\n'},
               "benign follow-up")

        r = run_gate(d, "--range", f"{base}...{rev_parse(d, 'HEAD')}")
        self.assertNotEqual(r.returncode, 0,
                            f"gate should FAIL on the full range:\n{r.stdout}")
        self.assertIn("NEW SYNTHETIC FILE", r.stdout)

    def test_old_auto_detect_path_misses_earlier_commit(self):
        """Documents the INFRA-2 hole: without --range, auto-detection sees
        only HEAD~1...HEAD (the last commit) and misses the earlier bridge."""
        d = init_repo()
        commit(d, {"main.go": 'package main\n\nfunc main() {}\n'}, "base")
        commit(d, {"internal/frontend/synth_evil.go":
                   'package frontend\n\n// synthetic bridge\n'},
               "bridge in earlier commit")
        commit(d, {"main.go":
                   'package main\n\nfunc main() { println("hi") }\n'},
               "benign follow-up")

        # No origin here, working tree clean: auto-detect falls through to
        # candidate 4 (HEAD~1...HEAD) — the benign last commit only.
        r = run_gate(d)
        self.assertEqual(r.returncode, 0,
                         f"old auto-detect path unexpectedly flagged:\n{r.stdout}")

    def test_empty_before_inspects_against_empty_tree(self):
        """First push (all-zeros before) inspects everything introduced —
        never passes vacuously."""
        d = init_repo()
        head = commit(d, {"x.go":
                          'package x\n\nfunc F() { panic("not implemented") }\n'},
                      "first push")

        r = run_gate(d, "--range", f"{'0' * 40}...{head}")
        self.assertNotEqual(r.returncode, 0,
                            f"gate should FAIL on first-push content:\n{r.stdout}")
        self.assertIn("PANIC STUB", r.stdout)

    def test_empty_resolved_range_passes_honestly(self):
        """A range that resolves but introduces no new lines is an honest
        pass, not a vacuous one."""
        d = init_repo()
        head = commit(d, {"main.go": 'package main\n\nfunc main() {}\n'},
                      "base")

        r = run_gate(d, "--range", f"{head}...{head}")
        self.assertEqual(r.returncode, 0,
                         f"gate should PASS on an empty resolved range:\n{r.stdout}"
                         f"\n{r.stderr}")
        self.assertIn("no new lines", r.stdout)

    def test_unresolvable_range_fails_loudly(self):
        """A --range that cannot be resolved is a loud failure, never a
        vacuous pass."""
        d = init_repo()
        commit(d, {"main.go": 'package main\n\nfunc main() {}\n'}, "base")

        r = run_gate(d, "--range",
                     f"{'d' * 40}...{rev_parse(d, 'HEAD')}")
        self.assertNotEqual(r.returncode, 0,
                            f"gate should FAIL on unresolvable range:\n{r.stdout}")
        self.assertIn("could not determine", r.stdout)

    def test_local_uncommitted_bridge_still_flagged(self):
        """Local dev flow unchanged: an uncommitted bridge in a TRACKED file
        is still caught by auto-detection (git diff HEAD only sees tracked
        changes — same as before this change)."""
        d = init_repo()
        commit(d, {"main.go": 'package main\n\nfunc main() {}\n'}, "base")
        with open(os.path.join(d, "main.go"), "w") as f:
            f.write('package main\n\nfunc main() { panic("todo: later") }\n')

        r = run_gate(d)
        self.assertNotEqual(r.returncode, 0,
                            f"gate should FAIL on uncommitted bridge:\n{r.stdout}")
        self.assertIn("PANIC STUB", r.stdout)


if __name__ == "__main__":
    unittest.main(verbosity=2)
