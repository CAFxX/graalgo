#!/usr/bin/env python3
# Scans Go files for func/method declarations with >=2 results.
import re, sys, glob

FUNC_PAT = re.compile(r'^func (\((.*?)\) )?([A-Za-z0-9_]+)\((.*?)\)( \(([^)]*)\)|, ([A-Za-z0-9_\[\]\*\.]+))? \{', re.M | re.S)

def split_top(s, sep=','):
    depth = 0; parts = []; cur = ''
    for ch in s:
        if ch in '([': depth += 1
        elif ch in ')]': depth -= 1
        if ch == sep and depth == 0:
            parts.append(cur); cur = ''
        else: cur += ch
    parts.append(cur)
    return parts

for path in sys.argv[1:]:
    src = open(path).read()
    # strip build tags not needed; find package
    m = re.search(r'^package\s+(\w+)', src, re.M)
    pkg = m.group(1) if m else '?'
    for mt in FUNC_PAT.finditer(src):
        recv, name = mt.group(2), mt.group(3)
        results = (mt.group(6) or '').strip()
        if not results:
            continue
        parts = [p.strip() for p in split_top(results) if p.strip()]
        # count results: each comma-separated part may be "a, b T" (2 results)
        n = 0
        for p in parts:
            toks = p.rsplit(' ', 1)
            if len(toks) == 2 and re.match(r'^[A-Za-z_][\w,]*$', toks[0].replace(' ','')):
                n += len(toks[0].split(','))
            else:
                n += 1
        if n >= 2:
            rtype = ''
            if recv:
                rtype = recv.strip().split()[-1].lstrip('*')
            print(f"{pkg}.{rtype}.{name}" if rtype else f"{pkg}.{name}", f"({n} results)")
