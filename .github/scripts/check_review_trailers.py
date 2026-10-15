#!/usr/bin/env python3
"""
Review-trailer gate (2026-10-15, his direct order).

Before ANYTHING merges to master, it MUST have a clean review from an
independent reviewer — a fresh subagent, NOT the agent that wrote the code.
The reviewer audits for bodges, bridges, hacks, workarounds, special cases,
correctness, and test coverage, and has veto power. Advisory brief text
failed to enforce this (lanes integrated anyway), so the enforcement is
MECHANICAL: every non-merge commit in the checked range must carry review
evidence in its git trailer block.

Pass conditions per commit (checked in order):
  1. VETO: a "Review-veto:" / "Vetoed-by:" trailer is a hard FAIL
     (reviewer veto power), regardless of anything else.
  2. "Reviewed-by: <identity>" where <identity> names someone OTHER than the
     commit author (name and email compared case-insensitively) -> PASS.
  3. "Review-record: <path-or-url>" pointing to the recorded verdict:
       - an https:// URL is accepted as an audit pointer, or
       - an in-repo path, verified to exist AT THAT COMMIT
         (git cat-file -e <sha>:<path>) -> PASS.
     Anything else -> FAIL.

Trailer parsing follows git conventions: the trailer block is the trailing
run of "Token: value" lines at the very end of the commit message
(continuation lines starting with whitespace are folded into the value).
Trailer keys are matched case-insensitively.

Range resolution (first wins):
  1. --range BASE..HEAD            (explicit)
  2. $GATE_BASE_SHA               (CI: ${{ github.event.before }} on push to
                                   master — the TRUE push range, not just the
                                   tip commit)
  3. <remote>/master..HEAD        (lane worktree: commits not yet on master;
                                   tries origin, main, upstream in order)

The check NEVER passes vacuously: an unresolvable range (bad SHA, all-zero
SHA, no origin/master) is a loud failure, never a skip. A resolved range
with zero commits is a real answer ("nothing new to review") and passes.

Identity convention: reviewer subagents are spawned by the coordinator;
record them as e.g.  "Reviewed-by: reviewer-<stream> (<agent-uuid>)".
The gate only enforces presence + reviewer != author; authenticity of the
review is a process matter (the Review-record pointer gives auditability).
CI enforcement: .github/workflows/review-gate.yml.
"""

import os
import re
import subprocess
import sys

TRAILER_RE = re.compile(r"^([A-Za-z][A-Za-z0-9-]*):[ \t]*(.*?)[ \t]*$")
ZERO_SHA = re.compile(r"^0+$")
URL_RE = re.compile(r"^https?://\S+$", re.I)


def run(cmd):
    try:
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
    except Exception as e:
        return 1, "", str(e)
    return r.returncode, r.stdout, r.stderr


def fail(msg):
    print("REVIEW-GATE CHECK FAILED:")
    print(f"  {msg}")
    return 1


def resolve_range():
    """Return (base, head, label) or print a loud failure and return None."""
    for i, a in enumerate(sys.argv):
        if a == "--range" and i + 1 < len(sys.argv):
            spec = sys.argv[i + 1]
            if ".." not in spec:
                print("REVIEW-GATE CHECK FAILED:")
                print(f"  --range must be BASE..HEAD, got: {spec!r}")
                return None
            base, head = spec.split("..", 1)
            return base.strip(), (head.strip() or "HEAD"), f"--range {spec}"

    base = os.environ.get("GATE_BASE_SHA", "").strip()
    if base:
        if ZERO_SHA.fullmatch(base):
            print("REVIEW-GATE CHECK FAILED:")
            print("  GATE_BASE_SHA is all zeros — cannot determine the push")
            print("  range (is this the first push of a new ref?). Refusing")
            print("  to pass vacuously.")
            return None
        rc, _, _ = run(["git", "cat-file", "-e", base])
        if rc != 0:
            print("REVIEW-GATE CHECK FAILED:")
            print(f"  GATE_BASE_SHA={base} does not resolve to a commit.")
            print("  Refusing to pass vacuously.")
            return None
        return base, "HEAD", "$GATE_BASE_SHA..HEAD (push range)"

    for remote in ("origin", "main", "upstream"):
        ref = f"{remote}/master"
        rc, _, _ = run(["git", "rev-parse", "--verify", ref])
        if rc == 0:
            return ref, "HEAD", f"{ref}..HEAD"
    print("REVIEW-GATE CHECK FAILED:")
    print("  could not determine any range to inspect: no --range, no")
    print("  GATE_BASE_SHA, and no origin/master, main/master, or")
    print("  upstream/master (shallow checkout?).")
    print("  Refusing to pass vacuously.")
    return None


