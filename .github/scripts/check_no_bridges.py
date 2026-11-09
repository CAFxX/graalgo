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
  0. <base>...HEAD         (pushed range, when a pre-push base is supplied —
                           CI passes github.event.before via NO_BRIDGES_BASE
                           on push events, so a multi-commit push is
                           inspected in full, not just its tip commit)
  1. origin/master...HEAD  (PR / feature branch)
  2. git diff HEAD         (uncommitted working-tree changes)
  3. git diff --cached     (staged changes)
  4. HEAD~1...HEAD         (last resort when no base is available: inspects
                           only the tip commit — a direct master push whose
                           base was not supplied is therefore only partly
                           covered; CI must always supply the base)

A supplied base that is the zero SHA (0000000…, a newly created branch —
no pre-push commit exists) or that does not resolve in this checkout
fails loudly instead of falling through to source 4: silently
inspecting only the tip would be exactly the hole this check exists
to close. An empty pushed range (base == HEAD — the push introduced
no new changes) likewise fails loudly rather than passing vacuously.

The check NEVER passes vacuously: if no diff can be determined (e.g. a
shallow checkout with no history), it fails loudly instead of skipping.
CI must check out full history (fetch-depth: 0) for the base and
source 4 to resolve.
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

_ZERO_SHA_RE = re.compile(r"^0+$")


def get_diff(base=None):
    """Get the diff of added lines, plus a label naming the diff source.

    `base` is the pre-push commit (CI: github.event.before, passed via
    the NO_BRIDGES_BASE environment variable). When supplied, the pushed
    range <base>...HEAD is inspected first, so a multi-commit push is
    covered in full — not just its tip commit.

    Returns ("", "") when no diff could be determined — the caller must
    treat that as a loud failure, never a vacuous pass.

    Raises ValueError when a supplied base is unusable: the zero SHA
    (new branch — there is no pre-push commit to diff against) or a SHA
    that does not resolve in this checkout. Both fail loudly instead of
    silently falling back to the tip-only HEAD~1...HEAD source, which
    would be exactly the hole this check exists to close.
    """
    base = (base or "").strip()
    if base:
        if _ZERO_SHA_RE.match(base):
            raise ValueError(
                f"no-bridges base {base} is the zero SHA: the pushed ref "
                "is newly created, so there is no pre-push commit to diff "
                "against — refusing to guess a range (and refusing to "
                "inspect only the tip commit).")
        try:
            result = subprocess.run(
                ["git", "diff", f"{base}...HEAD", "--", "."],
                capture_output=True, text=True, timeout=30)
        except Exception as e:
            raise ValueError(
                f"no-bridges base {base}: could not run git diff ({e}) — "
                "refusing to fall back to the tip-only diff.") from e
        if result.returncode != 0:
            raise ValueError(
                f"no-bridges base {base} does not resolve in this checkout "
                "(git diff failed) — is this a shallow checkout? CI must "
                "use fetch-depth: 0. Refusing to fall back to the "
                "tip-only diff.")
        if result.stdout.strip():
            return result.stdout, f"pushed range {base[:7]}...HEAD"
        print(f"no-bridges: pushed range {base[:7]}...HEAD is empty "
              "(the push introduced no new changes).",
              file=sys.stderr)
        return "", ""
    # No base supplied: fall back to the pre-existing candidate chain.
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

    # 4. New wire node kinds for stdlib
    for fname, line in added_lines:
        if not fname or "export.go" not in fname:
            continue
        m = re.search(r'e\.node\s*\(\s*"([^"]+)"', line)
        if m:
            node_kind = m.group(1)
            # Flag runtime*/fmt*/sync*/etc. node kinds (stdlib-specific)
            if re.match(r'^(runtime|fmt|sync|os|net)[a-z]', node_kind):
                # Adjudicated real implementations are not bridges.
                if any(f_sub in fname and nk == node_kind
                       for f_sub, nk in ADJUDICATED_WIRE_NODES):
                    continue
                errors.append(f"SUSPICIOUS WIRE NODE in {fname}: {node_kind}")

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
    # The pre-push base comes from the environment (CI sets
    # NO_BRIDGES_BASE from github.event.before on push events; empty on
    # PR/schedule/dispatch runs and local runs, where the fallback chain
    # in get_diff() applies). A bad base fails loudly, never silently.
    try:
        diff, source = get_diff(os.environ.get("NO_BRIDGES_BASE", ""))
    except ValueError as e:
        print("NO-BRIDGES CHECK FAILED:")
        print(f"  {e}")
        print("  Refusing to pass vacuously.")
        return 1
    if not diff.strip():
        # Never pass vacuously: on the standard integration path (wt.py
        # integrate + push, no PR) every diff source above is empty only when
        # the checkout has no history to diff against. Skipping here is
        # exactly the false-green hole this check exists to close.
        # (A supplied-but-empty pushed range is reported here too — see
        # the note get_diff() printed above.)
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
