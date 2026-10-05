# P2-M3 Remediation Re-review — reviewer3

**Verdict: GO-WITH-FINDINGS** (no P0/P1; four P2s + two P3s, all docs/comments/record-accuracy — no correctness issues).

**Snapshot:** `bce75f9`, branch `master`, `~/workspace/graalgo`. Remediation diff
`b3a7546` → `bce75f9` (two commits: `6b327f2` reviewer1 P0s, `bce75f9` reviewer2
P1-2/P2s). Working tree clean for all scoped paths at review time
(`git status --short -- phases/phase2/src phases/phase2/Makefile phases/phase2/corpus`
empty); the live tree *is* the snapshot. No files modified by this review.

**Method:** adversarial line-by-line review of the 667-line diff; exhaustive
lock-order trace (every `synchronized` site in `TimerHeap`/`GoChan`/`GoTimer`/
`SelectOp`/`SchedulerCore`); empirical red-green in scratch copies
(`/tmp/rg-old` = `b3a7546` + new tests, `/tmp/rg-new` = `bce75f9`; repo untouched):
(1) new drain tests ×3 runs on old code, ×2 on new; (2) no-op-`ioExit` mutation
vs `testIoExitReacquiresSlot`; (3) no-drain mutation vs the 7 claimed tests;
(4) full suite on the snapshot: **101/101 green** (independently reproduced);
(5) tm01/tm13/tm14 `.go` re-run 3× each under gc 1.27.1 — `.expected`
byte-identical every time; (6) gc 1.27.1 primary source (`runtime/time.go`
`stop`/`modify`, `runtime/chan.go` `timerchandrain`) as the drain oracle.

---

## Verification of the 9 assigned items

1. **P0-1 / monitor protocol — FIXED, verified exhaustive.** Every
   `trySend`/`tryRecv` call site in main sources is now monitor-held:
   `fireChanLocked` (`synchronized (e.chan)`, TimerHeap.java:371-373),
   `drainLocked` (`synchronized (ch)`, :267-278), `SelectOp.tryCompleteReady`/
   `completeUnderLock` (`synchronized (ch)` — pre-existing), test `drain()`
   (P1-5 fixed). Lock order is heap → owner → chan **everywhere, with no
   reverse path**: `GoChan.send`/`recv` enqueue under the monitor but park
   *outside* it (`sched.parkCurrent` → drainer token, no heap lock);
   `SelectOp` never touches the heap; `sched.unpark` (called inside
   trySend/tryRecv) is a lock-free `drainer.offer`; `Timers.sleep/startTimer/
   stopTimer` take the heap lock with no channel monitor held. No lock cycle
   exists. `GoTimer.stillCurrent` is `synchronized`, so the fire path's check
   is heap → owner as documented.

2. **P0-2 / unconditional drain — FIXED.** `stopChan`/`resetChan` evaluate
   `boolean drained = drainLocked(owner)` unconditionally and return
   `wasActive || drained` (TimerHeap.java:226, :253). No short-circuit: the
   drain is evaluated into a local *before* the `||` (the checkpoint notes a
   `wasActive || drainLocked(...)` short-circuit bug was caught and fixed
   during remediation — confirmed absent in the final code). gc-source check:
   go1.27.1 `stop()` and `modify()` both call `timerchandrain`
   unconditionally with `pending=true` if anything drained — exactly this.

3. **P0-3 / atomic fire — FIXED, structurally stronger than gc.** Pop,
   staleness check, `trySend`, and re-arm all execute under the heap lock
   (`timerLoop` → `fireChanLocked`, :344-359); `stopChan`/`resetChan` need the
   same lock, so no stop/reset can interleave a stale send or corrupt the
   return value. `trySend` is provably non-blocking: deque/array ops only,
   `accept()` trivial, `deliverToReceiver` → lock-free `drainer.offer`,
   `SelectOp.setResult` unsynchronized — nothing parks, nothing takes
   heap/owner locks. All three interleavings now match gc observables
   (stop-before-pop → true/no delivery; stop-after-fire → true/drained;
   stop-after-re-arm → true/drained).

4. **P0-4 / atomic re-arm — FIXED (with a correction to the record, see
   P2-1).** Re-arm (`heap.add`/`byId.put`) is under the heap lock with the
   send; `stillCurrent` (heap → owner) gates it. **Correction:** reviewer1's
   P0-4 mechanism as written was impossible — the *old* code already held
   `synchronized (lock)` across the `stillCurrent` check *and* the re-insert
   (verified in the `b3a7546` blob), and `stopChan`/`resetChan` also take the
   heap lock first, so "resetChan interleaves between check and re-insert"
   could not occur; the generation/id guards additionally closed the
   pop→check window. The real stale-tick race was P0-3's *unlocked send*,
   which the remediation closes. The new code is correct regardless — this
   is a record-accuracy issue, not a code issue.

