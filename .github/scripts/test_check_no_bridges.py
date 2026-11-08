#!/usr/bin/env python3
"""Regression tests for check_no_bridges.py.

Run: python3 .github/scripts/test_check_no_bridges.py

Fixtures marked "branch diff" are excerpted hunks (gate-relevant
lines verbatim) from the p3fmt-stub branch diff
(4a494cad...16547f17, 2026-11-05), which the pre-fix script failed with
3 false positives (CI run 42039218294, step "No-bridges check"):
- the loader.go narrowing edit (Rule 2 misfire),
- the io_test.go / timecontext_test.go drift tests calling pre-existing
  synthetic builders (Rule 5 misfire — no test-file exemption).

The G3/G6/G8 fixtures are synthetic diffs built on the same
loader.go wrapped-list shape (the real list wraps across 7 lines):
they pin the 2026-11-05 review findings — the anchored `case`
pattern (F1), wrapped-list handling in both directions (F2) — and
the import-block negative that the stateless reading of F2 would
have false-positived on.

The TestGetDiffPushRange fixtures are real (tiny) git repos, not
synthetic diffs: they pin R-1108-91 (review 2026-11-08) — get_diff()
used to fall through to HEAD~1...HEAD on a post-push checkout, so
only the tip commit was inspected and every other pushed commit
escaped all five rules. A bridge in a NON-tip commit of a
multi-commit push is the positive control: the old code passed it.

Positive controls are synthetic diffs that MUST still fail: the gate
is a tripwire, not a rubber stamp.
"""

import contextlib
import io
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import check_no_bridges as cnb

# --- Fixture 1: Rule 2 narrowing (excerpted hunk, branch diff) --------
# The branch retired the fmt/sync/runtime stubs, so the pre-existing
# switch in loader.go ResolveImport shrank from
#   case "runtime", "fmt", "sync", "time", ...
# to
#   case "time", ...
# Every package in the added line was already in the removed line: a
# narrowing edit, not a new package-name-keyed special case.
LOADER_NARROWING = """\
diff --git a/internal/frontend/p3frontend/loader.go b/internal/frontend/p3frontend/loader.go
index 3239f8e3..572e12b3 100644
--- a/internal/frontend/p3frontend/loader.go
+++ b/internal/frontend/p3frontend/loader.go
@@ -440,7 +439,7 @@ func (l *Loader) ResolveImport(fromDir, path string) (*PackageRef, error) {
 \t// Synthetic packages resolve independently of fromDir; keep them out
 \t// of the anchored cache.
 \tswitch path {
-\tcase "runtime", "fmt", "sync", "time", "context", "errors", "sort", "bytes", "math", "strings",
+\tcase "time", "context", "errors", "sort", "bytes", "math", "strings",
 \t\t"encoding/xml", "encoding/csv", "encoding/gob", "strconv", "encoding/json",
 \t\t"encoding/base32", "encoding/base64", "encoding/hex", "encoding/binary",
 \t\t"encoding/ascii85", "encoding/pem", "encoding/asn1", "encoding":
"""

# --- Fixture 2: Rule 5 drift test, new file (excerpted, branch diff) ---
# io_test.go is a NEW drift test calling the PRE-EXISTING
# syntheticIoPackage() builder to pin the inventoried io mirror against
# the live phase-1 source. No new synthetic package is registered.
IO_DRIFT_TEST = """\
diff --git a/internal/frontend/p3frontend/io_test.go b/internal/frontend/p3frontend/io_test.go
new file mode 100644
index 00000000..59d7cefb
--- /dev/null
+++ b/internal/frontend/p3frontend/io_test.go
@@ -0,0 +1,21 @@
+package p3frontend
+
+import (
+\t"testing"
+)
+
+// TestIoMirrorMatchesPhase1 is the drift test for the synthetic io
+// mirror: the mirror must match the live phase-1 syntheticIoSource
+// (internal/frontend/synth_io.go) in scope and in the method sets of
+// every named type (including the 22 interfaces, whose method sets
+// come from embedding). The mirror covers the full gc 1.27.1 public
+// API of the synthetic source; any divergence fails fast here instead
+// of silently diverging.
+func TestIoMirrorMatchesPhase1(t *testing.T) {
+\tmirror := syntheticIoPackage()
+\t// The io source imports errors (synthetic mirror) and sync (real);
+\t// syntheticStdImporter resolves both.
+\tphase1 := phase1SyntheticPackageForTest(t,
+\t\t"../synth_io.go", "syntheticIoSource", "io", syntheticStdImporter(t))
+\tassertScopeMatchesPhase1(t, "io", mirror, phase1)
+}
"""

