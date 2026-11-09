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

Positive controls are synthetic diffs that MUST still fail: the gate
is a tripwire, not a rubber stamp.
"""

import sys
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


# get_diff() diff-source selection (R05-G-01, 2026-11-09): the gate
# used to inspect only the tip commit on a multi-commit master push.
# These tests build real scratch repos (the gate shells out to git)
# and pin which diff each source shape yields. Imports are kept with
# the tests they serve.
import os
import shutil
import subprocess
import tempfile


def _git(repo, *args):
    """Run git in a scratch repo with a fixed identity; fail the test on error."""
    env = dict(os.environ)
    env.update({
        "GIT_AUTHOR_NAME": "gate-test",
        "GIT_AUTHOR_EMAIL": "gate-test@example.invalid",
        "GIT_COMMITTER_NAME": "gate-test",
        "GIT_COMMITTER_EMAIL": "gate-test@example.invalid",
    })
    r = subprocess.run(["git", *args], cwd=repo, capture_output=True,
                       text=True, env=env, timeout=60)
    assert r.returncode == 0, (args, r.stdout, r.stderr)
    return r.stdout.strip()


class TestGetDiff(unittest.TestCase):
    """get_diff() source selection on real scratch repos.

    Shape under test (R05-G-01): a 3-commit push where the middle
    commit adds a bridge (new synth_foo.go) and the tip commit is
    innocuous, with origin/master == HEAD (the post-push CI checkout,
    clean tree). Without a base the gate sees only the tip commit;
    with the pre-push base it sees the whole pushed range.
    """

    def setUp(self):
        self._old_cwd = os.getcwd()
        self.tmp = tempfile.mkdtemp(prefix="test-get-diff-")
        self.repo = os.path.join(self.tmp, "repo")
        os.makedirs(self.repo)
        _git(self.repo, "init", "-q", "-b", "master")
        self._commit("a.txt", "a\n", "c1 base")
        self.base = _git(self.repo, "rev-parse", "HEAD")
        self._commit("internal/frontend/synth_foo.go",
                     "package frontend\n\n// Synthetic foo package.\n",
                     "c2 adds bridge")
        self._commit("b.txt", "b\n", "c3 innocuous tip")
        self.head = _git(self.repo, "rev-parse", "HEAD")
        # Simulate the CI checkout after a direct master push.
        _git(self.repo, "update-ref", "refs/remotes/origin/master", self.head)
        os.chdir(self.repo)

    def _commit(self, path, content, msg):
        full = os.path.join(self.repo, path)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w") as f:
            f.write(content)
        _git(self.repo, "add", ".")
        _git(self.repo, "commit", "-q", "-m", msg)

    def tearDown(self):
        os.chdir(self._old_cwd)
        shutil.rmtree(self.tmp, ignore_errors=True)

    def _main_with_base(self, base):
        old = os.environ.get("NO_BRIDGES_BASE")
        os.environ["NO_BRIDGES_BASE"] = base
        try:
            return cnb.main()
        finally:
            if old is None:
                os.environ.pop("NO_BRIDGES_BASE", None)
            else:
                os.environ["NO_BRIDGES_BASE"] = old

    def test_multi_commit_push_with_base_sees_non_tip_bridge(self):
        diff, source = cnb.get_diff(self.base)
        self.assertIn("pushed range", source)
        self.assertIn("synth_foo.go", diff)
        errs = cnb.check_no_bridges(diff)
        self.assertTrue(any("NEW SYNTHETIC FILE" in e for e in errs), errs)

    def test_multi_commit_push_without_base_is_tip_only(self):
        # Documents the fallback's limit: with no base supplied the
        # gate can only see the tip commit — the base is what CI now
        # supplies (see test_main_with_base_env_*).
        diff, source = cnb.get_diff()
        self.assertIn("HEAD~1...HEAD", source)
        self.assertNotIn("synth_foo.go", diff)
        self.assertEqual(cnb.check_no_bridges(diff), [])

    def test_zero_base_fails_loudly(self):
        with self.assertRaises(ValueError):
            cnb.get_diff("0" * 40)

    def test_unresolvable_base_fails_loudly_not_tip_fallback(self):
        # A supplied-but-absent base must not silently degrade to the
        # tip-only diff.
        with self.assertRaises(ValueError):
            cnb.get_diff("deadbeef" * 5)

    def test_empty_range_returns_no_diff(self):
        # base == HEAD: the push introduced nothing new — no diff to
        # inspect, and main() fails loudly on ("", "") rather than
        # passing vacuously.
        self.assertEqual(cnb.get_diff(self.head), ("", ""))

    def test_pr_shape_without_base_sees_full_range(self):
        # Feature-branch shape: origin/master behind HEAD by two
        # commits — the pre-existing origin/master...HEAD candidate
        # covers the range even with no base.
        _git(self.repo, "update-ref", "refs/remotes/origin/master", self.base)
        diff, source = cnb.get_diff()
        self.assertIn("origin/master...HEAD", source)
        self.assertIn("synth_foo.go", diff)

    def test_working_tree_fallback(self):
        # A staged new synthetic file on a clean checkout whose
        # origin/master == HEAD: the working-tree candidate sees it.
        bar = os.path.join(self.repo, "internal", "frontend", "synth_bar.go")
        with open(bar, "w") as f:
            f.write("package frontend\n")
        _git(self.repo, "add", ".")
        diff, source = cnb.get_diff()
        self.assertIn("working tree", source)
        self.assertIn("synth_bar.go", diff)
        errs = cnb.check_no_bridges(diff)
        self.assertTrue(any("NEW SYNTHETIC FILE" in e for e in errs), errs)

    def test_main_with_base_env_flags_bridge(self):
        # Pins the CI wiring end to end: NO_BRIDGES_BASE -> get_diff
        # -> verdict. (main() prints its report; the return code is
        # the assertion.)
        self.assertEqual(self._main_with_base(self.base), 1)

    def test_main_with_base_env_passes_clean_range(self):
        # Same wiring, clean range: base at the bridge commit, so the
        # range holds only the innocuous tip commit.
        tip_parent = _git(self.repo, "rev-parse", "HEAD~1")
        self.assertEqual(self._main_with_base(tip_parent), 0)

    def test_main_with_zero_base_env_fails_loudly(self):
        self.assertEqual(self._main_with_base("0" * 40), 1)


if __name__ == "__main__":
    unittest.main()
