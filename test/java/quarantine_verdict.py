#!/usr/bin/env python3
"""Verdict for `make test-java-slow` under the quarantine
(test/java/quarantine.txt: 'class | reason | owner | expiry').

Reads ClassListRunner logs and fails (exit 1) when
  - a class OUTSIDE the quarantine has a failing test;
  - a QUARANTINED class now passes every test (stale: remove its entry —
    a quarantine that outlives its bug hides the next regression);
  - an expected class never completed (no `CLASS <fqn>` line: JVM crash,
    INFRA exit, or a list/log mismatch) — never a silent pass.

Usage: quarantine_verdict.py <quarantine.txt> "<expected classes>" <log>...
Exit: 0 verdict green, 1 red, 2 usage.
"""

import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from check_lists import read_quarantine  # noqa: E402

CLASS = re.compile(r"^CLASS (\S+) \d+ ms$")
FAIL = re.compile(r"^FAIL (\S+)\.(\w+) ")
PASS = re.compile(r"^PASS (\S+)\.(\w+) ")
INFRA = re.compile(r"^INFRA (cannot load test class |\S+ has no test methods)")


def parse(text):
    """-> {fqn: (passes, fails)}, [infra lines]"""
    out, infra = {}, []
    p = f = 0
    for line in text.splitlines():
        if PASS.match(line):
            p += 1
        elif FAIL.match(line):
            f += 1
        elif INFRA.match(line):
            infra.append(line)
        else:
            m = CLASS.match(line)
            if m:
                out[m.group(1)] = (p, f)
                p = f = 0
    return out, infra


def verdict(quarantine, expected, texts):
    q = {e[0]: e for e in quarantine}
    results, infra = {}, []
    for t in texts:
        r, i = parse(t)
        results.update(r)
        infra += i
    bad, notes = [], []
    for line in infra:
        bad.append(f"infrastructure: {line}")
    for cls in expected:
        if cls not in results:
            bad.append(f"{cls}: did not complete (no CLASS line)")
            continue
        passes, fails = results[cls]
        if cls in q:
            _, reason, owner, expiry = q[cls]
            if fails == 0:
                bad.append(f"{cls}: quarantined ({reason}; {owner}, expires {expiry}) but now passes "
                           f"all {passes} tests — remove it from test/java/quarantine.txt")
            else:
                notes.append(f"{cls}: {fails} quarantined failure(s) ({owner}, expires {expiry})")
        elif fails:
            bad.append(f"{cls}: {fails} failing test(s) outside the quarantine")
    for cls in q:
        if cls not in expected:
            bad.append(f"{cls}: quarantined but not in the slow lists")
    return bad, notes


def main(argv):
    if len(argv) < 4:
        print(__doc__)
        return 2
    quarantine = read_quarantine(Path(argv[1]))
    expected = argv[2].split()
    texts = [Path(p).read_text(errors="replace") if Path(p).exists() else "" for p in argv[3:]]
    bad, notes = verdict(quarantine, expected, texts)
    for n in notes:
        print("QUARANTINED " + n)
    if bad:
        print("TEST-JAVA-SLOW FAILED:")
        for b in bad:
            print("  " + b)
        return 1
    print(f"test-java-slow green ({len(expected)} classes, {len(notes)} quarantined)")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