# --- Fixture 3: Rule 5 drift test, existing file (excerpted hunk) -------
# Same shape as fixture 2, in a pre-existing _test.go file.
TIME_DRIFT_TEST = """\
diff --git a/internal/frontend/p3frontend/timecontext_test.go b/internal/frontend/p3frontend/timecontext_test.go
index d07f8720..f2d19599 100644
--- a/internal/frontend/p3frontend/timecontext_test.go
+++ b/internal/frontend/p3frontend/timecontext_test.go
@@ -343,3 +375,18 @@ func TestTimeContextImportResolvesSynthetic(t *testing.T) {
 \t\t}
 \t}
 }
+
+// TestTimeMirrorMatchesPhase1 is the drift test for the synthetic time
+// mirror: the mirror must match the live phase-1 syntheticTimeSource
+// (internal/frontend/synth_time_context.go) in scope and in the method
+// sets of every named type. ONE documented exception: the mirror declares
+// Time.String, which the phase-1 source lacks (a live-path gap — gc has
+// it; see the header comment in synth_time_context.go). Any other
+// divergence fails fast here instead of silently diverging.
+func TestTimeMirrorMatchesPhase1(t *testing.T) {
+\tmirror := syntheticTimePackage()
+\t// The time source imports nothing, so any import is a failure.
+\tphase1 := phase1SyntheticPackageForTest(t,
+\t\t"../synth_time_context.go", "syntheticTimeSource", "time", rejectImports{})
+\tassertScopeMatchesPhase1(t, "time", mirror, phase1)
+}
"""

# --- Positive controls (synthetic diffs; MUST still fail) ----------------

# A genuinely new package-name conditional in shipped code.
NEW_CONDITIONAL = """\
diff --git a/internal/frontend/p3frontend/loader.go b/internal/frontend/p3frontend/loader.go
index 3239f8e3..572e12b3 100644
--- a/internal/frontend/p3frontend/loader.go
+++ b/internal/frontend/p3frontend/loader.go
@@ -440,5 +440,6 @@ func (l *Loader) ResolveImport(fromDir, path string) (*PackageRef, error) {
 \t// Synthetic packages resolve independently of fromDir; keep them out
 \t// of the anchored cache.
 \tswitch path {
 \tcase "runtime", "fmt", "sync", "time", "context", "errors", "sort", "bytes", "math", "strings",
+\tcase "os", "net",
 \t\t"encoding/xml", "encoding/csv", "encoding/gob", "strconv", "encoding/json",
"""

# Widening: the added line keeps the pre-existing "time" but ADDS "fmt",
# which no removed line in the hunk mentioned. Still a new special case.
WIDENING = """\
diff --git a/internal/frontend/p3frontend/loader.go b/internal/frontend/p3frontend/loader.go
index 3239f8e3..572e12b3 100644
--- a/internal/frontend/p3frontend/loader.go
+++ b/internal/frontend/p3frontend/loader.go
@@ -440,5 +440,5 @@ func (l *Loader) ResolveImport(fromDir, path string) (*PackageRef, error) {
 \t// Synthetic packages resolve independently of fromDir; keep them out
 \t// of the anchored cache.
 \tswitch path {
-\tcase "runtime", "fmt", "sync", "time", "context", "errors", "sort", "bytes", "math", "strings",
+\tcase "runtime", "fmt", "sync", "time", "context", "errors", "sort", "bytes", "math", "strings", "os",
 \t\t"encoding/xml", "encoding/csv", "encoding/gob", "strconv", "encoding/json",
"""

