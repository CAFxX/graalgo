#!/usr/bin/env python3
"""Normalize a Go program's crash output for the lifecycle corpus (T0.A).

Input (stdin): the program's stderr. The lifecycle corpus checks what the
runtime lifecycle owns — the fatal/panic message lines, the panic chain,
and the goroutine headers with their wait reasons — not the frame lines
(stack-trace fidelity is the traceback corpus's subject, see
../traceback/README.md). So:
- goroutine IDs -> N in "goroutine N [reason]:" headers, also in the
  runtime-traceback form a fatal error ("fatal error: ...", gc throw)
  prints ("goroutine N gp=0x... m=... mp=0x... [reason]:");
- every line after a goroutine header up to the next blank line (the
  frames and the "created by" trailer) is dropped;
- a goroutine created by the runtime itself ("created by runtime.*":
  forcegc, bgsweep, the finalizer goroutine, ... which a gc throw dumps)
  is dropped whole: those are gc runtime internals, not program state;
- hex addresses -> 0xADDR (the SIGSEGV line's pc);
- trailing blank lines are dropped.
"""
import re
import sys

HEADER = re.compile(r'^goroutine \d+(?: gp=\S+ m=\S+(?: mp=\S+)?)? (\[.*\]:)$')


def normalize(text):
    out = []
    block = None  # [header line, is_runtime_goroutine] while in a trace

    def end_block():
        if not block[1]:
            out.append(block[0])
            out.append('')

    for line in text.splitlines():
        m = HEADER.match(line)
        if m:
            if block is not None:
                end_block()
            block = ['goroutine N ' + m.group(1), False]
            continue
        if block is not None:
            if line.strip() == '':
                end_block()
                block = None
            elif line.startswith('created by runtime.'):
                block[1] = True
            continue
        out.append(re.sub(r'0x[0-9a-fA-F]+', '0xADDR', line))
    if block is not None:
        end_block()
    while out and not out[-1].strip():
        out.pop()
    return '\n'.join(out) + ('\n' if out else '')


if __name__ == '__main__':
    sys.stdout.write(normalize(sys.stdin.read()))
