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

Diff sources (2026-10-30, P1-10 fix):
  Live channels, UNIONED — every one of them is inspected, never just
  the first non-empty one (the pre-fix bug: on a branch with a non-empty
  origin/master...HEAD diff, uncommitted and staged bridges were never
  seen, although the old docstring presented the order as fallbacks):
  1. origin/master...HEAD  (committed PR / feature-branch changes)
  2. git diff HEAD         (uncommitted changes to TRACKED files, staged
                            and unstaged)
  2b. untracked files      (git status --porcelain --untracked-files=all:
                            a brand-new file that has never been added is
                            invisible to every git diff above — a new
                            synth_*.go or panic stub sitting untracked was
                            a second silent skip of the same class as the
                            P1-10 bug, so untracked .go/.java files are
                            rendered as synthetic /dev/null diffs and
                            unioned in; --untracked-files=all lists every
                            file individually, including files inside a
                            brand-new directory (normal collapses such a
                            directory to one `?? sub/` entry the extension
                            filter skips — veto finding B1, 2026-10-30);
                            gitignored files are excluded by git itself)
  3. git diff --cached     (staged changes; a subset of 2 in practice,
                            kept so a staged-only change is inspected
                            even if a future git changes 2's semantics)
  Fallback, used ONLY when all three live channels are empty:
  4. HEAD~1...HEAD         (direct master push: wt.py integrate + push, no PR —
                           checkout lands exactly on origin/master, so 1-3 are
                           all empty; inspect what the push itself introduced)

  Source 4 is deliberately NOT unioned into every run: it is not a live
  channel, it is the already-landed tip commit. Unioning it always would
  re-litigate landed work on every local/PR run and could fail an
  unrelated change for a bridge the tip commit already carried past the
  gate (or predating it). The union-over-live-channels choice, rather
  than "fail loudly when sources differ", is forced by the channels'
  construction: 1, 2 and 3 are disjoint by design (committed vs
  uncommitted changes) and normally ALL differ, so failing on difference
  would make the gate unusable exactly in the branch-plus-dirty-tree
  case it must cover. Overlapping content (e.g. a staged line also
  present via 2) is deduplicated in the reported errors.

Panic-stub detection is multi-line as well as single-line: a
panic(fmt.Sprintf(\\n"not implemented...")) spans added lines, and the
old per-line regexes could not see across the newline. Java is covered
too: throw new UnsupportedOperationException("not implemented|...").
See check_no_bridges() section 3.
Known limitation: a stub whose opening line is pre-existing (a context line in the diff) with only its argument line added is not detected — the gate inspects added lines only.

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

def untracked_diffs():
    """Synthetic diffs for untracked .go/.java files, or "" if none.

    git diff never shows untracked files, so without this a new bridge
    file that has not been `git add`ed yet is invisible to the gate.
    Each file is rendered as a new-file diff (--- /dev/null) so the
    existing new-file / added-line checks apply unchanged. Only .go and
    .java files are rendered: those are the only extensions any check
    inspects, and rendering build outputs or notes would be noise.
    --untracked-files=all is required (not normal): normal collapses a
    brand-new directory to a single `?? sub/` entry, which the extension
    filter below skips, so files inside it were invisible (veto finding
    B1, 2026-10-30). With all, git lists every file individually, while
    gitignored files stay excluded by git itself.
    Returns "" when git status fails or no such file exists.
    """
    try:
        result = subprocess.run(
            ["git", "status", "--porcelain", "-z", "--untracked-files=all", "--", "."],
            capture_output=True, text=True, timeout=30)
    except Exception:
        return ""
    if result.returncode != 0:
        return ""
    parts = []
    for entry in result.stdout.split("\0"):
        if not entry.startswith("?? "):
            continue
        path = entry[3:]
        if not (path.endswith(".go") or path.endswith(".java")):
            continue
        try:
            with open(path, "r", encoding="utf-8", errors="replace") as f:
                content = f.read()
        except OSError:
            continue
        lines = content.split("\n")
        if lines and lines[-1] == "":
            lines.pop()
        parts.append("diff --git a/%s b/%s\n" % (path, path))
        parts.append("new file mode 100644\n")
        parts.append("--- /dev/null\n")
        parts.append("+++ b/%s\n" % path)
        parts.append("@@ -0,0 +1,%d @@\n" % len(lines))
        parts.extend("+" + line + "\n" for line in lines)
    return "".join(parts)


def get_diffs():
    """Get every available diff source as (label, text) pairs.

    Live channels (origin/master...HEAD, working tree, staged) are all
    returned when non-empty. The HEAD~1...HEAD fallback is returned only
    when every live channel is empty or unavailable — see the module
    docstring for why it is not unioned into every run. A source whose
    git command errors (e.g. origin/master does not exist in this clone)
    is unavailable, not empty; it contributes nothing, and the remaining
    sources are still inspected.

    Returns [] when no diff could be determined — the caller must treat
    that as a loud failure, never a vacuous pass.
    """
    live = [
        ("origin/master...HEAD", ["git", "diff", "origin/master...HEAD", "--", "."]),
        ("working tree", ["git", "diff", "HEAD", "--", "."]),
        ("staged", ["git", "diff", "--cached", "--", "."]),
    ]
    found = []
    for name, cmd in live:
        try:
            result = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
        except Exception:
            continue
        if result.returncode == 0 and result.stdout.strip():
            found.append((name, result.stdout))
    untracked = untracked_diffs()
    if untracked:
        found.append(("untracked", untracked))
    if found:
        return found
    # Fallback: direct master push with a clean tree. A shallow checkout
    # (no HEAD~1) yields nothing here — that is a failure, not a skip.
    try:
        result = subprocess.run(
            ["git", "diff", "HEAD~1...HEAD", "--", "."],
            capture_output=True, text=True, timeout=30)
    except Exception:
        return []
    if result.returncode == 0 and result.stdout.strip():
        return [("HEAD~1...HEAD (master push)", result.stdout)]
    return []


def get_diff():
    """Get the union of all available diffs, plus a label naming the sources.

    Returns ("", "") when no diff could be determined — the caller must
    treat that as a loud failure, never a vacuous pass.
    """
    found = get_diffs()
    if not found:
        return "", ""
    parts = []
    for _name, text in found:
        parts.append(text if text.endswith("\n") else text + "\n")
    label = " + ".join(name for name, _text in found)
    return "\n".join(parts), label


def added_line_runs(diff):
    """Maximal runs of consecutive added lines, as (file, [line, ...]).

    A run is broken by any non-added diff line (context, removal, hunk
    header, file header). This is what multi-line detection runs over:
    a newly added multi-line call appears in a diff as one consecutive
    run of added lines, so joining a run reconstructs the call. Joining
    all added lines of a file instead would splice unrelated hunks
    together and invent calls that do not exist.
    """
    runs = []
    current_file = None
    current_run = None
    for line in diff.split("\n"):
        if line.startswith("+++ b/"):
            current_file = line[6:]
            current_run = None
        elif line.startswith("+") and not line.startswith("+++"):
            if current_file is None:
                continue
            if current_run is None or current_run[0] != current_file:
                current_run = (current_file, [])
                runs.append(current_run)
            current_run[1].append(line[1:])
        else:
            current_run = None
    return [(fname, lines) for fname, lines in runs if lines]

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

    # 3. New panic stubs (Go) and not-implemented throws (Java). The old
    #    regex only caught literal "not implemented"/"unsupported"/"todo"
    #    messages; fail-fast stubs also hide behind bare panic(),
    #    panic(""), panic("unreachable"), and composed messages like
    #    panic(fmt.Sprintf("... not implemented")). Fail loud on all of
    #    them — a human decides whether an added panic is a legitimate
    #    invariant. P1-10: the per-line check alone misses multi-line
    #    calls (panic(fmt.Sprintf(\n"not implemented..."))) and all Java
    #    throws, so the same patterns are also run over each run of
    #    consecutive added lines with DOTALL; a multi-line hit is only
    #    reported when the match itself spans a newline, so single-line
    #    stubs are reported exactly once, by the per-line check.
    stub_keywords = (r"not implemented|unimplemented|not supported|unsupported|"
                     r"todo|FIXME|unreachable|NYI|not yet|tbd")
    panic_stub_res = [
        r'panic\s*\(\s*\)',            # bare panic() — nothing to go on
        r'panic\s*\(\s*""\s*\)',       # panic("") — nothing to go on
        r'panic\s*\(\s*"(' + stub_keywords + r')',
        # Stub keyword anywhere in the first string arg of a composed panic.
        r'panic\s*\(\s*(?:fmt\.Sprintf|fmt\.Errorf|errors\.New)\s*\(\s*"[^"]*(' + stub_keywords + r')',
        # Raw-string literal: panic(`not implemented ...`).
        r'panic\s*\(\s*`[^`]*(' + stub_keywords + r')',
    ]
    for fname, line in added_lines:
        if not fname or not fname.endswith(".go"):
            continue
        for pat in panic_stub_res:
            if re.search(pat, line, re.I):
                errors.append(f"PANIC STUB in {fname}: {line.strip()[:80]}")
                break
    # Multi-line Go panics: same patterns over each added-line run.
    for fname, lines in added_line_runs(diff):
        if not fname.endswith(".go"):
            continue
        run_text = "\n".join(lines)
        for pat in panic_stub_res:
            for m in re.finditer(pat, run_text, re.I | re.S):
                if "\n" in m.group(0):
                    first = lines[0].strip()[:80]
                    errors.append(f"PANIC STUB (multi-line) in {fname}: {first}")
                    break
            else:
                continue
            break
    # Java not-implemented throws: the exact analogue of a Go panic stub.
    # A bare or empty-message UnsupportedOperationException is a stub just
    # like bare panic(); a keyword message (or a String.format/MessageFormat
    # composed one) is a stub even when the throw spans lines.
    java_throw_res = [
        r'throw\s+new\s+UnsupportedOperationException\s*\(\s*\)',
        r'throw\s+new\s+UnsupportedOperationException\s*\(\s*""\s*\)',
        r'throw\s+new\s+UnsupportedOperationException\s*\(\s*"(' + stub_keywords + r')',
        r'throw\s+new\s+UnsupportedOperationException\s*\(\s*(?:String\.format|MessageFormat\.format)\s*\(\s*"[^"]*(' + stub_keywords + r')',
    ]
    for fname, line in added_lines:
        if not fname or not fname.endswith(".java"):
            continue
        for pat in java_throw_res:
            if re.search(pat, line, re.I):
                errors.append(f"UNSUPPORTED OPERATION STUB in {fname}: {line.strip()[:80]}")
                break
    for fname, lines in added_line_runs(diff):
        if not fname.endswith(".java"):
            continue
        run_text = "\n".join(lines)
        for pat in java_throw_res:
            for m in re.finditer(pat, run_text, re.I | re.S):
                if "\n" in m.group(0):
                    first = lines[0].strip()[:80]
                    errors.append(f"UNSUPPORTED OPERATION STUB (multi-line) in {fname}: {first}")
                    break
            else:
                continue
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
        if re.search(r'addFunc\s*\(|synthetic.*Package\s*\(|registerSynthetic', line):
            # Allow in existing synth files (grandfathered), flag in new contexts
            if "synth_" not in fname:
                # Adjudicated real implementations are not bridges.
                m = re.search(r'addFunc\s*\(\s*"([^"]+)"', line)
                if m and any(f_sub in fname and sym == m.group(1)
                             for f_sub, sym in ADJUDICATED_REGISTRATIONS):
                    continue
                errors.append(f"SYNTHETIC REGISTRATION in {fname}: {line.strip()[:80]}")

    # The unioned diff can legitimately contain the same added line twice
    # (e.g. a staged line via both the working-tree and staged sources, or
    # a branch-tip line via both the branch and fallback sources). Report
    # each distinct error once, in first-seen order.
    seen = set()
    deduped = []
    for e in errors:
        if e not in seen:
            seen.add(e)
            deduped.append(e)
    return deduped

def main():
    diff, source = get_diff()
    if not diff.strip():
        # Never pass vacuously: on the standard integration path (wt.py
        # integrate + push, no PR) every diff source above is empty only when
        # the checkout has no history to diff against. Skipping here is
        # exactly the false-green hole this check exists to close.
        print("NO-BRIDGES CHECK FAILED:")
        print("  could not determine any diff to inspect (no PR diff, no")
        print("  working-tree, staged, or untracked changes, and HEAD~1 is")
        print("  unavailable — is this a shallow checkout?")
        print("  CI must use fetch-depth: 0).")
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
