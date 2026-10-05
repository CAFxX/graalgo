# P2-M9 independent methodology review — reviewer2

**Milestone:** P2-M9 lost-wakeup model checking + JVM race stress (done-gate 4, GG-WAKE-0002)
**Snapshot reviewed:** commit `6d3d9e3` ("P2-M9: wakeup model explorer + race stress + JFR smoke robustness"), plus working-tree state at review time (tree was clean for phase2 at `6d3d9e3`)
**Reviewer role:** independent methodology reviewer (not the implementer). Adversarial brief: verify quantitative claims by re-running, probe the (a)–(f) coverage mapping, the abstraction gap, red-green evidence, stress-test strength, and escape-protocol compliance.
**Verdict: GO-WITH-FINDINGS** — the core methodology is sound and I independently reproduced the headline numbers, but sign-off must wait for the P1 remediations below. No P0: nothing I found invalidates the central result.

## Method (what I did independently)

1. Re-ran all 15 `WakeupModelTest` methods on JDK 25 (`~/tools/graalvm-jdk-25`, the build JDK): 15/15 pass; every §5 state/terminal count reproduced **exactly** (table in §A).
2. Re-ran all 4 `WakeupRaceStressTest` methods: 4/4 pass with exact accounting (herd 4s, timer 0s, tiny-quantum 1s, receiver-herd 1s on a loaded box).
3. Ran the redesigned `JfrEventsTest.testYieldThroughputSmoke` 5×: **2/5 pass** (ratios 0.996, 0.648✗, 1.176, 0.742✗, 0.748✗).
4. Read `Drainer`/`SchedulerCore`/`GoroutineHandle`/`ResumeToken`/`GoChan` (close path)/`SelectOp` (claim protocol) to check every coverage-mapping claim against the actual mechanism.
5. Checked the visited-set signature for completeness, the `processOneForTest` ≡ drain-loop-step claim, the JPF/TLA+ rejection rationale, and the checkpoint/commit trail for the two test-bug fixes.

## Findings

### P1-1 — Interleaving (c) coverage is hand-wavy; the "never duplicated" half has no script

REPORT §3 maps "(c) select in-flight stale SelSignal vs re-registration" to `unpark-consumed-once` ("stale unpark consumed exactly once, never duplicated"). Two problems:

1. **The specified mechanism does not exist in the implementation.** There is no `SelSignal` anywhere in the codebase. Select uses a shared CAS claim (`SelectOp.claim`: `UNCLAIMED` → winner/`SELF_CLAIM`); every UNPARK for a select waiter implies a won claim, so at most one UNPARK per select-park, and cross-operation staleness is just handle-id matching in the drainer. The report never says this — it presents the script as direct coverage of a mechanism that isn't there.
2. **The "never duplicated" property is untested.** I enumerated all 12 scripts: none offers two UNPARKs for the same parked handle. `unparkPending` is a `Set<Long>` so dedup is structural, but the explorer never exercises it (the closest, `exit-pending-unpark`, is unpark-for-*exited*, a different shape).

**Remediation:** (a) add `testModelDuplicateUnparkOnce` — program `[PARK, EXIT]`, driver `[u(0), u(0)]`, expect DONE; I traced all interleavings by hand and every terminal is DONE with the duplicate retained in `unparkPending`, never double-admitted; (b) add the pure-(b) complement `testModelStaleUnparkAfterRearm` — program `[PARK, PARK, EXIT]`, driver `[u(0), u(0)]`, expect DONE (wake → re-arm → stale unpark consumed by the fresh park); (c) rewrite the §3 row (c) to document the analogy explicitly: no SelSignal in the implementation (cite `SelectOp` claim CAS), drainer-level property = stale-UNPARK exactly-once consumption, decision logic out of scope per §7.

### P1-2 — The JFR smoke redesign is still flaky (2/5 in my runs), never fault-injected, and the checkpoint trail is stale

`testYieldThroughputSmoke` was redesigned (10x-work → paired interleaved D/E legs) to fix flakiness. Status:

