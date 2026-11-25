#!/usr/bin/env python3
"""Planted violations for test/corpus/std_gate.py (AGENTS.md §5: the gate
can fail): DRIFT (unlisted and re-signed divergences), STALE (a listed
program that passes or is gone), BAD (reason code, bug id, field count,
duplicate); a clean report against a matching register passes."""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import std_gate as g  # noqa: E402

BUGS = {"BUG-0033", "BUG-0011"}
REG = "\n".join([
    "# comment",
    "a.go\tdeferred-parity\tBUG-0033\tstdout differs\tdetail",
    "b.go\tcorpus-bug\tBUG-0033\tgc rejects the program (corpus bug)\tv1 API",
])


def verdicts(report, reg=REG):
    r, bad = g.parse_register(reg, BUGS)
    return [p.split()[0] for p in g.judge(g.parse_report(report), r, bad)]


def main():
    ok = "FAIL a.go: stdout differs\nFAIL b.go: gc rejects the program (corpus bug)\nPASS c.go\n"
    assert verdicts(ok) == [], verdicts(ok)
    assert verdicts(ok + "FAIL d.go: stderr differs\n") == ["DRIFT"]
    assert verdicts(ok.replace("a.go: stdout differs", "a.go: exit status 2, gc 0")) == ["DRIFT"]
    assert verdicts(ok.replace("FAIL a.go: stdout differs", "PASS a.go")) == ["STALE"]
    assert verdicts("FAIL b.go: gc rejects the program (corpus bug)\n") == ["STALE"]
    for bad in ("a.go\tflaky\tBUG-0033\tstdout differs\td",
                "a.go\tdeferred-parity\tBUG-9999\tstdout differs\td",
                "a.go\tdeferred-parity\tBUG-0033\tstdout differs",
                "a.go\tdeferred-parity\tBUG-0033\t \td"):
        assert "BAD" in verdicts("FAIL a.go: stdout differs\n", bad), bad
    dup = REG + "\na.go\tdeferred-parity\tBUG-0033\tstdout differs\tdetail"
    assert "BAD" in verdicts(ok, dup)
    # The program list is the corpus's importing package-main files.
    progs = g.programs()
    assert progs and all(p.startswith("test/corpus/") and p.endswith(".go") for p in progs)
    print("test_std_gate: ok")


if __name__ == "__main__":
    main()
