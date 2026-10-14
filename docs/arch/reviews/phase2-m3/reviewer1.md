# P2-M3 Independent Review — reviewer1

**Verdict: NO-GO**

**Snapshot:** `b3a7546` ("P2-M3: startTimer callback runs on timer thread (gc parity)"),
branch `master`, `~/workspace/graalgo`. Working tree was clean for all scoped
paths at review time, so the live tree at HEAD *is* the snapshot (verified
`git status --short -- phases/phase2/src/main phases/phase2/Makefile` empty).

**Scope (diff `245b46c` → `b3a7546`, `phases/phase2/src/main` + `phases/phase2/Makefile`):**
`runtime/TimerHeap.java` (new), `runtime/GoTimer.java` (new),
`runtime/Timers.java` (overlay), `runtime/Netpoll.java` (overlay),
`sched/SchedulerCore.java` (ioEnter/ioExit, timerHeap),
`sched/Drainer.java` (IO_ENTER/IO_EXIT, inIo set), `sched/ResumeToken.java`,
`sched/GoroutineHandle.java`, `Makefile` (netpoller-deletion gate).
Also read: `phases/phase2/PLAN.md` §2 WS C + §9 falsifiers,
`phases/phase1/INTERFACE_FREEZE.md` §3.4, `phases/phase1/docs/freeze-stub.md`
(Timers/Netpoll tables), `FreezeSignatureTest.java`, and gc 1.27.1 sources at
`~/tools/go/src/{runtime/time.go,runtime/chan.go,time/sleep.go,time/tick.go}`
(the same version the implementer probed).

**Method:** adversarial line-by-line review of the 919-line diff; traced every
lock acquisition for ordering; traced park/unpark generations through the
P2-D7 discipline for lost wakeups; checked the Stop/Reset state machine
against gc 1.27.1 *source* (not just behavior); verified frozen signatures by
diffing against the Phase-1 stubs; ran empirical probes compiled from the
snapshot sources into `/tmp/timerprobe` (repo untouched) to confirm each
deterministic divergence. Error-class hunt: for each bug pattern found, grepped
all callers for the same pattern.

**Bottom line:** the core mechanisms (heap, P2-D7 sleep discipline,
ioEnter/ioExit accounting, netpoller deletion, freeze compliance) are sound.
But the milestone's headline claim — "gc-exact Stop/Reset", "Go 1.23+ parity",
"verified differentially vs gc 1.27.1" — is false in deterministic,
high-frequency paths: Stop/Reset on a live ticker with a buffered tick do not
drain (gc 1.27 drains unconditionally via `timerchandrain`), there is no
stale-send suppression (gc 1.27's seq/sendLock protocol), and the timer thread
violates `GoChan`'s monitor protocol (data race). The 85/85 tests and 4/4
mutations do not cover any of these paths (all probes/mutations exercise only
the expired-undrained and active-no-buffer cases). Four P0s, each with
empirical or source-definitive proof below.

---

## P0 — correctness / soundness (must fix for GO)

### P0-1. Timer thread violates GoChan's monitor protocol — data race on channel state

`TimerHeap.java:319` (`fire()`), `:200` (`stopChan`), `:222` (`resetChan`).

`GoChan.trySend`/`tryRecv` are documented monitor-held primitives
(`GoChan.java:30-37`: "monitor-held primitives (Caller must hold the
monitor.)"); every other caller (`GoChan.send/recv`, `SelectOp.tryCompleteReady`
/`completeUnderLock`) holds `synchronized (ch)`. The three new `TimerHeap`
call sites do not:

- `:319`: `e.chan.trySend(System.nanoTime(), sched)` — timer thread, no monitor.
- `:200`,`:222`: `owner.channelImpl().tryRecv(sched)` — heap+owner locks held,
  channel monitor not held.

The timer thread therefore mutates `buf[]`, `count`/`head`/`tail`, and the
`recvWaiters`/`sendWaiters` `ArrayDeque`s concurrently with goroutines'
monitor-held `send`/`recv`/`select`. Consequences: torn `count` updates
(read-modify-write race → lost/duplicated ticks, buffer overrun), and
`ArrayDeque` corruption (not thread-safe). This fires on the *normal* path:
any `<-ticker.C` receive racing a tick delivery.