def trailer_block(body):
    """Parse the trailing run of 'Token: value' lines of a commit message."""
    trailers = []
    for line in reversed(body.rstrip("\n").split("\n")):
        m = TRAILER_RE.match(line)
        if m:
            trailers.append([m.group(1), m.group(2)])
            continue
        if line[:1] in (" ", "\t") and trailers:
            # Continuation line: fold into the previous trailer's value.
            trailers[-1][1] = line.strip() + " " + trailers[-1][1]
            continue
        break
    return [(k, v) for k, v in reversed(trailers)]


def norm(s):
    return re.sub(r"\s+", " ", s.strip()).lower()


def check_commit(sha):
    rc, body, _ = run(["git", "log", "-1", "--format=%B", sha])
    rc2, ident, _ = run(["git", "log", "-1", "--format=%an%x00%ae", sha])
    if rc != 0 or rc2 != 0:
        return [f"{sha[:8]}: could not read commit metadata"]
    author_name, _, author_email = ident.strip().partition("\x00")
    trailers = trailer_block(body)
    keys = {k.lower(): v for k, v in trailers}

    errors = []
    if "review-veto" in keys or "vetoed-by" in keys:
        veto = keys.get("review-veto", keys.get("vetoed-by"))
        errors.append(
            f"{sha[:8]}: REVIEWER VETO — "
            f"'{veto}' — this commit must not land on master"
        )
        return errors

    reviewed_by = keys.get("reviewed-by", "").strip()
    if reviewed_by:
        r = norm(reviewed_by)
        if r == norm(author_name) or r == norm(author_email) \
                or norm(author_email) in r or norm(author_name) in r:
            errors.append(
                f"{sha[:8]}: SELF-REVIEW — Reviewed-by names the author "
                f"({author_name} <{author_email}>); the reviewer must be an "
                f"independent agent"
            )
        else:
            return []  # valid independent review
    elif "review-record" in keys:
        rec = keys["review-record"].strip()
        if URL_RE.match(rec):
            return []  # audit pointer to an external recorded verdict
        rc3, _, _ = run(["git", "cat-file", "-e", f"{sha}:{rec}"])
        if rc3 == 0:
            return []  # in-repo review record exists at this commit
        errors.append(
            f"{sha[:8]}: Review-record pointer '{rec}' does not exist in "
            f"the repo at this commit"
        )
    else:
        subject = body.strip().split("\n", 1)[0][:72]
        errors.append(
            f"{sha[:8]} ({subject}): no review evidence — add "
            f"'Reviewed-by: <reviewer-id>' (reviewer != author) or "
            f"'Review-record: <path-or-url>' to the commit trailer block"
        )
    return errors


def main():
    resolved = resolve_range()
    if resolved is None:
        return 1
    base, head, label = resolved

    rc, out, err = run(["git", "rev-list", "--no-merges", "--reverse",
                        f"{base}..{head}"])
    if rc != 0:
        return fail(f"could not list commits in {label}: {err.strip()}")
    shas = [s for s in out.split() if s]
    print(f"Review-trailer gate: inspecting {len(shas)} commit(s) from {label}.")

    if not shas:
        print("No new commits in range — nothing to review. Gate passes.")
        return 0

    errors = []
    for sha in shas:
        errors.extend(check_commit(sha))

    if errors:
        print("REVIEW-GATE CHECK FAILED:")
        print()
        for e in errors:
            print(f"  ✗ {e}")
        print()
        print("His order (2026-10-15): independent review before ANYTHING")
        print("merges to master — no exceptions. The lane does not integrate")
        print("itself: report branch + evidence, the coordinator spawns a")
        print("fresh reviewer subagent, and the Reviewed-by: trailer is added")
        print("(trailer-only amend) after a clean review.")
        return 1

    print(f"Review-trailer gate passed: all {len(shas)} commit(s) carry "
          "independent review evidence.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
