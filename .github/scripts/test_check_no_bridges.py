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

# --- R23-P1-1 (review 2026-11-12-23): panic-stub gate evasions --------
# The gate's literal regex required the stub keyword at the START of
# the message, so the codebase's dominant fail-fast phrasing evaded
# it: the three lines below were run against the gate's actual regexes
# and all three returned no match. Each must now flag PANIC STUB.
# The scope/desugar counts for the live tree are pinned by
# scope_panic_manifest.json (see check_scope_panic_inventory.py).

# Verbatim evading line 1: package-prefixed scope panic (M8).
SCOPE_PANIC_M8 = """\
diff --git a/internal/frontend/synth_os.go b/internal/frontend/synth_os.go
index 00000000..11111111 100644
--- a/internal/frontend/synth_os.go
+++ b/internal/frontend/synth_os.go
@@ -1,3 +1,4 @@
 package frontend
+\tpanic("os.Chown: not in M8 scope (needs numeric chown(2); re-test: Panama FFI numeric-chown intrinsic)")
"""

# Verbatim evading line 2: scope panic in archive/tar (M7).
SCOPE_PANIC_TAR = """\
diff --git a/internal/frontend/synth_archive_tar.go b/internal/frontend/synth_archive_tar.go
index 00000000..11111111 100644
--- a/internal/frontend/synth_archive_tar.go
+++ b/internal/frontend/synth_archive_tar.go
@@ -1,3 +1,4 @@
 package frontend
+\tpanic("archive/tar.Read: not in M7 scope (multi-result calls need WS D; re-test when it lands)")
"""

# Verbatim evading line 3: "not implemented" hidden behind a package
# prefix — the keyword is present but not at string start.
PREFIXED_NOT_IMPLEMENTED = """\
diff --git a/internal/frontend/synth_mathrandv2.go b/internal/frontend/synth_mathrandv2.go
index 00000000..11111111 100644
--- a/internal/frontend/synth_mathrandv2.go
+++ b/internal/frontend/synth_mathrandv2.go
@@ -1,3 +1,4 @@
 package frontend
+\tpanic("math/rand/v2: ChaCha8.Read not implemented (P3-M9 tier-5b scope-limit: non-standard ECDSA curves); re-test when ChaCha8 lands")
"""

# Bare-scope variant: "not in scope" with no M-number (16 live lines:
# 15 in synth_testing*.go, 1 in synth_time_context.go) — the review's
# `grep 'not in .* scope'` cannot match this phrasing at all, so it was
# missing from the 344 count.
SCOPE_PANIC_BARE = """\
diff --git a/internal/frontend/synth_testing.go b/internal/frontend/synth_testing.go
index 00000000..11111111 100644
--- a/internal/frontend/synth_testing.go
+++ b/internal/frontend/synth_testing.go
@@ -1,3 +1,4 @@
 package frontend
+\tpanic("testing.outputWriter.Write: not in scope (multi-result calls need WS D; re-test when it lands)")
"""

# Adjacent fail-fast family found while hunting the R23-P1-1 class:
# multi-result-desugar stubs (554 live lines, 47 of them generated-code
# literals in synth_math_big.go). Long form.
DESUGAR_PANIC_LONG = """\
diff --git a/internal/frontend/synth_hash_fnv.go b/internal/frontend/synth_hash_fnv.go
index 00000000..11111111 100644
--- a/internal/frontend/synth_hash_fnv.go
+++ b/internal/frontend/synth_hash_fnv.go
@@ -1,3 +1,4 @@
 package frontend
+\tpanic("hash/fnv: Write: multi-result call needs the mrM8 desugar (re-test: WS D)")
"""

# Desugar family, short form.
DESUGAR_PANIC_SHORT = """\
diff --git a/internal/frontend/synth_crypto_fips140_nistec.go b/internal/frontend/synth_crypto_fips140_nistec.go
index 00000000..11111111 100644
--- a/internal/frontend/synth_crypto_fips140_nistec.go
+++ b/internal/frontend/synth_crypto_fips140_nistec.go
@@ -1,3 +1,4 @@
 package frontend
+\tpanic("crypto/internal/fips140/nistec: SetBytes: multi-result desugar")
"""

# F1 (review R23-P1-1, 2026-11-13): the callee-name desugar variant.
# Verbatim evading lines from internal/frontend/synth_os.go:2332, 2345 —
# the token between `multi-result` and `needs` is the callee name
# (io.ReadAll / io.WriteString), not `call(s)`, so the R23-P1-1 phrase
# missed them. Each must now flag PANIC STUB.
DESUGAR_PANIC_CALLEE_READALL = """\
diff --git a/internal/frontend/synth_os.go b/internal/frontend/synth_os.go
index 00000000..11111111 100644
--- a/internal/frontend/synth_os.go
+++ b/internal/frontend/synth_os.go
@@ -1,3 +1,4 @@
 package frontend
+\tpanic("os: __fileReadFrom: multi-result io.ReadAll needs the mrM8 desugar (re-test: WS D)")
"""

DESUGAR_PANIC_CALLEE_WRITESTRING = """\
diff --git a/internal/frontend/synth_os.go b/internal/frontend/synth_os.go
index 00000000..11111111 100644
--- a/internal/frontend/synth_os.go
+++ b/internal/frontend/synth_os.go
@@ -1,3 +1,4 @@
 package frontend
+\tpanic("os: __fileWriteTo: multi-result io.WriteString needs the mrM8 desugar (re-test: WS D)")
"""

