#!/usr/bin/env python3
"""The standard-library corpus gate on the new path (`make stdlib-gate`;
m3-plan D3, the layer-3 gate of SPEC §5.3 that replaced v1's wave runners).

  std_gate.py list
      every test/corpus program that imports a package (one `package main`
      file each; the import-free ones run per push in test-m1a), sorted:
      the programs test/m1r/run.sh compares with gc
  std_gate.py judge REPORT [REGISTER] [BUGS]
      judges test/m1r/run.sh's M1R_REPORT ("PASS|FAIL <program>[: reason]"
      lines) against the register (default test/corpus/std-register.tsv)
      and the bug register (default docs/dev/bugs.md)

The register lists the reviewed divergences, one per line:

  <program>\t<reason-code>\t<BUG-NNNN>\t<signature>\t<detail>

reason-code is SPEC §5.6's closed vocabulary (backend-specific,
unspecified-behavior, deferred-parity) or corpus-bug (the program tests v1,
not Go: gc rejects it or it prints nothing; deleted with v1 at m3-plan D6);
every entry cites a bugs.md id that exists; signature is test/m1r/run.sh's
FAIL reason (the exit-status pair, "stdout differs", "stderr differs", the
build failure's first line), so a listed program that diverges in another
way is DRIFT. Verdicts (exit 1 on any of them): DRIFT (a failing program
that is unlisted or fails with another signature), STALE (a listed program
that now passes, or that no longer exists), BAD (a malformed entry).
"""
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(HERE))
CODES = {"backend-specific", "unspecified-behavior", "deferred-parity", "corpus-bug"}


def programs(corpus=HERE):
    out = []
    for d, _, files in os.walk(corpus):
        for f in files:
            if not f.endswith(".go"):
                continue
            p = os.path.join(d, f)
            with open(p, errors="replace") as fh:
                s = fh.read()
            if re.search(r"^package main\b", s, re.M) and re.search(r"^import\b", s, re.M):
                out.append(os.path.relpath(p, ROOT))
    return sorted(out)


def parse_report(text):
    res = {}
    for line in text.splitlines():
        m = re.match(r"^(PASS|FAIL) (\S+?)(?:: (.*))?$", line)
        if m:
            res[m.group(2)] = (m.group(1), m.group(3) or "")
    return res


def parse_register(text, bug_ids):
    reg, bad = {}, []
    for n, line in enumerate(text.splitlines(), 1):
        if not line.strip() or line.startswith("#"):
            continue
        f = line.split("\t")
        if len(f) != 5 or not all(x.strip() for x in f):
            bad.append(f"BAD   line {n}: want 5 non-empty tab-separated fields")
            continue
        prog, code, bug, sig, _ = f
        if code not in CODES:
            bad.append(f"BAD   {prog}: reason code {code!r} not in {sorted(CODES)}")
        if not re.fullmatch(r"BUG-\d{4}", bug) or bug not in bug_ids:
            bad.append(f"BAD   {prog}: {bug} is not a docs/dev/bugs.md id")
        if prog in reg:
            bad.append(f"BAD   {prog}: listed twice")
        reg[prog] = (code, bug, sig)
    return reg, bad


def judge(report, reg, bad):
    out = list(bad)
    for prog, (verdict, reason) in sorted(report.items()):
        e = reg.get(prog)
        if verdict == "FAIL" and e is None:
            out.append(f"DRIFT {prog}: unlisted divergence: {reason}")
        elif verdict == "FAIL" and e[2] != reason:
            out.append(f"DRIFT {prog}: diverges as {reason!r}, listed as {e[2]!r}")
        elif verdict == "PASS" and e is not None:
            out.append(f"STALE {prog}: listed ({e[1]}) but matches gc: remove the entry")
    for prog in sorted(set(reg) - set(report)):
        out.append(f"STALE {prog}: listed but not run (no such program?)")
    return out


def bug_ids(path):
    with open(path) as fh:
        return set(re.findall(r"^\|\s*(BUG-\d{4})\b", fh.read(), re.M))


def main(argv):
    if argv[1:2] == ["list"]:
        print("\n".join(programs()))
        return 0
    if len(argv) >= 3 and argv[1] == "judge":
        regp = argv[3] if len(argv) > 3 else os.path.join(HERE, "std-register.tsv")
        bugs = argv[4] if len(argv) > 4 else os.path.join(ROOT, "docs/dev/bugs.md")
        with open(argv[2]) as fh:
            report = parse_report(fh.read())
        with open(regp) as fh:
            reg, bad = parse_register(fh.read(), bug_ids(bugs))
        problems = judge(report, reg, bad)
        for p in problems:
            print(p)
        npass = sum(1 for v, _ in report.values() if v == "PASS")
        print(f"stdlib-gate: {npass} match gc, {len(report) - npass} diverge "
              f"({len(reg)} listed), {len(problems)} problem(s)")
        return 1 if problems else 0
    print(__doc__, file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
