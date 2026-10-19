#!/usr/bin/env python3
"""
No-bridges check (2026-10-15, his direct order).

Fails if the diff introduces new bridges:
- New synth_*.go files
- New package-name conditionals in frontend/backend
- New panic stubs
- New wire node kinds on the frontend wire (inverted allowlist: any added
  kind outside the explicit allowlist fails — see check 4)
- New synthetic function registrations

Existing bridges are grandfathered (they're in the removal queue — see
docs/arch/bridge-inventory.md). This check only looks at ADDED lines in the diff.

Diff sources, in order:
  1. origin/master...HEAD  (PR / feature branch)
  2. git diff HEAD         (uncommitted working-tree changes)
  3. git diff --cached     (staged changes)
  4. HEAD~1...HEAD         (direct master push: wt.py integrate + push, no PR —
                           checkout lands exactly on origin/master, so 1-3 are
                           all empty; inspect what the push itself introduced)

The check NEVER passes vacuously: if no diff can be determined (e.g. a
shallow checkout with no history), it fails loudly instead of skipping.
CI must check out full history (fetch-depth: 0) for source 4 to resolve.
"""

import os
import re
import subprocess
import sys

# Adjudicated real implementations (NOT bridges): additions the mechanical
# checks below flag by pattern, but an independent reviewer cleared as
# genuine implementations. Narrowly scoped to the exact adjudicated symbols —
# do NOT broaden an entry or add one without a fresh independent review.
# Inventory record: docs/arch/bridge-inventory.md ("Adjudicated non-bridges").
ADJUDICATED_WIRE_NODES = {
    # (file substring, wire node kind)
    # PKG-023 (commit 1d4e7964, 2026-10-16; independent review CLEAN, no
    # veto): "runtimenumgoroutine" is a real implementation following the
    # runtimenumcpu precedent (backend NumGoroutineNode on the scheduler's
    # live-goroutine counter), not a new bridge.
    ("internal/frontend/export.go", "runtimenumgoroutine"),
    # PKG-040 (lane commit 19786941 + coordinator adjudication, 2026-10-16;
    # independent review CLEAN, no veto): "runtimegosched" is a real implementation
    # following the runtimenumgoroutine precedent (backend GoschedNode on
    # the scheduler's voluntary-yield primitive SchedulerCore.yieldCurrent),
    # not a new bridge.
    ("internal/frontend/export.go", "runtimegosched"),
}
ADJUDICATED_REGISTRATIONS = {
    # (file substring, addFunc'd symbol name)
    # PKG-023 (commit 1d4e7964, 2026-10-16; independent review CLEAN, no
    # veto): addFunc("NumGoroutine", ...) declares genuine stdlib surface in
    # the synthetic runtime package; the backend implements it for real.
    ("internal/frontend/export.go", "NumGoroutine"),
    # PKG-040 (lane commit 19786941 + coordinator adjudication, 2026-10-16;
    # independent review CLEAN, no veto): the Gosched overlay declaration (provided by
    # PKG-038 on master; the lane's duplicate declaration was dropped at
    # integration) is genuine stdlib surface — gc 1.27.1 src/runtime/proc.go
    # — and the backend implements it for real via GoschedNode. The
    # registerStmtKind("runtimegosched", ...) P3 registrations are the
    # public register-kind hook (no Phase-1 factory change), not synthetic
    # stdlib logic.
    ("internal/frontend/export.go", "Gosched"),
}

