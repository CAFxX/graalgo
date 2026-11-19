#!/usr/bin/env python3
"""GG-id existence gate (make check-repo).

Every requirement id cited anywhere in the tracked tree (docs, code,
comments, YAML: `GG-0042`, `GG-SCHED-0001`) must exist as
requirements/<id>.yaml. `tools/req lint` checks traceability annotations
in the scanned code but skips inline code spans and fenced blocks in docs;
this gate is the blunt complement: a cited id that was never minted (or a
typo) fails, wherever it is.

Second rule (a recurring error class): a SUPERSEDED or RETIRED id
(requirements/<id>.yaml `status:`) cited in live docs or code fails unless
the citation is marked as history — the line or an adjacent line says
supersede(d)/retired/history/re-issued/formerly/"now GG-…" — or the file is
a historical record (HISTORICAL: the registry itself, ADRs, the dated SPEC
extract, the SPEC's own amendment trail).

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


HISTORICAL = ("requirements/", "docs/dev/adr/", "docs/spec/spec-extract.md", "docs/arch/SPEC.md")
DEAD_STATUS = re.compile(r"^status:\s*(superseded|retired)\s*$", re.M)
HISTORY_MARK = re.compile(r"(?i)supersed|retire|histor|re-issu|formerly|\bnow GG-")


def known_ids(root):
    return {p.stem for p in (root / "requirements").glob("GG-*.yaml")}


def dead_ids(root):
    return {p.stem for p in (root / "requirements").glob("GG-*.yaml")
            if DEAD_STATUS.search(p.read_text(errors="replace"))}


def tracked(root):
    out = subprocess.run(["git", "-C", str(root), "ls-files", "-z"],
                         check=True, capture_output=True).stdout.decode()
    return [p for p in out.split("\0")
            if p and not p.startswith(ARCHIVED) and not FIXTURES.match(p)]


def scan(root, files=None):
    root = Path(root)
    known = known_ids(root)
    dead = dead_ids(root)
    errors = []
    for rel in (files if files is not None else tracked(root)):
        p = root / rel
        if not p.is_file():
            continue
        data = p.read_bytes()
        if b"\0" in data[:8192]:
            continue
        lines = data.decode(errors="replace").splitlines()
        historical = rel.startswith(HISTORICAL)
        for n, line in enumerate(lines, 1):
            for m in ID.finditer(line):
                if m.group(0) not in known:
                    errors.append(f"{rel}:{n}: {m.group(0)} is not in requirements/")
                elif (m.group(0) in dead and not historical
                      and not any(HISTORY_MARK.search(x) for x in lines[max(0, n - 2):n + 1])):
                    errors.append(f"{rel}:{n}: {m.group(0)} is superseded/retired: cite its successor, "
                                  f"or mark the citation as history (superseded/retired/history)")
    return errors


def main(argv):
    root = Path(argv[1]) if len(argv) > 1 else Path(__file__).resolve().parents[2]
    errors = scan(root)
    if errors:
        print("GG-ID GATE FAILED (unknown ids, or superseded/retired ids cited as live):")
        for e in errors:
            print("  " + e)
        return 1
    print("GG-id gate clean")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
