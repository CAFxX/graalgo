#!/usr/bin/env python3
"""Checks the Makefile's fast/slow Java test lists against the module
runners and the test sources:

  - every test class a module runner registers (TESTS.add(X.class)) must
    appear in exactly one of that module's lists, and every listed class
    must be registered;
  - every *Test.java under the runner's module src/test/java/ must be
    registered by some runner (a test class in no runner never runs —
    global review G1 found two);
  - every class in the quarantine file must be in a slow list (the fast
    lists are per-push and may not carry known failures).

Usage: check_lists.py [--quarantine FILE --slow "<slow classes>"] <Runner.java> "<listed classes>" [...]
Exit: 0 consistent, 1 mismatch, 2 usage.
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


def module_test_root(runner: Path):
    parts = runner.resolve().parts
    i = len(parts) - 1 - parts[::-1].index("java")
    return Path(*parts[: i + 1])


def test_sources(root: Path):
    out = []
    for p in sorted(root.rglob("*Test.java")):
        text = p.read_text(errors="replace")
        m = re.search(r"^package\s+([\w.]+);", text, re.M)
        out.append((f"{m.group(1)}.{p.stem}" if m else p.stem, p))
    return out


def read_quarantine(path: Path):
    """Lines 'class | reason | owner | expiry'; '#' comments."""
    entries = []
    for n, line in enumerate(path.read_text().splitlines(), 1):
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        fields = [f.strip() for f in line.split("|")]
        if len(fields) != 4 or not all(fields):
            raise ValueError(f"{path}:{n}: want 'class | reason | owner | expiry', got: {line}")
        entries.append(fields)
    return entries


def check(pairs, quarantine=None, slow=frozenset()):
    bad = []
    all_reg = set()
    roots = set()
    for runner, listed in pairs:
        reg = registered(runner)
        all_reg.update(reg)
        roots.add(module_test_root(runner))
        for c in reg:
            n = listed.count(c)
            if n != 1:
                bad.append(f"{runner}: {c} appears {n} times in the fast/slow lists (want exactly 1)")
        for c in listed:
            if c not in reg:
                bad.append(f"{runner}: listed class {c} is not registered in the runner")
    for root in sorted(roots):
        for fqn, p in test_sources(root):
            if fqn not in all_reg:
                bad.append(f"{p}: {fqn} is registered in no runner (it never runs): "
                           f"register and list it, or delete it")
    for cls, *_ in (quarantine or []):
        if cls not in slow:
            bad.append(f"quarantine: {cls} is not in a slow list (quarantine is for test-java-slow only)")
    return bad


def main(argv):
    args = argv[1:]
    quarantine, slow = None, frozenset()
    if args[:1] == ["--quarantine"]:
        quarantine = read_quarantine(Path(args[1]))
        args = args[2:]
    if args[:1] == ["--slow"]:
        slow = frozenset(args[1].split())
        args = args[2:]
    if not args or len(args) % 2:
        print(__doc__)
        return 2
    pairs = [(Path(args[i]), args[i + 1].split()) for i in range(0, len(args), 2)]
    bad = check(pairs, quarantine, slow)
    if bad:
        print("JAVA TEST LISTS INCONSISTENT:")
        for b in bad:
            print("  " + b)
        return 1
    print("java test lists consistent")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
