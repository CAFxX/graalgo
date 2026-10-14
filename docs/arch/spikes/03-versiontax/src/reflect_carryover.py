#!/usr/bin/env python3
"""Carry spike-04's hand-read categories onto a 1.26.8 inventory (surface b).

Lines with text identical (whitespace-normalized) to a categorized 1.27.1
line inherit that category mechanically. Lines that changed between releases
(or never existed on 1.27.1) are emitted as the judgment work list.

Usage: reflect_carryover.py <cat_py> <raw_1.27.1> <raw_1.26.8> <out_dir>
Writes: <out_dir>/reflect_carryover.txt, <out_dir>/reflect_judgment.txt
"""
import collections, importlib.util, os, re, sys

MATCH = re.compile(r'unsafe\.[A-Za-z0-9_]*')

def load_raw(path):
    rows = {}
    with open(path) as f:
        for line in f:
            body = line.rstrip("\n").split("|", 1)[1]
            file_, rest = body.split(":", 1)
            ln_s, code = rest.split(":", 1)
            rows[(file_, int(ln_s))] = code
    return rows

def norm(code):
    return re.sub(r"\s+", " ", code).strip()

def is_import_or_comment(code):
    s = code.strip()
    if re.match(r'^(?:[A-Za-z0-9_.]+\s+)?"unsafe"$', s):
        return True
    if s.startswith("//"):
        # comment-only match
        return True
    return False

def main():
    cat_py, raw_new, raw_old, outdir = sys.argv[1:5]
    spec = importlib.util.spec_from_file_location("cat", cat_py)
    m = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(m)
    CAT = m.CAT

    new_raw = load_raw(raw_new)   # 1.27.1
    old_raw = load_raw(raw_old)   # 1.26.8

    # (file, normcode) -> category set, from 1.27.1 CAT keyed by line
    carry = collections.defaultdict(set)
    for (file_, lineno), code in new_raw.items():
        if (file_, lineno) in CAT:
            carry[(file_, norm(code))].add(CAT[(file_, lineno)])
        elif is_import_or_comment(code):
            carry[(file_, norm(code))].add("NONOP")
        else:
            carry[(file_, norm(code))].add("UNASSIGNED-1.27")

    counts_new = collections.Counter(CAT.values())
    counts_old = collections.Counter()
    judgment = []
    collisions = []
    for (file_, lineno) in sorted(old_raw):
        code = old_raw[(file_, lineno)]
        nc = norm(code)
        cats = carry.get((file_, nc))
        if cats is None:
            if is_import_or_comment(code):
                counts_old["NONOP"] += 1
                continue
            counts_old["NEEDS-JUDGMENT"] += 1
            judgment.append((file_, lineno, "NEW-OR-CHANGED", code.strip()[:160]))
        elif len(cats) > 1:
            counts_old["NEEDS-JUDGMENT"] += 1
            collisions.append((file_, lineno, sorted(cats), code.strip()[:160]))
        else:
            counts_old[next(iter(cats))] += 1

    os.makedirs(outdir, exist_ok=True)
    with open(os.path.join(outdir, "reflect_carryover.txt"), "w") as fh:
        fh.write("category counts: 1.27.1 (CAT) vs 1.26.8 (carried)\n")
        for c in sorted(set(counts_new) | set(counts_old)):
            fh.write(f"  {c:14s} new={counts_new.get(c,0):4d}  old={counts_old.get(c,0):4d}\n")
        fh.write(f"\ncollisions (same text, >1 category on 1.27.1): {len(collisions)}\n")
        for c in collisions:
            fh.write(f"  {c}\n")
    with open(os.path.join(outdir, "reflect_judgment.txt"), "w") as fh:
        fh.write(f"lines needing human judgment: {len(judgment)}\n\n")
        for file_, lineno, kind, code in judgment:
            fh.write(f"{file_}:{lineno} [{kind}] {code}\n")
    print("judgment lines:", len(judgment), "| collisions:", len(collisions))

if __name__ == "__main__":
    main()
