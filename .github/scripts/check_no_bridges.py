#!/usr/bin/env python3
"""
No-bridges check (2026-10-15, his direct order).

Fails if the diff introduces new bridges:
- New synth_*.go files
- New package-name conditionals in frontend/backend
- New package-path conditionals (obj.Pkg().Path() == "<stdlib>", either
  operand order)
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
    # stdlib_pkgs is the COMPLETE top-level stdlib set for the tracked Go
    # release, derived mechanically — not hand-picked. Derivation
    # (go1.27.1, repo toolchain ~/tools/go/bin/go): `go list std` returns
    # 388 paths; their first components are exactly the 52 names below
    # (plus the pre-existing "http", a package *name* — net/http's Name()
    # — rather than a top-level path, kept so check 2's Name == "http"
    # form keeps working). Check 2b's prefix rule (path.startswith(p +
    # "/")) extends this top-level set to every subpath, so the 388
    # paths are all covered without listing each one. On a toolchain
    # upgrade, re-derive with `go list std` and update this list —
    # a new top-level package added by the toolchain would otherwise
    # slip past check 2b the same way "unicode"/"log" used to
    # (REVIEW-2026-11-01-21 V-1 fixup).
    stdlib_pkgs = ["fmt", "sync", "os", "net", "http", "runtime", "reflect", "time",
                   "strings", "bytes", "strconv", "sort", "math", "encoding", "crypto",
                   "syscall", "unsafe", "io", "bufio", "context", "errors",
                   # Remaining top-level names from `go list std` (go1.27.1).
                   "archive", "cmp", "compress", "container", "database", "debug",
                   "embed", "expvar", "flag", "go", "hash", "html", "image",
                   "index", "internal", "iter", "log", "maps", "mime", "path",
                   "plugin", "regexp", "slices", "structs", "testing", "text",
                   "unicode", "unique", "uuid", "vendor", "weak", "zzdiag",
                   # Full stdlib paths, so the == "<pkg>" pattern below also
                   # matches them (e.g. pkgPath == "sync/atomic"). The
                   # Pkg().Path() form is covered separately by check 2b.
                   "sync/atomic", "internal/runtime/atomic"]
    pkg_name_flagged = set()
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
                        pkg_name_flagged.add((fname, line))
                        break

    # 2b. Package-path conditionals (go/types form). Before this lane,
    # no check-2 pattern matched the selector interceptions
    # grandfathered by bridge-inventory B-3.1/B-3.2, which use the
    # go/types form obj.Pkg().Path() == "sync/atomic" (and
    # fn.Pkg().Path() == "internal/runtime/atomic",
    # rn.Obj().Pkg().Path() == "runtime") — so those interceptions
    # passed the gate while uninventoried (REVIEW-2026-11-01-21 P1-4).
    # Check 2 now also matches the listed full-path literals; this
    # check additionally covers stdlib path literals in Pkg().Path()
    # form that the list does not name as full paths (e.g.
    # "internal/abi", "encoding/xml"), and reports each added line once
    # (lines check 2 already flagged are skipped here). Coverage,
    # stated exactly so the next miss cannot hide behind a vague
    # comment (REVIEW-2026-11-01-21 V-1 fixup): a literal is flagged
    # iff _is_stdlib_path accepts it — i.e. it is a top-level name in
    # stdlib_pkgs (the complete `go list std` first-component set for
    # go1.27.1, see above), a subpath of one (prefix rule), or an
    # "internal/..." path — AND the comparison is a single-line ==
    # between that literal and a Pkg().Path() expression, in either
    # operand order (forward: obj.Pkg().Path() == "lit"; reversed:
    # "lit" == obj.Pkg().Path(), including longer receiver chains such
    # as rn.Obj().Pkg().Path()). NOT covered, by design or by scope
    # limit — do not read this check as covering them: != guards,
    # variable-to-variable comparisons (pkgPath == wantPkg, the general
    # mechanism), switch-case on a path value, multi-line comparisons,
    # parenthesized path expressions, and non-Pkg().Path() string forms
    # (those are check 2's scope, for the listed names/paths only).
    # Any literal package-path equality this check does flag is
    # package-name-keyed by definition; only stdlib paths are flagged
    # (a literal naming a user package is not a stdlib bridge). This
    # check, like check 2, only inspects ADDED lines, so the
    # grandfathered lines inventoried in B-3 are not flagged — only
    # future additions are. Test files are exempt (same rule as
    # check 2).
    def _is_stdlib_path(path):
        if path in stdlib_pkgs:
            return True
        if path.startswith("internal/"):
            return True
        return any(path.startswith(p + "/") for p in stdlib_pkgs)

    for fname, line in added_lines:
        if not fname or not (fname.endswith(".go") or fname.endswith(".java")):
            continue
        if (fname, line) in pkg_name_flagged:
            continue
        m = re.search(r'Pkg\(\)\.Path\(\)\s*==\s*"([^"]+)"', line)
        if m is None:
            # Reversed operand order: "lit" == <recv>.Pkg().Path().
            # The receiver is restricted to identifier/dot/paren chars
            # so the match cannot span across an unrelated comparison
            # on the same line.
            m = re.search(r'"([^"]+)"\s*==\s*[\w.()]*Pkg\(\)\.Path\(\)', line)
        if m and _is_stdlib_path(m.group(1)):
            if "/test" not in fname and "test_" not in fname and "_test." not in fname:
                errors.append(f"PACKAGE-PATH CONDITIONAL in {fname}: {line.strip()[:80]}")

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
