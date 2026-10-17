# Review trail — independent review before merge

**Order (2026-10-15, his direct order):** before ANYTHING merges to master,
it MUST have a clean review from an **independent reviewer** — a new
subagent, NOT the agent that wrote the code. The reviewer audits for bodges,
bridges, hacks, workarounds, special cases, correctness, and test coverage,
and has veto power. The lane does NOT integrate directly; it reports
completion, the coordinator spawns an independent reviewer, and only on a
clean review does the integration proceed. No exceptions, even for "trivial"
changes.

This document makes that order mechanically enforceable: every pushed commit
must carry a review trailer, and CI fails the push otherwise.

## The trailer convention

Every commit merged to master carries two trailers in its message:

```
Reviewed-by: <reviewer-id>
Review-verdict: clean
```

- `Reviewed-by:` — the independent reviewer's identity: the review
  subagent's id (e.g. `b3f1a2c4-…`), or a stable reviewer name for human
  review. It must name the reviewer, not the author.
- `Review-verdict: clean` — the reviewer's verdict. Only `clean` merges.
  Any other value (`changes-requested`, …) fails the gate the same as a
  missing trailer.

## Who adds the trailers, and when

1. The lane finishes its work and reports completion (it does NOT integrate).
2. The coordinator spawns an independent review subagent; the reviewer
   audits and either clears the change or vetoes with findings.
3. On a clean review, the **lane** adds both trailers to each of its commits
   (the reviewer never touches the author's commits):

   ```sh
   git rebase -i <merge-base> --exec \
     'git interpret-trailers --in-place \
        --trailer "Reviewed-by: <reviewer-id>" \
        --trailer "Review-verdict: clean" HEAD'
   ```

   then re-verifies (`make all` green) and reports back. A rebase voids any
   earlier approval: the trailers go on the final, rebased commits only.
4. The coordinator integrates (`wt.py integrate`).

## Mechanical enforcement

[.github/workflows/review-trail.yml](../../.github/workflows/review-trail.yml)
runs on every push to master and executes
[.github/scripts/check_review_trail.py](../../.github/scripts/check_review_trail.py),
which fails the push when any commit in the push delta (`before..sha`)
lacks `Reviewed-by:` + `Review-verdict: clean`.

Two deliberate exemptions (both print a notice, both are the documented
behavior, not loopholes):

- **Zero `before` SHA** (new ref): no delta exists to audit.
- **Bootstrap**: if the workflow file did not exist at `before`, the rule
  was not in force for that push — the push that lands the workflow itself
  is covered by the coordinator's manual review, and every later push is
  checked. The order is not applied ex post facto.

Lane branches are not gated: work-in-progress commits land there, and the
trailers are added after the independent review, before integration.
