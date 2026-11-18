# ADR-0012 — Open-coded defers accepted (A8); safepoint preemption (A7) conditional; closures (A9) deferred; 2026-11-18 requirements re-issued as drafts

**Date:** 2026-11-18
**Status:** accepted 2026-11-18 (owner decision, relayed by the coordinator
instruction for lane `wt/spec/a8`) for A8; A7 *accepted conditionally on
prototype P-B*; A9 *deferred pending benchmarks*. The A8 *decision* is the
owner's; the requirement text `GG-0067` that encoded it was written, locked
and activated by the lane without owner sign-off. **Re-issue (owner
decision, 2026-11-18):** `GG-0067` and the three requirements of
[ADR-0011](ADR-0011-spec-reconciliation.md) are superseded by drafts with
the wording of an independent review; `GG-0067` is split into a semantics
and a mechanism requirement (decision 4). All five drafts **await owner
sign-off**: the owner locks them after reading; until then nothing here is
locked.
**Scope:** [SPEC.md](../../arch/SPEC.md) §3.2, §3.5, §4.5, §7.15 and Appendix B; requirements
`GG-0019` → `GG-0067` → GG-0070 + GG-0071, `GG-0065` → GG-0068, `GG-0066` →
GG-0069, `GG-SCHED-0005` → GG-SCHED-0006 (all five draft);
[truffle-design.md](../../arch/truffle-design.md) §4.5, §14, §14.1
(amendments A7, A8, A9); [traceback-normalization.md](../../arch/traceback-normalization.md);
[RECOVERY.md](../../arch/RECOVERY.md) decision log; `tools/req` lint rule (e).
**Evidence:** truffle-design H15 (the per-call `ArrayDeque` of `DeferChain`),
decision T7, §14.1 trade-offs; gc has open-coded defers since Go 1.14.

## Context

truffle-design §14 proposed three SPEC amendments that the new node package
would need (A7 safepoint preemption, A8 open-coded defers, A9 closures over
typed cells). Until the owner decided, the m1a-nodes lane followed SPEC for
all three. `GG-0019` required "defer SHALL be an explicit per-goroutine
chain" and SPEC §3.5 said open-coded defers are "a later phase, not v1".

## Decisions

