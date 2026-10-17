#!/usr/bin/env python3
"""
No-bridges check (2026-10-15, his direct order; gate overhaul 2026-10-17).

Fails if the diff introduces new bridges:
- New synth_*.go files
- New package-name conditionals in frontend/backend
- New panic/fail-fast stubs (Go and Java)
- New wire node kinds for stdlib functions
- New synthetic function registrations
- New synthetic stdlib package registrations (syntheticSources map)

Existing bridges are grandfathered (they're in the removal queue — see
docs/arch/bridge-inventory.md). This check only looks at ADDED lines in the diff.

Diff sources, in order:
  1. Push delta from CI: git diff ${GITHUB_EVENT_BEFORE}...${GITHUB_SHA}
     (push events only — P0-1: the old HEAD~1...HEAD fall-through inspected
     only the tip commit of a multi-commit push)
  2. origin/master...HEAD  (PR / feature branch)
  3. git diff HEAD         (uncommitted working-tree changes, staged and
                            unstaged — G20: the old separate --cached
                            candidate was dead code, subsumed by this)
  4. merge-base(origin/master, HEAD)...HEAD  (local multi-commit branch)
  5. HEAD~1...HEAD         (last resort: single commit)

The check NEVER passes vacuously: if no diff can be determined (e.g. a
shallow checkout with no history), it fails loudly instead of skipping.
CI must check out full history (fetch-depth: 0) so the push-delta base
resolves.
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

# One stdlib list shared by check 2 (package-name conditionals) and check 4
# (wire-node kinds) — P1-G4: the two checks used to disagree on what counts
# as "stdlib", so e.g. time*/reflect* wire nodes slipped past check 4.
STDLIB_PKGS = ["fmt", "sync", "os", "net", "http", "runtime", "reflect", "time",
               "strings", "bytes", "strconv", "sort", "math", "encoding", "crypto",
               "syscall", "unsafe", "io", "bufio", "context", "errors"]
STDLIB_PREFIX_RE = re.compile(r"^(?:" + "|".join(STDLIB_PKGS) + r")[a-z]")

# The real registration path for a synthetic stdlib package (check 6): one
# line in the syntheticSources map in internal/frontend/synth_machinery.go.
SYNTH_MAP_FILE = "internal/frontend/synth_machinery.go"
SYNTH_MAP_ENTRY_RE = re.compile(
    r'^\s*"([a-z0-9_/]+)"\s*:\s*synthetic\w+Source', re.M)

STUB_WORDS = (r"not implemented|unimplemented|not supported|unsupported|"
              r"todo|FIXME|unreachable|NYI|not yet|tbd")


def _run(cmd):
    try:
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
    except Exception:
        return None
    return r if r.returncode == 0 else None


def _merge_base(a, b):
    r = _run(["git", "merge-base", a, b])
    if r is not None and r.stdout.strip():
        return r.stdout.strip().split("\n")[0]
    return None


def _is_test_path(fname):
    """Component-based test-path detection.

    The old substring checks ("/test" / "test_" anywhere in fname) exempted
    innocent files like contest_runner.go (G19). Match path components
    instead: a `test` or `test_*` directory component, or a *_test.go file.
    """
    if not fname:
        return False
    return (any(p == "test" or p.startswith("test_") for p in fname.split("/"))
            or fname.endswith("_test.go"))


def get_diff():
    """Get the diff of added lines, plus a label and the diff's base rev.

    Returns ("", "", None) when no diff could be determined — the caller must
    treat that as a loud failure, never a vacuous pass. The base rev is the
    pre-diff side, used by check 6 to compute the grandfathered set.
    """
    candidates = []  # (label, cmd, base_rev)
    event = os.environ.get("GITHUB_EVENT_NAME", "")
    before = (os.environ.get("GITHUB_EVENT_BEFORE") or "").strip()
    sha = (os.environ.get("GITHUB_SHA") or "").strip() or "HEAD"
    # P0-1: on push events CI knows the pre-push tip — diff the WHOLE push
    # delta, not just HEAD~1...HEAD. A 40-zero `before` (new branch) falls
    # through to the branch-vs-master candidates below.
    if (event == "push" and re.fullmatch(r"[0-9a-f]{40}", before)
            and before != "0" * 40):
        sha7 = sha[:7] if re.fullmatch(r"[0-9a-f]{40}", sha) else sha
        candidates.append((
            f"push delta ({before[:7]}...{sha7})",
            ["git", "diff", f"{before}...{sha}", "--", "."],
            before,
        ))
    candidates.append(("origin/master...HEAD",
                       ["git", "diff", "origin/master...HEAD", "--", "."],
                       _merge_base("origin/master", "HEAD")))
    candidates.append(("working tree (staged+unstaged vs HEAD)",
                       ["git", "diff", "HEAD", "--", "."],
                       "HEAD"))
    mb = _merge_base("origin/master", "HEAD")
    if mb:
        candidates.append((f"merge-base with origin/master ({mb[:7]}...HEAD)",
                           ["git", "diff", f"{mb}...HEAD", "--", "."],
                           mb))
    candidates.append(("HEAD~1...HEAD (single commit)",
                       ["git", "diff", "HEAD~1...HEAD", "--", "."],
                       "HEAD~1"))
    for name, cmd, base in candidates:
        r = _run(cmd)
        if r is not None and r.stdout.strip():
            return r.stdout, name, base
    return "", "", None


def _grandfathered_synth_packages(base_rev):
    """Import paths already registered in syntheticSources at the diff base.

    The committed map keys are the mechanical grandfather list (the prose
    inventory docs/arch/bridge-inventory.md is authoritative); check 6 flags
    only keys the diff ADDS. Fail-closed: None when the base can't be read.
    """
    r = _run(["git", "show", f"{base_rev or 'HEAD'}:{SYNTH_MAP_FILE}"])
    if r is None:
        return None
    return set(SYNTH_MAP_ENTRY_RE.findall(r.stdout))


def check_no_bridges(diff, base_rev):
    errors = []
    added_lines = []       # (fname, line) — single added lines
    hunk_blocks = {}       # (fname, hunk) -> [added lines in order], for
                           # line-split stubs (G21): panic(\n"not implemented")

    current_file = None
    current_hunk = 0
    for line in diff.split("\n"):
        if line.startswith("+++ b/"):
            current_file = line[6:]
        elif line.startswith("@@"):
            current_hunk += 1
        elif line.startswith("+") and not line.startswith("+++"):
            added_lines.append((current_file, line[1:]))
            hunk_blocks.setdefault((current_file, current_hunk), []).append(line[1:])

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
    for fname, line in added_lines:
        if not fname or not (fname.endswith(".go") or fname.endswith(".java")):
            continue
        # Match: if pkg == "fmt", case "sync", pkg.Name == "os", etc.
        for pkg in STDLIB_PKGS:
            patterns = [
                rf'==\s*"{pkg}"',
                rf'case\s*"{pkg}"',
                rf'Name\s*==\s*"{pkg}"',
            ]
            for pat in patterns:
                if re.search(pat, line):
                    # Allow in test files and docs. ("_test.go" covers
                    # *_test.go harnesses, e.g. drift-test importers that
                    # resolve synthetic import paths like the pre-existing
                    # `if path == "bytes"` stubs.)
                    if not _is_test_path(fname):
                        errors.append(f"PACKAGE-NAME CONDITIONAL in {fname}: {line.strip()[:80]}")
                        break

    # 3. Fail-fast stubs: Go panics and Java throws. Fail loud on all of
    #    them — a human decides whether an added stub is a legitimate
    #    invariant. P1-G3: the old check skipped non-.go files, so the Java
    #    stub idiom (throw new UnsupportedOperationException("not
    #    implemented")) passed silently.
    go_stub_res = [
        r'panic\s*\(\s*\)',            # bare panic() — nothing to go on
        r'panic\s*\(\s*""\s*\)',       # panic("") — nothing to go on
        r'panic\s*\(\s*"(?:' + STUB_WORDS + r')',
        # Stub keyword anywhere in the first string arg of a composed panic.
        r'panic\s*\(\s*(?:fmt\.Sprintf|fmt\.Errorf|errors\.New)\s*\(\s*"[^"]*(?:' + STUB_WORDS + r')',
        # Indirect: panic(errNotImplemented) and friends (G21).
        r'panic\s*\(\s*err\w*(?:notimplemented|unimplemented|notsupported|unsupported|todo|unreachable|nyi|notyet|tbd)',
        # log.Fatal("todo") and friends are fail-fast stubs too (G21).
        r'log\.(?:Fatal|Fatalf|Panic|Panicf)\s*\(\s*"(?:' + STUB_WORDS + r')',
    ]
    java_stub_res = [
        r'throw\s+new\s+UnsupportedOperationException\s*\(\s*\)',
        r'throw\s+new\s+UnsupportedOperationException\s*\(\s*""\s*\)',
        r'throw\s+new\s+(?:UnsupportedOperationException|IllegalStateException|RuntimeException|AssertionError)\s*\(\s*"(?:' + STUB_WORDS + r')',
    ]
    for fname, line in added_lines:
        if not fname:
            continue
        if fname.endswith(".go"):
            pats = go_stub_res
        elif fname.endswith(".java"):
            pats = java_stub_res
        else:
            continue
        for pat in pats:
            if re.search(pat, line, re.I):
                errors.append(f"FAIL-FAST STUB in {fname}: {line.strip()[:80]}")
                break
    # G21: line-split stubs — panic( \n "not implemented"). Applied per
    # (file, hunk) block; the separator tolerates trailing // comments on
    # the paren line. Reported only when the match actually spans a newline
    # (single-line hits are already reported by the line pass above).
    go_block_res = [
        r'panic\s*\((?:\s|//[^\n]*)*"(?:' + STUB_WORDS + r')',
        r'log\.(?:Fatal|Fatalf|Panic|Panicf)\s*\((?:\s|//[^\n]*)*"(?:' + STUB_WORDS + r')',
    ]
    java_block_res = [
        r'throw\s+new\s+(?:UnsupportedOperationException|IllegalStateException|'
        r'RuntimeException|AssertionError)\s*\((?:\s|//[^\n]*)*"(?:' + STUB_WORDS + r')',
    ]
    block_pats = {
        ".go": go_block_res,
        ".java": java_block_res,
    }
    for (fname, _hunk), lines in hunk_blocks.items():
        if not fname:
            continue
        ext = ".go" if fname.endswith(".go") else ".java" if fname.endswith(".java") else None
        if ext is None:
            continue
        block = "\n".join(lines)
        for pat in block_pats[ext]:
            m = re.search(pat, block, re.I)
            if m and "\n" in m.group(0):
                errors.append(f"LINE-SPLIT FAIL-FAST STUB in {fname}: {m.group(0)[:80].replace(chr(10), ' / ')}")
                break

    # 4. New wire node kinds for stdlib
    for fname, line in added_lines:
        if not fname or "export.go" not in fname:
            continue
        m = re.search(r'e\.node\s*\(\s*"([^"]+)"', line)
        if m:
            node_kind = m.group(1)
            # Flag stdlib-specific node kinds, using the same prefix list
            # as check 2 (P1-G4).
            if STDLIB_PREFIX_RE.match(node_kind):
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

    # 6. New synthetic stdlib package registrations (P0-2). The real
    #    registration path is one line in the syntheticSources map —
    #    checks 1/2/5 all miss it. Grandfathered = already registered at the
    #    diff base; a newly added key (including an uncommented RETIRED
    #    entry) is a new bridge.
    grandfathered = _grandfathered_synth_packages(base_rev)
    for fname, line in added_lines:
        if not fname or not fname.endswith(".go"):
            continue
        m = SYNTH_MAP_ENTRY_RE.match(line)
        if m and not _is_test_path(fname):
            pkg = m.group(1)
            if grandfathered is None or pkg not in grandfathered:
                errors.append(
                    f"NEW SYNTHETIC PACKAGE REGISTRATION in {fname}: "
                    f'"{pkg}" — route the real package via the disk importer instead')

    return errors


def main():
    diff, source, base_rev = get_diff()
    if not diff.strip():
        # Never pass vacuously: on the standard integration path (wt.py
        # integrate + push, no PR) every diff source above is empty only when
        # the checkout has no history to diff against (e.g. a shallow
        # checkout — CI must use fetch-depth: 0 so github.event.before
        # resolves). Skipping here is exactly the false-green hole this
        # check exists to close.
        print("NO-BRIDGES CHECK FAILED:")
        print("  could not determine any diff to inspect (no push delta, no")
        print("  PR diff, no working-tree changes, and HEAD~1 is unavailable")
        print("  — is this a shallow checkout? CI must use fetch-depth: 0).")
        print("  Refusing to pass vacuously.")
        return 1

    print(f"No-bridges check: inspecting added lines from {source}.")
    errors = check_no_bridges(diff, base_rev)
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