# ---------------------------------------------------------------------------
# ---------------------------------------------------------------------------
# Wire-node kind allowlist (check 4). INVERTED CHECK (2026-10-19, P1-3
# remediation; 2026-10-19 round 2 closes the variable-emission hole the
# P1-3 review vetoed in c03b45ec):
# The old check flagged name patterns that *looked* stdlib-specific
# (r'^(runtime|fmt|sync|os|net)[a-z]') -- a denylist that silently missed
# time*/unsafe*/strings*/bytes*/strconv*/reflect*/crypto*/syscall*/io*/
# bufio*/context*/errors*/encoding*/math*/sort* kinds and only scanned
# export.go. The inverted check flags ANY node kind added to the wire that
# is not in the explicit allowlist below. A new bridge needs a new kind
# name, so it cannot pass silently; a genuinely new generic AST kind fails
# loudly and a human adds it here in the same change (with independent
# review).
#
# VARIABLE-EMITTED KINDS (round-2 fix for veto finding F1): wire nodes are
# not only emitted as .node("literal", ...). Four sites emit through a
# variable, so the literal-only enumeration was blind to 13 kinds:
#   - internal/frontend/synth_machinery.go exportMathIntrinsic: the six
#     math* kinds come from a map[string]string literal lookup
#     ("Float64bits"->"mathf64bits", "Float64frombits"->"mathf64frombits",
#     "Float32bits"->"mathf32bits", "Float32frombits"->"mathf32frombits",
#     "Sqrt"->"mathsqrt", "FMA"->"mathfma"), then e.node(kind, ...).
#   - internal/frontend/mrdesugar_m5.go desugarRuntimeStack: kind =
#     "runtimecaller"/"runtimeframesnext"/"runtimefuncfileline" in the
#     rtStackOp branches, then e.node(kind, ...).
#   - internal/frontend/export.go exportCall (runtime.Func Name/Entry):
#     kind := "runtimefuncname"; if mname == "Entry" { kind =
#     "runtimefuncentry" }, then e.node(kind, ...).
#   - internal/frontend/export.go exportBuiltin (len/cap of channels):
#     kind := "chanlen"; if b.Name() == "cap" { kind = "chancap" }, then
#     e.node(kind, ...).
# Check 4 now resolves such variables MECHANICALLY (resolve_var_kinds
# below): every static assignment to the identifier in the file must be a
# string literal or a literal->literal map[string]string lookup. If all
# resolved literals are allowlisted (or adjudicated), the emission passes;
# otherwise it fails loudly as SUSPICIOUS VARIABLE WIRE NODE. Anything the
# resolver cannot prove literal -- computed kinds, cross-file assignments,
# function returns, concatenation -- fails LOUD, never passes silently
# (fail-closed; a human adjudicates). The frontend is the sole wire
# emitter (export.go's node() helper is the single constructor), so no
# silent-pass shape exists outside it.
#
# Generation (disk truth -- re-run to refresh after a legitimate addition):
#   1. literals:
#      grep -rhoE '\.node\s*\(\s*"[^"]+"' --include='*.go' internal/frontend/ \
#        | sed -E 's/.*\(\s*"([^"]+)".*/\1/' | sort -u
#      grep -rhoE 'Node\{Kind:\s*"[^"]+"' --include='*.go' internal/frontend/ \
#        | sed -E 's/.*"([^"]+)".*/\1/' | sort -u
#   2. variable emissions: resolve_var_kinds() over every .node(<ident>,)
#      / Node{Kind: <ident>} site in internal/frontend/ (the four sites
#      above). The self-test's cross-check does exactly this, so the
#      snapshot cannot go stale silently.
# Snapshot below taken 2026-10-19 at master 56a3dd68 (83 literal kinds:
# 51+32) plus the 13 variable-emitted kinds (11 stdlib + 2 generic).
#
# GENERIC_WIRE_NODE_KINDS: go/ast-vocabulary node kinds -- the language's own
# constructs (statements, expressions, channel builtins, builtin calls like
# clear/minmax). The method* entries (methodref/methodvalue/ifacemethod/
# funcfieldcall) are dispatch machinery, not stdlib surface. "chanlen"/
# "chancap" (round 2): lowerings of the BUILTIN len()/cap() applied to
# channels -- the frozen LenNode rejects CHAN (P3-M5 WS E, ADR-0005 App C;
# export.go ~line 9027) -- so they are language vocabulary like
# chanclose/makechan, not stdlib surface.
#
# KNOWN_STDLIB_WIRE_NODE_KINDS: stdlib-specific kinds already present in the
# tree at the snapshot above (time*/unsafe*/runtime*/math* families plus
# syncop, reflectop, sliceoverlap, abifuncpc, atomicaddr). Grandfathering
# authority for these is docs/arch/bridge-inventory.md -- MERGE-ORDER NOTE
# (veto finding F2): the entries covering the time*/unsafe*/runtime*/math*
# families (B-8, B-9, B-10, B-11) and the miscellaneous nodes (B-12:
# sliceoverlap, abifuncpc, atomicaddr, reflectop, syncop) exist ONLY on
# branch wt/graalgo-independent-codebase-review/rem-bridge-inventory,
# which is UNMERGED as of this commit. That branch MUST merge to master
# BEFORE this gate change; until then the "grandfathered by inventory"
# claim in this comment is prospective, not yet true on master. The gate
# itself only inspects ADDED lines, so anything already in the tree is
# grandfathered by construction -- this set exists so a re-added or moved
# kind does not false-positive, and the gate fires only on genuinely NEW
# kinds. The 14 kinds the veto called out (runtimegomaxprocs,
# runtimegoroot, runtimereadmemstats, runtimestack, runtimeversion,
# sliceoverlap, runtimenumcpu, unsafefromptr, unsafefromuint, unsafeslice,
# unsafeslicedata, unsafestring, unsafestringdata, unsafetooptr) are covered
# by B-8/B-10/B-11/B-12 on that branch; the 13 variable-emitted kinds by
# B-9 (math*), B-10 (runtimecaller/runtimeframesnext/runtimefuncfileline/
# runtimefuncname/runtimefuncentry) and the GENERIC classification above
# (chanlen/chancap).
#
# Adjudicated entries (ADJUDICATED_WIRE_NODES above) are honored first: they
# are the narrow per-symbol, per-file exemption record with review refs.
GENERIC_WIRE_NODE_KINDS = {
    "addrof",
    "assign",
    "basiclit",
    "binary",
    "blank",
    "block",
    "branch",
    "call",
    "caseclause",
    "chanclose",
    "chancap",
    "chanlen",
    "clear",
    "complit",
    "compositelit",
    "convert",
    "defer",
    "deref",
    "exprstmt",
    "for",
    "funcdecl",
    "funcfieldcall",
    "funclit",
    "gostmt",
    "ident",
    "if",
    "ifacemethod",
    "incdec",
    "index",
    "labeled",
    "makechan",
    "methodref",
    "methodvalue",
    "minmax",
    "nil",
    "param",
    "paren",
    "pkgref",
    "range",
    "recv",
    "return",
    "select",
    "selectcase",
    "selector",
    "send",
    "slice",
    "sliceexpr",
    "switch",
    "typeassert",
    "typecase",
    "typeswitch",
    "unary",
    "vardecl"
}

