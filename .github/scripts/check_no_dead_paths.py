#!/usr/bin/env python3
"""Gate: executable paths must not reference the deleted pre-restructure
tree or the banned scratch directory.

The repository restructure deleted the research-era per-phase tree, and the
no-debris rule bans the old untracked scratch directory. Scripts that still
point there do not fail loudly: they report ClassNotFoundException as a
bogus divergence, or write evidence into a banned location. This gate
fails on any such reference in an executable path:

  - files under tools/, test/, .github/, internal/backend/ and the root
    Makefile, with extensions .sh .bash .py .go .mk .yml .yaml, Makefiles,
    Dockerfiles, and any file starting with a shebang;
  - comment lines (stripped line starting with '#' or '//') are skipped:
    historical prose is not an executable reference;
  - a line may opt out with the marker 'dead-path-ok' (use sparingly,
    for code that detects the banned names, e.g. this gate's tests).

Excluded: *.md, *.json, *.txt data/docs, testdata/ trees, this script and
its test, build output (out/, .tmp/).

Exit status: 0 clean, 1 hits found (each listed as file:line).
Run: python3 .github/scripts/check_no_dead_paths.py [repo-root]
"""

import os
import re
import sys
from pathlib import Path

ROOTS = ["tools", "test", ".github", "internal/backend"]
TOP_FILES = ["Makefile"]
EXTS = {".sh", ".bash", ".py", ".go", ".mk", ".yml", ".yaml"}
NAMES = {"Makefile", "Dockerfile"}
SKIP_DIRS = {".git", "out", ".tmp", "testdata", "__pycache__", "node_modules"}
SELF = {"check_no_dead_paths.py", "test_check_no_dead_paths.py"}
# Built from parts so this file does not match itself.
PATTERN = re.compile(r"\b" + "phases" + r"/|\b" + "hidden" + "_files")
OPT_OUT = "dead-path-ok"


def is_executable_path(path: Path) -> bool:
    if path.name in SELF:
        return False
    if path.suffix in EXTS or path.name in NAMES:
        return True
    if path.suffix in {".md", ".json", ".txt"}:
        return False
    try:
        with open(path, "rb") as f:
            return f.read(2) == b"#!"
    except OSError:
        return False


def candidate_files(root: Path):
    for top in TOP_FILES:
        p = root / top
        if p.is_file():
            yield p
    for r in ROOTS:
        base = root / r
        if not base.is_dir():
            continue
        for dirpath, dirnames, filenames in os.walk(base):
            dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS]
            for fn in filenames:
                p = Path(dirpath) / fn
                if is_executable_path(p):
                    yield p


def scan_text(text: str):
    """Yields (line number, line) of every non-comment hit."""
    for i, line in enumerate(text.splitlines(), 1):
        stripped = line.strip()
        if stripped.startswith("#") or stripped.startswith("//"):
            continue
        if OPT_OUT in line:
            continue
        if PATTERN.search(line):
            yield i, line


def scan(root: Path):
    hits = []
    for p in candidate_files(root):
        try:
            text = p.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        for n, line in scan_text(text):
            hits.append(f"{p.relative_to(root)}:{n}: {line.strip()[:160]}")
    return hits


def main(argv):
    root = Path(argv[1] if len(argv) > 1 else ".").resolve()
    hits = scan(root)
    if hits:
        print("DEAD-PATH GATE FAILED: executable references to the deleted "
              "per-phase tree or the banned scratch dir:")
        for h in hits:
            print("  " + h)
        return 1
    print("dead-path gate clean")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
