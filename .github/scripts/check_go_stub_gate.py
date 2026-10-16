#!/usr/bin/env python3
r"""
Go-side stub gate (2026-10-16, review finding MECH-1).

The placeholder grep gates in internal/backend/{truffle,runtime,interop}/Makefile
(`TODO|FIXME|XXX|panic("not implemented")|throw new UnsupportedOperationException("stub`)
scan only the Java src/ trees. Nothing scanned the Go sources, so the repo's
largest fail-fast stub class — 313 `panic("<pkg>.<sym>: not in M<k> scope (...)")`
stubs across internal/frontend/synth_*.go — was mechanically invisible to
`make check`. This gate closes that hole. It runs in the top-level `make check`
(hence in the per-push `make all`).

Scope: every *.go file under internal/frontend/, EXCLUDING:
  - internal/frontend/go/        — vendored GOROOT tree (byte-identity diff-gated
                                   by internal/frontend/build.sh's vendoring gate;
                                   a naive TODO grep trips hundreds of upstream
                                   markers there)
  - any testdata/ directory      — Go programs fed TO the frontend (test inputs,
                                   not frontend sources; the go tool itself
                                   ignores testdata directories)

Failing patterns (any hit fails the gate; file:line:text is printed):
  1. panic("...not in M<k> scope...") — the milestone-scoping fail-fast stubs.
     ZERO TOLERANCE: not allowlistable. A stub that ships is a bodge, full stop.
     The <k> is any milestone digit (M2/M5/M7/M8/M9 today); the pattern is
     future-proof against new milestone-scoped stubs.
  2. panic("not implemented"           — parity with the Java placeholder gate.
  3. panic("TODO"                      — parity with the Java placeholder gate.
  4. FIXME                             — placeholder marker; no legitimate use
     in the tree (checked in hand-written files only, like TODO).
  5. TODO in hand-written files        — graalgo-authored TODOs that paper over
     problems. Generated files (synth_*.go, zz_*_gen.go, or a "Code generated"
     marker) are excluded: their TODOs are upstream-carried (embedded GOROOT
     source text and comments). The "TODO" string literal (e.g.
     insertFunc("TODO", ...) — the real context.TODO API) and the .TODO
     identifier form are excluded. Anything else may be allowlisted below,
     ONLY with a documented reason and owner. The allowlist is for accepted
     tech debt, never for stubs (patterns 1-3 are not allowlistable).

Deliberately NOT gated:
  - XXX: every occurrence is legitimate (pXXXCurve wrapper names, \uXXXX
    escapes in comments).
  - panic("unreachable"): upstream-idiomatic for impossible branches.
  - scopeErrorf("... not in M<k> scope"): honest user-facing scope errors
    returned by the exporter, not fail-fast stubs (the gate requires the
    panic(" prefix).
  - Bare "unimplemented": the exporter's honest unimplemented-field mechanism
    and gc-parity panics use the word legitimately.

Usage: python3 .github/scripts/check_go_stub_gate.py [--root <repo-root>]
Exit 0 = clean, 1 = violations found. Never passes vacuously: a missing
internal/frontend tree is an error, not a pass.
"""

import argparse
import os
import re
import sys

# --- patterns ---------------------------------------------------------------

# 1: milestone-scoping fail-fast stubs. The panic(" prefix is load-bearing:
# it separates the stubs from the honest scopeErrorf("... not in M<k> scope")
# errors in export.go and the test assertions on those errors.
FAILFAST_SCOPE = re.compile(r'panic\("[^"\n]*not in M[0-9]+ scope')
# 2, 3: Java-gate parity literals.
FAILFAST_LITERAL = re.compile(r'panic\("(not implemented|TODO)"')
# 4: placeholder marker.
FIXME = re.compile(r'FIXME')
# 5: graalgo-authored TODO markers (checked only in hand-written files).
TODO = re.compile(r'TODO')

# --- allowlist ----------------------------------------------------------------
# Accepted tech debt ONLY: (path suffix, substring, reason + owner).
# Stubs (patterns 1-3) can NEVER be allowlisted — remove the stub instead.
# An entry that matches nothing warns loudly (stale entry) but does not fail.
ALLOWLIST = [
    (
        "internal/frontend/v2/emit/emit.go",
        "TODO(v2): track source lines",
        "Accepted: the v1 wire format carries no rune columns, so exact "
        "columns are a v2-emitter precision follow-up, not a correctness gap. "
        "Owner: frontend v2 track.",
    ),
]

GENERATED_BASENAMES = ("synth_",)  # synth_*.go in any directory
GENERATED_SUFFIXES = ("_gen.go",)   # zz_*_gen.go etc.
# Excluded directory names. "go" is the top-level vendored GOROOT tree only;
# "testdata" is excluded at any depth (Go convention: test input fixtures).
EXCLUDED_TOP_DIRS = ("go",)
EXCLUDED_ANY_DIRS = ("testdata",)


