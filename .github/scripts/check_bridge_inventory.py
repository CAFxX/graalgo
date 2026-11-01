#!/usr/bin/env python3
"""Bridge-inventory count check (review F-05, 2026-11-01).

The bridge inventory (docs/arch/bridge-inventory.md) is the no-bridges
gate's grandfather list. Its file counts drifted silently (B-4 claimed
142 synth_*.go against 140 on disk; the syntheticSources entry count
drifted 133 -> 130), so the gate adjudicated against stale text. This
check recomputes every count the inventory states from the tree and
fails loudly when the inventory disagrees with disk.

Counts are computed from whole directories, never from one named file,
and the test/non-test split uses the anchored ``_test.go$`` suffix:
a bare ``grep -v _test`` also drops the real sources
``synth_testing*.go`` (``_test`` is a substring of ``testing``) and
undercounts non-test files by 3 (129 instead of 132).

Checked against the inventory:
  - B-4 heading: total / non-test / test synth_*.go in
    internal/frontend/, plus p3frontend mirror total / non-test count.
  - B-3: syntheticSources import-path entry count, parsed from the
    map literal in internal/frontend/synth_machinery.go.

Usage: python3 .github/scripts/check_bridge_inventory.py [--root DIR]
  --root defaults to the repository root (two levels above this script).
  --inventory overrides the inventory path (used to prove the check
  fails on a tampered copy without touching the tree).
"""

import argparse
import glob
import os
import re
import sys

B4_RE = re.compile(
    r"^### B-4\.\s+(\d+) synth_\*\.go files "
    r"\((\d+) non-test \+ (\d+) \*_test\.go\) "
    r"\+ (\d+) p3frontend mirrors",
    re.M,
)
B4_MIRROR_NONTEST_RE = re.compile(
    r"plus (\d+) `synth_\*\.go` mirrors under .*?p3frontend/.*? "
    r"— all (\d+) non-test \((\d+) `\*_test\.go`\)",
    re.S,
)
# \s+ (not a literal space): the inventory wraps the count across lines
# ("130\n    entries"), and a literal space made the check fail on the
# correct inventory — the exact bug this successor lane found.
B3_ENTRIES_RE = re.compile(r"syntheticSources.*?(\d+)\s+entries", re.S)
MAP_RE = re.compile(
    r"var syntheticSources = map\[string\]string\{(.*?)\n\}", re.S
)
KEY_RE = re.compile(r'^\s*"([^"]+)":', re.M)


def count_synth(directory):
    """Return (total, non_test, test) for synth_*.go in directory."""
    files = glob.glob(os.path.join(directory, "synth_*.go"))
    test = sum(1 for f in files if f.endswith("_test.go"))
    return len(files), len(files) - test, test


def fail(errors):
    for e in errors:
        print(f"check_bridge_inventory: FAIL: {e}", file=sys.stderr)
    sys.exit(1)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default=None)
    ap.add_argument("--inventory", default=None)
    args = ap.parse_args()
    root = args.root or os.path.dirname(
        os.path.dirname(os.path.abspath(__file__))
    )
    inv_path = args.inventory or os.path.join(
        root, "docs", "arch", "bridge-inventory.md"
    )
    errors = []

    with open(inv_path, encoding="utf-8") as fh:
        inventory = fh.read()
    b3_section = inventory.split("### B-4.")[0].split("### B-3.")[-1]
    b4_section = inventory.split("### B-4.")[1].split("### B-5.")[0]

    total, non_test, test = count_synth(
        os.path.join(root, "internal", "frontend")
    )
    m_total, m_non_test, m_test = count_synth(
        os.path.join(root, "internal", "frontend", "p3frontend")
    )

    m = B4_RE.search("### B-4." + b4_section)
    if not m:
        errors.append("B-4 heading does not carry the machine-readable "
                      "count line (total/non-test/test/mirrors)")
    else:
        want = (total, non_test, test, m_total)
        got = tuple(int(x) for x in m.groups())
        if got != want:
            errors.append(
                f"B-4 heading counts {got} != tree counts {want} "
                f"(total/non-test/test/p3frontend-mirrors)"
            )
    mm = B4_MIRROR_NONTEST_RE.search(b4_section)
    if not mm:
        errors.append("B-4 body does not carry the machine-readable "
                      "p3frontend mirror split line")
    else:
        want = (m_total, m_non_test, m_test)
        got = tuple(int(x) for x in mm.groups())
        if got != want:
            errors.append(
                f"B-4 mirror split {got} != tree counts {want} "
                f"(total/non-test/test)"
            )

    with open(
        os.path.join(root, "internal", "frontend", "synth_machinery.go"),
        encoding="utf-8",
    ) as fh:
        machinery = fh.read()
    map_m = MAP_RE.search(machinery)
    if not map_m:
        errors.append("syntheticSources map literal not found in "
                      "synth_machinery.go")
        entries = None
    else:
        entries = len(KEY_RE.findall(map_m.group(1)))
    em = B3_ENTRIES_RE.search(b3_section)
    if not em:
        errors.append("B-3 does not state a syntheticSources entry count")
    elif entries is not None and int(em.group(1)) != entries:
        errors.append(
            f"B-3 syntheticSources entries {em.group(1)} != "
            f"tree count {entries}"
        )

    if errors:
        fail(errors)
    print(
        f"check_bridge_inventory: OK: synth {total} "
        f"({non_test} non-test + {test} test), "
        f"p3frontend mirrors {m_total} "
        f"({m_non_test} non-test + {m_test} test), "
        f"syntheticSources {entries} entries"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
