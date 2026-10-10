#!/usr/bin/env python3
"""Aggregate graalgo-fuzz shard results (local post-fetch step).

Usage: aggregate.py <results-dir>  (contains shard-*/results.jsonl)

Emits: summary counts, deduped failure groups with example seeds,
and a findings list suitable for bug filing.
"""
import json
import os
import sys
from collections import Counter, defaultdict


def main():
    root = sys.argv[1]
    results = []
    for shard in sorted(os.listdir(root)):
        p = os.path.join(root, shard, "results.jsonl")
        if not os.path.isfile(p):
            continue
        # Quarantine: skip shards whose canary failed.
        if os.path.exists(os.path.join(root, shard, "CANARY_FAIL")):
            print(f"QUARANTINED {shard} (canary fail)")
            continue
        with open(p) as f:
            for line in f:
                line = line.strip()
                if line:
                    results.append(json.loads(line))

    counts = Counter(r["result"] for r in results)
    print(f"total: {len(results)}")
    for k, v in sorted(counts.items()):
        print(f"  {k}: {v}")

    groups = defaultdict(list)
    for r in results:
        if r["result"] in ("PASS", "SCOPE"):
            continue
        sig = (r["result"], r["detail"][:100])
        groups[sig].append((r.get("shard"), r["seed"]))

    if groups:
        print("\n== failure groups ==")
        for (res, sig), items in sorted(groups.items(),
                                        key=lambda x: -len(x[1])):
            print(f"  {res} x{len(items)}: {sig[:80]}")
            print(f"    e.g. shard {items[0][0]} seed {items[0][1]}")

    scope_rate = counts.get("SCOPE", 0) / max(1, len(results))
    print(f"\nscope-hit rate: {scope_rate:.1%}")


if __name__ == "__main__":
    main()
