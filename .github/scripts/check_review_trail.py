#!/usr/bin/env python3
"""
Review-trail check (his 2026-10-15 direct order: independent review before
ANYTHING merges to master).

Fails the push when any commit in the push delta lacks the review trailers:
    Reviewed-by: <independent-reviewer-id>
    Review-verdict: clean

Convention documented in docs/dev/review-trail.md; enforced by
.github/workflows/review-trail.yml on pushes to master.

Usage: check_review_trail.py <before-sha> <sha> [--repo DIR]

Exit 0: every commit in before..sha carries both trailers (or the check is
not applicable — see below). Exit 1: at least one commit lacks a trailer.
Exit 2: usage / git errors.

Not applicable (exit 0 with a notice, not a failure):
- before is the zero SHA (branch/repo creation: no delta to audit).
- the workflow file did not exist at `before`: the rule was not in force
  for this push (bootstrap exemption — the push that lands the workflow
  itself is covered by the coordinator's manual review process, and every
  later push is checked).
"""

import re
import subprocess
import sys

ZERO_SHA = "0" * 40
WORKFLOW_PATH = ".github/workflows/review-trail.yml"

REVIEWED_BY = re.compile(r"^Reviewed-by:\s*\S+", re.IGNORECASE | re.MULTILINE)
VERDICT_CLEAN = re.compile(r"^Review-verdict:\s*clean\s*$", re.IGNORECASE | re.MULTILINE)


def git(repo, *args):
    r = subprocess.run(
        ["git", "-C", repo, *args], capture_output=True, text=True)
    if r.returncode != 0:
        print(f"git {' '.join(args)} failed:\n{r.stderr}", file=sys.stderr)
        sys.exit(2)
    return r.stdout


def main(argv):
    if len(argv) < 3:
        print(__doc__.strip().splitlines()[-4], file=sys.stderr)
        sys.exit(2)
    before, sha = argv[1], argv[2]
    repo = "."
    if "--repo" in argv:
        repo = argv[argv.index("--repo") + 1]

    if before == ZERO_SHA or set(before) == {"0"}:
        print("NOTICE: zero before-SHA (new ref) — nothing to audit; skipping.")
        return 0

    # Bootstrap exemption: the rule binds only pushes made after it landed.
    has_workflow = subprocess.run(
        ["git", "-C", repo, "cat-file", "-e", f"{before}:{WORKFLOW_PATH}"],
        capture_output=True).returncode == 0
    if not has_workflow:
        print(f"NOTICE: {WORKFLOW_PATH} did not exist at {before[:12]} — "
              "rule not yet in force for this push; skipping.")
        return 0

    commits = git(repo, "rev-list", f"{before}..{sha}").split()
    if not commits:
        print("review-trail: empty push delta — nothing to audit.")
        return 0

    bad = []
    for c in commits:
        body = git(repo, "log", "-1", "--format=%B", c)
        missing = []
        if not REVIEWED_BY.search(body):
            missing.append("Reviewed-by:")
        if not VERDICT_CLEAN.search(body):
            missing.append("Review-verdict: clean")
        if missing:
            bad.append((c, missing))

    if bad:
        print("REVIEW-TRAIL GATE FAILED — commits without a clean "
              "independent-review trailer:")
        for c, missing in bad:
            subject = git(repo, "log", "-1", "--format=%s", c).strip()
            print(f"  {c[:12]} {subject}  [missing: {', '.join(missing)}]")
        print("See docs/dev/review-trail.md for the trailer convention.")
        return 1

    print(f"review-trail: {len(commits)} commit(s) in push delta, "
          "all carry Reviewed-by + Review-verdict: clean.")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
