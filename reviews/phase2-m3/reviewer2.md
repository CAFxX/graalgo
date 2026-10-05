# P2-M3 independent methodology/testing review (reviewer2)

**Verdict: GO-WITH-FINDINGS** — the milestone's testing methodology is sound (real red-green, genuine
gc corpus, faithful translations, independently verified gc semantics), but sign-off requires remediating
two P1s: a deterministic ticker Stop/Reset pending-tick divergence from gc (fix designed, implemented and
verified here — all 85 green, divergence gone), and a flaky-by-design 50ms-margin differential test whose
own "jitter cannot reorder" comment is empirically false (it failed once in three suite runs).

**Snapshot:** commit `b3a7546` ("P2-M3: startTimer callback runs on timer thread (gc parity)"), branch
master, `~/workspace/graalgo`. Reviewed read-only against `b3a7546` blobs. **Note:** the live tree moved
during this review — uncommitted modifications to `TimerHeap.java`, `GoTimer.java`, `Drainer.java`,
`TimerTest.java`, `IoSlotsTest.java`, `TimerDifferentialTest.java`, `SchedTestRunner.java`, `Makefile`,
plus untracked `corpus/timers/tm13_ticker_stop_buffered.{go,expected}` (the `tm13` name matches this
report's P1-1 exactly). This review covers `b3a7546` only; the parent should treat any remediation as
unverified-by-me unless re-reviewed.

**Scope reviewed:**
1. `phases/phase2/src/test/java/graalgo/runtime/TimerTest.java` (15 tests)
2. `phases/phase2/src/test/java/graalgo/sched/IoSlotsTest.java` (6 tests)
3. `phases/phase2/src/test/java/graalgo/sched/TimerDifferentialTest.java` (12 tests)
4. `phases/phase2/corpus/timers/` — tm01–tm12 `.go` + `.expected` + `regen.sh` + `README.md`
5. `phases/phase2/CHECKPOINT.md` P2-M3 entries (~14:30, ~15:30 CEST 2026-10-05) — every number/claim checked
6. `phases/phase2/PLAN.md` §4 P2-M3 acceptance criteria — each checked

## Method

**Suite runs.** Built and ran `make test` on the snapshot three times (GraalVM 25.0.4.1.1): runs were
85/85, **84/85 (testTm01SleepOrder failed: `fast,slow,mid` vs expected `fast,mid,slow`)**, 85/85.
Isolated tm01 rerun: 30/30 pass clean, 120/120 under `stress` — a rare load-dependent flake, not a
systematic failure. `make check`: all three grep gates clean.

**gc semantic probes** (independent, `~/tools/go/bin/go` go1.27.1 — output table below). All implementer
claims about `Stop`/`Reset`/`Ticker.Stop`/`NewTicker(0)` **confirmed correct** — no P0. I additionally
read go1.27.1 primary source (`src/runtime/time.go`: `stop()`/`modify()` both call `timerchandrain`
unconditionally; `src/runtime/chan.go`: `chanlen` on timer channels always returns 0, "present to users
as unbuffered"; the Go 1.23+ channel-timer model is *lazy*: the timer is only heaped while a goroutine
is blocked on the channel, and in-flight sends are suppressed via a `seq` bump in `stop`/`modify`).

**Red-green reproduction** (pristine copy `/tmp/m3rg/phase2`, P1 overridden to the real phase1): all four
checkpoint mutations reproduced — A killed `testSleepReleasesSlot` **+ `testSleepOrder` + `testTm01SleepOrder`**
(decoding the checkpoint's unexplained "(+2)"); B killed `testIoEnterReleasesSlot` (and the full suite
*hangs* in `testIoExitReacquiresSlot` — see P2-6); C killed tm05 + `testTimerStopUndrained`; D killed tm08 +
`testTimerResetExpiredUndrained`.

**Corpus genuineness.** All 12 `.go` programs re-ran 3× each with `go run`; all outputs byte-identical to
`.expected`. No `.expected` contains wall-clock values — all assert event order only. Translations checked
line-by-line against the Go originals; faithful (notes in P2-10).

**Timing probes.** Single 50ms timer fire lateness (end-to-end to a parked vthread): 0.26–69ms
load-dependent; control `Object.wait(50ms)` max 4.2ms; burst of 300× 200ms sleeps: p99 oversleep ~33ms.
The 50ms test margins sit inside the measured noise band.

**Edge probes** (custom drivers on the snapshot): T1 ticker Stop leaves 1 pending tick (gc: 0) —
**confirmed divergence**; T2 ticker Reset with pending tick delivered a stale tick immediately in 1/5 iters
(gc: never); T3 callback runs on `graalgo-timer` daemon ✓; T4 throwing callback doesn't kill timer thread ✓;
T5 all five `Netpoll` methods throw UOE with the GG-0018 message ✓; T6 `ChanOps.len()` on a timer channel
with a buffered tick returns 1 (gc: **always 0**).

**Fix verification.** I implemented the P1-1 remediation (unconditional drain in `stopChan`/`resetChan`,
gc `timerchandrain` parity) in a scratch copy: full suite **85/85 green**, T1 → 0 remaining, T2 → stale
always drained (5/5 iters). Diff available on request; not committed anywhere.

## Findings

### P0 — none
Every gc semantic claim that determines P0 was verified correct against independent probes **and** gc
1.27.1 primary source:

| Implementer claim | My probe (go1.27.1) | gc source | Verdict |
|---|---|---|---|
| Reset on expired-undrained → `true` | P1: `true`, no stale delivered (n=500: all true) | `modify()`: `pending = t.when > 0` → true; calls `timerchandrain` | ✓ outcome |
| Stop on expired-undrained → `true` + drained | P2: `true`, drained (n=500: all true) | `stop()`: `pending = t.when > 0` → true; calls `timerchandrain` | ✓ outcome |
| `Ticker.Stop` drains pending tick | P3: 0 remaining after Stop (n=500) | `stop()` → `timerchandrain(t.C)` unconditional | ✓ gc outcome |
| `NewTicker(0)` panics | P4: `panic: non-positive interval for NewTicker` — exact message | — | ✓ exact |
| Reset on expired+drained → `false` | P5: `false` (n=200) | `modify()`: pending false | ✓ |
| Stop active → `true`; double stop → `false` | P6: `true` / `false` | — | ✓ |

Mechanism differs (gc channel timers are *lazy* — no heap, no materialized sends while nobody is blocked;
the impl is eager), but every observable one-shot-timer outcome the implementer claimed is correct.

### P1
**P1-1. Ticker `Stop`/`Reset` do not drain pending ticks — deterministic gc divergence, falsely claimed
as tested.** `TimerHeap.java:187` (`stopChan`) and `:214` (`resetChan`) drain only when `!wasActive`.
But a live ticker is `wasActive=true` *and* can hold a stale tick in its buffer (the impl eagerly sends
every tick). gc's `stop()` calls `timerchandrain` unconditionally — my P3 probe: 0 ticks remain after
`Ticker.Stop`. My T1 probe on the snapshot: **1 tick remains** after `Timers.stopTicker()` on a ticker
with a buffered tick. T2: `Timers.resetTicker` with a pending stale tick delivered the stale tick
immediately in 1/5 iters (gc never delivers it). No corpus program and no unit test covers
stop/reset-with-pending-tick for tickers — the checkpoint's "Ticker.Stop drains pending tick … All
encoded in corpus + unit tests" (CHECKPOINT.md, ~15:30 entry) is **false** on both halves.
*Remediation:* unconditional drain in both paths (gc `timerchandrain` parity) —
```java
boolean drained = false;
while (owner.channelImpl().buffered() > 0) { owner.channelImpl().tryRecv(sched); drained = true; }
return wasActive || drained;  // stopChan; analogously in resetChan
```
I verified this exact patch in a scratch copy: 85/85 green, T1 → 0 remaining, T2 → 5/5 fresh ticks.
Add `tm13` (Go: ticker, let ticks accumulate, `Stop`, assert zero further receives) + unit tests
`testTickerStopDrainsBufferedTick` / `testTickerResetDrainsStaleTick` (the live tree's in-flight
remediation appears to be doing exactly this — re-review it independently).

**P1-2. `testTm01SleepOrder` (and `TimerTest.testSleepOrder`) are flaky by design; the "jitter cannot
reorder" comment is empirically false.** `TimerDifferentialTest.java:20` claims "sleep margins are ≥50ms
so jitter cannot reorder events". Measured on this box: single-timer end-to-end fire lateness up to
~69ms (cold) / ~24ms (warm), burst-sleep oversleep p99 ~33ms — the 50ms separations sit inside the noise
band. The suite failed this test once in three runs (`fast,slow,mid`). The 50/100/150ms ladder is also
narrow for a box running ~85 lingering daemon timer threads + ~85 drainer threads from earlier tests in
the same JVM (no `SchedulerCore` shutdown exists; see P2-7). gc's wakeup path is far tighter, so the
margin would be safe for gc but is not safe for this implementation.
*Remediation:* widen separations to ≥300ms in both tests (tm01 ladder and `testSleepOrder`), delete or
correct the "cannot reorder" comment, and/or restructure tm01 to assert *relative* ordering constraints
that are timing-robust (e.g., a rendezvous that forces order deterministically). Keep the 3-run suite
evidence after the change.

### P2
**P2-1. `len()` on timer channels diverges from gc and is undocumented.** gc presents timer channels as
unbuffered: `chanlen` always returns 0 (`src/runtime/chan.go`). The impl's timer channels are ordinary
`GoChan`s — my T6 probe: `ChanOps.len()` returns 1 after an undelivered fire. Latently observable the
moment the exporter lowers the `time` package (any `len(t.C)` program diverges).
*Remediation:* decide — either report 0 for timer channels (gc parity) or record as an accepted
divergence in `corpus/timers/README.md` and `GoTimer` docs. Do not leave it silent.

**P2-2. `testIoExitReacquiresSlot` passes vacuously if `ioExit` is a no-op.**
`IoSlotsTest.java:109` asserts only `continued` and `maxInflightObserved() <= 1`. If `ioExit` returned
without re-acquiring, the goroutine keeps running with `inflight` still 0 — both assertions still pass
(`maxInflight` counts admissions, not actual concurrent execution, so it cannot detect "running without
a slot"). The re-acquire property is never falsified.
*Remediation:* after `ioExit`, perform a second `ioEnter`/`ioExit` pair. Under a no-op `ioExit`, `inIo`
stays set and the second `ioEnter` throws the re-entrancy ISE — the test then fails. (One line each;
also add a white-box variant asserting `h.inIo()` is false after `ioExit`.)

**P2-3. Checkpoint claims about the tm05/tm07/tm08 *preconditions* are weaker than stated, and the tm05
README bullet is wrong.** `corpus/timers/README.md:20` says tm05 is "Stop after fire+drain → false" —
the program actually covers Stop on an expired-*undrained* timer → `true` then drain. More
substantively: tm05/tm07/tm08's "fired, not drained" preconditions rely on `Timers.sleep` (the
implementation under test) to create the pending tick; under mutation A (sleep broken) tm05 still passed
vacuously. These are testing the impl with the impl.
*Remediation:* fix the README bullet; in the Java translations, assert the precondition directly
(`ch.buffered() == 1` before the Stop/Reset) so a sleep regression fails the precondition rather than
passing the test vacuously.

**P2-4. Mutation-B full-suite kill is not a clean red — it hangs.** Under mutation B (ioEnter without
slot release), running the whole `IoSlotsTest` class hangs in `testIoExitReacquiresSlot`: `awaitAck`
(`SchedulerCore.java:268`) and `awaitQuiescence` (`:429`) have no timeouts, and `SchedTestRunner` has no
per-test timeout. The checkpoint's "B … → testIoEnterReleasesSlot" red-green claim is true for the
isolated test but the suite as a whole does not go red — it goes catatonic.
*Remediation:* give `SchedTestRunner` a per-test timeout (e.g., 60s → FAIL), and/or bounded waits in
`awaitAck`/`awaitQuiescence` that fail loudly instead of hanging.

**P2-5. `GoTimer` javadoc overclaims.** `GoTimer.java:10`: "semantics exactly (verified differentially vs
gc 1.27.1)". At `b3a7546` this is false for tickers (P1-1) and for `len()` (P2-1).
*Remediation:* scope the claim to one-shot timers, or fix P1-1/P2-1 first.

