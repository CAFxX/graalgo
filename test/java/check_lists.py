#!/usr/bin/env python3
"""Checks the Makefile's fast/slow Java test lists against the module
runners: every test class a module runner registers (TESTS.add(X.class))
must appear in exactly one of the module's lists, and every listed class
must be registered. A new test class therefore cannot silently fall out of
both the per-push and the nightly run.

Usage: check_lists.py <Runner.java> "<listed classes>" [<Runner.java> "<...>"]...
Exit: 0 consistent, 1 mismatch.
"""

import re
import sys
from pathlib import Path


def registered(runner: Path):
    text = runner.read_text()
    pkg = re.search(r"^package\s+([\w.]+);", text, re.M).group(1)
    out = []
    for name in re.findall(r"TESTS\.add\(([\w.]+)\.class\)", text):
        out.append(name if "." in name else f"{pkg}.{name}")
    return out


def main(argv):
    args = argv[1:]
    if not args or len(args) % 2:
        print(__doc__)
        return 2
    bad = []
    for i in range(0, len(args), 2):
        runner, listed = Path(args[i]), args[i + 1].split()
        reg = registered(runner)
        for c in reg:
            n = listed.count(c)
            if n != 1:
                bad.append(f"{runner}: {c} appears {n} times in the fast/slow lists (want exactly 1)")
        for c in listed:
            if c not in reg:
                bad.append(f"{runner}: listed class {c} is not registered in the runner")
    if bad:
        print("JAVA TEST LISTS INCONSISTENT:")
        for b in bad:
            print("  " + b)
        return 1
    print("java test lists consistent")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
