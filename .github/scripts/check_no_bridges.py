#!/usr/bin/env python3
"""
No-bridges check (2026-10-15, his direct order).

Fails if the diff introduces new bridges:
- New synth_*.go files
- New package-name conditionals in frontend/backend (Go and Java forms)
- New panic stubs (Go panic() and Java throwPanic / fail-fast
  UnsupportedOperationException)
- New wire node kinds for stdlib functions (Go e.node() and Java
  registerExprKind/registerStmtKind)
- New synthetic function registrations

The Java side is covered by checks 2-4 below, not just the Go side:
a Java package comparison uses .equals("pkg"), not == "pkg"; a Java
fail-fast stub uses Panic.throwPanic / UnsupportedOperationException,
not panic(); and a Java wire kind is added via the public
registerExprKind/registerStmtKind hook, not e.node(). A gate that only
understood the Go forms would pass all three (R1-7, 2026-10-30 review).

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
# Adjudicated Java wire kinds (NOT bridges): additions the Java check 4
# below flags by pattern, but an independent reviewer cleared as genuine
# implementations. Same tuple form as ADJUDICATED_WIRE_NODES, matched
# against the Java registration file.
# INTENTIONALLY EMPTY: the grandfathered registrations in
# P3KindRegistrations.java predate this check and are never re-inspected
# (the gate only looks at ADDED lines), so they need no entry here. Do NOT
# pre-populate this set with them, and do NOT broaden an existing
# ADJUDICATED_* entry to cover a Java file: a future re-addition of a
# kind in Java is a new addition and needs its own fresh independent
# review before an entry is added here.
ADJUDICATED_JAVA_WIRE_NODES = {
    # (file substring, wire node kind)
}

# Stdlib families with dedicated wire node kinds, shared by the Go (e.node)
# and Java (register*Kind) checks. Generic language kinds (ident, call,
# block, send, recv, select, ...) never match: they carry no stdlib-family
# prefix. Channel kinds chanlen/chancap/makechan are language constructs
# and are likewise out of this prefix rule.
STDLIB_KIND_PREFIXES = ("runtime", "fmt", "sync", "os", "net", "time",
                        "math", "reflect", "unsafe", "crypto", "http")

def is_test_file(fname):
    """Test-harness files, exempt from the package-name and Java wire-kind
    checks (drift-test importers resolve synthetic paths; Java builder
    tests register kinds for the node under test). Mirrors the exemption
    check 2 has always applied."""
    return ("/test" in fname or "test_" in fname or "_test." in fname)

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
        # Java compares strings with .equals, not ==, so the Go-only
        # patterns above would pass `pkg.equals("reflect")` untouched
        # (R1-7): cover both operand orders plus Objects.equals.
        for pkg in stdlib_pkgs:
            patterns = [
                rf'==\s*"{pkg}"',
                rf'case\s*"{pkg}"',
                rf'Name\s*==\s*"{pkg}"',
                rf'\.equals(?:IgnoreCase)?\s*\(\s*"{pkg}"',
                rf'"{pkg}"\s*\.equals(?:IgnoreCase)?\s*\(',
                rf'Objects\.equals\s*\([^,]+,\s*"{pkg}"',
            ]
            for pat in patterns:
                if re.search(pat, line):
                    # Allow in test files and docs. ("_test." covers
                    # *_test.go harnesses, e.g. drift-test importers that
                    # resolve synthetic import paths like the pre-existing
                    # `if path == "bytes"` stubs.)
                    if not is_test_file(fname):
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

    # 3b. New Java fail-fast stubs. The Go-only check above scans .go
    # files, so a Java stub passes untouched (R1-7): Synctest's
    # `Panic.throwPanic(GoString.fromUtf8("synctest.Run: not implemented
    # by graalgo"))` is exactly this shape. Mirror the Go keyword set and
    # philosophy: flag, and let a human decide whether an added fail-fast
    # is a legitimate invariant (a genuine Go runtime error such as a nil
    # dereference carries no stub keyword and passes). Two idiomatic
    # Java forms are covered: the Go-visible Panic.throwPanic, and the
    # Java-side fail-fast `throw new UnsupportedOperationException`
    # (bare, or carrying a stub keyword). Plain `throw new
    # RuntimeException(...)` is NOT covered: it is the general error
    # channel, and flagging it by keyword would drown the gate in
    # legitimate error paths. Line-based like the Go check: a message
    # composed across lines escapes this check.
    java_stub_keywords = (r"not implemented|unimplemented|not supported|"
                          r"unsupported|todo|FIXME|unreachable|NYI|not yet")
    for fname, line in added_lines:
        if not fname or not fname.endswith(".java"):
            continue
        if re.search(r'throwPanic\s*\(.*(' + java_stub_keywords + r')',
                     line, re.I):
            errors.append(f"JAVA PANIC STUB in {fname}: {line.strip()[:80]}")
            continue
        if re.search(r'throw\s+new\s+UnsupportedOperationException\s*\(\s*\)', line):
            errors.append(f"JAVA PANIC STUB in {fname}: {line.strip()[:80]}")
            continue
        if re.search(r'throw\s+new\s+UnsupportedOperationException\s*\(.*(' +
                     java_stub_keywords + r')', line, re.I):
            errors.append(f"JAVA PANIC STUB in {fname}: {line.strip()[:80]}")
            continue

    # 4. New wire node kinds for stdlib. Two producer sides share one
    # prefix rule: the Go frontend's e.node("kind") in export.go, and the
    # Java backend's public registerExprKind/registerStmtKind hook (used
    # by P3KindRegistrations.java and NodeFactory.java). The old check
    # scanned only export.go, so a kind added via the Java hook bypassed
    # the gate entirely (R1-7). Adjudication is per-side and narrow:
    # ADJUDICATED_WIRE_NODES covers only the Go side, and
    # ADJUDICATED_JAVA_WIRE_NODES (empty until a fresh independent review
    # clears a Java kind) covers the Java side — neither set is broadened
    # to cover the other side. Test files are exempt on the Java side:
    # builder tests register kinds for the node under test (e.g.
    # RtMemStatsBuilderTest registering "runtimeversion"), which is
    # harness setup, not a product bridge.
    kind_prefix_re = r'^(' + '|'.join(STDLIB_KIND_PREFIXES) + r')[a-z]'
    for fname, line in added_lines:
        if not fname:
            continue
        node_kind = None
        adjudicated = None
        if "export.go" in fname:
            m = re.search(r'e\.node\s*\(\s*"([^"]+)"', line)
            if m:
                node_kind = m.group(1)
                adjudicated = ADJUDICATED_WIRE_NODES
        elif fname.endswith(".java") and not is_test_file(fname):
            m = re.search(r'register(?:Expr|Stmt)Kind\s*\(\s*"([^"]+)"', line)
            if m:
                node_kind = m.group(1)
                adjudicated = ADJUDICATED_JAVA_WIRE_NODES
        if node_kind is not None and re.match(kind_prefix_re, node_kind):
            # Adjudicated real implementations are not bridges.
            if any(f_sub in fname and nk == node_kind
                   for f_sub, nk in adjudicated):
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