**Remediation:** wrap each site with the channel monitor —
`synchronized (e.chan) { e.chan.trySend(...); }` in `fire()` (no locks held
there today, so no ordering risk), and
`synchronized (owner.channelImpl()) { ... }` around the `buffered()`/`tryRecv`
sequence in `stopChan`/`resetChan`. Global order heap→owner→chan is
consistent: no path acquires chan→heap or chan→owner (`deliverToReceiver` /
`takeFromSender` only do the lock-free `drainer.offer`). Also fix the same
pattern in test scaffolding (`TimerTest.java:55`, see P1-5).

### P0-2. Stop/Reset do not drain on the active path — deterministic gc 1.27 divergence

`TimerHeap.java:187-208` (`stopChan`), `:214-231` (`resetChan`).

gc 1.27 `timer.stop()` (`runtime/time.go:478`) and `timer.modify()`
(`:549`, used by both `Timer.Reset` and `Ticker.Reset`) call
`timerchandrain` **unconditionally** (`time.go:512`, `:637`;
`chan.go:358` drains *all* buffered elements). The `time.Timer.Stop` doc
(`time/sleep.go`) guarantees: *"any receive from t.C after Stop has returned
is guaranteed to block rather than receive a stale time value from before the
Stop."* graalgo drains only when `!wasActive`:

- `stopChan`: `wasActive=true` (entry found — the normal case for a live
  ticker, which re-arms under the same id) → **no drain**.
- `resetChan`: same — drain only on the `!wasActive` branch; nothing when
  `id < 0` (stopped) either.

Empirically confirmed against snapshot sources (5/5 runs):
`ticker (20ms) → sleep 200ms (tick buffered) → stop()` returns `true` but
`buffered()==1` afterwards — a subsequent receive gets a **stale tick after
Stop**, exactly what Go 1.23+ eliminated. Worse, the stale tick then eats the
buffer: after `reset(100ms)` + 350ms, only **1** value is receivable where the
fresh post-reset ticks should arrive — the first new tick is dropped because
the buffer is still occupied by the pre-reset tick (probe `Probe3`).
This directly contradicts the checkpoint's own probe note
("Ticker.Stop drains pending tick … All encoded in corpus + unit tests") —
no test encodes it, and the implementation does not do it. The existing tests
only cover the expired-undrained one-shot path (tm05/tm08), which is why
85/85 stays green.