# A synthetic-builder call in shipped (non-test) code.
NEW_SYNTHETIC_CALL = """\
diff --git a/internal/frontend/p3frontend/loader.go b/internal/frontend/p3frontend/loader.go
index 3239f8e3..572e12b3 100644
--- a/internal/frontend/p3frontend/loader.go
+++ b/internal/frontend/p3frontend/loader.go
@@ -440,5 +440,6 @@ func (l *Loader) ResolveImport(fromDir, path string) (*PackageRef, error) {
 \t// Synthetic packages resolve independently of fromDir; keep them out
 \t// of the anchored cache.
 \tswitch path {
 \tcase "runtime", "fmt", "sync", "time", "context", "errors", "sort", "bytes", "math", "strings",
+\tmirror := syntheticFooPackage()
 \t\t"encoding/xml", "encoding/csv", "encoding/gob", "strconv", "encoding/json",
"""

# A genuinely new synthetic stdlib package file.
NEW_SYNTH_FILE = """\
diff --git a/internal/frontend/synth_foo.go b/internal/frontend/synth_foo.go
new file mode 100644
index 00000000..59d7cefb
--- /dev/null
+++ b/internal/frontend/synth_foo.go
@@ -0,0 +1,3 @@
+package frontend
+
+// Synthetic foo package.
"""

# A package-name conditional in a test file was already exempt under
# Rule 2; pin that the exemption still holds.
TEST_FILE_CONDITIONAL = """\
diff --git a/internal/frontend/p3frontend/loader_test.go b/internal/frontend/p3frontend/loader_test.go
index 3239f8e3..572e12b3 100644
--- a/internal/frontend/p3frontend/loader_test.go
+++ b/internal/frontend/p3frontend/loader_test.go
@@ -440,4 +439,5 @@ func TestResolveImport(t *testing.T) {
 \tswitch path {
+\tcase "fmt":
 \t\t"encoding/xml", "encoding/csv", "encoding/gob", "strconv", "encoding/json",
"""

# --- F1 (review 2026-11-05): non-case lines mentioning quoted -------
# stdlib names (G3). The unanchored `case\b.*` pattern flagged all
# three; the anchored `^\s*case\b` pattern must flag none of them:
# a comment, an identifier ending in "case", prose containing "case".
NON_CASE_LINES = """\
diff --git a/internal/frontend/p3frontend/loader.go b/internal/frontend/p3frontend/loader.go
index 3239f8e3..572e12b3 100644
--- a/internal/frontend/p3frontend/loader.go
+++ b/internal/frontend/p3frontend/loader.go
@@ -440,5 +440,8 @@ func (l *Loader) ResolveImport(fromDir, path string) (*PackageRef, error) {
 \t// Synthetic packages resolve independently of fromDir; keep them out
 \t// of the anchored cache.
 \tswitch path {
+\t// edge case: prefer "fmt" over "os" here
+\ttestcase := "fmt"
+\t// lowercase "sync" handling
 \t\t"encoding/xml", "encoding/csv", "encoding/gob", "strconv", "encoding/json",
"""

# --- F2 (review 2026-11-05): wrapped case lists, both directions ------
# G6: widening appended to a CONTINUATION line of the wrapped list —
# the `case` line itself is unchanged (context only) and the added
# continuation line mentions "os"/"net", which no removed line
# mentioned. Must flag.
CONTINUATION_WIDENING = """\
diff --git a/internal/frontend/p3frontend/loader.go b/internal/frontend/p3frontend/loader.go
index 3239f8e3..572e12b3 100644
--- a/internal/frontend/p3frontend/loader.go
+++ b/internal/frontend/p3frontend/loader.go
@@ -440,5 +440,5 @@ func (l *Loader) ResolveImport(fromDir, path string) (*PackageRef, error) {
 \tswitch path {
 \tcase "time", "context", "errors", "sort", "bytes", "math", "strings",
-\t\t"encoding/xml", "encoding/csv", "encoding/gob", "strconv", "encoding/json",
+\t\t"os", "net", "encoding/xml", "encoding/csv", "encoding/gob", "strconv", "encoding/json",
 \t\t"encoding/base32", "encoding/base64", "encoding/hex", "encoding/binary",
"""