def is_generated(path):
    base = os.path.basename(path)
    if base.startswith(GENERATED_BASENAMES) or base.endswith(GENERATED_SUFFIXES):
        return True
    try:
        with open(path, "r", encoding="utf-8", errors="replace") as f:
            for i, line in enumerate(f):
                if i >= 5:
                    break
                if "Code generated" in line and "DO NOT EDIT" in line:
                    return True
    except OSError:
        pass
    return False


def iter_go_files(root):
    frontend = os.path.join(root, "internal", "frontend")
    if not os.path.isdir(frontend):
        print(f"STUB GATE ERROR: {frontend} is not a directory", file=sys.stderr)
        sys.exit(2)
    for dirpath, dirnames, filenames in os.walk(frontend):
        rel = os.path.relpath(dirpath, frontend)
        parts = rel.split(os.sep)
        if parts[0] in EXCLUDED_TOP_DIRS or \
                any(p in EXCLUDED_ANY_DIRS for p in parts):
            dirnames[:] = []
            continue
        for name in sorted(filenames):
            if name.endswith(".go"):
                yield os.path.join(dirpath, name)


def todo_violation(line):
    """A TODO match that is graalgo's own marker, not an API reference.

    Excludes the "TODO" string literal (e.g. insertFunc("TODO", ...) — the
    real context.TODO API surface) and the .TODO identifier form
    (context.TODO). A TODO inside any other string (e.g. "TODO: fix later")
    still counts: markers do not belong in strings either.
    """
    for m in TODO.finditer(line):
        s, e = m.start(), m.end()
        if s > 0 and line[s - 1] == ".":
            continue  # context.TODO — the real API
        if s > 0 and e < len(line) and line[s - 1] == '"' and line[e] == '"':
            continue  # the "TODO" string literal — API name under test
        return m.group(0)
    return None


def check_file(path, rel):
    violations = []  # (lineno, pattern_name, text)
    generated = is_generated(path)
    try:
        with open(path, "r", encoding="utf-8", errors="replace") as f:
            for lineno, line in enumerate(f, 1):
                text = line.rstrip("\n")
                m = FAILFAST_SCOPE.search(text)
                if m:
                    violations.append((lineno, "fail-fast scope stub", m.group(0)))
                    continue
                m = FAILFAST_LITERAL.search(text)
                if m:
                    violations.append((lineno, "fail-fast stub", m.group(0)))
                    continue
                # Placeholder markers are graalgo-authored concerns: generated
                # files carry upstream's markers, not ours.
                if not generated and FIXME.search(text):
                    violations.append((lineno, "FIXME marker", "FIXME"))
                    continue
                if not generated and todo_violation(text):
                    violations.append((lineno, "TODO marker", text.strip()[:120]))
    except OSError as e:
        print(f"STUB GATE ERROR: cannot read {rel}: {e}", file=sys.stderr)
        sys.exit(2)
    return violations


def main():
    ap = argparse.ArgumentParser(description="Go-side stub gate (review MECH-1).")
    ap.add_argument("--root", default=None,
                    help="repo root (default: derived from this script's location)")
    args = ap.parse_args()
    root = args.root or os.path.dirname(
        os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

    violations = []  # (rel, lineno, pattern_name, text)
    scanned = 0
    for path in iter_go_files(root):
        scanned += 1
        rel = os.path.relpath(path, root)
        for lineno, name, text in check_file(path, rel):
            violations.append((rel, lineno, name, text))

    # Apply the allowlist (TODO markers only — stubs are never allowlisted).
    allow_used = [False] * len(ALLOWLIST)
    kept = []
    for rel, lineno, name, text in violations:
        if name != "TODO marker":
            kept.append((rel, lineno, name, text))
            continue
        allowed = False
        for i, (suffix, substr, _reason) in enumerate(ALLOWLIST):
            if rel.endswith(suffix) and substr in text:
                allowed = True
                allow_used[i] = True
                break
        if not allowed:
            kept.append((rel, lineno, name, text))

    for (suffix, _substr, _reason), used in zip(ALLOWLIST, allow_used):
        if not used:
            print(f"STUB GATE WARNING: allowlist entry for {suffix} matched "
                  f"nothing (stale entry?)", file=sys.stderr)

    print(f"stub gate: scanned {scanned} Go files under internal/frontend/ "
          f"(excl. the vendored go/ tree and testdata/ dirs)", file=sys.stderr)
    if kept:
        print("GO STUB GATE FAILED:", file=sys.stderr)
        for rel, lineno, name, text in kept:
            print(f"  {rel}:{lineno}: [{name}] {text}")
        print(f"{len(kept)} violation(s); "
              f"scope stubs are zero-tolerance (no allowlist)", file=sys.stderr)
        return 1
    print("go stub gate clean", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
