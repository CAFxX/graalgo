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
}
ADJUDICATED_REGISTRATIONS = {
    # (file substring, addFunc'd symbol name)
    # PKG-023 (commit 1d4e7964, 2026-10-16; independent review CLEAN, no
    # veto): addFunc("NumGoroutine", ...) declares genuine stdlib surface in
    # the synthetic runtime package; the backend implements it for real.
    ("internal/frontend/export.go", "NumGoroutine"),
}

# All importable Go stdlib top-level package paths (from `go list std`,
# go1.27.1, 2026-10-16; internal/vendor/zzdiag/testdata excluded). A
# package-name conditional keyed on ANY of these (bare or as a path prefix)
# is a bridge signal.
STDLIB_TOP = [
    "archive", "bufio", "bytes", "cmp", "compress", "container", "context",
    "crypto", "database", "debug", "embed", "encoding", "errors", "expvar",
    "flag", "fmt", "go", "hash", "html", "image", "index", "io", "iter",
    "log", "maps", "math", "mime", "net", "os", "path", "plugin", "reflect",
    "regexp", "runtime", "slices", "sort", "strconv", "strings", "structs",
    "sync", "syscall", "testing", "text", "time", "unicode", "unique",
    "unsafe", "uuid", "weak",
]
# Common leaf names, for conditionals keyed via path.Base(pkg) (e.g. a
# switch on the base of "net/http" sees "http").
STDLIB_LEAVES = [
    "http", "url", "json", "tls", "smtp", "rpc", "template", "scanner",
    "aes", "sha256", "rand", "atomic",
]

# Non-test files allowed to carry package-name conditionals (narrowed
# 2026-10-16, R2-P2-21: the old "/test" substring exemption also matched
# "/testdata/", letting conditionals in compiled fixtures slip through).
PKG_COND_ALLOWLIST = {
    # file substring: reason
    "cmd/graalgo/testsynth.go":
        "test harness; resolves synthetic import paths (e.g. `if path == \"testing\"`)",
}