KNOWN_STDLIB_WIRE_NODE_KINDS = {
    "abifuncpc",
    "atomicaddr",
    "mathf32bits",
    "mathf32frombits",
    "mathf64bits",
    "mathf64frombits",
    "mathfma",
    "mathsqrt",
    "reflectop",
    "runtimecaller",
    "runtimecallers",
    "runtimecallersframes",
    "runtimeframesnext",
    "runtimefuncfileline",
    "runtimefuncforpc",
    "runtimefuncname",
    "runtimefuncentry",
    "runtimegomaxprocs",
    "runtimegoroot",
    "runtimegosched",
    "runtimenumcpu",
    "runtimenumgoroutine",
    "runtimereadmemstats",
    "runtimestack",
    "runtimeversion",
    "sliceoverlap",
    "syncop",
    "timeafter",
    "timeafterfunc",
    "timec",
    "timenew",
    "timenow",
    "timeop",
    "timesleep",
    "timetick",
    "unsafeadd",
    "unsafefromptr",
    "unsafefromuint",
    "unsafeslice",
    "unsafeslicedata",
    "unsafestring",
    "unsafestringdata",
    "unsafetooptr"
}

WIRE_NODE_KINDS_ALLOWLIST = GENERIC_WIRE_NODE_KINDS | KNOWN_STDLIB_WIRE_NODE_KINDS


