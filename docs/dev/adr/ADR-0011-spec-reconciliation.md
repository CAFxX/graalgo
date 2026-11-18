# ADR-0011 — Spec reconciliation (GODEBUG, frontend provenance, blocking I/O wording)

**Date:** 2026-11-18
**Status:** accepted 2026-11-18 (coordinator instruction relaying the program
owner's decisions from the global spec-conformance review; owner
confirmation of the lock is requested in the review report).
**Scope:** [SPEC.md](../../arch/SPEC.md) §3.2, §3.3, §3.5, §4.5, §7.15, Appendix B, Appendix C;
requirements `GG-0055` → GG-0065 (locked), `GG-0012` → GG-0066 (locked, active),
`GG-SCHED-0004` → GG-SCHED-0005 (locked).
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

2. **Frontend provenance and GIR (`GG-0012` → GG-0066; review items B2,
   D1).** toolexec-design D10 (implemented by W1/W2:
   `internal/toolexec/release`, `TestBuiltByTrackedToolchain`) takes
   `go/parser` + `go/types` from the toolchain that builds `graalgo-tool` and
   asserts at build/test time that it is the tracked release. `GG-0012`
   required a vendored copy, re-vendored atomically and verified
   byte-identical to upstream — a copy the new path does not use (the one
   under `internal/frontend/go` serves v1 and is deleted at M3). GG-0066
   keeps the substance (the frontend *is* the tracked release's `go/*`;
   frontend and stdlib move as one unit) and replaces the verification with
   the toolchain assertion. SPEC §3.2/§3.3 now describe the actual pipeline:
   the frontend runs at compile time inside `graalgo-tool` and serializes a
   resolved typed AST (GIR) into each package archive; the JVM-side node
   factory decodes GIR, never Go source. GG-0066 is locked and active: v1's
   frontend tests (annotations re-pointed) and the toolexec module's
   `TestBuiltByTrackedToolchain` exercise it; the latter has no `verifies:`
   annotation yet (code follow-up).

3. **Blocking I/O wording (`GG-SCHED-0004` → GG-SCHED-0005; review item
   B6).** `GG-SCHED-0004` required "the slot-releasing NIO wrapper"; since
   GG-0063 (ADR-0009 decision 3) there is no blocking NIO — verbatim
   `syscall`/`internal/poll`/`net` reach the runtime as raw system calls
   through the Panama executor, and SPEC §3.5 already said the wrapper
   brackets blocking downcalls while readiness waits park in the epoll
   poller. GG-SCHED-0005 (locked, parent GG-0017) states exactly that; the
   mechanism (`ioEnter`/`ioExit`, `wakeFifo` re-acquire, the unpaired-
   `ioEnter` assert) and its falsifier are unchanged. GG-0063's locked text
   cites `GG-SCHED-0004`; the registry's `superseded_by` link resolves it.
4. **Goroutine-local state (review item D3).** SPEC §3.5 said goroutine-local
   state lives in "virtual-thread-scoped holders". The decided design
   ([truffle-design.md](../../arch/truffle-design.md) T3, §2.5–§2.6) keeps it
   in a per-goroutine `G` reached through the context's thread-local slot,
   and goroutine threads may be recycled, so thread identity is not
   goroutine identity. §3.5 now says so and points to the `[OPEN]` item on
   goroutine threads in native-image modes (truffle-design A1). No
   requirement text is affected (GG-0017 says "virtual thread plus a
   scheduler token", which T3 keeps on HotSpot).

## Consequences

- SPEC §4.5 and §7.15 state the gc-compatible rule; §3.2/§3.3 the GIR
  pipeline; §3.5 the downcall wording and the `G` object; Appendix B lists
  `GG-0055`, `GG-0012` and `GG-SCHED-0004` as superseded.
- Code comments and `verifies:` annotations that named the superseded IDs
  were re-pointed by `req supersede` (text only), except the GODEBUG tests
  of decision 1.
- Historical records (phase plans and checkpoints, ADR-0002) quote the old
  ID in code font so they keep their meaning.
