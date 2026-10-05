# Phase 2 checkpoint — Runtime: goroutines, channels, GC interop

Append-only. Newest state at the TAIL. Each entry: date, milestone, what
landed (commits), evidence paths, review status, blockers, next.

## 2026-10-05 — Phase-2 planning

Plan written: `phases/phase2/PLAN.md` (draft for parent review). Milestones
P2-M1…P2-M10; done-gates 1–5 with binary criteria; kill criteria verbatim from
PHASES.md §4; review gate carried from Phase 1; Phase-1 lessons recorded.
[OPEN] items: GC interop heap layout (P2-M4 design spike first), `sync`
package scope for gate 1, model-checking tool choice, repo layout.

## 2026-10-05 — P2-M1: [OPEN] repo layout resolved (P2-D8)

**Decision: CONFIRM supervisor's direction — scheduler lives in
`phases/phase2/src/` as its own build module, mirroring the Phase-1 layout;
Phase 1 is consumed as a FROZEN dependency via
`phases/phase1/INTERFACE_FREEZE.md` — with ONE explicit, narrow, documented
exception (below).**

Verified facts driving the decision:
- The `Safepoint.poll()` seam EXISTS in the Phase-1 AST (`GoRootNode`
  function entries, `GoLoopNode`/etc. back-edges) — confirmed by grep, not
  assumed. Phase-1 impl is a static no-op.
- Phase 1's own docs designate three classes as "Phase 2 owns":
  `graalgo.safepoint.Safepoint` ("Phase 2 gives it semantics"),
  `graalgo.runtime.Scheduler` ("Phase 2 owns goroutine scheduling",
  stub methods throwing UOE), `graalgo.runtime.GoroutineState`
  ("Phase 2 replaces the static holder with virtual-thread-scoped holders").
- Freeze §3.5: method *bodies* are INTERNAL, signatures FROZEN.
  `FreezeSignatureTest` pins name/params/return-type/static-shape.
- `NodeFactory.registerStmtKind` is PUBLIC and documented as the WS D/E
  extension hook ("WS E registers new statement kinds") — Phase 2 can
  register the `go`-statement builder without touching NodeFactory.java.
- `*ast.GoStmt` is currently REJECTED by `frontend/export.go`
  ("Phase 2 (goroutines; M5 task brief)").

Mechanism (the designed extension, not entanglement):
1. `phases/phase2/src/` — own Makefile, own `out/`; compiles against
   Phase-1 `out/classes` + Truffle jars. New package `graalgo.sched`
   (scheduler core, no FQN collisions).
2. Same-FQN replacements for exactly the three "Phase 2 owns" classes,
   with byte-identical signatures and real bodies. Runtime classpath puts
   `phase2/out/classes` FIRST; a startup fail-fast asserts the real
   `Scheduler` (not the stub) is loaded. Bodies-INTERNAL per freeze §3.5,
   so no ADR/version bump is required.
3. The `go` statement: Phase-2 `GoStatementNode` calling the frozen
   `Scheduler.spawn(GoFunc)`, registered via the public
   `registerStmtKind` hook from the Phase-2 launcher.
4. **The one exception:** `frontend/export.go` must emit the `GoStmt` wire
   node instead of rejecting it (~10 lines). Done as a single documented
   commit; `gofrontend` toolVersion bump per freeze §5 (JSON behavior
   change); wire stays v1 (additive node kind; old consumers ignore it per
   §2.3 forward-compatibility). No ADR needed. Rationale for not avoiding
   it: source-to-source rewriting of `go f()` in the launcher would break
   SourceSections and be exactly the silent entanglement the direction
   forbids.

P2-D8 recorded. Next: scheduler core implementation (WS A).

## 2026-10-05 — P2-M1: scheduler core implemented (WS A)

Commits: 26b5d8b (core), 2e5f012 (phase1 exporter exception), b993df7
(goldens), 8030950 (gate fix).

**What landed:**
- `graalgo.sched`: SchedulerCore (spawn/park/unpark, 10ms quantum,
  cap=min(GOMAXPROCS,carriers)), GoroutineHandle (vthread+ticket),
  ResumeToken (INIT/RESUME/WAKE/EXIT/PARKED/UNPARK), Drainer (platform
  thread, wake>init>run lanes, ack-before-unpark, hard cap assert,
  swallow-and-continue on interrupt).
- Same-FQN replacements (P2-D8, signatures byte-identical):
  graalgo.safepoint.Safepoint (quantum check; no-op w/o scheduler),
  graalgo.runtime.Scheduler (real spawn/yield/park/runnableCount),
  graalgo.runtime.GoroutineState (ThreadLocal vthread-scoped holder).
- `go` statement: GoStatementNode + public registerStmtKind hook;
  exporter emits additive "gostmt" wire node (toolVersion 0.2.0).
- SchedMain launcher: `--gmp=N`, quiescence await, Phase-1 exit codes.

**Evidence:**
- Unit: 10/10 green (`make -C phases/phase2 test`) — park/unpark (both
  orders), 200-cycle ack stress, cap+1 hard-assert (white-box),
  cap-held-under-load, quantum preemption (red-green), Safepoint
  overlay wiring, deterministic lane-order.
- Differential: 9/9 single-goroutine corpus programs byte-identical
  Phase-1 vs `--gmp=1`; Phase-1 suite 208/0 green after exporter change.
- Preemption: 2-goroutine busy-loop completes at `--gmp=1`
  (impossible without quantum expiry), output matches gc.
- `make -C phases/phase2 check` green; `make -C phases/phase1 check`
  green (incl. regenerated goldens).

**Review status:** 2 independent reviews dispatched (code + methodology),
reports pending in reviews/phase2-m1/.

**Next:** remediate findings → fresh suite+check → sign-off.

## 2026-10-05 ~12:45 CEST — P2-M1 review remediation (reviewer1 NO-GO)

Reviewer1 (code) returned NO-GO with 2 P0s + 4 P1s + 1 P2, all verified
as real. Remediation in progress (reviewer2 methodology still running).

**P0-1 (quiescence latch):** one-shot CountDownLatch could return
mid-spawn-stream. Fixed by documenting the closed-world spawn contract
(spawn set must be closed before awaiting; bulk spawns go through a
spawner goroutine) and rewriting both violating tests
(testSpawnAndJoin, testQuiescenceAt10k) to use the spawner pattern.
The flake reproduced in testSpawnAndJoin (1/6 runs) before the fix;
6/6 green after.

**P0-2 (wake action dropped):** unpark-before-park ran the wake action
on the waker thread (dropped if unpark won the race). Fixed: wake action
now stored in an AtomicReference on the handle and run by the WOKEN
goroutine after resume (both orders). Regression test
testWakeActionSurvivesUnparkBeforePark added (red-green: fails pre-fix).

**P1-1 (panic exit code):** spawned-goroutine panics now caught in the
spawn wrapper, printed as `panic: <value>`, recorded; launcher exits 2
(gc parity). Goexit kills just that goroutine. Verified: panic program
exits 2 with `panic: test panic` on stderr.

**P1-2 (SchedMain error handling):** main-body catch simplified —
PanicException/StackOverflowError now handled by the spawn wrapper;
SchedMain checks panicked()/crashed() after quiescence for exit codes.
Goexit-in-main: deadlock message + exit 2 iff no other goroutine live.

**P1-3 (overlay fail-fast):** Scheduler.isPhase2() marker added
(Phase-2-only, not in freeze); SchedMain asserts it at startup.
Verified: wrong classpath order fails fast with clear message.

**P1-4 (drainer death):** drainLoop now catches Throwable around
route/admit/checkInvariants and halts loudly (halt(1) after stderr)
instead of letting the thread die silently.

