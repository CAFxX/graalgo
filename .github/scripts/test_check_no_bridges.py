#!/usr/bin/env python3
"""Regression tests for check_no_bridges.py.

Run: python3 .github/scripts/test_check_no_bridges.py

Fixtures are verbatim hunks from the p3fmt-stub branch diff
(4a494cad...16547f17, 2026-11-05), which the pre-fix script failed with
3 false positives (CI run 42039218294, step "No-bridges check"):
- the loader.go narrowing edit (Rule 2 misfire),
- the io_test.go / timecontext_test.go drift tests calling pre-existing
  synthetic builders (Rule 5 misfire — no test-file exemption).

Positive controls are synthetic diffs that MUST still fail: the gate
is a tripwire, not a rubber stamp.
"""

import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import check_no_bridges as cnb

# --- Fixture 1: Rule 2 narrowing (verbatim hunk, branch diff) -----------
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

# --- Fixture 2: Rule 5 drift test, new file (verbatim, branch diff) ------
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

# --- Fixture 3: Rule 5 drift test, existing file (verbatim hunk) ---------
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


if __name__ == "__main__":
    unittest.main()
