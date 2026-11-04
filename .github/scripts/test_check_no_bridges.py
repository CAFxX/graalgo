#!/usr/bin/env python3
"""Positive/negative controls for check_no_bridges.py.

No test harness existed for this script (it is run directly by CI:
``python3 .github/scripts/check_no_bridges.py``), so this module is a
standalone unittest suite. Run it from anywhere with:

    python3 .github/scripts/test_check_no_bridges.py

or ``python3 -m unittest`` from the scripts directory. It imports the
script by path (no package) and calls check_no_bridges() on synthetic
diffs, so it never touches the working tree or the network.

The panic-stub cases pin REVIEW-2026-11-04-03 NEW-P1-2: a stub keyword
mid-string, and a stub built via AST (the internal/frontend/mr_disk.go
shape) must both FAIL the gate, while the adjudicated entries in
ADJUDICATED_WIRE_NODES / ADJUDICATED_REGISTRATIONS still pass and
legitimate invariant panics still pass. The gate inspects ADDED diff
lines only, so a stub that appears merely as a context line (grandfathered
code) must not be flagged.
"""

import importlib.util
import os
import sys
import unittest

_HERE = os.path.dirname(os.path.abspath(__file__))
_SPEC = importlib.util.spec_from_file_location(
    "check_no_bridges", os.path.join(_HERE, "check_no_bridges.py"))
check_no_bridges_mod = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(check_no_bridges_mod)
check_no_bridges = check_no_bridges_mod.check_no_bridges


def diff_for(fname, added_lines):
    """Build a minimal unified diff with the given lines as additions."""
    lines = ["--- a/" + fname, "+++ b/" + fname]
    lines += ["+" + line for line in added_lines]
    return "\n".join(lines) + "\n"


def panic_errors(errors):
    return [e for e in errors if "PANIC STUB" in e]


class TestPanicStubs(unittest.TestCase):
    def test_start_anchored_stub_still_flagged(self):
        errors = check_no_bridges(
            diff_for("internal/frontend/x.go", ['\tpanic("not implemented")']))
        self.assertTrue(panic_errors(errors), errors)

    def test_mid_string_literal_stub_flagged(self):
        # NEW-P1-2 evasion (a): the keyword is mid-string, not leading.
        errors = check_no_bridges(diff_for(
            "internal/frontend/x.go",
            ['\tpanic("helper failed: not implemented")']))
        self.assertTrue(panic_errors(errors), errors)

    def test_mid_string_composed_stubs_flagged(self):
        for line in (
            '\tpanic(fmt.Sprintf("helper %s failed: not implemented", name))',
            '\tpanic(fmt.Errorf("worker failed: unreachable state"))',
            '\tpanic(errors.New("backend: todo"))',
            '\tpanic(fmt.Sprintf("not done: tbd"))',
        ):
            with self.subTest(line=line):
                errors = check_no_bridges(
                    diff_for("internal/frontend/x.go", [line]))
                self.assertTrue(panic_errors(errors), errors)

    def test_raw_string_literal_stub_flagged(self):
        errors = check_no_bridges(diff_for(
            "internal/frontend/x.go",
            ['\tpanic(`helper failed: not implemented`)']))
        self.assertTrue(panic_errors(errors), errors)

    def test_ast_constructed_stub_flagged(self):
        # NEW-P1-2 evasion (b): the internal/frontend/mr_disk.go:891 shape.
        # No added line contains a panic(" literal: the panic is built via
        # AST on one line, the stub-keyword fragment on another.
        errors = check_no_bridges(diff_for("internal/frontend/mr_disk.go", [
            '\t\t\t\t\t\tFun: ast.NewIdent("panic"),',
            '\t\t\t\t\t\t\t\tValue: `"__` + helper + '
            '` not implemented: no Go body (backend intrinsic)"`},',
        ]))
        self.assertTrue(
            [e for e in errors if "AST-CONSTRUCTED PANIC STUB" in e], errors)

    def test_ast_panic_without_keyword_fragment_passes(self):
        # An AST-built panic whose message carries no stub keyword is a
        # legitimate invariant panic, not a stub — section 3b must not fire.
        errors = check_no_bridges(diff_for("internal/frontend/x.go", [
            '\t\t\t\t\t\tFun: ast.NewIdent("panic"),',
            '\t\t\t\t\t\t\t\tValue: `"__` + helper + ` failed"`,',
        ]))
        self.assertEqual([], errors)

    def test_keyword_fragment_without_ast_panic_passes(self):
        # A stub-keyword string on its own (e.g. test data, a diagnostic)
        # is not an AST-constructed panic stub.
        errors = check_no_bridges(diff_for("internal/frontend/x.go", [
            '\tmsg := "status: not implemented yet"',
        ]))
        self.assertEqual([], errors)

    def test_legitimate_invariant_panics_pass(self):
        for line in (
            '\tpanic("index out of range")',
            '\tpanic("must not happen: inconsistent scheduler state")',
            '\tpanic(fmt.Sprintf("bad state %d", state))',
        ):
            with self.subTest(line=line):
                errors = check_no_bridges(
                    diff_for("internal/frontend/x.go", [line]))
                self.assertEqual([], errors)

    def test_context_lines_not_scanned(self):
        # Grandfathering semantics: only ADDED lines are inspected. A stub
        # that appears as a context line of the diff is pre-existing code.
        diff = ("--- a/internal/frontend/x.go\n"
                "+++ b/internal/frontend/x.go\n"
                "@@ -1,1 +1,1 @@\n"
                ' panic("helper failed: not implemented")\n')
        self.assertEqual([], check_no_bridges(diff))


class TestAdjudicatedEntriesStillPass(unittest.TestCase):
    def test_adjudicated_wire_nodes_pass(self):
        for kind in ("runtimenumgoroutine", "runtimegosched"):
            with self.subTest(kind=kind):
                errors = check_no_bridges(diff_for(
                    "internal/frontend/export.go",
                    [f'\tx := e.node("{kind}")']))
                self.assertEqual([], errors)

    def test_non_adjudicated_wire_node_still_flagged(self):
        errors = check_no_bridges(diff_for(
            "internal/frontend/export.go", ['\tx := e.node("runtimefoo")']))
        self.assertTrue(
            [e for e in errors if "SUSPICIOUS WIRE NODE" in e], errors)

    def test_adjudicated_registrations_pass(self):
        for sym in ("NumGoroutine", "Gosched"):
            with self.subTest(sym=sym):
                errors = check_no_bridges(diff_for(
                    "internal/frontend/export.go",
                    [f'\taddFunc("{sym}", func() {{}})']))
                self.assertEqual([], errors)

    def test_non_adjudicated_registration_still_flagged(self):
        errors = check_no_bridges(diff_for(
            "internal/frontend/export.go",
            ['\taddFunc("SomethingElse", func() {})']))
        self.assertTrue(
            [e for e in errors if "SYNTHETIC REGISTRATION" in e], errors)


if __name__ == "__main__":
    unittest.main()
