#!/usr/bin/env python3
"""GG-id existence gate (make check-repo).

Every requirement id cited anywhere in the tracked tree (docs, code,
comments, YAML: `GG-0042`, `GG-SCHED-0001`) must exist as
requirements/<id>.yaml. `tools/req lint` checks traceability annotations
in the scanned code but skips inline code spans and fenced blocks in docs;
this gate is the blunt complement: a cited id that was never minted (or a
typo) fails, wherever it is.

Skipped: test fixtures that cite fake ids on purpose (FIXTURES), the archived research-era trees (ARCHIVED), which cite ids that
were renumbered before the registry was frozen, and binary files. An id
inside a range ("GG-0039–0043") is checked only at its literal endpoints.

Usage: check_gg_ids.py [repo-root]
"""

import re
import subprocess
import sys
from pathlib import Path

ID = re.compile(r"\bGG-(?:[A-Z]+-)?\d{4}\b")
# Test fixtures that cite fake ids on purpose (the req CLI's own tests
# and this gate's test).
FIXTURES = re.compile(r"^(tools/req/.*_test\.go|\.github/scripts/test_check_gg_ids\.py)$")
ARCHIVED = ("docs/arch/phases/", "docs/arch/reviews/", "docs/arch/spikes/",  # dead-path-ok (archive)
            "docs/arch/research/", "research/")


def known_ids(root):
    return {p.stem for p in (root / "requirements").glob("GG-*.yaml")}


def tracked(root):
    out = subprocess.run(["git", "-C", str(root), "ls-files", "-z"],
                         check=True, capture_output=True).stdout.decode()
    return [p for p in out.split("\0")
            if p and not p.startswith(ARCHIVED) and not FIXTURES.match(p)]


def scan(root, files=None):
    root = Path(root)
    known = known_ids(root)
    errors = []
    for rel in (files if files is not None else tracked(root)):
        p = root / rel
        if not p.is_file():
            continue
        data = p.read_bytes()
        if b"\0" in data[:8192]:
            continue
        for n, line in enumerate(data.decode(errors="replace").splitlines(), 1):
            for m in ID.finditer(line):
                if m.group(0) not in known:
                    errors.append(f"{rel}:{n}: {m.group(0)} is not in requirements/")
    return errors


def main(argv):
    root = Path(argv[1]) if len(argv) > 1 else Path(__file__).resolve().parents[2]
    errors = scan(root)
    if errors:
        print("GG-ID GATE FAILED (cited requirement ids that do not exist):")
        for e in errors:
            print("  " + e)
        return 1
    print("GG-id gate clean")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
