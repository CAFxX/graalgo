#!/usr/bin/env python3
"""Normalize Go panic tracebacks for differential comparison (P3-M4).

gc and graalgo differ in incidental details; the corpus compares the
semantic content:
- goroutine IDs -> N
- main. prefix on frame names -> stripped (graalgo qualifies only main.main)
- (...) / (args...) -> ()
- +0x... offsets -> stripped
- absolute file paths -> basename
- created by ... lines -> dropped (graalgo doesn't record creators)
"""
import re
import sys

def normalize(text):
    lines = []
    for line in text.splitlines():
        # Drop gc's "created by" lines (documented limitation).
        if line.startswith("created by "):
            continue
        # goroutine IDs.
        line = re.sub(r'^goroutine \d+ \[running\]:$', 'goroutine N [running]:', line)
        # Frame names: strip main. prefix, normalize arg lists.
        line = re.sub(r'^main\.', '', line)
        line = re.sub(r'\(.*\)$', '()', line)
        # +0x offsets.
        line = re.sub(r' \+0x[0-9a-f]+$', '', line)
        # Absolute paths -> basename.
        line = re.sub(r'^(\s+).*/([^/]+\.go:\d+)$', r'\1\2', line)
        lines.append(line)
    # Drop trailing empty lines (gc's "exit status" goes to the runner, not here).
    while lines and not lines[-1].strip():
        lines.pop()
    return "\n".join(lines) + "\n"

if __name__ == "__main__":
    sys.stdout.write(normalize(sys.stdin.read()))
