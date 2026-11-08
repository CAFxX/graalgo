#!/usr/bin/env python3
"""Gate on a fuzz-soak report (R-1108-144).

Single verdict owner for the soak: `run_soak.sh` (overnight finale) and
the `fuzz-soak` workflow both call this script on `soak-report.json`,
so the verdict cannot drift between the two callers.

Exit 0 (CLEAN) only when the soak ran at least one batch, ran at least
one program, had zero failed batches, and found zero divergences.
Exit 1 (FAILED) when the report shows zero batches, zero programs run,
any failed batch, or any divergence. Exit 2 (ERROR) when the report is
missing or unreadable — a soak that produced no report is not clean.

Reports written before `failed_batches` / `programs_run` existed are
gated on the fields they do carry (`batches`, `diverged_total`).

Usage: check_soak_report.py <soak-report.json>
"""
import json
import sys


def main():
    if len(sys.argv) != 2:
        print(f"usage: {sys.argv[0]} <soak-report.json>", file=sys.stderr)
        return 2
    path = sys.argv[1]
    try:
        with open(path) as f:
            rep = json.load(f)
    except FileNotFoundError:
        print(f"SOAK FAILED: no soak-report.json at {path} "
              f"(soak produced no report)", file=sys.stderr)
        return 2
    except (OSError, ValueError) as e:
        print(f"SOAK FAILED: unreadable soak-report.json at {path}: {e}",
              file=sys.stderr)
        return 2
    batches = rep.get("batches", 0)
    diverged = rep.get("diverged_total", 0)
    failed = rep.get("failed_batches", 0)
    programs = rep.get("programs_run")
    if batches == 0:
        print(f"SOAK FAILED: report shows 0 batches run: {rep}",
              file=sys.stderr)
        return 1
    if programs is not None and programs == 0:
        print(f"SOAK FAILED: report shows 0 programs run: {rep}",
              file=sys.stderr)
        return 1
    if failed:
        print(f"SOAK FAILED: {failed} failed batch(es): {rep}",
              file=sys.stderr)
        return 1
    if diverged:
        print(f"SOAK FAILED: {diverged} diverged program(s): {rep}",
              file=sys.stderr)
        return 1
    print(f"soak verdict: CLEAN ({batches} batches, "
          f"{programs if programs is not None else '?'} programs, "
          f"0 diverged)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