**P1-5 (docs):** GoroutineHandle javadoc corrected — the CAS ticket
discipline described the spike-2b external-waker protocol, not the
P2-M1 drainer-matched implementation. Now documents what P2-M1 does.

**P2 (spawn leak):** vthread.start() moved before live-count increment
and INIT offer; start failure leaks nothing.

**Tests:** 13/13 green (10 original + 3 new: wake-action survival,
spawned-panic recorded, spawned-RuntimeException recorded), 6/6 runs
stable. `make check` green (debug gate exclusions documented for
production diagnostics).

**Next:** reviewer2 report → remediate → fresh suite+check → sign-off.

## 2026-10-05 ~13:00 CEST — P2-M1 reviewer2 remediation

Reviewer2 (methodology) returned GO-WITH-FINDINGS: 1 P0 + 4 P1s + 5 P2s.
Report at reviews/phase2-m1/reviewer2.md.

**P0 (quiescence latch genuinely broken):** Reviewer2 confirmed the
one-shot CountDownLatch fires on transient mid-spawn zeros and can never
un-fire (reproduced: 9/10, expected=10000 actual=620). Fixed by replacing
the latch with a monitor (synchronized + wait/notify) + re-check loop:
awaitQuiescence() re-checks live==0 at call time, so a transient zero
during a prior spawn loop cannot cause early return. Documented the
no-concurrent-spawn contract (spawn must not race await from another
thread). Tests simplified back to the canonical spawn-loop-then-await
pattern. 8/8 runs stable at 15 tests.

**P1-1 (wake-action coverage):** Verified by mutation — deleting
action.run() kills testWakeActionSurvivesUnparkBeforePark (FAIL as
expected). The test provides the missing coverage.

**P1-2 (yieldCurrent untested):** Added testYieldCurrent (cap=1, quantum
disabled; 100 yields must let the peer run; red-green: no-op yield
hangs).

**P1-3 (preemption unauditable):** Archived smoke/preempt.go,
smoke/panic.go, smoke/hello.go with smoke/README.md documenting the
verification (wait-loop shape, quantum-neutered hangs, gc output match).

**P1-4 (GG annotations):** Removed inaccurate verifies: GG-SCHED-0001
from spawn/quiescence/wiring/crash tests (GG-SCHED-0001 is specifically
quantum-expiry; only testQuantumExpiryPreempts keeps it).

**P2s:**
- armed field javadoc: clarified the CAS protocol is not implemented in
  P2-M1 (drainer-matched only).
- README: fixed cap formula disagreement (intro now says
  min(GOMAXPROCS, carrier parallelism), matching P2-D6).
- Differential: re-ran with 9 named programs (sl01_consts, sl02_arith,
  cf01_ifelse, gv01_const, sl08_incdec, cf02_for, sl05_int_uint, hello,
  panic). 8/8 non-panicking byte-identical. panic.go differs only in
  stack-trace verbosity (Phase-1 gc-style vs M1 minimal); exit code (2)
  and message match. Known M1 limitation, not a correctness issue.
- Drainer interrupt-swallow: added testDrainerSurvivesInterrupt
  (verifies GG-SCHED-0002 non-terminating).
- Ack/unpark stress: documented as probabilistic (P2; acceptable for
  stress test).

**Tests:** 15/15 green (13 + yield + interrupt), 8/8 runs stable.
`make check` green.

## 2026-10-05 ~13:15 CEST — P2-M1 SIGN-OFF

**All reviewer findings remediated and re-tested.** Two independent
reviews completed (reviewer1: NO-GO with 2 P0s; reviewer2:
GO-WITH-FINDINGS with 1 P0). Every finding from both reports has been
fixed, with red-green verification where applicable.

**Final verification (commit 93c43d1, fresh clean build):**
- `make -C phases/phase2 test`: 15/15 green (8/8 runs stable).
- `make -C phases/phase2 check`: both gates clean.
- Differential: 8/8 non-panicking programs byte-identical (named in
  checkpoint); panic program: exit 2 + message match (verbosity differs).
- Preemption: smoke/preempt.go completes at --gmp=1, output matches gc.
- Phase-1 suite: 208/0 (reviewer2 independently re-ran; zero phase1
  files changed by P2-M1 remediation).

**P2-M1 acceptance criteria (PLAN.md §4):**
1. ✅ Scheduler core with goroutine=vthread+token, quantum preemption
   (10ms), drainer (wake>init>run lanes), cap=min(GOMAXPROCS,carriers)
   with hard assert, armed-ticket discipline.
2. ✅ Safepoint.poll() wired at function entries/loop back-edges (seam
   verified in Phase-1 AST; overlay mechanism documented).
3. ✅ Differential smoke: single-goroutine ≡ Phase-1 (8/8 identical).
4. ✅ Preemption smoke: 2-goroutine busy-loop interleaves at --gmp=1.

**Review gate:** ≥2 independent reviews completed; all findings
remediated and re-tested. Sign-off granted.

**GG IDs activated:** GG-SCHED-0001 (quantum expiry),
GG-SCHED-0002 (drainer), GG-SCHED-0003 (cap), GG-WAKE-0001
(ack-before-unpark), GG-WAKE-0003 (lane order), GG-0017 (crash
discipline) — core slice per PLAN.md.

## 2026-10-05 ~13:45 CEST — P2-M2 START (channels + select, WS B)

**Scope decision (parent constraint: no Phase-1 file modifications).**
The Phase-1 exporter rejects channel programs (`SendStmt`/`SelectStmt`/
`make(chan)` are "not supported in frontend/v1 scope" — verified:
gofrontend exits 2 on `ch <- 1`). End-to-end Go-source execution needs a
small exporter addition (5 lowering functions, additive wire kinds, under
the freeze change policy like the `gostmt` precedent) — flagged as a
DEPENDENCY for the parent, not done here. P2-M2 therefore owns:
1. The channel RUNTIME: same-FQN `graalgo.runtime.ChanOps` replacement
   (frozen signatures byte-identical, real bodies) + new
   `graalgo.runtime.GoChan`.
2. Java-level red-green tests driving ChanOps via spawned goroutines.
3. Differential corpus: Go programs in `phases/phase2/corpus/channels/`
   with gc-recorded `.expected` outputs; `ChannelDifferentialTest`
   executes mechanical Java translations on the scheduler and diffs.
   The translations are 1:1 syntactic (`ch <- 1` → `send(ch, 1L)`); the
   concurrency semantics under test emerge from the runtime, not the
   translation. These .go files double as the future end-to-end corpus
   once the exporter lowering lands.

**Design (Go runtime/chan.go adapted to scheduler-aware park):**
- `GoChan`: circular buffer, closed flag, separate FIFO send/recv waiter
  queues. All state under the channel monitor; park happens AFTER
  releasing the lock via `SchedulerCore.parkCurrent(null)`; post-park the
  waiter re-checks its outcome under the lock.
- Lost-wakeup freedom comes from the P2-M1 drainer protocol: waiter
  enqueues (visible) before park; waker dequeues + `unpark(handle)` under
  the same lock; the drainer matches PARKED/UNPARK by handle tolerating
  both orders (verified in Drainer.java: `parked` map + `unparkPending`).
- Close: wakes recv waiters with (zero, ok=false), send waiters with a
  must-panic flag. Send on closed → `Panic.throwPanic("send on closed
  channel")`; close of closed → panic "close of closed channel".
- Zero value: frozen `makeChan(long,int)` cannot carry the element type;
  added overload `makeChan(long,int,Object zeroValue)` (additive,
  documented). 2-arg form delegates with null zero.
- Select: `selectGo(SelectCase[]) -> int` (frozen). Poll all cases; random
  choice among ready (ThreadLocalRandom); `default` = case with
  `chan == null` (documented convention for the future exporter);
  blocking path enqueues one SelectCaseWaiter per (channel, case) sharing
  a SelectOp with CAS-claim (exactly one waker wins); resumed goroutine
  removes itself from all other channels (one lock at a time — no lock
  ordering issue). Received value/ok stashed in a ThreadLocal, read via
  added `selectRecvValue()`/`selectRecvOk()`.