# Scope panic through the composed form: the scope phrase must be
# caught inside fmt.Sprintf's first string argument too.
SCOPE_PANIC_COMPOSED = """\
diff --git a/internal/frontend/synth_os.go b/internal/frontend/synth_os.go
index 00000000..11111111 100644
--- a/internal/frontend/synth_os.go
+++ b/internal/frontend/synth_os.go
@@ -1,3 +1,4 @@
 package frontend
+\tpanic(fmt.Sprintf("os.Chown: not in M8 scope (%s)", reason))
"""

# Generated-code literal form (synth_math_big.go builds its synthetic
# source as string literals, so a new stub there arrives as
# `"\tpanic(\"...\"...)\n" +`). Raw string: the backslashes are literal
# file bytes here, not Python escapes.
SCOPE_PANIC_GENERATED_LITERAL = r"""\
diff --git a/internal/frontend/synth_math_big.go b/internal/frontend/synth_math_big.go
index 00000000..11111111 100644
--- a/internal/frontend/synth_math_big.go
+++ b/internal/frontend/synth_math_big.go
@@ -1,3 +1,4 @@ package frontend
+"\tpanic(\"math/big: Foo not in M7 scope (multi-result calls need WS D)\")\n" +
"""

# Legitimate invariant panics: real failure modes, no stub phrase.
# The unanchored keyword match must not sweep these in.
LEGITIMATE_PANICS = """\
diff --git a/internal/frontend/synth_foo.go b/internal/frontend/synth_foo.go
index 00000000..11111111 100644
--- a/internal/frontend/synth_foo.go
+++ b/internal/frontend/synth_foo.go
@@ -1,3 +1,7 @@
 package frontend
+\t\tpanic("division by zero")
+\t\tpanic("d.nx != 0")
+\t\tpanic("output smaller than input")
+\t\tpanic("cannot create context from nil parent")
"""

# Negatives for the new alternatives: the phrase must match as a
# phrase, not as loose words.
NOT_IN_RANGE = """\
diff --git a/internal/frontend/synth_foo.go b/internal/frontend/synth_foo.go
index 00000000..11111111 100644
--- a/internal/frontend/synth_foo.go
+++ b/internal/frontend/synth_foo.go
@@ -1,3 +1,4 @@
 package frontend
+\tpanic("value not in range")
"""

DESUGAR_WORD_ALONE = """\
diff --git a/internal/frontend/synth_foo.go b/internal/frontend/synth_foo.go
index 00000000..11111111 100644
--- a/internal/frontend/synth_foo.go
+++ b/internal/frontend/synth_foo.go
@@ -1,3 +1,4 @@
 package frontend
+\tpanic("desugar failed: nil helper")
"""

OUT_OF_SCOPE_WORD = """\
diff --git a/internal/frontend/synth_foo.go b/internal/frontend/synth_foo.go
index 00000000..11111111 100644
--- a/internal/frontend/synth_foo.go
+++ b/internal/frontend/synth_foo.go
@@ -1,3 +1,4 @@
 package frontend
+\tpanic("name out of scope")
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

    def test_scope_panic_m8_flagged(self):
        errs = cnb.check_no_bridges(SCOPE_PANIC_M8)
        self.assertTrue(any("PANIC STUB" in e for e in errs), errs)

    def test_scope_panic_tar_flagged(self):
        errs = cnb.check_no_bridges(SCOPE_PANIC_TAR)
        self.assertTrue(any("PANIC STUB" in e for e in errs), errs)

    def test_prefixed_not_implemented_flagged(self):
        errs = cnb.check_no_bridges(PREFIXED_NOT_IMPLEMENTED)
        self.assertTrue(any("PANIC STUB" in e for e in errs), errs)

    def test_scope_panic_bare_flagged(self):
        errs = cnb.check_no_bridges(SCOPE_PANIC_BARE)
        self.assertTrue(any("PANIC STUB" in e for e in errs), errs)

    def test_desugar_panic_long_flagged(self):
        errs = cnb.check_no_bridges(DESUGAR_PANIC_LONG)
        self.assertTrue(any("PANIC STUB" in e for e in errs), errs)

    def test_desugar_panic_short_flagged(self):
        errs = cnb.check_no_bridges(DESUGAR_PANIC_SHORT)
        self.assertTrue(any("PANIC STUB" in e for e in errs), errs)

    def test_desugar_panic_callee_readall_flagged(self):
        errs = cnb.check_no_bridges(DESUGAR_PANIC_CALLEE_READALL)
        self.assertTrue(any("PANIC STUB" in e for e in errs), errs)

    def test_desugar_panic_callee_writestring_flagged(self):
        errs = cnb.check_no_bridges(DESUGAR_PANIC_CALLEE_WRITESTRING)
        self.assertTrue(any("PANIC STUB" in e for e in errs), errs)

    def test_scope_panic_composed_flagged(self):
        errs = cnb.check_no_bridges(SCOPE_PANIC_COMPOSED)
        self.assertTrue(any("PANIC STUB" in e for e in errs), errs)

    def test_scope_panic_generated_literal_flagged(self):
        errs = cnb.check_no_bridges(SCOPE_PANIC_GENERATED_LITERAL)
        self.assertTrue(any("PANIC STUB" in e for e in errs), errs)

    def test_legitimate_panics_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(LEGITIMATE_PANICS), [])

    def test_not_in_range_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(NOT_IN_RANGE), [])

    def test_desugar_word_alone_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(DESUGAR_WORD_ALONE), [])

    def test_out_of_scope_not_flagged(self):
        self.assertEqual(cnb.check_no_bridges(OUT_OF_SCOPE_WORD), [])


if __name__ == "__main__":
    unittest.main()
