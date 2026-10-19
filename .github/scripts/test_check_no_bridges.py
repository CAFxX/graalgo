#!/usr/bin/env python3
"""
Self-test for check_no_bridges.py (2026-10-19, P1-3 remediation;
2026-10-19 round 2: variable-emission coverage for veto findings F1/F2).

Positive controls: a synthetic diff adding e.node("timefoo", ...) or
e.node("unsafebar", ...) in internal/frontend/synth_machinery.go MUST fail
check 4 (SUSPICIOUS WIRE NODE). Under the old
r'^(runtime|fmt|sync|os|net)[a-z]' denylist these passed silently, so these
controls are what keep the P1-3 hole closed — run this self-test against
the old check and it fails ("POSITIVE CONTROL DID NOT FIRE").

Round-2 positive controls (veto finding F1): the literal-only enumeration
was blind to variable-emitted kinds, so
  kind := "cryptobridge2"; ...; e.node(kind, ...)
in a synthetic diff MUST fail check 4 (SUSPICIOUS VARIABLE WIRE NODE) —
this is the exact hole class being closed — and a variable the resolver
cannot prove literal (e.node(mysteryKind, ...) with no static assignment)
MUST also fail loudly (fail-closed), never pass silently.

Negative controls: the adjudicated additions (runtimenumgoroutine,
runtimegosched in export.go), grandfathered stdlib kinds (timenow) and
generic AST kinds (ident/block) MUST pass; a variable emission resolving
to an allowlisted kind (kind := "mathsqrt"; e.node(kind, ...)) MUST pass;
and every one of the 13 previously-missing variable-emitted kinds
(mathf64bits/mathf64frombits/mathf32bits/mathf32frombits/mathsqrt/mathfma,
runtimecaller/runtimeframesnext/runtimefuncfileline/runtimefuncname/
runtimefuncentry, chanlen/chancap) MUST be allowlisted.

Also asserts the gate never passes vacuously (empty diff -> loud failure)
and that the check-4 allowlist exactly matches the kinds the tree emits
(disk truth: literal kinds PLUS mechanically-resolved variable-emitted
kinds via resolve_var_kinds), so the allowlist cannot go stale silently:
if you add a genuinely new node kind, add it to GENERIC_WIRE_NODE_KINDS or
KNOWN_STDLIB_WIRE_NODE_KINDS (with independent review) in the same change.

Run:  python3 .github/scripts/test_check_no_bridges.py
  or:  python3 .github/scripts/check_no_bridges.py --self-test
Exit 0 = every control behaves as specified; non-zero = gate regression.

NOTE for the gate's diff parser: no line of this file starts with
"+++ b/", so the synthetic diffs built below (as string values) can never
confuse check_no_bridges' file tracking when the gate scans the diff that
adds this file. And check 4 only scans internal/frontend/ .go files, so
the fixture lines below are never inspected by it.
"""

import io
import os
import re
import subprocess
import sys
from contextlib import redirect_stdout

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import check_no_bridges as cnb


def make_diff(path, *added):
    """Build a minimal unified diff adding the given lines to path."""
    head = [
        "diff --git a/%s b/%s" % (path, path),
        "--- a/" + path,
        "+++ b/" + path,
        "@@ -1,2 +1,%d @@" % (2 + len(added)),
        " ctx",
        " ctx",
    ]
    return "\n".join(head + ["+" + ln for ln in added]) + "\n"


def wire_errors(diff):
    return [e for e in cnb.check_no_bridges(diff) if "WIRE NODE" in e]


