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

def _pkg_name_patterns(pkg):
    """Regexes matching package-name-keyed conditionals for one stdlib pkg.

    Covers Go forms (==, case, Name ==, strings.HasPrefix/HasSuffix/Contains)
    and Java forms (.equals, reversed .equals, .startsWith, .contains).

    The package-name boundary (?=["./]) requires the char after the name to
    be the end of the literal, a qualified-symbol dot ("sync.Pool"), or an
    import-path slash ("sync/atomic") — so words that merely start with the
    package name ("synchronous", "osmand") do not match.
    """
    b = r'(?=["./])'  # package-name boundary inside the string literal
    return [
        rf'==\s*"{pkg}"',                        # Go:  if pkg == "fmt"
        rf'case\s*"{pkg}"',                       # Go/Java: case "sync":
        rf'Name\s*==\s*"{pkg}"',                  # Go:  pkg.Name == "os"
        # Java name-comparison forms. (2026-10-18, review P1-3: the gate
        # previously matched NONE of these — the vetoed isOpaquePrimitive
        # carve-out dodged it via .equals("sync.P…") and only the human
        # reviewer caught it.)
        rf'\.equals\s*\(\s*"{pkg}{b}',            # x.equals("sync.Pool")
        rf'"{pkg}{b}[^"]*"\s*\.\s*equals\s*\(',   # "sync.Pool".equals(x)
        rf'\.startsWith\s*\(\s*"{pkg}{b}',        # p.startsWith("sync/atomic.")
        rf'\.contains\s*\(\s*"{pkg}{b}',          # n.contains("sync")
        # Go name-comparison forms:
        rf'strings\.(?:HasPrefix|HasSuffix|Contains)\s*\([^,)]*,\s*"{pkg}{b}',
    ]


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
        # Match Go and Java name-comparison forms against stdlib package
        # names; see _pkg_name_patterns for the full form list.
        for pkg in stdlib_pkgs:
            for pat in _pkg_name_patterns(pkg):
                if re.search(pat, line):
                    # Allow in test files and docs. ("_test." covers
                    # *_test.go harnesses, e.g. drift-test importers that
                    # resolve synthetic import paths like the pre-existing
                    # `if path == "bytes"` stubs; "Test.java" covers Java
                    # *Test.java harnesses.)
                    if ("/test" not in fname and "test_" not in fname
                            and "_test." not in fname
                            and not fname.endswith("Test.java")):
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


def _self_test_cases():
    """Positive/negative controls for the no-bridges gate.

    Each entry: (name, file, added line, expect_fail). Positive controls
    prove the gate catches bridge shapes (including the vetoed
    isOpaquePrimitive .equals("sync.P…") carve-out from review P1-3);
    negative controls prove it stays silent on legitimate code. Every
    control must be capable of failing in both directions — a control
    that cannot fail is deleted.
    """
    return [
        # --- positive: Java name-comparison forms (review P1-3) ---
        ("java-equals-dotted",
         "internal/backend/foo/Bar.java",
         'if (typeName.equals("sync.Pool")) { return true; }',
         True),
        ("java-equals-exact",
         "internal/backend/foo/Bar.java",
         'if (name.equals("sync")) { return true; }',
         True),
        ("java-equals-reversed",
         "internal/backend/foo/Bar.java",
         'if ("sync.Pool".equals(typeName)) { return true; }',
         True),
        ("java-startswith",
         "internal/backend/foo/Bar.java",
         'if (path.startsWith("sync/atomic.")) { return true; }',
         True),
        ("java-contains",
         "internal/backend/foo/Bar.java",
         'if (names.contains("sync")) { return true; }',
         True),
        # --- positive: Go forms (old and new) ---
        ("go-eq",
         "internal/frontend/foo/bar.go",
         'if pkg == "sync" { return true }',
         True),
        ("go-case",
         "internal/frontend/foo/bar.go",
         '\tcase "fmt":',
         True),
        ("go-name-eq",
         "internal/frontend/foo/bar.go",
         'if imp.Name == "os" { return true }',
         True),
        ("go-hasprefix",
         "internal/frontend/foo/bar.go",
         'if strings.HasPrefix(path, "sync/") { return true }',
         True),
        ("go-contains",
         "internal/frontend/foo/bar.go",
         'if strings.Contains(name, "sync") { return true }',
         True),
        # --- negative: legitimate code must not trip the gate ---
        ("java-word-prefix",
         "internal/backend/foo/Bar.java",
         'if (mode.equals("synchronous")) { return true; }',
         False),
        ("java-other-literal",
         "internal/backend/foo/Bar.java",
         'if (kind.equals("osmand")) { return true; }',
         False),
        ("java-startswith-word",
         "internal/backend/foo/Bar.java",
         'if (s.startsWith("synchronized")) { return true; }',
         False),
        ("java-plain-string",
         "internal/backend/foo/Bar.java",
         'log.info("sync.Pool fast path enabled");',
         False),
        ("go-word-prefix",
         "internal/frontend/foo/bar.go",
         'if strings.HasPrefix(p, "synchronous") { return true }',
         False),
        ("go-plain-string",
         "internal/frontend/foo/bar.go",
         'fmt.Println("syncing caches")',
         False),
        # --- negative: test files are excluded ---
        ("java-test-excluded",
         "internal/backend/foo/BarTest.java",
         'if (typeName.equals("sync.Pool")) { return true; }',
         False),
        ("go-test-excluded",
         "internal/frontend/foo/bar_test.go",
         'if pkg == "sync" { return true }',
         False),
    ]


def run_self_test():
    """Run the positive/negative control battery. Return 0 iff all pass."""
    failures = []
    for name, fname, line, expect_fail in _self_test_cases():
        diff = ("--- /dev/null\n"
                f"+++ b/{fname}\n"
                "@@ -0,0 +1 @@\n"
                f"+{line}\n")
        errors = check_no_bridges(diff)
        got_fail = bool(errors)
        status = "ok" if got_fail == expect_fail else "MISMATCH"
        print(f"  [{status}] {name}: expected "
              f"{'FAIL' if expect_fail else 'PASS'}, got "
              f"{'FAIL' if got_fail else 'PASS'}")
        if got_fail != expect_fail:
            failures.append(name)
    if failures:
        print(f"\nSELF-TEST FAILED: {len(failures)} control(s) mismatched: "
              f"{', '.join(failures)}")
        return 1
    print(f"\nSelf-test passed: {len(_self_test_cases())} controls green.")
    return 0


def main():
    if len(sys.argv) > 1 and sys.argv[1] == "--self-test":
        return run_self_test()
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
