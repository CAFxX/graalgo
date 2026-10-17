#!/usr/bin/env python3
"""Self-tests for check_no_bridges.py.

Every detection rule gets a POSITIVE control (a synthetic offending diff
that must fire — proving the rule is not dead) plus NEGATIVE controls
(legitimate/grandfathered shapes that must stay quiet — proving the rule
is not a hair-trigger). Run:

    python3 .github/scripts/test_check_no_bridges.py

CI runs it right after the no-bridges check itself.
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import check_no_bridges as g

PASS = []
FAIL = []


def fake_diff(entries):
    """Build a minimal unified diff from (path, added_line, is_new) entries."""
    chunks = []
    for path, added, is_new in entries:
        old = "/dev/null" if is_new else f"a/{path}"
        chunks.append(
            f"diff --git {old} b/{path}\n"
            f"--- {old}\n"
            f"+++ b/{path}\n"
            "@@ -1 +1 @@\n"
            f"+{added}\n"
        )
    return "\n".join(chunks)


def check(name, cond, detail=""):
    (PASS if cond else FAIL).append(name)
    print(("PASS " if cond else "FAIL ") + name + (f" — {detail}" if detail and not cond else ""))


def fires(diff, token):
    """The gate flags the diff AND names the offending token."""
    errs = g.check_no_bridges(diff)
    return any(token in e for e in errs), errs


def quiet(diff):
    errs = g.check_no_bridges(diff)
    return not errs, errs


# --- 1. push-base diff source (R2 P1-1 / D6) -------------------------------
os.environ["NO_BRIDGES_PUSH_BASE"] = "deadbeef" * 5
cands = g._diff_candidates()
check("push-base: used as diff source 1 when set",
      cands[0][1] == ["git", "diff", "deadbeef" * 5 + "...HEAD", "--", "."],
      str(cands[0][1]))
check("push-base: label names the range", "whole pushed range" in cands[0][0])
os.environ["NO_BRIDGES_PUSH_BASE"] = "0" * 40
check("push-base: all-zeros base skipped",
      g._diff_candidates()[0][0] == "origin/master...HEAD")
del os.environ["NO_BRIDGES_PUSH_BASE"]
check("push-base: unset falls back to PR diff",
      g._diff_candidates()[0][0] == "origin/master...HEAD")

# --- 4. wire nodes: inverted check (D3) ------------------------------------
d = fake_diff([("internal/frontend/synth_machinery.go",
                '\treturn e.node("timenew2", c, nil), nil', False)])
ok, _ = fires(d, "timenew2")
check("wire-node: new time-family kind fires", ok)

d = fake_diff([("internal/frontend/export.go",
                '\t\treturn e.node("runtimefoo", c, nil), nil', False)])
ok, _ = fires(d, "runtimefoo")
check("wire-node: new runtime-family kind fires", ok)

d = fake_diff([("internal/frontend/synth_machinery.go",
                '\te.node("timenow", c)', False)])
ok, _ = fires(d, "timenow")
check("wire-node: existing stdlib kind newly emitted fires", ok)

d = fake_diff([("internal/frontend/export.go",
                '\treturn e.node("block", c, nil), nil', False)])
ok, _ = quiet(d)
check("wire-node: generic kind stays quiet", ok)

# Veto 1 (2026-10-17): compositelit/pkgref/slice are live generic kinds
# from the frontend-v2 emitter (nodes.go:395,451,589) — new call sites
# must stay quiet.
for k in ["compositelit", "pkgref", "slice"]:
    d = fake_diff([("internal/frontend/v2/emit/nodes.go",
                    f'\treturn e.node("{k}", c, nil), nil', False)])
    ok, _ = quiet(d)
    check(f"wire-node: new {k} call site stays quiet", ok)

# Veto 2 (2026-10-17): the dynamic-kind idiom — kind :=/kind = string
# assignments and "Sym": "kindval" intrinsic-map entries in files that
# feed a variable kind to e.node — must fire for out-of-vocabulary kinds.
d = fake_diff([("internal/frontend/export.go",
                '\t\t\t\t\tkind := "timenew2"', False)])
ok, _ = fires(d, "timenew2")
check("wire-node: dynamic kind := assignment fires", ok)

d = fake_diff([("internal/frontend/export.go",
                '\t\t\t\t\tkind = "timenew2"', False)])
ok, _ = fires(d, "timenew2")
check("wire-node: dynamic kind = reassignment fires", ok)

d = fake_diff([("internal/frontend/synth_machinery.go",
                '\t\t"Frobnicate":        "mathfrob",', False)])
ok, _ = fires(d, "mathfrob")
check("wire-node: dynamic intrinsic-map entry fires", ok)

d = fake_diff([("internal/frontend/export.go",
                '\t\t\t\t\tkind := "block"', False)])
ok, _ = quiet(d)
check("wire-node: dynamic generic-kind assignment stays quiet", ok)

d = fake_diff([("internal/frontend/export.go",
                '\t\t\t\t\tkind := "runtimegosched"', False)])
ok, _ = quiet(d)
check("wire-node: dynamic adjudicated-kind assignment stays quiet", ok)

d = fake_diff([("internal/frontend/other.go",
                '\t\t"Frobnicate": "mathfrob",', False)])
ok, _ = quiet(d)
check("wire-node: map entry in file without dynamic consumer stays quiet", ok)

d = fake_diff([("internal/frontend/export.go",
                '\t\treturn e.node("runtimegosched", c, nil), nil', False)])
ok, _ = quiet(d)
check("wire-node: adjudicated kind stays quiet", ok)

d = fake_diff([("internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3KindRegistrations.java",
                '        factory.registerStmtKind("timefoo", TimeFooNode::build);', False)])
ok, _ = fires(d, "timefoo")
check("wire-node: new Java registerStmtKind fires", ok)

# Veto 3 (2026-10-17): registerExprKind (132 live call sites, the method
# used for stdlib-intrinsic expr kinds) must be covered too.
d = fake_diff([("internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3KindRegistrations.java",
                '        factory.registerExprKind("timefoo", TimeFooNode::build);', False)])
ok, _ = fires(d, "timefoo")
check("wire-node: new Java registerExprKind fires", ok)

d = fake_diff([("internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3KindRegistrations.java",
                '        factory.registerExprKind("select", SelectNode::build);', False)])
ok, _ = quiet(d)
check("wire-node: generic Java registerExprKind stays quiet", ok)

d = fake_diff([("internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3SyncMain.java",
                '            factory.registerStmtKind("runtimegosched", GoschedNode::build);', False)])
ok, _ = quiet(d)
check("wire-node: adjudicated Java registration stays quiet", ok)

d = fake_diff([("internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3KindRegistrations.java",
                '        factory.registerStmtKind("select", SelectNode::build);', False)])
ok, _ = quiet(d)
check("wire-node: generic Java kind stays quiet", ok)

check("wire-node: generic vocabulary sane",
      "block" in g.GENERIC_WIRE_KINDS
      and {"compositelit", "pkgref", "slice"} <= set(g.GENERIC_WIRE_KINDS)
      and len(g.GENERIC_WIRE_KINDS) == 52
      and not {"timenow", "unsafeadd", "runtimecallers", "reflectop",
               "abifuncpc", "syncop", "atomicaddr"} & set(g.GENERIC_WIRE_KINDS))

# --- 6. syntheticSources overlay (D2) ---------------------------------------
d = fake_diff([("internal/frontend/synth_machinery.go",
                '\t"newpkg": syntheticNewpkgSource,', False)])
ok, _ = fires(d, "syntheticSources entry")
check("synth-overlay: new map entry fires", ok)

d = fake_diff([("internal/frontend/synth_machinery.go",
                "const syntheticNewpkgSource = `package newpkg", False)])
ok, _ = fires(d, "source const")
check("synth-overlay: new source const fires", ok)

# The const fires only when the file is NOT a generator-pipeline output:
# synth_archive_tar.go carries the "Generated by gen_synth_" marker.
d = fake_diff([("internal/frontend/synth_archive_tar.go",
                "const syntheticWibbleSource = `package wibble", False)])
ok, _ = quiet(d)
check("synth-overlay: const in generated file stays quiet", ok)
check("synth-overlay: marker detection is real",
      g._is_gen_synth_output("internal/frontend/synth_archive_tar.go")
      and not g._is_gen_synth_output("internal/frontend/synth_machinery.go"))

# Veto 5 (2026-10-17): the marker scan covers the WHOLE file — 5 of the 18
# pipeline outputs carry the marker past line 26 (synth_mathbits.go:39).
# A const added on the sanctioned regeneration path must stay quiet.
d = fake_diff([("internal/frontend/synth_mathbits.go",
                "const syntheticWibbleSource = `package wibble", False)])
ok, _ = quiet(d)
check("synth-overlay: const in generated file (marker at line 39) stays quiet", ok)
check("synth-overlay: beyond-25-lines marker detected (non-hermetic)",
      g._is_gen_synth_output("internal/frontend/synth_mathbits.go"))

# Hermetic control: marker past the old 25-line window in a temp file.
import tempfile
with tempfile.NamedTemporaryFile("w", suffix=".go", delete=False) as tf:
    tf.write("// line\n" * 30 + "// Generated by gen_synth_test.py\n")
    tmp_marker_path = tf.name
check("synth-overlay: marker at line 31 detected (hermetic)",
      g._file_has_gen_synth_marker(tmp_marker_path))
with tempfile.NamedTemporaryFile("w", suffix=".go", delete=False) as tf:
    tf.write("// line\n" * 40)
    tmp_nomarker_path = tf.name
check("synth-overlay: markerless file not detected (hermetic)",
      not g._file_has_gen_synth_marker(tmp_nomarker_path))
os.unlink(tmp_marker_path)
os.unlink(tmp_nomarker_path)

# --- 3. fail-fast stubs: Java + Python (D4) ---------------------------------
d = fake_diff([("internal/backend/truffle/src/main/java/graalgo/frontend/FooNode.java",
                '        throw new UnsupportedOperationException("not implemented");', False)])
ok, _ = fires(d, "FAIL-FAST STUB")
check("java-stub: new UOE throw fires", ok)

d = fake_diff([("internal/backend/truffle/src/main/java/graalgo/frontend/FooNode.java",
                '        throw new IllegalStateException("TODO: implement Foo");', False)])
ok, _ = fires(d, "FAIL-FAST STUB")
check("java-stub: TODO in throw site fires", ok)

# Veto 4 (2026-10-17): the throw-allowlist is anchored on the Phase-1
# truffle module path — the Phase-2 runtime/... real implementations with
# the same class names are NOT exempt. Driven by the gate's own
# FROZEN_JAVA_STUB_FILES so the two cannot drift.
for frozen_suffix in g.FROZEN_JAVA_STUB_FILES:
    assert frozen_suffix.startswith("truffle/src/main/java/"), frozen_suffix
    d = fake_diff([(f"internal/backend/{frozen_suffix}",
                    '        throw new UnsupportedOperationException("Phase 2");', False)])
    ok, _ = quiet(d)
    check(f"java-stub: frozen contract {frozen_suffix.split('/')[-1]} stays quiet", ok)

d = fake_diff([("internal/backend/truffle/src/main/java/graalgo/runtime/Timers.java",
                '        throw new UnsupportedOperationException("Phase 2");', False)])
ok, _ = fires(d, "FAIL-FAST STUB")
check("java-stub: non-frozen file still fires", ok)

# Phase-2 real implementation with the same class name: a new throw is a
# genuine bodge and must fire (veto 4 — the old bare-suffix exemption
# covered it).
d = fake_diff([("internal/backend/runtime/src/main/java/graalgo/runtime/GcInterop.java",
                '        throw new UnsupportedOperationException("Phase 2");', False)])
ok, _ = fires(d, "FAIL-FAST STUB")
check("java-stub: Phase-2 runtime GcInterop throw still fires", ok)

# Safepoint.java was dropped from the allowlist (veto 4/item 6): neither
# Safepoint file throws, so a new throw in either module must fire.
for safepoint_path in ["internal/backend/truffle/src/main/java/graalgo/safepoint/Safepoint.java",
                       "internal/backend/runtime/src/main/java/graalgo/safepoint/Safepoint.java"]:
    d = fake_diff([(safepoint_path,
                    '        throw new UnsupportedOperationException("x");', False)])
    ok, _ = fires(d, "FAIL-FAST STUB")
    check(f"java-stub: Safepoint throw fires ({safepoint_path.split('/')[2]})", ok)

# Built by concatenation so the gate's own textual scan does not flag this
# test file when the change is pushed (the gate scans added lines literally).
d = fake_diff([("tools/foo.py", '    raise Not' + 'ImplementedError("todo")', False)])
ok, _ = fires(d, "FAIL-FAST STUB")
check("py-stub: NotImplementedError raising fires", ok)

d = fake_diff([("internal/frontend/export.go", '\tpanic("not implemented")', False)])
ok, _ = fires(d, "FAIL-FAST STUB")
check("go-stub: panic stub still fires", ok)

# --- 2. package-name conditionals: mechanical list + shapes (D5/D7) ---------
pkgs = g._stdlib_package_names()
check("pkg-list: previously-missed packages covered",
      {"mime", "mime/multipart", "iter", "hash", "path", "regexp", "unicode",
       "html", "log", "flag", "maps", "slices", "cmp", "testing", "image",
       "archive/tar", "compress/gzip", "text/template", "database/sql",
       "os/exec", "internal/bytealg"} <= pkgs)
check("pkg-list: non-GOROOT overlay keys covered",
      "internal/graalgo/fmtstate" in pkgs and "golang.org/x/crypto/cryptobyte" in pkgs)

d = fake_diff([("internal/frontend/synth_machinery.go",
                '\tif p == "mime" || p == "mime/multipart" {', False)])
ok, _ = fires(d, "PACKAGE-NAME CONDITIONAL")
check("pkg-cond: p == \"mime\" fires", ok)

d = fake_diff([("internal/frontend/synth_machinery.go",
                '\tif fn.Pkg().Path() == "iter" {', False)])
ok, _ = fires(d, "PACKAGE-NAME CONDITIONAL")
check("pkg-cond: .Path() == \"iter\" fires", ok)

d = fake_diff([("internal/frontend/export.go",
                '\tif strings.HasPrefix(path, "net") {', False)])
ok, _ = fires(d, "PACKAGE-NAME CONDITIONAL")
check("pkg-cond: HasPrefix(path, \"net\") fires", ok)

d = fake_diff([("internal/frontend/export.go",
                '\tif strings.Contains(path, "internal/bytealg") {', False)])
ok, _ = fires(d, "PACKAGE-NAME CONDITIONAL")
check("pkg-cond: Contains fires", ok)

d = fake_diff([("internal/frontend/export.go",
                '\tif overrides["time"] {', False)])
ok, _ = fires(d, "PACKAGE-NAME CONDITIONAL")
check("pkg-cond: map index [\"time\"] fires", ok)

d = fake_diff([("internal/frontend/export.go", '\tcase "regexp":', False)])
ok, _ = fires(d, "PACKAGE-NAME CONDITIONAL")
check("pkg-cond: case \"regexp\" fires", ok)

d = fake_diff([("internal/backend/truffle/src/main/java/graalgo/frontend/Foo.java",
                '        if (pkg.startsWith("sync")) {', False)])
ok, _ = fires(d, "PACKAGE-NAME CONDITIONAL")
check("pkg-cond: Java startsWith fires", ok)

d = fake_diff([("internal/frontend/export_internal_visibility_test.go",
                '\tif path == "runtime" || path == "fmt" {', False)])
ok, _ = quiet(d)
check("pkg-cond: *_test.go stays excluded", ok)

d = fake_diff([("internal/backend/truffle/src/test/java/graalgo/frontend/FooTest.java",
                '        if (path == "bytes") {', False)])
ok, _ = quiet(d)
check("pkg-cond: src/test tree stays excluded", ok)

d = fake_diff([("internal/frontend/contest_helper.go",
                '\tif x == "mime" {', False)])
ok, _ = fires(d, "PACKAGE-NAME CONDITIONAL")
check("pkg-cond: test_ substring no longer excludes (D7)", ok)

# --- preserved behavior: checks 1 and 5 -------------------------------------
d = fake_diff([("internal/frontend/synth_newpkg.go",
                "const syntheticNewpkgSource = `package newpkg", True)])
ok, _ = fires(d, "NEW SYNTHETIC FILE")
check("check-1: new synth_*.go file still fires", ok)

d = fake_diff([("internal/frontend/export.go",
                '\taddFunc("Wibble", nil)', False)])
ok, _ = fires(d, "SYNTHETIC REGISTRATION")
check("check-5: unadjudicated addFunc still fires", ok)

d = fake_diff([("internal/frontend/export.go",
                '\taddFunc("Gosched", nil)', False)])
ok, _ = quiet(d)
check("check-5: adjudicated registration stays quiet", ok)

print()
print(f"{len(PASS)} passed, {len(FAIL)} failed")
if FAIL:
    print("FAILURES:")
    for name in FAIL:
        print(f"  ✗ {name}")
    sys.exit(1)
print("gate self-tests green.")