# (name, diff path, added line, expect a SUSPICIOUS WIRE NODE error)
CASES = [
    # (name, diff path, expect a SUSPICIOUS WIRE NODE error, added lines...)
    # --- positive controls: new bridge-style kinds must FAIL ---
    ("timefoo in synth_machinery.go fails",
     "internal/frontend/synth_machinery.go", True,
     '\treturn e.node("timefoo", pos, map[string]any{"x": x}), nil'),
    ("unsafebar in synth_machinery.go fails",
     "internal/frontend/synth_machinery.go", True,
     '\tx := e.node("unsafebar", n, nil)'),
    ("new fmt-family kind in export.go fails (old-regex coverage kept)",
     "internal/frontend/export.go", True,
     '\treturn e.node("fmtdebug", n, nil), nil'),
    ("unknown kind via &Node{Kind:} literal fails",
     "internal/frontend/synth_unsafe.go", True,
     '\treturn &Node{Kind: "fakekind", Fields: nil}, nil'),
    # --- round-2 positive controls: variable-emitted kinds (veto F1) ---
    ("variable-emitted cryptobridge2 fails (the exact F1 hole class)",
     "internal/frontend/synth_machinery.go", True,
     '\tkind := "cryptobridge2"',
     '\treturn e.node(kind, n, nil), nil'),
    ("unresolvable variable kind fails loudly (fail-closed)",
     "internal/frontend/export.go", True,
     '\treturn e.node(mysteryKind, n, nil), nil'),
    ("variable-emitted bad kind via &Node{Kind:} fails",
     "internal/frontend/synth_unsafe.go", True,
     '\tkind := "cryptobridge2"',
     '\treturn &Node{Kind: kind, Fields: nil}, nil'),
    # --- negative controls: legitimate additions must PASS ---
    ("adjudicated runtimenumgoroutine passes",
     "internal/frontend/export.go", False,
     '\treturn e.node("runtimenumgoroutine", n, nil), nil'),
    ("adjudicated runtimegosched passes",
     "internal/frontend/export.go", False,
     '\treturn e.node("runtimegosched", n, nil), nil'),
    ("grandfathered stdlib kind timenow passes",
     "internal/frontend/synth_machinery.go", False,
     '\treturn e.node("timenow", pos, map[string]any{"type": typ}), nil'),
    ("generic kind in widened file scope (v2 emitter) passes",
     "internal/frontend/v2/emit/nodes.go", False,
     '\treturn e.node("ident", name, map[string]any{"name": id}), nil'),
    ("generic kind in export.go passes",
     "internal/frontend/export.go", False,
     '\treturn e.node("block", b, map[string]any{"stmts": stmts}), nil'),
    # --- round-2 negative control: allowlisted variable emission passes ---
    ("variable-emitted allowlisted kind passes",
     "internal/frontend/synth_machinery.go", False,
     '\tkind := "mathsqrt"',
     '\treturn e.node(kind, n, nil), nil'),
]

# The 13 variable-emitted kinds the literal-only enumeration missed (veto
# finding F1). Every one must be allowlisted — a re-added or moved kind
# must not false-positive.
PREVIOUSLY_MISSING_KINDS = [
    "mathf64bits", "mathf64frombits", "mathf32bits", "mathf32frombits",
    "mathsqrt", "mathfma",
    "runtimecaller", "runtimeframesnext", "runtimefuncfileline",
    "runtimefuncname", "runtimefuncentry",
    "chanlen", "chancap",
]


def check_no_vacuous_pass():
    """The gate must never pass vacuously: no diff -> loud failure."""
    orig = cnb.get_diff
    cnb.get_diff = lambda: ("", "")
    try:
        buf = io.StringIO()
        with redirect_stdout(buf):
            try:
                rc = cnb.main([])
            except TypeError:
                rc = cnb.main()  # pre---self-test gate: main() took no argv
        out = buf.getvalue()
    finally:
        cnb.get_diff = orig
    if rc != 1:
        return "vacuity broken: main([]) on empty diff returned %r, want 1" % (rc,)
    if "vacuously" not in out:
        return "vacuity wording lost from the empty-diff failure message"
    return None


