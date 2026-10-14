# P2-M2 methodology/testing review — reviewer 2

**Snapshot:** `38179dc037de17d7e84fd8ebc8517675f3627cfb` (branch master, `~/workspace/graalgo`).
**Scope:** WS B (channels + select) — `ChannelTest.java` (20 tests),
`ChannelDifferentialTest.java` (10 tests), `phases/phase2/corpus/channels/`
(10 Go programs + `.expected` + README), CHECKPOINT.md P2-M2 tail.
**Mode:** read-only review. I ran the suite and `go run` reproductions myself;
I did not modify the repo and did not re-run the worker's mutations
(per brief — assessed from evidence + reasoning).

**Verdict: NO-GO** — two P0 findings. One is a proven, deterministic gc
divergence in select semantics (core WS B scope, untested); the other is a
false evidence claim in the sign-off checkpoint. Both have small, precisely
scoped fixes; this is a "fix and re-verify" NO-GO, not a structural rejection.

## Independent verification (all on the snapshot tree)

1. `make -C phases/phase2 test`: **45/45 green** (15 `SchedulerCoreTest` +
   20 `ChannelTest` + 10 `ChannelDifferentialTest`), exit 0. Claim "45/45
   green" checks out.
2. `make -C phases/phase2 check`: both grep gates clean. Claim checks out.
3. `git diff HEAD -- phases/phase2` empty; `git log --oneline -5` shows
   exactly the four P2-M2 commits (`89545d6`, `80f401e`, `ceb8d14`,
   `4d0b545`) plus the checkpoint commit `38179dc`. Commit claims check out.
4. `go run` (go1.27.1, `~/tools/go/bin`) reproduces **8/8** non-panic
   `.expected` files byte-for-byte. gc version used for recording is not
   documented anywhere (P2-8).
5. gc panic first lines: `panic: send on closed channel` /
   `panic: close of closed channel` — match the hardcoded strings in
   `testCh09`/`testCh10`. Message parity itself is real (first line only);
   see P0-1 for the evidence-form problem.
6. gc probes I ran for select/close interaction (see P0-2, P1-5):
   - Blocked `select { case ch <- 1: }`, channel closed while parked →
     **gc panics** `send on closed channel` (recovered in-probe).
   - Deterministic `close(ch)` then `select { case ch <- 1: default: }` →
     **gc panics** `send on closed channel`; default does NOT win.
   - `{poisoned send case, ready recv case}` x 200 runs → **109 panics /
     91 takes**: gc itself is nondeterministic here (random poll order).

---

## Findings

### P0-1 — Checkpoint's "10/10 byte-identical vs gc (incl. both panic messages)" is false

`phases/phase2/CHECKPOINT.md` (~15:30 entry) claims "10/10 corpus programs
byte-identical vs gc (incl. both panic messages)". Evidence:

- `ch09_send_on_closed.expected` and `ch10_close_of_closed.expected` are
  **0 bytes** (`od -c` shows empty; `wc -c` -> 0).
- `testCh09SendOnClosedPanic` / `testCh10CloseOfClosedPanic`
  (`ChannelDifferentialTest.java:209,233`) never read the `.expected`
  files. They build the expectation inline:
  `assertEquals("panic: send on closed channel\n", panicLine.get() + "\n", ...)`.
- So these two are hand-written unit assertions against a hardcoded string,
  not differential tests at all — and the corpus contains two empty files
  masquerading as gc recordings.

