# ADR-0011 — Spec reconciliation (GODEBUG, frontend provenance, blocking I/O wording)

**Date:** 2026-11-18
**Status:** accepted 2026-11-18 (coordinator instruction relaying the program
owner's decisions from the global spec-conformance review; owner
confirmation of the lock is requested in the review report).
**Scope:** [SPEC.md](../../arch/SPEC.md) §4.5, §7.15, Appendix B;
requirement `GG-0055` (superseded) → GG-0065 (new, locked).
**Evidence:** the global spec-conformance review of 2026-11-18 (items A2, B2,
B6, D1, D3); each finding was re-checked against the tree at `cbb2ebd0`
before it was acted on.

## Context

SPEC.md is authoritative, and the recovery designs
([toolexec-design.md](../../arch/toolexec-design.md),
[truffle-design.md](../../arch/truffle-design.md)) and the code built since
2026-11-17 implement it. The spec-conformance review found places where the
locked requirement text and the decided design or gc's own behaviour had
drifted apart without an amendment. This ADR records the requirement-level
fixes; the launcher's temporary deviations are recorded separately in
[ADR-0010](ADR-0010-launcher-deviations.md).

## Decisions

1. **GODEBUG unknown keys (`GG-0055` → GG-0065; review item A2).** gc parses
   `GODEBUG` and silently ignores keys it does not define (a program built by
   go1.27 runs unchanged with a go1.28 knob in its environment). `GG-0055`
   required graalgo to deny unknown knobs with an error, which makes
   `alias go=graalgo` observably different from gc (GG-0001/GG-0002) for any
   environment that carries a knob of another release or a typo. GG-0065
   keeps the per-release registry and its review, accepts unknown keys
   silently, and denies only non-default values of known knobs classified
   *unsupported* (plus the values gc itself rejects fatally).
   *Not changed by this decision:* the v1 launcher's `GoDebug` validator and
   `GoDebugTest` still implement the superseded deny-unknown behaviour; that
   is a code follow-up. The six tests that assert a rejection gc does not
   make (unknown key, near-miss key, unknown key at startup, bare field
   without `=`, malformed value, non-ASCII digits — gc's `parsegodebug`
   skips all of them silently) lost their `verifies:` annotations in the
   supersession; the other annotations were re-pointed to GG-0065.

## Consequences

- SPEC §4.5 and §7.15 state the gc-compatible rule; Appendix B lists
  `GG-0055` as superseded.
- Historical records (phase plans and checkpoints, ADR-0002) quote the old
  ID in code font so they keep their meaning.
