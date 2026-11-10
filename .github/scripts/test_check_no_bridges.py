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


# --- Rule 5b (review 2026-11-09-12, N-P1-6): syntheticSources map entries ---
# A new entry in the registry IS a new synthetic package registration,
# even though no addFunc/synthetic*Package(/registerSynthetic call exists.
NEW_SYNTH_ENTRY = """\
diff --git a/internal/frontend/synth_machinery.go b/internal/frontend/synth_machinery.go
index 11111111..22222222 100644
--- a/internal/frontend/synth_machinery.go
+++ b/internal/frontend/synth_machinery.go
@@ -70,6 +70,7 @@ var syntheticSources = map[string]string{
 \t"time":    syntheticTimeSource,
 \t"context": syntheticContextSource,
+\t"foo":     syntheticFooSource,
 \t"errors":  syntheticErrorsSource,
"""

# Re-activating a RETIRED entry: the commented-out "reflect" line is
# removed and the live entry added. The removed line is a comment, so
# it contributes no key to the removed set — the added entry flags.
REACTIVATED_RETIRED_ENTRY = """\
diff --git a/internal/frontend/synth_machinery.go b/internal/frontend/synth_machinery.go
index 11111111..22222222 100644
--- a/internal/frontend/synth_machinery.go
+++ b/internal/frontend/synth_machinery.go
@@ -108,6 +108,6 @@ var syntheticSources = map[string]string{
 \t"os":    syntheticOsSource,
 \t"io/fs": syntheticIoFsSource,
-\t// "reflect": syntheticReflectSource, // RETIRED - uses disk importer
+\t"reflect": syntheticReflectSource,
 \t"iter":                 syntheticIterSource,
"""

# A pure rewrap moving "time" within the literal (removed + added with
# the same key in one hunk) is a reshuffle, not a new registration.
SYNTH_ENTRY_REWRAP = """\
diff --git a/internal/frontend/synth_machinery.go b/internal/frontend/synth_machinery.go
index 11111111..22222222 100644
--- a/internal/frontend/synth_machinery.go
+++ b/internal/frontend/synth_machinery.go
@@ -70,6 +70,6 @@ var syntheticSources = map[string]string{
-\t"time":    syntheticTimeSource,
 \t"context": syntheticContextSource,
+\t"time":    syntheticTimeSource,
 \t"errors":  syntheticErrorsSource,
"""

# Adding a still-commented (retired) entry registers nothing.
COMMENTED_ENTRY_ADDED = """\
diff --git a/internal/frontend/synth_machinery.go b/internal/frontend/synth_machinery.go
index 11111111..22222222 100644
--- a/internal/frontend/synth_machinery.go
+++ b/internal/frontend/synth_machinery.go
@@ -70,6 +70,7 @@ var syntheticSources = map[string]string{
 \t"time":    syntheticTimeSource,
 \t"context": syntheticContextSource,
+\t// "foo": syntheticFooSource, // RETIRED - not yet real
 \t"errors":  syntheticErrorsSource,
"""

# The same entry shape in any other file is not a registration: only
# the registry files' literals are read by the importer.
SYNTH_ENTRY_OTHER_FILE = """\
diff --git a/internal/frontend/p3frontend/loader.go b/internal/frontend/p3frontend/loader.go
index 11111111..22222222 100644
--- a/internal/frontend/p3frontend/loader.go
+++ b/internal/frontend/p3frontend/loader.go
@@ -440,5 +440,6 @@ func (l *Loader) ResolveImport(fromDir, path string) (*PackageRef, error) {
 \t// Synthetic packages resolve independently of fromDir; keep them out
 \t// of the anchored cache.
 \tswitch path {
+\t"foo": syntheticFooSource,
 \tcase "time", "context", "errors", "sort", "bytes", "math", "strings",
"""

# Concatenated source value (the real crypto/cipher shape): the value
# starts with a *Source ident, so it is still one entry.
CONCAT_VALUE_ENTRY = """\
diff --git a/internal/frontend/synth_machinery.go b/internal/frontend/synth_machinery.go
index 11111111..22222222 100644
--- a/internal/frontend/synth_machinery.go
+++ b/internal/frontend/synth_machinery.go
@@ -150,6 +150,7 @@ var syntheticSources = map[string]string{
 \t"crypto/subtle": syntheticSubtleSource,
 \t"crypto/hmac":   syntheticHmacSource,
+\t"crypto/cipher": syntheticCipherSource + syntheticCipherGcmSource,
 \t"crypto/aes":    syntheticAesSource,
"""

