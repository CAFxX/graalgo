#!/usr/bin/env python3
"""Tracked-file size/binary gate (make check-repo).

Fails if any git-tracked file
  - is larger than LIMIT (1 MiB), or
  - is an ELF object (executable / shared library), a .so, or a .pyc,
unless its path matches an entry of the allowlist
(.github/scripts/large_files_allowlist.txt: one glob per line, then
`#` and the reason; a line without a reason is rejected).

It also caps run-log debris per tree: the tracked log files
(LOG_SUFFIXES) under any 3-component directory (e.g. docs/arch/spikes)
may total at most TREE_LOG_CAP (1.5 MiB) — 298 spike logs (19.8 MB) had
accumulated one small file at a time under the per-file limit. Logs a
report cites stay; the rest live in history and in commit messages.

Binaries, archives, recordings and multi-MB logs are build outputs or
debris: they belong in commit messages, scratch, or a download step, never
in git (AGENTS.md §7).

Usage: check_tracked_files.py [repo-root]
"""

import fnmatch
import subprocess
import sys
from pathlib import Path

LIMIT = 1024 * 1024
TREE_LOG_CAP = 1536 * 1024
LOG_SUFFIXES = (".log", ".wraplog", ".out", ".jfr")
BANNED_SUFFIXES = (".so", ".pyc")
ALLOWLIST = Path(".github/scripts/large_files_allowlist.txt")


def load_allowlist(root):
    path = root / ALLOWLIST
    globs, errors = [], []
    if not path.exists():
        return globs, errors
    for n, line in enumerate(path.read_text().splitlines(), 1):
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        pat, sep, reason = line.partition("#")
        if not sep or not reason.strip():
            errors.append(f"{ALLOWLIST}:{n}: entry without a '# reason': {line}")
            continue
        globs.append(pat.strip())
    return globs, errors


def tracked(root):
    out = subprocess.run(["git", "-C", str(root), "ls-files", "-z"],
                         check=True, capture_output=True).stdout
    return [p for p in out.decode().split("\0") if p]


def scan(root, files=None):
    root = Path(root)
    globs, errors = load_allowlist(root)
    used = set()
    for rel in (files if files is not None else tracked(root)):
        p = root / rel
        if not p.is_file() or p.is_symlink():
            continue
        allowed = [g for g in globs if fnmatch.fnmatch(rel, g)]
        why = []
        size = p.stat().st_size
        if size > LIMIT:
            why.append(f"{size} bytes > {LIMIT}")
        if rel.endswith(BANNED_SUFFIXES):
            why.append("banned file type (.so/.pyc)")
        else:
            with p.open("rb") as f:
                if f.read(4) == b"\x7fELF":
                    why.append("ELF binary")
        if why:
            if allowed:
                used.update(allowed)
            else:
                errors.append(f"{rel}: {', '.join(why)}")
    trees = {}
    for rel in (files if files is not None else tracked(root)):
        p = root / rel
        if rel.endswith(LOG_SUFFIXES) and p.is_file() and not p.is_symlink():
            tree = "/".join(rel.split("/")[:3]) if rel.count("/") >= 3 else "/".join(rel.split("/")[:-1]) or "."
            trees[tree] = trees.get(tree, 0) + p.stat().st_size
    for tree, total in sorted(trees.items()):
        if total > TREE_LOG_CAP:
            errors.append(f"{tree}/: tracked logs total {total} bytes > {TREE_LOG_CAP} "
                          f"(prune what no report cites; history keeps it)")
    for g in globs:
        if g not in used:
            errors.append(f"{ALLOWLIST}: stale entry '{g}' matches no offending tracked file")
    return errors


def main(argv):
    root = Path(argv[1]) if len(argv) > 1 else Path(__file__).resolve().parents[2]
    errors = scan(root)
    if errors:
        print("TRACKED-FILE GATE FAILED (size > 1 MiB, ELF, .so, .pyc or a tree's logs over the cap; allowlist: "
              f"{ALLOWLIST}):")
        for e in errors:
            print("  " + e)
        return 1
    print("tracked-file gate clean")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
