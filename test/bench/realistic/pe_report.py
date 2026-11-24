"""Summarize a Truffle compilation trace (engine.TraceCompilation) — pe-report.sh.

Usage: pe_report.py <name> <trace file> [max churn]
Prints one summary line, every failed compilation (root, tier, reason) and the
roots deoptimized or invalidated most often. Exit 1 when a compilation failed
or, with max churn, when one root was deoptimized or invalidated more often
(a deopt loop that has not yet tripped Truffle's cycle detection).
"""
import collections
import re
import sys

LINE = re.compile(r'^\[engine\] opt (done|failed|deopt|inv\S*)\s+engine=\d+\s+id=\d+\s+(\S+)\s*\|(.*)$')
TIER = re.compile(r'Tier (\d)')
REASON = re.compile(r'Reason:\s*([^|]*(?:\|ValuePhi[^|]*)?)')


def short(reason):
    reason = re.sub(r'^\S*Exception\S*: ', '', reason)
    return reason[:160]


def main():
    name, path = sys.argv[1], sys.argv[2]
    max_churn = int(sys.argv[3]) if len(sys.argv) > 3 else None
    counts = collections.Counter()
    failed = []
    churn = collections.Counter()
    with open(path, errors='replace') as f:
        for ln in f:
            m = LINE.match(ln)
            if not m:
                continue
            kind, root, rest = m.group(1), m.group(2), m.group(3)
            kind = 'inval' if kind.startswith('inv') else kind
            counts[kind] += 1
            if kind == 'failed':
                t = TIER.search(rest)
                r = REASON.search(rest)
                failed.append((root, t.group(1) if t else '?', short(r.group(1) if r else rest)))
            elif kind in ('deopt', 'inval'):
                churn[root] += 1
    print('%-11s done=%-5d failed=%-3d deopt=%-4d inval=%-4d' % (
        name, counts['done'], counts['failed'], counts['deopt'], counts['inval']))
    for root, tier, reason in failed:
        print('  FAILED  %s (tier %s): %s' % (root, tier, reason))
    for root, n in churn.most_common(5):
        if n >= 3:
            print('  churn   %s: %d deopt/inval' % (root, n))
    over = [(r, n) for r, n in churn.items() if max_churn is not None and n > max_churn]
    for root, n in over:
        print('  CHURN   %s: %d deopt/inval > %d' % (root, n, max_churn))
    return 1 if failed or over else 0


if __name__ == '__main__':
    sys.exit(main())
