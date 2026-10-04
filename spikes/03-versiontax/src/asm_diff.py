#!/usr/bin/env python3
"""Semantic diff of two asm inventory files (surface a).

Normalizes symbol names (strip package qualifiers, <>, (SB), +off(SB)),
compares symbol SETS per (pkg, file), and reports:
  - files added / removed
  - per-file symbols added / removed
  - ABI-flag flips (<ABIInternal> gained/lost)
Usage: asm_diff.py <old_inv> <new_inv> <out>
"""
import re, sys

def parse(path):
    inv = {}
    pkg = None
    with open(path) as f:
        for line in f:
            line = line.rstrip("\n")
            if line.startswith("=== PKG "):
                pkg = line[len("=== PKG "):-len(" ===")]
            elif line.startswith("  ") and pkg:
                parts = line.split()
                fn = parts[0]
                inv[(pkg, fn)] = parts[1:]
    return inv

BASE = re.compile(r"^(?:[a-z0-9_]+·)?(.*?)(?:<ABI\w+>)?(?:<>\(SB\)|\(SB\)|\+0\(SB\))?$")

def norm(sym):
    m = BASE.match(sym)
    base = m.group(1) if m else sym
    abi = "<ABIInternal>" in sym
    return base, abi

def main():
    old, new = parse(sys.argv[1]), parse(sys.argv[2])
    out = open(sys.argv[3], "w")
    def w(s): out.write(s + "\n")

    old_files, new_files = set(old), set(new)
    for k in sorted(new_files - old_files):
        w(f"FILE-ADDED   {k[0]}/{k[1]}")
    for k in sorted(old_files - new_files):
        w(f"FILE-REMOVED {k[0]}/{k[1]}")
    for k in sorted(old_files & new_files):
        osyms, nsyms = old[k], new[k]
        ob = {norm(s)[0]: s for s in osyms}
        nb = {norm(s)[0]: s for s in nsyms}
        added = sorted(set(nb) - set(ob))
        removed = sorted(set(ob) - set(nb))
        flips = [b for b in set(ob) & set(nb)
                 if norm(ob[b])[1] != norm(nb[b])[1]]
        if added or removed or flips:
            w(f"FILE-CHANGED {k[0]}/{k[1]}")
            for a in added:
                w(f"  + {nb[a]}")
            for r in removed:
                w(f"  - {ob[r]}")
            for f_ in sorted(flips):
                w(f"  ~ ABI-flip {ob[f_]} -> {nb[f_]}")
    out.close()

if __name__ == "__main__":
    main()