def resolve_var_kinds(file_text, ident):
    """Resolve a wire-node kind variable to the literals it can hold.

    Mechanical, fail-closed: returns the set of string literals the
    identifier can hold iff EVERY static assignment to it in this file is
    a string literal (kind := "..." / kind = "...") or a literal->literal
    map[string]string lookup (kind, ok := map[string]string{...}[name]).
    Returns None (unresolvable) when no assignment is found or any
    assignment is non-literal (function call, concatenation, another
    variable, named map, ...). Callers must fail LOUD on None, never pass.
    """
    idp = re.escape(ident)
    vals = set()
    found = False
    # Every assignment statement to ident:  kind := ... / kind = ... /
    # kind, ok := ...  (the (?<![\w.]) guard keeps "otherkind" and
    # "e.kind" from matching; the (?!=) guard keeps "kind ==" out).
    assign_re = re.compile(r'(?<![\w.])' + idp + r'\b\s*(?:,\s*\w+\s*)?(:?=)(?!=)\s*')
    for m in assign_re.finditer(file_text):
        found = True
        rest = file_text[m.end():]
        # The RHS must be EXACTLY a string literal: the terminator class
        # after the closing quote rejects concatenation ("a" + suffix),
        # which would otherwise match the leading literal and slip
        # through as allowlisted.
        sm = re.match(r'"([^"]*)"\s*(?:[;,)\n}]|//|$)', rest)
        if sm:
            vals.add(sm.group(1))
            continue
        mm = re.match(r'map\[string\]string\{', rest)
        if mm:
            depth, i = 0, m.end() + mm.end() - 1
            j = i
            while j < len(file_text):
                if file_text[j] == '{':
                    depth += 1
                elif file_text[j] == '}':
                    depth -= 1
                    if depth == 0:
                        break
                j += 1
            else:
                return None  # unbalanced -- cannot prove literal
            body = file_text[i + 1:j]
            # The whole map body must be literal->literal pairs; a single
            # non-literal value (or key) makes the lookup unresolvable.
            if not re.fullmatch(r'\s*(?:"[^"]*"\s*:\s*"[^"]*"\s*,?\s*)*', body):
                return None
            vals.update(re.findall(r':\s*"([^"]*)"', body))
            continue
        return None  # non-literal RHS -- fail closed
    return vals if found else None


def _check4_kind_ok(fname, node_kind):
    """True iff a resolved kind passes check 4 (adjudicated or allowlisted)."""
    if any(f_sub in fname and nk == node_kind
           for f_sub, nk in ADJUDICATED_WIRE_NODES):
        return True
    return node_kind in WIRE_NODE_KINDS_ALLOWLIST

def get_diff():
    """Get the diff of added lines, plus a label naming the diff source.

    Returns ("", "") when no diff could be determined — the caller must
    treat that as a loud failure, never a vacuous pass.
    """
    # Try vs origin/master first (for PRs), fall back to working tree,
    # then to the master-push delta. A shallow checkout (no HEAD~1)
    # yields nothing from the last source — that is a failure, not a skip.
    candidates = [
        ("origin/master...HEAD", ["git", "diff", "origin/master...HEAD", "--", "."]),
        ("working tree (unstaged)", ["git", "diff", "HEAD", "--", "."]),
        ("staged", ["git", "diff", "--cached", "--", "."]),
        ("HEAD~1...HEAD (master push)", ["git", "diff", "HEAD~1...HEAD", "--", "."]),
    ]
    for name, cmd in candidates:
        try:
            result = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
        except Exception:
            continue
        if result.returncode == 0 and result.stdout.strip():
            return result.stdout, name
    return "", ""

def _repo_top():
    """Repo toplevel for reading post-diff file text (best effort)."""
    try:
        top = subprocess.run(["git", "rev-parse", "--show-toplevel"],
                             capture_output=True, text=True, timeout=15)
    except Exception:
        return None
    if top.returncode != 0:
        return None
    return top.stdout.strip()


