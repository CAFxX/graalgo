# P2-M2 code review — reviewer1 (channels + select)

- **Snapshot:** `38179dc` (branch master, `~/workspace/graalgo`); diff `c5066cd..HEAD`.
- **Scope:** `GoChan.java`, `SelectOp.java`, `ChanOps.java` (phase2 overlay),
  `GoStrings.java`, `SchedulerCore.currentGoroutine()` (additive).
- **Method:** read-only review. No source files modified. Interleavings traced
  by hand against the P2-M1 drainer protocol (`Drainer.route`: `parked` map +
  `unparkPending` set, ack-before-unpark); gc semantics cross-checked
  empirically with Go 1.27.1 (`~/tools/go-1.27.1/bin/go`); one targeted stress
  experiment in `/tmp/raceprobe` (not in the repo) to test the suspected claim
  race. The repo test suite was not re-run as primary evidence.

## Verdict: NO-GO

One P0 correctness bug (silent corruption of *correct* programs under
contention), **reproduced empirically 3/3 runs**, plus three deterministic
Go-semantic deviations (P1). The P0 alone blocks sign-off; the P1s should be
fixed in the same pass since two of them are mis-documented in CHECKPOINT.md
as deliberate decisions.

## Findings

### P0-1 — Select's block-phase immediate completion races the shared claim CAS: double completion + stale UNPARK → silent corruption

**File:** `phases/phase2/src/main/java/graalgo/runtime/SelectOp.java:158-196`
(the block-phase loop; `completeUnderLock` at :173, `immediate` at :175).

**The bug.** In the block phase, the loop enqueues a `SelectCaseWaiter` per
channel and, under each channel's monitor, re-checks via `completeUnderLock`
— if the case became ready in the poll→block gap it completes *immediately*
without ever claiming the `SelectOp`. But the claim CAS is the only thing
that serializes the selecting goroutine against concurrent wakers on the
*earlier-enqueued* channels (different monitors, so no mutual exclusion).
Two broken interleavings (goroutine G selects recv over `[chA, chB]`; `W_A`
enqueued on chA; sender `S_A` active on chA; chB becomes ready):

- (i) `S_A` claims `W_A` *before* G's chB completion: `setResult(0, vA)` then
  G's `completeUnderLock(chB)` overwrites with `setResult(1, vB)`. G returns
  chB's index with chB's value — consistent-looking — **but `S_A`'s UNPARK was
  already offered and is now stale** (`unparkPending[G]` in the drainer, never
  consumed). G's *next* park of any kind spuriously resumes immediately.
- (ii) `S_A` claims *after* G's chB completion (before `cleanup` removes
  `W_A`): `setResult(0, vA)` overwrites G's `(1, vB)`; G returns chB's index
  with **chA's value**, plus the same stale UNPARK.

A stale UNPARK poisons the next park: `send()` returns without delivering,
`recv()` returns a bogus `(null, false)` as if the channel were closed,
`selectGo()` returns `-1`.

**Empirical reproduction (3/3 runs, first attempt each):** `/tmp/raceprobe/RaceProbe4.java`
(selector over `[chA, 20000 dummies, chB]`; helper releases a pre-spawned
sender the moment `W_A` appears via reflection, then fills chB). Output:

```
attempt 0: BUG: STALE-DRAIN attempt 0: drain got null ok=false
```

i.e. select#1 returned chB's index with chB's value, and the immediately
following plain `recv` on the *non-closed* chB — which held exactly one
buffered value and must return it without parking — returned `(null, false)`:
the spurious resume from case (i). The existing 45 tests cannot catch this;
neither can the documented mutations (it is an *extra* unpark, not a missing
one — Mutation B only deletes unparks).

**Suggested fix.** Claim-before-complete, per iteration, inside the monitor
hold. Add e.g. `boolean tryClaimSelf()` (CAS `UNCLAIMED → SELF` sentinel) and
restructure the block-phase loop body to:

```java
synchronized (ch) {
    if (!(c.send() && ch.isClosed())) {
        if (!tryClaimSelf()) {
            break; // another channel won; its unpark is in flight -> park below
        }
        Integer doneNow = completeUnderLock(ch, i, sched);
        if (doneNow != null) {
            immediate = doneNow;   // claim held: concurrent accept() fails, no overwrite, no unpark
        } else {
            releaseClaim();        // CAS SELF -> UNCLAIMED, still under ch's monitor
            SelectCaseWaiter w = new SelectCaseWaiter(...);
            ch.enqueueSelectWaiter(w, c.send());
            mine.add(w);
        }
    } else {
        poison = true;
    }
}
```

Why this shape: (a) a waker that already won makes `tryClaimSelf` fail, so we
never run `completeUnderLock` after losing — no double completion (a post-hoc
CAS *after* completing would still let both a send-case handoff and the
winner's case complete); (b) while we hold `SELF`, concurrent `accept()` on
earlier-enqueued waiters fails, so no `setResult` overwrite and no UNPARK;
(c) on the park path the claim is released before parking, so `close()` and
peer wakers claim normally and their UNPARK is absorbed by the drainer in
either order. If `tryClaimSelf` fails with `mine` empty (unreachable for a
fresh op — fail loud, never park without an in-flight unpark).

**Suggested regression test:** adapt the RaceProbe4 scenario as a unit test
(pre-spawned sender + helper, channel-count sized so the window is reliable;
assert the drain `recv` returns the buffered value and a second `selectGo`
never returns `-1`). Design it to *retry* on inconclusive timing and only fail
on observed corruption, or gate it as a stress test.

### P1-1 — `select` with send-on-closed + `default` takes default; gc panics

**File:** `SelectOp.java:118` (`poison` noted), `:155-157` (`defaultIndex`
returned unconditionally).

Verified against gc (Go 1.27.1):

```go
ch := make(chan int); close(ch)
select { case ch <- 1: println("sent"); default: println("default") }
// gc: panic: send on closed channel   (always; selectnbsend path)
```

The implementation returns the default index. CHECKPOINT.md documents "default
wins over poison" as a decision — it is wrong vs gc, deterministically.
**Fix:** in the poll phase, `if (poison && defaultIndex >= 0) panic("send on closed channel")`
before returning default. (Note: poison+*ready* without default is genuinely
racy in gc — measured 4× `recv 7` / 2× panic over 6 runs — so completing the
ready case there is within gc's observable envelope and is *not* a finding.)

### P1-2 — `select` with send-on-closed + other *blocked* cases blocks forever; gc panics immediately

**File:** `SelectOp.java:158-196` (block phase); the `poison` variable at
`:118` is **written but never read** — the poison design is unfinished; the
`mine.isEmpty()` panic at `:191-194` fires for the wrong reason (see P1-3).

Verified against gc:

```go
ch := make(chan int); close(ch); open := make(chan int)
select { case ch <- 1: println("sent"); case v := <-open: println("recv", v) }
// gc: panic: send on closed channel   (immediate, deterministic)
```

The implementation enqueues on `open` and parks indefinitely. **Fix:** in the
block phase, if `poison` is set, panic `"send on closed channel"` instead of
parking (deterministic, matches gc). The close-during-blocked-select poison
path (`setPoison` → panic on resume) is *acceptable*: gc also panics the
selecting goroutine in that scenario (via re-poll discovering the closed send
case); the implementation's deterministic panic is inside gc's racy envelope.
Keep it, but fix the checkpoint wording ("gc never async-panics" is
imprecise — gc panics synchronously after wakeup).

### P1-3 — `select {}` panics with `"send on closed channel"` instead of blocking forever

**File:** `SelectOp.java:191-194`. Zero cases → poll finds nothing → block
phase enqueues nothing → `mine.isEmpty()` → `Panic.throwPanic("send on closed
channel")`. In Go, `select {}` blocks forever; the panic message is doubly
wrong. **Fix:** if `cases.length == 0`, park without waiters (blocks until
quiescence timeout/deadlock handling, never a bogus panic).

### P2-1 — `takeFromSender` leaves a stale reference in the consumed buffer slot

**File:** `GoChan.java:260-276` (line `:267`: `v = buf[head];` is never
nulled, unlike `tryRecv` at `:212` which does `buf[head] = null`).

On the buffered-refill path the old head slot keeps referencing the dequeued
value until the ring wraps around — pins objects up to `capacity` long after
they were received. **Fix:** `buf[head] = null;` after reading (safe also for
the `cap==1` aliasing case, where the slot is immediately overwritten by
`buf[tail] = sv`).

### P2-2 — Negative-capacity panic message differs from gc

**File:** `ChanOps.java:51`: `"makechan: negative capacity"`; gc (runtime
value, verified): `panic: makechan: size out of range`. **Fix:** use gc's
message. (Companion: enormous capacities hit `new Object[capacity]` →
`OutOfMemoryError` instead of a Go panic; clamp to a Go panic for parity.)

### P2-3 — Nil-channel operations diverge from gc

`requireGoChan(null)` throws `IllegalArgumentException`, but gc defines:
`close(nil)` → `panic: close of nil channel`; `send`/`recv` on nil →
**block forever**; `len(nil)`/`cap(nil)` → `0`. A nil `Chan` cannot arise from
`makeChan`, so this is an edge, but the failure modes should match gc rather
than leak a Java exception type. (Related: a `SelectCase` with a nil non-default
channel — gc blocks that case forever; here the `chan == null` convention
reserves null for `default`, so it is inexpressible. Document the limitation
for the exporter.)

### P2-4 — Block-phase immediate completion is index-ordered, not shuffled

Poll-phase choice among ready cases is uniform (`ThreadLocalRandom`, verified
by `testSelectFairness`), but the block-phase re-check loop
(`SelectOp.java:158`) completes the *first* index-ordered case that became
ready in the poll→block gap. Under contention, low-index cases are
systematically preferred in exactly the window where gc shuffles case order
(`order[]` via fastrand). **Fix:** iterate the block-phase channels in random
order (or collect immediately-completable cases and pick uniformly).

### P2-5 — No deadlock detection

gc: `fatal error: all goroutines are asleep - deadlock!` (exit 2). Here a
deadlocked program parks forever; only test harnesses notice (quiescence
timeout). Probably a later milestone, but record it — silent hangs are the
failure mode the scheduler work has been careful to avoid elsewhere.

### P2-6 — `selectGo(null)` → NPE; duplicate `default` cases silently last-wins

Defensive notes for the exporter contract: null array should fail loud with a
clear message; gc rejects multiple defaults at compile time — document that
the runtime takes the last one.

## Interleavings traced (lost-wakeup analysis)

Park sites: `GoChan.send:301`, `GoChan.recv:325`, `SelectOp.execute:203`.
Unpark sites (all under the channel monitor; `Drainer.offer` is a
non-blocking unbounded `LinkedBlockingQueue.offer`, so unpark-under-monitor
cannot deadlock): `deliverToReceiver` ×2 (`GoChan:243,247`),
`takeFromSender` (`GoChan:275`), `close()` ×4 (`GoChan:347,352,359,368`).

For every park/unpark pair I traced three orders — (a) waker fully before
waiter enqueues (waker sees no waiter; waiter enqueues and parks; a later
waker finds it), (b) waiter enqueues then waker dequeues+unparks before the
waiter parks (UNPARK first → `unparkPending`, PARKED → immediate WAKE
admission), (c) waiter parks first (PARKED → `parked` map, UNPARK → WAKE
lane) — plus the select poll→block gap, which is closed by re-checking
(`completeUnderLock`) under each channel's monitor before enqueueing, and the
`close()` sweep, which runs atomically under the monitor (a waiter that
checked `closed==false` cannot miss a concurrent close: enqueue and the
closed-check are one critical section).

**Conclusion: no lost wakeup on any park path** — *except* via P0-1, which is
not a lost wakeup but its evil twin: a *lost claim* producing a *spurious*
wakeup. The drainer protocol itself is sound for the orders above; the bug is
that `SelectOp` lets a completion happen without holding the claim, so a
loser's UNPARK outlives the operation and fires during an unrelated later
park. Plain `send`/`recv` are single-phase (enqueue→park, no second decision
point) and are not affected.

Lock discipline verified: park is never called with the channel monitor held
(`send:301`, `recv:325`, `SelectOp:203` are all outside `synchronized`);
unpark is always under the monitor but never blocks; select `cleanup`
acquires one monitor at a time (no lock ordering issue); the claim CAS is the
sole cross-channel synchronizer and is only ever CAS-won once per op, always
followed by `unpark` of the same handle at all four win sites.

## Verified OK (no finding)

1. **Frozen signatures byte-identical.** All 7 phase-1 methods
   (`makeChan(long,int)`, `send`, `recv`, `close`, `len`, `cap`,
   `selectGo(SelectCase[])`), the `Chan` interface, and the
   `SelectCase(Chan, boolean, Object)` record match name/params/return-type
   exactly (source-level diff of both `ChanOps.java`). Additive
   `makeChan(long,int,Object)`, `recvOk`, `selectRecvValue/Ok` don't alter the
   freeze.
2. **Panic messages byte-identical to gc's first stderr line:**
   `panic: send on closed channel`, `panic: close of closed channel`
   (`GoString.toString()` renders raw UTF-8; differential `ch09`/`ch10`
   confirm).
3. **Close semantics:** send-on-closed panic, double-close panic, recv
   drain-then-`(zero,false)`, close wakes recv waiters with `(zero,false)`
   even with buffered data (invariant `recvWaiters ≠ ∅ ⇒ count == 0` holds,
   matching `chan.go`), parked senders panic via `panicOnWake`.
4. **Buffered handoff with waiting sender** refills the freed slot exactly
   like `chan.go` (`takeFromSender`: head out, sender value to tail, count
   unchanged); direct sender→receiver handoff preferred over buffering, as
   in gc.
5. **`SchedulerCore.currentGoroutine()`** is a thin `requireHandle()`
   exposure; no behavior change.
6. **Fairness (poll path):** uniform random among ready cases; FIFO waiter
   queues; no starvation path found.
7. **No resource leaks:** `SELECT_RESULT` ThreadLocal is overwritten per
   `selectGo` (vthreads aren't pooled); buffer slots nulled on dequeue except
   P2-1; select `cleanup` removes non-winning entries (also tolerates already-
   dequeued entries).
8. **Poison+ready (no default) completing the ready case** is within gc's
   measured nondeterminism — not flagged. **Close-poisons-blocked-select-send**
   (deterministic panic on resume) is within gc's envelope — acceptable, but
   the checkpoint should describe gc's actual behavior (panic-on-re-poll,
   racy if another case is ready) rather than "gc never async-panics".

## Test/code-review notes

- The 20 unit tests + 10 differential tests cover the happy paths well, but
  nothing exercises: immediate-completion racing a concurrent claim (P0-1),
  poison+default (P1-1), poison+blocked-case (P1-2), `select{}` (P1-3).
  Recommend adding corpus programs `ch11_select_send_closed_default.go` and
  `ch12_select_send_closed_blocked.go` with gc-recorded `.expected` (both
  panic), plus the P0-1 regression test sketched above.
- The dead `poison` variable (`SelectOp.java:118`, written never read) should
  have been caught by review — it marks exactly where P1-1/P1-2 went wrong.

## Experiment artifacts (not in repo)

- `/tmp/raceprobe/RaceProbe4.java` — P0-1 reproducer (3/3 hits, attempt 0).
  Suggest porting to `ChannelTest` (fewer dummies + retry-on-inconclusive).
- gc probes in `/tmp/gcprobe` (`p1/p2/p5/p7/p8.go`) — basis for the P1-1,
  P1-2, P2-2, P2-3 claims above.