- Send-on-closed in select: poison — panics only if select would
  otherwise block with no default; default wins over poison.

**GG-0017 chan slice →active** when the first channel test lands
(activation policy).

## 2026-10-05 ~15:30 CEST — P2-M2 implementation + tests complete

**Commits:**
- 89545d6: GoChan + SelectOp + ChanOps overlay + SchedulerCore.currentGoroutine
- 80f401e: ChannelTest (20 unit tests)
- ceb8d14: differential corpus (10 Go programs + .expected + translations)
- 4d0b545: corpus README

**Tests:** 45/45 green (15 scheduler + 20 channel + 10 differential).
`make check` green (both grep gates).

**Red-green validation (mutation checks, all reverted):**
- Mutation A (send never parks/delivers): 2 failures
  (testBufferedBlocksWhenFull, testCloseWakesSenderToPanic).
- Mutation B (no unpark on handoff): 2 failures, both as quiescence
  timeouts (testSelectSendHandoff, testPingPongStress) — lost wakeups
  are loud, never silent hangs.
- Mutation C (carrier-blocking sleep instead of parkCurrent): 4
  failures, including testBlockedSendDoesNotPinCarrier ("timeout waiting
  for: peer progress while sender parked") — the P2-D4 falsifier works.

**Differential:** 10/10 corpus programs byte-identical vs gc (incl. both
panic messages). No scheduling-observable divergences found; nothing to
add to the P2-M10 divergence list yet.

**GG-0017 chan slice →active** (first channel test landed with
verifies: GG-0017 annotations).

**Design notes for reviewers:**
- Same-FQN ChanOps overlay: frozen signatures byte-identical; added
  makeChan 3-arg overload, recvOk, selectRecvValue/Ok (additive).
- Select default = SelectCase with chan==null (documented).
- close() wakes select send waiters with poison → panic on resume
  (documented deviation risk: gc never async-panics a blocked select;
  only triggers in a racy interleave; deterministic close-before-select
  panics identically).
- Zero value: makeChan 2-arg form defaults to null; 3-arg form takes the
  type-directed zero (exporter contract).

## 2026-10-05 ~16:15 CEST — P2-M2 reviewer2 remediation (NO-GO → fixes)

Reviewer2 (methodology) returned NO-GO: 2 P0 + 4 P1 + 4 P2. All
remediated below. Reviewer1 (code) still running.

**P0-2 (select poison/default order — REAL BUG, fixed):** I had guessed
`select { case closedCh <- v: default: }` chooses default. Reviewer2
probed gc (go1.27.1); I reproduced: gc PANICS "send on closed channel".
Further probes established the exact rule: ready > poison-panic >
default > block (poison+ready-recv → ready wins, no panic; verified
p1–p4). Fixed in SelectOp.execute: poison check moved before the
default branch. Also fixed `select {}` (zero cases): was panicking,
now parks forever (gc deadlock parity).
- New unit tests: testSelectPoisonBeforeDefault,
  testSelectPoisonIgnoredWhenReady.
- New corpus: ch11_select_poison_default.go (panics),
  ch12_select_poison_ready.go ("recv ch2 99").

**P0-1 (false "10/10 byte-identical" claim — fixed):** ch09/ch10
.expected files were 0 bytes; tests hardcoded the panic strings.
Now: `ch*.panic` files hold gc's first stderr line (generated by
regen.sh); tests read them. Claim corrected: 9/12 stdout-identical +
3/3 panic-message-identical, all from files.

**P1-5 (poison-on-resume is gc parity, not a deviation):** I probed gc:
closing a select's send-case channel while the select is blocked
PANICS "send on closed channel" (p5). GoChan.close()'s poison wake is
therefore correct. Checkpoint's "deviation risk" language was wrong;
corrected.

**P1-6 (sleep-gated assertions → deterministic):** added
GoChan.sendWaitersCount()/recvWaitersCount() probes; all
Thread.sleep-based "must be parked" assertions now wait for the waiter
count (testBufferedBlocksWhenFull, testCloseWakesSenderToPanic,
testSelectBlocksUntilReady, testSelectSendHandoff, testSelectWakesOnClose,
testSelectCloseWhileParked).

**P1-3 (mutation evidence, made auditable):** exact patches:
- Mutation A: GoChan.send — replaced the post-enqueue
  `sched.parkCurrent(null)` + outcome check with `return` (sender
  enqueues waiter, never parks/delivers). Red: testBufferedBlocksWhenFull
  ("sender must be parked"), testCloseWakesSenderToPanic (panicVal null).
  NOT red: testUnbufferedSendRecv, testPingPongStress, ch01/ch04 —
  CORRECT, because the receiver drives the handoff via takeFromSender;
  the sender's park is not on the critical path when a receiver drains.
- Mutation B: GoChan.deliverToReceiver — removed
  `sched.unpark(rw.handle)` for plain receivers. Red:
  testSelectSendHandoff, testPingPongStress — both as quiescence
  timeouts ("lost wakeup?"), proving hangs are loud.
