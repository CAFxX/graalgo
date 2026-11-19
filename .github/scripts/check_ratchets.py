#!/usr/bin/env python3
"""Ratcheted debt gates (make check-repo).

Two per-file counters, each with a committed budget in
.github/scripts/ratchet_budget.json:

  placeholder    TODO / FIXME / XXX markers in tracked Go, Python and shell
                 sources (the Java module gates already hold Java at zero).
  unimplemented  stub sites in shipped Go/Java code under internal/ (tests
                 excluded): string literals that emit an "unimplemented" /
                 "not implemented" runtime stub (each one is a program that
                 fails at run time instead of at build time), and the new
                 path's equivalents — `new Unsupported(` in the new node
                 package (graalgo: not supported by the new node package
                 yet) and `.unsupportedf(` in the toolexec emitter
                 (unsupported: ...).

The ratchet only goes down, in the tree and against the git base:
  - a file whose count exceeds its budget (or a new file with any) FAILS;
  - a file whose count dropped below its budget also FAILS until the
    budget is lowered (`check_ratchets.py --update` rewrites the budget to
    the current counts and refuses to raise any entry), so a paid-down
    debt can never silently grow back;
  - a budget entry above the same entry at the base commit FAILS (the
    base is $GATE_BASE — CI: the push's `before` sha or the PR base — else
    the merge-base of HEAD with origin/master, else master), so a commit
    cannot raise the budget file by hand. A counter whose definition
    (files/exclude/pattern) changed since the base is re-baselined by that
    change and exempt from the base comparison; the definition is recorded
    in the budget file under "_definitions".

Usage: check_ratchets.py [--update] [repo-root]
"""

import json
import os
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
        "pattern": re.compile(
            r'"[^"\n]*(?i:unimplemented|not implemented)[^"\n]*"'
            r'|\bnew Unsupported\('
            r'|\.unsupportedf\('),
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


def definitions():
    """A stable text of each counter's definition (files, exclude, pattern)."""
    return {name: " ".join((c["files"].pattern, c["exclude"].pattern, c["pattern"].pattern))
            for name, c in COUNTERS.items()}


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


ZERO_SHA = re.compile(r"^0+$")


def git(root, *args):
    r = subprocess.run(["git", "-C", str(root)] + list(args), capture_output=True, text=True)
    return r.stdout.strip() if r.returncode == 0 else None


def base_commit(root):
    """$GATE_BASE if it names a commit, else merge-base with origin/master
    (else master); None when nothing resolves."""
    env = os.environ.get("GATE_BASE", "")
    if env and not ZERO_SHA.match(env):
        sha = git(root, "rev-parse", "--verify", "--quiet", env + "^{commit}")
        if sha:
            return sha
    for up in ("origin/master", "master"):
        if git(root, "rev-parse", "--verify", "--quiet", up + "^{commit}"):
            mb = git(root, "merge-base", "HEAD", up)
            if mb:
                return mb
    return None


def compare_base(budget, base_budget):
    """Budget entries raised above the base's (per unchanged counter)."""
    errors = []
    base_defs = base_budget.get("_definitions", {})
    defs = definitions()
    for name in COUNTERS:
        if base_defs.get(name) != defs[name]:
            continue  # re-baselined by a definition change in this diff
        now, was = budget.get(name, {}), base_budget.get(name, {})
        for rel in sorted(now):
            if now[rel] > was.get(rel, 0):
                errors.append(f"{name}: {rel}: budget {now[rel]} > {was.get(rel, 0)} at the base "
                              f"commit (budgets may only go down)")
    return errors


def main(argv):
    update = "--update" in argv
    args = [a for a in argv[1:] if not a.startswith("--")]
    root = Path(args[0]) if args else Path(__file__).resolve().parents[2]
    counts = count(root, tracked(root))
    bpath = root / BUDGET
    budget = json.loads(bpath.read_text()) if bpath.exists() else {}
    defs = definitions()
    if update:
        old_defs = budget.get("_definitions", {})
        raised = [e for e in compare(counts, budget)
                  if " > budget " in e and old_defs.get(e.split(":", 1)[0]) == defs[e.split(":", 1)[0]]]
        if raised and budget:
            print("RATCHET: refusing to raise budgets:")
            for e in raised:
                print("  " + e)
            return 1
        out = dict(counts)
        out["_definitions"] = defs
        bpath.write_text(json.dumps(out, indent=1, sort_keys=True) + "\n")
        print(f"ratchet budget written: {bpath}")
        return 0
    errors = compare(counts, budget)
    if budget.get("_definitions", {}) != defs:
        errors.append("counter definitions changed since the budget was written: "
                      "re-baseline with check_ratchets.py --update")
    base = base_commit(root)
    if base:
        text = git(root, "show", f"{base}:{BUDGET.as_posix()}")
        if text:
            errors += compare_base(budget, json.loads(text))
    if errors:
        print(f"RATCHET GATE FAILED ({BUDGET}):")
        for e in errors:
            print("  " + e)
        return 1
    totals = ", ".join(f"{k}={sum(v.values())}" for k, v in counts.items())
    print(f"ratchet gate clean ({totals}; base {base[:12] if base else 'none'})")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
