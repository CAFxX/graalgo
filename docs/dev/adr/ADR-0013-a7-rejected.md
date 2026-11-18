# ADR-0013 — Amendment A7 (Truffle-safepoint preemption) rejected: prototype P-B failed; `Safepoint.poll()` stays

**Date:** 2026-11-18
**Status:** accepted 2026-11-18. The owner's rule in
[ADR-0012](ADR-0012-open-coded-defers.md) decision 2 was "switch only if
prototype P-B validates"; P-B did not validate, so A7 is rejected by that
rule. No requirement or SPEC text changes.
**Scope:** [truffle-design.md](../../arch/truffle-design.md) decision T4,
§2.7, §13.2 (P-B), §14, §14.1, work package TR-6;
[RECOVERY.md](../../arch/RECOVERY.md) decision log and work package
WP-POLLCOST; requirements `GG-SCHED-0001` and `GG-0017` (unchanged);
[SPEC.md](../../arch/SPEC.md) §3.5 (unchanged).
**Evidence:** the P-B prototype report and probes, on branch
`wt/pb/safepoint` (see "Where the experiment is").

## Context

truffle-design §14 proposed amendment A7: replace the explicit
`Safepoint.poll()` at loop back-edges and function entries with preemption by
Truffle safepoints — a monitor thread submits a `ThreadLocalAction` to a
goroutine whose quantum expired, and the action performs the quantum park.
The promise was a compiled loop with no scheduler code in it. ADR-0012
accepted A7 conditionally on prototype P-B (truffle-design §13.2: fairness
and torture suites with the monitor + action implementation behind a
switch). P-B was built and measured on this box (Oracle GraalVM 25.0.4.1.1,
go1.27.1 linux/amd64, medians under load).

## Decision

1. **A7 is rejected.** Goroutine preemption is not moved to Truffle
   safepoints. Decision T4 of truffle-design is rejected for goroutines on
   virtual threads; TR-6 as written (monitor + `ThreadLocalAction`, deleting
   `Safepoint.poll`) is not built.
2. **What stays.** `Safepoint.poll()` at loop back-edges and function entries,
   exactly as `GG-SCHED-0001`, `GG-0017` and SPEC §3.5 state it. No
   requirement is superseded, SPEC is not amended, and the new node package
   keeps emitting the poll in its TR-1 form (countdown in the context
   thread-local slot). The parts of §2.7 that never depended on T4 — parks
   through `TruffleSafepoint.setBlocked…` and the
   `TraceMissingSafepointPollInterval` missing-poll gate — are unaffected.
3. **The finding that motivated A7 stands and becomes its own backlog item.**
   The explicit poll is expensive on tight loops (see evidence), so making it
   cheaper is work package WP-POLLCOST in RECOVERY.md: prototype the flag
   fallback (a per-goroutine `preempt` flag set by the scheduler's
   preemption monitor, with the poll reduced to a load and a branch), or strip
   the per-back-edge thread-slot store and the volatile `exitPending` read
   from today's poll. Gate: `loop`, `slicesum` and `fib` no worse than today,
   and torture legs F1, F3, F4 and F6 green; estimate 3–6 agent-days.

## Evidence

Verdict of P-B: FAIL, on criteria set before measuring. Failing criteria:

- **Pinned parks (C7).** Parking a goroutine inside a `ThreadLocalAction`
  from compiled code is a park under a native/VM frame, so the virtual thread
  cannot unmount: JFR `jdk.VirtualThreadPinned` recorded **94 events in
  safepoint mode against 0 with the explicit poll** on the same one-second
  spinner (`GOMAXPROCS=1`), every one "Native or VM frame on stack" in
  `LockSupport.park` under `quantumPark`. Each preempted goroutine holds a
  carrier while waiting for re-admission, which GG-SCHED-0001 (a park that
  takes the virtual thread out of the pool) and the pin audit (GG-0057)
  forbid. No setting of this runtime removes the native frame.
