#!/usr/bin/env python3
"""Regression tests for check_bridge_inventory.py.

Run: python3 .github/scripts/test_check_bridge_inventory.py

The 2026-11-10 failure (REVIEW-2026-11-10-07 P1-4): B-4 claimed "142
generated synth_*.go" while disk held 140, and its own "~24 authored,
~110 verbatim vendored" summed to ~134 — the inventory had drifted
with no mechanical check to catch it. The same review round found the
same drift class in B-3 (syntheticSources "133 entries" vs 130 on
disk, every cited line number stale) and B-6 (unsafe.Add "always
traps" although UnsafeAddNode implements it since fix-redmaster-r39).

Fixtures below build minimal fake repo trees in a temp dir and assert
the checker passes on correct numbers and fails on each wrong one.
Positive controls are wrong pins that MUST still fail: the checker is
a tripwire, not a rubber stamp.
"""

import os
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import check_bridge_inventory as cbi

# Disk layout of the standard fixture tree.
GEN = ["synth_a.go", "synth_b.go"]
TEST = ["synth_t_test.go"]
HAND = ["synth_h.go"]
P3 = ["synth_x.go", "synth_y.go"]
SOURCES = ["time", "context", "errors"]


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        f.write(text)


def make_tree(root, pins=None, gen_makefile=None, gen_runner=None,
              sources=None, drop_symbol=None):
    """Build a minimal fake repo tree. `pins` overrides the pin values
    written into the inventory (default: the true disk numbers)."""
    gen_makefile = GEN if gen_makefile is None else gen_makefile
    gen_runner = GEN if gen_runner is None else gen_runner
    sources = SOURCES if sources is None else sources
    # synth_machinery.go and synth_reflect.go are themselves synth_*.go
    # files (hand-maintained, like on the real tree) — the totals count
    # them, so the pins must too.
    total = len(GEN) + len(TEST) + len(HAND) + 2
    default_pins = {
        "synth_frontend_total": total,
        "synth_frontend_generated": len(GEN),
        "synth_frontend_test": len(TEST),
        "synth_frontend_hand": len(HAND) + 2,
        "synth_p3frontend": len(P3),
        "synthetic_sources_entries": len(sources),
    }
    if pins:
        default_pins.update(pins)
    b4 = "\n".join(f"{k} = {v}" for k, v in default_pins.items()
                   if k != "synthetic_sources_entries")
    b3 = ("synthetic_sources_entries = "
          f"{default_pins['synthetic_sources_entries']}")
    write(os.path.join(root, "docs/arch/bridge-inventory.md"),
          "### B-3\n```inventory-pins\n" + b3 + "\n```\n"
          "### B-4\n```inventory-pins\n" + b4 + "\n```\n")
    mk = "GENERATED_SYNTH := \\\n" + "".join(
        f"\tinternal/frontend/{g} \\\n" for g in gen_makefile)
    write(os.path.join(root, "Makefile"), mk + "\n")
    run = ("GENERATORS = [\n" + "".join(
        f'    ("gen_{g}.py", ["{g}"]),\n' for g in gen_runner) + "]\n")
    write(os.path.join(root, "internal/frontend/run_gen_synth.py"), run)
    for name in GEN + TEST + HAND:
        write(os.path.join(root, "internal/frontend", name),
              "package main\n")
    for name in P3:
        write(os.path.join(root, "internal/frontend/p3frontend", name),
              "package p3frontend\n")
    entries = "\n".join(f'\t"{s}": synthetic{s.title()}Source,'
                        for s in sources)
    machinery = (
        "package main\n\n"
        "var syntheticSources = map[string]string{\n" + entries + "\n}\n\n"
        "func isMultiResultSynthetic(fn *types.Func) bool { return false }\n"
        "func multiResultSyntheticError(fn *types.Func) error { return nil }\n"
        "func isTimeIntrinsicFunc(fn *types.Func) bool { return false }\n"
        "func isMathIntrinsicFunc(fn *types.Func) bool { return false }\n"
        "func isAliasIntrinsicFunc(fn *types.Func) bool { return false }\n"
        "func isAbiFuncPCIntrinsicFunc(fn *types.Func) bool { return false }\n")
    write(os.path.join(root, "internal/frontend/synth_machinery.go"),
          machinery)
    write(os.path.join(root, "internal/frontend/synth_reflect.go"),
          "package main\n\n"
          "func isReflectIntrinsicFunc(fn *types.Func) bool { return false }\n")
    write(os.path.join(root, "internal/frontend/diskimport.go"),
          "package main\n\ntype overlayImporter struct{}\n"
          "func resolvePkgTree(path string) (*syntheticPackage, error) {\n"
          "\treturn nil, nil\n}\n")
    write(os.path.join(root, "internal/frontend/export.go"),
          'package main\n\n// e.node("syncop", c, nil)\n')
    write(os.path.join(root, "internal/frontend/mrdesugar_m5.go"),
          'package main\n\n// kind = "runtimecaller"\n')
    write(os.path.join(root, "internal/backend/truffle/src/main/java/"
                       "graalgo/frontend/NodeFactory.java"),
          "class NodeFactory {\n"
          "    private static String syncPrimitiveName() { return null; }\n}\n")
    write(os.path.join(root, "internal/backend/interop/src/main/java/"
                       "graalgo/p3x509/GoX509.java"),
          "class GoX509 {\n"
          "    scalarMultiply(); ecAdd(); ecDouble();\n}\n")
    for name in cbi.RTSTACK_FILES:
        write(os.path.join(root, "internal/backend/interop/src/main/java/"
                           "graalgo/rtstack", name),
              "class X {}\n")
    if drop_symbol:
        rel, needle = drop_symbol
        path = os.path.join(root, rel)
        with open(path, encoding="utf-8") as f:
            text = f.read()
        with open(path, "w", encoding="utf-8") as f:
            f.write(text.replace(needle, "RENAMED"))


