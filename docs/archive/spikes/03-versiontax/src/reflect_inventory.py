#!/usr/bin/env python3
"""Extract unsafe.<ident> matches from non-test src/reflect/*.go (surface b).

Same matching rules as spike 04's inventory_raw.txt:
  - non-test .go files that import unsafe
  - textual occurrences of unsafe.[A-Za-z0-9_]* (or `unsafe` as a type word)
  - matches inside // comments are separated into comment-only counts
Output: INV|<file>:<lineno>:\t<code> lines (one per matching line).
Usage: reflect_inventory.py <go_src_root> <out_raw>
"""
import os, re, sys

MATCH = re.compile(r'unsafe\.[A-Za-z0-9_]*')

def imports_unsafe(path):
    """True if the file has an import of the unsafe package (single-line or block)."""
    try:
        with open(path, errors="replace") as f:
            lines = f.readlines()
    except OSError:
        return False
    in_block = False
    for line in lines:
        s = line.strip()
        if s.startswith("import"):
            if "(" in s:
                in_block = True
                continue
            if re.match(r'^import\s+(?:[A-Za-z0-9_.]+\s+)?"unsafe"$', s):
                return True
        elif in_block:
            if s.startswith(")"):
                in_block = False
            elif re.match(r'^(?:[A-Za-z0-9_.]+\s+)?"unsafe"$', s):
                return True
    return False

def main():
    root, out = sys.argv[1], sys.argv[2]
    rdir = os.path.join(root, "src/reflect")
    rows = []
    for fn in sorted(os.listdir(rdir)):
        if not fn.endswith(".go") or fn.endswith("_test.go"):
            continue
        path = os.path.join(rdir, fn)
        if not imports_unsafe(path):
            continue
        with open(path, errors="replace") as f:
            for i, line in enumerate(f, 1):
                code = line.rstrip("\n")
                if MATCH.search(code) or re.search(r'\bunsafe\b', code):
                    rows.append((fn, i, code))
    with open(out, "w") as fh:
        for fn, i, code in rows:
            fh.write(f"INV|{fn}:{i}:\t{code}\n")
    print(f"{len(rows)} matching lines in {rdir}")

if __name__ == "__main__":
    main()