def check_no_bridges(diff):
    errors = []
    added_lines = []
    current_file = None

    for line in diff.split("\n"):
        if line.startswith("+++ b/"):
            current_file = line[6:]
        elif line.startswith("+") and not line.startswith("+++"):
            added_lines.append((current_file, line[1:]))

    # 1. New synth_*.go files. Only genuinely NEW files count (the --- line
    #    is /dev/null); modifying a grandfathered synth_*.go is not a new
    #    bridge — without this, every master push touching a synthetic file
    #    would fail the gate.
    new_synth = set()
    prev_was_devnull = False
    for line in diff.split("\n"):
        if line.startswith("--- /dev/null"):
            prev_was_devnull = True
        elif line.startswith("+++ b/"):
            if prev_was_devnull and "synth_" in line and line.endswith(".go"):
                new_synth.add(line[6:])
            prev_was_devnull = False
        elif line.startswith("--- "):
            prev_was_devnull = False
    for f in new_synth:
        errors.append(f"NEW SYNTHETIC FILE: {f} — synthetic stdlib packages are banned")

    # 2. Package-name conditionals (stdlib names)
    stdlib_pkgs = ["fmt", "sync", "os", "net", "http", "runtime", "reflect", "time",
                   "strings", "bytes", "strconv", "sort", "math", "encoding", "crypto",
                   "syscall", "unsafe", "io", "bufio", "context", "errors"]
    for fname, line in added_lines:
        if not fname or not (fname.endswith(".go") or fname.endswith(".java")):
            continue
        # Match: if pkg == "fmt", case "sync", pkg.Name == "os", etc.
        for pkg in stdlib_pkgs:
            patterns = [
                rf'==\s*"{pkg}"',
                rf'case\s*"{pkg}"',
                rf'Name\s*==\s*"{pkg}"',
            ]
            for pat in patterns:
                if re.search(pat, line):
                    # Allow in test files and docs. ("_test." covers
                    # *_test.go harnesses, e.g. drift-test importers that
                    # resolve synthetic import paths like the pre-existing
                    # `if path == "bytes"` stubs.)
                    if "/test" not in fname and "test_" not in fname and "_test." not in fname:
                        errors.append(f"PACKAGE-NAME CONDITIONAL in {fname}: {line.strip()[:80]}")
                        break

    # 3. New panic stubs. The old regex only caught literal "not implemented"/
    #    "unsupported"/"todo" messages; fail-fast stubs also hide behind bare
    #    panic(), panic(""), panic("unreachable"), and composed messages like
    #    panic(fmt.Sprintf("... not implemented")). Fail loud on all of them —
    #    a human decides whether an added panic is a legitimate invariant.
    panic_stub_res = [
        r'panic\s*\(\s*\)',            # bare panic() — nothing to go on
        r'panic\s*\(\s*""\s*\)',       # panic("") — nothing to go on
        r'panic\s*\(\s*"(not implemented|unimplemented|not supported|unsupported|todo|FIXME|unreachable|NYI|not yet|tbd)',
        # Stub keyword anywhere in the first string arg of a composed panic.
        r'panic\s*\(\s*(?:fmt\.Sprintf|fmt\.Errorf|errors\.New)\s*\(\s*"[^"]*(not implemented|unimplemented|not supported|unsupported|todo|FIXME|unreachable|NYI|not yet)',
    ]
    for fname, line in added_lines:
        if not fname or not fname.endswith(".go"):
            continue
        for pat in panic_stub_res:
            if re.search(pat, line, re.I):
                errors.append(f"PANIC STUB in {fname}: {line.strip()[:80]}")
                break

    # 4. New wire node kinds (inverted allowlist — see the sets above).
    # Scans every frontend .go file that emits wire nodes (the directory
    # scope covers future emitter files without a file list to go stale).
    # Three emission shapes are covered:
    #   (a) literal: .node("kind", ...) and &Node{Kind: "kind", ...};
    #   (b) variable: .node(kind, ...) and &Node{Kind: kind, ...} — the
    #       variable is resolved mechanically against the post-diff file
    #       text (resolve_var_kinds, fail-closed); an unresolvable
    #       variable fails loudly as SUSPICIOUS VARIABLE WIRE NODE.
    # Resolution input is the file on disk plus the diff's own added lines
    # for that file, so a kind assigned in the same change is visible.
    def _resolve_in_file(fname, ident, added_for_file):
        top = _repo_top()
        disk = ""
        if top:
            p = os.path.join(top, fname)
            try:
                with open(p, encoding="utf-8") as f:
                    disk = f.read()
            except OSError:
                disk = ""
        return resolve_var_kinds(disk + "\n" + "\n".join(added_for_file), ident)

    added_by_file = {}
    for fname, line in added_lines:
        if fname and fname.endswith(".go") and "internal/frontend/" in fname:
            added_by_file.setdefault(fname, []).append(line)

    for fname, line in added_lines:
        if not fname or not fname.endswith(".go"):
            continue
        if "internal/frontend/" not in fname:
            continue
        # (a) literal shapes.
        kinds = re.findall(r'\.node\s*\(\s*"([^"]+)"', line)
        kinds += re.findall(r'Node\{Kind:\s*"([^"]+)"', line)
        for node_kind in sorted(set(kinds)):
            if not _check4_kind_ok(fname, node_kind):
                errors.append(f"SUSPICIOUS WIRE NODE in {fname}: {node_kind}")
        # (b) variable shapes: .node(ident, ...) / Node{Kind: ident, ...}
        # where ident is a bare identifier, not a string literal.
        var_idents = re.findall(r'\.node\s*\(\s*([A-Za-z_][\w]*)\s*,', line)
        var_idents += re.findall(r'Node\{Kind:\s*([A-Za-z_][\w]*)\s*[,}]', line)
        for ident in sorted(set(var_idents)):
            resolved = _resolve_in_file(fname, ident, added_by_file.get(fname, []))
            if resolved is None:
                errors.append(
                    f"SUSPICIOUS VARIABLE WIRE NODE in {fname}: {ident} — "
                    "the emitted kind cannot be statically resolved to "
                    "allowlisted literals (computed kind, cross-file "
                    "assignment, or non-literal RHS); get an independent "
                    "review or make the kind a literal")
            elif not all(_check4_kind_ok(fname, k) for k in resolved):
                bad = sorted(k for k in resolved if not _check4_kind_ok(fname, k))
                errors.append(
                    f"SUSPICIOUS VARIABLE WIRE NODE in {fname}: {ident} "
                    f"resolves to non-allowlisted kind(s): {', '.join(bad)}")

    # 5. New synthetic function registrations
    for fname, line in added_lines:
        if not fname or not fname.endswith(".go"):
            continue
        if re.search(r'addFunc\s*\(|synthetic.*Package\s*\(|registerSynthetic', line):
            # Allow in existing synth files (grandfathered), flag in new contexts
            if "synth_" not in fname:
                # Adjudicated real implementations are not bridges.
                m = re.search(r'addFunc\s*\(\s*"([^"]+)"', line)
                if m and any(f_sub in fname and sym == m.group(1)
                             for f_sub, sym in ADJUDICATED_REGISTRATIONS):
                    continue
                errors.append(f"SYNTHETIC REGISTRATION in {fname}: {line.strip()[:80]}")

    return errors