**P2-6. Racy pop-then-fire: `fire()` never checks `e.cancelled`.** `TimerHeap.java:292` polls the entry
and removes it from `byId`; `cancel(id)` between poll and `fire()` (`:305`) returns false without
marking `e.cancelled`, and `fire()` sends/unparks unconditionally. A `Stop`/`Reset` racing the fire
still delivers (gc's `seq` bump suppresses in-flight sends). The window is nanoseconds (no blocking
between poll and fire) but nonzero; the ticker re-arm path (`stillCurrent`) already shows the authors
know this class of race.
*Remediation:* set `e.cancelled = true` in `cancelLocked` even when the entry is absent from `byId`,
and check `e.cancelled` under the entry monitor in `fire()` before acting. Or document as an accepted
race with the window analysis.

**P2-7. No test pins the `b3a7546` headline change.** The commit message is "startTimer callback runs on
timer thread (gc parity)" — yet no test asserts which thread runs the callback (my T3 probe confirmed
`graalgo-timer` ✓), the `catch (Throwable)` in `fire()` (my T4 confirmed the timer thread survives a
throwing callback ✓), or that `Netpoll`'s five methods throw the GG-0018 UOE (my T5 confirmed ✓).
The `make check` netpoller-deletion gate also excludes `src/test` — a test could call `Netpoll.*`
without tripping it (probably intentional; undocumented).
*Remediation:* add the three pins (thread-name assertion; throwing-callback-then-next-timer-fires;
a `NetpollDeletionTest` asserting UOE + message on all five methods), and document the gate's
test-exclusion as intentional.

