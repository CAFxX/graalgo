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

The R10-07 fixtures (review 2026-11-09) pin the Rule 4 allowlist
rewrite: a new wire-node kind must flag wherever it is emitted and
however it is spelled (literal in any *.go file, variable-kind
e.node(kind, ...) call, kind assignment, intrinsic-map entry), while
the 47 known kinds and the known-generic kinds must not flag.
The drift tests pin the allowlist against the exporter's own kind
enumeration, so the two cannot drift apart silently.

Positive controls are synthetic diffs that MUST still fail: the gate
is a tripwire, not a rubber stamp.
"""

import re
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



# --- R10-07 (review 2026-11-09): Rule 4 allowlist fixtures -------------
# The old Rule 4 only saw literal e.node("KIND", ...) lines in files
# whose name contains "export.go" with a runtime*/fmt*/sync*/os*/net*
# prefix. These fixtures pin the rewrite: a new kind must flag wherever
# it is emitted and however it is spelled; a known kind must not flag.
# Novel kind spellings (timenewx, atomicptropx, ...) stand in for the
# next bridge — the real R10-07 kinds are pinned as known by the
# KNOWN_* negative controls and by the drift tests below.

# A new stdlib-specific kind emitted literally from a non-export.go file
# (synth_machinery.go): file-blind under the old rule. Must flag.
NEW_KIND_SYNTH_FILE = """\
diff --git a/internal/frontend/synth_machinery.go b/internal/frontend/synth_machinery.go
index 11111111..22222222 100644
--- a/internal/frontend/synth_machinery.go
+++ b/internal/frontend/synth_machinery.go
@@ -1460,6 +1460,7 @@ func (e *exporter) exportTimeNow(c *ast.CallExpr) (*Node, error) {
 \tif !ok {
 \t\treturn nil, fmt.Errorf("bad time op")
 \t}
+\twire := e.node("timenewx", c, fields)
 \treturn wire, nil
"""

# A new stdlib-specific kind emitted literally from export.go with a
# prefix the old regex never matched (atomic*): prefix-blind. Must flag.
NEW_KIND_EXPORT_PREFIX = """\
diff --git a/internal/frontend/export.go b/internal/frontend/export.go
index 11111111..22222222 100644
--- a/internal/frontend/export.go
+++ b/internal/frontend/export.go
@@ -9360,6 +9360,7 @@ func (e *exporter) exportAtomicOp(c *ast.CallExpr) (*Node, error) {
 \tif !ok {
 \t\treturn nil, fmt.Errorf("bad atomic op")
 \t}
+\twire := e.node("atomicptropx", c, fields)
 \treturn wire, nil
"""

# A new kind via a variable-kind site (mrdesugar_m5.go shape): the hunk
# adds the assignment AND the e.node(kind, ...) call. Must flag.
NEW_KIND_VARIABLE = """\
diff --git a/internal/frontend/mrdesugar_m5.go b/internal/frontend/mrdesugar_m5.go
index 11111111..22222222 100644
--- a/internal/frontend/mrdesugar_m5.go
+++ b/internal/frontend/mrdesugar_m5.go
@@ -3320,6 +3320,7 @@ func (e *exporter) exportRTStackOp(c *ast.CallExpr, plan rtStackPlan) (*Node, e
 \tif len(c.Args) != 1 {
 \t\treturn nil, fmt.Errorf("takes 1 argument")
 \t}
+\tkind = "timefoox"
 \ta, err := e.exportExpr(c.Args[0])
 \tif err != nil {
 \t\treturn nil, err
 \t}
+\tcall := e.node(kind, c, fields)
 \treturn call, nil
"""

# A new kind via a standalone intrinsic-map entry (exportMathIntrinsic
# shape): the hunk adds only the map line, no e.node line. Must flag.
NEW_KIND_INTRINSIC_MAP = """\
diff --git a/internal/frontend/synth_machinery.go b/internal/frontend/synth_machinery.go
index 11111111..22222222 100644
--- a/internal/frontend/synth_machinery.go
+++ b/internal/frontend/synth_machinery.go
@@ -1590,6 +1590,7 @@ func (e *exporter) exportMathIntrinsic(c *ast.CallExpr, name string) (*Node, er
 \tkind, ok := map[string]string{
 \t\t"Float64bits":     "mathf64bits",
+\t\t"NewFunc":         "mathfoox",
 \t}[name]
 \tif !ok {
 \t\treturn nil, scopeErrorf("not a recognized intrinsic", name)
"""

# A variable-kind e.node(kind, ...) call with NO kind assignment in the
# hunk: unresolvable here, so it must flag for human review, not pass.
UNRESOLVED_VARIABLE_KIND = """\
diff --git a/internal/frontend/export.go b/internal/frontend/export.go
index 11111111..22222222 100644
--- a/internal/frontend/export.go
+++ b/internal/frontend/export.go
@@ -8810,6 +8810,7 @@ func (e *exporter) exportSelectorCall(c *ast.CallExpr) (*Node, error) {
 \tif err != nil {
 \t\treturn nil, err
 \t}
+\treturn e.node(kind, c, fields), nil
 \treturn nil, fmt.Errorf("unreachable")
"""

# Negative: a KNOWN kind emitted literally from a non-export.go file
# (synth_reflect.go, R10-07 Category C shape) must NOT flag.
KNOWN_KIND_SYNTH_FILE = """\
diff --git a/internal/frontend/synth_reflect.go b/internal/frontend/synth_reflect.go
index 11111111..22222222 100644
--- a/internal/frontend/synth_reflect.go
+++ b/internal/frontend/synth_reflect.go
@@ -2920,6 +2920,7 @@ func (e *exporter) exportReflectOp(c *ast.CallExpr) (*Node, error) {
 \tif !ok {
 \t\treturn nil, fmt.Errorf("bad reflect op")
 \t}
+\twire := e.node("reflectop", c, fields)
 \treturn wire, nil
"""

# Negative: a KNOWN kind via a variable-kind site (export.go:8816 shape:
# kind := "runtimefuncname", reassigned to "runtimefuncentry", then
# e.node(kind, ...)) must NOT flag.
KNOWN_VARIABLE_KIND = """\
diff --git a/internal/frontend/export.go b/internal/frontend/export.go
index 11111111..22222222 100644
--- a/internal/frontend/export.go
+++ b/internal/frontend/export.go
@@ -8810,6 +8810,7 @@ func (e *exporter) exportFuncForPC(c *ast.CallExpr) (*Node, error) {
 \tif err != nil {
 \t\treturn nil, err
 \t}
+\tkind := "runtimefuncname"
+\tif mname == "Entry" {
+\t\tkind = "runtimefuncentry"
+\t}
+\treturn e.node(kind, c, map[string]any{"f": recv, "type": typ}), nil
 \treturn nil, fmt.Errorf("unreachable")
"""

# Negative: a KNOWN intrinsic-map entry (exportMathIntrinsic shape)
# must NOT flag.
KNOWN_KIND_INTRINSIC_MAP = """\
diff --git a/internal/frontend/synth_machinery.go b/internal/frontend/synth_machinery.go
index 11111111..22222222 100644
--- a/internal/frontend/synth_machinery.go
+++ b/internal/frontend/synth_machinery.go
@@ -1590,6 +1590,7 @@ func (e *exporter) exportMathIntrinsic(c *ast.CallExpr, name string) (*Node, er
 \tkind, ok := map[string]string{
 \t\t"Float64bits":     "mathf64bits",
+\t\t"Sqrt":            "mathsqrt",
 \t}[name]
 \tif !ok {
 \t\treturn nil, scopeErrorf("not a recognized intrinsic", name)
"""

# Negative: a v2-only generic kind (v2/emit/nodes.go) must NOT flag.
KNOWN_KIND_V2_GENERIC = """\
diff --git a/internal/frontend/v2/emit/nodes.go b/internal/frontend/v2/emit/nodes.go
index 11111111..22222222 100644
--- a/internal/frontend/v2/emit/nodes.go
+++ b/internal/frontend/v2/emit/nodes.go
@@ -535,6 +535,7 @@ func (e *emitter) emitCompositeLit(c *ast.CompositeLit) (*Node, error) {
 \tif err != nil {
 \t\treturn nil, err
 \t}
+\treturn e.node("compositelit", c, map[string]any{"x": x}), nil
 \treturn nil, fmt.Errorf("unreachable")
"""

# Negative: an adjudicated kind (runtimenumgoroutine, PKG-023) must NOT flag.
KNOWN_KIND_ADJUDICATED = """\
diff --git a/internal/frontend/export.go b/internal/frontend/export.go
index 11111111..22222222 100644
--- a/internal/frontend/export.go
+++ b/internal/frontend/export.go
@@ -8400,6 +8400,7 @@ func (e *exporter) exportNumGoroutine(c *ast.CallExpr) (*Node, error) {
 \tif err != nil {
 \t\treturn nil, err
 \t}
+\twire := e.node("runtimenumgoroutine", c, fields)
 \treturn wire, nil
"""

# --- R10-07 drift tests: the allowlist must track the exporter ---------
REPO_ROOT = Path(__file__).resolve().parent.parent.parent


def _load_validator_kinds():
    """_exporter_kinds() from the repo's v1 validator, or None when the
    validator is not present next to this test (standalone use)."""
    import importlib.util

    validator = (REPO_ROOT / "internal" / "frontend" / "tools"
                 / "validate_frontend_v1.py")
    if not validator.is_file():
        return None
    spec = importlib.util.spec_from_file_location(
        "validate_frontend_v1", validator)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module._exporter_kinds()

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

    def test_new_kind_synth_file_flagged(self):
        errs = cnb.check_no_bridges(NEW_KIND_SYNTH_FILE)
        self.assertTrue(any("SUSPICIOUS WIRE NODE" in e for e in errs), errs)

    def test_new_kind_export_prefix_flagged(self):
        errs = cnb.check_no_bridges(NEW_KIND_EXPORT_PREFIX)
        self.assertTrue(any("SUSPICIOUS WIRE NODE" in e for e in errs), errs)

    def test_new_kind_variable_flagged(self):
        errs = cnb.check_no_bridges(NEW_KIND_VARIABLE)
        self.assertTrue(any("SUSPICIOUS WIRE NODE" in e for e in errs), errs)

    def test_new_kind_intrinsic_map_flagged(self):
        errs = cnb.check_no_bridges(NEW_KIND_INTRINSIC_MAP)
        self.assertTrue(any("SUSPICIOUS WIRE NODE" in e for e in errs), errs)

    def test_unresolved_variable_kind_flagged(self):
        errs = cnb.check_no_bridges(UNRESOLVED_VARIABLE_KIND)
        self.assertTrue(any("UNRESOLVED WIRE NODE" in e for e in errs), errs)

    def test_known_kind_synth_file_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(KNOWN_KIND_SYNTH_FILE), [])

    def test_known_variable_kind_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(KNOWN_VARIABLE_KIND), [])

    def test_known_kind_intrinsic_map_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(KNOWN_KIND_INTRINSIC_MAP), [])

    def test_known_kind_v2_generic_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(KNOWN_KIND_V2_GENERIC), [])

    def test_known_kind_adjudicated_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(KNOWN_KIND_ADJUDICATED), [])

    def test_allowlist_covers_exporter_kinds(self):
        self.assertEqual(len(cnb.KNOWN_WIRE_NODE_KINDS), 47)
        self.assertEqual(len(cnb.KNOWN_GENERIC_NODE_KINDS_V1), 47)
        exporter = _load_validator_kinds()
        if exporter is None:
            self.skipTest("validator not present next to test file")
        self.assertEqual(
            exporter,
            cnb.KNOWN_WIRE_NODE_KINDS | cnb.KNOWN_GENERIC_NODE_KINDS_V1)

    def test_v2_generic_kinds_known(self):
        nodes = (REPO_ROOT / "internal" / "frontend" / "v2" / "emit"
                 / "nodes.go")
        if not nodes.is_file():
            self.skipTest("v2 emitter not present next to test file")
        src = nodes.read_text()
        emitted = set(re.findall(r'\.node\s*\(\s*"([^"]+)"', src))
        for kind in emitted:
            self.assertIn(
                kind,
                cnb.KNOWN_WIRE_NODE_KINDS | cnb.KNOWN_GENERIC_NODE_KINDS,
                f"v2 emits unknown kind {kind!r}")
        for kind in cnb.KNOWN_GENERIC_NODE_KINDS_V2:
            self.assertIn(kind, emitted)



if __name__ == "__main__":
    unittest.main()