# G6 (deep): the same widening, but the hunk starts mid-list — the
# `case` line falls outside the hunk's context window, so only the
# trailing-comma fallback sees the continuation line. Must flag.
DEEP_CONTINUATION_WIDENING = """\
diff --git a/internal/frontend/p3frontend/loader.go b/internal/frontend/p3frontend/loader.go
index 3239f8e3..572e12b3 100644
--- a/internal/frontend/p3frontend/loader.go
+++ b/internal/frontend/p3frontend/loader.go
@@ -441,2 +441,2 @@ func (l *Loader) ResolveImport(fromDir, path string) (*PackageRef, error) {
 \t\t"encoding/xml", "encoding/csv", "encoding/gob", "strconv", "encoding/json",
-\t\t"encoding/base32", "encoding/base64", "encoding/hex", "encoding/binary",
+\t\t"os", "encoding/base32", "encoding/base64", "encoding/hex", "encoding/binary",
 \t\t"encoding/ascii85", "encoding/pem", "encoding/asn1", "encoding":
"""

# G8: a pure rewrap moving "os" from the continuation line onto the
# `case` line (no semantic change). The removed continuation line
# contributes ("case", "os") to the removed set, so the added line
# must NOT flag — the pre-F2 script flagged this.
PURE_REWRAP = """\
diff --git a/internal/frontend/p3frontend/loader.go b/internal/frontend/p3frontend/loader.go
index 3239f8e3..572e12b3 100644
--- a/internal/frontend/p3frontend/loader.go
+++ b/internal/frontend/p3frontend/loader.go
@@ -440,5 +440,4 @@ func (l *Loader) ResolveImport(fromDir, path string) (*PackageRef, error) {
 \tswitch path {
-\tcase "time", "context",
-\t\t"os",
+\tcase "time", "context", "os",
 \t\t"encoding/xml", "encoding/csv", "encoding/gob", "strconv", "encoding/json",
"""

# Import-block addition: a bare `"os"` line is not a case-list
# continuation (no trailing `,`/`:` and no `case` line above it), so
# adding an stdlib import must NOT flag. Pins the punctuation guard
# in _case_list_pairs: the stateless reading of F2 flagged this.
IMPORT_ADDITION = """\
diff --git a/internal/frontend/p3frontend/loader.go b/internal/frontend/p3frontend/loader.go
index 00000000..11111111 100644
--- a/internal/frontend/p3frontend/loader.go
+++ b/internal/frontend/p3frontend/loader.go
@@ -1,5 +1,6 @@ package p3frontend
 import (
 \t"fmt"
+\t"os"
 \t"time"
 )
"""


class TestNoBridgesGate(unittest.TestCase):
    def test_narrowing_loader_case_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(LOADER_NARROWING), [])

    def test_drift_test_new_file_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(IO_DRIFT_TEST), [])

    def test_drift_test_existing_file_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(TIME_DRIFT_TEST), [])

    def test_conditional_in_test_file_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(TEST_FILE_CONDITIONAL), [])

    def test_new_conditional_flagged(self):
        errs = cnb.check_no_bridges(NEW_CONDITIONAL)
        self.assertTrue(any("PACKAGE-NAME CONDITIONAL" in e for e in errs), errs)

    def test_widening_conditional_flagged(self):
        errs = cnb.check_no_bridges(WIDENING)
        self.assertTrue(any("PACKAGE-NAME CONDITIONAL" in e for e in errs), errs)

    def test_synthetic_call_in_shipped_code_flagged(self):
        errs = cnb.check_no_bridges(NEW_SYNTHETIC_CALL)
        self.assertTrue(any("SYNTHETIC REGISTRATION" in e for e in errs), errs)

    def test_new_synth_file_flagged(self):
        errs = cnb.check_no_bridges(NEW_SYNTH_FILE)
        self.assertTrue(any("NEW SYNTHETIC FILE" in e for e in errs), errs)

    def test_non_case_lines_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(NON_CASE_LINES), [])

    def test_continuation_widening_flagged(self):
        errs = cnb.check_no_bridges(CONTINUATION_WIDENING)
        self.assertTrue(any("PACKAGE-NAME CONDITIONAL" in e for e in errs), errs)

    def test_deep_continuation_widening_flagged(self):
        errs = cnb.check_no_bridges(DEEP_CONTINUATION_WIDENING)
        self.assertTrue(any("PACKAGE-NAME CONDITIONAL" in e for e in errs), errs)

    def test_pure_rewrap_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(PURE_REWRAP), [])

    def test_import_addition_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(IMPORT_ADDITION), [])



