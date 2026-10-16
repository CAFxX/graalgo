#!/usr/bin/env python3
"""
No-bridges check (2026-10-15, his direct order).

Fails if the diff introduces new bridges:
- New synth_*.go files (rule 1), plus content-based detection of synthetic
  payloads in new files that dodge the synth_ filename (rule 1b, R2-P1-5)
- New package-name conditionals in frontend/backend (rule 2)
- New panic stubs (rule 3)
- New wire node kinds for stdlib functions (rule 4)
- New synthetic function registrations (rule 5)
- Growth of the synthetic-package set without independent-review
  adjudication (rule 0, R2-P1-5): the committed ledger
  (.github/scripts/synthetic_package_ledger.json) records the exact
  grandfathered set — the keys of `syntheticSources`
  (internal/frontend/synth_machinery.go) plus `dedicatedSyntheticStubs`
  (internal/frontend/export.go). The gate fails when the ledger does not
  match the code, and fails on any *addition* to the set that is not in
  ADJUDICATED_SYNTHETIC_PACKAGES with a filed review record.
  Removals need no sign-off: shrinking the set is the goal.

Existing bridges are grandfathered (they're in the removal queue — see
docs/arch/bridge-inventory.md). Rules 1-5 only look at ADDED lines in the
diff; rule 0 additionally compares the post-change tree against the
committed ledger.

Diff sources, in order:
  1. origin/master...HEAD  (PR / feature branch)            [base: origin/master]
  2. git diff HEAD         (uncommitted working-tree changes) [base: HEAD]
  3. git diff --cached     (staged changes)                  [base: HEAD]
  4. HEAD~1...HEAD         (direct master push: wt.py integrate + push, no PR —
                           checkout lands exactly on origin/master, so 1-3 are
                           all empty; inspect what the push itself introduced)
                                                             [base: HEAD~1]

The check NEVER passes vacuously: if no diff can be determined (e.g. a
shallow checkout with no history), it fails loudly instead of skipping.
CI must check out full history (fetch-depth: 0) for source 4 to resolve.

`--emit-ledger`: regenerate the ledger from the working-tree code and exit.
Used by `make gen-ledger` and by lanes updating the ledger in the same
change as the code. Writes only when the content differs.
"""

import json
import os
import re
import subprocess
import sys

LEDGER_REL = ".github/scripts/synthetic_package_ledger.json"
INVENTORY_REL = "docs/arch/bridge-inventory.md"

# Adjudicated NEW synthetic packages (NOT grandfathered): additions to the
# synthetic-package set that an independent reviewer cleared as genuine
# implementations. Maps the new import path -> the filed review-record
# anchor in docs/arch/bridge-inventory.md ("Adjudicated non-bridges").
# The gate verifies the cited record exists; a dangling citation fails.
# Do NOT add an entry without a fresh independent review AND its filed
# record (R2-P2-11).
ADJUDICATED_SYNTHETIC_PACKAGES = {
}

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
}
ADJUDICATED_REGISTRATIONS = {
    # (file substring, addFunc'd symbol name)
    # PKG-023 (commit 1d4e7964, 2026-10-16; independent review CLEAN, no
    # veto): addFunc("NumGoroutine", ...) declares genuine stdlib surface in
    # the synthetic runtime package; the backend implements it for real.
    ("internal/frontend/export.go", "NumGoroutine"),
}


def _repo_root():
    try:
        r = subprocess.run(["git", "rev-parse", "--show-toplevel"],
                           capture_output=True, text=True, timeout=30)
        if r.returncode == 0 and r.stdout.strip():
            return r.stdout.strip()
    except Exception:
        pass
    return os.getcwd()


def get_diff(repo_root):
    """Get the diff of added lines, a label, and the base ref for the old ledger.

    Returns ("", "", "") when no diff could be determined — the caller must
    treat that as a loud failure, never a vacuous pass.
    """
    # Try vs origin/master first (for PRs), fall back to working tree,
    # then to the master-push delta. A shallow checkout (no HEAD~1)
    # yields nothing from the last source — that is a failure, not a skip.
    candidates = [
        ("origin/master...HEAD", ["git", "diff", "origin/master...HEAD", "--", "."],
         "origin/master"),
        ("working tree (unstaged)", ["git", "diff", "HEAD", "--", "."],
         "HEAD"),
        ("staged", ["git", "diff", "--cached", "--", "."],
         "HEAD"),
        ("HEAD~1...HEAD (master push)", ["git", "diff", "HEAD~1...HEAD", "--", "."],
         "HEAD~1"),
    ]
    for name, cmd, base in candidates:
        try:
            result = subprocess.run(cmd, capture_output=True, text=True,
                                    timeout=30, cwd=repo_root)
        except Exception:
            continue
        if result.returncode == 0 and result.stdout.strip():
            return result.stdout, name, base
    return "", "", ""