def _is_test_exempt(fname):
    if fname.endswith("_test.go") or fname.endswith("_test.java"):
        return True
    return any(sub in fname for sub in PKG_COND_ALLOWLIST)

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

    # 2. Package-name conditionals (stdlib names). Matches full stdlib paths
    # ("net/http"), bare tops ("fmt"), and leaf names ("http", via
    # path.Base switches); conditional forms: ==, case, .Name ==,
    # .equals() (Java); prefix/containment probes: strings.HasPrefix /
    # HasSuffix / Contains and Java startsWith/endsWith/contains; map
    # lookups keyed by a package path (special[pkgPath]); path.Base(pkg)
    # switches. Exemption is narrow: *_test.go/_test.java plus the explicit
    # PKG_COND_ALLOWLIST — a "/test" substring used to also exempt
    # "/testdata/", i.e. compiled fixtures (R2-P2-21).
    _top = "|".join(STDLIB_TOP)
    _leaf = "|".join(STDLIB_LEAVES)
    _path_lit = rf'"(?:{_top})(?:/[^"]*)?"'
    _leaf_lit = rf'"(?:{_leaf})"'
    _seg_lit = rf'"(?:{_top}|{_leaf})(?:/[^"]*)?"'
    pkg_cond_patterns = [
        rf'==\s*{_path_lit}',
        rf'==\s*{_leaf_lit}',
        rf'case\s*{_path_lit}',
        rf'case\s*{_leaf_lit}',
        rf'Name\s*==\s*{_path_lit}',
        rf'\.equals\s*\(\s*{_path_lit}\s*\)',
        rf'(?:HasPrefix|HasSuffix|Contains)\s*\([^,()]+,\s*{_seg_lit}\s*\)',
        rf'\.(?:startsWith|endsWith|contains)\s*\(\s*{_seg_lit}\s*\)',
        rf'path\.Base\s*\(\s*[A-Za-z_]*[Pp]kg',
        rf'switch\s+path\.Base\s*\(',
        rf'\w+\[[A-Za-z_]*[Pp]kg[Pp]ath\w*\]',
    ]
    for fname, line in added_lines:
        if not fname or not (fname.endswith(".go") or fname.endswith(".java")):
            continue
        for pat in pkg_cond_patterns:
            if re.search(pat, line):
                # Allow in test files and the explicit allowlist only.
                if not _is_test_exempt(fname):
                    errors.append(f"PACKAGE-NAME CONDITIONAL in {fname}: {line.strip()[:80]}")
                break

    # 3. New panic stubs. The old regex only caught literal "not implemented"/
    #    "unsupported"/"todo" messages; fail-fast stubs also hide behind bare
    #    panic(), panic(""), panic("unreachable"), composed messages like
    #    panic(fmt.Sprintf("... not implemented")), non-keyword composed
    #    messages (panic(errors.New("operation failed"))), and non-panic
    #    fail-fast (log.Fatal/log.Fatalf/os.Exit — os.Exit is unrecoverable
    #    by definition). Fail loud on all of them — a human decides whether
    #    an added fail-fast is a legitimate invariant. log.Fatal/os.Exit in
    #    cmd/ and tools/ CLIs are legitimate (a CLI's job is to exit); the
    #    stub-keyword variants are flagged everywhere.
    _stub_kw = (r'not implemented|unimplemented|not supported|unsupported|'
                r'todo|FIXME|unreachable|NYI|not yet|tbd|stub')
    # (pattern, allowed_in_cli_dirs)
    panic_stub_res = [
        (r'panic\s*\(\s*\)', False),            # bare panic() — nothing to go on
        (r'panic\s*\(\s*""\s*\)', False),       # panic("") — nothing to go on
        (rf'panic\s*\(\s*"({_stub_kw})', False),
        # Stub keyword anywhere in the first string arg of a composed panic.
        (rf'panic\s*\(\s*(?:fmt\.Sprintf|fmt\.Errorf|errors\.New)\s*\(\s*"[^"]*({_stub_kw})', False),
        # Composed-message panic with NO stub keyword: still fail-fast by
        # construction — the message wording is not a safety property.
        (r'panic\s*\(\s*(?:errors\.New|fmt\.Errorf)\s*\(', False),
        # log.Fatal / log.Fatalf call os.Exit: fail-fast in disguise.
        (rf'log\.Fatalf?\s*\(\s*"[^"]*({_stub_kw})', False),
        (r'log\.Fatalf?\s*\(', True),
        (r'os\.Exit\s*\(', True),
    ]
    _cli_dirs = ("/cmd/", "/tools/")
    _cli_prefixes = ("cmd/", "tools/")

    def _is_cli_file(fname):
        return (fname.startswith(_cli_prefixes)
                or any(d in fname for d in _cli_dirs))

    for fname, line in added_lines:
        if not fname or not fname.endswith(".go"):
            continue
        for pat, cli_ok in panic_stub_res:
            if re.search(pat, line, re.I):
                if cli_ok and _is_cli_file(fname):
                    continue  # legitimate CLI exit path; try remaining patterns
                errors.append(f"PANIC STUB in {fname}: {line.strip()[:80]}")
                break

    # 4. New wire node kinds for stdlib. The old gate watched only
    #    e.node("...") in export.go for 5 prefixes (runtime|fmt|sync|os|net):
    #    e.node("jsonmarshal"), dynamically composed e.node("sync" + name),
    #    and wire nodes registered from any other file all slipped through.
    #    Now: any e.node("literal") whose kind starts with a stdlib top-level
    #    name (full STDLIB_TOP list) is flagged, in ANY frontend .go file;
    #    string concatenation inside e.node(...) is always flagged. (The
    #    stronger alternative — flagging every new e.node literal — was
    #    considered; generic new kinds are already pinned by TestGolden and
    #    the wire-format validator, so stdlib-prefixed + dynamic is the
    #    bridge-relevant surface.)
    for fname, line in added_lines:
        if not fname or not fname.endswith(".go"):
            continue
        if "internal/frontend" not in fname and "export" not in fname:
            continue
        code = line.split("//")[0]  # ignore trailing comments for the concat check
        m = re.search(r'e\.node\s*\(\s*"([^"]+)"', code)
        if m:
            node_kind = m.group(1)
            # Flag runtime*/fmt*/sync*/etc. node kinds (stdlib-specific).
            # Both top-level names ("encoding" in "encodingjson...") and
            # leaf names ("json" in "jsonmarshal") count: kind strings are
            # lowercase concatenations, so either segment may lead.
            if any(node_kind == t or node_kind.startswith(t)
                   for t in STDLIB_TOP + STDLIB_LEAVES):
                # Adjudicated real implementations are not bridges.
                if any(f_sub in fname and nk == node_kind
                       for f_sub, nk in ADJUDICATED_WIRE_NODES):
                    continue
                errors.append(f"SUSPICIOUS WIRE NODE in {fname}: {node_kind}")
                continue
        # Dynamically composed node kind: e.node("sync" + name) and friends.
        if re.search(r'e\.node\s*\(.*\+', code):
            errors.append(f"DYNAMIC WIRE NODE in {fname}: {line.strip()[:80]}")

    # 5. New synthetic function registrations. The old gate matched only
    #    addFunc(/synthetic.*Package(/registerSynthetic: registrations via
    #    defineFunc/addBuiltin/registerBuiltin/declareFunc and new map-literal
    #    entries with synthetic-valued identifiers ("NewReader":
    #    syntheticNewReader,) slipped through. Grandfathered synth_*.go files
    #    stay exempt (growth inside them is R2-P1-5's ledger lane).
    registration_res = [
        r'addFunc\s*\(',
        r'defineFunc\s*\(',
        r'addBuiltin\s*\(',
        r'registerBuiltin\s*\(',
        r'declareFunc\s*\(',
        r'synthetic.*Package\s*\(',
        r'registerSynthetic',
    ]
    for fname, line in added_lines:
        if not fname or not fname.endswith(".go"):
            continue
        # Allow in existing synth files (grandfathered), flag in new contexts
        if "synth_" in fname:
            continue
        code = line.split("//")[0]
        hit = any(re.search(pat, code) for pat in registration_res)
        # New map-literal entry whose value identifier is synthetic-flavored:
        #   "NewReader": syntheticNewReader,
        if re.search(r'"[^"]+"\s*:\s*[A-Za-z0-9_]*[Ss]ynthetic[A-Za-z0-9_]*\s*,?\s*$', code):
            hit = True
        if hit:
            # Adjudicated real implementations are not bridges.
            m = re.search(r'(?:addFunc|defineFunc|addBuiltin|registerBuiltin|declareFunc)\s*\(\s*"([^"]+)"', code)
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