# --- R-1108-91 (review 2026-11-08): the master-push hole -----------------
# Real-repo fixtures for get_diff()/main(): a post-push master checkout
# (origin/master already at the tip, clean tree) with three pushed
# commits, the bridge (a new synth_*.go file) in the NON-tip commit.

_GIT_ENV = {
    "GIT_AUTHOR_NAME": "Gate Fixture",
    "GIT_AUTHOR_EMAIL": "fixture@example.invalid",
    "GIT_COMMITTER_NAME": "Gate Fixture",
    "GIT_COMMITTER_EMAIL": "fixture@example.invalid",
    "GIT_AUTHOR_DATE": "2026-11-08T10:00:00+01:00",
    "GIT_COMMITTER_DATE": "2026-11-08T10:00:00+01:00",
}


def _fixture_env():
    # Fixture git commands run in a temp repo: drop any ambient git
    # redirection (a lane worktree on a temp index exports
    # GIT_INDEX_FILE; without this the fixture commits would read that
    # foreign index and fail). CI never sets these, so this only
    # hardens local runs.
    env = dict(os.environ)
    env.update(_GIT_ENV)
    for var in ("GIT_INDEX_FILE", "GIT_DIR", "GIT_WORK_TREE",
                "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES"):
        env.pop(var, None)
    return env


def _git(cwd, *args):
    subprocess.run(["git", "-C", cwd, *args], check=True,
                   capture_output=True, text=True, env=_fixture_env())


def _git_out(cwd, *args):
    return subprocess.run(["git", "-C", cwd, *args], check=True,
                          capture_output=True, text=True,
                          env=_fixture_env()).stdout.strip()


# Ambient git redirection must never leak into a fixture repo: a lane
# worktree on a temp index exports GIT_INDEX_FILE, and get_diff()
# inherits the test process environment for its `git diff` calls.
_GIT_REDIRECT_VARS = ("GIT_INDEX_FILE", "GIT_DIR", "GIT_WORK_TREE",
                      "GIT_OBJECT_DIRECTORY",
                      "GIT_ALTERNATE_OBJECT_DIRECTORIES")


@contextlib.contextmanager
def _gate_env(**kwargs):
    """Set exactly the given GITHUB_* vars for the block (None = remove).

    Also drops any ambient GIT_* redirection for the block, so the
    gate's own `git diff` calls inside the fixture repo never read a
    foreign index (see _fixture_env)."""
    keys = ("GITHUB_EVENT_BEFORE", "GITHUB_EVENT_NAME")
    saved = {k: os.environ.get(k) for k in keys + _GIT_REDIRECT_VARS}
    try:
        for k, v in kwargs.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v
        for k in _GIT_REDIRECT_VARS:
            os.environ.pop(k, None)
        yield
    finally:
        for k, v in saved.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v


@contextlib.contextmanager
def _in_dir(path):
    old = os.getcwd()
    try:
        os.chdir(path)
        yield
    finally:
        os.chdir(old)


