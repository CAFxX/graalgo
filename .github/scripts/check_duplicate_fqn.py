#!/usr/bin/env python3
"""Duplicate-FQN gate (review R-01, 2026-10-31; tightened by T0.D, 2026-11-18).

Fails if any fully-qualified Java class name exists in more than one
backend module's main source tree. Two classes with one FQN on one
classpath means the class that runs is decided by classpath order — the
"same-FQN stub overlay" that T0.D removed (8 phase-1 stub classes shadowed
by their runtime replacements). There is no allowlist: no such pair may
come back.

Scope: the main source trees of the backend modules, truffle (the core
module) and interop.

Usage: python3 .github/scripts/check_duplicate_fqn.py  (from the repo root)
Exit 0 = no duplicates; exit 1 = gate failed (list printed).
"""

import sys
from collections import defaultdict
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
MODULES = ("truffle", "interop")


def scan_module(module):
    """Return {FQN: path} for a module's main source tree."""
    root = REPO_ROOT / "internal/backend" / module / "src/main/java"
    if not root.is_dir():
        print(f"DUPLICATE-FQN GATE FAILED: missing source tree: {root}")
        sys.exit(1)
    out = {}
    for path in sorted(root.rglob("*.java")):
        fqn = path.relative_to(root).with_suffix("").as_posix().replace("/", ".")
        out[fqn] = path
    return out


def main():
    by_fqn = defaultdict(dict)  # FQN -> {module: path}
    total = 0
    for module in MODULES:
        classes = scan_module(module)
        if not classes:
            print(f"DUPLICATE-FQN GATE FAILED: no sources in module {module} "
                  "(refusing to pass vacuously)")
            return 1
        total += len(classes)
        for fqn, path in classes.items():
            by_fqn[fqn][module] = path

    failures = [
        f"  {fqn} in {sorted(mods)}: "
        + ", ".join(str(mods[m]) for m in sorted(mods))
        for fqn, mods in sorted(by_fqn.items())
        if len(mods) > 1
    ]
    if failures:
        print("DUPLICATE-FQN GATE FAILED: same-FQN classes across backend "
              "modules (classpath order would decide which one runs):")
        print("\n".join(failures))
        return 1
    print(f"duplicate-FQN gate clean: {total} classes across "
          f"{'/'.join(MODULES)}, no FQN in two modules")
    return 0


if __name__ == "__main__":
    sys.exit(main())
