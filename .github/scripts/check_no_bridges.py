#!/usr/bin/env python3
"""
No-bridges check (2026-10-15, his direct order).

Fails if the diff introduces new bridges:
- New synth_*.go files
- New package-name conditionals in frontend/backend, in every spelling the
  tree uses: Go ==/!= and case, Java .equals/.equalsIgnoreCase (including
  getName().equals and the "pkg".equals Yoda form) and Objects.equals
- New panic stubs (Go) and new Java stub throws: a bare
  UnsupportedOperationException, or any UnsupportedOperationException /
  RuntimeException / IllegalStateException whose message carries a stub
  keyword (not implemented, not supported, todo, ...)
- New wire node kinds: EVERY new e.node("kind") in any Go file and every
  new Java registerExprKind/registerStmtKind("kind"). The kind namespace
  IS the bridge surface (a new kind needs a factory counterpart to mean
  anything), so every new kind is flagged and a human adjudicates — only
  the ADJUDICATED_* entries below pass. This deliberately includes kinds
  a future language feature might add: a new wire kind always deserves a
  reviewer's eyes, and adjudication is one entry, not a redesign.
- New synthetic function registrations
- New syntheticSources map entries (gofrontend path): an entry added to
  the map IS a new synthetic package even though no new file appears and
  no addFunc call is made. A modified entry (same key, new value) or a
  removed entry (retirement) is not flagged.
- New frozen synthetic packages in the p3frontend path (the analogue of
  syntheticSources there — p3frontend emits no wire kinds, it hands
  merged source to the frozen gofrontend service, so its registration
  surface is the frozen-synthetic case lists): a new case arm in
  importer.go's synthetic switch, or a new string in loader.go's
  kindSynthetic case list.

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

Known static-analysis limits (documented, not hidden):
- A wire kind passed as a variable, not a string literal, is not seen.
- A Java stub message held in a variable, or a stub keyword in a later
  string of a composed message, is not seen (only the first string
  literal after the open paren is inspected).
- A syntheticSources entry whose value does not start with "synthetic"
  is not seen as a map entry (it would still need its source variable,
  which check 5 flags outside synth_ files).
- The p3frontend checks are hunk-scoped: a case arm is flagged only when
  its hunk also mentions a synthetic*Package call (importer.go) or
  kindSynthetic (loader.go). A hunk that adds both a case arm and an
  unrelated synthetic*Package mention in the same hunk over-flags; a
  human adjudicates.

Positive controls: .github/scripts/test_check_no_bridges.py runs one
must-fail fixture per bypass above (plus negatives and old-behavior
regressions). If a fixture ever passes, the gate has regressed — fix
the gate, never delete the fixture.
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
# Java factory registrations of an adjudicated kind are the same real
# implementation, not a new bridge. Derived from ADJUDICATED_WIRE_NODES —
# there is deliberately NO separate Java allowlist to widen.
ADJUDICATED_JAVA_KINDS = {kind for _, kind in ADJUDICATED_WIRE_NODES}

STDLIB_PKGS = ["fmt", "sync", "os", "net", "http", "runtime", "reflect", "time",
               "strings", "bytes", "strconv", "sort", "math", "encoding", "crypto",
               "syscall", "unsafe", "io", "bufio", "context", "errors"]

# The gofrontend synthetic-package map and the p3frontend frozen-synthetic
# registrations. Paths are exact: these are single-definition sites.
SYNTH_MAP_FILE = "internal/frontend/synth_machinery.go"
P3_IMPORTER_FILE = "internal/frontend/p3frontend/importer.go"
P3_LOADER_FILE = "internal/frontend/p3frontend/loader.go"

# A syntheticSources entry line: `"key": synthetic<Something>...`. Commented
# lines (retired entries) do not match: the first non-space char must be `"`.
# The value prefix "synthetic" (not the full "synthetic*Source") also covers
# concatenated values ("crypto/cipher": syntheticCipherSource + ...GcmSource).
SYNTH_MAP_ENTRY_RE = re.compile(r'^\s*"([^"]+)"\s*:\s*synthetic\w+')
# A runtime registration through the map: syntheticSources["k"] = ... or
# syntheticSources[v] = ... . The (?! =) keeps == comparisons (reads) out.
SYNTH_MAP_ASSIGN_RE = re.compile(r'syntheticSources\s*\[[^\]]+\]\s*=(?!=)')


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


def parse_diff(diff):
    """Split a unified diff into added lines, removed lines, new files, hunks.

    Returns (added, removed, new_files, hunks):
    - added/removed: lists of (filename, line-text) in diff order.
    - new_files: filenames whose --- line is /dev/null (genuinely new files).
    - hunks: lists of (filename, [(prefix, text)]) with prefix in "+- ".
      A hunk's lines are in diff order, context lines included, so
      hunk-scoped checks can see what an added line sits next to.
    """
    added, removed, new_files, hunks = [], [], set(), []
    current_file = None
    current_hunk = None
    prev_was_devnull = False
    for line in diff.split("\n"):
        if line.startswith("--- /dev/null"):
            prev_was_devnull = True
            current_hunk = None
        elif line.startswith("+++ b/"):
            current_file = line[6:]
            if prev_was_devnull:
                new_files.add(current_file)
            prev_was_devnull = False
            current_hunk = None
        elif line.startswith("--- "):
            prev_was_devnull = False
            current_hunk = None
        elif line.startswith("@@"):
            current_hunk = (current_file, [])
            hunks.append(current_hunk)
        elif line.startswith("+") and not line.startswith("+++"):
            text = line[1:]
            added.append((current_file, text))
            if current_hunk is not None:
                current_hunk[1].append(("+", text))
        elif line.startswith("-") and not line.startswith("---"):
            text = line[1:]
            removed.append((current_file, text))
            if current_hunk is not None:
                current_hunk[1].append(("-", text))
        elif line.startswith(" "):
            if current_hunk is not None:
                current_hunk[1].append((" ", line[1:]))
        # "\ No newline at end of file" and anything else: ignored.
    return added, removed, new_files, hunks


def _is_test_file(fname):
    # Test files and docs. ("_test." covers *_test.go harnesses, e.g.
    # drift-test importers that resolve synthetic import paths like the
    # pre-existing `if path == "bytes"` stubs; "/test/" covers Java
    # src/test trees.)
    return ("/test" in fname or "test_" in fname or "_test." in fname
            or fname.endswith("Test.java"))


def _quoted_strings(text):
    return re.findall(r'"([^"]+)"', text)


def check_no_bridges(diff):
    errors = []
    added_lines, removed_lines, new_files, hunks = parse_diff(diff)

    # 1. New synth_*.go files. Only genuinely NEW files count; modifying a
    #    grandfathered synth_*.go is not a new bridge — without this, every
    #    master push touching a synthetic file would fail the gate.
    for f in sorted(new_files):
        if "synth_" in f and f.endswith(".go"):
            errors.append(f"NEW SYNTHETIC FILE: {f} — synthetic stdlib packages are banned")

    # 2. Package-name conditionals (stdlib names), every spelling in use.
    #    Go: ==, !=, case, Name ==. Java: pkg.equals("x") (which also covers
    #    getName().equals("x") and negated !pkg.equals("x")), "x".equals(pkg),
    #    Objects.equals(a, "x"), equalsIgnoreCase variants.
    for fname, line in added_lines:
        if not fname or not (fname.endswith(".go") or fname.endswith(".java")):
            continue
        if _is_test_file(fname):
            continue
        for pkg in STDLIB_PKGS:
            patterns = [
                rf'==\s*"{pkg}"',
                rf'!=\s*"{pkg}"',
                rf'case\s*"{pkg}"',
                rf'Name\s*==\s*"{pkg}"',
                rf'\.equals\s*\(\s*"{pkg}"\s*\)',
                rf'\.equalsIgnoreCase\s*\(\s*"{pkg}"\s*\)',
                rf'"{pkg}"\s*\.equals\s*\(',
                rf'Objects\.equals\s*\([^)]*"{pkg}"',
            ]
            for pat in patterns:
                if re.search(pat, line):
                    errors.append(f"PACKAGE-NAME CONDITIONAL in {fname}: {line.strip()[:80]}")
                    break
            else:
                continue
            break

    # 3. New panic stubs (Go) and new Java stub throws.
    #    Go: the old regex only caught literal "not implemented"/
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

    #    Java: the backend's stub idiom is throw new
    #    UnsupportedOperationException("...not implemented"), which the Go-only
    #    check above passed untouched. Flag it, plus the bare no-arg form
    #    (nothing to go on, mirroring bare panic()) and the same stub
    #    keywords in the other fail-fast throw types. Legitimate throws
    #    ("not an integer op", "cannot read ... as long") carry no stub
    #    keyword and pass. Added lines are joined per file so a throw whose
    #    message sits on the next line is still seen.
    #    The "stub" keyword applies to UnsupportedOperationException only
    #    (the backend Makefiles' own marker: grep for
    #    'throw new UnsupportedOperationException("stub'). An
    #    IllegalStateException saying a "stub" class was loaded instead of
    #    the real overlay (acbb849b's classpath-order fail-fast) is a
    #    legitimate guard, not a stub — so "stub" is NOT a keyword for
    #    RuntimeException/IllegalStateException.
    java_stub_keywords = r'not implemented|unimplemented|not supported|unsupported|todo|FIXME|unreachable|NYI|not yet'
    java_stub_res = [
        r'throw\s+new\s+UnsupportedOperationException\s*\(\s*\)',
        r'throw\s+new\s+UnsupportedOperationException\s*\(\s*"[^"]*(?:' + java_stub_keywords + r'|stub)[^"]*"',
        r'throw\s+new\s+(?:RuntimeException|IllegalStateException)\s*\(\s*"[^"]*(?:' + java_stub_keywords + r')[^"]*"',
    ]
    java_added = {}
    for fname, line in added_lines:
        if fname and fname.endswith(".java") and not _is_test_file(fname):
            java_added.setdefault(fname, []).append(line)
    for fname, lines in java_added.items():
        text = "\n".join(lines)
        for pat in java_stub_res:
            if re.search(pat, text, re.I):
                snippet = next((ln.strip()[:80] for ln in lines
                                if re.search(r'throw\s+new\s+(?:UnsupportedOperationException|RuntimeException|IllegalStateException)', ln)),
                               lines[0].strip()[:80] if lines else "")
                errors.append(f"JAVA STUB THROW in {fname}: {snippet}")
                break

    # 4. New wire node kinds — every kind, every registration site.
    #    Go: any .node("kind") call in any Go file (export.go is no longer
    #    the only emitter: chanops_m5.go, mrdesugar_m5.go, synth_*.go,
    #    unsafe_derive.go and v2/emit/nodes.go all emit kinds). The old
    #    runtime|fmt|sync|os|net prefix filter passed reflect…/strings…/
    #    time…/crypto… kinds untouched.
    #    Java: any registerExprKind/registerStmtKind("kind") call (the
    #    factory hooks in P3KindRegistrations.java and friends).
    #    Only ADJUDICATED_* entries pass, and Go adjudication stays
    #    file-scoped: the same kind in another file is still flagged.
    for fname, line in added_lines:
        if not fname or not fname.endswith(".go") or "_test." in fname:
            continue
        m = re.search(r'\.node\s*\(\s*"([^"]+)"', line)
        if m:
            node_kind = m.group(1)
            if any(f_sub in fname and nk == node_kind
                   for f_sub, nk in ADJUDICATED_WIRE_NODES):
                continue
            errors.append(f"NEW WIRE NODE KIND in {fname}: {node_kind}")
    for fname, line in added_lines:
        if not fname or not fname.endswith(".java") or _is_test_file(fname):
            continue
        m = re.search(r'register(?:Expr|Stmt)Kind\s*\(\s*"([^"]+)"', line)
        if m:
            node_kind = m.group(1)
            if node_kind in ADJUDICATED_JAVA_KINDS:
                continue
            errors.append(f"NEW WIRE NODE KIND (Java factory) in {fname}: {node_kind}")

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

    # 6. New syntheticSources map entries (gofrontend path).
    #    Adding an entry creates a new synthetic package with neither a new
    #    file (check 1) nor an addFunc call (check 5). Only additions count:
    #    a key present in both added and removed lines is a modified entry,
    #    a key only in removed lines is a retirement. A runtime index
    #    assignment (syntheticSources["k"] = ...) is the same registration
    #    through another spelling and is flagged outright.
    synth_added_keys = set()
    synth_removed_keys = set()
    for fname, line in added_lines:
        if fname == SYNTH_MAP_FILE:
            m = SYNTH_MAP_ENTRY_RE.match(line)
            if m:
                synth_added_keys.add(m.group(1))
    for fname, line in removed_lines:
        if fname == SYNTH_MAP_FILE:
            m = SYNTH_MAP_ENTRY_RE.match(line)
            if m:
                synth_removed_keys.add(m.group(1))
    for key in sorted(synth_added_keys - synth_removed_keys):
        errors.append(f"NEW SYNTHETIC PACKAGE (syntheticSources) in {SYNTH_MAP_FILE}: \"{key}\" — synthetic stdlib packages are banned")
    for fname, line in added_lines:
        if fname and fname.endswith(".go") and "_test." not in fname:
            if SYNTH_MAP_ASSIGN_RE.search(line):
                errors.append(f"NEW SYNTHETIC PACKAGE (syntheticSources assignment) in {fname}: {line.strip()[:80]}")

    # 7. New frozen synthetic packages in the p3frontend path.
    #    importer.go: a new case arm in the synthetic switch — detected
    #    hunk-scoped (the hunk must also mention a synthetic*Package call,
    #    which is what the switch arms do). loader.go: a new string in the
    #    kindSynthetic case list — detected hunk-scoped (the hunk must
    #    mention kindSynthetic). Added-minus-removed keeps edits and
    #    retirements from flagging.
    for fname, hunk_lines in hunks:
        if fname == P3_IMPORTER_FILE:
            hunk_text = "\n".join(text for _, text in hunk_lines)
            if not re.search(r'synthetic\w+Package\s*\(', hunk_text):
                continue
            added_cases, removed_cases = set(), set()
            for prefix, text in hunk_lines:
                # A case line ends with ":" only on the LAST line of a
                # multi-line list; every line starts with "case " and
                # carries quoted strings, so match on that, not on ":".
                if text.strip().startswith("case "):
                    keys = _quoted_strings(text)
                    if prefix == "+":
                        added_cases.update(keys)
                    elif prefix == "-":
                        removed_cases.update(keys)
            for key in sorted(added_cases - removed_cases):
                errors.append(f"NEW FROZEN SYNTHETIC PACKAGE (p3frontend importer) in {fname}: \"{key}\" — synthetic stdlib packages are banned")
        elif fname == P3_LOADER_FILE:
            hunk_text = "\n".join(text for _, text in hunk_lines)
            if "kindSynthetic" not in hunk_text:
                continue
            added_strs, removed_strs = set(), set()
            for prefix, text in hunk_lines:
                if prefix not in ("+", "-"):
                    continue
                stripped = text.strip()
                if stripped.startswith("case ") or stripped.startswith('"'):
                    keys = _quoted_strings(stripped)
                else:
                    continue
                if prefix == "+":
                    added_strs.update(keys)
                else:
                    removed_strs.update(keys)
            for key in sorted(added_strs - removed_strs):
                errors.append(f"NEW FROZEN SYNTHETIC PACKAGE (p3frontend loader) in {fname}: \"{key}\" — synthetic stdlib packages are banned")

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
