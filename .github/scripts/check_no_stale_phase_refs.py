#!/usr/bin/env python3
"""
No-stale-phase-refs gate (G14, codebase review 2026-10-17).

Fails when a tracked file outside the documented exclusion set references
the removed research-era layout (`phases/phase1|2|3`). The migration
(2026-10-14) relocated everything; new references to the old tree are
always wrong — either a stale comment/doc pointer (retarget it to the new
location per docs/REPO_STRUCTURE.md's old → new mapping) or dead code that
should be rewired or deleted.

Usage: check_no_stale_phase_refs.py [--repo DIR]
Exit 0: clean. Exit 1: stale references found (listed file:line).

Exclusions (each deliberate, each documented):
- this script itself (.github/scripts/check_no_stale_phase_refs.py) — its
  docstring legitimately names the pattern it polices
- docs/arch/** — archived history (phase docs, reviews, consolidated SPEC
  history). Frozen by design.
- docs/dev/adr/** — dated architecture decision records. Rewriting them
  falsifies history; docs/REPO_STRUCTURE.md's mapping table resolves them.
- docs/REPO_STRUCTURE.md — its old → new mapping table and "moved here
  from" provenance notes are intentional; the table is the resolver.
- requirements/** — dated registry journal entries (historical record).
- .github/workflows/ci.yml — owned by the gate-overhaul lane (its :9
  PLAN.md pointer is flagged for that lane).
- Dead developer tooling, pending an explicit rewire-or-delete decision by
  the test-infra owner (review-t2-testinfra / backend owner). These files
  are broken against the removed layout AND their harness logic is the
  template for the rewiring, so they are kept, not silently fixed:
  - test/corpus/**/run_differential.sh, run_wave.sh, run_wave3.sh,
    run_e2e.sh (per-corpus differential runners; invoked by the live
    layer-3 gate tools/run_stdlib_gate.sh — the gate is red until rewired)
  - test/e2e/run_module.sh (already flagged honest in test/e2e/README.md)
  - tools/fuzz-soak/harness/diff_one.sh, tools/fuzz-soak/harness/run_batch.py
    (layer-4a nightly soak; run_batch.py's build step targets the removed
    tree — the nightly is red until rewired)
  - tools/fuzz-soak/gha/** (dead Docker packaging of the old layout)
  - tools/godebug-diff/godebug-diff.sh (P2 paths dead), tools/godebug-diff/REPORT.md
    (dated report)
  - tools/mutation/main.go (dead paths incl. banned hidden_files/),
    tools/mutation/mutants.json (data artifact with historical paths)
"""

import fnmatch
import re
import subprocess
import sys

PATTERN = re.compile(r"phases/phase[123](?![0-9])")
# Matches already migrated to the archive location are not stale.
ARCHIVED = re.compile(r"docs/arch/phases/phase[123](?![0-9])")

EXCLUDE_DIRS = [
    "docs/arch/",
    "docs/dev/adr/",
    "requirements/",
    "tools/fuzz-soak/gha/",
]
EXCLUDE_FILES = {
    "docs/REPO_STRUCTURE.md",
    ".github/scripts/check_no_stale_phase_refs.py",  # self: docstring names the pattern
    ".github/workflows/ci.yml",
    "test/e2e/run_module.sh",
    "tools/fuzz-soak/harness/diff_one.sh",
    "tools/fuzz-soak/harness/run_batch.py",
    "tools/godebug-diff/godebug-diff.sh",
    "tools/godebug-diff/REPORT.md",
    "tools/mutation/main.go",
    "tools/mutation/mutants.json",
}
EXCLUDE_GLOBS = [
    "test/corpus/**/run_differential.sh",
    "test/corpus/**/run_wave.sh",
    "test/corpus/**/run_wave3.sh",
    "test/corpus/**/run_e2e.sh",
]


def excluded(path):
    if path in EXCLUDE_FILES:
        return True
    if any(path.startswith(d) for d in EXCLUDE_DIRS):
        return True
    return any(fnmatch.fnmatch(path, g) for g in EXCLUDE_GLOBS)


def main(argv):
    repo = "."
    if "--repo" in argv:
        repo = argv[argv.index("--repo") + 1]
    toplevel = subprocess.run(
        ["git", "-C", repo, "rev-parse", "--show-toplevel"],
        capture_output=True, text=True)
    if toplevel.returncode != 0:
        print("check_no_stale_phase_refs: not a git repo", file=sys.stderr)
        return 2
    root = toplevel.stdout.strip()

    r = subprocess.run(
        ["git", "-C", root, "grep", "-n", "-E", r"phases/phase[123]([^0-9]|$)"],
        capture_output=True, text=True)
    # git grep exits 1 when nothing matches — that is the clean case.
    if r.returncode not in (0, 1):
        print(f"git grep failed:\n{r.stderr}", file=sys.stderr)
        return 2

    hits = []
    for line in r.stdout.splitlines():
        # format: path:lineno:text (path never contains ':' here)
        path, _, rest = line.partition(":")
        if excluded(path):
            continue
        text = rest.partition(":")[2]
        # Strip already-archived references before matching.
        if PATTERN.search(ARCHIVED.sub("", text)):
            hits.append(line)

    if hits:
        print("STALE-PHASE-REFS GATE FAILED — references to the removed "
              "phases/phase1|2|3 layout:")
        for h in hits:
            print(f"  {h}")
        print("Retarget to the new location (docs/REPO_STRUCTURE.md) or "
              "remove; see the exclusion list in this script for the "
              "deliberate exceptions.")
        return 1
    print("stale-phase-refs: clean.")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
