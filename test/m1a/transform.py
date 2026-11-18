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
  of main and before main's closing brace (the first line that is exactly
  `}` after `func main() {`: gofmt'd source) — never into a function that
  follows main.

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
    start = next((i for i, l in enumerate(lines) if l.startswith('func main() {')), -1)
    end = next((i for i in range(start + 1, len(lines)) if lines[i] == '}'), -1) if start >= 0 else -1
    if end < 0:
        raise SystemExit('transform.py: no main function (or no closing brace) for the println injection')
    for i, line in enumerate(lines):
        if start < i < end and line.strip() == 'return':
            out.append('\t' + probe)
        if i == end:
            out.append('\t' + probe)
        out.append(line)
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