5. **P1-1 (reviewer2) = P0-2 — CONFIRMED FIXED** (same change, same evidence).

6. **P1-2 / flake — FIXED.** Margins are 300/600/900ms in tm01 `.go` ✓, in
   `TimerTest.testSleepOrder` ✓, and in `TimerDifferentialTest.testTm01SleepOrder`
   ✓ (consecutive separations ≥300ms in all three). The "jitter cannot reorder"
   comments are corrected with the empirical basis (README: "50ms sat inside
   the measured fire-lateness noise band and flaked — P1-2"; both class
   javadocs updated). `.expected` unchanged and re-derived byte-identical
   under gc 1.27.1 (3/3 runs). Residual: the margin (4.3× the ~69ms observed
   max) is empirical, not proven — honestly documented, acceptable.

7. **New tests — 2 genuine falsifiers, 2 pins, P2-7 sound.**
   - `testTickerStopDrainsBufferedTick`: **red→green verified** — FAILS 3/3
     on old code (`expected=0 actual=1`), PASSES 2/2 on new. True falsifier.
   - `testTickerResetDrainsStaleTick`: **red→green verified** — FAILS 3/3 on
     old (stale tick, dt≈100µs < 40ms threshold), PASSES 2/2 on new. True
     falsifier. The 40ms threshold is sound (timers never fire early —
     `timerLoop` re-waits after spurious wakeups; fresh tick lands ~80ms).
   - `testTimerStopAfterFireDrains`: **green on old code too** — the one-shot
     expired-undrained case was already drained pre-fix. It is a regression
     pin (and a P0-1 monitor pin), *not* a red-green falsifier. The
     checkpoint's "3 P0-2 red-green" overstates: it is 2 falsifiers + 1 pin
     (see P2-2).
   - `testNoTickAfterStopStress`: **green 3/3 on old code** — not a reliable
     falsifier (consistent with the P2-1 correction: the pre-fix re-arm race
     it names could not fire; the real pre-fix stale path was P0-3's narrow
     unlocked-send window). Valid stress/regression pin on new code.
   - P2-7 pins sound: callback thread name asserted `"graalgo-timer"`
     (matches `TimerHeap.start()`), throwing-callback survival (matches
     `fireNonChan`'s `catch (Throwable)`), `testNetpollDeletion` asserts UOE
     + `GG-0018` on all five methods. The `make check` netpoller gate still
     passes (test references excluded by design).
   - Checkpoint's "no-drain mutation kills 7 tests" **verified**: no-drain
     mutation on new code kills the 3 new P0-2 tests +
     `testTimerStopUndrained` + `testTimerResetExpiredUndrained` + tm05 +
     tm08 (tm13/tm14 also die — 9 total; the claim is conservative, fine).

8. **P2-2 — FIXED, verified non-vacuous.** Under a no-op-`ioExit` mutation,
   `testIoExitReacquiresSlot` now **FAILS** ("second ioEnter/ioExit failed:
   re-entrant ioEnter"); with the real `ioExit` it **PASSES**. The white-box
   `inIo`-cleared assertion is also present. (Side observation: under the
   mutation the drainer thread's EXIT-while-inIo fail-fast fires as designed
   — P1-4 working.)

9. **New issues introduced by the remediation — none at P0/P1.** Checked:
   short-circuit (fixed, §2), grid-alignment formula
   (`deadline += period*(1+delay/period)` — matches gc `unlockAndRun`'s
   `t.when + t.period*(1+delay/t.period)`; skip-don't-burst preserved),
   deadlock in heap → owner → chan ordering (no cycle — exhaustive trace, §1),
   `stopChan`'s `id < 0 → false` early return without drain (safe: buffer is
   provably empty in that state — every path that sets id<0 drains first, and
   the timer thread cannot send without the heap lock; gc-outcome-equivalent),
   `resetChan` drains-before-re-arm under one critical section (no fire can
   interleave). Remaining nits are P2/P3 below.

---

## Findings

### P0 — none
### P1 — none

### P2
- **P2-1. Reviewer1's P0-4 mechanism was impossible as described — record
  correction.** The `b3a7546` re-arm block already held `synchronized (lock)`
  across the `stillCurrent` check and the `heap.add`/`byId.put` re-insert, and
  `stopChan`/`resetChan` take the heap lock first; the posited
  check→reset→re-insert interleaving could not occur (it contradicts
  reviewer1's own lock-order documentation). The genuine pre-fix stale-tick
  race was P0-3's unlocked `trySend`. The remediation (send under the heap
  lock) is correct and strictly stronger than needed — no code change
  required, but the milestone record should not cite P0-4's mechanism as a
  real race, and `testNoTickAfterStopStress`'s comment ("On the pre-fix code
  the ticker can re-arm after stop") overstates what the old code could do.
- **P2-2. Checkpoint overstates the P0-2 red-green: 2 falsifiers + 1 pin, not
  3 falsifiers.** `testTimerStopAfterFireDrains` passes on the pre-fix code
  (verified 3/3 runs) — the one-shot expired-undrained drain predates the
  fix. It remains a valuable regression + monitor-protocol pin, but the
  "~16:00 CHECKPOINT.md" entry "3 P0-2 red-green" should read "2 red-green
  falsifiers + 1 pin". (The "no-drain mutation kills 7" claim itself is
  accurate — verified.)
- **P2-3. `testNoTickAfterStopStress` is a stress pin, not a falsifier.**
  Passes 3/3 on pre-fix code; it cannot reliably catch the P0-3 stale-send
  race (nanosecond window, no blocking between pop and send). Keep it as a
  regression net, but do not cite it as red-green evidence for P0-3/P0-4.
- **P2-4. Reviewer1's P2-2 doc nit half-fixed.** The ticker re-arm now uses
  gc grid alignment ✓, but `TimerHeap`'s class javadoc still says the SLEEP
  waiter parks "via `SchedulerCore.parkCurrent` (sleep/startTimer)" —
  `startTimer` does not park. One-word doc fix.

### P3
- **P3-1.** `fireChanLocked`'s `e.owner == null` branch is now dead
  (`addChan` rejects null owners). Harmless defensive check; remove or keep
  with a comment.
- **P3-2.** The re-arm overflow guard (`deadlineNanos < 0 → Long.MAX_VALUE`)
  only catches negative wrap; a positive-but-wrong deadline from product
  overflow is uncaught. Requires a ~292-year stall — unreachable in
  practice; gc has no guard at all. Note only.

**Deferred items (unchanged, recorded in checkpoint):** P2-4 per-test timeout
(mutation-B suite hang), P2-9 `SchedulerCore` shutdown / thread accumulation.
Not re-litigated here; reviewer2 graded them P2.

---

## Verified correct (adversarial pass, no findings)

1. Unconditional drain matches gc 1.27.1 source (`stop`/`modify` →
   `timerchandrain`, drained ⇒ active) in all paths, including `resetChan`
   with `id < 0` (gc's `modify` also drains unconditionally).
2. Heap → owner → chan ordering consistent at every dual/triple-lock site;
   no chan → heap or chan → owner or owner → heap path exists
   (`GoChan.send/recv` park outside the monitor; `SelectOp` never touches
   the heap; `unpark` is a lock-free queue offer).
3. `trySend` provably non-blocking (no park, no nested locks).
4. `addChan` assigns the owner id under heap → owner locks before heap
   publication — the P1-1 (reviewer1) this-escape is closed; `GoTimer.id`/
   `generation` are only touched under the owner lock.
5. `tick(0)` → null; `Ticker.Reset(0)` panics with the gc message;
   `Timer.Reset(0)` allowed (P1-2/P1-3 reviewer1) — covered by new tests,
   green.
6. P1-4 EXIT/PARKED-while-inIo fail-fast tests present and precise
   (message-matched; inflight untouched by the failed EXIT).
7. `FreezeGapTest` covers the 4 signatures Phase-1's `FreezeSignatureTest`
   misses (reflection; does not trip the netpoller gate).
8. Netpoller-deletion gate extended with the `java.nio.channels|java.net`
   second disjunct (reviewer1 P2-3); `make check` fully clean.
9. Corpus: tm13/tm14 `.go` + `.expected` genuinely gc-produced (3/3
   byte-identical re-runs); README documents tm13/tm14, fixes the tm05
   bullet, records the P2-1 `len()` divergence with a Phase-3 resolution
   requirement; `GoTimer` javadoc carries the same divergence note.
10. Full suite 101/101 green, independently reproduced in a scratch build of
    the snapshot (repo untouched).

## Suggested follow-ups (non-blocking)

1. Fix the four P2s (record/comment/doc accuracy) — small, mechanical.
2. Consider a deterministic P0-3 regression test: the stale-send window is
   now closed structurally, but no test *names* it (the stress test does not
   reliably exercise it). A white-box test (e.g., stop-then-fire ordering via
   `processOneForTest`-style control of the timer loop) would pin the
   invariant; optional given the structural argument.
3. Re-run the full suite 3× before sign-off, per reviewer2's flake lesson
   (N2) — suite-green claims should carry run counts.

---
*Reviewer: reviewer3 (independent subagent). Read-only: no repo files
modified. Empirical work in /tmp/rg-old, /tmp/rg-new (scratch, not in repo).
gc oracle: ~/tools/go 1.27.1 sources.*
