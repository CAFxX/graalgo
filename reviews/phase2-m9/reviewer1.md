# P2-M9 code review — reviewer1 (correctness)

**Verdict: NO-GO** — narrowly scoped. The model checker itself (deliverables 1–3, 5)
is sound and accepted; the milestone cannot sign off because the JFR gate
(deliverable 4) is broken: the committed redesign still flakes and turned
`make test` red during this review, and the working-tree replacement
(`JfrParkCostTest`) flakes too (3/7 runs on this VM). The rework is small
and specified exactly in P0-1.

**Review basis:** commit `6d3d9e3` ("P2-M9: wakeup model explorer + race stress +
JFR smoke robustness"), plus working-tree changes observed mid-review
(`JfrParkCostTest.java` new, `JfrEventsTest.java`/`SchedTestRunner.java`/
`WakeupModelTest.java` modified — an in-progress remediation, reviewed as well).

**Method:** read all five deliverables plus `SchedulerCore.java`,
`GoroutineHandle.java`, `ResumeToken.java`, `JfrEvents.java`; re-derived the
soundness arguments from scratch rather than trusting the javadoc; enumerated
all mutable state for the visited-set merge proof; wrote throwaway probes
(driving the real `Drainer` via `processOneForTest`) for double-unpark,
stashed-then-exit, and stale-unpark-during-RESUME; ran the full `make test`
twice (190/1 then 190/0), the M9 classes standalone (24/0), the smoke test 9×,
the new `JfrParkCostTest` 7×, and a direct hook-cost microbenchmark.

## What is accepted (no findings)

1. **Explorer soundness — holds.** (a) DRAIN = one token's route+admit+check,
   atomic: in production an offer landing between the drainer's `take()` and
   the synchronized block sits in the inbox, invisible to `admit()` (which
   reads only the lanes), so it is observationally equivalent to
   offer-before-DRAIN or offer-after-DRAIN — both generated. (b) The
   WAIT→RUNNING settling rule (`WakeupModelTest.java:163`) is sound: production
   `awaitAck` cannot act before observing `acked>=t`, and delayed observation
   is covered because the explorer can schedule other actions before the
   goroutine's next offer. (c) The visited-set signature (`:229`) is
   **complete**: I enumerated every mutable field reachable from `Exec`
   (pcs, st, waitGen, driverPc, inbox order+contents, ackeds, parkSeqs, and
   the drainer snapshot: inflight, admissions, max, parked, pending, inIo,
   all three lanes in order, provenance counters). `armed` is a pure function
   of parkSeq history (write-only, never read — verified by grep); JFR/`inIo`/
   `wakeAction`/timer fields are never touched by the model. Same signature ⇒
   same future: merges are sound. Missed merges (P1-2) cost states, never
   correctness.
2. **Protocol paths asked about:** EXIT-while-parked is impossible (EXIT
   requires running past `awaitAck`, which requires the PARKED fully routed
   and its wake admitted — the parked entry is always gone by then);
   double-PARKED-without-admission is impossible (second park requires
   observing the first ack); UNPARK-for-RESUME-parked is covered
   (`testModelUnparkDuringQuantum`) and verified by probe; IO_ENTER/IO_EXIT vs
   the parked map is fail-fast and unreachable in both production and model.
3. **`snapshotForTest` (`Drainer.java:271`):** read-only (synchronized read,
   string build, no mutation), no behavior change, zero production callers
   (verified by grep); `idNames` normalization is a per-`Exec` bijection —
   sound.
4. **Directed tests + red-green guard** all pass; the guard fails closed
   (state-explosion also fails it). The explorer is itself a mutation guard
   for the PARKED/UNPARK matching logic (removing the stash branch fails
   `basic` with LOST WAKEUP on the unpark-first order).
5. **`WakeupRaceStressTest`:** the scheduler-aware barrier fix is correct
   (JVM-blocking barriers would deadlock admission — the CHECKPOINT's Finding
   1); parked goroutines release slots so cap=2/4 cannot deadlock; 60 s
   timeouts are generous (measured legs run in seconds); counts are exact.

## P0 (correctness-blocking)

### P0-1. The JFR gate is broken — committed redesign still flakes, suite went red; working-tree replacement flakes too
`JfrEventsTest.java:442` (committed).

**Committed redesign:** full `make test` on `6d3d9e3`: **190 tests, 1 failure**
— `testYieldThroughputSmoke`, ratio=0.765 < 0.80. (Confound note: my own
concurrent probing added load during that suite run, so 0.765 is not a clean
sample — but the implementer's own pre-review data, 0.50–1.29 with ~40% flake,
and my quiet-box margins as thin as 0.022, stand independently.) My isolated
runs: 0.638 (FAIL, during suite load), then 1.21 / 1.29 / 0.867 / 0.882 /
0.908 / 0.822 / 1.146 (quiet box).
The implementer's own CHECKPOINT acknowledges the old ratios swung 0.50–1.29
("pure noise, not signal"); the "10× work per leg" fix targets within-leg
burst noise, but the dominant noise is **between-leg load variation** on this
shared VM, which interleaving cannot cancel. Three independent defects:
(a) the gate cannot detect what it claims — I measured the enabled hook path
at ~467 ns/park vs ~18 µs/yield (~2.6% of yield cost), so tripping 0.80 needs
a **~10× blowup** of the JFR path; the comment "a true JFR hot-path regression
(>=20% slowdown) still trips it" conflates end-to-end slowdown with JFR-path
slowdown and is false; (b) the quiet-box ratio sits at ~0.82–0.91, never ~1.0
— consistent with JFR **infrastructure** (background threads/chunk management)
CPU contention scaling with base cost, i.e. the test measures the wrong
quantity; (c) D-always-first pair order leaves monotonic drift uncancelled.

**Working-tree replacement** (`JfrParkCostTest.java`, new): right idea (direct
hook-cost measurement, the correct instrument), but its **absolute budgets
flake on this VM: 3 failures in 7 runs** — 2× the 500 ns absolute budget
(measured enabled 579 ns, 616 ns) and 1× the 400 ns delta budget (418 ns).
This box's clock runs ~5× slow (`nanoTime`≈160 ns; the enabled path is
dominated by 2× `nanoTime`), so a budget calibrated as "~8× the measured ~60 ns"
elsewhere is ~1.1–1.4× here. Two more defects: the "Fault-injection verified
(see CHECKPOINT)" claim has **no CHECKPOINT entry** — unverifiable; and "the
1-in-1024 emit path is covered separately by the trickle test" is imprecise —
`testHotLoopParkFilter` covers filter *exactness*, not emit-path *cost*.

