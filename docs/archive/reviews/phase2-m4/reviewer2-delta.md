# Reviewer 2 — DELTA RE-VERIFICATION of ADR-0002 remediation (P2-M4)

**Reviewer:** independent reviewer 2 of 2 · **Date:** 2026-10-05
**Scope:** commit `3d94f0f` vs snapshot `8044d2a`, restricted to
`phases/phase2/adr/ADR-0002-gc-interop.md` and `phases/phase2/PLAN.md`.
**Mode:** read-only; the full diff was read, and every one of my original
P1-1…P1-6, P2-1…P2-4 findings was checked against the remediated text.
Reviewer 1's findings are not re-verified here (their delta is theirs).

## Verdict: DELTA-GO

All 10 of my findings (6 P1 + 4 P2) are genuinely addressed in `3d94f0f`.
The remediation is substantive, not cosmetic: the D4 table was rewritten
rather than reworded (shared Cleaner, MXBean-poll GC, field-table
commitment), the barrier gate gained a correctly-labeled rationale, and the
blocking closeout checklist exists with evidence files. The ADR may proceed
to implementation. Two non-blocking observations are noted at the end.

## Per-finding verification

1. **P1-1 (finalizer/exporter residual risk) — ADDRESSED.** The D4
   `SetFinalizer` divergences now contain an explicit "Residual risk
   (exporter mapping)" bullet: the registered Java object must stay
   JVM-reachable exactly as long as Go considers the value live, the spike
   cannot catch a boxing/copy early-death, and "the P2-M4 differential
   corpus must include a finalizer-lifetime test driven through the real
   `runtime.SetFinalizer` exporter, including a remove-and-replace case."
   Removal (`SetFinalizer(x, nil)` → cancelable wrapper, Cleaner has no
   unregister) and resurrection-impossible are recorded as explicit rows,
   as demanded.

2. **P1-2 (KeepAlive vehicle) — ADDRESSED.** The D4 row now names
   `Reference.reachabilityFence(x)` at the intrinsic as the vehicle —
   the exact mechanism the finding required, replacing the no-op-intrinsic
   aspiration. The P2-M4 closeout checklist item 1 requires the "KeepAlive
   litmus under C2", i.e. the classic reachabilityFence differential test
   is a blocking deliverable.

3. **P1-3 (GC() blocking overclaim) — ADDRESSED.** The row is now a
   documented approximation with the precise divergence stated in the
   stance column: "callers must not depend on reclamation having
   completed" — the exact no-post-GC-reclamation-asserts rule required.
   The vehicle was additionally strengthened beyond the minimum:
   `System.gc()` + poll of GC MXBeans until collection counts advance,
   bounded timeout, with explicit-GC-disabled returning as a documented
   divergence. No "blocks until complete" language remains.

4. **P1-4 (ReadMemStats punt) — ADDRESSED.** The row now commits P2-M4 to a
   field-by-field table ("exact / approximated-with-mechanism /
   unavailable-with-justification per `MemStats` field") and routes every
   unmappable field to `DIVERGENCES.md` per the P2-M10 discipline. The
   field table is named in closeout checklist item 1, so it cannot be
   silently dropped.

5. **P1-5 ([MEASURED] overreach) — ADDRESSED.** All three tags are now
   scoped: `[MEASURED: default GC, Java-local roots, channel-waiter park
   path]` and `[MEASURED: same scope]`. Truffle-frame root coverage is an
   explicit verification-backlog item ("the spike exercised Java locals;
   materialized/interpreter frames need their own weakref-torture case"),
   as is re-running the spike per supported collector. On the re-test
   trigger: the literal "change of default JVM GC" wording was replaced
   with "change of supported collector set". I judge this an acceptable,
   in fact stronger, form: the new collector decision names "G1 the
   tested default" explicitly, so a default change alters the decision
   record, and per-collector re-run is now a backlog item plus F4 runs
   per collector. The trigger's function is preserved, not dropped.

6. **P1-6 (procedural: no closeout on ADR sign-off alone) — ADDRESSED.**
   The ADR now carries "## P2-M4 closeout checklist (blocking — sign-off
   requires all three with evidence files)" naming exactly the three
   required deliverables: (1) D4 surface per the corrected table,
   evidence "implementation + differential tests"; (2) root/barrier
   differential suite green vs gc, evidence "suite log + corpus JSON,
   0 unexplained divergences"; (3) off-heap grep gate in `make check`
   and green, evidence "`make check` log". Milestone sign-off is
   textually blocked until all three cite evidence.

7. **P2-1 (spin-GC precision) — ADDRESSED.** Consequences now reads:
   promote the check against a **named retention policy** ("dead
   goroutine stacks releasable within N ms of exit") and cover
   accumulation with a soak differential vs gc, noting the spike's ≤20
   spin-GCs falsify permanent retention only.

8. **P2-2 (UnsafeHandles/finalizer interaction) — ADDRESSED.** Recorded
   as an explicit bullet: handle-held-only objects are never collected,
   so `SetFinalizer` on them silently never runs; "P2-M4 records the
   decision in `DIVERGENCES.md` (weak handle entries vs documented
   never-run)."

9. **P2-3 (allocateInstance miscategorization) — ADDRESSED.** The grep
   gate lists only genuine off-heap shapes (`allocateMemory`,
   `MemorySegment`, `Arena`, `allocateDirect`, reflective acquisition,
   `sun.misc.*`/`jdk.internal.misc.*` imports) with a one-line rationale
   per pattern, and states the rationale discipline explicitly:
   "`Unsafe.allocateInstance` creates an ordinary *heap* object (no
   barrier hole) — it is not listed under the barrier gate." The wrong
   invariant is no longer taught.

10. **P2-4 (canonical spike path) — ADDRESSED.** The header now reads:
    "**Spike evidence:** `phases/phase2/hidden_files/p2-m4-spike/`
    (canonical; re-run recipe in its README; provenance: `/tmp/gcspike`,
    throwaway)." The /tmp path is provenance only, as required.

## Status-line honesty — CONFIRMED

The header states "**Status:** remediated 2026-10-05 … Delta
re-verification by both reviewers pending — implementation starts after
they confirm." It does **not** claim re-verification has occurred. The
"all 13 P1 + 9 P2 findings are addressed in this revision" claim is the
author's assertion under verification; for my 6+4 it holds (reviewer 1's
7+5 are for their delta). The line is honest.

## Observations (non-blocking)

1. PLAN.md §4's P2-M4 acceptance line ("design spike + ADR first …
   barrier/root differential vs gc") was not updated to cross-reference
   the ADR's new blocking closeout checklist. The gate exists and is
   textual in the ADR, so the remediation target is met; but a pointer
   from PLAN.md §4 to the ADR checklist would make the gate discoverable
   from the plan itself. Suggested as a one-line follow-up, not a
   re-verification blocker. (Note: §4's line also still says
   "Truffle-level write barriers", which the ADR rejected — pre-existing
   staleness, outside my findings, but worth fixing in the same pass.)
2. The "13 P1 + 9 P2" count in the status line implies reviewer 1 had
   7 P1 + 5 P2; that arithmetic is consistent with my 6+4, but I did not
   verify reviewer 1's remediation — their delta stands on its own.

## Bottom line

Remediation verified: 10/10 of my findings addressed with substance, no
finding papered over, status line honest about pending re-verification.
**DELTA-GO** — ADR-0002 may proceed to implementation.