**P2-8. `testTimerResetActive` doesn't verify the old deadline was cancelled.**
`TimerTest.java:193`: if `resetChan` re-armed without cancelling the old entry, the single `recv`
still sees "fired" and the test passes; the stale old-deadline entry fires silently later.
*Remediation:* after the first recv, drain and assert 0 (no second fire).

**P2-9. Lingering scheduler threads: no `SchedulerCore` shutdown; every `withScheduler` leaks a daemon
timer thread and a drainer thread.** By design for the drainer (P1-4), but for tests it means ~85
timer threads accumulate in one JVM run, adding scheduling noise that feeds P1-2's flake and would trip
any thread-leak detector.
*Remediation:* add a test-only `SchedulerCore.close()` (or per-test timer-thread shutdown) and use it
in `withScheduler` teardown; at minimum document the accumulation as accepted.

**P2-10. Corpus and tests landed in the same commit (`1db223a`), so git history cannot show
gc-first provenance.** I compensated by re-running all 12 programs 3× against gc 1.27.1 — byte-identical
every time — so genuineness is established by re-derivation, not by history.
*Remediation (process):* future corpora commit the `.go` + gc-produced `.expected` first, translations
second; keep the `go run` evidence (I have it) alongside.

### P3 / notes
- **N1.** Checkpoint "(+2)" (red-green A) is unexplained in the text — it is `testSleepOrder` +
  `testTm01SleepOrder`, which I confirmed also die under mutation A. One clause would have sufficed.
