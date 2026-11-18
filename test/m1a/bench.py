#!/usr/bin/env python3
"""Rewrite a test/bench benchmark so that it imports no package (M1a).

The benchmarks under test/bench time themselves with `time` and report
through `fmt`/`os`, which the bundle path cannot run before the standard
library compiles through graalgo-tool (toolexec M1b-M1d). This rewrite keeps
the workload byte for byte and replaces only the harness:

    time.Now()/time.Since   -> runtime.nanotime through //go:linkname
                               (a body-less function: gc links it, graalgo
                               resolves it as an intrinsic, toolexec D9)
    fmt.Println(r)          -> println(r)                    (stderr)
    fmt.Fprintf(os.Stderr, "BENCH name %d\\n", ...) -> println("BENCH", "name", best)

Usage: bench.py SRC_main.go DST_DIR   (writes main.go, stub.s, go.mod)
"""
import os
import re
import sys


def rewrite(text):
    head, sep, rest = text.partition('import (')
    if not sep:
        raise SystemExit('bench.py: no import block')
    rest = rest[rest.index(')') + 1:]
    text = head + 'import _ "unsafe" // for go:linkname\n\n//go:linkname nanotime runtime.nanotime\nfunc nanotime() int64\n' + rest
    rules = [
        (r'time\.Duration\(1 << 62\)', 'int64(1 << 62)'),
        (r't0 := time\.Now\(\)', 't0 := nanotime()'),
        (r'd := time\.Since\(t0\)', 'd := nanotime() - t0'),
        (r'fmt\.Println\((\w+)\)', r'println(\1)'),
        (r'fmt\.Fprintf\(os\.Stderr, "BENCH (\w+) %d\\n", best\.Nanoseconds\(\)\)', r'println("BENCH", "\1", best)'),
    ]
    for pattern, repl in rules:
        text, n = re.subn(pattern, repl, text)
        if n == 0:
            raise SystemExit('bench.py: harness pattern not found: ' + pattern)
    for pkg in ('time', 'fmt', 'os', 'strings'):
        if re.search(r'(?<![\w.])' + pkg + r'\.', text):
            raise SystemExit('bench.py: the workload itself uses package ' + pkg + ' (not an M1a benchmark)')
    return text


def main():
    if len(sys.argv) != 3:
        raise SystemExit(__doc__)
    src, dst = sys.argv[1], sys.argv[2]
    with open(src, encoding='utf-8') as f:
        text = rewrite(f.read())
    os.makedirs(dst, exist_ok=True)
    with open(os.path.join(dst, 'main.go'), 'w', encoding='utf-8') as f:
        f.write(text)
    # An assembly file makes gc accept the body-less declaration.
    with open(os.path.join(dst, 'stub.s'), 'w', encoding='utf-8') as f:
        f.write('// intentionally empty: allows body-less function declarations\n')
    with open(os.path.join(dst, 'go.mod'), 'w', encoding='utf-8') as f:
        f.write('module m1a/bench\n\ngo 1.27\n')


if __name__ == '__main__':
    main()