The underlying semantic claim (message parity) is true — I verified gc's
first panic line matches — but the *evidence* claim is false, and per the
Phase-1 lesson carried into PLAN.md section 4 ("every number in the entry
must cite its evidence file"), a sign-off may not rest on it.

**Fix:** correct the checkpoint to "8/10 byte-identical vs gc; 2 panic
programs assert first-line message parity (hand-asserted, gc first line
verified)". Either record gc's first panic line in the `.expected` files and
have the tests read them, or delete the empty files so nothing masquerades
as a recording. Also fix the corpus README, which implies all programs have
"gc-recorded `.expected` files".

### P0-2 — Select send-on-closed + default diverges from gc deterministically (untested)

`SelectOp.execute()`
(`phases/phase2/src/main/java/graalgo/runtime/SelectOp.java:129,148`):
a send case on a closed channel sets `poison = true` in the poll phase, but
if a `default` case exists the code returns `defaultIndex` and silently drops
the poison. The checkpoint documents this as deliberate: "default wins over
poison".

**gc does not do this.** Verified empirically (go1.27.1):

```go
ch := make(chan int)
close(ch)
select {
case ch <- 1:
    println("sent")
default:
    println("default")
}
// gc output: panic: send on closed channel
```

Default never gets a chance: gc panics in the poll phase. This is a
deterministic, in-scope semantic divergence — the exact class P2-M2's
"differential vs gc on channel semantics" acceptance exists to catch — and
no test covers it (`testSelectSendOnClosedPanics`,
`ChannelTest.java:510`, only covers the no-default shape).

**Fix:**

1. In the poll loop, a closed send case should panic immediately
   (`Panic.throwPanic("send on closed channel")` at `SelectOp.java:129`),
   before the default branch at line 148. This matches gc for every
   deterministic program shape.
2. Add a corpus program (e.g. `ch11_select_send_closed_default.go`,
   `.expected` = `panic: send on closed channel` first line per P0-1's
   convention) plus a unit test asserting the panic.
3. The multi-case race (`{poisoned send, ready recv}`: gc 109/200 panics,
   impl always takes the ready case) is genuinely nondeterministic in gc —
   record it on the P2-M10 divergence list with rationale and the measured
   numbers, rather than claiming parity.

Related: the async-poison path (close races a *parked* select ->
`resultPoison` -> panic on resume, `SelectOp.java:192`) **does** match gc —
my probe 1 shows gc panics a blocked select when its send case's channel is
closed while parked. See P1-5: the checkpoint's framing of this as a
"deviation risk" rests on a false premise.

### P1-3 — Red-green mutation evidence is unauditable and internally inconsistent

The checkpoint cites three mutations with exact failure lists but **no
file:line, no patch/diff, no location** — they cannot be independently
audited. Worse, under the plain reading of the descriptions, the reported
failure sets are too small:

- **Mutation A** ("send never parks/delivers" -> 2 failures:
  `testBufferedBlocksWhenFull`, `testCloseWakesSenderToPanic`). Under the
  plain reading (blocked sends never park and never deliver), these must
  *also* go red: `testUnbufferedSendRecv` (receiver parks forever ->
  quiescence timeout), `testPingPongStress` (same),
  `testBlockedSendDoesNotPinCarrier` (spawned `recv` parks forever),
  `testSelectSendHandoff` (plain receiver never woken), and the ch01/ch04
  differentials. I can construct a narrower mutation ("buffered-full send
  drops the value instead of parking") that yields exactly the 2 reported
  failures — but that is my reconstruction, not the worker's evidence.
- **Mutation B** ("no unpark on handoff" -> 2 failures:
  `testSelectSendHandoff`, `testPingPongStress`, "both as quiescence
  timeouts"). Under the plain reading (handoff dequeues a waiter but never
  unparks it), *every* cross-goroutine handoff test must time out:
  `testUnbufferedSendRecv`, `testBufferedBlocksWhenFull` (blocked sender
  never woken), `testBlockedSendDoesNotPinCarrier`, plus ch01/ch04
  differentials — roughly 7 failures, not 2. I could not reconstruct any
  single-line "no unpark" mutation that yields exactly the reported pair
  while keeping `testUnbufferedSendRecv` green.
- **Mutation C** ("carrier-blocking sleep instead of parkCurrent" -> 4
  failures incl. `testBlockedSendDoesNotPinCarrier`) is the one credible
  entry: the quoted failure text ("timeout waiting for: peer progress while
  sender parked") matches the test's `waitFor` message verbatim
  (`ChannelTest.java:75` + `:175`), and the mechanism (no PARKED token ->
  slot held at cap=1 -> peer never admitted) is exactly what the test was
  built to falsify.

**Fix:** re-run each mutation with the exact patch cited (file:line +
unified diff) and record the *complete* failure list, or downgrade the
checkpoint wording to "representative failures observed" and stop short of
exact-count claims. Exact failure lists that don't survive a plain-reading
reconstruction actively damage trust in the red-green discipline.

### P1-4 — ch08 differential translation is not 1:1 as claimed

The test javadoc (`ChannelDifferentialTest.java:19-38`) and the checkpoint
both claim the translations are "1:1 syntactic" (`ch <- 1` -> `send(ch, 1L)`
etc.). For ch08 this is not true:

- Go (`ch08_close_while_blocked.go`): `main` spawns one goroutine (recv +
  print + `done <- true`), then `main` itself does `close(ch)` and
  `<-done`.
- Java (`testCh08CloseWhileBlocked`): `main`'s role is split into a
  *second* spawned goroutine (`close(ch)` + `recv(done)`); the test body
  relies on quiescence instead of the `done` handshake.

The assertion is order-independent (parked-then-closed and
closed-then-parked both observe `0 false` — the in-test comment says so),
so no runtime bug is masked *here*. But the "1:1 syntactic" claim is
inaccurate, and the methodology's whole point is that translation shape is
boring so the *runtime* is what's under test. A structural rewrite in the
one test exercising the close/park race is exactly where a translation bug
could hide a runtime bug next time.

**Fix:** correct the javadoc to describe ch08's actual translation (or
restructure the Java to keep `main`'s role: close on the test thread after a
deterministic park-gate, then `recv(done)` on the test thread — matching
the Go program's shape).

### P1-5 — Checkpoint's "deviation risk" note rests on a false premise

Design note in CHECKPOINT.md: "close() wakes select send waiters with poison
-> panic on resume (documented deviation risk: gc never async-panics a
blocked select; only triggers in a racy interleave...)".

The premise is empirically false. My gc probe: a goroutine blocked in
`select { case ch <- 1: }`, channel closed 200ms later while parked -> **gc
panics** `send on closed channel` (caught via deferred recover in-probe).
The implementation's poison-on-resume (`SelectOp.java:192`) therefore
*matches* gc — it is parity, not a deviation, and the note should say so.

What *is* genuinely racy (and belongs on the P2-M10 divergence list, not in
a "deviation risk" note): when close races a parked select that has *other*
cases becoming ready concurrently, the impl's sticky `resultPoison` always
panics while gc re-polls and may take the ready case. That is a real,
narrow, racy divergence — document it as such with the measured gc behavior
(109/200 panics in the analogous poll race).

**Fix:** rewrite the design note: async-poison == gc parity (cite probe);
move the racy multi-case shape to the divergence list.

### P1-6 — Sleep-gated negative assertions are vacuous under slowness

Two tests gate a *negative* assertion on a fixed `Thread.sleep(200)`:

- `testBufferedBlocksWhenFull` (`ChannelTest.java:128`): `waitFor` buffer-full
  (deterministic), then `sleep(200)`, then `assertTrue(!thirdSent.get(),
  "sender must be parked on full buffer")`.
- `testSelectBlocksUntilReady` (`ChannelTest.java:377`): `sleep(200)`, then
  `assertTrue(!selected.get(), "select must block with no ready case")`.

On a slow/loaded machine the 200ms can elapse before the goroutine even
*attempts* the blocking op: the negative assertion then passes vacuously —
the test degrades to not testing the "blocks" property at all. This is a
**false-pass** risk (worse than flaky-failure: silent coverage loss), not a
flaky-failure risk. The subsequent `waitFor(...to complete...)` still
exercises the wakeup path, so the tests aren't fully vacuous — but the
"does it actually block" half is.

The other sleeps (`testCloseWakesReceiver`, `testCloseWakesSenderToPanic`,
`testSelectSendHandoff`, `testSelectWakesOnClose`,
`testSelectCloseWhileParked`) I verified are non-load-bearing courtesies:
close-then-park and park-then-close both satisfy the assertions, so the
tests are order-independent with or without the sleep.

**Fix:** add package-private test probes — e.g.
`GoChan.sendWaiterCount()` / `recvWaiterCount()` (queue depths under the
monitor) and a `SelectOp` parked-case count — and replace the fixed sleeps
with `waitFor(() -> ch.sendWaiterCount() == 1, ...)`. That deterministically
establishes "the goroutine is parked" before asserting the negative, on any
machine speed.

### P2-7 — Missing coverage a reasonable reviewer would demand

None of these failed (nothing tests them); listed for the P2-M10
remediation pass or explicit out-of-scope rulings:

1. **Select send-on-closed with default** — now P0-2 (proven divergence).
2. **Select with the same channel in two cases**
   (`select { case v := <-ch: ...; case v := <-ch: ... }`) — exercises
   duplicate-case enqueue/cleanup paths in `SelectOp` (two waiters, same
   channel, shared claim). Untested.
3. **Buffered channel, select send case when full** — send case not ready ->
   default/other case must be chosen; `canSend()` false-path. Untested
   (ch07 only covers send-with-room).
4. **Close with multiple parked receivers** — broadcast: all must observe
   `(zero, false)`. `close()` iterates `recvWaiters`; only single-receiver
   tested (`testCloseWakesReceiver`, ch08).
5. **Close with a parked sender *and* a parked receiver simultaneously** —
   mixed waiter queues on close. Untested.
6. **Select poll over an already-closed channel** (recv case) — must
   complete immediately with `(zero, false)` via the poll fast path, never
   park. `testSelectWakesOnClose`/`testSelectCloseWhileParked` only cover
   close-*while*-parked.
7. **Many-to-many stress** — `testPingPongStress` is many-senders/one-receiver;
   add N senders x M receivers with an exact accounting check (sum/count),
   which stresses `pollAccepting`'s claimed-waiter purge under contention.
8. **Nil channels** — `send`/`recv` on nil must block forever; `close(nil)`
   must panic. Current impl would NPE (`requireGoChan(null)`). If nil
   channels are out of scope for P2-M2, say so explicitly; otherwise test.
9. **Fairness beyond 2 cases + wake-path fairness** — `testSelectFairness`
   covers the poll-path random choice with 2 cases only; the blocked-park
   wake path (several selects parked on one channel, FIFO wake order per
   Go's sudog queues) has no fairness test.
10. **`len()` with parked waiters** — Go's `len` excludes waiters;
    `testLenCap` never parks. One parked-sender + `len` assertion is cheap.
11. **Non-`Long` zero value** — the 3-arg `makeChan` overload's `zeroValue`
    is only ever exercised with `0L`; a distinct zero (e.g. a string or a
    boxed sentinel) would catch a hardcoded-zero bug in `tryRecv`/close
    paths.

### P2-8 — `.expected` generation is not scripted or versioned

README says "gc-recorded `.expected` files" but there is no generation
script and no recorded gc version. I reproduced 8/8 with go1.27.1, so the
files are good — but reproducibility currently depends on my ad-hoc check,
not on a repo artifact.

**Fix:** add `corpus/channels/regen.sh` (loop `go run` -> `.expected`, with
`go version` stamped into a header comment or a `RECORDED_WITH` file).
Panic programs: record gc's *first* stdout/stderr line per the P0-1
convention.

### P2-9 — `ChannelDifferentialTest` lacks `verifies: GG-0017` annotations

`ChannelTest.java` carries 21 `verifies: GG-0017` markers; the differential
file carries 0, though its 10 tests are equally "first real tests" of
GG-0017 chan-slice semantics.

**GG-0017 activation assessment (brief item 6):** activation itself is
**valid**. PLAN.md's policy ("→active exactly when its first real test
lands with a `verifies: GG-XXXX` annotation") is satisfied: P2-M1 activated
only the *core* slice (crash discipline), and `ChannelTest` is the first
real channel test, carrying the markers in the project's established
`verifies:`-comment form (same form Phase-1 used, e.g.
`// verifies: GG-0027` in `MainTest.java`). **Fix:** add the markers to the
10 differential tests for consistency, so a future grep for "what tests
verify GG-0017" doesn't miss half the suite.

### P2-10 — Confusing comment on `testCh06SelectDefault`

`ChannelDifferentialTest.java` (~line 168): "gc prints just \"default\";
our translation prints the branch name. Normalize: the Go program prints
\"recv \<v\>\" or \"default\"." — but the committed `.go` program itself
prints the branch names (`fmt.Println("recv", v)` / `fmt.Println("default")`),
so no normalization happens anywhere; the comment describes a non-issue and
will confuse the next reader about which side was adjusted.

**Fix:** reword to state the actual convention (branch-name printing chosen
in the Go source so the translation is 1:1).

---

## Coverage matrix (PLAN.md section 4 P2-M2 acceptance)

Acceptance: *"channels + select (WS B) — differential vs gc on channel
semantics corpus incl. close-while-parked and select-fairness (GG-0017
chan→active)"*, decomposed:

| # | Acceptance sub-criterion | Test(s) | Assessment |
|---|---|---|---|
| 1 | Unbuffered send/recv rendezvous | `testUnbufferedSendRecv` (C:92); `testCh01Unbuffered` + `ch01.expected` byte-identical | PASS — covered unit + differential |
| 2 | Buffered FIFO order | `testBufferedFifo` (C:108); `testCh02Buffered` byte-identical | PASS |
| 3 | Buffered sender blocks when full; recv unblocks | `testBufferedBlocksWhenFull` (C:128) | WEAK — covered but the "blocks" half is sleep-gated/vacuous under slowness (P1-6); no differential program |
| 4 | Blocked send does not pin carrier (P2-D4) | `testBlockedSendDoesNotPinCarrier` (C:158) | PASS — genuine falsifier (see below) |
| 5 | Close wakes parked receiver with (zero,false) | `testCloseWakesReceiver` (C:186); `testCh08CloseWhileBlocked` byte-identical | PASS — but ch08 translation not 1:1 (P1-4) |
| 6 | Close wakes parked sender -> panic "send on closed channel" | `testCloseWakesSenderToPanic` (C:202) | PASS unit; NO differential program for parked-sender close |
| 7 | Send on closed -> panic | `testSendOnClosedPanics` (C:226); `testCh09...` hand-asserted (P0-1) | PARTIAL — message parity real, differential form false |
| 8 | Close of closed -> panic | `testCloseOfClosedPanics` (C:246); `testCh10...` hand-asserted (P0-1) | PARTIAL — same as 7 |
| 9 | Recv drains buffer after close, then (zero,false) forever | `testRecvOnClosedDrainsThenZero` (C:267); `testCh03CloseDrain` byte-identical | PASS |
| 10 | len/cap | `testLenCap` (C:297) | PASS — no parked-waiter case (P2-7.10) |
| 11 | Select: ready recv case chosen, value delivered | `testSelectRecvChoosesReady` (C:318); `testCh05SelectRecv` byte-identical | PASS |
| 12 | Select default (nothing ready; ready beats default) | `testSelectDefault` (C:345), `testSelectDefaultLosesToReady` (C:360); `testCh06SelectDefault`, `testCh07SelectSend` byte-identical | PASS for the tested shapes |
| 13 | Select send-on-closed panics | `testSelectSendOnClosedPanics` (C:510) — no-default shape only | **FAIL — P0-2**: with-default shape diverges from gc, untested |
| 14 | Select blocks until a case is ready (scheduler-aware park) | `testSelectBlocksUntilReady` (C:377) | WEAK — "blocks" half sleep-gated (P1-6) |
| 15 | Select fairness (no starvation) | `testSelectFairness` (C:404) | PASS — genuine statistical fairness test (see below); 2-case poll-path only (P2-7.9) |
| 16 | Select send case hands off to parked receiver | `testSelectSendHandoff` (C:438); `testCh07SelectSend` byte-identical | PASS |
| 17 | Select parked on recv wakes on close with (zero,false) | `testSelectWakesOnClose` (C:459), `testSelectCloseWhileParked` (C:485) | PASS |
| 18 | Range-over-channel drain | `testCh04ProducerConsumer` byte-identical | PASS differential; no Java unit equivalent (fine — differential covers it) |
| 19 | Wakeup accounting under contention (no lost/duplicated values) | `testPingPongStress` (C:537) | PASS — 20x50 exact sum/count |
| 20 | GG-0017 chan slice activation | `verifies: GG-0017` in `ChannelTest` | PASS — policy satisfied (P2-9 nit on the differential file) |

(C:nnn = `ChannelTest.java` line.)

**Nothing in the acceptance line is fully untested** except via the P0-2
hole (select/close/default interaction) — but rows 3, 7, 8, 14 are
WEAK/PARTIAL for the reasons given.

---

## Non-tautological arguments (brief item 2, concrete)

**`testBlockedSendDoesNotPinCarrier` (C:158) is non-tautological.**
Mechanism: `GoChan.send` parks via `SchedulerCore.parkCurrent`, which
enqueues a PARKED token; the drainer (`Drainer.java:125`) processes PARKED
by `inflight--`, freeing the slot. At `GOMAXPROCS=1`, if `send` instead
blocked the carrier thread (raw Java block — the P2-D4 violation), no
PARKED token is ever emitted, `inflight` stays 1, the drainer's
`admit()` loop (`inflight < cap`) never admits the peer, and
`waitFor(peerRan::get)` fails after 10s with "timeout waiting for: peer
progress while sender parked". The test also fails for a subtler
variant — parking via bare `LockSupport.park()` without the drainer
protocol (carrier released by Loom, but slot still held). The *only*
implementations that pass are those that release the in-flight slot on
block, which is exactly P2-D4's content. (Corroboration: the worker's
mutation C — carrier-blocking sleep — failed this test with precisely the
predicted timeout message.)

**`testSelectFairness` (C:404) is non-tautological.** 2000 iterations, both
cases always ready, assertion `counts[i] > 600` each. Under a first-case-
biased select (the natural broken implementation: `ready.get(0)` instead
of random choice), `counts[1] = 0` and the test fails deterministically.
Under the implemented uniform-random choice, each count is
Binomial(2000, 0.5): mean 1000, sd ~22.4 — the 600 threshold is ~18 sd below
the mean, i.e. a false failure is computationally impossible while a
starving implementation fails with certainty. It tests *fairness*, not just
"select returns something" (a pure smoke test would pass with
`ready.get(0)`).

---

## What checks out (for the record)

- Suite, `make check`, commit hashes, test counts (15/20/10): all verified
  independently on the snapshot tree.
- 8/8 non-panic `.expected` files reproduce via `go run` (go1.27.1).
- Panic first-line messages match gc.
- `testBlockedSendDoesNotPinCarrier` genuinely falsifies P2-D4 (drainer
  mechanics verified in code).
- `testSelectFairness` is a real fairness test, not random-choice smoke.
- GG-0017 chan-slice activation follows the plan's policy.
- Lost-wakeup discipline in the tests (quiescence timeouts as loud
  failures rather than silent hangs) is good practice and consistent.

## Remediation checklist for re-review

1. [ ] P0-2: panic on poll-phase select poison (before default) +
   `ch11` corpus program + unit test; divergence-list entry for the racy
   multi-case shape with the 109/200 gc measurement.
2. [ ] P0-1: correct checkpoint/README differential claims; fix or remove
   the empty ch09/ch10 `.expected` files.
3. [ ] P1-3: re-run mutations with exact patch citations + complete failure
   lists (or soften the claims).
4. [ ] P1-4: fix ch08 javadoc/translation-shape claim.
5. [ ] P1-5: correct the "gc never async-panics" premise; move the racy
   shape to the divergence list.
6. [ ] P1-6: waiter-count probes; replace load-bearing sleeps with
   `waitFor` on parked-waiter counts.
7. [ ] P2-7/8/9/10: add the missing tests (or explicit out-of-scope
   rulings), `regen.sh` + recorded gc version, `verifies:` markers in the
   differential file, ch06 comment reword.
8. [ ] Re-run full suite + `make check` on the remediated tree; supervisor
   independently re-runs before sign-off (standing P2-M1 lesson).

**Bottom line:** the runtime core (GoChan/SelectOp park/unpark/close
mechanics) is well-designed and well-tested — the P2-D4 falsifier and the
fairness test are genuinely strong. The NO-GO rests on one real semantic
bug (P0-2), one false evidence claim (P0-1), and mutation evidence that
cannot be audited (P1-3). All are days-scale fixes, not redesigns.
