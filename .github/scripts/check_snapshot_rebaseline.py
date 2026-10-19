#!/usr/bin/env python3
"""
Snapshot re-baseline guard (2026-10-19, G-2 review finding).

`make snapshot` re-records the behavior baseline in test/snapshots/. A
commit that changes behavior AND re-records the snapshot in the same change
passes `snapshot-check` green — the drift is laundered through the
re-record, with no record of what changed. This gate closes that hole: if
test/snapshots/*.json changed in the pushed range, EVERY commit that
touched it must carry an explicit trailer in its commit message:

    Snapshot-Rebaseline: <what deliberately changed and why>

The trailer is the human's declaration that the drift is intended. A bare
`Snapshot-Rebaseline:` line (empty value) is rejected — the value must name
the deliberate change. Re-recording is checked commit by commit, so a
re-record cannot ride along inside an unrelated commit of the same push.

Range determination, in order:
  1. $GITHUB_PUSH_BEFORE..HEAD  (direct push to the tracked branch: CI
     threads github.event.before in via GITHUB_PUSH_BEFORE, same as the
     no-bridges gate; --before SHA serves local/fixture runs)
  2. origin/master..HEAD        (PR / feature branch)
  3. otherwise: fail loudly — the gate never passes vacuously.

Uncommitted or staged changes under test/snapshots/ also fail loudly: a
re-record must be committed with the trailer, never left dirty.
"""

import os
import re
import subprocess
import sys

SNAPSHOT_DIR = "test/snapshots"
TRAILER_RE = re.compile(r"(?m)^Snapshot-Rebaseline:[ \t]*\S")


def run(cmd, cwd=None):
    return subprocess.run(cmd, capture_output=True, text=True, timeout=30,
                          cwd=cwd)


def get_push_before():
    """Pre-push ref, or None. Same contract as check_no_bridges.py."""
    before = ""
    if "--before" in sys.argv:
        i = sys.argv.index("--before")
        if i + 1 < len(sys.argv) and not sys.argv[i + 1].startswith("--"):
            before = sys.argv[i + 1]
    if not before:
        before = os.environ.get("GITHUB_PUSH_BEFORE", "")
    before = before.strip()
    if not before or set(before) == {"0"}:
        return None
    return before


def rev_parse(ref):
    r = run(["git", "rev-parse", "--verify", "--quiet", ref])
    if r.returncode != 0:
        return None
    return r.stdout.strip()


def determine_range():
    """Return (range_spec, label) or (None, error_message)."""
    before = get_push_before()
    head = rev_parse("HEAD")
    master = rev_parse("origin/master")
    if before:
        if rev_parse(before + "^{commit}") is None:
            return None, (f"GITHUB_PUSH_BEFORE={before} does not resolve "
                          "in this checkout (CI must use fetch-depth: 0).")
        if master is not None and head == master:
            # Direct push to the tracked branch (wt.py integrate + push):
            # the pushed range is exactly before..HEAD.
            return f"{before}..HEAD", f"push range {before[:12]}..HEAD"
        if master is not None:
            # PR / feature branch: the whole branch range (a superset of
            # the latest push — conservative and correct).
            return "origin/master..HEAD", "origin/master..HEAD"
        # No origin (local/fixture runs): the before ref is the pushed range.
        return f"{before}..HEAD", f"push range {before[:12]}..HEAD"
    if master is not None and head != master:
        return "origin/master..HEAD", "origin/master..HEAD"
    return None, ("could not determine the pushed range (no GITHUB_PUSH_BEFORE "
                  "and no origin/master to diff against). Refusing to pass "
                  "vacuously.")


def snapshot_touched_by(sha):
    r = run(["git", "diff-tree", "--no-commit-id", "--name-only", "-r", sha])
    if r.returncode != 0:
        return []
    return [n for n in r.stdout.splitlines()
            if n.startswith(SNAPSHOT_DIR + "/") and n.endswith(".json")]


def commit_message(sha):
    r = run(["git", "log", "-1", "--format=%B", sha])
    return r.stdout if r.returncode == 0 else ""


def commit_subject(sha):
    r = run(["git", "log", "-1", "--format=%s", sha])
    return r.stdout.strip() if r.returncode == 0 else sha


def dirty_snapshot_files():
    """Uncommitted/staged changes to the snapshot JSON files (not docs).

    Only *.json under test/snapshots/ is the behavior baseline — a dirty
    README.md next to it cannot launder a behavior change and must not
    trip the guard.
    """
    r = run(["git", "status", "--porcelain", "--", SNAPSHOT_DIR])
    if r.returncode != 0:
        return []
    dirty = []
    for line in r.stdout.splitlines():
        # porcelain v1: "XY <path>" or "XY <orig> -> <new>" on renames.
        path = line[3:].split(" -> ")[-1].strip().strip('"')
        if path.startswith(SNAPSHOT_DIR + "/") and path.endswith(".json"):
            dirty.append(line.strip())
    return dirty


def main():
    # A re-record must be committed with the trailer — never left dirty.
    dirty = dirty_snapshot_files()
    if dirty:
        print("SNAPSHOT RE-BASELINE GUARD FAILED:")
        print("  uncommitted changes to snapshot baselines:")
        for line in dirty:
            print(f"    {line}")
        print("  Commit the re-recorded snapshot with a Snapshot-Rebaseline:")
        print("  trailer (see test/snapshots/README.md), then push.")
        return 1

    range_spec, label = determine_range()
    if range_spec is None:
        print("SNAPSHOT RE-BASELINE GUARD FAILED:")
        print(f"  {label}")
        return 1

    r = run(["git", "log", "--format=%H", "--no-merges", range_spec])
    if r.returncode != 0:
        print("SNAPSHOT RE-BASELINE GUARD FAILED:")
        print(f"  could not list commits in {label}: {r.stderr.strip()}")
        return 1
    shas = r.stdout.split()
    print(f"Snapshot re-baseline guard: inspecting {len(shas)} commit(s) "
          f"in {label}.")

    offenders = []
    for sha in shas:
        touched = snapshot_touched_by(sha)
        if not touched:
            continue
        if not TRAILER_RE.search(commit_message(sha)):
            offenders.append((sha[:12], commit_subject(sha), touched))

    if offenders:
        print("SNAPSHOT RE-BASELINE GUARD FAILED:")
        print()
        for sha, subject, touched in offenders:
            print(f"  ✗ {sha} {subject}")
            for f in touched:
                print(f"      touches {f} with no Snapshot-Rebaseline: trailer")
        print()
        print("  A commit that re-records the behavior baseline must declare")
        print("  the deliberate change in its commit message, e.g.:")
        print()
        print("      Snapshot-Rebaseline: deliberate output change in 01_hello")
        print("      after the channel-close semantic fix (PKG-123)")
        print()
        print("  Convention documented in test/snapshots/README.md.")
        return 1

    print("Snapshot re-baseline guard passed.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
