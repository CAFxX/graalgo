#!/usr/bin/env python3
"""Bridge-inventory counts gate (review P1-3, 2026-10-31).

The bridge inventory (docs/arch/bridge-inventory.md, entry B-4) is the
no-bridges gate's grandfather list, so its counts must be re-derived from
the tree, not hand-maintained: the pre-P1-3 text claimed "142 generated
synth_*.go", a number no definition reproduces (140 top-level files,
132 non-test, 18 Makefile-generated). This gate fails when the
inventory's counts disagree with the tree.

Single source of truth for the claimed counts: the machine-readable
marker inside the B-4 section,
  <!-- bridge-inventory-counts: top_total=... top_nontest=... ... -->
parsed from that section only. If the marker is missing, misplaced, or
unparseable, this gate fails loudly instead of passing vacuously (same
class as the duplicate-FQN gate's STUB_REL parse failure).

Counting rule (anchored, never a bare substring): a file is a test file
iff its name ends in "_test.go" (anchored _test\\.go$). The bare-substring
filter `grep -v _test` undercounts at 129: it also drops synth_testing.go,
synth_testing_iotest.go, and synth_testing_quick.go (substring "_test"
inside "testing").

Also guarded here, because both are P1-3 corrections that must not
silently regress: B-6 must NOT appear under "Active bridges" (it retired
in code with fix-redmaster-r39 and lives in "Retired"), and the
GENERATED_SYNTH list parsed from the root Makefile must match the
inventory's generated count, with every listed file present in the
top-level synth set.

Usage: python3 .github/scripts/check_bridge_inventory.py (from the repo root)
Exit 0 = inventory counts match the tree; exit 1 = gate failed (list printed).
"""

import re
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
INVENTORY = REPO_ROOT / "docs/arch/bridge-inventory.md"
FRONTEND = REPO_ROOT / "internal/frontend"
MIRRORS = FRONTEND / "p3frontend"
MAKEFILE = REPO_ROOT / "Makefile"

MARKER_RE = re.compile(
    r"bridge-inventory-counts:\s*"
    r"top_total=(\d+)\s+top_nontest=(\d+)\s+top_test=(\d+)\s+"
    r"mirrors=(\d+)\s+generated=(\d+)"
)


def section(text, start_heading, end_heading):
    """Return the slice between two headings (end exclusive), or None."""
    start = text.find(start_heading)
    if start < 0:
        return None
    end = text.find(end_heading, start + len(start_heading))
    return text[start : end if end >= 0 else len(text)]


def parse_generated_synth():
    """Return the root Makefile's GENERATED_SYNTH file list (relative paths)."""
    lines = MAKEFILE.read_text().splitlines()
    block = []
    in_block = False
    for line in lines:
        if not in_block:
            if line.startswith("GENERATED_SYNTH"):
                in_block = True
                block.append(line)
                if not line.rstrip().endswith("\\"):
                    break
        else:
            block.append(line)
            if not line.rstrip().endswith("\\"):
                break
    if not block:
        return []
    joined = " ".join(block).replace("\\", " ")
    return re.findall(r"(\S+\.go)", joined)


def count_synth(directory):
    """Return (total, nontest, test) for top-level synth_*.go files."""
    if not directory.is_dir():
        print(f"BRIDGE-INVENTORY GATE FAILED: missing directory: {directory}")
        sys.exit(1)
    files = sorted(p for p in directory.glob("synth_*.go") if p.is_file())
    test = [p for p in files if p.name.endswith("_test.go")]
    return len(files), len(files) - len(test), len(test)


def main():
    failures = []
    if not INVENTORY.is_file():
        print(f"BRIDGE-INVENTORY GATE FAILED: missing {INVENTORY}")
        return 1
    text = INVENTORY.read_text()

    b4 = section(text, "### B-4.", "### B-5.")
    if b4 is None:
        failures.append("B-4 section not found (want '### B-4.' before '### B-5.')")
        b4 = ""
    match = MARKER_RE.search(b4)
    if match is None:
        print("BRIDGE-INVENTORY GATE FAILED: no parseable "
              "bridge-inventory-counts marker inside the B-4 section "
              "(refusing to pass vacuously)")
        return 1
    claimed_total, claimed_nontest, claimed_test, claimed_mirrors, claimed_generated = (
        int(g) for g in match.groups()
    )

    total, nontest, test = count_synth(FRONTEND)
    mirrors_total, _, _ = count_synth(MIRRORS)
    if not MAKEFILE.is_file():
        print(f"BRIDGE-INVENTORY GATE FAILED: missing {MAKEFILE}")
        return 1
    generated = parse_generated_synth()
    if not generated:
        print("BRIDGE-INVENTORY GATE FAILED: could not parse GENERATED_SYNTH "
              "from the root Makefile (refusing to pass vacuously)")
        return 1

    if claimed_total != total:
        failures.append(f"top_total: inventory claims {claimed_total}, "
                        f"tree has {total} internal/frontend/synth_*.go")
    if claimed_nontest != nontest:
        failures.append(f"top_nontest: inventory claims {claimed_nontest}, "
                        f"tree has {nontest} (anchored _test.go$ test split)")
    if claimed_test != test:
        failures.append(f"top_test: inventory claims {claimed_test}, "
                        f"tree has {test}")
    if claimed_total != claimed_nontest + claimed_test:
        failures.append(f"marker internally inconsistent: top_total "
                        f"{claimed_total} != top_nontest {claimed_nontest} "
                        f"+ top_test {claimed_test}")
    if claimed_mirrors != mirrors_total:
        failures.append(f"mirrors: inventory claims {claimed_mirrors}, "
                        f"tree has {mirrors_total} "
                        f"internal/frontend/p3frontend/synth_*.go")
    if claimed_generated != len(generated):
        failures.append(f"generated: inventory claims {claimed_generated}, "
                        f"Makefile GENERATED_SYNTH lists {len(generated)}")
    top_names = {p.name for p in FRONTEND.glob("synth_*.go") if p.is_file()}
    for entry in generated:
        path = REPO_ROOT / entry
        if not path.is_file():
            failures.append(f"GENERATED_SYNTH entry missing on disk: {entry}")
        elif Path(entry).name not in top_names:
            failures.append(f"GENERATED_SYNTH entry not in top-level synth "
                            f"set: {entry}")

    active = section(text, "## Active bridges", "## Retired")
    retired = section(text, "## Retired", "\n## ")
    if active is None:
        failures.append("Active bridges section not found")
    elif "### B-6." in active:
        failures.append("B-6 still listed under Active bridges (it retired "
                        "with fix-redmaster-r39; it belongs in Retired)")
    if retired is None:
        failures.append("Retired section not found")
    elif "B-6" not in retired:
        failures.append("B-6 missing from the Retired section")

    if failures:
        print("BRIDGE-INVENTORY GATE FAILED: inventory disagrees with the tree:")
        print("\n".join(f"  {f}" for f in failures))
        return 1
    print(f"bridge-inventory gate clean: top-level {total} "
          f"({nontest} non-test + {test} test), mirrors {mirrors_total}, "
          f"generated {len(generated)}, B-6 retired")
    return 0


if __name__ == "__main__":
    sys.exit(main())
