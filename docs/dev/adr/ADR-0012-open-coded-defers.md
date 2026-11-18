# ADR-0012 — Open-coded defers accepted (A8); safepoint preemption (A7) conditional; closures (A9) deferred

**Date:** 2026-11-18
**Status:** accepted 2026-11-18 (owner decision, relayed by the coordinator
instruction for lane `wt/spec/a8`) for A8; A7 *accepted conditionally on
prototype P-B*; A9 *deferred pending benchmarks*.
**Scope:** [SPEC.md](../../arch/SPEC.md) §3.5 and Appendix B; requirement
`GG-0019` → GG-0067 (locked, active); [truffle-design.md](../../arch/truffle-design.md)
§4.5, §14, §14.1 (amendments A7, A8, A9);
[RECOVERY.md](../../arch/RECOVERY.md) decision log.
**Evidence:** truffle-design H15 (the per-call `ArrayDeque` of `DeferChain`),
decision T7, §14.1 trade-offs; gc has open-coded defers since Go 1.14.

## Context

truffle-design §14 proposed three SPEC amendments that the new node package
would need (A7 safepoint preemption, A8 open-coded defers, A9 closures over
typed cells). Until the owner decided, the m1a-nodes lane followed SPEC for
all three. `GG-0019` required "defer SHALL be an explicit per-goroutine
chain" and SPEC §3.5 said open-coded defers are "a later phase, not v1".

## Decisions

1. **A8 accepted (`GG-0019` → GG-0067).** Defers are open-coded for the
   common case and chained otherwise:
   - a function whose defer statements are all outside loops and which is
     under the open-coded limit lowers them statically (frame slots plus a
     defer-bits word, run at function exit; no per-call chain allocation, no
     `G` access on the non-panic path);
   - a function with a defer in a loop, over the limit, or in a
     range-over-func body keeps the explicit per-goroutine defer chain
     (choice is per function, so the two orders never interleave — gc's
     rule);
   - the explicit chain is also the unwinding path for panic, recover and
     `Goexit`;
   - `recover`, `Goexit`, panics in deferred calls and `runtime.Caller` /
     `Callers` frames must match gc on both paths.
   GG-0067 keeps every obligation of `GG-0019` (panic is an
   `AbstractTruffleException` with lazy traces; `Goexit` is a
   `ControlFlowException`; `panic(nil)` yields `*PanicNilError`; the rejected
   `ControlFlowException`-for-panic alternative stays rejected) and makes the
   Go-spec/gc defer, recover and `runtime.Caller` obligations explicit. Its
   verification plan requires every defer/panic/recover/Goexit corpus program
   to run on both paths (open-coded, and a test-only switch forcing the
   chain) with identical output, so neither path can regress unobserved.
   SPEC §3.5 and Appendix B are amended; truffle-design §4.5 is the design.
   *Not changed by this decision:* the v1 node classes still build the
   chain only; open-coding is TR-3 work in the new node package. The
   `verifies:` annotation of `PanicCorpusTest` was re-pointed to GG-0067
   (it verifies the retained semantics); the both-paths obligation has no
   test yet — a follow-up that lands with TR-3.
2. **A7 accepted conditionally on P-B `[OPEN]`.** The owner will switch from
   `Safepoint.poll()` to Truffle-safepoint preemption only if prototype P-B
   (truffle-design §13.2: fairness and torture suites with the monitor +
   `ThreadLocalAction` implementation behind a switch) validates. Until then
   SPEC, `GG-SCHED-0001` and `GG-0017` are unchanged, `Safepoint.poll()`
   stays, and no requirement is superseded. truffle-design §14 records the
   status; SPEC is deliberately not amended.
3. **A9 deferred pending benchmarks.** The owner decides closures over typed
   cells after the closure benchmarks. SPEC §3.4 is unchanged
   (`{RootCallTarget, MaterializedFrame|null}`); the node package follows
   SPEC (rule F2's single allowed `materialize()`).

## Consequences

- `GG-0019` is superseded by GG-0067; `req supersede` re-pointed the live
  references (SPEC, RECOVERY, truffle-design, Java comments, the panic
  corpus README, `PanicCorpusTest`'s annotation). Historical records (phase
  plans and checkpoints, the M5 review snapshot, the spec extract, two
  README status notes dated 2026-10-04) quote `GG-0019` in code font so they
  keep their meaning. `req supersede` mis-computes line numbers in Markdown
  files with fenced code blocks (truffle-design.md was edited by hand where it
  missed).
- truffle-design §14/§14.1 and the RECOVERY decision log carry the three
  statuses; TR-3 builds open-coded defers with the chain fallback, TR-6
  remains conditional on P-B, closure nodes follow SPEC.
