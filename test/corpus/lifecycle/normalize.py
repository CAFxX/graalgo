#!/usr/bin/env python3
"""Normalize a Go program's crash output for the lifecycle corpus (T0.A).

Input (stdin): the program's stderr. The lifecycle corpus checks what the
runtime lifecycle owns — the fatal/panic message lines, the panic chain,
and the goroutine headers with their wait reasons — not the frame lines
(stack-trace fidelity is the traceback corpus's subject, see
../traceback/README.md). So:
- goroutine IDs -> N in "goroutine N [reason]:" headers;
- every line after a goroutine header up to the next blank line (the
  frames and the "created by" trailer) is dropped;
- hex addresses -> 0xADDR (the SIGSEGV line's pc);
- trailing blank lines are dropped.
"""
import re
import sys

HEADER = re.compile(r'^goroutine \d+ (\[.*\]:)$')


def normalize(text):
    out = []
    in_trace = False
    for line in text.splitlines():
        m = HEADER.match(line)
        if m:
            out.append('goroutine N ' + m.group(1))
            in_trace = True
            continue
        if in_trace:
            if line.strip() == '':
                in_trace = False
                out.append('')
            continue
        out.append(re.sub(r'0x[0-9a-fA-F]+', '0xADDR', line))
    while out and not out[-1].strip():
        out.pop()
    return '\n'.join(out) + ('\n' if out else '')


if __name__ == '__main__':
    sys.stdout.write(normalize(sys.stdin.read()))