1. **Still flaky.** My 5 independent runs: ratios 0.996 (pass), 0.648 (FAIL), 1.176 (pass — enabled *faster* than disabled, pure noise), 0.742 (FAIL), 0.748 (FAIL). The disabled-leg mean swings 21.9k–39.4k/s run to run; the D/E interleave does not cancel bursty load on this shared box. The old design failed ~40% "on a loaded box" per the code comment; the new one failed 60% in my sample.
2. **Never verified to catch regressions.** The comment claims "a true JFR hot-path regression (>=20% slowdown) still trips it" — no fault-injection evidence exists anywhere (no hidden_files/jfr experiment, no checkpoint entry). Worse, the arithmetic is wrong: ratio = enabled/disabled throughput; the 0.80 gate trips only when slowdown > 25% (1/1.25 = 0.80), not ≥20%.
3. **Stale checkpoint.** CHECKPOINT.md's last P2-M9 entry ends with "Verifying stability 5/5 now" — for the *superseded 10x design*. The 10x failure and the interleaved redesign are recorded only in a code comment in `JfrEventsTest.java`, not in the milestone log. A reader of the record believes the 10x fix is under verification.

**Remediation:** (a) decide the test's disposition *before* sign-off — either robustify (more legs + outlier rejection, or median-of-3 runs, or move to a quiet-hardware nightly gate) or quarantine it out of `make test`; a suite that fails 3/5 on a loaded box cannot satisfy "full suite green"; **sign-off is blocked until the suite is deterministically green**; (b) append a checkpoint entry recording the 10x failure → interleaved redesign history and the new design's stability evidence (whatever N consecutive passes are measured); (c) fix the comment arithmetic (≥25%, or re-tune the gate); (d) demonstrate regression-catching once via fault injection (e.g., temporary sleep in the JFR commit path) and record it — the user's "tests must be demonstrably capable of failing" pin applies.

### P1-3 — Escape protocol incomplete for both test bugs: instances fixed, class not hunted, process not patched