- **N2.** Checkpoint "85/85 green" does not mention the observed `testTm01SleepOrder` flake (1/3 runs).
  Suite-green claims should carry the run count and any flakes (see P1-2).
- **N3.** CHECKPOINT.md ~14:30 design entry still says `startTimer` "arms + parks current goroutine,
  callback runs as wakeAction on next drainer cycle" — superseded by `b3a7546` (callback runs on the
  timer thread; committed 14:38 CEST, 8 minutes after the entry). Stale design text in the checkpoint.
- **N4.** `IoSlotsTest.java:76` comment claims a "real blocking syscall" but `PipedInputStream.read()`
  is a monitor wait, not a syscall. The test's logic is sound (slot released while parked); fix the
  comment or use a real blocking read (e.g., socket).
- **N5.** Untested acceptance-adjacent edges (each small; none contradict a claim): `Timers.tick`
  (`time.Tick`) has no test; `after(0)`/`newTimer(0)` immediate-fire untested; `sleep` with huge/negative
  ns untested (negative wraps to immediate wake in the impl; gc sleeps — undocumented); double-`Reset`
  untested; concurrent `Stop`+`Reset` from two threads untested (docs claim serialization); no
  timer-heap scale test (e.g., 1k concurrent timers); ioEnter/ioExit only exercised at GOMAXPROCS=1
  (acceptance only requires GMP=1 — note for P2-M10).
