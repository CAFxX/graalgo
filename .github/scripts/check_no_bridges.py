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
  2. <before>...HEAD       (direct master push: --before/NO_BRIDGES_BEFORE
                           carries the pre-push head — CI passes
                           github.event.before. wt.py integrate
                           fast-forward-merges multi-commit lane branches,
                           so HEAD~1 alone would inspect only the tip commit;
                           the push's FULL range must be diffed). An
                           explicitly provided, non-all-zero --before that
                           cannot be diffed fails LOUDLY (never a silent
                           downgrade to the tip-only diff); an all-zero
                           --before (new branch) is skipped like an absent
                           one.
  3. git diff HEAD         (uncommitted working-tree changes)
  4. git diff --cached     (staged changes)
  5. HEAD~1...HEAD         (fallback when no --before is given; covers
                           only the tip commit)

The check NEVER passes vacuously: if no diff can be determined (e.g. a
shallow checkout with no history, or an unresolvable --before SHA), it fails
loudly instead of skipping.
CI must check out full history (fetch-depth: 0) for the pre-push range to
resolve.
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

def parse_args(argv=None):
    parser = argparse.ArgumentParser(
        description="No-bridges gate: fail if the diff introduces new bridge "
                    "patterns (synthetic stdlib, package-name conditionals, "
                    "panic stubs, stdlib wire nodes, synthetic registrations).")
    parser.add_argument(
        "--before",
        default=os.environ.get("NO_BRIDGES_BEFORE", ""),
        help="Pre-push head SHA for direct master pushes (CI passes "
             "github.event.before). Diffs the push's full commit range "
             "instead of just the tip commit. Also read from "
             "$NO_BRIDGES_BEFORE; empty disables this diff source.",
    )
    return parser.parse_args(argv)


def get_diff(before=None):
    """Get the diff of added lines, plus a label naming the diff source.

    Returns ("", "") when no diff could be determined — the caller must
    treat that as a loud failure, never a vacuous pass.

    An explicitly provided, non-all-zero --before is authoritative: if it
    cannot be diffed (pruned history, unknown SHA) the function raises
    RuntimeError — a loud failure, never a silent downgrade to the tip-only
    fallback (never pass vacuously). An all-zero --before (new branch) is
    skipped exactly like an absent --before, keeping the tip-only fallback.
    """
    before = (before or "").strip()
    # An all-zero --before (new branch / github.event.before == 0{40}) is
    # skipped exactly like an absent --before: the tip-only fallback below
    # stays available. Only an explicitly provided, non-all-zero --before
    # is authoritative, and only it fails loudly when unresolvable.
    before_usable = bool(before) and not re.fullmatch(r"0+", before)
    # Try vs origin/master first (for PRs), then the pre-push head (for
    # direct master pushes), fall back to working tree, then to the
    # tip-commit delta. A shallow checkout (no resolvable history) yields
    # nothing from the history-based sources — that is a failure, not a
    # skip. The tip-only fallback is for the no---before case only: when
    # --before was explicitly provided, the push's full range must be
    # diffed, and an unresolvable --before fails loudly instead of
    # degrading to one commit.
    candidates = [
        ("origin/master...HEAD", ["git", "diff", "origin/master...HEAD", "--", "."]),
    ]
    if before_usable:
        candidates.append(
            (f"{before}...HEAD (master push, full range)",
             ["git", "diff", f"{before}...HEAD", "--", "."]),
        )
    candidates += [
        ("working tree (unstaged)", ["git", "diff", "HEAD", "--", "."]),
        ("staged", ["git", "diff", "--cached", "--", "."]),
    ]
    if not before_usable:
        candidates.append(
            ("HEAD~1...HEAD (tip only)", ["git", "diff", "HEAD~1...HEAD", "--", "."]),
        )
    for name, cmd in candidates:
        is_before = before_usable and name.startswith(f"{before}...HEAD")
        try:
            result = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
        except Exception as e:
            if is_before:
                raise RuntimeError(
                    f"could not diff the --before range {before}...HEAD: {e}")
            continue
        if result.returncode != 0:
            if is_before:
                raise RuntimeError(
                    f"--before {before} is unresolvable "
                    f"(git diff exited {result.returncode}: "
                    f"{result.stderr.strip()[:160]}). The push's full range "
                    f"cannot be inspected; fetch full history (fetch-depth: 0) "
                    f"and re-run. Refusing to fall back to the tip-only diff.")
            continue
        if result.stdout.strip():
            return result.stdout, name
        if is_before:
            # Resolvable but empty: the push genuinely added nothing —
            # nothing to flag. Return the source label so the caller can
            # report an explicit pass instead of a vacuous skip.
            return "", name
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
    # Java fail-fast stubs. A bare `throw new UnsupportedOperationException`
    # was once treated as a stub by construction, but that pattern
    # false-positives on the codebase's own defensive-throw idiom
    # (e.g. `default -> throw new UnsupportedOperationException("not an
    # integer op: " + op);` in frontend op-dispatch switches), which is a
    # legitimate invariant throw, not a placeholder. So the
    # UnsupportedOperationException patterns are keyword-restricted exactly
    # like the others: flag stub keywords literally or composed via
    # String.format, and keep flagging bare no-arg throws (mirrors Go's
    # bare-panic rule — nothing to go on). Other exception types are flagged
    # when the thrown message carries a stub keyword, literally or composed
    # via String.format. As with the Go patterns, a human adjudicates each
    # flag — a legitimate invariant throw is fine, a "not implemented yet"
    # placeholder is not.
    java_stub_res = [
        r'throw\s+new\s+UnsupportedOperationException\s*\(\s*\)',  # bare — nothing to go on
        r'throw\s+new\s+UnsupportedOperationException\s*\(\s*"(not implemented|unimplemented|not supported|unsupported|todo|FIXME|unreachable|NYI|not yet|tbd|stub)',
        # Stub keyword in a String.format-composed message.
        r'throw\s+new\s+UnsupportedOperationException\s*\(\s*String\.format\s*\(\s*"[^"]*(not implemented|unimplemented|not supported|unsupported|todo|FIXME|unreachable|NYI|not yet|tbd|stub)',
        r'throw\s+new\s+[\w.]+\s*\(\s*"(not implemented|unimplemented|not supported|unsupported|todo|FIXME|unreachable|NYI|not yet|tbd|stub)',
        r'throw\s+new\s+[\w.]+\s*\(\s*String\.format\s*\(\s*"[^"]*(not implemented|unimplemented|not supported|unsupported|todo|FIXME|unreachable|NYI|not yet|tbd|stub)',
    ]
    for fname, line in added_lines:
        if not fname:
            continue
        if fname.endswith(".go"):
            pats = panic_stub_res
        elif fname.endswith(".java"):
            pats = java_stub_res
        else:
            continue
        for pat in pats:
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

def main(argv=None):
    args = parse_args(argv)
    try:
        diff, source = get_diff((args.before or "").strip() or None)
    except RuntimeError as e:
        # An explicitly provided --before that cannot be diffed is a loud
        # failure (never pass vacuously, never fall back to tip-only).
        print("NO-BRIDGES CHECK FAILED:")
        print(f"  {e}")
        print("  Refusing to pass vacuously or fall back to the tip-only diff.")
        return 1
    if not source:
        # Never pass vacuously: on the standard integration path (wt.py
        # integrate + push, no PR) every diff source above is empty only when
        # the checkout has no history to diff against. Skipping here is
        # exactly the false-green hole this check exists to close.
        print("NO-BRIDGES CHECK FAILED:")
        print("  could not determine any diff to inspect (no PR diff, no")
        print("  --before/NO_BRIDGES_BEFORE range, no working-tree changes,")
        print("  and HEAD~1 is unavailable — is this a shallow checkout?")
        print("  CI must use fetch-depth: 0).")
        print("  Refusing to pass vacuously.")
        return 1
    if not diff.strip():
        # The diff source resolved (an explicitly provided --before range)
        # and is genuinely empty: the push added nothing, so there is
        # nothing to flag. Report the pass explicitly.
        print(f"No-bridges check: {source} — no added lines to inspect.")
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
