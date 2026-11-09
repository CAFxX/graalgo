#!/usr/bin/env python3
"""
No-bridges check (2026-10-15, his direct order).

Fails if the diff introduces new bridges:
- New synth_*.go files
- New package-name conditionals in frontend/backend
- New panic stubs
- New wire node kinds for stdlib functions
- New synthetic function registrations

Existing bridges are grandfathered (they're in the removal queue — see
docs/arch/bridge-inventory.md). This check only looks at ADDED lines in the diff.

Two exemptions keep the gate from misfiring on bridge RETIREMENTS:
- Rule 2 (package-name conditionals) is narrowing-aware: an added
  conditional line is not a new special case when the same hunk removes a
  conditional line mentioning the same (kind, package) pair — the edit
  only shrinks or reshuffles a pre-existing conditional. Widening (adding
  a package the removed lines never mentioned) still flags.
- Rule 5 (synthetic registrations) exempts test files, like Rule 2
  already did: a drift test calling a pre-existing synthetic builder pins
  the inventoried mirror against the live source; it registers no new
  synthetic package, and test files are never shipped code.
Both are pinned by .github/scripts/test_check_no_bridges.py (fixtures
taken from the p3fmt-stub branch diff, 16547f17, which the old script
failed with 3 false positives).

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

# Stdlib-specific wire node kinds (R10-07 allowlist) + known-generic kinds.
#
# Derivation (mechanical, do not hand-count): internal/frontend/tools/
# validate_frontend_v1.py _exporter_kinds() returns 94 kinds at the
# R10-07 base (d4936e9b): 47 stdlib-specific (KNOWN_WIRE_NODE_KINDS
# below) and 47 generic AST kinds (KNOWN_GENERIC_NODE_KINDS_V1). The 4
# v2-only generic kinds (internal/frontend/v2/emit/nodes.go:
# compositelit, fieldname, pkgref, slice) are NOT in the 94 — the v1
# validator never sees them (REVIEW-2026-11-09-10 R10-P2-23) — so they
# are listed separately. test_check_no_bridges.py pins the counts
# (test_allowlist_covers_exporter_kinds, test_v2_generic_kinds_known):
# a kind the exporter constructs but neither set contains is a gate hole.
# Adding a kind to either set needs an independent reviewer's
# adjudication — the lane proposes, it does not self-adjudicate
# (REVIEW-2026-11-09-10 R10-07). A kind in NEITHER set is a new wire
# node until a reviewer says otherwise.
#
# Count correction to the finding's hand count (verified mechanically at
# the base): the finding says "36 blind" kinds; distinct blind
# kind-strings are 35 (47 allowlist − 12 the old gate could see: the 11
# literal runtime* kinds in export.go plus syncop). The 36th entry is
# syncop's second emission site (mrdesugar_m5.go:3369) — a blind SITE of
# a gate-visible KIND. The 47 total stands.
KNOWN_WIRE_NODE_KINDS = {
    # abi
    "abifuncpc",
    # atomic (export.go literals; the old Rule 4 prefix never matched them)
    "atomicaddr", "atomicptrop",
    # chan (variable kind: export.go kind := "chanlen" / = "chancap")
    "chancap", "chanlen",
    # math (map-literal values: exportMathIntrinsic, synth_machinery.go)
    "mathf32bits", "mathf32frombits", "mathf64bits", "mathf64frombits",
    "mathfma", "mathsqrt",
    # reflect (export.go literal + synth_reflect.go second site)
    "reflectop",
    # runtime: 11 literals in export.go — the only kinds the old gate
    # could see — plus 5 variable kinds (runtimefuncname/funcentry in
    # export.go; runtimecaller/framesnext/funcfileline in mrdesugar_m5.go)
    "runtimecaller", "runtimecallers", "runtimecallersframes",
    "runtimeframesnext", "runtimefuncentry", "runtimefuncfileline",
    "runtimefuncforpc", "runtimefuncname", "runtimegomaxprocs",
    "runtimegoroot", "runtimegosched", "runtimenumcpu",
    "runtimenumgoroutine", "runtimereadmemstats", "runtimestack",
    "runtimeversion",
    # slice (exportAliasIntrinsic, synth_machinery.go)
    "sliceoverlap",
    # sync (export.go literal + mrdesugar_m5.go second site)
    "syncop",
    # time (synth_machinery.go literals; the old rule never scanned the file)
    "timeafter", "timeafterfunc", "timec", "timenew", "timenow",
    "timeop", "timesleep", "timetick",
    # unsafe (synth_unsafe.go + unsafe_derive.go literals)
    "unsafeadd", "unsafederive", "unsafefromptr", "unsafefromuint",
    "unsafeslice", "unsafeslicedata", "unsafestring", "unsafestringdata",
    "unsafetooptr",
}
# Adjudicated members (independent review CLEAN, not bridges):
#   "runtimenumgoroutine" — PKG-023 (commit 1d4e7964, 2026-10-16).
#   "runtimegosched" — PKG-040 (lane commit 19786941, 2026-10-16).
# Their adjudication records live in ADJUDICATED_WIRE_NODES above and in
# docs/arch/bridge-inventory.md ("Adjudicated non-bridges"); allowlist
# membership is the exemption Rule 4 consults.
KNOWN_GENERIC_NODE_KINDS_V1 = {
    "addrof", "assign", "basiclit", "binary", "blank", "block", "branch",
    "call", "caseclause", "chanclose", "clear", "complit", "convert",
    "defer", "deref", "exprstmt", "for", "funcdecl", "funclit", "gostmt",
    "ident", "if", "ifacemethod", "incdec", "index", "labeled",
    "makechan", "methodref", "methodvalue", "minmax", "nil", "param",
    "paren", "range", "recv", "return", "select", "selectcase",
    "selector", "send", "sliceexpr", "switch", "typeassert", "typecase",
    "typeswitch", "unary", "vardecl",
}
KNOWN_GENERIC_NODE_KINDS_V2 = {
    # v2-only generic kinds (internal/frontend/v2/emit/nodes.go).
    "compositelit", "fieldname", "pkgref", "slice",
}
KNOWN_GENERIC_NODE_KINDS = KNOWN_GENERIC_NODE_KINDS_V1 | KNOWN_GENERIC_NODE_KINDS_V2

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

def _is_test_file(fname):
    """Test files and test harnesses: never shipped code, so a pattern
    that is only meaningful in shipped code does not apply there.
    (Same predicate Rule 2 already used; Rule 5 now shares it.)"""
    return "/test" in fname or "test_" in fname or "_test." in fname


def check_no_bridges(diff):
    errors = []
    added_lines = []
    current_file = None
    # Per-hunk (file, ordered) pairs: Rule 2's narrowing check
    # compares an added conditional against the removed lines of the SAME
    # hunk — a narrowing edit is by definition a removed+added line pair
    # in one hunk; a conditional added with no removed counterpart in its
    # hunk is new until proven otherwise. `ordered` keeps every hunk
    # line in diff order as (status, text) — context lines included —
    # so wrapped case lists can be tracked across unchanged lines: a
    # continuation line only counts as part of the `case` list above it.
    hunks = []
    current_hunk = None

    for line in diff.split("\n"):
        if line.startswith("+++ b/"):
            current_file = line[6:]
        elif line.startswith("@@"):
            current_hunk = (current_file, [])
            hunks.append(current_hunk)
        elif line.startswith("+") and not line.startswith("+++"):
            added_lines.append((current_file, line[1:]))
            if current_hunk is not None:
                current_hunk[1].append(("+", line[1:]))
        elif line.startswith("-") and not line.startswith("---"):
            if current_hunk is not None:
                current_hunk[1].append(("-", line[1:]))
        elif line.startswith(" ") and current_hunk is not None:
            current_hunk[1].append((" ", line[1:]))

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

    def conditional_pairs(line):
        """All (kind, package) package-name conditionals in one line.

        The `case` pattern is anchored to a real clause
        (`^\\s*case\\b`): the unanchored `case\\b.*` matched the
        substring anywhere on the line, so comments (`// edge case:
        "fmt"`), identifiers (`testcase := "fmt"`), and prose
        (`// lowercase "sync"`) false-positived. A case list is one
        keyword followed by many quoted names (`case "time", "fmt",
        ...`), so the pattern sees every label on the line, not just
        the first — and widening the list by appending a package is
        visible to the gate.
        The `==` patterns need no such treatment: each value there is
        preceded by its own `==`."""
        found = []
        for pkg in stdlib_pkgs:
            for kind, pat in (
                ("eq", rf'==\s*"{pkg}"'),
                ("case", rf'^\s*case\b.*"{pkg}"'),
                ("name-eq", rf'Name\s*==\s*"{pkg}"'),
            ):
                if re.search(pat, line):
                    found.append((kind, pkg))
        return found

    # A wrapped case list continues on lines that are only quoted
    # strings (`"os", "net",`, or the clause-terminating
    # `"encoding":`). Such a line is a case-list continuation when it
    # directly follows a `case` line (or another continuation) in the
    # hunk's ordered lines — the Rule 2 walk tracks that state across
    # context lines — or, failing that, when it ends with `,` or `:`,
    # the punctuation every case-list line carries. The fallback
    # covers a widening appended to a deep continuation whose `case`
    # line falls outside the hunk's 3-line context window
    # (loader.go's list wraps across 7 lines). The punctuation guard
    # is load-bearing: a bare `"os"` line — an import-block addition
    # — is NOT a continuation, so this never fires on imports (the
    # stateless reading of "every pure-quoted-string line is a
    # case-list line" would: adding `"os"` to an import block flagged
    # as a package-name conditional). Residual: a string-slice /
    # composite-literal element line (`"os",`) added with no removed
    # counterpart in the hunk is treated as a case continuation and
    # flags — a rare shape, adjudicated by a human like any other
    # tripwire hit.
    _pure_string_line_re = re.compile(
        r'^\s*"[^"]*"(,\s*"[^"]*")*,?\s*:?\s*(//.*)?$')
    _case_list_end_re = re.compile(r'[,:]\s*(//.*)?$')
    _case_line_re = re.compile(r'^\s*case\b')

    def _case_list_pairs(line, in_case_list):
        """("case", pkg) pairs if `line` is a case-list continuation."""
        if not _pure_string_line_re.match(line):
            return []
        if not in_case_list and not _case_list_end_re.search(line):
            return []
        return [("case", pkg) for pkg in stdlib_pkgs
                if re.search(rf'"{pkg}"', line)]

    for fname, ordered in hunks:
        if not fname or not (fname.endswith(".go") or fname.endswith(".java")):
            continue
        # Allow in test files and docs. ("_test." covers *_test.go
        # harnesses, e.g. drift-test importers that resolve synthetic
        # import paths like the pre-existing `if path == "bytes"` stubs.)
        if _is_test_file(fname):
            continue
        # Pairs the hunk's removed lines already conditioned on: an added
        # line mentioning only such pairs narrows/reshuffles a
        # pre-existing conditional instead of adding a new special case.
        # Any pair NOT in the removed set is a widening and still flags
        # (one error per offending added line). Pairs are assigned to
        # every line in one ordered walk first — an added line is exempt
        # no matter where in the hunk its removed counterpart sits —
        # and only then are added lines judged against the removed set.
        # Known limitation, by design: a removed pair exempts an
        # identical added pair anywhere in the same hunk, even in a
        # different conditional (same-hunk, same-kind
        # cross-conditional reuse). The gate is a heuristic tripwire
        # backed by human review, not a proof.
        removed_pairs = set()
        added_with_pairs = []
        in_case_list = False
        for status, line in ordered:
            if _case_line_re.match(line):
                in_case_list = True
            elif not _pure_string_line_re.match(line):
                in_case_list = False
            pairs = conditional_pairs(line)
            pairs += _case_list_pairs(line, in_case_list)
            if status == "-":
                removed_pairs.update(pairs)
            elif status == "+":
                added_with_pairs.append((line, pairs))
        for line, pairs in added_with_pairs:
            new_pairs = [p for p in pairs if p not in removed_pairs]
            if new_pairs:
                errors.append(f"PACKAGE-NAME CONDITIONAL in {fname}: {line.strip()[:80]}")

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

    # 4. New wire node kinds (allowlist check; R10-07).
    #
    # The old rule only inspected added lines in files whose name
    # contains "export.go", only matched a literal e.node("KIND", ...)
    # call, and only flagged kinds matching ^(runtime|fmt|sync|os|net)[a-z].
    # That left 35 of the 47 stdlib-specific kinds gate-blind: literals in
    # non-export.go files (synth_*.go, unsafe_derive.go), variable-kind
    # e.node(kind, ...) sites (export.go, mrdesugar_m5.go,
    # synth_machinery.go), and map-literal kinds (exportMathIntrinsic).
    # The rewrite:
    #   (a) scan added lines in ALL *.go files, not just *export.go*;
    #   (b) resolve variable kinds from `kind :=` / `kind =` assignments
    #       and intrinsic-map values in the same hunk; a non-literal
    #       e.node( call that resolves to no known kind — or to an
    #       unknown one — flags for human review;
    #   (c) flag any kind string not on the allowlist or the
    #       known-generic set, including a new arm that only adds the
    #       assignment (no e.node line in the hunk);
    #   (d) regression fixtures live in test_check_no_bridges.py.
    # Residual hole, by design (finding §R10-07): the gate is Go-diff-only
    # — a new backend Java registration (registerExprKind /
    # registerStmtKind in P3KindRegistrations.java) with no Go change is
    # still invisible. That needs a Java-side check, not a Rule 4 tweak.
    _kind_literal_re = re.compile(r'e\.node\s*\(\s*"([^"]+)"')
    _kind_call_re = re.compile(r'e\.node\s*\(')
    _kind_assign_re = re.compile(r'\bkind\s*:?=\s*"([a-z][a-z0-9]*)"')
    # A new stdlib wire-node kind carries a stdlib-ish prefix by the
    # project's convention (every one of the 47 does). The standalone
    # assignment rule below uses it to stay silent on unrelated `kind`
    # variables (vendored go/doc, go/printer, tests use `kind` for other
    # things: "file", "comm", "ul>\n", ...).
    _stdlib_kind_prefix_re = re.compile(
        r'^(abi|atomic|chan|math|reflect|runtime|slice|sync|time|unsafe)')
    _intrinsic_map_re = re.compile(
        r'^\s*"[A-Z][A-Za-z0-9_]*":\s*"([a-z][a-z0-9]*)"\s*,?\s*$')

    def _known_kind(kind, fname):
        return (kind in KNOWN_WIRE_NODE_KINDS
                or kind in KNOWN_GENERIC_NODE_KINDS
                or any(f_sub in fname and nk == kind
                       for f_sub, nk in ADJUDICATED_WIRE_NODES))

    for fname, ordered in hunks:
        if not fname or not fname.endswith(".go"):
            continue
        if _is_test_file(fname):
            continue
        # Comment-only lines never emit a wire node.
        added = [line for status, line in ordered if status == "+"
                 and not line.lstrip().startswith("//")]
        # (a)+(c) literal kinds on added lines, in any *.go file.
        for line in added:
            m = _kind_literal_re.search(line)
            if m and not _known_kind(m.group(1), fname):
                errors.append(
                    f"SUSPICIOUS WIRE NODE in {fname}: {m.group(1)}")
        # (b) non-literal e.node( calls: resolve the kind from assignments
        #     and intrinsic-map values added in the same hunk. The hunk is
        #     one logical change, so a new call site carries its kind
        #     assignments (or its map entries) with it; a call whose kind
        #     cannot be resolved here is a human-review item, not a pass.
        for line in added:
            if _kind_call_re.search(line) and not _kind_literal_re.search(line):
                candidates = set()
                for aline in added:
                    candidates |= set(_kind_assign_re.findall(aline))
                    mm = _intrinsic_map_re.match(aline)
                    if mm:
                        candidates.add(mm.group(1))
                unknown = sorted(k for k in candidates
                                 if not _known_kind(k, fname))
                if not candidates:
                    errors.append(
                        f"UNRESOLVED WIRE NODE in {fname}: variable-kind "
                        f"e.node call with no kind assignment in the hunk "
                        f"— human review required: {line.strip()[:80]}")
                elif unknown:
                    errors.append(
                        f"SUSPICIOUS WIRE NODE in {fname}: "
                        f"{', '.join(unknown)} (variable-kind e.node call)")
        # (c, standalone) a new arm that only adds the kind assignment —
        # or a new intrinsic-map entry — with no e.node line in the hunk.
        # Scoped to the exporter tree: every wire node is emitted from
        # internal/frontend/ except the vendored go/ subtree (upstream Go
        # sources, no e.node calls) and p3frontend (verified: no e.node
        # calls at the R10-07 base). The prefix filter keeps unrelated
        # `kind` variables silent (see above).
        if (fname.startswith("internal/frontend/")
                and "/go/" not in fname
                and "/p3frontend/" not in fname):
            for line in added:
                for kind in _kind_assign_re.findall(line):
                    if (_stdlib_kind_prefix_re.match(kind)
                            and not _known_kind(kind, fname)):
                        errors.append(
                            f"SUSPICIOUS WIRE NODE in {fname}: {kind} "
                            f"(kind assignment)")
                mm = _intrinsic_map_re.match(line)
                if (mm and _stdlib_kind_prefix_re.match(mm.group(1))
                        and not _known_kind(mm.group(1), fname)):
                    errors.append(
                        f"SUSPICIOUS WIRE NODE in {fname}: {mm.group(1)} "
                        f"(intrinsic-map entry)")

    # 5. New synthetic function registrations
    for fname, line in added_lines:
        if not fname or not fname.endswith(".go"):
            continue
        # Test files are not shipped code: a drift test calling a
        # pre-existing synthetic builder (e.g. syntheticIoPackage()) pins
        # the inventoried mirror against the live source; it registers no
        # new synthetic package. Same exemption as Rule 2.
        if _is_test_file(fname):
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

def main():
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