- **N6.** `testTickerDropsSlowReceiver`'s `n <= 2` bound is correct but subtle (buffer cap 1 + 1
  in-flight recv); a comment deriving the bound would prevent future "tightening" mistakes.

## What the milestone got right (adversarial credit)
- Every gc semantic claim I could falsify survived: Stop/Reset drain rules, `NewTicker(0)` panic
  message, ticker-Stop drain — all confirmed against independent probes and gc 1.27.1 source.
- Red-green is real: all four mutations killed exactly the claimed tests, with no collateral damage in
  the other 80+.
- The 12 corpus translations are faithful (same concurrency structure, same event order, no wall-clock
  values); `.expected` files are genuinely gc-produced (re-derived byte-identical 3×).
- The slot-release falsifiers are well-constructed: GOMAXPROCS=1 starvation design, both mutations
  caught, `maxInflightObserved() <= 1` invariant holding across all runs.
- Honest failure handling in the impl: `fire()`'s `catch (Throwable)`, `Entry.cancelled` races
  partially handled via `stillCurrent` on the re-arm path, fail-fast drainer asserts — all sound.

## Acceptance criteria (§4) — verdicts
1. **Timer semantics differential vs gc (Sleep/After/Tick/NewTimer incl. Stop/Reset edges): MET WITH
   P1-1 EXCEPTION.** One-shot timers: fully met. Tickers: `Stop`/`Reset` with a pending tick diverge
   from gc (fix verified, tests to be added).
2. **Slot-release proof (GOMAXPROCS=1: sleeping+busy; blocking-I/O+busy): MET.**
   `testSleepReleasesSlot`, `testIoEnterReleasesSlot` — both green, both verified red under mutation.
3. **Red-green for every semantic: SUBSTANTIALLY MET.** All four mutations kill exactly the claimed
   tests. Gaps: semantics with no test have no red-green (P1-1 ticker edges, P2-7 callback-thread /
   callback-exception / Netpoll-deletion pins); mutation B's suite-level kill hangs rather than going
   red (P2-4).

## Checkpoint honesty audit (P2-M3 entries)
| Claim | Evidence | Verdict |
|---|---|---|
| "85/85 green (52 carried + 33 new: TimerTest 15, IoSlotsTest 6, TimerDifferentialTest 12)" | Counts verified in source (15/6/12 new; 15+25+12=52 carried). My runs: 85/85, **84/85**, 85/85 | Numbers right; "green" omits the flake (P1-2, N2) |
| Red-green A→`testSleepReleasesSlot` (+2); B→`testIoEnterReleasesSlot`; C→tm05+`testTimerStopUndrained`; D→tm08+`testTimerResetExpiredUndrained` | All four reproduced in `/tmp/m3rg/phase2`; "(+2)" = the two order tests, also killed | ✓ (with P2-4 hang caveat on B) |
| "Reset on expired-undrained → true (drains); Stop … → true (drains); Ticker.Stop drains pending tick; NewTicker(0) panics. All encoded in corpus + unit tests" | gc outcomes all confirmed by my probes. But: ticker-drain is **not** encoded anywhere (P1-1); tm05/tm08 encode only the one-shot cases | gc claims ✓; "all encoded" ✗ |
| "`make check`: placeholder + debug + netpoller-deletion gates clean" | Re-ran: all clean | ✓ |
| ~14:30 design: "startTimer arms + parks current goroutine, callback runs as wakeAction" | Superseded by `b3a7546` (timer-thread callbacks, landed 14:38 CEST) | Stale (N3) |