@contextlib.contextmanager
def _push_repo(with_bridge=True):
    """Temp repo shaped like a post-push master checkout.

    C0 is the pre-push tip; C1 carries the bridge (a new synth file)
    iff with_bridge; C2/C3 are benign; origin/master already points at
    the tip, as it does after a push lands. Yields (path, base, tip).
    """
    d = tempfile.mkdtemp(prefix="no-bridges-push-")
    try:
        _git(d, "init", "-q", "-b", "master", ".")
        Path(d, "base.txt").write_text("base\n")
        _git(d, "add", "base.txt")
        _git(d, "commit", "-qm", "C0 base")
        base = _git_out(d, "rev-parse", "HEAD")
        if with_bridge:
            Path(d, "internal", "frontend").mkdir(parents=True)
            Path(d, "internal", "frontend", "synth_foo.go").write_text(
                "package frontend\n// Synthetic foo package.\n")
            _git(d, "add", "internal/frontend/synth_foo.go")
            _git(d, "commit", "-qm", "C1 bridge: new synth_foo.go (NON-tip)")
        else:
            Path(d, "b1.txt").write_text("benign1\n")
            _git(d, "add", "b1.txt")
            _git(d, "commit", "-qm", "C1 benign")
        Path(d, "b2.txt").write_text("benign2\n")
        _git(d, "add", "b2.txt")
        _git(d, "commit", "-qm", "C2 benign")
        Path(d, "b3.txt").write_text("benign3\n")
        _git(d, "add", "b3.txt")
        _git(d, "commit", "-qm", "C3 benign tip")
        tip = _git_out(d, "rev-parse", "HEAD")
        _git(d, "update-ref", "refs/remotes/origin/master", tip)
        yield d, base, tip
    finally:
        shutil.rmtree(d, ignore_errors=True)


def _run_main(argv):
    """Run cnb.main() capturing stdout; return (returncode, output)."""
    buf = io.StringIO()
    with contextlib.redirect_stdout(buf):
        rc = cnb.main(argv)
    return rc, buf.getvalue()


