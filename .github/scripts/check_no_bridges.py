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
  4. The push range <before>...HEAD, where <before> is the pre-push tip
     of the branch (github.event.before, passed via GITHUB_EVENT_BEFORE
     or --before) — the exact commits a direct master push introduced.
     This is the master-push source: on a post-push checkout the push
     already updated origin/master, so source 1 is empty by construction
     and the old HEAD~1...HEAD fallback inspected only the tip commit,
     letting every other pushed commit escape all five rules (R-1108-91).
  5. HEAD~1...HEAD — LOCAL tip-only fallback, used only when the
     checkout is NOT sitting exactly on origin/master (i.e. not a
     post-push checkout) and no push range is available: a developer
     running the gate by hand on a clean local checkout.

On a push event (GITHUB_EVENT_NAME=push, also passed via env) with no
usable push range — no GITHUB_EVENT_BEFORE, a zero SHA (branch
creation), or a SHA that does not resolve — the gate FAILS LOUDLY
instead of falling back to the tip: a push range that cannot be
determined must never silently become a tip-only inspection. Non-push
runs (schedule / workflow_dispatch / local) keep the old HEAD~1
fallback: they introduce no push, so there is no push range to
determine, and the tip inspection is the pre-existing backstop, not
the enforcement point.

The check NEVER passes vacuously: if no diff can be determined (e.g. a
shallow checkout with no history), it fails loudly instead of skipping.
CI must check out full history (fetch-depth: 0) for the push range to
resolve — and the workflow must pass GITHUB_EVENT_BEFORE and
GITHUB_EVENT_NAME to the gate step (see .github/workflows/ci.yml).
"""

import argparse
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

# Detail for the last loud get_diff() failure ("" when none): the
# specific reason the diff could not be determined. main() prints it
# under the single NO-BRIDGES CHECK FAILED header, so a failed run
# reports the reason once instead of printing two failure blocks.
_FAILURE_DETAIL = ""


def _run_git(args):
    """Run a git command; return the CompletedProcess (never raises)."""
    try:
        return subprocess.run(["git"] + args, capture_output=True,
                              text=True, timeout=30)
    except Exception:
        return None


def _rev_parse(ref):
    """SHA for a ref, or None when it does not resolve."""
    r = _run_git(["rev-parse", "--verify", ref])
    if r is None or r.returncode != 0:
        return None
    return r.stdout.strip() or None


def _push_range_diff(before):
    """Diff the exact commits a push introduced: <before>...HEAD.

    Returns (diff, source), or ("", "") with the reason printed when the
    range cannot be determined — a bad range must fail loudly, never
    silently degrade to a tip-only inspection.
    """
    global _FAILURE_DETAIL
    if not before or set(before) == {"0"}:
        # A zero SHA is what GitHub sends for a branch-creation push:
        # there is no pre-push tip, so there is no range to diff.
        _FAILURE_DETAIL = (
            "  GITHUB_EVENT_BEFORE is the zero SHA (branch-creation push) —\n"
            "  there is no pre-push tip, so the push range cannot be\n"
            "  determined.")
        return "", ""
    if _rev_parse(before + "^{commit}") is None:
        _FAILURE_DETAIL = (
            f"  GITHUB_EVENT_BEFORE={before} does not resolve to a commit\n"
            "  in this checkout — the push range cannot be determined.")
        return "", ""
    result = _run_git(["diff", f"{before}...HEAD", "--", "."])
    if result is None or result.returncode != 0:
        _FAILURE_DETAIL = (
            f"  could not diff the push range {before}...HEAD.")
        return "", ""
    return result.stdout, f"push range {before[:12]}...HEAD (github.event.before)"


def _is_post_push_checkout():
    """True when the checkout sits exactly on origin/master with a clean
    tree — the shape a post-push CI checkout has: the push already
    updated origin/master, so origin/master...HEAD is empty by
    construction. (Callers only reach this after the working-tree and
    staged candidates came back empty, so the tree is clean here.)"""
    head = _rev_parse("HEAD")
    remote = _rev_parse("origin/master")
    return head is not None and remote is not None and head == remote


def get_diff(before=None):
    """Get the diff of added lines, plus a label naming the diff source.

    Returns ("", "") when no diff could be determined — the caller must
    treat that as a loud failure, never a vacuous pass. A non-empty
    source with an empty diff is a positively determined empty range
    (e.g. a push that introduced no new commits) — that one may pass.
    """
    global _FAILURE_DETAIL
    _FAILURE_DETAIL = ""
    if before is None:
        before = os.environ.get("GITHUB_EVENT_BEFORE") or None
    before = (before or "").strip() or None
    # Try vs origin/master first (for PRs), fall back to working tree,
    # then to the push range, then to the local tip-only fallback.
    # A shallow checkout (no history at all) yields nothing from any
    # source — that is a failure, not a skip.
    candidates = [
        ("origin/master...HEAD", ["git", "diff", "origin/master...HEAD", "--", "."]),
        ("working tree (unstaged)", ["git", "diff", "HEAD", "--", "."]),
        ("staged", ["git", "diff", "--cached", "--", "."]),
    ]
    for name, cmd in candidates:
        result = _run_git(cmd[1:])
        if result is not None and result.returncode == 0 and result.stdout.strip():
            return result.stdout, name
    # Sources 1-3 are all empty: this is either a post-push checkout
    # (push already updated origin/master; clean tree) or a clean local
    # checkout. The push range is authoritative here when available.
    if before is not None:
        return _push_range_diff(before)
    if _is_post_push_checkout():
        # A push introduced these commits, but the event did not tell us
        # the range — inspecting only the tip (the old HEAD~1 fallback)
        # would let every other pushed commit escape all five rules
        # (R-1108-91). Fail loudly instead, unless this is not a push
        # run at all: schedule / workflow_dispatch / local runs
        # introduce no push, so the tip inspection below is the
        # pre-existing backstop, not the enforcement point.
        if os.environ.get("GITHUB_EVENT_NAME") == "push":
            _FAILURE_DETAIL = (
                "  post-push checkout (HEAD == origin/master, clean tree)\n"
                "  but no push range is available: GITHUB_EVENT_BEFORE is\n"
                "  unset, so the pushed commits cannot be enumerated.\n"
                "  The workflow must pass GITHUB_EVENT_BEFORE\n"
                "  (${{ github.event.before }}) to the gate step.")
            return "", ""
    result = _run_git(["diff", "HEAD~1...HEAD", "--", "."])
    if result is not None and result.returncode == 0 and result.stdout.strip():
        return result.stdout, "HEAD~1...HEAD (local tip-only fallback)"
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

def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Fail if the diff introduces new bridges.")
    parser.add_argument(
        "--before", default=None,
        help="pre-push tip SHA (github.event.before); diffs "
             "<before>...HEAD on the master-push path. Overrides "
             "the GITHUB_EVENT_BEFORE environment variable.")
    args = parser.parse_args(argv)
    diff, source = get_diff(before=args.before)
    if not source:
        # Never pass vacuously: on the standard integration path (wt.py
        # integrate + push, no PR) every diff source above is empty only when
        # the checkout has no history to diff against, or when the push
        # range cannot be determined. Skipping here is exactly the
        # false-green hole this check exists to close.
        print("NO-BRIDGES CHECK FAILED:")
        if _FAILURE_DETAIL:
            print(_FAILURE_DETAIL)
        print("  could not determine any diff to inspect (no PR diff, no")
        print("  working-tree changes, no usable push range, and HEAD~1 is")
        print("  unavailable — is this a shallow checkout? CI must use")
        print("  fetch-depth: 0 and pass GITHUB_EVENT_BEFORE /")
        print("  GITHUB_EVENT_NAME to the gate step).")
        print("  Refusing to pass vacuously.")
        return 1
    if not diff.strip():
        # A source was positively determined and the range is empty
        # (e.g. a push that introduced no new commits): nothing to
        # inspect, and the emptiness itself was verified, not assumed.
        print(f"No-bridges check: {source} — no new commits, nothing to inspect.")
        print("No-bridges check passed.")
        return 0

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