**Remediation:** mirror `timerchandrain`: in `stopChan`/`resetChan`, after the
cancel, always drain the channel (loop `tryRecv` until null; cap-1 makes one
pass sufficient but write the loop), and set `wasActive=true` if anything was
drained. Do the drain under the channel monitor (with P0-1's fix).

### P0-3. No stale-send suppression; wrong return value in the send-in-progress window

`TimerHeap.java:305-343` (`fire()`).

gc 1.27 `unlockAndRun` (`runtime/time.go:1089`) implements a seq/sendLock
protocol *specifically* for this race: the firing path copies `seq` under the
timer lock; `stop`/`modify` do `t.seq++` under `sendLock`; before sending, the
fire path re-checks `t.seq != seq` under `sendLock` and **skips the send** if a
stop/modify intervened. If a one-shot send was in flight and got suppressed,
`stop`/`modify` return `true` (`isSending` → `pending=true`, `time.go:503-508`,
`:628-633`). graalgo's `fire()` does `trySend` unconditionally with the
staleness check (`stillCurrent`) only *after* the send, for re-arm:

- Interleaving: timer thread pops entry → `stopChan` runs
  (`cancelLocked`→null, `buffered()==0` → returns **false**) → `fire()`
  delivers the tick anyway. gc returns **true** and delivers nothing.
  Two observable divergences (return value + stale delivery) in one window,
  violating even the task's stated rule (expired+undrained→true).
- Same for `resetChan`: returns `false`, delivers a stale pre-reset tick,
  which then occupies the buffer (feeds P0-2's drop).

**Remediation:** for CHAN entries, check staleness *before* the send under
heap→owner lock (`e.cancelled || !owner.stillCurrent(e.id, e.generation)`),
and hold the heap lock across check+`trySend` (heap→owner→chan nesting;
`trySend` never parks — it only does the lock-free `drainer.offer`).
Track "send in flight" (flag set under heap lock when the check passes,
cleared after the send); `stopChan`/`resetChan` treat it as active
(return `true`), mirroring gc's `isSending→pending`.

### P0-4. Ticker re-arm check and re-insertion are not atomic — spurious tick from a dead generation

`TimerHeap.java:318-341` (`fire()` re-arm block).

The `stillCurrent` check runs inside `synchronized (e.owner)`, but the block
exits the owner lock before mutating `e.deadlineNanos` and re-inserting
(`heap.add(e); byId.put(e.id, e)`) under the heap lock alone. A concurrent
`resetChan` can interleave exactly there: check passes (gen g current) →
`resetChan` bumps generation, arms E2 under a new id → `fire()` re-inserts
the **old entry E (gen g) under the old id**. E then fires once more,
delivering a tick from the dead generation (and, if a `stop()` intervened,
a tick *after Stop*). `byId` transiently holds both generations.

**Remediation:** extend the inner `synchronized (e.owner)` to cover the
deadline update and the `heap.add`/`byId.put` (still nested inside
`synchronized (lock)`), making check+reinsert atomic w.r.t.
`stopChan`/`resetChan`.

---

## P1 — semantic divergence risk / test gap (should fix for GO)

### P1-1. `GoTimer` constructor leaks `this` before the id is assigned (unsafe publication)

`GoTimer.java:49`: `this.id = heap.addChan(deadline, chan, this, periodNanos, generation);`

`this` (as `ChanOwner`) is inserted into the shared heap before the
constructor assigns `id`. If the timer thread pops and fires the entry in
that window, `stillCurrent` reads `this.id`/`this.generation` with no
happens-before edge (still `-1`/`0`) → a new ticker's re-arm check fails and
the ticker **silently dies after its first tick**. Window is nanoseconds
(deadline ≥1ns out), so practically unreachable — but the pattern is
definitively wrong, and the consequence (dead ticker) is silent.

**Remediation:** two-phase arm — constructor sets only final fields; a
package-private heap method (or static factory) takes heap→owner locks,
assigns the owner id *before* inserting the entry into heap/byId. This
preserves the documented heap→owner order (do NOT take owner→heap; that
inverts the order and can deadlock against `fire()`'s re-arm path).

### P1-2. `GoTimer.tick(0)` panics; gc `time.Tick` returns nil

`GoTimer.java:86-88`. gc (`time/tick.go:87-91`): `Tick` returns `nil` for
`d <= 0` ("Unlike NewTicker, Tick will return nil if d <= 0"). graalgo
delegates to `newTicker`, which panics. Empirically confirmed
(`Probe`: `tick(0)` threw `PanicException`).

**Remediation:** `if (ns <= 0) return null;` in `tick()` (null channels are
already representable — `selectGo` accepts null cases in tests).

### P1-3. Ticker `reset(0)` does not panic; gc panics

`GoTimer.java:113-115`. gc (`time/tick.go:66-68`): `Ticker.Reset` with
`d <= 0` panics `"non-positive interval for Ticker.Reset"` (while
`Timer.Reset` allows it). graalgo clamps via `Math.max(0, ns)` for both.
Empirically confirmed (`Probe`: `reset(0)` on a ticker did not panic).
`GoTimer` can distinguish (`periodNanos > 0` ⟺ ticker).

**Remediation:** in `reset(long ns)`,
`if (periodNanos > 0 && ns <= 0) Panic.throwPanic(GoStrings.of("non-positive interval for Ticker.Reset"));`

### P1-4. EXIT-while-inIo fail-fast is untested (and it halts the VM)

`Drainer.java:128-136`. The task asked to check for double-decrement:
there is none — EXIT with the handle in `inIo` throws `AssertionError`
(no decrement at all), which `drainLoop` turns into `halt(1)`. The path is
*reachable* (any runtime I/O code that misses `ioExit`, e.g. no
try/finally around a throwing blocking call), fail-fast-by-design — but no
test covers it (unlike duplicate-`IO_ENTER` and unpaired-`IO_EXIT`, which
have white-box tests). Same gap for `PARKED`-while-inIo (`:139-143`).

**Remediation:** add `processOneForTest`-driven tests: EXIT with the handle
in the inIo set → expect `AssertionError` (and assert `inflight` was not
decremented); PARKED with the handle in the inIo set → expect
`AssertionError`.

### P1-5. Test scaffolding repeats the P0-1 pattern

`TimerTest.java:55` (`drain()`): `ch.tryRecv(sched)` without the channel
monitor — the same violation as P0-1, in a test that runs concurrently with
the timer thread (`testTickerDropsSlowReceiver` drains a live ticker).

**Remediation:** `synchronized (ch) { ... }` around the drain loop.

---

## P2 — nits / docs / gate precision

### P2-1. Dead `!e.cancelled` check in `fire()`

`TimerHeap.java:324`. A cancelled entry can never be popped: peek,
cancelled-check, and poll are all under the heap lock in `timerLoop`
(`:242-262`), and `cancel()`/`cancelLocked()` set `cancelled` under the same
lock. The real guard is `stillCurrent`. Remove or document as belt-and-braces
— as written it suggests a protection that cannot trigger.

### P2-2. Ticker skip-ahead is grid-unaligned (gc aligns to the period grid)

`TimerHeap.java:327-332` vs gc `runtime/time.go:1113`:
`next = t.when + t.period*(1+delay/t.period)`. Both skip rather than burst
(the javadoc's "(gc drops ticks for slow receivers)" parenthetical
misattributes slightly — gc's drop is the 1-buffer; the skip is the re-arm),
but gc lands on the next period-grid point while graalgo does `now+period`.
Sub-period, transient, unobservable in practice — note only. (Also:
`Timers` javadoc says the sleep waiter parks "via parkCurrent
(sleep/startTimer)" — `startTimer` does not park; doc nit.)

### P2-3. Netpoller-deletion gate does not encode the falsifier's second disjunct

`Makefile:89-93`. PLAN §9 GG-0018: "netpoller code path reachable, **or NIO
bypassing slot discipline** → red". The gate greps only `Netpoll\.`
(excluding tests, which is correct — deletion tests must reference it).
No NIO exists in the goroutine path today (`SchedMain`'s `java.nio.file`/
`charset` is launcher startup file I/O, not scheduled I/O — verified), so
this is latent, not live.

**Remediation:** extend the gate with a `java\.nio\.channels|java\.net`
grep over `src/main` with an allowlist, or record why the `Netpoll\.` grep
is considered sufficient.

### P2-4. `FreezeSignatureTest` under-covers the frozen surface

`phases/phase1/.../FreezeSignatureTest.java:120-139` checks
`Timers.nanotime/sleep` and `Netpoll.init/open/close` but **not**
`Timers.startTimer/stopTimer` or `Netpoll.waitForEvents/breakWait`. I
verified all nine signatures byte-identical by diffing the Phase-1 stubs
against the snapshot (all match; additive methods don't collide), so this is
a machine-check gap only.

**Remediation:** add the four missing `Sig` entries.

---

## Verified correct (adversarial pass, no findings)

1. **Freeze compliance:** all frozen signatures byte-identical
   (`Timers.nanotime/sleep/startTimer/stopTimer`,
   `Netpoll.init/open/close/waitForEvents/breakWait`); bodies INTERNAL as
   allowed; `SchedulerCore.ioEnter/ioExit/timerHeap` and all new
   classes/methods are on unfrozen types or additive.
2. **P2-D7 sleep discipline:** `Timers.sleep` registers via `addSleep`
   *before* `parkCurrent`, with no safepoint between (straight-line Java) —
   traced UNPARK-before-PARKED (absorbed by `unparkPending` → immediate WAKE)
   and PARKED-before-UNPARK (parked record → WAKE): no lost wakeup on any
   order. `sleep(ns<=0)` immediate-return matches gc.
3. **Lock ordering:** every dual-lock site uses heap→owner
   (`stopChan`, `resetChan`, `fire()` re-arm); no reverse path exists
   (the `GoTimer` constructor takes no owner lock; `SchedulerCore`→`TimerHeap`
   construction does not publish). chan monitor is a leaf everywhere.
4. **ioEnter/ioExit accounting:** all `inflight`/lane/`inIo` mutations are
   single-drainer-thread; `IO_ENTER`→`inflight--`, `IO_EXIT`→WAKE→admit→
   `inflight++`, always paired by token order; `checkInvariants` (cap,
   non-negative) runs after every route+admit. EXIT-while-inIo
   fail-fasts instead of double-decrementing (see P1-4 for the test gap).
   The synchronous-release claim holds: `inflight--` precedes `acked.set`
   on the drainer thread, so `ioEnter()` returns only after release.
5. **Drainer fail-fasts are live, not dead code:** duplicate `IO_ENTER` and
   unpaired `IO_EXIT` throw `AssertionError` and are covered by
   `IoSlotsTest.testDrainerRejectsDuplicateIoEnter` /
   `testDrainerRejectsUnpairedIoExit` (via `processOneForTest`); the
   calling-thread CAS guards (`GoroutineHandle.inIo`) fail fast on
   re-entrant `ioEnter`/unpaired `ioExit` with tests.
6. **Netpoller deletion:** every `Netpoll` method throws UOE documenting
   GG-0018; the `make check` gate is adequate for `Netpoll.` references;
   no `java.nio`/`java.net` in the scheduled-I/O path.
7. **Timer mechanics:** `newTicker(0)` panic message byte-matches gc;
   `PriorityQueue` entries are never mutated while queued (pop → mutate →
   re-add); the wait loop cannot busy-spin (`wait<=0` breaks before
   `wait(0,0)`); the timer thread is daemon and never counts against the
   cap; `cancel()` lazy-removal is race-free (peek/check/poll under one
   lock); SLEEP/CALLBACK entries are one-shot with no stale-delivery
   concept; callback exceptions are contained (loud, thread survives).
8. **No other `this`-escape:** `SchedulerCore`→`TimerHeap`/`Drainer`
   constructors store but do not publish the reference (threads start in
   `start()`, not the constructor).

## Why "85/85 green, 4/4 mutations killed" does not cover this

The four red-green mutations (sleep-via-`Thread.sleep`, ioEnter-without-release,
stop-without-drain, reset-without-drain) and the 12-program differential
corpus exercise only: the expired-undrained one-shot path, active timers with
empty buffers, and sequential stop/reset. None exercise (a) stop/reset on a
live ticker with a buffered tick (P0-2 — deterministic), (b) fire racing
stop/reset (P0-3, P0-4), or (c) the timer thread against the channel monitor
(P0-1). gc 1.27.1 *source* (`runtime/time.go` `unlockAndRun`/`stop`/`modify`,
`runtime/chan.go` `timerchandrain`, `time/sleep.go` Stop doc,
`time/tick.go` `Ticker.Reset`/`Tick`) was used as the oracle above; the
implementation contradicts it in each P0.

## Suggested re-review gates

1. `stopChan`/`resetChan` always drain (P0-2) + regression tests:
   ticker-stop-with-buffered-tick → channel empty; ticker-reset-active →
   stale drained, fresh ticks arrive (the `Probe3` scenario).
2. Stale-send suppression (P0-3) + a stress test hammering stop/reset
   concurrent with fires, asserting return values and no post-Stop/Reset
   delivery.
3. Channel-monitor wrapping (P0-1) + a race detector or stress run
   (many receivers × rapid ticker) under `-Xcheck:jni`-style scrutiny —
   at minimum ThreadSanitizer-equivalent reasoning re-verified.
4. Re-arm atomicity (P0-4) + concurrent reset-during-fire test.
5. `FreezeSignatureTest` extended (P2-4); netpoller gate extended (P2-3).

---
*Reviewer: reviewer1 (independent subagent). No files modified. Empirical
probes compiled from snapshot sources to `/tmp/timerprobe` (scratch, not in
repo). gc oracle: `~/tools/go` 1.27.1 sources, the version the implementer
probed.*
