#!/usr/bin/env python3
"""
No-bridges check (2026-10-15, his direct order).

Fails if the diff introduces new bridges:
- New synth_*.go files
- New package-name conditionals in frontend/backend
- New panic stubs
- New wire node kinds for stdlib functions
- New synthetic function registrations (Rule 5: addFunc /
  synthetic*Package() / registerSynthetic calls, plus Rule 5b:
  map-literal entries in the syntheticSources registry
  (internal/frontend/synth_machinery.go) and the
  dedicatedSyntheticStubs registry (internal/frontend/export.go))

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

Diff sources — UNIONED, never first-match (2026-11-10, L12-P1-B):
first-match shadowing let a stale origin/master mask the working tree:
in a lane worktree origin/master lags HEAD by the commits landed since
the worktree was claimed, so `origin/master...HEAD` returns those
already-merged commits' diff, the loop broke, and the working-tree diff
— the change actually under review — was never inspected. A planted
`syntheticSources` entry in internal/frontend/synth_machinery.go
escaped Rule 5b entirely on a live run for exactly this reason. Every
non-empty source below is therefore concatenated and judged together;
one added line appearing in two sources is reported once (dedupe in
check_no_bridges), and the run labels every source that contributed.
  1. origin/master...HEAD  (PR / feature branch)
  2. git diff HEAD         (uncommitted working-tree changes)
  3. git diff --cached     (staged changes)
  4. HEAD~1...HEAD         (direct master push: wt.py integrate + push, no PR —
                           checkout lands exactly on origin/master, so 1-3 are
                           all empty; inspect what the push itself introduced)
  5. untracked .go/.java files, synthesized as a diff against /dev/null
     and APPENDED to whichever of 1-4 fired (or returned alone when none
     did): `git diff` never shows untracked files, so without this a new
     synthetic package sitting uncommitted in the working tree was
     invisible to every rule — Rule 1 could never fire on it.
Note the union judges committed-range lines too: if a stale
origin/master makes the gate flag already-merged lines, refresh it
(`git fetch origin` — origin is the self-remote) and re-run; the range
then empties. That noise is fail-loud by design — a tripwire must never
silently skip a source again.

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

def _untracked_source_diff(cwd=None):
    """Synthesized diff for untracked .go/.java source files.

    `git diff` never shows untracked files, so a new synthetic package
    (or a new package-name conditional) sitting uncommitted in the
    working tree was invisible to every rule. Each untracked source
    file is diffed against /dev/null, which yields the same header
    shape (`--- /dev/null`, `+++ b/<path>`) the rules already parse —
    Rule 1 fires on a new untracked synth_*.go file exactly as it does
    for a committed one. Only non-ignored files appear in
    `git status --porcelain` (build debris is gitignored), and only
    .go/.java files are synthesized (the rules' scope). Returns "" when
    there are no untracked source files.
    """
    try:
        status = subprocess.run(
            ["git", "status", "--porcelain", "-z",
             "--untracked-files=all"],
            capture_output=True, text=True, timeout=30, cwd=cwd)
    except Exception:
        return ""
    if status.returncode != 0:
        return ""
    chunks = []
    for entry in status.stdout.split("\0"):
        if not entry.startswith("?? "):
            continue
        path = entry[3:]
        if not (path.endswith(".go") or path.endswith(".java")):
            continue
        try:
            diff = subprocess.run(
                ["git", "diff", "--no-index", "/dev/null", "--", path],
                capture_output=True, text=True, timeout=30, cwd=cwd)
        except Exception:
            continue
        # --no-index exits 1 when the files differ (always, here);
        # its stdout is the synthesized new-file diff.
        if diff.stdout.strip():
            chunks.append(diff.stdout)
    return "".join(chunks)


def get_diff(cwd=None):
    """Get the diff of added lines, plus a label naming the diff source.

    Returns ("", "") when no diff could be determined — the caller must
    treat that as a loud failure, never a vacuous pass.
    """
    # Union of every diff source (see the module docstring): first-match
    # shadowing let a stale origin/master mask the working tree entirely
    # (2026-11-10, L12-P1-B). A shallow checkout (no HEAD~1) yields
    # nothing from the last source — that is a failure, not a skip.
    candidates = [
        ("origin/master...HEAD", ["git", "diff", "origin/master...HEAD", "--", "."]),
        ("working tree (unstaged)", ["git", "diff", "HEAD", "--", "."]),
        ("staged", ["git", "diff", "--cached", "--", "."]),
        ("HEAD~1...HEAD (master push)", ["git", "diff", "HEAD~1...HEAD", "--", "."]),
    ]
    diffs, sources = [], []
    for name, cmd in candidates:
        try:
            result = subprocess.run(cmd, capture_output=True, text=True,
                                  timeout=30, cwd=cwd)
        except Exception:
            continue
        if result.returncode == 0 and result.stdout.strip():
            diffs.append(result.stdout)
            sources.append(name)
    # Untracked source files are invisible to every candidate above;
    # append their synthesized diff so the rules judge them too.
    untracked = _untracked_source_diff(cwd)
    if untracked:
        diffs.append(untracked)
        sources.append("untracked .go/.java files")
    if not diffs:
        return "", ""
    return "".join(diffs), " + ".join(sources)

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

    # 5b. New synthetic-registry map entries (REVIEW-2026-11-09-12,
    # N-P1-6). New synthetic packages are registered by adding a
    # map-literal entry, e.g. `"time": syntheticTimeSource,` in the
    # `syntheticSources` registry
    # (internal/frontend/synth_machinery.go) — a line none of Rule 5's
    # three patterns matches (no addFunc/synthetic*Package(/registerSynthetic,
    # no `==`/case, and the pure-string-line continuation regex
    # requires the line to be only quoted strings, which the trailing
    # source ident disqualifies). Rule 1 only covers NEW synth_*.go
    # files, so re-activating a RETIRED entry (the commented-out
    # `"reflect"` lines) or adding an entry backed by a source const
    # in an existing file escaped entirely. The same hole exists in
    # the sibling registry `dedicatedSyntheticStubs`
    # (internal/frontend/export.go: `"runtime": syntheticRuntimePackage,`)
    # — a bare `synthetic*Package` ident, no call parens, so Rule 5
    # misses it too. Both registries are covered by one mechanism:
    # an added map-entry line in either registry file whose value is
    # the builder for a package. Entry shapes:
    #   syntheticSources:       `"path": <Ident>Source[, + ...],`
    #                           (values are source consts, optionally
    #                           concatenated, e.g. crypto/cipher)
    #   dedicatedSyntheticStubs: `"path": synthetic<Name>Package,`
    #                           (bare builder-func ident)
    # The shapes are unambiguous in their files: the only other
    # string-keyed map in synth_machinery.go carries quoted string
    # values, and dedicatedSyntheticStubs is the only
    # map[string]func() *types.Package literal in export.go. A
    # commented-out entry (`// "reflect": ...`) is still retired and
    # does not flag (the pattern anchors at the first `"`).
    # Narrowing-aware like Rule 2: an added entry whose key was
    # removed in the SAME hunk is a rewrap/reorder, not a new
    # registration. Known limitation, by design (same as Rule 2): a
    # move across hunks still flags — the gate is a heuristic
    # tripwire backed by human review, not a proof.
    _registry_entry_res = {
        "internal/frontend/synth_machinery.go": (
            "syntheticSources",
            re.compile(r'^\s*"([^"]+)"\s*:\s*[A-Za-z_][A-Za-z0-9_]*Source\b[^,]*,\s*(//.*)?$')),
        "internal/frontend/export.go": (
            "dedicatedSyntheticStubs",
            re.compile(r'^\s*"([^"]+)"\s*:\s*synthetic[A-Za-z0-9_]*Package\s*,\s*(//.*)?$')),
    }
    for fname, ordered in hunks:
        if fname not in _registry_entry_res:
            continue
        registry, entry_re = _registry_entry_res[fname]
        removed_keys = set()
        added_entries = []
        for status, line in ordered:
            m = entry_re.match(line)
            if not m:
                continue
            if status == "-":
                removed_keys.add(m.group(1))
            elif status == "+":
                added_entries.append((line, m.group(1)))
        for line, key in added_entries:
            if key in removed_keys:
                continue
            errors.append(
                f"SYNTHETIC REGISTRATION in {fname}: {registry} entry "
                f"for {key!r} — {line.strip()[:80]}")

    # Dedupe, order-preserving: get_diff() unions several diff sources,
    # so one added line can appear twice (e.g. committed on the branch
    # and touched again unstaged) — report it once, not twice.
    seen = set()
    unique = []
    for e in errors:
        if e not in seen:
            seen.add(e)
            unique.append(e)
    return unique

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