- **Finding 1** (CountDownLatch barrier deadlock): instance fixed with `herdBarrier` + explanatory comment. My own class hunt found the remaining instances benign (SchedulerCoreTest latches are awaited on the *test* thread, which holds no slot; `Thread.sleep(200)` inside one goroutine at cap=2; TortureBattery sleeps inside the sanctioned `ioEnter`/`ioExit` path) — but that hunt is *mine*, not the implementer's, and appears nowhere in the record. There is no durable rule: PLAN.md P2-D4 bans JVM-blocking in the adaptation layer, but nothing covers *test* code, and `make check` has no gate for it.
- **Finding 2** (flaky timing test): instance "fixed" via redesign (see P1-2 — the fix didn't hold). No documented rule for timing-gate design (e.g., "interleaved A/B legs, stability evidence required") and no hunt for sibling flaky timing tests.

**Remediation:** (a) add a test-authoring rule — "no JVM-blocking (latch.await, LockSupport.park, queue.take, Thread.sleep) inside `sched.spawn` bodies except via `ioEnter`/scheduler-aware primitives" — to PLAN.md's worker checklist or a TESTING.md, plus a `make check` grep gate over `src/test` (cheap, same pattern as the existing gates); (b) record the class-hunt results for Finding 1 in the checkpoint; (c) adopt the timing-test standard (interleaved legs + N-pass stability evidence in the checkpoint) as a documented rule.

### P1-4 — "Race-detector-equivalent": no JVM race tool was run and no equivalence argument is written

Done-gate 4 says "race-detector-equivalent stress (JVM race tools on the runtime, not the guest)". REPORT §6 delivers four excellent stress tests but never mentions a race tool and never argues the equivalence. For *this* design the equivalence argument is strong (single-threaded drainer ⇒ all lane/inflight/parked mutation under one lock; vthread side is atomic generations + one MPSC inbox; the residual data-race surface is tiny), but it is not written down — the gate's letter ("JVM race tools") is silently reinterpreted.

**Remediation:** add an explicit §6 note: name the equivalence argument (or the deviation), enumerate the residual shared-state surface (drainer lock, `parked`/`unparkPending`/`inIo`, `acked`/`parkSeq`/`armed` atomics, inbox), and state why a generic JVM race detector adds nothing here — or run one and report the result.

### P2-1 — Row (a): "while receivers drain" is not modeled; say what threeway actually covers

`threeway` (3 parked × 3 unparks, all orders) genuinely covers the *drainer-side* token matching: I verified `GoChan.close()` wakes parked senders via `sched.unpark` (same UNPARK token; `panicOnWake` set before unpark under the channel monitor), so the drainer indeed "does not distinguish abort unparks". But no receivers exist in the script, and the abort-*decision* logic (who gets unparked, claim/poison paths) is the §7-excluded decision logic. **Remediation:** reword the row to "drainer-side matching for close()-issued UNPARKs (close() verified to route via `sched.unpark`; abort-decision logic covered by ChannelTest/ChannelDifferentialTest)".

### P2-2 — Rows (b)/(f): "generation skew" and "drainer batching" don't exist; the mapping is looser than stated

UNPARK tokens carry no generation (`-1`; matching is by handle id), so "generation skew" is vacuous at the drainer — the real generation discipline lives in `armPark`/`acked` (real code, exercised) and `awaitAck` (abstracted). The drainer processes one token per `take()` — there is no batching. The underlying orders *are* exhaustively explored, so the coverage is real but the description oversells. **Remediation:** reword (b) to "UNPARK↔PARKED both-orders incl. stashed vs direct paths (UNPARK is generation-free; skew handled by armPark/acked)"; reword (f) to "UNPARK-before-PARKED (the IDLE case reduces to offer order; directed `testUnparkBeforeParkFastPath`)".

### P2-3 — §7 omits three residuals that are trusted, not modeled

The gap section is admirably honest, but three items on the wakeup path are neither modeled nor disclosed: (1) the `awaitAck` *loop condition itself* (`while (acked.get() < t)`) — an off-by-one here is invisible to the explorer (currently covered only empirically by §6, which does run the real loop); (2) ack→unpark *ordering* inside `admit()` — the model has no real parking, so a reversed order would not be caught by the tool (covered by code review per the GG-WAKE-0001 falsifier); (3) the JFR hooks on the park path (`notePark`/`parkResumed` called from `parkCurrent`/`awaitAck`) — observability-only, but unmodeled interactions on the wakeup path. **Remediation:** extend §7 with these three + where each is actually covered.

### P2-4 — Wrong requirement ID in `WakeupModelTest` javadoc; missing `verifies:` annotations

The class javadoc says "(done-gate 4; the GG-0044 lost-wakeup class)" — GG-0044 is *patch-stack discipline*, unrelated. It should be GG-WAKE-0002. Per PLAN.md's activation policy, the tests landing here should also carry `verifies: GG-WAKE-0002` annotations (the mechanism by which the ID goes →active). **Remediation:** fix the ID; add `verifies:` annotations.

### P2-5 — §4 overstates what the explorer proves about spurious admissions

"The explorer proves no admission occurs without an UNPARK or IO_EXIT behind it." What the explorer proves is *no lost wakeup* (every offered UNPARK → admission; every PARKED-with-offered-UNPARK → admitted). The no-spurious-admission direction rests on the two directed tests' provenance-counter assertions on two specific paths plus code structure (WAKE tokens are only constructed in `route()` from UNPARK/PARKED-stashed/IO_EXIT) — not on the explorer generally. (INIT/RESUME admissions legitimately occur without UNPARK.) **Remediation:** reword to match what is actually shown.

### P2-6 — §2 property statement needs the retention case

"every UNPARK is either matched to a parked PARKED or stashed and consumed by exactly one later PARKED" — but `exit-pending-unpark` shows the third case: retained in `unparkPending` with no later PARKED (bounded because handle ids are unique, never reused — verified: `ID_GEN` monotonic in `GoroutineHandle`). **Remediation:** "…or retained (bounded)".

### P3-1 — Visited-set signature omits `armed` without comment

`signature()` includes pcs, wait states, generations, acked, parkSeq, inbox, and the full drainer snapshot, but not `h.armed`. This is *safe* — I verified `armed` is written only by `armPark` and read nowhere in production (write-only dead state, as §7 says) — but a reviewer auditing soundness will trip over it. One-line note in the Soundness notes.

## What I verified as sound (no finding)

1. **Tool spike (task Q1).** The bespoke choice is the strongest option, not a cop-out. TLA+ rejection is correct (a green TLC run would verify a hand encoding, not the shipped `Drainer`). JPF rejection without attempting it is justified: JPF executes bytecode in its own VM with a hand-written JDK model that does not cover virtual threads (vthreads bottom out in `jdk.internal.vm.Continuation`/carrier mechanics JPF doesn't model), so it *cannot* run this code on JDK 25 — and even a working JPF would explore nothing beyond token order, because the drainer is single-threaded and all cross-thread state is atomic generations + one MPSC inbox. Attempting it would have been yak-shaving for zero marginal coverage. No JPF attempt exists in the tree, consistent with a paper rejection.
2. **`processOneForTest` ≡ production drain step.** `drainLoop` does `synchronized (lock) { route(token); admit(); checkInvariants(); }`; the hook does exactly the same. The "offer landing mid-iteration is observationally equivalent" soundness note holds: `admit()` never reads the inbox, and both inbox implementations are FIFO, so every production interleaving maps to an explored offer/DRAIN order.
3. **Vthread side fidelity.** The model calls the *real* `armPark` (not a reimplementation); generations, `waitGen`, and the acked≥gen settle rule match production. The LockSupport-permit proof sketch in §7 is correct (stale permits are absorbed by the `awaitAck` re-check loop; I traced the one-permit-per-generation accounting).
4. **Red-green guard (task Q4, model half).** `testModelDetectsWithheldUnpark` genuinely exercises failure: the explorer raises LOST WAKEUP on the withheld script and the test asserts on that string. The explorer is not vacuous.
5. **Stress-test strength (task Q5).** The four tests are well-designed *for the lost-wakeup class*: any lost UNPARK/IO_EXIT → goroutine parked forever → `runnableCount` never reaches 0 → 60s quiescence timeout → loud failure; any drainer invariant violation → `Runtime.halt` → loud. Counts are asserted exactly (sends, recvs, sleeps, side-traffic, rounds). I could not construct a lost-wakeup manifestation on these paths that escapes as a silent pass — spurious wakeups can't double-count because counters increment after the op completes. Gaps (not findings at P1): no cap=1 stress leg (the model covers cap=1), no select in the stress (acknowledged in the code; ChannelTest covers the claim protocol).
6. **Finding-1 arithmetic.** "2 admitted goroutines … 62 never admitted" = 64 − 2. ✓. The `herdBarrier` fix (spin + `yieldCurrent`) is correct: every spinning goroutine yields its slot, `arrived` is monotonic, no deadlock.

## Appendix A — quantitative verification (independent re-runs)

Explorer (`WakeupModelTest`, JDK 25, 15/15 pass); REPORT §5 claimed → observed:

| Script | Claimed states/terminals | Observed |
|---|---|---|
| basic | 22 / 2 | 22 / 2 ✓ |
| cross | 528 / 6 | 528 / 6 ✓ |
| threeway | 13,208 / 8 | 13,208 / 8 ✓ |
| quantum | 7 / 1 | 7 / 1 ✓ |
| io | 9 / 1 | 9 / 1 ✓ |
| unpark-consumed-once | 22 / 2 | 22 / 2 ✓ |
| cap1-handoff | 228 / 3 | 228 / 3 ✓ |
| exit-pending-unpark | 17 / 1 | 17 / 1 ✓ |
| unpark-during-quantum | 17 / 1 | 17 / 1 ✓ |
| mixed-io-park | 339 / 4 | 339 / 4 ✓ |
| unpark-io-park | 36 / 2 | 36 / 2 ✓ |
| init-under-wake-flood | 3,021 / 3 | 3,021 / 3 ✓ |

Directed: `testUnparkBeforeParkFastPath` (stashed=1/direct=0) ✓, `testParkBeforeUnparkDirectPath` (direct=1/stashed=0) ✓, `testModelDetectsWithheldUnpark` (fails-as-required) ✓.

Stress (`WakeupRaceStressTest`): 4/4 pass — thundering-herd 4s, timer-storm ~0s, tiny-quantum 1s, receiver-herd 1s.

JFR smoke (`testYieldThroughputSmoke`, 5 runs): ratios 0.996 / **0.648** / 1.176 / **0.742** / **0.748** → 2/5 pass (bold = gate failure at 0.80).

## Appendix B — answers to the six review questions

1. **Tool spike:** sound; JPF rejection without attempt justified (cannot execute JDK 25 vthreads; single-threaded drainer makes full-JVM exploration redundant).
2. **(a)–(f) mapping:** (d) and (e) genuine; (a), (b), (f) genuine at the drainer level but described more grandly than modeled (P2-1, P2-2); (c) hand-wavy — specified mechanism absent from the implementation, analogy undocumented, duplicate-UNPARK shape untested (P1-1).
3. **Abstraction gap:** honest in the main, three residuals undisclosed (P2-3).
4. **Red-green:** model guard verified working; JFR redesign neither stability-verified (2/5 in my runs) nor regression-catch-verified, checkpoint stale, ≥20% arithmetic wrong (P1-2).
5. **Stress strength:** strong for lost-wakeup-as-hang with exact assertions; "race-detector-equivalent" needs the written equivalence argument (P1-4); no cap=1 leg, no select (minor gaps).
6. **Escape protocol:** instances fixed; class hunts and process patches missing for both findings (P1-3).
