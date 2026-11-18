#!/usr/bin/env python3
"""Ratcheted debt gates (make check-repo).

Two per-file counters, each with a committed budget in
.github/scripts/ratchet_budget.json:

  placeholder    TODO / FIXME / XXX markers in tracked Go, Python and shell
                 sources (the Java module gates already hold Java at zero).
  unimplemented  string literals that emit an "unimplemented" /
                 "not implemented" runtime stub in shipped Go/Java code
                 under internal/ (tests excluded): each one is a program
                 that fails at run time instead of at build time.

The ratchet only goes down:
  - a file whose count exceeds its budget (or a new file with any) FAILS;
  - a file whose count dropped below its budget also FAILS until the
    budget is lowered (`check_ratchets.py --update` rewrites the budget to
    the current counts and refuses to raise any entry), so a paid-down
    debt can never silently grow back.

Usage: check_ratchets.py [--update] [repo-root]
"""

import json
import re
import subprocess
import sys
from pathlib import Path

BUDGET = Path(".github/scripts/ratchet_budget.json")
SKIP = ("internal/frontend/go/", "docs/arch/", "research/")

COUNTERS = {
    "placeholder": {
        "files": re.compile(r"\.(go|py|sh)$"),
        "exclude": re.compile(r"^\.github/scripts/(check|test_check)_ratchets\.py$"),
        "pattern": re.compile(r"\b(TODO|FIXME|XXX)\b"),
    },
    "unimplemented": {
        "files": re.compile(r"^internal/.*\.(go|java)$"),
        "exclude": re.compile(r"(_test\.go$|/src/test/)"),
        "pattern": re.compile(r'"[^"\n]*(?i:unimplemented|not implemented)[^"\n]*"'),
    },
}


def tracked(root):
    out = subprocess.run(["git", "-C", str(root), "ls-files", "-z"], check=True,
                         capture_output=True).stdout.decode()
    return [p for p in out.split("\0") if p and not p.startswith(SKIP)]


def count(root, files):
    counts = {k: {} for k in COUNTERS}
    for rel in files:
        p = root / rel
        if not p.is_file():
            continue
        text = None
        for name, c in COUNTERS.items():
            if not c["files"].search(rel) or c["exclude"].search(rel):
                continue
            if text is None:
                text = p.read_text(errors="replace")
            n = len(c["pattern"].findall(text))
            if n:
                counts[name][rel] = n
    return counts


def compare(counts, budget):
    errors = []
    for name in COUNTERS:
        have, allow = counts.get(name, {}), budget.get(name, {})
        for rel in sorted(set(have) | set(allow)):
            h, a = have.get(rel, 0), allow.get(rel, 0)
            if h > a:
                errors.append(f"{name}: {rel}: {h} > budget {a} (debt may only go down)")
            elif h < a:
                errors.append(f"{name}: {rel}: {h} < budget {a} — paid down: lower the budget "
                              f"(check_ratchets.py --update) to lock it in")
    return errors


def main(argv):
    update = "--update" in argv
    args = [a for a in argv[1:] if not a.startswith("--")]
    root = Path(args[0]) if args else Path(__file__).resolve().parents[2]
    counts = count(root, tracked(root))
    bpath = root / BUDGET
    budget = json.loads(bpath.read_text()) if bpath.exists() else {}
    if update:
        raised = [e for e in compare(counts, budget) if " > budget " in e]
        if raised and budget:
            print("RATCHET: refusing to raise budgets:")
            for e in raised:
                print("  " + e)
            return 1
        bpath.write_text(json.dumps(counts, indent=1, sort_keys=True) + "\n")
        print(f"ratchet budget written: {bpath}")
        return 0
    errors = compare(counts, budget)
    if errors:
        print(f"RATCHET GATE FAILED ({BUDGET}):")
        for e in errors:
            print("  " + e)
        return 1
    totals = ", ".join(f"{k}={sum(v.values())}" for k, v in counts.items())
    print(f"ratchet gate clean ({totals})")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
