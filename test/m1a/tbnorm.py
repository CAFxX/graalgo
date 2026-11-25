#!/usr/bin/env python3
"""Normalize a Go program's crash output keeping the traceback frames.

The stderr normalizer of the m1a programs that opt in with a
`// m1a:traceback` line (test/m1a/run.sh): where
test/corpus/lifecycle/normalize.py drops every frame, this one compares
them — function names and file:line of each Go frame (WP-TR-AUDIT-6):
- goroutine IDs -> N in "goroutine N [reason]:" headers;
- a frame's argument list -> "(...)": gc prints argument words (or "..."
  for an inlined frame), graalgo records none;
- a frame's file -> its base name, and the "+0x..." pc offset dropped;
- frames of gc's runtime (a file under /src/runtime/, e.g. the "panic(...)"
  frame of a panic raised during a deferred call) and "created by" trailers
  are dropped: gc runtime internals and goroutine creators are not
  recorded by graalgo;
- other hex numbers -> 0xADDR (the SIGSEGV line's pc).
Tested by test_tbnorm.py.
"""
import re
import sys

HEADER = re.compile(r'^goroutine \d+(?: gp=\S+ m=\S+(?: mp=\S+)?)? (\[.*\]:)$')
FILE_LINE = re.compile(r'^\t(\S*?)([^/\s]+\.(?:go|s)):(\d+)(?: \+0x[0-9a-fA-F]+)?$')


def arg_list_start(line):
    """Index of the '(' opening the parenthesized group that ends line, or -1."""
    depth = 0
    for i in range(len(line) - 1, -1, -1):
        c = line[i]
        if c == ')':
            depth += 1
        elif c == '(':
            depth -= 1
            if depth == 0:
                return i
    return -1


def normalize(text):
    out = []
    lines = text.splitlines()
    in_trace = False
    i = 0
    while i < len(lines):
        line = lines[i]
        m = HEADER.match(line)
        if m:
            out.append('goroutine N ' + m.group(1))
            in_trace = True
            i += 1
            continue
        if in_trace and line.strip() == '':
            in_trace = False
        if in_trace:
            nxt = lines[i + 1] if i + 1 < len(lines) else ''
            fm = FILE_LINE.match(nxt)
            if line.startswith('created by '):
                i += 2 if fm else 1
                continue
            if fm:
                if '/src/runtime/' in fm.group(1) or fm.group(1).startswith('runtime/'):
                    i += 2
                    continue
                k = arg_list_start(line)
                name = line[:k] if k >= 0 else line
                out.append(name + '(...)')
                out.append('\t' + fm.group(2) + ':' + fm.group(3))
                i += 2
                continue
        out.append(re.sub(r'0x[0-9a-fA-F]+', '0xADDR', line))
        i += 1
    while out and not out[-1].strip():
        out.pop()
    return '\n'.join(out) + ('\n' if out else '')


if __name__ == '__main__':
    sys.stdout.write(normalize(sys.stdin.read()))