- Mutation C: GoChan.send — replaced `sched.parkCurrent(null)` with
  `Thread.sleep(60000)` (carrier-blocking). Red: 4 tests, incl.
  testBlockedSendDoesNotPinCarrier ("timeout waiting for: peer progress
  while sender parked") — the P2-D4 falsifier.

**P1-4 (ch08 translation doc):** corrected — the Go main's close+done
role is split into a second goroutine in the translation; documented.

**P2s:** added testSelectSameChannelTwice, testCloseWakesMultipleReceivers;
regen.sh scripts .expected/.panic generation; verifies: GG-0017 on the
differential class; ch06 comment clarified.

**Tests:** 51/51 green (15 scheduler + 24 channel + 12 differential).

## 2026-10-05 ~16:40 CEST — P2-M2 reviewer1 remediation (NO-GO → fixes)

Reviewer1 (code) returned NO-GO: 1 P0 + 3 P1 + 6 P2. P1-1/P1-2/P1-3
were already fixed in the reviewer2 remediation (poison-before-default,
select{} parks). Remaining:

**P0-1 (select lost-claim race — REAL BUG, fixed, reproduced by
reviewer 3/3):** The block-phase sweep's `completeUnderLock` set
`resultIndex` directly without winning the shared claim CAS. A
concurrent waker on another channel could win the claim, call
`setResult` + `unpark`, while the sweep also completed → resultIndex
overwrite + stale unpark (spurious wakeup on next park; reviewer's
probe saw a later `recv` spuriously return `(null, false)`).
Fix: `tryClaimSelf()` CAS before `completeUnderLock` in the sweep;
on CAS failure, break to park (winner's unpark in flight) and read
its result. Waker path already claimed via `pollAccepting→accept()`;
only the sweep bypassed it.
- New regression test: testSelectRacyCompleteNoStaleWakeup (50-channel
  sweep to widen the window; probes for stale unpark via a recv on an
  untouched channel). Note: the race is narrow; the test is a smoke
  test, not a deterministic reproducer. The fix was verified by code
  reasoning (claim CAS now guards all result-setting paths).
- P2-1: takeFromSender nulls the consumed buffer slot (was retaining).
- P2-4: block-phase sweep now iterates in random order (was
  index-ordered; poll phase already shuffled).
- P2-6: selectGo(null) → IllegalArgumentException (was NPE).

P2-2 (negative capacity message), P2-3 (nil channels), P2-5 (deadlock
detection): deferred — P2-2/P2-3 are frontend-rejected shapes;
P2-5 is P2-M10 scope. Documented in code.

**Tests:** 52/52 green (3 consecutive runs), incl. new P0-1 regression.

## 2026-10-05 ~17:00 CEST — P2-M2 SIGN-OFF

**Verdict: GO.** All acceptance criteria met, both reviews remediated,
all suites green on the final tree.

**Evidence (fresh on final tree 245b46c):**
- Phase-2 `make test`: 52/52, 0 failures (3 consecutive runs).
- Phase-1 `make test`: 208/208, 0 failures (no Phase-1 files modified).
- Phase-2 `make check`: clean (placeholder + debug gates).
- Differential: 12 corpus programs — 9 stdout-identical, 3 panic-message-
  identical vs gc 1.27.1, all from checked-in files (regen.sh).
- Red-green: 4 mutations (A/B/C + P0-1 revert), all caught by tests.
- Reviews: 2 independent (reviewer1.md, reviewer2.md), all P0/P1 remediated
  and re-tested; P2s fixed or documented as deferred.

**Acceptance:**
1. Unbuffered + buffered, close (send-panic/recv-drain/close-panic),
   select (multi-channel, default, fairness) — 26 unit tests.
2. Differential vs gc: 12/12 (no divergences; 2 gc behaviors corrected
   during review: poison>default, close-while-parked panics).
3. P2-D4: testBlockedSendDoesNotPinCarrier (GOMAXPROCS=1) — falsifies
   carrier-blocking (mutation C: 4 failures).
4. Red-green for every semantic — documented in checkpoint.

**Deliverables:** GoChan.java, SelectOp.java, ChanOps.java (overlay),
GoStrings.java; ChannelTest.java (26), ChannelDifferentialTest.java (12);
corpus/channels/ (12 programs + expected/panic + regen.sh + README).

**Parent dependencies flagged:** exporter lowering for channel/select
AST (Phase-1 rejects `*ast.SendStmt`); P2-M10 owns deadlock detection
and the divergence list (none found).

## 2026-10-05 ~14:30 CEST — P2-M3 START (timers/sleep + netpoller deletion)

**Design (verified against P2-M1/M2 code, not assumed):**

1. **TimerHeap** (`graalgo.runtime`, owned by SchedulerCore): priority-queue
   of deadlines + one daemon platform timer thread (not a carrier).
   Fire actions: SLEEP/CALLBACK → `sched.unpark(handle)` (waiter parked
   via `parkCurrent`, wakeAction runs on its vthread); CHAN →
   `chan.trySend(nanotime, sched)` non-blocking from the timer thread
   (drop if full — gc ticker parity). UNPARK-before-PARKED is safe via
   the drainer's unparkPending (P2-D7).

2. **Timers overlay** (same-FQN, frozen signatures byte-identical):
   `sleep(ns)` parks (ns<=0 returns immediately, gc parity);
   `startTimer(whenNanos, callback)` arms + parks current goroutine,
   callback runs as wakeAction; `stopTimer(id)` cancels. Additive:
   `after(ns)`, `newTimer(ns)`, `newTicker(ns)` → GoTimer.

3. **GoTimer**: NewTimer/NewTicker + Stop/Reset with gc-exact semantics
   (Stop→bool, Reset→bool; generation-guarded heap entries; Reset on
   expired-undrained verified differentially vs gc 1.27). Ticker re-arms
   deadline+=period with skip-ahead (no spin); NewTicker(0) panics
   (gc message parity).

4. **ioEnter/ioExit** (SchedulerCore, GG-0018/GG-SCHED-0004): new
   ResumeToken kinds IO_ENTER/IO_EXIT. ioEnter: synchronous slot release
   (offer + ack + unpark, awaitAck) so the release is deterministic;
   inIo set on handle (calling-thread fail-fast for re-entrant/unpaired)
   AND drainer-side set (halt-level invariant). ioExit: re-acquire via
   wakeFifo + awaitAck (blocks until a slot frees — gc exitsyscall
   parity). EXIT-while-inIo and PARKED-while-inIo are fail-fast.
   While in I/O the vthread blocks in the real syscall (Loom unmounts);
   the slot is free for other goroutines.

5. **Netpoll overlay**: same-FQN, all methods throw UOE documenting
   DELETION per GG-0018 (no readiness poller exists; blocking I/O uses
   ioEnter/ioExit). `make check` gains a netpoller-deletion grep gate
   (no `Netpoll.` references outside the overlay).

6. **Differential strategy**: frontend rejects `import "time"`, so the
   corpus is Java-level equivalents of Go timer programs (translation
   convention like P2-M2); `.expected` from `go run`; compare event
   ORDER, never wall-clock. 12 programs: sleep order, After, Timer
   Stop/Reset edges, Ticker, ticker-drop, select+After, Sleep(0).

## 2026-10-05 ~15:30 CEST — P2-M3 progress: core + tests green

**Implemented** (commit eb61aae): TimerHeap (deadline heap + daemon timer
thread), GoTimer (NewTimer/NewTicker + gc-exact Stop/Reset incl. Go 1.23+
expired-undrained drain rule, probed vs gc 1.27.1), Timers overlay (real
sleep/startTimer/stopTimer + additive after/newTimer/newTicker/tick),
Netpoll overlay documenting DELETION per GG-0018, ioEnter/ioExit on
SchedulerCore + Drainer IO_ENTER/IO_EXIT (inIo set, fail-fast asserts),
netpoller-deletion grep gate in `make check`.

**Tests** (commit 1db223a): 85/85 green (52 carried + 33 new: TimerTest 15,
IoSlotsTest 6, TimerDifferentialTest 12). Differential corpus:
corpus/timers/tm01–tm12 + .expected (gc regen) + regen.sh + README.

**Red-green** (all kill): A sleep-via-Thread.sleep → testSleepReleasesSlot
(+2); B ioEnter w/o slot release → testIoEnterReleasesSlot; C stop w/o
drain → tm05 + testTimerStopUndrained; D reset w/o drain → tm08 +
testTimerResetExpiredUndrained.

**gc probes that shaped semantics** (1.27.1): Reset on expired-undrained
→ true (drains); Stop on expired-undrained → true (drains); Ticker.Stop
drains pending tick; NewTicker(0) panics. All encoded in corpus + unit
tests.

**`make check`**: placeholder + debug + netpoller-deletion gates clean.

## 2026-10-05 ~16:00 CEST — P2-M3 review remediation (reviewer1 NO-GO → fixed)

**Reviewer1** (code) returned NO-GO with 4 P0s, all verified valid against
gc 1.27.1 source (runtime/time.go). All remediated:

- **P0-1** (timer thread violated GoChan monitor protocol): all
  trySend/tryRecv sites now hold the channel monitor (heap→owner→chan).
- **P0-2** (Stop/Reset did not drain on active path): stopChan/resetChan
  now drain unconditionally (gc timerchandrain parity); return
  wasActive||drained. Fixed a short-circuit bug during remediation
  (wasActive||drainLocked skipped the drain).
- **P0-3** (stale-send window): pop+staleness-check+send now atomic under
  the heap lock (trySend never blocks, so stronger than gc's
  seq/sendLock protocol — no interleaving possible).
- **P0-4** (re-arm not atomic): re-arm now under heap lock with the send.
- **P1-1** (this-escape): addChan assigns owner id under heap→owner locks
  before publication. **P1-2**: tick(0) returns null. **P1-3**:
  Ticker.Reset(0) panics (gc message). **P1-4**: EXIT/PARKED-while-inIo
  tests added. **P1-5**: test drain holds monitor. **P2-2**: gc grid
  alignment for ticker re-arm. **P2-3**: gate extended with
  java.nio.channels|java.net. **P2-4**: FreezeGapTest (phase-2 reflection
  covers the 4 signatures Phase-1 FreezeSignatureTest misses; Phase-1
  immutable).

**Tests**: 98/98 green (was 85). New: 7 TimerTest (3 P0-2 red-green,
stress, tick(0), reset panics), 2 IoSlotsTest (P1-4), 2 FreezeGapTest,
2 differential (tm13/tm14). **Red-green**: no-drain mutation kills 7
tests (3 new P0-2 + 4 existing).

**Reviewer2** (methodology) still running.

## 2026-10-05 ~16:30 CEST — P2-M3 reviewer2 remediation

**Reviewer2** (methodology) returned GO-WITH-FINDINGS. P1-1 (ticker
drain) was already fixed in 6b327f2 (reviewer1 P0-2 — same issue;
reviewer2 independently verified the fix). Remediated:

- **P1-2**: `testTm01SleepOrder`/`testSleepOrder` flaky (50ms inside
  ~69ms noise band; failed 1/3 runs) → widened to 300/600/900ms in the
  .go, both Java translations, and the class javadoc. gc re-run confirms
  .expected unchanged (order-only).
- **P2-1**: `len()` on timer channels (gc: always 0; impl: 1 when
  buffered) → documented as accepted Phase-2 divergence in GoTimer
  javadoc + corpus README (must fix before Phase 3 lowers `time`).
- **P2-2**: `testIoExitReacquiresSlot` passed vacuously under no-op
  ioExit → now does a second ioEnter/ioExit pair and asserts inIo
  cleared (fails under mutation).
- **P2-3**: README tm05 bullet fixed (was "fire+drain → false"; actually
  expired-undrained → true); tm13/tm14 documented; margin note updated.
- **P2-7**: added pins — callback runs on `graalgo-timer` thread,
  throwing callback doesn't kill timer thread, Netpoll 5× UOE+GG-0018.
- **P2-8**: `testTimerResetActive` now asserts no second fire (old
  deadline cancelled).
- **N3 correction**: the ~14:30 design entry's "startTimer arms + parks
  current goroutine, callback runs as wakeAction" was superseded 8
  minutes later by b3a7546 (callback runs on timer thread; caller never
  parks). Current design: TimerHeap javadoc.
- **N4**: PipedInputStream comment fixed (monitor wait, not syscall).

Deferred: P2-4 (per-test timeout; mutation-B hang), P2-9 (SchedulerCore
shutdown; thread accumulation documented as accepted for now).

**Tests**: 101/101 green.

## 2026-10-05 ~15:45 CEST — P2-M3 reviewer3 re-review (GO-WITH-FINDINGS)

Reviewer3 (independent re-review of bce75f9) verified all 9 items:
P0-1..P0-4 fixed (lock order exhaustive, no cycle), P1-1 confirmed,
P1-2 fixed (3/3 gc byte-identical), 2 genuine red-green falsifiers
(testTickerStopDrainsBufferedTick, testTickerResetDrainsStaleTick —
3/3 red on old, 2/2 green on new), P2-2 non-vacuous, no new P0/P1.
Full suite independently reproduced 101/101.

**Record corrections (P2-1, P2-2, reviewer3):**
- Reviewer1's P0-4 mechanism as described was IMPOSSIBLE — the b3a7546
  code already held the heap lock across the stillCurrent check and the
  re-insert, so "resetChan interleaves between check and re-insert"
  could not occur. The real pre-fix stale-tick race was P0-3's unlocked
  trySend. The remediation is correct and strictly stronger than needed;
  this is record accuracy only.
- The "~16:00" entry's "3 P0-2 red-green" is 2 falsifiers + 1 pin:
  testTimerStopAfterFireDrains passes on pre-fix code (one-shot
  expired-undrained drain predates the fix) — a regression + monitor
  pin, not a falsifier. testNoTickAfterStopStress is likewise a stress
  pin, not a falsifier (comments corrected).
- TimerHeap javadoc "sleep/startTimer" park claim fixed (startTimer
  never parks); fireChanLocked's dead owner==null branch annotated.

Deferred (unchanged): P2-4 per-test timeout, P2-9 SchedulerCore shutdown.

## 2026-10-05 ~16:00 CEST — P2-M3 SIGN-OFF

**Verdict: DONE.** All acceptance criteria met with evidence.

**Evidence (final tree 6056a98):**
- phase-2 `make test`: **101/101 green** (3× consecutive runs, 0 flakes)
- phase-2 `make check`: all gates clean (netpoller-deletion, freeze, deprecation)
- Phase-1 suite (`make -C phases/phase1 test`): **208/208 green**
  (run on bce75f9; delta to 6056a98 is comment-only in phase-2 + checkpoint)
- Differential corpus: 14 programs (tm01–tm14), all gc-identical
  (byte-identical .expected, re-derived 3× under gc 1.27.1)
- Red-green: 2 verified falsifiers (3/3 red on old, 2/2 green on new);
  no-drain mutation kills 7+ tests

**Reviews:** 3 independent reviews, all findings remediated + re-tested.
- reviewer1 (code): NO-GO → 4 P0s + P1s/P2s all fixed (6b327f2)
- reviewer2 (methodology): GO-WITH-FINDINGS → P1-2 + P2s fixed (bce75f9)
- reviewer3 (re-review): GO-WITH-FINDINGS, no P0/P1 → record/doc nits
  fixed (6056a98)

**Accepted divergences:** (1) `len()` on timer channels returns 1 when
buffered (gc: 0) — must fix before Phase 3 lowers `time`; (2) per-test
timeout deferred; (3) SchedulerCore shutdown / thread accumulation
deferred.

**Commits:** eb61aae (impl) → b3a7546 → 6b327f2 (reviewer1) →
bce75f9 (reviewer2) → 6056a98 (reviewer3).

## 2026-10-05 ~15:45 CEST — P2-M4 design spike + ADR-0002 (phase gate)

**Spike** (`/tmp/gcspike/Spike.java`, throwaway, 7/7 PASS):
- Parked-vthread roots via the real SchedulerCore + GoChan park path:
  ~20MB graph root + deep leaf both survive forced GCs while parked
  (roots enumerated from the parked stack; transitive closure exact —
  no holes); after goroutine exit the graph is collected (no
  over-retention by scheduler/channel structures).
- `runtime.SetFinalizer` vehicle (`java.lang.ref.Cleaner`): runs once
  after unreachability, never while reachable.
- Barrier audit: zero off-heap allocation (`allocateMemory`: 0 hits);
  zero `Unsafe`/`MemorySegment`/`Arena` in sources; `unsafe.Pointer`
  is the JVM-heap handle table. Every Go reference store is JVM-heap →
  JVM GC barriers cover all stores.

**ADR-0002** (`phases/phase2/adr/ADR-0002-gc-interop.md`, status: proposed):
closes PLAN.md §3 [OPEN] heap layout. Decisions: (D1) all Go objects in
JVM heap, JVM GC *is* the Go GC — no segregated region, no Go marker
(SPEC §3.4 [DECIDED] + spike); (D2) root enumeration is the JVM's, exact
by construction — rejects cooperative root publication as duplicated
machinery; (D3) no Truffle-level write barrier — JVM barriers cover all
stores, guarded by a new `make check` grep gate; (D4) Go-observable GC
surface (SetFinalizer/KeepAlive/GC/weak/memstats) emulated on JVM
primitives, tuning knobs → P2-M5 GODEBUG registry. Falsifiers F1–F3 +
re-test triggers recorded. Implementation starts after review sign-off.

## 2026-10-05 ~16:10 CEST — P2-M4 ADR reviews remediated, delta re-verification pending

Both ADR reviews returned GO-WITH-FINDINGS (reviewer1: 7 P1 + 5 P2;
reviewer2: 6 P1 + 4 P2). All 22 findings remediated in 3d94f0f:
- D4 table repaired: single shared Cleaner + resurrection/thread-identity
  divergence rows; KeepAlive → Reference.reachabilityFence; runtime.GC() →
  System.gc() + MXBean-poll wait w/ bounded timeout, documented
  approximation; ReadMemStats → field-table requirement;
  pprof + FreeOSMemory rows added; UnsafeHandles/finalizer → DIVERGENCES.md.
- F2 rewritten (coherent pause-based trigger); collector decision recorded
  (G1 default, {G1,ZGC,Shenandoah} expected); F4 added (standing GC-torture
  differential across park kinds × collectors); re-test triggers += cgo/FFM,
  collector change.
- D2 now argues from the funnel invariant (single park site); [MEASURED]
  scoped to what ran; grep gate patterns extended + rationale split;
  GC-visibility differential wired to F3; archived spike path canonical.
- PLAN.md WS D wording fixed (was: "write barriers at the Truffle level").
- P2-M4 closeout checklist recorded in ADR (3 blocking deliverables w/
  evidence files).
Delta re-verification dispatched to both reviewers; implementation starts
after DELTA-GO from both.

## 2026-10-05 ~17:30 CEST — P2-M4 implementation complete, tests green

- Phase-2 `graalgo/runtime/GcInterop.java` (real, shadows Phase-1 stub): single
  PhantomReference finalizer thread (nil-arg, serialized), KeepAlive via
  reachabilityFence, GC() via System.gc()+MXBean poll (30s bound), weak.Pointer
  vehicle, memStats() field table + PauseNs ring via MXBean notifications.
- `make check`: new off-heap/barrier grep gate (ADR-0002 D3/F3) + GcInterop
  debug-println exclusion (documented, TimerHeap precedent).
- Differential corpus `corpus/gc/` (4 programs + gc-1.27.1 .expected):
  gc01 finalizer runs, gc02 SetFinalizer(x,nil) removal, gc03 KeepAlive,
  gc04 GC hint. Key empirical finding: stock gc needs GC()+allocation in the
  poll loop for prompt finalization (bare runtime.GC() loop never fired).
- `GcDifferentialTest` (subprocess SchedMain end-to-end, diffs vs .expected)
  + `GcInteropTest` (7 unit tests). Suite: 113/113 green (101 + 12 new).
- `DIVERGENCES.md` created with 5 P2-M4 entries (DIV-M4-1..5).
- Phase-1 suite 208/208 green with exporter change (verified in background).
- Next: commit, dispatch ≥2 independent implementation reviews, remediate,
  re-verify, sign off.

## 2026-10-05 ~18:30 CEST — P2-M4 remediation complete, awaiting reviewer #2

- Remediated all reviewer #1 findings (P1-1..3, P2-4..8, P3-15):
  * Switched to java.lang.ref.Cleaner (ADR-0002 D4 compliance)
  * Fixed Cleanable.clean() misuse (it runs the action; use cancellable flag)
  * Added PINNED set for register/cancel window lifetime
  * Matched gc's 'finalizer already set' fatal
  * Added DIV-M4-6..9 to DIVERGENCES.md
  * Added gc05_finalizer_replace corpus case
- Key insight: back-to-back runtime.GC() starves finalizer goroutine under
  multi-P (world almost always stopped); GOMAXPROCS=1 required for recording.
- Key insight: Cleanable.clean() RUNS the action (not cancel); use flag.
- Tests: 114/114 green. make check green. Committed.
- Reviewer #2 (redispatch) completed; report pending delivery.

## 2026-10-05 ~19:30 CEST — P2-M4 SIGN-OFF

**Status:** COMPLETE. All done-gates met.

**Evidence:**
- Phase-2 `make test`: 124/124 green (fresh, final tree)
- Phase-1 suite: 208/208 green (fresh, final tree)
- `make check`: all gates green (placeholder, debug-leftover, netpoller-deletion, off-heap/barrier)
- Per-collector: G1 124/124 PASS, ZGC 124/124 PASS (Shenandoah N/A — Graal JIT unsupported)

**Reviews:** 2 independent reviews, all findings remediated and re-tested.
- Reviewer #1 (correctness): GO-WITH-FINDINGS (3 P1, 5 P2, 8 P3) — all fixed.
- Reviewer #2 (design fidelity): NO-GO (5 P1, 5 P2) — all fixed, including
  F4 torture test, F3 visibility backstop, gc01 determinism (GOMAXPROCS=1),
  ADR scope amendment for weak/ReadMemStats deferral.

**Deliverables:**
- `graalgo/runtime/GcInterop.java` (phase-2, Cleaner-based finalizers,
  KeepAlive via reachabilityFence, GC() via MXBean poll, weak vehicle,
  memStats field table)
- Differential corpus `corpus/gc/` (6 programs: finalizer, remove, replace,
  keepalive, GC hint, panic-loudness; gc-1.27.1 .expected where applicable)
- `GcDifferentialTest`, `GcInteropTest`, `GcTortureTest`, `GcVisibilityTest`
- `DIVERGENCES.md` (DIV-M4-1..9)
- ADR-0002 accepted with scope amendment; `make check` off-heap/barrier gate

**Key findings:**
- Back-to-back runtime.GC() starves finalizer goroutine under multi-P;
  GOMAXPROCS=1 required for deterministic recording.
- Cleanable.clean() RUNS the action (not cancel); use cancellable flag.
- gc fatals on double SetFinalizer without nil (matched).
- JIT liveness requires KeepAlive for reliable cancellation; PINNED set
  bridges the register/cancel window.

## 2026-10-05 — P2-M5 START (GODEBUG surface triage, WS E)

**Goal:** versioned registry of every GODEBUG knob (emulated / no-op /
unsupported-with-justification); unknown knobs deny-with-error (never
silently ignore); `godebug-diff` tooling diffing emulated-knob behavior vs
gc. GG-0055→active. Gate 5 (part).

**Knob universe** (from go1.27.1 source): 33 `dbgvars` in
`src/runtime/runtime1.go` + `memprofilerate` + 10 removed
(`src/internal/godebugs/table.go`) + ~45 non-runtime `internal/godebugs`
entries.

**Triage plan:**
- EMULATED (4): `schedtrace`, `scheddetail` (periodic scheduler-state
  lines, graalgo format, documented), `gctrace` (per-cycle line from JVM
  GC notifications, documented), `panicnil` (pre-1.21 panic(nil)
  semantics via same-FQN `graalgo.runtime.Panic` overlay + drift test —
  P2-D8 pattern).
- UNSUPPORTED (deny on non-default): `gccheckmark`, `gcpacertrace`,
  `gcstoptheworld`, `scavtrace` (JVM GC internals), `inittrace`
  (Phase-1 frontend territory, frozen), `tracebackancestors` (no creator
  tracking), `clobberfree`, `efence` (allocator debugging on JVM heap).
- NO-OP (accept+ignore, justified): the rest — incl. `asyncpreemptoff`
  (no signal-based async preemption exists; effective behavior always
  equals asyncpreemptoff=1), all non-runtime knobs (subsystem absent).
- REMOVED: deny iff set to gc's "old" value (matches gc fatal);
  accept otherwise. `asynctimerchan=1` denied (our timers are async).
- Deliberate divergences from gc (→ DIVERGENCES.md): unknown knob →
  hard error (gc silently ignores); malformed value → error (gc keeps
  old); bare field without `=` → error (gc skips).

**Design:** `graalgo.sched.launcher.GoDebug` (parse/apply/accessors) +
`src/main/resources/graalgo/sched/godebug/registry.json` (versioned;
Makefile copies resources) + `SchedMain` startup hook (exit 2 on deny,
matching gc's fatal). Machine-checked version test (version constant +
knob-name-set hash). `tools/godebug-diff/` probes per emulated knob.

## 2026-10-05 — P2-M5 progress (implementation complete, reviews dispatched)

**Commits:** f1c8774 (registry v1 + Makefile resources), bc67b1a
(GoDebug/SchedTrace/GcTrace/Panic overlay/SchedMain hook), bb3c268
(22 tests + drift test + corpus), d5eb521 (godebug-diff 4/4 green),
fdc7d7f (GODEBUG.md + DIV-M5-1..4), c97b933 (make-check gate exclusions).

**State:**
- `GoDebug` parses/validates GODEBUG at SchedMain startup; unknown knobs,
  malformed values, bare fields, and non-default unsupported knobs all deny
  with exit 2. Registry: 79 knobs (4 emulated, 9 unsupported, 66 noop) +
  9 removed, version 1, hash-pinned by test.
- Emulated: schedtrace/scheddetail (daemon), gctrace (GC notifications),
  panicnil (Panic overlay + drift test; byte-identical vs gc on recover path).
- Tests: phase-2 suite 146/146 green (124 existing + 22 new). `make check` green.
- `tools/godebug-diff`: 4/4 differential green vs gc (REPORT.md committed).
- `GODEBUG.md` published; DIV-M5-1..4 recorded.

**Reviews dispatched** against snapshot c97b933 (read-only):
- reviewer1 (correctness) → reviews/phase2-m5/reviewer1.md
- reviewer2 (methodology) → reviews/phase2-m5/reviewer2.md

**Remaining:** remediate findings → re-test → fresh phase-2 suite +
Phase-1 suite (208) + make check on final tree → sign-off.

## 2026-10-05 — P2-M5 reviewer1 (correctness) report: NO-GO → remediated

**Verdict:** NO-GO. Headline: snapshot c97b933 didn't compile —
`GcTrace.resetForTests()` was left uncommitted (my commit-discipline
slip: tests committed without their production counterpart). Fixed by
committing.

**P1s fixed (commit fe297b8):**
1. Registry publication race (removed assigned before knobs now).
2. `String.format` locale → Locale.ROOT in GcTrace.
3. `cgocheck>1` now denied like gc (was silently accepted, then killed
   the gc-built gofrontend child confusingly).
4. gofrontend no longer inherits GODEBUG (scrubbed in SchedMain;
   keeps the build log deterministic).

**P2s fixed:** non-ASCII digits rejected; JSON `\b`/`\f` escapes;
panicnil nil-interface collapses to nil (uncaught prints "panic: nil");
memprofilerate accepts int64 like gc; registry-load failure denies
cleanly via GoDebugException.

**New tests:** testCgocheck2Denied, testNonAsciiDigitsDenied,
testMemprofilerateHugeAccepted. Registry bumped to v2 (cgocheck
justification); GODEBUG.md regenerated.

**Re-verified:** phase-2 suite 149/149 green, `make check` green,
godebug-diff 4/4 green.

**Still pending:** reviewer2 (methodology) report.

## 2026-10-05 — P2-M5 reviewer2 (methodology) report: NO-GO → remediated

**Verdict:** NO-GO. P0 was the same uncommitted-method issue as
reviewer1 (fixed). Methodology findings remediated (commit bba197c):

- **Hash strengthened:** now covers name+disposition+default (a
  disposition retriage fails the check and forces a version bump).
- **DIV-M5-5 added:** the five NO-OP knobs whose effective behavior
  permanently equals a non-default value; triage principles stated in
  GODEBUG.md; tracebacklabels justification addresses the no-labels case.
- **godebug-diff comparators strengthened:** schedtrace requires >=3
  lines with strictly increasing timestamps; scheddetail has a negative
  control (no lines without the knob); gctrace requires >=2 cycle lines.
  4/4 green.
- **panicnil uncaught coverage:** new db05 probe; positive
  ("panic: nil" with panicnil=1) and negative (default prints the
  error) subprocess tests.
- **gc-faithful fixes:** panicnil enables old behavior only for ==1
  (panic.go:811); negative schedtrace accepted as off like gc;
  memprofilerate int64 (was reviewer1 P1); cgocheck>1 denied (was
  reviewer1 P1).
- **multipartfiles removed:** it is commented out of go1.27.1's
  godebug table (not a real knob). Registry v3, 78 knobs + 9 removed.
- **Checkpoint correction:** the "10 removed" in the triage plan was
  wrong; it is 9 (matches upstream).

**Re-verified:** phase-2 suite 152/152 green, `make check` green,
godebug-diff 4/4 green.

**Still pending:** Phase-1 suite re-run (208 tests) before sign-off.

## 2026-10-05 — P2-M5 SIGN-OFF

**Acceptance (phases/phase2/PLAN.md §4 P2-M5):** versioned registry
published (v3, 78 knobs + 9 removed, GODEBUG.md); unknown-knob
deny-with-error (falsifiable: testUnknownKnobDenied asserts exit 2);
godebug-diff 4/4 green with strengthened comparators.

**Fresh verification on the final tree (this sign-off):**
- phase-2 `make test`: **152/152, 0 failures**
- Phase-1 suite: **208/208, 0 failures** (INTERFACE_FREEZE untouched)
- `make check`: **green**
- `tools/godebug-diff`: **4/4 PASS**

**Reviews:** 2 independent (correctness + methodology), both NO-GO →
all P0/P1/P2 findings remediated and re-tested (commits fe297b8,
2a57796, bba197c, 2cdcd01). No open findings.

**Deliverables:** GoDebug parser/applier/registry (deny exit 2),
SchedTrace/GcTrace daemons, panicnil overlay (drift-guarded),
godebug-diff tool, GODEBUG.md, DIVERGENCES.md DIV-M5-1..5.

P2-M5 done. Workstream E complete.

## 2026-10-05 — P2-M6 START (JFR goroutine/channel events, WS F → gate 3)

**Design (from SPEC §2.7/§3.10 + task brief):**
- New package `graalgo.jfr`: `GoroutineStart/End`, `GoroutinePark` (reason +
  duration, committed on resume, duration-threshold filtered), `ChannelBlock`
  (operation SEND/RECV/SELECT). Fast-path (non-blocking) channel ops emit
  nothing — rate control by design.
- Hook points: `SchedulerCore.spawn` (start/end), `quantumPark` (QUANTUM),
  `parkCurrent` overload with `ParkReason` (CHANNEL/TIMER/SELECT via
  GoChan/SelectOp/Timers), `ioEnter`/`ioExit` (IO, full syscall duration),
  `awaitAck` post-resume emission (threshold: `graalgo.jfr.parkThresholdNanos`,
  default 100µs — quantum parks ~µs vanish, real blocks stay).
- Disabled = near-zero: every emission guarded by prototype `isEnabled()`
  (one field read); no allocation when no recording.
- `jdk.VirtualThreadStart/End` fire for free (goroutines are vthreads);
  test will assert correlation via thread name `goroutine-<id>`.
- Evidence target: `phases/phase2/hidden_files/jfr/EVENTS.md` + recorded JFR
  file + event counts (done-gate 3).

## 2026-10-05 — P2-M6 progress
- Hookups committed: 6f8ca2b (events+facade), 401132b (scheduler:
  start/end, park reasons, IO duration), 1c5a03d (channel/timer:
  ChannelBlock, CHANNEL/TIMER/SELECT reasons). Suite 152/152 green at
  each commit; Phase-1 untouched.
- **Design correction (JFR default-enable trap):** `@Enabled` defaults to
  true for custom events — a bare `new Recording()` was capturing our
  events (and `isEnabled()` returned true) even without explicit enable.
  Caught by `testNoEventsWhenDisabled` (red). Fix: all four events now
  `@Enabled(false)` — opt-in via `rec.enable("graalgo.*")`. The guard
  pattern is only sound with default-off; documented in JfrEvents.
- `testEventsFlow` green first run: start/end pairing, exactly 1 RECV
  ChannelBlock (send via direct handoff emits nothing), CHANNEL+TIMER
  park events with durations, jdk.VirtualThreadStart correlation.

## 2026-10-05 — P2-M6 DONE (JFR goroutine/channel events, WS F → gate 3)

**Reviews (2, both GO-WITH-FINDINGS, all remediated + re-tested):**
- Reviewer 1 (correctness): 0 P0 / 1 P1 (IO double-count) / 7 P2 —
  remediated in 4a9f393 (notePark removed from ioEnter/ioExit,
  emitQuietly isolation, doc fixes).
- Reviewer 2 (methodology/testing): 4 P1 / 8 P2, independently verified
  (evidence-file counts/durations via jfr summary/print; red-green
  genuineness via standalone probe; 5/5 determinism replication of the
  1-in-1024 sampler; 24.3% replication of the "measured 26%"). Reports:
  reviews/phase2-m6/reviewer1.md, reviewer2.md.
  - P1-4: the ~600ns Event.isEnabled() claim did not reproduce (measured
    7.4ns/9.2ns on GraalVM 25 here, 3.5–5.9ns by reviewer) — the caching
    layer built on it was REMOVED; isEnabled() is called directly
    (f4589fa). Disabled guard re-measured 23ns/park, 0.38× nanoTime.
  - P2-7: IO park now begin()s at ioEnter / commits at ioExit (spans the
    syscall); probe showed begin() is a no-op while disabled, so a
    mid-syscall-started recording falls back to emit-at-exit (5e3439a,
    red-green via mutation: old code fails "interval does not span: 0ns").
  - P1-1: testEventsFlow hardened — sleep-paced (1ms) waiter probe with
    10s fail-fast (the yield-spin pinned carriers on 2-core VMs, ~40%
    flake), absolute 200ms bound replaced by "≥1 long (≥50ms) park per
    reason" (9c44ebe).
  - P2-5/P2-6: ensureRegistered() before rec.start(); throughput smoke
    is median-of-3 with adjacent pairing (single-shot flaked at 0.54
    under VM load 4.4).
  - Docs: EVENTS.md ChannelBlock line fixed to the actual file (2 RECV +
    1 SEND, genuine 5ms block — P1-2); DIV-M6-1 honestly worded
    (runtime/trace layer (a) maps directly, only (b) P/M/STW+format+UI
    lacks counterparts; import fails loudly — P1-3); SPEC §3.10 gains
    graalgo.GoroutinePark (P2-3); residual rate-control risks documented
    (P2-4); jcmd recipe replaces non-existent `jfr start` (P2-1);
    measurement conditions recorded (P2-8).

**Open scheduler question (not a JFR defect, for the scheduler owner):**
reviewer2 observed a 5.6ms CHANNEL park ending +278ms with g2's send at
+1659ms — i.e. a drainer re-admission with no matching UNPARK in
flight. 30-min bounded investigation ruled out: handle-ID reuse (IDs are
unique), spurious LockSupport wakeup (awaitAck re-parks on acked < t),
timer broadcast (unpark is handle-specific). Mechanism not found
statically; may be 2-core timing-distortion artifact. The JFR hook
measures the true notePark→resume interval faithfully either way.

**Final verification (fresh on sign-off tree dfad56a, by me):**
phase-2 `make test` 158/158, `make check` green (placeholder, debug,
netpoller, off-heap gates), Phase-1 suite 208/208 (untouched files).

P2-M6 done. Workstream F complete → Phase-2 done-gate 3 met ("JFR
flowing": events observable on the conformance corpus; evidence
phases/phase2/hidden_files/jfr/EVENTS.md + p2-m6-evidence.jfr + counts).

## 2026-10-05 — P2-M7 pinning audit (in progress)

**Spike findings (pipeline validation):**
- GraalVM 25 = JDK 25: JEP 491 in effect — `synchronized` blocks/methods and
  `Object.wait()` NO LONGER pin virtual threads (verified: `-Djdk.tracePinnedThreads=full`
  silent on synchronized+park and pipe-read controls).
- `jdk.VirtualThreadPinned` fires only on park-while-pinned. Verified positive
  control: JNI `GetPrimitiveArrayCritical` + `LockSupport.parkNanos` → exactly 1
  event with full stack (`VirtualThread.postPinnedEvent` → `doPark` →
  `parkWhileCritical`). Controls that do NOT pin on JDK 25: synchronized+park,
  blocking `PipedInputStream.read`, 2s blocking FFM `sleep(3)` downcall.
- Codebase grep: zero `native` methods, zero `java.lang.foreign` usage in
  phase1+phase2 main sources → no JNI/FFM pinning surface on goroutine paths.
- All scheduler parks (`GoChan.send/recv`, `SelectOp`, `Timers`, `quantumPark`)
  occur outside monitors (static review).
- JFR plumbing: `-XX:StartFlightRecording:filename=x.jfr,jdk.VirtualThreadPinned#enabled=true,jdk.VirtualThreadPinned#stackTrace=true`
  (inline; custom .jfc silently enabled nothing — do not use bare .jfc).

## P2-M7 checkpoint (2026-10-05 ~18:20 UTC)
- PinAudit tool built, committed (a1c2673). Full corpus audit found 13 UNEXPECTED pins (lazy JFR event <clinit> on goroutine carriers).
- Root cause: ensureRegistered() used class literals (no <clinit> per JLS 12.4.1); FlightRecorder.register parks while pinned. Fixed (0dd2e2f): force <clinit> via enabled(), call in SchedulerCore ctor.
- JfrRegistrationTest red-green verified. Re-audit: 0 pins (186 programs). make check green with pin-surface gate + pin-audit smoke.
- Commits: a1c2673 (tool), 0dd2e2f (fix), b65fffc (test-cp), 0a7ef96 (make check wiring).
- Remaining: final make pin-audit (running), commit AUDIT.md, 2 reviews, final verify (phase2 test + phase1 suite + check), sign-off.

## P2-M7 SIGN-OFF (2026-10-05 ~19:30 UTC) — done-gate 2 MET

**Goal:** jdk.VirtualThreadPinned shows zero unexpected pins on the conformance corpus (GG-0057 falsifier: unexpected pin → red).

**Commits (8):** a1c2673 (PinAudit tool), 0dd2e2f (JFR <clinit> fix), b65fffc (test-cp), 0a7ef96 (make check wiring), 5b07719 (AUDIT.md), cf2e651 (JFR timing skip), 9828c4a (resilience+report), 6a0f694 (reviewer2), ecaabe0 (reviewer1).

**Evidence:**
- `make pin-audit`: 186 programs (148 phase-1 corpus + 37 phase-2 corpus + SchedTestRunner), 0 pin events. Report: hidden_files/pinning/pinaudit-report.md (AUDIT_RESULT=PASS).
- Honest coverage: 147 clean + 13 panic + 1 tests-leg with pin evidence; 26 build-failures (channels/timers frontend v1) contribute none.
- Finding: 13 pins from lazy JFR event <clinit> on carriers → fixed (force <clinit> on startup thread) → re-audit 0.
- cgo/FFM budget: +0 (no native/FFM on goroutine paths; grep gate in make check).

**Reviews:** 2 independent, both GO-WITH-FINDINGS, all findings remediated and re-tested.
- reviewer1-code.md: 1 P1 (vacuous PASS on --tests failure → fixed: fail closed), 7 P2 (all fixed).
- reviewer2-methodology.md: 2 P1 (intermittency documented, honest coverage accounting), 3 P2 (all addressed).

**Final verification (sign-off tree ecaabe0):**
- phase-2 `make test`: 163 tests, 0 failures.
- Phase-1 suite: 208 tests, 0 failures.
- `make check`: all gates clean, pin-audit smoke PASS (unexpected=0, unreadable=0).

**Done-gate 2: MET.** Zero unexpected pins. GG-0057 falsifier not triggered.