def _strip_go_strings(line):
    # Blank double-quoted and backtick Go string literals so brace counting
    # is not confused by braces inside strings; then drop // comments.
    line = re.sub(r'"(?:[^"\\]|\\.)*"', '""', line)
    line = re.sub(r'`[^`]*`', '``', line)
    line = re.sub(r'//.*$', '', line)
    return line


def _parse_go_string_map(path, var_name):
    """Parse the keys of `var <var_name> = map[...]{...}` from a Go file.

    Returns the set of string-literal keys. Raises on parse failure —
    loud, never silent: an unparseable registration map means the gate
    cannot verify the synthetic-package set.
    """
    with open(path, encoding="utf-8") as f:
        lines = f.read().split("\n")
    anchor = None
    for i, line in enumerate(lines):
        if re.match(r'^\s*var\s+%s\s*=\s*map\[' % re.escape(var_name), line):
            anchor = i
            break
    if anchor is None:
        raise ValueError(f"could not find `var {var_name} = map[` in {path}")
    keys = set()
    depth = 0
    for line in lines[anchor:]:
        if depth >= 1:
            m = re.match(r'^\s*"((?:[^"\\]|\\.)*)"\s*:', line)
            if m:
                keys.add(m.group(1))
        code = _strip_go_strings(line)
        depth += code.count("{") - code.count("}")
        if depth <= 0 and line is not lines[anchor]:
            break
        if depth <= 0:
            # The anchor line itself must open the literal.
            raise ValueError(f"`var {var_name}` literal never opens in {path}")
    if depth != 0:
        raise ValueError(f"unbalanced braces parsing `{var_name}` in {path}")
    return keys


def parse_synthetic_set(repo_root):
    """The actual synthetic-package set from the working-tree code.

    Returns (sources, stubs): keys of `syntheticSources` in
    internal/frontend/synth_machinery.go and of `dedicatedSyntheticStubs`
    in internal/frontend/export.go. Both maps are read-only after init
    (every use is a lookup); the literals are the complete registration.
    """
    sources = _parse_go_string_map(
        os.path.join(repo_root, "internal/frontend/synth_machinery.go"),
        "syntheticSources")
    stubs = _parse_go_string_map(
        os.path.join(repo_root, "internal/frontend/export.go"),
        "dedicatedSyntheticStubs")
    return sources, stubs


def load_ledger(repo_root):
    """Read the committed ledger; returns (sources, stubs) sets."""
    with open(os.path.join(repo_root, LEDGER_REL), encoding="utf-8") as f:
        ledger = json.load(f)
    return (set(ledger["packages"]["syntheticSources"]),
            set(ledger["packages"]["dedicatedSyntheticStubs"]))


def emit_ledger(repo_root):
    """Regenerate the ledger from the working-tree code (only writes on change)."""
    sources, stubs = parse_synthetic_set(repo_root)
    ledger = {
        "_note": (
            "Synthetic-package ledger for the no-bridges gate "
            "(check_no_bridges.py, rule 0; R2-P1-5). DO NOT hand-edit: "
            "regenerate with `python3 .github/scripts/check_no_bridges.py "
            "--emit-ledger` and commit the result in the same change as the "
            "code (`make gen-ledger`; CI fails on drift). The gate fails when "
            "this ledger does not match the code, and fails on any ADDITION "
            "to the set without an independent-review sign-off recorded in "
            "docs/arch/bridge-inventory.md ('Adjudicated non-bridges') plus an "
            "entry in the gate's ADJUDICATED_SYNTHETIC_PACKAGES allowlist. "
            "Removals need no sign-off: shrinking the set is the goal. "
            "Baseline: 133 syntheticSources keys + 3 dedicatedSyntheticStubs "
            "keys, counted on disk 2026-10-16 (review-r2-synth-ledger lane)."
        ),
        "code": {
            "syntheticSources": "internal/frontend/synth_machinery.go",
            "dedicatedSyntheticStubs": "internal/frontend/export.go",
        },
        "packages": {
            "syntheticSources": sorted(sources),
            "dedicatedSyntheticStubs": sorted(stubs),
        },
    }
    out = json.dumps(ledger, indent=2) + "\n"
    path = os.path.join(repo_root, LEDGER_REL)
    old = None
    if os.path.exists(path):
        with open(path, encoding="utf-8") as f:
            old = f.read()
    if old == out:
        print(f"ledger up to date: {LEDGER_REL} "
              f"({len(sources)} syntheticSources + {len(stubs)} dedicatedStubs)")
        return
    with open(path, "w", encoding="utf-8") as f:
        f.write(out)
    print(f"ledger regenerated: {LEDGER_REL} "
          f"({len(sources)} syntheticSources + {len(stubs)} dedicatedStubs)")


