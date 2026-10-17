#!/usr/bin/env python3
"""
Self-test for check_no_bridges.py (P1-2 gate hardening, 2026-10-17).

Positive AND negative cases for the no-bridges gate. Every positive case
feeds a synthetic diff through check_no_bridges() and asserts the gate
flags it; every negative case asserts the gate stays quiet.

The four bypass_* cases are the PROVEN bypasses from the 2026-10-17
independent review: each one passes the pre-hardening gate and must fail
it after. Run against the pre-hardening script to confirm every positive
test is non-tautological:
    git show HEAD:.github/scripts/check_no_bridges.py > /tmp/cnb_old.py
    (import it and re-run these cases — the bypass_* tests must fail)

Usage: python3 .github/scripts/check_no_bridges_test.py
"""

import importlib.util
import os
import sys
import unittest

_HERE = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location(
    "check_no_bridges", os.path.join(_HERE, "check_no_bridges.py"))
cnb = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(cnb)


def diff(fname, added, new_file=False):
    """Build a minimal unified diff adding `added` lines to `fname`."""
    lines = [f"diff --git a/{fname} b/{fname}"]
    lines.append("--- /dev/null" if new_file else f"--- a/{fname}")
    lines.append(f"+++ b/{fname}")
    lines.append("@@ -1,1 +1,2 @@")
    if not new_file:
        lines.append(" existing line")
    lines += ["+" + l for l in added]
    return "\n".join(lines) + "\n"


class TestPackageNameConditionals(unittest.TestCase):
    # --- the four proven bypasses (pre-hardening gate: NOT flagged) ---
    def test_bypass_full_import_path(self):
        d = diff("internal/frontend/lower.go", ['\tif pkg == "net/http" {'])
        self.assertTrue(any("PACKAGE-NAME CONDITIONAL" in e
                            for e in cnb.check_no_bridges(d)), d)

    def test_bypass_subpackage_case(self):
        d = diff("internal/backend/truffle/emit.go", ['\t\tcase "encoding/json":'])
        self.assertTrue(any("PACKAGE-NAME CONDITIONAL" in e
                            for e in cnb.check_no_bridges(d)), d)

    def test_bypass_new_stdlib_package(self):
        d = diff("internal/frontend/resolve.go", ['\tif name == "slices" {'])
        self.assertTrue(any("PACKAGE-NAME CONDITIONAL" in e
                            for e in cnb.check_no_bridges(d)), d)

    def test_bypass_wire_node_other_stdlib_root(self):
        d = diff("internal/frontend/export.go",
                 ['\te.node("tlsverify", tlsVerifyNode{})'])
        self.assertTrue(any("SUSPICIOUS WIRE NODE" in e
                            for e in cnb.check_no_bridges(d)), d)

    def test_bypass_wire_node_crypto_prefix(self):
        d = diff("internal/frontend/export.go",
                 ['\te.node("cryptox509", x509Node{})'])
        self.assertTrue(any("SUSPICIOUS WIRE NODE" in e
                            for e in cnb.check_no_bridges(d)), d)

    # --- pre-existing behavior must keep working ---
    def test_bare_name_still_flagged(self):
        d = diff("internal/frontend/lower.go", ['\tif pkg == "fmt" {'])
        self.assertTrue(any("PACKAGE-NAME CONDITIONAL" in e
                            for e in cnb.check_no_bridges(d)), d)

    def test_name_attr_still_flagged(self):
        d = diff("internal/frontend/lower.go", ['\tif imp.Name == "os" {'])
        self.assertTrue(any("PACKAGE-NAME CONDITIONAL" in e
                            for e in cnb.check_no_bridges(d)), d)

    def test_java_file_still_flagged(self):
        d = diff("internal/backend/truffle/GoNodes.java",
                 ['\t\tif (pkg == "sync") {'])
        self.assertTrue(any("PACKAGE-NAME CONDITIONAL" in e
                            for e in cnb.check_no_bridges(d)), d)

    # --- negatives: must stay clean ---
    def test_non_stdlib_name_clean(self):
        d = diff("internal/frontend/lower.go", ['\tif pkg == "myapp/widget" {'])
        self.assertEqual(cnb.check_no_bridges(d), [])

    def test_plain_import_clean(self):
        d = diff("internal/frontend/foo.go", ['\t_ "net/http"'])
        self.assertEqual(cnb.check_no_bridges(d), [])

    def test_test_file_allowance_kept(self):
        d = diff("internal/frontend/lower_test.go", ['\tif pkg == "net/http" {'])
        self.assertEqual(cnb.check_no_bridges(d), [])

    def test_test_prefix_file_allowance_kept(self):
        d = diff("internal/frontend/test_helper.go", ['\tif pkg == "slices" {'])
        self.assertEqual(cnb.check_no_bridges(d), [])


class TestWireNodes(unittest.TestCase):
    def test_adjudicated_node_stays_clean(self):
        d = diff("internal/frontend/export.go",
                 ['\te.node("runtimenumgoroutine", numGoroutineNode{})'])
        self.assertEqual(cnb.check_no_bridges(d), [])

    def test_adjudicated_node_gosched_stays_clean(self):
        d = diff("internal/frontend/export.go",
                 ['\te.node("runtimegosched", goschedNode{})'])
        self.assertEqual(cnb.check_no_bridges(d), [])

    def test_adjudicated_registration_stays_clean(self):
        d = diff("internal/frontend/export.go",
                 ['\taddFunc("NumGoroutine", numGoroutineFn)'])
        self.assertEqual(cnb.check_no_bridges(d), [])

    def test_non_stdlib_node_clean(self):
        d = diff("internal/frontend/export.go",
                 ['\te.node("frobnicate", frobNode{})'])
        self.assertEqual(cnb.check_no_bridges(d), [])

    def test_go_prefixed_generic_node_fails_loud(self):
        # Documented intentional collision: a generic AST name starting with
        # a stdlib base ("go", like the existing "gostmt") needs adjudication.
        d = diff("internal/frontend/export.go",
                 ['\te.node("gostmt2", goStmt2Node{})'])
        self.assertTrue(any("SUSPICIOUS WIRE NODE" in e
                            for e in cnb.check_no_bridges(d)), d)


class TestOtherChecks(unittest.TestCase):
    def test_new_synth_file_flagged(self):
        d = diff("internal/frontend/synth_evil.go",
                 ['package frontend'], new_file=True)
        self.assertTrue(any("NEW SYNTHETIC FILE" in e
                            for e in cnb.check_no_bridges(d)), d)

    def test_panic_stub_flagged(self):
        d = diff("internal/frontend/lower.go", ['\t\tpanic("not implemented")'])
        self.assertTrue(any("PANIC STUB" in e
                            for e in cnb.check_no_bridges(d)), d)

    def test_synthetic_registration_flagged(self):
        d = diff("internal/frontend/lower.go",
                 ['\taddFunc("EvilFunc", evilFn)'])
        self.assertTrue(any("SYNTHETIC REGISTRATION" in e
                            for e in cnb.check_no_bridges(d)), d)

    def test_clean_diff_passes(self):
        d = diff("internal/frontend/lower.go",
                 ['\t// normalize import path', '\tp = cleanPath(p)'])
        self.assertEqual(cnb.check_no_bridges(d), [])


if __name__ == "__main__":
    unittest.main(verbosity=2)