**Remediation:** (1) make both budgets machine-relative, following the
existing `testDisabledGuardCostNegligible` pattern (`guardCost < 2 * nanoCost`),
e.g. `enabledMed < 8 * nanoCost` and `(enabledMed - disabledMed) < 6 * nanoCost`;
(2) write the fault-injection evidence into CHECKPOINT or drop the claim;
(3) fix the "trickle test" wording. Re-run ≥10× on this VM before sign-off.

## P1 (should fix)

### P1-1. REPORT.md overclaims what the explorer proves
`hidden_files/modelcheck/REPORT.md:24` and `:53`.

- §2: "every UNPARK is either matched to a parked PARKED or stashed and
  consumed by exactly one later PARKED." False under double-UNPARK-before-PARKED:
  `unparkPending` is a `Set<Long>`, so two unparks collapse to one stash entry;
  the second is **absorbed, never consumed**. I verified empirically with a
  direct `Drainer` probe: UNPARK,UNPARK,PARKED → exactly one stashed admission;
  a second PARKED with no new UNPARK correctly waits. The mechanism is right
  (wakeups aren't counted; absorption is by design) but the universal is false.
  Fix: "stashed and consumed by **at most** one later PARKED".
- §4: "the explorer proves no admission occurs without an UNPARK or IO_EXIT
  behind it." The terminal expectations catch *spurious* admissions only where
  they'd flip a WAIT terminal (effectively just `unpark-consumed-once`); the
  other scripts' terminals are insensitive to extra admissions. The
  no-spurious-admission direction rests on the drainer's structure (admit only
  acks lane tokens; WAKE tokens originate only from UNPARK/IO_EXIT/stash), not
  on an explorer assertion. Fix: qualify the claim, or assert provenance
  counters at explorer terminals.

### P1-2. `snapshotForTest` emits map/set entries in non-canonical order → run-dependent signatures and report numbers
`Drainer.java:271` (`parked`/`unparkPending`/`inIo` iterate `HashMap`/`HashSet`
keyed by raw handle ids).

Not a soundness bug (different orders for the same logical state only cause
missed merges), but it makes the explorer non-deterministic in its state
counts: I measured cross 528→537, threeway 13208→13200, cap1-handoff 228→227,
init-under-wake-flood 3021→3023 across runs (ids differ per JVM via the global
`ID_GEN`). REPORT.md §5's exact-count table is therefore not reproducible.
Fix (cheap — the sets are tiny): sort entries by normalized name in
`snapshotForTest`; then re-record the §5 table.