def _ledger_at_ref(repo_root, ref):
    """The ledger set committed at git ref; None when unavailable."""
    try:
        r = subprocess.run(["git", "show", f"{ref}:{LEDGER_REL}"],
                           capture_output=True, text=True, timeout=30,
                           cwd=repo_root)
    except Exception:
        return None
    if r.returncode != 0:
        return None
    try:
        ledger = json.loads(r.stdout)
        return (set(ledger["packages"]["syntheticSources"]) |
                set(ledger["packages"]["dedicatedSyntheticStubs"]))
    except Exception:
        return None


def _is_shallow(repo_root):
    try:
        r = subprocess.run(["git", "rev-parse", "--is-shallow-repository"],
                           capture_output=True, text=True, timeout=30,
                           cwd=repo_root)
        return r.returncode == 0 and r.stdout.strip() == "true"
    except Exception:
        return False


def check_synthetic_ledger(diff, base_ref, repo_root):
    """Rule 0 (R2-P1-5): the synthetic-package set cannot grow undetected.

    Returns (errors, notes). Fails when the committed ledger does not match
    the post-change code, and when the set gains a key that lacks an
    independent-review adjudication with a filed record.
    """
    errors = []
    notes = []
    try:
        ledger_sources, ledger_stubs = load_ledger(repo_root)
    except Exception as e:
        return ([f"SYNTHETIC LEDGER UNREADABLE: {LEDGER_REL}: {e} — regenerate "
                 "with `python3 .github/scripts/check_no_bridges.py --emit-ledger` "
                 "and commit it"], notes)
    ledger_set = ledger_sources | ledger_stubs

    try:
        actual_sources, actual_stubs = parse_synthetic_set(repo_root)
    except Exception as e:
        return ([f"SYNTHETIC SET PARSE FAILED: {e} — the gate cannot verify "
                 "the synthetic-package set; fix the map literal or the parser, "
                 "never silence this"], notes)
    actual_set = actual_sources | actual_stubs

    if actual_set != ledger_set:
        missing = sorted(ledger_set - actual_set)
        extra = sorted(actual_set - ledger_set)
        errors.append(
            "SYNTHETIC LEDGER DRIFT: the committed ledger does not match the "
            f"code. In ledger but not in code: {missing or '—'}; "
            f"in code but not in ledger: {extra or '—'}. Regenerate with "
            "`python3 .github/scripts/check_no_bridges.py --emit-ledger` and "
            "commit the result in the same change as the code.")

    base_set = _ledger_at_ref(repo_root, base_ref)
    if base_set is None:
        base_set = _ledger_at_ref(repo_root, "HEAD")
    if base_set is None:
        if _is_shallow(repo_root):
            return (errors + ["SYNTHETIC LEDGER BASELINE UNAVAILABLE: shallow "
                              "checkout has no history to diff against — CI must "
                              "use fetch-depth: 0. Refusing to pass vacuously"],
                    notes)
        # The ledger genuinely does not exist at the base: this change
        # establishes the baseline (its content is under independent review
        # as part of this commit). The ledger/code consistency check above
        # still applies. Note: after the baseline lands, every future base
        # (origin/master, HEAD, HEAD~1) carries the ledger, so growth can no
        # longer pass through this path unnoticed.
        notes.append(
            "synthetic ledger is new in this diff: establishing the "
            "grandfathered baseline — addition-adjudication is skipped for "
            "this commit (the baseline itself is under independent review).")
        return errors, notes

    added = actual_set - base_set
    removed = base_set - actual_set
    if removed:
        notes.append(f"synthetic packages retired in this diff (no sign-off "
                     f"needed — removal is the goal): {sorted(removed)}")
    if not added:
        return errors, notes

    try:
        with open(os.path.join(repo_root, INVENTORY_REL),
                   encoding="utf-8") as f:
            inventory = f.read()
    except Exception as e:
        return (errors + [f"cannot read {INVENTORY_REL}: {e} — the "
                          "adjudication records cannot be verified"], notes)
    for pkg in sorted(added):
        record = ADJUDICATED_SYNTHETIC_PACKAGES.get(pkg)
        if record is None:
            errors.append(
                f"NEW SYNTHETIC PACKAGE {pkg!r}: the synthetic-package set "
                "grew without an independent-review sign-off. His order "
                "(2026-10-15): no new bridges — new synthetic stdlib packages "
                "require an independent review, its record filed in "
                f"{INVENTORY_REL} ('Adjudicated non-bridges'), and an entry in "
                "the gate's ADJUDICATED_SYNTHETIC_PACKAGES allowlist.")
        elif record not in inventory:
            errors.append(
                f"NEW SYNTHETIC PACKAGE {pkg!r}: adjudication cites {record!r} "
                f"but that record is not filed in {INVENTORY_REL} — file the "
                "review record first (R2-P2-11); a dangling citation is a "
                "whitewash-shaped hole.")
        else:
            notes.append(f"new synthetic package {pkg!r} adjudicated "
                         f"({record}) — allowed.")
    return errors, notes


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
    #    would fail the gate. (Rule 0 separately guards the package set
    #    against growth inside grandfathered files.)
    new_synth = set()
    new_go_files = set()
    prev_was_devnull = False
    for line in diff.split("\n"):
        if line.startswith("--- /dev/null"):
            prev_was_devnull = True
        elif line.startswith("+++ b/"):
            fname = line[6:]
            if prev_was_devnull:
                if fname.endswith(".go"):
                    new_go_files.add(fname)
                if "synth_" in fname and fname.endswith(".go"):
                    new_synth.add(fname)
            prev_was_devnull = False
        elif line.startswith("--- "):
            prev_was_devnull = False
    for f in new_synth:
        errors.append(f"NEW SYNTHETIC FILE: {f} — synthetic stdlib packages are banned")

    # 1b. Content-based synthetic-payload detection (R2-P1-5): rule 1 only
    #     looks at the filename. A new .go file without "synth_" in its name
    #     can still carry a synthetic package — the embedded source payload
    #     and/or a registration entry. Flag the content shapes.
    for f in sorted(new_go_files):
        if "synth_" in f:
            continue
        for fname, line in added_lines:
            if fname != f:
                continue
            if re.search(r'^\s*(?:var|const)\s+synthetic[A-Z][A-Za-z0-9]*Source\b', line):
                errors.append(
                    f"SYNTHETIC-CONTENT FILE: {f} defines an embedded synthetic "
                    f"source payload ({line.strip()[:70]}) but lacks the synth_ "
                    "name — synthetic stdlib packages are banned; a new synthetic "
                    "package requires the ledger plus independent-review adjudication")
                break
            if re.search(r'^\s*"[^"]+"\s*:\s*synthetic[A-Z][A-Za-z0-9]*Source', line):
                errors.append(
                    f"SYNTHETIC-CONTENT FILE: {f} registers a synthetic package "
                    f"({line.strip()[:70]}) but lacks the synth_ name — new "
                    "synthetic packages require the ledger plus independent-review "
                    "adjudication")
                break

    # 1c. Registration-map writes outside the literals, and brand-new
    #     synthetic-named maps: the ledger parser reads the two known map
    #     literals; post-init writes or parallel maps would dodge it.
    for fname, line in added_lines:
        if not fname or not fname.endswith(".go") or fname.endswith("_test.go"):
            continue
        if re.search(r'\b(syntheticSources|dedicatedSyntheticStubs)\s*\[[^\]]+\]\s*=', line):
            errors.append(f"SYNTHETIC MAP WRITE in {fname}: {line.strip()[:80]} — "
                          "register via the map literal, the ledger, and adjudication")
        elif re.search(r'^\s*var\s+[A-Za-z_][A-Za-z0-9_]*[Ss]ynthetic[A-Za-z0-9_]*\s*=\s*map\[', line):
            errors.append(f"NEW SYNTHETIC MAP in {fname}: {line.strip()[:80]} — "
                          "new synthetic-package registries require adjudication")

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
    if "--emit-ledger" in sys.argv[1:]:
        emit_ledger(_repo_root())
        return 0

    repo_root = _repo_root()
    diff, source, base_ref = get_diff(repo_root)
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
    ledger_errors, ledger_notes = check_synthetic_ledger(diff, base_ref, repo_root)
    errors = ledger_errors + errors
    for n in ledger_notes:
        print(f"  note: {n}")
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