# Sibling registry: a new dedicatedSyntheticStubs entry in export.go
# is the same hole in the other map — a bare synthetic*Package ident,
# no call parens, so Rule 5 misses it too.
DEDICATED_STUB_ENTRY = """\
diff --git a/internal/frontend/export.go b/internal/frontend/export.go
index 11111111..22222222 100644
--- a/internal/frontend/export.go
+++ b/internal/frontend/export.go
@@ -586,6 +586,7 @@ var dedicatedSyntheticStubs = map[string]func() *types.Package{
 \t"runtime": syntheticRuntimePackage,
 \t"unsafe":  syntheticUnsafePackage,
+\t"foo":     syntheticFooPackage,
 }
"""

# Same rewrap exemption in the sibling registry.
DEDICATED_STUB_REWRAP = """\
diff --git a/internal/frontend/export.go b/internal/frontend/export.go
index 11111111..22222222 100644
--- a/internal/frontend/export.go
+++ b/internal/frontend/export.go
@@ -586,6 +586,6 @@ var dedicatedSyntheticStubs = map[string]func() *types.Package{
-\t"runtime": syntheticRuntimePackage,
 \t"unsafe":  syntheticUnsafePackage,
+\t"runtime": syntheticRuntimePackage,
 }
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

    # --- Rule 5b (N-P1-6): syntheticSources map entries ---

    def test_new_synth_entry_flagged(self):
        errs = cnb.check_no_bridges(NEW_SYNTH_ENTRY)
        self.assertTrue(any("SYNTHETIC REGISTRATION" in e and "foo" in e
                            for e in errs), errs)

    def test_reactivated_retired_entry_flagged(self):
        errs = cnb.check_no_bridges(REACTIVATED_RETIRED_ENTRY)
        self.assertTrue(any("SYNTHETIC REGISTRATION" in e and "reflect" in e
                            for e in errs), errs)

    def test_synth_entry_rewrap_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(SYNTH_ENTRY_REWRAP), [])

    def test_commented_entry_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(COMMENTED_ENTRY_ADDED), [])

    def test_synth_entry_other_file_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(SYNTH_ENTRY_OTHER_FILE), [])

    def test_concat_value_entry_flagged(self):
        errs = cnb.check_no_bridges(CONCAT_VALUE_ENTRY)
        self.assertTrue(any("SYNTHETIC REGISTRATION" in e
                            and "crypto/cipher" in e for e in errs), errs)

    def test_dedicated_stub_entry_flagged(self):
        errs = cnb.check_no_bridges(DEDICATED_STUB_ENTRY)
        self.assertTrue(any("SYNTHETIC REGISTRATION" in e and "foo" in e
                            for e in errs), errs)

    def test_dedicated_stub_rewrap_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(DEDICATED_STUB_REWRAP), [])


class TestGetDiffUntracked(unittest.TestCase):
    """get_diff() must see untracked source files (N-P1-6): `git diff`
    never shows them, so Rule 1 could never fire on a new synthetic
    file sitting uncommitted in the working tree."""

    def _init_repo(self, tmp):
        subprocess.run(["git", "init", "-q", "-b", "main", tmp],
                       check=True)
        subprocess.run(["git", "-C", tmp, "config", "user.email", "t@t"],
                       check=True)
        subprocess.run(["git", "-C", tmp, "config", "user.name", "t"],
                       check=True)
        Path(tmp, "README.md").write_text("x\n")
        subprocess.run(["git", "-C", tmp, "add", "."], check=True)
        subprocess.run(["git", "-C", tmp, "commit", "-qm", "base"],
                       check=True)

    def test_untracked_synth_file_visible_to_rule1(self):
        with tempfile.TemporaryDirectory() as tmp:
            self._init_repo(tmp)
            synth = Path(tmp, "internal", "frontend", "synth_foo.go")
            synth.parent.mkdir(parents=True)
            synth.write_text("package frontend\n\n// Synthetic foo.\n")
            diff, source = cnb.get_diff(cwd=tmp)
            self.assertIn("untracked", source)
            errs = cnb.check_no_bridges(diff)
            self.assertTrue(any("NEW SYNTHETIC FILE" in e for e in errs),
                            errs)

    def test_untracked_files_appended_to_primary_diff(self):
        with tempfile.TemporaryDirectory() as tmp:
            self._init_repo(tmp)
            # A benign tracked change (primary diff) plus an untracked
            # synthetic file: the gate must judge both.
            Path(tmp, "README.md").write_text("x\nchanged\n")
            synth = Path(tmp, "internal", "frontend", "synth_foo.go")
            synth.parent.mkdir(parents=True)
            synth.write_text("package frontend\n\n// Synthetic foo.\n")
            diff, source = cnb.get_diff(cwd=tmp)
            self.assertIn("working tree", source)
            self.assertIn("untracked", source)
            errs = cnb.check_no_bridges(diff)
            self.assertTrue(any("NEW SYNTHETIC FILE" in e for e in errs),
                            errs)

    def test_untracked_non_source_files_ignored(self):
        with tempfile.TemporaryDirectory() as tmp:
            self._init_repo(tmp)
            Path(tmp, "notes.txt").write_text("scratch\n")
            diff, source = cnb.get_diff(cwd=tmp)
            self.assertEqual((diff, source), ("", ""))


class TestGetDiffUnion(unittest.TestCase):
    """get_diff() unions every diff source (2026-11-10, L12-P1-B): the
    old first-match loop let a stale origin/master mask the working
    tree — a planted syntheticSources entry escaped Rule 5b live
    because the stale range diff fired first and the loop broke."""

    def _git(self, tmp, *args):
        subprocess.run(["git", "-C", tmp, *args], check=True,
                       capture_output=True, text=True)

    def _init_repo(self, tmp, with_map=True):
        subprocess.run(["git", "init", "-q", "-b", "main", tmp],
                       check=True)
        self._git(tmp, "config", "user.email", "t@t")
        self._git(tmp, "config", "user.name", "t")
        Path(tmp, "README.md").write_text("x\n")
        if with_map:
            mdir = Path(tmp, "internal", "frontend")
            mdir.mkdir(parents=True)
            (mdir / "synth_machinery.go").write_text(
                "package frontend\n\n"
                "var syntheticSources = map[string]string{\n"
                "\t\"time\": syntheticTimeSource,\n"
                "}\n")
        subprocess.run(["git", "-C", tmp, "add", "."], check=True)
        subprocess.run(["git", "-C", tmp, "commit", "-qm", "base"],
                       check=True)

    def _add_planted_entry(self, tmp):
        p = Path(tmp, "internal", "frontend", "synth_machinery.go")
        p.write_text(p.read_text().replace(
            "\t\"time\": syntheticTimeSource,\n",
            "\t\"time\": syntheticTimeSource,\n"
            "\t\"planted\": syntheticPlantedSource,\n"))

    def test_stale_range_does_not_shadow_working_tree(self):
        # origin/master lags HEAD (two commits landed since the
        # worktree was claimed): the range diff is non-empty, but the
        # uncommitted planted entry must still be judged.
        with tempfile.TemporaryDirectory() as tmp:
            self._init_repo(tmp)
            base = subprocess.run(
                ["git", "-C", tmp, "rev-parse", "HEAD"],
                check=True, capture_output=True, text=True).stdout.strip()
            Path(tmp, "README.md").write_text("x\nsecond\n")
            subprocess.run(["git", "-C", tmp, "commit", "-qam", "second"],
                           check=True)
            # Stale tracking ref, as in a lane worktree whose
            # origin/master predates the latest master commits.
            self._git(tmp, "update-ref", "refs/remotes/origin/master", base)
            self._add_planted_entry(tmp)
            diff, source = cnb.get_diff(cwd=tmp)
            self.assertIn("working tree", source)
            self.assertIn("origin/master...HEAD", source)
            errs = cnb.check_no_bridges(diff)
            self.assertTrue(
                any("SYNTHETIC REGISTRATION" in e and "planted" in e
                    for e in errs),
                errs)

    def test_same_line_in_two_sources_reported_once(self):
        # A staged line appears in both `git diff HEAD` and
        # `git diff --cached`: the union contains it twice, but the
        # gate must report it once.
        with tempfile.TemporaryDirectory() as tmp:
            self._init_repo(tmp)
            head = subprocess.run(
                ["git", "-C", tmp, "rev-parse", "HEAD"],
                check=True, capture_output=True, text=True).stdout.strip()
            self._git(tmp, "update-ref", "refs/remotes/origin/master", head)
            self._add_planted_entry(tmp)
            subprocess.run(["git", "-C", tmp, "add", "."],
                           check=True)
            diff, source = cnb.get_diff(cwd=tmp)
            self.assertIn("working tree", source)
            self.assertIn("staged", source)
            errs = cnb.check_no_bridges(diff)
            planted = [e for e in errs
                       if "SYNTHETIC REGISTRATION" in e
                       and "planted" in e]
            self.assertEqual(len(planted), 1, errs)


if __name__ == "__main__":
    unittest.main()