def main(argv=None):
    if argv is None:
        argv = sys.argv[1:]
    if "--self-test" in argv:
        # Self-test with positive controls (a synthetic timefoo/unsafebar
        # wire node must fail the gate; adjudicated additions must pass).
        # Imported lazily so the gate itself stays dependency-free.
        sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
        import test_check_no_bridges
        return test_check_no_bridges.main()

    diff, source = get_diff()
    if not diff.strip():
        # Never pass vacuously: on the standard integration path (wt.py
        # integrate + push, no PR) every diff source above is empty only when
        # the checkout has no history to diff against. Skipping here is
        # exactly the false-green hole this check exists to close.
        print("NO-BRIDGES CHECK FAILED:")
        print("  could not determine any diff to inspect (no PR diff, no")
        print("  working-tree changes, and HEAD~1 is unavailable — is this a")
        print("  shallow checkout? CI must use fetch-depth: 0).")
        print("  Refusing to pass vacuously.")
        return 1

    print(f"No-bridges check: inspecting added lines from {source}.")
    errors = check_no_bridges(diff)
    if errors:
        print("NO-BRIDGES CHECK FAILED:")
        print()
        for e in errors:
            print(f"  ✗ {e}")
        print()
        print("His order (2026-10-15): No new bridges. Fix the compiler/runtime properly.")
        return 1

    print("No-bridges check passed.")
    return 0

if __name__ == "__main__":
    sys.exit(main())
