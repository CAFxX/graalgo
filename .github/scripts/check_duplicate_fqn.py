#!/usr/bin/env python3
"""Duplicate-FQN overlay gate (review R-01, 2026-10-31).

Fails if any fully-qualified Java class name exists in more than one
backend module's main source tree, EXCEPT the 8 frozen Phase-1 stub /
Phase-2 replacement pairs whose shadowing is deliberate and load-bearing
(classpath order: runtime/out/classes before truffle/out/classes, enforced
at startup by the isPhase2() assertion in graalgo.launcher.Main,
graalgo.sched.launcher.SchedMain, and graalgo.p3sync.launcher.P3SyncMain).

The allowlist is NOT hardcoded here: it is parsed from STUB_REL in
internal/backend/runtime/Makefile, the single source of truth for the
pair set (the runtime build's stub-shadow gate fails if any pair goes
missing). If that parse yields nothing, this gate fails loudly instead
of passing vacuously.

Also fails if an allowlisted FQN is missing from either the runtime or
the truffle tree (a silently dropped pair would re-expose a stub).

Scope: main source trees of runtime, truffle, and interop. A future
interop duplicate would be exactly as load-bearing as a truffle one, so
interop is scanned too; it currently contributes no duplicates.

Usage: python3 .github/scripts/check_duplicate_fqn.py  (from the repo root)
Exit 0 = no unexpected duplicates; exit 1 = gate failed (list printed).
"""

import re
import sys
from collections import defaultdict
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
MODULES = ("runtime", "truffle", "interop")
RUNTIME_MAKEFILE = REPO_ROOT / "internal/backend/runtime/Makefile"


def parse_stub_rel():
    """Return the set of FQNs listed in the runtime Makefile's STUB_REL."""
    lines = RUNTIME_MAKEFILE.read_text().splitlines()
    block = []
    in_block = False
    for line in lines:
        if not in_block:
            if line.startswith("STUB_REL"):
                in_block = True
                block.append(line)
                if not line.rstrip().endswith("\\"):
                    break
        else:
            block.append(line)
            if not line.rstrip().endswith("\\"):
                break
    if not block:
        return set()
    joined = " ".join(block).replace("\\", " ")
    fqns = set()
    for token in re.findall(r"(\S+\.java)", joined):
        fqns.add(token[:-5].replace("/", "."))
    return fqns


def scan_module(module):
    """Return {FQN: path} for a module's main source tree."""
    root = REPO_ROOT / "internal/backend" / module / "src/main/java"
    if not root.is_dir():
        print(f"DUPLICATE-FQN GATE FAILED: missing source tree: {root}")
        sys.exit(1)
    out = {}
    for path in sorted(root.rglob("*.java")):
        fqn = path.relative_to(root).with_suffix("").as_posix().replace("/", ".")
        out[fqn] = path
    return out


def main():
    if not RUNTIME_MAKEFILE.is_file():
        print(f"DUPLICATE-FQN GATE FAILED: missing {RUNTIME_MAKEFILE}")
        return 1
    allowed = parse_stub_rel()
    if not allowed:
        print("DUPLICATE-FQN GATE FAILED: could not parse STUB_REL from "
              "internal/backend/runtime/Makefile (refusing to pass vacuously)")
        return 1

    by_fqn = defaultdict(dict)  # FQN -> {module: path}
    for module in MODULES:
        for fqn, path in scan_module(module).items():
            by_fqn[fqn][module] = path

    failures = []
    for fqn in sorted(by_fqn):
        modules = by_fqn[fqn]
        if len(modules) < 2:
            continue
        if set(modules) == {"runtime", "truffle"} and fqn in allowed:
            continue
        failures.append(
            f"  {fqn} in {sorted(modules)}: "
            + ", ".join(str(by_fqn[fqn][m]) for m in sorted(modules))
        )
    for fqn in sorted(allowed):
        present = set(by_fqn.get(fqn, {}))
        if not {"runtime", "truffle"} <= present:
            failures.append(
                f"  allowlisted overlay pair incomplete: {fqn} present only in "
                f"{sorted(present) or ['nowhere']} (want runtime + truffle)"
            )

    if failures:
        print("DUPLICATE-FQN GATE FAILED: unexpected same-FQN classes across "
              "backend modules (or incomplete allowlisted pairs):")
        print("\n".join(failures))
        print("Allowed overlay pairs (from runtime Makefile STUB_REL): "
              + ", ".join(sorted(allowed)))
        return 1
    print(f"duplicate-FQN gate clean: {len(allowed)} allowlisted overlay pairs, "
          "no other same-FQN classes across runtime/truffle/interop")
    return 0


if __name__ == "__main__":
    sys.exit(main())
