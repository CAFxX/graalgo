#!/usr/bin/env python3
"""Fail-closed verdict check for a fuzz-soak report (soak-report.json).

Exit 0 only when the report proves a clean soak: at least one batch ran,
no diverged programs, and no failed minimization. Anything else — a
missing or unreadable report, zero batches, divergences, a failed
minimization — exits 1. A missing report is a failure, not a pass:
zero *verified* divergences is not the same as no report.

Used by both tools/fuzz-soak/run_soak.sh (overnight verdict) and the
fuzz-soak workflow's verdict step, so the gate logic has one source of
truth instead of two copies that can drift apart.

Usage: check_soak_report.py SOAK-REPORT.json
"""
import json
import sys


def verdict(rep):
    """Return (ok, message) for a parsed soak-report dict."""
    if rep.get("batches", 0) == 0:
        return False, "no batches ran (zero programs sampled)"
    if rep.get("minimize_failed"):
        return False, "minimization failed in at least one batch"
    if "diverged_total" not in rep:
        return False, "report has no diverged_total field"
    if rep["diverged_total"] > 0:
        return False, f"{rep['diverged_total']} diverged programs"
    return True, (f"zero divergences across {rep['batches']} batches")


def main():
    if len(sys.argv) != 2:
        print("usage: check_soak_report.py SOAK-REPORT.json", file=sys.stderr)
        return 2
    try:
        with open(sys.argv[1]) as f:
            rep = json.load(f)
    except (OSError, ValueError) as e:
        print(f"SOAK RED: soak-report.json missing or unreadable ({e}) — "
              "cannot verify zero divergences")
        return 1
    ok, msg = verdict(rep)
    print(f"SOAK {'GREEN' if ok else 'RED'}: {msg}")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
