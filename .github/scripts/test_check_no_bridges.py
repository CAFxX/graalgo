#!/usr/bin/env python3
"""Tests for check_no_bridges.py (the no-new-bridges gate itself).

Run: python3 test_check_no_bridges.py   (from this directory, or anywhere;
the test locates the script next to itself). Stdlib only, no network.

Coverage contract (P1-10 remediation, 2026-10-30):
  (a) a bridge in the working tree is caught even when the branch diff
      is non-empty (union of live diff sources);
  (b) a multi-line Go panic stub is caught;
  (c) a Java not-implemented throw is caught;
  (d) the adjudicated non-bridges (ADJUDICATED_WIRE_NODES /
      ADJUDICATED_REGISTRATIONS) still pass.
Plus positive controls for the pre-existing checks, the never-vacuous
contract, the master-push fallback, and error deduplication across
overlapping union sources. The new-behaviour tests fail on the pre-fix
script (9 of 18, measured 2026-10-30): the union tests fail because
get_diff returned only the first source, the multi-line tests fail
because panic detection was per-line, the Java tests fail because no
Java pattern existed, and the new-subdirectory test fails because
--untracked-files=normal collapsed the directory to one `?? sub/` entry.
"""

import importlib.util
import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "check_no_bridges.py")


def load_gate():
    spec = importlib.util.spec_from_file_location("check_no_bridges", SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


GATE = load_gate()


def make_diff(fname, added, new_file=False):
    """Minimal unified diff adding `added` (list of source lines) to fname."""
    lines = ["diff --git a/%s b/%s" % (fname, fname)]
    if new_file:
        lines.append("--- /dev/null")
    else:
        lines.append("--- a/%s" % fname)
    lines.append("+++ b/%s" % fname)
    lines.append("@@ -0,0 +1,%d @@" % len(added))
    lines.extend("+" + line for line in added)
    return "\n".join(lines) + "\n"


def git(repo, *args):
    subprocess.run(["git", "-C", repo] + list(args), check=True,
                   capture_output=True, text=True)


def make_repo(n_commits=2):
    """Temp git repo with n_commits on master; returns its path."""
    repo = tempfile.mkdtemp(prefix="nobridges-test-")
    git(repo, "init", "-q", "-b", "master")
    git(repo, "config", "user.email", "test@example.invalid")
    git(repo, "config", "user.name", "Gate Test")
    for i in range(n_commits):
        with open(os.path.join(repo, "file%d.txt" % i), "w") as f:
            f.write("base %d\n" % i)
        git(repo, "add", ".")
        git(repo, "commit", "-q", "-m", "commit %d" % i)
    return repo


class TestDiffStringChecks(unittest.TestCase):
    def test_single_line_go_panic_still_caught(self):
        # Positive control: pre-existing behaviour must not regress.
        diff = make_diff("internal/foo.go",
                         ['func f() { panic("not implemented") }'])
        errs = GATE.check_no_bridges(diff)
        self.assertTrue(any("PANIC STUB" in e for e in errs), errs)

    def test_multiline_go_panic_sprintf_caught(self):
        # (b) The exact evasion from P1-10: the keyword sits on the
        # continuation line of a composed panic.
        diff = make_diff("internal/foo.go", [
            "func f() {",
            "\tpanic(fmt.Sprintf(",
            '\t\t"not implemented yet"))',
            "}",
        ])
        errs = GATE.check_no_bridges(diff)
        self.assertTrue(any("PANIC STUB" in e for e in errs), errs)

    def test_multiline_go_panic_plain_string_caught(self):
        diff = make_diff("internal/foo.go", [
            "func f() {",
            "\tpanic(",
            '\t\t"unsupported operation")',
            "}",
        ])
        errs = GATE.check_no_bridges(diff)
        self.assertTrue(any("PANIC STUB" in e for e in errs), errs)

    def test_multiline_go_panic_legit_message_passes(self):
        # Negative control: a multi-line panic with no stub keyword is a
        # legitimate invariant, not a stub.
        diff = make_diff("internal/foo.go", [
            "func f(x int) {",
            "\tpanic(fmt.Sprintf(",
            '\t\t"invalid value %d", x))',
            "}",
        ])
        self.assertEqual(GATE.check_no_bridges(diff), [])

    def test_java_single_line_throw_caught(self):
        # (c) Single-line Java throw.
        diff = make_diff(
            "internal/backend/Foo.java",
            ['throw new UnsupportedOperationException("not implemented");'])
        errs = GATE.check_no_bridges(diff)
        self.assertTrue(any("UNSUPPORTED OPERATION STUB" in e for e in errs),
                        errs)

    def test_java_multiline_throw_caught(self):
        # (c) Multi-line Java throw: keyword on the continuation line.
        diff = make_diff("internal/backend/Foo.java", [
            "void f() {",
            "\tthrow new UnsupportedOperationException(",
            '\t\t"todo: implement this");',
            "}",
        ])
        errs = GATE.check_no_bridges(diff)
        self.assertTrue(any("UNSUPPORTED OPERATION STUB" in e for e in errs),
                        errs)

    def test_java_legit_throw_passes(self):
        # Negative controls: a different exception type, and an
        # UnsupportedOperationException with a non-stub message.
        diff = make_diff("internal/backend/Foo.java", [
            'throw new IllegalStateException("closed");',
            'throw new UnsupportedOperationException("channel closed");',
        ])
        self.assertEqual(GATE.check_no_bridges(diff), [])

    def test_adjudicated_wire_node_passes(self):
        # (d) PKG-023 adjudicated real implementation.
        diff = make_diff("internal/frontend/export.go",
                         ['e.node("runtimenumgoroutine")'])
        self.assertEqual(GATE.check_no_bridges(diff), [])

    def test_adjudicated_registration_passes(self):
        # (d) PKG-023 adjudicated real implementation.
        diff = make_diff("internal/frontend/export.go",
                         ['addFunc("NumGoroutine", fn)'])
        self.assertEqual(GATE.check_no_bridges(diff), [])

    def test_non_adjudicated_wire_node_still_caught(self):
        # Positive control for (d): adjudication is narrow, not a blanket
        # exemption for export.go wire nodes.
        diff = make_diff("internal/frontend/export.go",
                         ['e.node("runtimefoo")'])
        errs = GATE.check_no_bridges(diff)
        self.assertTrue(any("SUSPICIOUS WIRE NODE" in e for e in errs), errs)

    def test_new_synth_file_still_caught(self):
        diff = make_diff("internal/frontend/synth_newpkg.go",
                         ["package newpkg"], new_file=True)
        errs = GATE.check_no_bridges(diff)
        self.assertTrue(any("NEW SYNTHETIC FILE" in e for e in errs), errs)

    def test_duplicate_added_line_reported_once(self):
        # A unioned diff can contain the same added line twice (staged +
        # working-tree, branch + fallback). The gate must fail once, not
        # print the same error twice.
        diff = (make_diff("internal/foo.go",
                          ['func f() { panic("not implemented") }'])
                + make_diff("internal/foo.go",
                            ['func f() { panic("not implemented") }']))
        errs = GATE.check_no_bridges(diff)
        stub_errs = [e for e in errs if "PANIC STUB" in e]
        self.assertEqual(len(stub_errs), 1, errs)


class TestGetDiffUnion(unittest.TestCase):
    def run_in(self, repo, fn):
        old = os.getcwd()
        try:
            os.chdir(repo)
            return fn()
        finally:
            os.chdir(old)

    def test_branch_diff_and_working_tree_both_inspected(self):
        # (a) The core P1-10 regression: branch diff non-empty AND a
        # working-tree bridge in a TRACKED file. Pre-fix, get_diff
        # returned only the branch diff and the working-tree bridge was
        # invisible.
        repo = make_repo(n_commits=1)
        try:
            # Fake origin/master at the base commit, then a branch commit.
            base = subprocess.run(
                ["git", "-C", repo, "rev-parse", "HEAD"],
                check=True, capture_output=True, text=True).stdout.strip()
            git(repo, "update-ref", "refs/remotes/origin/master", base)
            with open(os.path.join(repo, "branch.go"), "w") as f:
                f.write("package branch\n")
            with open(os.path.join(repo, "work.go"), "w") as f:
                f.write("package work\n")
            git(repo, "add", ".")
            git(repo, "commit", "-q", "-m", "branch change")
            # Uncommitted working-tree bridge (tracked file, modified).
            with open(os.path.join(repo, "work.go"), "a") as f:
                f.write('func f() { panic("not implemented") }\n')
            diff, label = self.run_in(repo, GATE.get_diff)
            self.assertIn("branch.go", diff)
            self.assertIn("work.go", diff)
            self.assertIn("origin/master...HEAD", label)
            self.assertIn("working tree", label)
            errs = GATE.check_no_bridges(diff)
            self.assertTrue(any("PANIC STUB" in e and "work.go" in e
                                for e in errs), errs)
        finally:
            import shutil
            shutil.rmtree(repo, ignore_errors=True)

    def test_branch_diff_and_untracked_file_both_inspected(self):
        # (a), second silent skip of the same class: a brand-new bridge
        # file that was never `git add`ed is invisible to every git diff,
        # so it must be picked up via git status and unioned in.
        repo = make_repo(n_commits=1)
        try:
            base = subprocess.run(
                ["git", "-C", repo, "rev-parse", "HEAD"],
                check=True, capture_output=True, text=True).stdout.strip()
            git(repo, "update-ref", "refs/remotes/origin/master", base)
            with open(os.path.join(repo, "branch.go"), "w") as f:
                f.write("package branch\n")
            git(repo, "add", ".")
            git(repo, "commit", "-q", "-m", "branch change")
            # Untracked new bridge file.
            with open(os.path.join(repo, "work.go"), "w") as f:
                f.write('package work\nfunc f() { panic("not implemented") }\n')
            diff, label = self.run_in(repo, GATE.get_diff)
            self.assertIn("branch.go", diff)
            self.assertIn("work.go", diff)
            self.assertIn("untracked", label)
            errs = GATE.check_no_bridges(diff)
            self.assertTrue(any("PANIC STUB" in e and "work.go" in e
                                for e in errs), errs)
        finally:
            import shutil
            shutil.rmtree(repo, ignore_errors=True)

    def test_untracked_file_in_new_subdirectory_both_inspected(self):
        # Veto finding B1 (2026-10-30): with --untracked-files=normal,
        # git status collapses a brand-new directory to a single
        # `?? sub/` entry, which the .go/.java extension filter skips —
        # so sub/new.go was invisible to the gate. With
        # --untracked-files=all, git lists the file individually.
        repo = make_repo(n_commits=1)
        try:
            base = subprocess.run(
                ["git", "-C", repo, "rev-parse", "HEAD"],
                check=True, capture_output=True, text=True).stdout.strip()
            git(repo, "update-ref", "refs/remotes/origin/master", base)
            with open(os.path.join(repo, "branch.go"), "w") as f:
                f.write("package branch\n")
            git(repo, "add", ".")
            git(repo, "commit", "-q", "-m", "branch change")
            # Untracked new bridge file inside a brand-new directory.
            os.mkdir(os.path.join(repo, "sub"))
            with open(os.path.join(repo, "sub", "new.go"), "w") as f:
                f.write('package sub\nfunc f() { panic("not implemented") }\n')
            diff, label = self.run_in(repo, GATE.get_diff)
            self.assertIn("branch.go", diff)
            self.assertIn("sub/new.go", diff)
            self.assertIn("untracked", label)
            errs = GATE.check_no_bridges(diff)
            self.assertTrue(any("PANIC STUB" in e and "sub/new.go" in e
                                for e in errs), errs)
        finally:
            import shutil
            shutil.rmtree(repo, ignore_errors=True)

    def test_staged_bridge_inspected_when_branch_diff_nonempty(self):
        # Same union property for the staged channel.
        repo = make_repo(n_commits=1)
        try:
            base = subprocess.run(
                ["git", "-C", repo, "rev-parse", "HEAD"],
                check=True, capture_output=True, text=True).stdout.strip()
            git(repo, "update-ref", "refs/remotes/origin/master", base)
            with open(os.path.join(repo, "branch.go"), "w") as f:
                f.write("package branch\n")
            git(repo, "add", ".")
            git(repo, "commit", "-q", "-m", "branch change")
            with open(os.path.join(repo, "staged.go"), "w") as f:
                f.write('package staged\nfunc f() { panic("todo") }\n')
            git(repo, "add", "staged.go")
            diff, _label = self.run_in(repo, GATE.get_diff)
            errs = GATE.check_no_bridges(diff)
            self.assertTrue(any("PANIC STUB" in e and "staged.go" in e
                                for e in errs), errs)
        finally:
            import shutil
            shutil.rmtree(repo, ignore_errors=True)

    def test_master_push_fallback_when_live_channels_empty(self):
        # Clean tree, HEAD == origin/master: the only source is the tip
        # commit itself, and the gate must still inspect it (not pass
        # vacuously, and not skip to nothing).
        repo = make_repo(n_commits=2)
        try:
            git(repo, "update-ref", "refs/remotes/origin/master", "HEAD")
            diff, label = self.run_in(repo, GATE.get_diff)
            self.assertTrue(diff.strip(), "fallback diff must be non-empty")
            self.assertIn("HEAD~1...HEAD", label)
            self.assertIn("file1.txt", diff)
        finally:
            import shutil
            shutil.rmtree(repo, ignore_errors=True)

    def test_no_diff_is_vacuous_not_pass(self):
        # Single commit, clean tree, HEAD == origin/master: every source
        # is empty or unavailable (no HEAD~1). get_diff must say so, and
        # the script itself must exit non-zero, never 0.
        repo = make_repo(n_commits=1)
        try:
            git(repo, "update-ref", "refs/remotes/origin/master", "HEAD")
            diff, label = self.run_in(repo, GATE.get_diff)
            self.assertEqual((diff, label), ("", ""))
            proc = subprocess.run(
                [sys.executable, SCRIPT], cwd=repo,
                capture_output=True, text=True, timeout=60)
            self.assertNotEqual(proc.returncode, 0)
            self.assertIn("Refusing to pass vacuously", proc.stdout)
        finally:
            import shutil
            shutil.rmtree(repo, ignore_errors=True)


if __name__ == "__main__":
    unittest.main(verbosity=2)