## P2 (nits)

- **P2-1. Script-family gaps** (`WakeupModelTest.java`): all same-code-path, but
  cheap to close — (a) RESUME → PARK with a stale UNPARK stashed during the
  RESUME window (`unpark-during-quantum` ends at RESUME); (b) the
  UNPARK,UNPARK-before-PARKED stash-dedupe order (my probe verified it; the new
  working-tree `testDuplicateUnparkAdmittedOnce` covers the PARKED-first order
  only); (c) UNPARK while in-I/O → IO_EXIT → PARK (stale unpark surviving an
  I/O round trip). The working tree's two new directed tests already cover
  adjacent cases — good.
- **P2-2. Misleading comment** (`WakeupRaceStressTest.java:173`): "Burn quantum
  to force expiry mid-protocol" — the 500-iteration burn is ~0.5 µs, far below
  the 100 µs quantum; expiry actually comes from channel round-trip latency
  (~40 µs/round). The test is valid; the comment misstates the mechanism.
- **P2-3. `testModelInitUnderWakeFlood`** (`WakeupModelTest.java:463`): two
  unparks is not a "flood", and RESUME-vs-WAKE/INIT lane contention under cap
  is absent from the family. Fairness/starvation under *infinite* flood is
  out of model-checker scope anyway — document the boundary.
- **P2-4. Snapshot rendering asymmetry** (`Drainer.java:271`): `wake=` prints
  `kind:id:gen` but `init=`/`run=` print `id:gen`. Harmless (lanes are
  kind-homogeneous by construction) but a future lane-kind bug would be harder
  to spot in snapshots.
- **P2-5. Red-green guard is one-directional** (`WakeupModelTest.java`
  `testModelDetectsWithheldUnpark`): proves the explorer detects *lost*
  wakeups; no complementary guard proves it would flag a *spurious* admission
  (a mutant drainer test, or asserting `unpark-consumed-once`'s WAIT is
  violation-sensitive).
- **P2-6. `execute()` replays the full path per DFS node** (`:296`) — quadratic;
  fine at 13k states, note for future script growth.
- **P2-7. REPORT.md §7** omits `wakeAction` from the abstraction gap (runs after
  admission, cannot cause lost wakeup — completeness only).

## Empirical appendix (this VM, GraalVM 25.0.4.1.1)

- `make test` @ `6d3d9e3`: 190 tests, 1 failure (smoke, ratio 0.765). Re-run on
  quiet box: 190/0, smoke ratio 1.146.
- Smoke ratios observed: 0.638, 0.765, 1.21, 1.29, 0.867, 0.882, 0.908, 0.822,
  1.146 — noise-dominated; gate 0.80 meaningless.
- Direct hook cost (my probe): `nanoTime`=160 ns, disabled=227 ns/park,
  enabled=467 ns/park → enabled ≈ 2.6% of an ~18 µs yield; ~10× blowup needed
  to trip 0.80.
- `JfrParkCostTest` (working tree): 7 runs → 3 failures (enabled 616/579 ns >
  500 ns budget; delta 418 ns > 400 ns budget). Absolute budgets unfit for this VM.
- Drainer probes (all pass): double-unpark → exactly one admission, second
  park waits; stashed-unpark-then-EXIT → inflight exact, benign pending leak;
  stale-unpark-during-RESUME → consumed once by later park.
- Explorer state counts (my runs): basic 22, cross 537, threeway 13200,
  quantum 7, io 9, unpark-consumed-once 22, cap1-handoff 227,
  exit-pending-unpark 17, unpark-during-quantum 17, mixed-io-park 339,
  unpark-io-park 36, init-under-wake-flood 3023 — all "no lost wakeup";
  small deltas vs REPORT §5 are the P1-2 ordering artifact.
- New working-tree directed tests (`testDuplicateUnparkAdmittedOnce`,
  `testStaleUnparkAfterRearm`): pass.

## Bottom line for the parent

Sign-off is blocked **only** by P0-1 (the JFR gate). Everything else in M9 —
the explorer, the hook, the race stress, the report's core claims — survived
adversarial review. The fix for P0-1 is ~5 lines (machine-relative budgets in
`JfrParkCostTest`) plus two doc corrections; the remaining P1s are a sentence
each in REPORT.md and a sort in `snapshotForTest`. Recommend: fix P0-1 and
P1-1/P1-2, re-run the suite plus 10× `JfrParkCostTest` on this VM, then sign off.