class TestCheckBridgeInventory(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="check-bridge-inventory-")

    def tearDown(self):
        shutil.rmtree(self.tmp, ignore_errors=True)

    def errors(self, **kw):
        make_tree(self.tmp, **kw)
        errors, _, _ = cbi.check_inventory(self.tmp)
        return errors

    def test_correct_tree_passes(self):
        self.assertEqual(self.errors(), [])

    def test_wrong_total_pin_fails(self):
        errs = self.errors(pins={"synth_frontend_total": 999})
        self.assertTrue(any("synth_frontend_total" in e and "999" in e
                            for e in errs), errs)

    def test_wrong_generated_pin_fails(self):
        errs = self.errors(pins={"synth_frontend_generated": 1})
        self.assertTrue(any("synth_frontend_generated" in e for e in errs),
                        errs)

    def test_wrong_test_pin_fails(self):
        errs = self.errors(pins={"synth_frontend_test": 7})
        self.assertTrue(any("synth_frontend_test" in e for e in errs), errs)

    def test_wrong_hand_pin_fails(self):
        errs = self.errors(pins={"synth_frontend_hand": 42})
        self.assertTrue(any("synth_frontend_hand" in e for e in errs), errs)

    def test_wrong_p3_pin_fails(self):
        errs = self.errors(pins={"synth_p3frontend": 12})
        self.assertTrue(any("synth_p3frontend" in e for e in errs), errs)

    def test_wrong_synthetic_sources_pin_fails(self):
        errs = self.errors(pins={"synthetic_sources_entries": 133})
        self.assertTrue(any("synthetic_sources_entries" in e for e in errs),
                        errs)

    def test_makefile_runner_disagreement_fails(self):
        errs = self.errors(gen_makefile=["synth_a.go", "synth_b.go",
                                         "synth_extra.go"],
                           gen_runner=["synth_a.go", "synth_b.go"])
        self.assertTrue(any("GENERATED_SYNTH != run_gen_synth.py" in e
                            for e in errs), errs)

    def test_generated_file_missing_on_disk_fails(self):
        # Makefile + runner agree on 3 generated files, but only 2
        # exist on disk: the generated pin (3) then also disagrees
        # with the on-disk generated set.
        errs = self.errors(gen_makefile=["synth_a.go", "synth_b.go",
                                         "synth_missing.go"],
                           gen_runner=["synth_a.go", "synth_b.go",
                                       "synth_missing.go"])
        self.assertTrue(any("missing on disk" in e for e in errs), errs)

    def test_missing_pins_block_fails_vacuously_never(self):
        make_tree(self.tmp)
        inv = os.path.join(self.tmp, "docs/arch/bridge-inventory.md")
        with open(inv, "w", encoding="utf-8") as f:
            f.write("### B-4. no pins here\n")
        errors, _, _ = cbi.check_inventory(self.tmp)
        self.assertTrue(any("no ```inventory-pins block" in e for e in errors),
                        errors)

    def test_missing_symbol_fails(self):
        errs = self.errors(
            drop_symbol=("internal/frontend/synth_machinery.go",
                         "func isMultiResultSynthetic("))
        self.assertTrue(any("isMultiResultSynthetic" in e for e in errs),
                        errs)

    def test_missing_rtstack_file_fails(self):
        make_tree(self.tmp)
        os.remove(os.path.join(
            self.tmp, "internal/backend/interop/src/main/java/graalgo/"
            "rtstack/RtStack.java"))
        errors, _, _ = cbi.check_inventory(self.tmp)
        self.assertTrue(any("RtStack.java" in e for e in errors), errors)

    def test_stray_fence_text_in_prose_is_not_a_pins_block(self):
        # The 2026-11-10 intro prose mentioned the fence marker inline
        # ("live in ```inventory-pins blocks ..."): the unanchored
        # parser swallowed the whole file up to the next closing fence
        # as one giant block and reported every prose line unparsable.
        # Only a fence at line start opens a pins block.
        make_tree(self.tmp)
        inv = os.path.join(self.tmp, "docs/arch/bridge-inventory.md")
        with open(inv, encoding="utf-8") as f:
            text = f.read()
        with open(inv, "w", encoding="utf-8") as f:
            f.write("The numbers live in ```inventory-pins blocks, "
                    "see below.\n" + text)
        errors, _, _ = cbi.check_inventory(self.tmp)
        self.assertEqual(errors, [])


if __name__ == "__main__":
    unittest.main()
