#!/usr/bin/env python3
"""
Self-test for check_no_bridges.py (2026-10-19, P1-3 remediation).

Positive controls: a synthetic diff adding e.node("timefoo", ...) or
e.node("unsafebar", ...) in internal/frontend/synth_machinery.go MUST fail
check 4 (SUSPICIOUS WIRE NODE). Under the old
r'^(runtime|fmt|sync|os|net)[a-z]' denylist these passed silently, so these
controls are what keep the P1-3 hole closed — run this self-test against
the old check and it fails ("POSITIVE CONTROL DID NOT FIRE").

Negative controls: the adjudicated additions (runtimenumgoroutine,
runtimegosched in export.go), grandfathered stdlib kinds (timenow) and
generic AST kinds (ident/block) MUST pass.

Also asserts the gate never passes vacuously (empty diff -> loud failure)
and that the check-4 allowlist exactly matches the kinds the tree emits
(disk truth), so the allowlist cannot go stale silently: if you add a
genuinely new node kind, add it to GENERIC_WIRE_NODE_KINDS (with
independent review) in the same change.

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
    # --- positive controls: new bridge-style kinds must FAIL ---
    ("timefoo in synth_machinery.go fails",
     "internal/frontend/synth_machinery.go",
     '\treturn e.node("timefoo", pos, map[string]any{"x": x}), nil',
     True),
    ("unsafebar in synth_machinery.go fails",
     "internal/frontend/synth_machinery.go",
     '\tx := e.node("unsafebar", n, nil)',
     True),
    ("new fmt-family kind in export.go fails (old-regex coverage kept)",
     "internal/frontend/export.go",
     '\treturn e.node("fmtdebug", n, nil), nil',
     True),
    ("unknown kind via &Node{Kind:} literal fails",
     "internal/frontend/synth_unsafe.go",
     '\treturn &Node{Kind: "fakekind", Fields: nil}, nil',
     True),
    # --- negative controls: legitimate additions must PASS ---
    ("adjudicated runtimenumgoroutine passes",
     "internal/frontend/export.go",
     '\treturn e.node("runtimenumgoroutine", n, nil), nil',
     False),
    ("adjudicated runtimegosched passes",
     "internal/frontend/export.go",
     '\treturn e.node("runtimegosched", n, nil), nil',
     False),
    ("grandfathered stdlib kind timenow passes",
     "internal/frontend/synth_machinery.go",
     '\treturn e.node("timenow", pos, map[string]any{"type": typ}), nil',
     False),
    ("generic kind in widened file scope (v2 emitter) passes",
     "internal/frontend/v2/emit/nodes.go",
     '\treturn e.node("ident", name, map[string]any{"name": id}), nil',
     False),
    ("generic kind in export.go passes",
     "internal/frontend/export.go",
     '\treturn e.node("block", b, map[string]any{"stmts": stmts}), nil',
     False),
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

    Guards against the allowlist going stale: a new kind added without an
    allowlist update would false-positive on the author's own diff; a kind
    removed from the tree but left in the allowlist would be a silent hole
    for a bridge reusing that name.
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
    kinds = set()
    for dirpath, _dirnames, filenames in os.walk(fe):
        for fn in filenames:
            if not fn.endswith(".go"):
                continue
            with open(os.path.join(dirpath, fn), encoding="utf-8") as f:
                src = f.read()
            kinds.update(re.findall(r'\.node\s*\(\s*"([^"]+)"', src))
            kinds.update(re.findall(r'Node\{Kind:\s*"([^"]+)"', src))
    try:
        allow = cnb.GENERIC_WIRE_NODE_KINDS | cnb.KNOWN_STDLIB_WIRE_NODE_KINDS
    except AttributeError:
        return ("gate module predates the check-4 allowlist "
                "(P1-3 inversion not applied)")
    if kinds != allow:
        return ("allowlist/tree mismatch: tree=%d allowlist=%d "
                "missing-from-allowlist=%s stale-in-allowlist=%s "
                "(add the new kind to GENERIC_WIRE_NODE_KINDS with "
                "independent review, or drop the stale entry)" %
                (len(kinds), len(allow),
                 sorted(kinds - allow), sorted(allow - kinds)))
    return None


def main():
    failures = []
    for name, path, added, expect_err in CASES:
        errs = wire_errors(make_diff(path, added))
        if expect_err and not errs:
            failures.append(
                "POSITIVE CONTROL DID NOT FIRE: %s (gate passed a new "
                "bridge-style kind — P1-3 hole is back)" % name)
        elif not expect_err and errs:
            failures.append("FALSE POSITIVE: %s -> %s" % (name, errs))

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
    print("self-test passed: %d controls green" % (len(CASES) + 3))
    return 0


if __name__ == "__main__":
    sys.exit(main())
