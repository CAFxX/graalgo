#!/usr/bin/env python3
"""Root-Makefile consistency gate (make check-repo).

Fails when
  1. a target named like a command (`[a-z][a-z0-9-]*`) is missing from
     .PHONY, or .PHONY names a target that does not exist;
  2. the header's "`make all` = ..." list differs from the `all:` rule's
     prerequisites (the header is what humans and CI docs read);
  3. the generated-files list is wrong: a GENERATED/GENERATED_EXTERNAL
     entry that is neither tracked nor gitignored (stale), or a tracked
     file whose header carries Go's "Code generated ... DO NOT EDIT."
     marker but is in neither list (an unlisted generator output).

Usage: check_makefile.py [repo-root]
"""

import re
import subprocess
import sys
from pathlib import Path

CMD_TARGET = re.compile(r"^[a-z][a-z0-9-]*$")
RULE = re.compile(r"^([^\s#=:][^=:]*?)\s*:(?![=:])\s*(.*)$")
MARKER = re.compile(r"^(//|#) Code generated .*DO NOT EDIT")
# Vendored upstream / archived trees whose generated files are not ours.
NOT_OURS = ("internal/frontend/go/", "docs/archive/spikes/")


def logical_lines(text):
    buf = ""
    for line in text.splitlines():
        if line.endswith("\\"):
            buf += line[:-1] + " "
            continue
        yield buf + line
        buf = ""
    if buf:
        yield buf


def parse(text):
    targets, phony, rules, vars_ = set(), set(), {}, {}
    in_define = False
    for line in logical_lines(text):
        if line.startswith("define "):
            in_define = True
            continue
        if in_define:
            if line.strip() == "endef":
                in_define = False
            continue
        if line.startswith("\t") or line.lstrip().startswith("#"):
            continue
        m = re.match(r"^([A-Z_][A-Z0-9_]*)\s*(:=|\+=|\?=|=)\s*(.*)$", line)
        if m:
            name, op, val = m.groups()
            if op == "+=":
                vars_[name] = vars_.get(name, "") + " " + val
            else:
                vars_[name] = val
            continue
        m = RULE.match(line)
        if not m:
            continue
        names, prereqs = m.group(1).split(), m.group(2).split()
        for n in names:
            if n == ".PHONY":
                phony.update(prereqs)
            else:
                targets.add(n)
                rules.setdefault(n, prereqs)
    return targets, phony, rules, vars_


def expand(val, vars_, depth=0):
    if depth > 10:
        return val
    return re.sub(r"\$\(([A-Z_][A-Z0-9_]*)\)", lambda m: expand(vars_.get(m.group(1), ""), vars_, depth + 1), val)


def header_all(text):
    lines = text.splitlines()
    for i, line in enumerate(lines):
        m = re.match(r"^#\s+`make all` = (.*)$", line)
        if not m:
            continue
        words = m.group(1)
        j = i
        while not words.rstrip().endswith(":") and j + 1 < len(lines):
            j += 1
            words += " " + lines[j].lstrip("#")
        return words.rstrip().rstrip(":").split()
    return None


def check(text, tracked, ignored, file_head):
    """tracked: set of tracked paths; ignored(path)->bool; file_head(path)->str."""
    errors = []
    targets, phony, rules, vars_ = parse(text)
    for t in sorted(targets):
        if CMD_TARGET.match(t) and t not in phony:
            errors.append(f".PHONY is missing target '{t}'")
    for p in sorted(phony - targets):
        errors.append(f".PHONY names '{p}', which is not a target")
    hdr = header_all(text)
    if hdr is None:
        errors.append("header has no \"`make all` = ...\" line")
    elif hdr != rules.get("all", []):
        errors.append(f"header `make all` = {' '.join(hdr)} != all: {' '.join(rules.get('all', []))}")
    gen = set(expand(vars_.get("GENERATED", ""), vars_).split())
    ext = set(expand(vars_.get("GENERATED_EXTERNAL", ""), vars_).split())
    if not gen:
        errors.append("GENERATED is empty or missing")
    for p in sorted(gen | ext):
        if p not in tracked and not ignored(p):
            errors.append(f"generated-files list names '{p}', which is neither tracked nor gitignored (stale)")
    for p in sorted(tracked):
        if p.startswith(NOT_OURS) or p in gen or p in ext:
            continue
        if any(MARKER.match(l) for l in file_head(p)):
            errors.append(f"'{p}' has a 'Code generated ... DO NOT EDIT' header but is not in GENERATED/GENERATED_EXTERNAL")
    return errors


def main(argv):
    root = Path(argv[1]) if len(argv) > 1 else Path(__file__).resolve().parents[2]
    text = (root / "Makefile").read_text()
    out = subprocess.run(["git", "-C", str(root), "ls-files", "-z"], check=True,
                         capture_output=True).stdout.decode()
    tracked = {p for p in out.split("\0") if p}

    def ignored(p):
        return subprocess.run(["git", "-C", str(root), "check-ignore", "-q", "--no-index", p]).returncode == 0

    def head(p):
        try:
            with open(root / p, "rb") as f:
                return f.read(2048).decode(errors="replace").splitlines()[:5]
        except OSError:
            return []

    errors = check(text, tracked, ignored, head)
    if errors:
        print("MAKEFILE GATE FAILED:")
        for e in errors:
            print("  " + e)
        return 1
    print("makefile gate clean")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
