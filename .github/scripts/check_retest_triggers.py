#!/usr/bin/env python3
"""Fail CI when a re-test trigger names a package that has already landed.

REVIEW-2026-11-03-16 S9: `path.Match` and `text/scanner` kept
"re-test trigger: <package> lands" comments long after `unicode` and
`unicode/utf8` landed in `syntheticSources`, so the re-ports they were
waiting for never happened. This check makes that class of staleness
mechanical: a trigger comment whose landing condition has already fired
fails the build until the limitation is removed (re-ported) or the
trigger is adjudicated (rewritten as a documented limitation with a
tracked follow-up — the trigger has then served its purpose and is no
longer "pending").

Rule (deliberately narrow — see below for what it does NOT flag):
  a comment line in `internal/frontend/synth_*.go` (not `*_test.go`)
  that contains the word "trigger" AND the word "lands" AND names a
  package from `syntheticSources` (in `synth_machinery.go`) BEFORE the
  word "lands" is a violation.

Why this shape, and what it intentionally does not flag:
  - The landing phrasing ("when io lands", "math/big lands", "once
    that lands") is the trigger family whose event is checkable from
    the tree: the package is either in `syntheticSources` or not.
  - Triggers conditioned on something else are NOT flagged, because
    their event is not a package landing: "WS D lands" (a frontend
    workstream), "frontend struct-literal bug fixed" (a compiler bug),
    "a full math/rand port" (a completeness bar, not a landing —
    math/rand is in the map but is not a full port), "time multi-result
    desugar" (a desugar gap in an already-landed package). Those stay
    as ordinary review-tracked comments.
  - A package name appearing as an English word ("single bytes") is
    not a trigger condition and is not flagged: the "lands" word must
    be present.
  - Panic strings are not comments and are not flagged: the trigger
    word must appear in a `//` comment on the line.
  - Commented-out map entries (`// "reflect": ...`) do not count as
    landed packages.

Positive control: before the S9-extension dispositions, this script
flagged 8 lines (encoding/ascii85, encoding/binary, encoding/hex,
encoding/pem, encoding/gob x2, crypto/rand, io/fs) whose landing
conditions (io, math/big, os) had fired. Those lines were adjudicated
one by one and rewritten as documented limitations with follow-ups;
this script passes on the tree that resulted.

Usage: python3 .github/scripts/check_retest_triggers.py
Exit 0 when no violation is found, 1 otherwise. Dependency-free.
"""

import glob
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(HERE))
FRONTEND = os.path.join(ROOT, "internal", "frontend")
MACHINERY = os.path.join(FRONTEND, "synth_machinery.go")

TRIGGER_RE = re.compile(r"trigger", re.IGNORECASE)
LANDS_RE = re.compile(r"\blands\b", re.IGNORECASE)


def landed_packages():
    """Parse the keys of the syntheticSources map literal."""
    with open(MACHINERY) as f:
        lines = f.read().split("\n")
    start = next(
        i for i, line in enumerate(lines)
        if line.startswith("var syntheticSources")
    )
    pkgs = set()
    for line in lines[start + 1:]:
        if line == "}":
            break
        stripped = line.strip()
        if stripped.startswith("//"):
            continue  # commented-out entry: not landed
        m = re.match(r'"([^"]+)":', stripped)
        if m:
            pkgs.add(m.group(1))
    if not pkgs:
        raise SystemExit("check_retest_triggers: no packages parsed "
                         "from syntheticSources (did the format change?)")
    return pkgs


def token_before(line, pkg, lands_at):
    """True if pkg occurs as a whole token strictly before lands_at."""
    pat = re.compile(
        r"(?<![A-Za-z0-9_./-])" + re.escape(pkg) + r"(?![A-Za-z0-9_./-])"
    )
    return any(m.start() < lands_at for m in pat.finditer(line))


def check():
    landed = landed_packages()
    violations = []
    files = sorted(glob.glob(os.path.join(FRONTEND, "synth_*.go")))
    files = [f for f in files if not f.endswith("_test.go")]
    for path in files:
        with open(path) as f:
            for lineno, line in enumerate(f.read().split("\n"), 1):
                comment_at = line.find("//")
                trigger_m = TRIGGER_RE.search(line)
                if comment_at < 0 or trigger_m is None:
                    continue
                if trigger_m.start() < comment_at:
                    continue  # trigger word before the comment: not a comment
                lands_m = LANDS_RE.search(line)
                if lands_m is None:
                    continue
                for pkg in sorted(landed):
                    if token_before(line, pkg, lands_m.start()):
                        violations.append((path, lineno, pkg, line.strip()))
                        break
    return violations


def main():
    violations = check()
    if not violations:
        print("Retest-trigger check passed: no trigger comment names "
              "an already-landed package.")
        return 0
    print("RETEST-TRIGGER CHECK FAILED:")
    print()
    for path, lineno, pkg, line in violations:
        rel = os.path.relpath(path, ROOT)
        print(f"  ✗ {rel}:{lineno}: trigger names landed package "
              f"'{pkg}': {line[:100]}")
    print()
    print("The landing condition has fired: re-port the limitation or "
          "adjudicate the trigger (rewrite it as a documented "
          "limitation with a tracked follow-up).")
    return 1


if __name__ == "__main__":
    sys.exit(main())
