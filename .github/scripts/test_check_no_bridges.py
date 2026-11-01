#!/usr/bin/env python3
"""Positive controls for check_no_bridges.py (run: python3 this file).

Each bypass the 2026-11-01 independent review found in the no-bridges gate
gets at least one fixture: a small diff that MUST fail the gate. Negatives
(modifications, retirements, commented lines, legitimate throws) MUST pass,
and old-behavior regressions (new synth files, addFunc, Go panic stubs,
Go == conditionals) MUST still fail. If any fixture flips, the gate has
regressed — fix the gate, never delete the fixture.

The fixtures are synthetic diffs fed straight to check_no_bridges(); the
no-vacuous-pass rule is tested separately by monkeypatching get_diff() to
return empty and asserting main() still fails.
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import check_no_bridges as gate

SYNTH_MAP = "internal/frontend/synth_machinery.go"
EXPORT_GO = "internal/frontend/export.go"
CHANOPS = "internal/frontend/chanops_m5.go"
V2_NODES = "internal/frontend/v2/emit/nodes.go"
P3REG = "internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3KindRegistrations.java"
REFLINK = "internal/backend/interop/src/main/java/graalgo/p3reflect/ReflectLinknames.java"
P3IMP = "internal/frontend/p3frontend/importer.go"
P3LOAD = "internal/frontend/p3frontend/loader.go"


def diff_for(fname, lines):
    """Build a minimal unified diff for one file.

    lines: list of (prefix, text) with prefix in " +-" (context/added/removed).
    Hunk headers are not validated by the gate, so a single @@ line suffices.
    """
    out = ["diff --git a/%s b/%s" % (fname, fname),
           "--- a/%s" % fname,
           "+++ b/%s" % fname,
           "@@ -1 +1 @@"]
    for prefix, text in lines:
        out.append(prefix + text)
    return "\n".join(out) + "\n"


def new_file_diff(fname, lines):
    """Diff for a genuinely new file (--- /dev/null)."""
    out = ["diff --git a/%s b/%s" % (fname, fname),
           "new file mode 100644",
           "--- /dev/null",
           "+++ b/%s" % fname,
           "@@ -0,0 +1 @@"]
    for text in lines:
        out.append("+" + text)
    return "\n".join(out) + "\n"


# Each case: (name, diff, expected-error-substring or None for must-pass).
CASES = [
    # Bypass (a): new synthetic package via the syntheticSources map.
    ("synth-map-add",
     diff_for(SYNTH_MAP, [
         (" ", "\t\"io\": syntheticIoSource,"),
         ("+", "\t\"mynewpkg\": syntheticMynewpkgSource,"),
     ]), "NEW SYNTHETIC PACKAGE (syntheticSources)"),
    ("synth-map-modify-same-key-passes",
     diff_for(SYNTH_MAP, [
         ("-", "\t\"io\": syntheticIoSource,"),
         ("+", "\t\"io\": syntheticIoSourceV2,"),
     ]), None),
    ("synth-map-remove-passes",
     diff_for(SYNTH_MAP, [
         ("-", "\t\"io\": syntheticIoSource,"),
     ]), None),
    ("synth-map-commented-add-passes",
     diff_for(SYNTH_MAP, [
         ("+", "\t// \"mynewpkg\": syntheticMynewpkgSource, // RETIRED"),
     ]), None),
    ("synth-map-index-assignment",
     diff_for(EXPORT_GO, [
         ("+", "\tsyntheticSources[\"mynewpkg\"] = mynewpkgSource"),
     ]), "NEW SYNTHETIC PACKAGE (syntheticSources assignment)"),
    # Bypass (b): Java name-keyed conditionals.
    ("java-equals",
     diff_for(REFLINK, [
         ("+", "\t\tboolean full = pkg.equals(\"reflect\");"),
     ]), "PACKAGE-NAME CONDITIONAL"),
    ("java-getname-equals",
     diff_for(REFLINK, [
         ("+", "\t\tif (obj.getName().equals(\"runtime\")) {"),
     ]), "PACKAGE-NAME CONDITIONAL"),
    ("java-yoda-equals",
     diff_for(REFLINK, [
         ("+", "\t\tif (\"reflect\".equals(pkg)) {"),
     ]), "PACKAGE-NAME CONDITIONAL"),
    ("java-objects-equals",
     diff_for(REFLINK, [
         ("+", "\t\tif (Objects.equals(pkg, \"sync\")) {"),
     ]), "PACKAGE-NAME CONDITIONAL"),
    ("java-equals-nonstdlib-passes",
     diff_for(REFLINK, [
         ("+", "\t\tboolean full = pkg.equals(\"myownlib\");"),
     ]), None),
    # Bypass (c): != forms.
    ("go-neq-pkg-name",
     diff_for(EXPORT_GO, [
         ("+", "\tif obj.Pkg() == nil || obj.Pkg().Name() != \"runtime\" {"),
     ]), "PACKAGE-NAME CONDITIONAL"),
    ("go-neq-ident",
     diff_for(EXPORT_GO, [
         ("+", "\tif !ok || pkgIdent.Name != \"runtime\" {"),
     ]), "PACKAGE-NAME CONDITIONAL"),
    ("go-neq-nonstdlib-passes",
     diff_for(EXPORT_GO, [
         ("+", "\tif x != \"foo\" {"),
     ]), None),
    # Bypass (d): wire node kinds — all prefixes, all emitters, Java factory.
    ("wire-export-reflect-kind",
     diff_for(EXPORT_GO, [
         ("+", "\tcall := e.node(\"reflectnewkind\", c, map[string]any{}),"),
     ]), "NEW WIRE NODE KIND"),
    ("wire-other-file-strings-kind",
     diff_for(CHANOPS, [
         ("+", "\tcall := e.node(\"stringsnewkind\", c, map[string]any{}),"),
     ]), "NEW WIRE NODE KIND"),
    ("wire-v2-crypto-kind",
     diff_for(V2_NODES, [
         ("+", "\treturn e.node(\"cryptonewkind\", fd, fields), nil"),
     ]), "NEW WIRE NODE KIND"),
    ("wire-java-expr-kind",
     diff_for(P3REG, [
         ("+", "\t\tfactory.registerExprKind(\"cryptonewkind\", CryptoNode::build);"),
     ]), "NEW WIRE NODE KIND (Java factory)"),
    ("wire-java-stmt-kind",
     diff_for(P3REG, [
         ("+", "\t\tfactory.registerStmtKind(\"timenewkind\", TimeNode::build);"),
     ]), "NEW WIRE NODE KIND (Java factory)"),
    # Adjudication stays narrow: the two adjudicated kinds pass only where
    # adjudicated (Go: exact file; Java: exact kind).
    ("wire-adjudicated-go-passes",
     diff_for(EXPORT_GO, [
         ("+", "\tcall := e.node(\"runtimenumgoroutine\", c, map[string]any{}),"),
     ]), None),
    ("wire-adjudicated-go-wrong-file-fails",
     diff_for(CHANOPS, [
         ("+", "\tcall := e.node(\"runtimenumgoroutine\", c, map[string]any{}),"),
     ]), "NEW WIRE NODE KIND"),
    ("wire-adjudicated-java-passes",
     diff_for(P3REG, [
         ("+", "\t\tfactory.registerExprKind(\"runtimenumgoroutine\", NumGoroutineNode::build);"),
         ("+", "\t\tfactory.registerStmtKind(\"runtimegosched\", GoschedNode::build);"),
     ]), None),
    # Bypass (e): Java stub throws.
    ("java-stub-uoe-message",
     diff_for(REFLINK, [
         ("+", "\t\tthrow new UnsupportedOperationException(\"foo not implemented\");"),
     ]), "JAVA STUB THROW"),
    ("java-stub-uoe-bare",
     diff_for(REFLINK, [
         ("+", "\t\tthrow new UnsupportedOperationException();"),
     ]), "JAVA STUB THROW"),
    ("java-stub-uoe-multiline",
     diff_for(REFLINK, [
         ("+", "\t\tthrow new UnsupportedOperationException("),
         ("+", "\t\t\t\"bar not supported\");"),
     ]), "JAVA STUB THROW"),
    ("java-stub-runtime-todo",
     diff_for(REFLINK, [
         ("+", "\t\tthrow new RuntimeException(\"todo: implement this\");"),
     ]), "JAVA STUB THROW"),
    ("java-stub-uoe-stub-word",
     diff_for(REFLINK, [
         ("+", "\t\tthrow new UnsupportedOperationException(\"stub\");"),
     ]), "JAVA STUB THROW"),
    ("java-failfast-stub-loaded-passes",
     diff_for(REFLINK, [
         ("+", "\t\tthrow new IllegalStateException("),
         ("+", "\t\t\t\"FATAL: Phase-1 Scheduler stub loaded instead of the Phase-2 \""),
         ("+", "\t\t\t\t+ \"overlay; fix the classpath order\","),
     ]), None),
    ("java-throw-legit-op-passes",
     diff_for(REFLINK, [
         ("+", "\t\tthrow new UnsupportedOperationException(\"not an integer op: \" + op);"),
     ]), None),
    ("java-throw-legit-read-passes",
     diff_for(REFLINK, [
         ("+", "\t\tthrow new UnsupportedOperationException(\"cannot read \" + kind() + \" as long\");"),
     ]), None),
    # P3frontend frozen synthetics (the p3frontend registration surface:
    # p3frontend emits no wire kinds, it registers frozen packages).
    ("p3importer-new-case",
     diff_for(P3IMP, [
         (" ", "\tswitch path {"),
         ("+", "\tcase \"mynew\":"),
         ("+", "\t\tpkg := syntheticMynewPackage()"),
         ("+", "\t\timp.pkgs[path] = pkg"),
         ("+", "\t\treturn pkg, nil"),
     ]), "NEW FROZEN SYNTHETIC PACKAGE (p3frontend importer)"),
    ("p3importer-case-without-synth-passes",
     diff_for(P3IMP, [
         (" ", "\tswitch path {"),
         ("+", "\tcase \"mynew\":"),
         ("+", "\t\treturn nil, errNoSuchPackage"),
     ]), None),
    ("p3loader-new-list-string",
     diff_for(P3LOAD, [
         (" ", "\tcase \"runtime\", \"fmt\", \"sync\","),
         ("+", "\t\t\"mynewpkg\","),
         (" ", "\t\treturn &PackageRef{ImportPath: path, Kind: kindSynthetic}, nil"),
     ]), "NEW FROZEN SYNTHETIC PACKAGE (p3frontend loader)"),
    ("p3loader-edit-adds-string",
     diff_for(P3LOAD, [
         ("-", "\tcase \"runtime\", \"fmt\", \"sync\","),
         ("+", "\tcase \"runtime\", \"fmt\", \"sync\", \"mynewpkg\","),
         (" ", "\t\treturn &PackageRef{ImportPath: path, Kind: kindSynthetic}, nil"),
     ]), "NEW FROZEN SYNTHETIC PACKAGE (p3frontend loader)"),
    # Old behavior must not regress.
    ("old-new-synth-file",
     new_file_diff("internal/frontend/synth_foo.go", ["package frontend"]),
     "NEW SYNTHETIC FILE"),
    ("old-addfunc-outside-synth",
     diff_for(EXPORT_GO, [
         ("+", "\taddFunc(\"Foo\", fooDecl)"),
     ]), "SYNTHETIC REGISTRATION"),
    ("old-go-panic-stub",
     diff_for(EXPORT_GO, [
         ("+", "\t\tpanic(\"not implemented\")"),
     ]), "PANIC STUB"),
    ("old-go-eq-conditional",
     diff_for(EXPORT_GO, [
         ("+", "\tif pkg == \"fmt\" {"),
     ]), "PACKAGE-NAME CONDITIONAL"),
    ("clean-diff-passes",
     diff_for("cmd/graalgo/main.go", [
         (" ", "// A plain comment change."),
         ("+", "// With one more line of comment."),
     ]), None),
]


def main():
    failures = []
    for name, diff, expect in CASES:
        errors = gate.check_no_bridges(diff)
        if expect is None:
            ok = not errors
            detail = "errors=%s" % (errors,) if not ok else ""
        else:
            ok = any(expect in e for e in errors)
            detail = "errors=%s" % (errors,) if not ok else ""
        print("%s %s %s" % ("PASS" if ok else "FAIL", name, detail))
        if not ok:
            failures.append(name)

    # No-vacuous-pass rule: with no determinable diff, main() must fail.
    orig_get_diff = gate.get_diff
    gate.get_diff = lambda: ("", "")
    try:
        rc = gate.main()
    finally:
        gate.get_diff = orig_get_diff
    if rc == 1:
        print("PASS no-vacuous-pass")
    else:
        print("FAIL no-vacuous-pass (main() returned %s, want 1)" % rc)
        failures.append("no-vacuous-pass")

    if failures:
        print("FAILED: %d case(s): %s" % (len(failures), ", ".join(failures)))
        return 1
    print("All %d no-bridges positive controls passed." % (len(CASES) + 1))
    return 0


if __name__ == "__main__":
    sys.exit(main())
