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

    # 3c. Java UnsupportedOperationException stubs. Checks #3/#3b only scan
    # .go added lines; a newly added `throw new UnsupportedOperationException`
    # fail-fast stub in backend Java would pass the gate silently. Fail loud
    # on added .java lines that throw UOE with no message (nothing to go on —
    # the Java analog of #3's bare-panic() rule) or whose line carries a stub
    # keyword (same keyword set as #3/#3b: literal message, composed message,
    # or trailing comment — a human adjudicates). Grandfathered sites are
    # pre-existing lines, never in a diff, inventoried in
    # docs/arch/bridge-inventory.md ("Adjudicated non-bridges": 53 sites at
    # master 56a3dd68). Scope notes, deliberate and mirroring #3: a UOE with
    # a specific non-keyword message (e.g. "send on closed channel") is NOT
    # flagged — specific messages are legitimate errors, not stubs. A throw
    # whose message sits on the following line is invisible to this per-line
    # scan, the same limitation #3 has for Go.
    java_stub_kw = r"(not implemented|unimplemented|not supported|unsupported|todo|FIXME|unreachable|NYI|not yet|tbd)"
    java_uoe_res = [
        # Bare throw — nothing to go on.
        r"throw\s+new\s+(?:java\.lang\.)?UnsupportedOperationException\s*\(\s*\)",
        # Throw whose line carries a stub keyword.
        rf"throw\s+new\s+(?:java\.lang\.)?UnsupportedOperationException\b.*{java_stub_kw}",
    ]
    for fname, line in added_lines:
        if not fname or not fname.endswith(".java"):
            continue
        for pat in java_uoe_res:
            if re.search(pat, line, re.I):
                errors.append(f"JAVA STUB in {fname}: {line.strip()[:80]}")
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

    return errors

def _self_test():
    """Positive/negative controls for the no-bridges checks.

    Runs synthetic diffs through check_no_bridges() in-process: no repo I/O,
    no network, no diff-source probing (main()'s never-pass-vacuously path
    is untouched). Each case asserts the gate fails (or passes) as intended.
    A control that stops failing fails LOUD — a silently-passing gate is
    worse than no gate. Run: python3 .github/scripts/check_no_bridges.py
    --self-test
    """
    java_file = "internal/backend/truffle/src/main/java/graalgo/runtime/Foo.java"

    def diff_for(fname, *added):
        lines = ["diff --git a/%s b/%s" % (fname, fname),
                 "new file mode 100644",
                 "--- /dev/null",
                 "+++ b/%s" % fname,
                 "@@ -0,0 +1,%d @@" % len(added)]
        lines += ["+" + l for l in added]
        return "\n".join(lines) + "\n"

    cases = [
        # (name, diff, expect_fail)
        ("3c positive: java UOE stub, literal keyword message",
         diff_for(java_file,
                  "public final class Foo {",
                  '        throw new UnsupportedOperationException("not implemented");',
                  "}"),
         True),
        ("3c positive: java UOE stub, bare throw",
         diff_for(java_file,
                  "public final class Foo {",
                  "        throw new UnsupportedOperationException();",
                  "}"),
         True),
        ("3c positive: java UOE stub, composed message with keyword",
         diff_for(java_file,
                  "public final class Foo {",
                  '        throw new UnsupportedOperationException("op " + name + " not supported");',
                  "}"),
         True),
        ("3c positive: java UOE stub, switch-default guard with keyword",
         diff_for(java_file,
                  "public final class Foo {",
                  '            default -> throw new UnsupportedOperationException("NYI: " + kind);',
                  "}"),
         True),
        ("3c negative: java UOE with a specific non-keyword message is benign",
         diff_for(java_file,
                  "public final class Foo {",
                  '        throw new UnsupportedOperationException("send on closed channel");',
                  "}"),
         False),
        ("3c negative: grandfathered kind-guard shape (no keyword) passes",
         diff_for("internal/backend/truffle/src/main/java/graalgo/frontend/NewNode.java",
                  "public final class NewNode extends GoExprNode {",
                  '            default -> throw new UnsupportedOperationException("not an integer op: " + op);',
                  "}"),
         False),
        ("3c negative: ordinary java addition is untouched",
         diff_for(java_file,
                  "public final class Foo {",
                  "        return System.nanoTime();",
                  "}"),
         False),
        # Regression: the Go checks still behave.
        ("3 positive: go panic stub still caught",
         diff_for("internal/frontend/export.go",
                  "package frontend",
                  '        panic("not implemented")'),
         True),
        ("3 negative: go panic with a specific message still passes",
         diff_for("internal/frontend/export.go",
                  "package frontend",
                  '        panic("send on closed channel")'),
         False),
    ]
    failures = []
    for name, diff, expect_fail in cases:
        errors = check_no_bridges(diff)
        failed = bool(errors)
        ok = (failed == expect_fail)
        print(("PASS " if ok else "FAIL ") + name)
        if not ok:
            print("   expected %s, got errors=%r"
                  % ("fail" if expect_fail else "pass", errors))
            failures.append(name)
    if failures:
        print("SELF-TEST FAILED: %d/%d controls broken"
              % (len(failures), len(cases)))
        return 1
    print("SELF-TEST PASSED: %d/%d controls hold" % (len(cases), len(cases)))
    return 0

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
    if "--self-test" in sys.argv:
        sys.exit(_self_test())
    sys.exit(main())
