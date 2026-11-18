#!/usr/bin/env python3
"""Make a phase-1 corpus program observable through println (toolexec M1a gate).

The phase-1 corpus (test/corpus/phase1) was written for a harness that reads
frame slots: most programs print nothing and end with a trailer

    // observe: v1 v2 ...

naming the variables of main to compare. This transform is the gc-oracle
injection of that harness (StraightLineCorpusTest / PanicCorpusTest), applied
to BOTH legs of the differential, so the program itself prints the values:

- a `// probe` anchor line becomes `defer func() { println(v1, v2, ...) }()`
  (the panic corpus: the probe is main's first-registered defer);
- otherwise `println(v1, v2, ...)` is inserted before every bare `return`
  and before the last line that is exactly `}` (main's closing brace).

Programs without the trailer are copied unchanged.

Usage: transform.py SRC.go DST.go
"""
import sys


def transform(text):
    lines = text.split('\n')
    names = None
    for line in lines:
        t = line.strip()
        if t.startswith('// observe:'):
            names = t[len('// observe:'):].split()
    if not names:
        return text
    probe = 'println(' + ', '.join(names) + ')'
    anchored = any(l.strip() == '// probe' or l.strip().startswith('// probe:') for l in lines)
    out = []
    if anchored:
        placed = False
        for line in lines:
            t = line.strip()
            if not placed and (t == '// probe' or t.startswith('// probe:')):
                out.append('\tdefer func() { ' + probe + ' }()')
                placed = True
            else:
                out.append(line)
        return '\n'.join(out)
    for line in lines:
        if line.strip() == 'return':
            out.append('\t' + probe)
        out.append(line)
    last = max((i for i, l in enumerate(out) if l == '}'), default=-1)
    if last < 0:
        raise SystemExit('transform.py: no closing brace for the println injection')
    out.insert(last, '\t' + probe)
    return '\n'.join(out)


def main():
    if len(sys.argv) != 3:
        raise SystemExit(__doc__)
    with open(sys.argv[1], encoding='utf-8', errors='surrogateescape') as f:
        text = f.read()
    with open(sys.argv[2], 'w', encoding='utf-8', errors='surrogateescape') as f:
        f.write(transform(text))


if __name__ == '__main__':
    main()