- **Plain programs hang.** A program with 3,000 CPU-bound goroutines
  (20 ms each) finishes in 17.2 s with the explicit poll and never finishes
  in safepoint mode (killed at 90 s; one spare carrier does not help: all
  carriers parked in `quantumPark`). It is a deadlock, not a slowdown.
  Torture leg F1 with 10,000 spinners hung 2/2 (the monitor blocked on the
  polyglot context's lock held by a starting goroutine that needed a
  carrier); a latency probe with 8 spinners at `GOMAXPROCS=4` hung 2/2.
- **Truffle mount-hook recursion.** With a pending action, the pinned
  Truffle's virtual-thread mount hook recursed to `StackOverflowError`
  (3 of about 45 runs of F1 `cap2par8`); an upstream defect in experimental
  code, workable around only by warming a method handle.
- **Counted loops lose the safepoint.** Under the community Graal compiler
  configuration an `int32`-counted loop starved a sibling goroutine for
  1,807 ms and a constant-bound `int` loop for 1,050 ms (10 ms and 8 ms with
  the enterprise default); whether a Go loop is preemptible became the
  compiler's decision.
- **Unwind-and-resume does not work either.** Throwing from the action and
  parking in an ordinary frame does unpin the park (the 3,000-goroutine
  program then finishes in 12.6 s), but compiled code reports the root node,
  not the loop, as the safepoint location, so a compiled `for {}` was never
  preempted.

What passed (necessary, not sufficient): unit tests, the 35-program lifecycle
corpus, torture legs F3, F4, F6 and F5, deadlock detection and exit freeze.
The torture battery flatters safepoint mode because its spinners are
host-compiled lambdas whose actions do not go through the VM stub.

Measured benefit that A7 was after, still unclaimed (best round, median of
3, ms; gc from one `test/bench/run.sh` run):

| Benchmark | gc | explicit poll | safepoint mode |
|---|---|---|---|
| `loop` | 4.4 | 18.3 | 1.0 |
| `slicesum` | 0.94 | 10.7 | 6.8 |
| `fib` | 0.57 | 13.6 | 13.1 |

The poll is not a few cycles per back-edge: its store to the thread slot and
the volatile `exitPending` read keep Graal from treating the loop as counted.
That is the case for WP-POLLCOST, which does not need Truffle safepoints.

## Where the experiment is

The prototype is not merged. It lives on branch `wt/pb/safepoint`: the switch
(`-Dgraalgo.preempt=poll|safepoint`, default `poll`), the monitor and the
torture battery's context mode, the probes (`spin`, `gostorm`, `latency`,
`counted`) under test/spikes/pb-safepoint/ — those probe sources are on that
branch only and were not merged to master — and the report, docs/arch/pb-safepoint.md
(branch `wt/pb/safepoint` only, not merged to master). Read it with
`git show wt/pb/safepoint:docs/arch/pb-safepoint.md`; its §7 lists the
reproduction commands. The branch is the record: do not delete it without
moving the report and probes into [docs/arch/](../../arch/) first.

## Re-test triggers

Reopen A7 only if one of these happens, and then re-run P-B unchanged
(its criteria C1–C7 are in the report):

- a Truffle/Graal release whose thread-local actions are safe to park in on
  an unmounted-continuation virtual thread — in particular, a handshake slow
  path from compiled code that does not leave a native frame on the stack,
  or compiled-safepoint location fidelity (loop vs root node);
- a JEP or JDK change to virtual-thread pinning under native/VM frames, so
  that a park there can unmount;
- Truffle dropping the experimental status of virtual polyglot threads and
  fixing the mount-hook recursion, together with the counted-loop safepoint
  behaving the same under every compiler configuration.

## Consequences

- truffle-design: T4 and A7 marked REJECTED (§2.7 decision, §14 row, §14.1
  status and column), TR-6 replaced by WP-POLLCOST for the poll, P-B row
  records the result; the tradeoff column "if rejected" is now the plan.
- RECOVERY: decision-log row; new work package WP-POLLCOST in the table and
  the coverage matrix; GG-0017 and GG-SCHED-0001 rows point to WP-POLLCOST
  instead of "A7 conditional on P-B". WP-SCHED no longer waits on an A7
  decision.
- ADR-0012 decision 2 is resolved by this record (note added there).
- No code changed on master; SPEC, requirements, `LOCKED-TEXT.sum` untouched.