def check_allowlist_matches_tree():
    """The check-4 allowlist must exactly equal the kinds the tree emits.

    Disk truth, enumerated the same way the gate sees them: literal kinds
    from .node("...") / Node{Kind: "..."} shapes, PLUS mechanically
    resolved variable-emitted kinds via cnb.resolve_var_kinds (the four
    sites: exportMathIntrinsic's math map, desugarRuntimeStack's switch,
    exportCall's Name/Entry kind, exportBuiltin len/cap's chan kind).

    Guards against the allowlist going stale in both directions: a new
    kind added without an allowlist update false-positives on the
    author's own diff; a kind removed from the tree but left in the
    allowlist is a silent hole for a bridge reusing that name. A tree
    emission the resolver cannot prove literal is reported as a failure
    here (fail-closed): the gate would fail loudly on any diff touching
    it, so the snapshot and the resolver must be fixed together.
    """
    try:
        top = subprocess.run(["git", "rev-parse", "--show-toplevel"],
                             capture_output=True, text=True, timeout=15)
    except Exception:
        return "SKIP (git unavailable)"
    if top.returncode != 0:
        return "SKIP (not a git checkout)"
    fe = os.path.join(top.stdout.strip(), "internal", "frontend")
    if not os.path.isdir(fe):
        return "SKIP (no internal/frontend in this checkout)"
    try:
        resolve = cnb.resolve_var_kinds
        allow = cnb.WIRE_NODE_KINDS_ALLOWLIST
    except AttributeError:
        return ("gate module predates the check-4 allowlist/resolver "
                "(P1-3 inversion + F1 variable coverage not applied)")
    kinds = set()
    unresolvable = []
    for dirpath, _dirnames, filenames in os.walk(fe):
        for fn in filenames:
            if not fn.endswith(".go"):
                continue
            p = os.path.join(dirpath, fn)
            with open(p, encoding="utf-8") as f:
                src = f.read()
            kinds.update(re.findall(r'\.node\s*\(\s*"([^"]+)"', src))
            kinds.update(re.findall(r'Node\{Kind:\s*"([^"]+)"', src))
            # Variable shapes: .node(ident, ...) / Node{Kind: ident, ...}.
            # The helper definition `func (e *exporter) node(kind string,`
            # does not match (ident must be followed by a comma).
            idents = set(re.findall(r'\.node\s*\(\s*([A-Za-z_][\w]*)\s*,', src))
            idents.update(re.findall(r'Node\{Kind:\s*([A-Za-z_][\w]*)\s*[,}]', src))
            for ident in idents:
                resolved = resolve(src, ident)
                if resolved is None:
                    unresolvable.append("%s:%s" % (os.path.relpath(p, fe), ident))
                else:
                    kinds.update(resolved)
    if unresolvable:
        return ("tree has variable-emitted kinds the resolver cannot prove "
                "literal (gate would fail loudly on them): %s "
                "(extend resolve_var_kinds or make the kind a literal)" %
                sorted(set(unresolvable)))
    if kinds != allow:
        return ("allowlist/tree mismatch: tree=%d allowlist=%d "
                "missing-from-allowlist=%s stale-in-allowlist=%s "
                "(add the new kind to GENERIC_WIRE_NODE_KINDS or "
                "KNOWN_STDLIB_WIRE_NODE_KINDS with independent review, "
                "or drop the stale entry)" %
                (len(kinds), len(allow),
                 sorted(kinds - allow), sorted(allow - kinds)))
    return None


def main():
    failures = []
    for case in CASES:
        name, path, expect_err = case[0], case[1], case[2]
        added_lines = case[3:]
        errs = wire_errors(make_diff(path, *added_lines))
        if expect_err and not errs:
            failures.append(
                "POSITIVE CONTROL DID NOT FIRE: %s (gate passed a new "
                "bridge-style kind — P1-3/F1 hole is back)" % name)
        elif not expect_err and errs:
            failures.append("FALSE POSITIVE: %s -> %s" % (name, errs))

    # The 13 previously-missing variable-emitted kinds must be allowlisted.
    try:
        allow = cnb.WIRE_NODE_KINDS_ALLOWLIST
    except AttributeError:
        failures.append("gate module predates the check-4 allowlist")
        allow = set()
    for k in PREVIOUSLY_MISSING_KINDS:
        if k not in allow:
            failures.append(
                "MISSING FROM ALLOWLIST: %s (veto F1 kind not allowlisted)" % k)

    # Full-gate sanity: a diff with no wire nodes at all must be clean.
    clean = make_diff("internal/frontend/export.go", "// comment only")
    clean_errs = cnb.check_no_bridges(clean)
    if clean_errs:
        failures.append("clean diff produced errors: %s" % (clean_errs,))

    vac = check_no_vacuous_pass()
    if vac:
        failures.append(vac)

    tree = check_allowlist_matches_tree()
    if tree and not tree.startswith("SKIP"):
        failures.append(tree)
    elif tree:
        print("note: allowlist/tree cross-check %s" % tree)

    if failures:
        print("SELF-TEST FAILED:")
        for f in failures:
            print("  x " + f)
        return 1
    print("self-test passed: %d controls green"
          % (len(CASES) + len(PREVIOUSLY_MISSING_KINDS) + 3))
    return 0


if __name__ == "__main__":
    sys.exit(main())