@unittest.skipUnless(shutil.which("git"), "git binary required for push-range fixtures")
class TestGetDiffPushRange(unittest.TestCase):
    def test_push_range_catches_bridge_in_non_tip_commit(self):
        # The R-1108-91 positive control: the bridge is in C1, the tip
        # (C3) is benign. The pre-fix gate inspected HEAD~1 only and
        # passed this push.
        with _push_repo(with_bridge=True) as (d, base, tip):
            with _gate_env(GITHUB_EVENT_BEFORE=base,
                           GITHUB_EVENT_NAME="push"), _in_dir(d):
                diff, source = cnb.get_diff()
                self.assertIn("push range", source)
                self.assertIn("synth_foo.go", diff)
                errs = cnb.check_no_bridges(diff)
                self.assertTrue(any("NEW SYNTHETIC FILE" in e for e in errs), errs)
                rc, out = _run_main([])
                self.assertEqual(rc, 1)
                self.assertIn("NEW SYNTHETIC FILE", out)

    def test_push_range_clean_push_passes(self):
        with _push_repo(with_bridge=False) as (d, base, tip):
            with _gate_env(GITHUB_EVENT_BEFORE=base,
                           GITHUB_EVENT_NAME="push"), _in_dir(d):
                diff, source = cnb.get_diff()
                self.assertIn("push range", source)
                self.assertEqual(cnb.check_no_bridges(diff), [])
                rc, out = _run_main([])
                self.assertEqual(rc, 0, out)

    def test_push_without_before_fails_loudly(self):
        # No push range available on a push run: loud failure, never a
        # silent tip-only inspection.
        with _push_repo(with_bridge=True) as (d, base, tip):
            with _gate_env(GITHUB_EVENT_BEFORE=None,
                           GITHUB_EVENT_NAME="push"), _in_dir(d):
                self.assertEqual(cnb.get_diff(), ("", ""))
                rc, out = _run_main([])
                self.assertEqual(rc, 1)
                self.assertIn("GITHUB_EVENT_BEFORE", out)

    def test_push_zero_before_fails_loudly(self):
        with _push_repo(with_bridge=True) as (d, base, tip):
            with _gate_env(GITHUB_EVENT_BEFORE="0" * 40,
                           GITHUB_EVENT_NAME="push"), _in_dir(d):
                self.assertEqual(cnb.get_diff(), ("", ""))
                rc, out = _run_main([])
                self.assertEqual(rc, 1)
                self.assertIn("zero SHA", out)

    def test_push_unknown_before_fails_loudly(self):
        with _push_repo(with_bridge=True) as (d, base, tip):
            with _gate_env(GITHUB_EVENT_BEFORE="deadbeef" * 5,
                           GITHUB_EVENT_NAME="push"), _in_dir(d):
                self.assertEqual(cnb.get_diff(), ("", ""))
                rc, out = _run_main([])
                self.assertEqual(rc, 1)
                self.assertIn("does not resolve", out)

    def test_empty_push_passes(self):
        # before == HEAD: the range is positively determined and empty.
        with _push_repo(with_bridge=True) as (d, base, tip):
            with _gate_env(GITHUB_EVENT_BEFORE=tip,
                           GITHUB_EVENT_NAME="push"), _in_dir(d):
                diff, source = cnb.get_diff()
                self.assertEqual(diff, "")
                self.assertNotEqual(source, "")
                rc, out = _run_main([])
                self.assertEqual(rc, 0, out)

    def test_before_arg_overrides_missing_env(self):
        with _push_repo(with_bridge=True) as (d, base, tip):
            with _gate_env(GITHUB_EVENT_BEFORE=None,
                           GITHUB_EVENT_NAME=None), _in_dir(d):
                diff, source = cnb.get_diff(before=base)
                self.assertIn("push range", source)
                rc, out = _run_main(["--before", base])
                self.assertEqual(rc, 1)
                self.assertIn("NEW SYNTHETIC FILE", out)

    def test_schedule_run_keeps_tip_fallback(self):
        # Non-push runs introduce no push, so there is no push range to
        # determine: the tip inspection is the pre-existing backstop,
        # not the enforcement point. Pin that the fallback survives.
        with _push_repo(with_bridge=True) as (d, base, tip):
            with _gate_env(GITHUB_EVENT_BEFORE=None,
                           GITHUB_EVENT_NAME="schedule"), _in_dir(d):
                diff, source = cnb.get_diff()
                self.assertIn("local tip-only fallback", source)
                rc, out = _run_main([])
                self.assertEqual(rc, 0, out)

    def test_local_run_keeps_tip_fallback(self):
        with _push_repo(with_bridge=True) as (d, base, tip):
            with _gate_env(GITHUB_EVENT_BEFORE=None,
                           GITHUB_EVENT_NAME=None), _in_dir(d):
                diff, source = cnb.get_diff()
                self.assertIn("local tip-only fallback", source)
                rc, out = _run_main([])
                self.assertEqual(rc, 0, out)

    def test_branch_diff_still_preferred(self):
        # On a feature branch the whole-branch diff is the source, and
        # it is a superset of any single push — it must still win over
        # the push range.
        d = tempfile.mkdtemp(prefix="no-bridges-branch-")
        try:
            _git(d, "init", "-q", "-b", "master", ".")
            Path(d, "base.txt").write_text("base\n")
            _git(d, "add", "base.txt")
            _git(d, "commit", "-qm", "C0 base")
            base = _git_out(d, "rev-parse", "HEAD")
            _git(d, "update-ref", "refs/remotes/origin/master", base)
            _git(d, "checkout", "-qb", "feature")
            Path(d, "internal", "frontend").mkdir(parents=True)
            Path(d, "internal", "frontend", "synth_foo.go").write_text(
                "package frontend\n// Synthetic foo package.\n")
            _git(d, "add", "internal/frontend/synth_foo.go")
            _git(d, "commit", "-qm", "branch bridge")
            with _gate_env(GITHUB_EVENT_BEFORE=None,
                           GITHUB_EVENT_NAME=None), _in_dir(d):
                diff, source = cnb.get_diff()
                self.assertEqual(source, "origin/master...HEAD")
                errs = cnb.check_no_bridges(diff)
                self.assertTrue(any("NEW SYNTHETIC FILE" in e for e in errs), errs)
        finally:
            shutil.rmtree(d, ignore_errors=True)


if __name__ == "__main__":
    unittest.main()