1. **A8 accepted (`GG-0019` → `GG-0067`, re-issued as GG-0070 + GG-0071, decision 4).** Defers are open-coded for the
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
     `Goexit` (wrong, and corrected by GG-0071: each frame runs its own
     pending defers; the chain holds only chained frames' records);
   - `recover`, `Goexit`, panics in deferred calls and `runtime.Caller` /
     `Callers` frames must match gc on both paths.
   `GG-0067` kept every obligation of `GG-0019` (panic is an
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
   `verifies:` annotation of `PanicCorpusTest` was re-pointed to `GG-0067`
   and `GG-0067` activated on it; `PanicCorpusTest` is a v1 test, so the
   re-issue removed the annotation. The both-paths obligation has no test
   yet — a follow-up that lands with TR-3.
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

4. **Re-issue of the 2026-11-18 requirements (owner decision on the
   independent review).** The review found the four requirement texts
   locked that day (`GG-0065`, `GG-0066`, `GG-0067`, `GG-SCHED-0005`)
   imprecise or wrong in ways a lock should not carry, and found that the
   owner had confirmed none of them. Each is superseded by a draft with the
   review's corrected wording (checked against the go1.27.1 sources:
   `runtime/runtime1.go` `parsegodebug`, `runtime/proc.go`'s removed-knob
   fatal, `cmd/go/internal/modload` `CheckGodebug`, `cmd/compile`'s
   `maxOpenDefers` and returns × defers bound, and a gc probe of caller
   frames and tracebacks):

   | Old (superseded) | New (draft) | What changed |
   |---|---|---|
   | `GG-0065` | GG-0068 | bare fields and unparsable values ignored like unknown keys, at startup and on later updates; startup failure only for an unsupported knob's *effective* value from any source (environment, `//go:debug`, `go.mod`, go-version default); gc's own fatals reproduced with gc's message and status |
   | `GG-0066` | GG-0069 | "unmodified"; graalgo-tool refuses to run when built by another release (today only a test fails); the existing `-goversion`/`-V=full` refusals stated |
   | `GG-SCHED-0005` | GG-SCHED-0006 | a downcall pins its carrier, so the carrier is off-pool or in the +k budget (GG-0057); semaphore and poller parks; re-entrant `ioEnter`, `ioExit` without `ioEnter` and exit inside the bracket fail fast; verification on real pinned downcalls and the role-table gate instead of NIO-era Spike 2b F5 and a manual audit |
   | `GG-0067` | GG-0070 (semantics) | observable contract only, plus `panicnil`; caller frames and tracebacks compared as defined in [traceback-normalization.md](../../arch/traceback-normalization.md) |
   | `GG-0067` | GG-0071 (mechanism, parent GG-0070) | open-coded iff all defers are outside loops and range-over-func bodies and number ≤ *L* (fixed, pinned by a boundary test); per-frame unwinding under one panic state; no `G` access or allocation on the non-panic path; boundary (*L*, *L*+1) tests asserting the path taken |

   *L* may differ from gc's 8: the probe shows the choice unobservable
   (identical frames at normal exit, `runtime.gopanic` inserted on the panic
   path on both). GG-0063 (owner-locked through ADR-0009) is not re-issued:
   its only defect is the citation of `GG-SCHED-0004`, which the registry's
   `superseded_by` chain resolves to GG-SCHED-0006; the owner may re-issue
   it with the citation updated when signing off GG-SCHED-0006.
   `GG-0013`'s "vendored stdlib" wording is cosmetic and not re-issued
   (that would demote an active, tested requirement to a draft); it is
   listed for the owner.

   **Sign-off state:** GG-0068, GG-0069, GG-0070, GG-0071 and GG-SCHED-0006
   are `draft`; each history ends "drafted from Fable review; awaiting owner
   sign-off". The owner locks them with `req lock` after reading, which
   records their text sums (below). SPEC marks their wording `[PROPOSAL]`.
   **Locked-text integrity:** nothing stopped an in-place edit of a locked
   text; `req lint` rule (e) now fails on one (sha256 per requirement in
   [LOCKED-TEXT.sum](../../../requirements/LOCKED-TEXT.sum), written by
   `req lock`, baselined once for the 78 earlier non-draft records by
   `req seal`).

## Consequences

- `GG-0019` is superseded by `GG-0067`, and `GG-0067` by GG-0070 +
  GG-0071; `req supersede` re-pointed the live
  references (SPEC, RECOVERY, truffle-design, Java comments, the panic
  corpus README, `PanicCorpusTest`'s annotation). Historical records (phase
  plans and checkpoints, the M5 review snapshot, the spec extract, two
  README status notes dated 2026-10-04) quote `GG-0019` in code font so they
  keep their meaning. `req supersede` mis-computes line numbers in Markdown
  files with fenced code blocks (truffle-design.md was edited by hand where it
  missed).
- truffle-design §14/§14.1 and the RECOVERY decision log carry the three
  statuses and the re-issue; TR-3 builds open-coded defers with the chain
  fallback against the drafts GG-0070/GG-0071, TR-6 remains conditional on
  P-B, closure nodes follow SPEC.
- Code follow-ups of the re-issue (none is done by this ADR): graalgo-tool's
  refuse-to-run check (`runtime.Version()` vs `release.GoVersion`) and a
  `toolchain` line in the toolexec module's `go.mod` (GG-0069); the GODEBUG
  validator's effective-value rule across sources and the inverted v1 tests
  (WP-GODEBUG, GG-0068); the may-block bit in R15's table-vs-source gate and
  the CapExhaustion falsifier on real pinned downcalls (WP-PIN,
  GG-SCHED-0006); the harness normalizer rules T4–T6 and caller-frame rules
  C1–C4 of traceback-normalization.md, the *L*/*L*+1 boundary tests and the
  path-introspection hook (TR-3, GG-0070/GG-0071).
