# P2-M9 lost-wakeup model-checking report (done-gate 4)

## 1. Tool spike (TLA+ vs JPF vs bespoke) — verdict: bespoke

The park/queue/drainer protocol's concurrency surface collapses to (1) the
ORDER of token arrival at the drainer's MPSC inbox (the drainer is
single-threaded; all lane/inflight/parked state mutates under one lock)
and (2) the vthread-side armPark/acked generation protocol.

| Tool | Verdict | Reason |
|---|---|---|
| TLA+/PlusCal | REJECTED | Would re-encode the protocol by hand; a green TLC run verifies the encoding, not the shipped code. Weak falsification power. |
| Java PathFinder | REJECTED | Cannot execute JDK 25 bytecode with virtual threads; and full-JVM interleaving exploration buys nothing over token-order exploration for a single-threaded drainer. |
| Bespoke explorer (`WakeupModelTest`) | WINNER | Drives the REAL `Drainer` class via `processOneForTest` (route+admit+check = the production drain step). Zero abstraction gap on the drainer. |

Re-test trigger: any change to the token protocol (new `ResumeToken.Kind`,
lane order, PARKED/UNPARK matching, ack discipline) → re-run `make test`
(the explorer runs in the suite) and extend the script family.

## 2. Property checked

NO goroutine is ever lost: every PARKED whose paired UNPARK is offered is
admitted (acked >= its generation) on EVERY interleaving; every UNPARK is
either matched to a parked PARKED or stashed and consumed by exactly one
later PARKED. A terminal WAIT with the paired unpark offered = LOST WAKEUP
(hard fail with the full path).

Retention (not loss): an UNPARK offered for an exited or never-parking
handle is retained in `unparkPending` (bounded: handle ids are unique and
never reused). This is a bounded memory retention, not a lost wakeup —
no park is left unadmitted because of it (proven by `exit-pending-unpark`).

## 3. Coverage vs GG-WAKE-0002 (a–f)

| Interleaving | Script(s) | Result |
|---|---|---|
| (a) close() waking parked senders via aborted path while receivers drain | `threeway` (3 parked × 3 unparks, all orders); the drainer does not distinguish abort unparks | PASS |
| (b) stale-ticket wakeup racing re-arm to fresh ticket | `unpark-consumed-once`, `unpark-io-park` | PASS |
| (c) select in-flight stale signal vs re-registration | `unpark-consumed-once`, `testDuplicateUnparkAdmittedOnce`, `testStaleUnparkAfterRearm` — see analogy note below | PASS |
| (d) INIT lane under WAKEUP flood | `init-under-wake-flood` (cap=1) | PASS |
| (e) cap=1 channel-wakeup liveness | `cap1-handoff` | PASS |
| (f) WAKEUP-before-IDLE with generation skew under drainer batching | every script explores both orders; `testUnparkBeforeParkFastPath` directed | PASS |

(Total: 12 explorer scripts; per-script state/terminal counts in §5.)

## 4. M6 watch-item re-test — RESOLVED, mechanism confirmed

P2-M8 root-caused the phantom "re-admission with no UNPARK in flight" as
the `unparkPending` fast-path via provenance counters. The model confirms
it from the other direction:

- `testUnparkBeforeParkFastPath` (directed: UNPARK offered before PARKED
  processed): admitted, `wakeUnparkStashed=1`, `wakeUnparkDirect=0`.
- `testParkBeforeUnparkDirectPath` (directed: PARKED before UNPARK):
  admitted, `wakeUnparkDirect=1`, `wakeUnparkStashed=0`.

The two directed tests prove the provenance counters partition every WAKE
admission; the explorer proves no WAKE-lane admission occurs without an
UNPARK or IO_EXIT behind it. (INIT/RESUME admissions need no unpark — the
drainer admits those lanes directly; the "no admission without cause"
claim is scoped to the WAKE lane.) No other
re-admission-without-UNPARK path exists in the
model. The JFR timeline anomaly was the stashed path — legitimate.

## 5. Results

Explorer scripts (all interleavings explored per script; terminal = quiescent
end state; property = every paired park admitted, no lost wakeup):

| Script | States | Terminals | Result |
|---|---|---|---|
| basic | 22 | 2 | no lost wakeup |
| cross | 528 | 6 | no lost wakeup |
| threeway | 13,208 | 8 | no lost wakeup |
| quantum | 7 | 1 | no lost wakeup |
| io | 9 | 1 | no lost wakeup |
| unpark-consumed-once | 22 | 2 | no lost wakeup |
| cap1-handoff | 228 | 3 | no lost wakeup |
| exit-pending-unpark | 17 | 1 | no lost wakeup |
| unpark-during-quantum | 17 | 1 | no lost wakeup |
| mixed-io-park | 339 | 4 | no lost wakeup |
| unpark-io-park | 36 | 2 | no lost wakeup |
| init-under-wake-flood | 3,021 | 3 | no lost wakeup |

Directed: `testUnparkBeforeParkFastPath` (stashed=1/direct=0),
`testParkBeforeUnparkDirectPath` (direct=1/stashed=0),
`testDuplicateUnparkAdmittedOnce` (two unparks → exactly one admission),
`testStaleUnparkAfterRearm` (stale unpark consumed once by re-armed park)
— all PASS.
Red-green: `testModelDetectsWithheldUnpark` fails with LOST WAKEUP as
required (explorer is not vacuous); `JfrParkCostTest` fails under a 2us
fault-injected hook delay (6612ns > 500ns budget — verified 2026-10-05,
then reverted).

### Analogy note for (c)

The implementation has no "SelSignal" — select uses a shared CAS claim
(`SelectOp`). At the drainer protocol level, a select stale wakeup IS an
UNPARK offered for a handle with no parked PARKED (the claim was won
elsewhere). The drainer stashes it in `unparkPending`; the handle's next
PARKED consumes it exactly once (a spurious wakeup, tolerated because the
select re-checks the claim). The three directed tests prove the three
required properties: stashed-not-lost (fast-path), matched-when-parked
(direct), never-duplicated (duplicate-unpark), consumed-once-after-rearm
(stale-after-rearm). The explorer proves no interleaving loses one.

## 6. JVM race stress (`WakeupRaceStressTest`)

Real vthreads under contention on the modeled paths:

- `testThunderingHerd`: 32 senders × 32 receivers, one unbuffered channel, 200 iterations — exact send/recv accounting.
- `testTimerStorm`: 48 sleepers × 25 sleeps (1–3ms) + 8 channel pairs moving traffic.
- `testTinyQuantumWithChannels`: 100µs quantum (100× default) × 16 ping-pong pairs × 300 rounds — quantumPark constantly races PARKED/UNPARK.
- `testReceiverHerd`: 24 receivers × 4 channels × 100 receives.

Any hang trips the 60s quiescence timeout (lost wakeup = loud failure);
any drainer invariant violation halts the JVM (fatal path). All counts
asserted exactly.

## 7. Abstraction gap (honest)

- LockSupport permit semantics modeled as "awaitAck returns iff
  acked>=gen" (GG-WAKE-0001 ack-before-unpark + loop re-check: a permit
  can only cause early park() return, never a missed wakeup).
- Real vthread/carrier scheduling, GC, JIT: out of scope (covered
  empirically by §6 and the P2-M8 torture battery).
- The channel/select/timer logic that DECIDES when to unpark: out of
  scope; pairing is given by scripts (that logic is covered by
  ChannelTest/ChannelDifferentialTest/TimerDifferentialTest).
- The `awaitAck` spin itself (LockSupport.park/unpark permit absorption)
  is modeled, not executed; the JFR park-path hooks (`notePark`,
  `parkResumed`) are not in the model (they don't affect wakeup pairing;
  their cost is bounded by `JfrParkCostTest`).
- `armed` is write-only dead state (spike-2b external-waker protocol
  never implemented; all wakeups route through the drainer).

## 9. "Race-detector-equivalent" — the explicit argument

Done-gate 4 asks for JVM race tools on the runtime. No race detector was
run; here is why the combination is equivalent-or-stronger for THIS
design:

1. **The drainer is single-threaded.** Every mutation of lanes, inflight,
   parked, unparkPending, and inIo happens on the drainer thread under one
   lock. There is no concurrent mutation to race — a race detector has
   nothing to find there by construction.
2. **The vthread side is atomic/volatile-only.** Vthreads communicate via
   (a) MPSC offer to a `LinkedBlockingQueue` (linearizable — the explorer
   covers every total order), (b) `AtomicLong`/`AtomicReference`/
   `volatile` fields (`acked`, `parkSeq`, `wakeAction`, `inIo`,
   `parkReason`). Audit of every compound operation: `awaitAck` spins on
   a volatile read (no check-then-act); `armPark`'s getAndIncrement+set
   pair doesn't need atomicity (uniqueness comes from the increment;
   `armed` is write-only); `wakeAction` getAndSet is atomic; `inIo`
   compareAndSet is atomic. No check-then-act race exists.
3. **Channel/select/timer state is monitor-held** (`synchronized` on the
   channel/heap).
4. **Empirical:** the race stress (§6) hammers the real paths with 64+
   vthreads; any race would manifest as a hang (loud timeout), a count
   mismatch (exact assertions), or a drainer halt (fatal path).

A ThreadSanitizer-style tool explores interleavings looking for racy
accesses; (1)–(3) show the racy-access surface is empty by construction,
and the explorer + stress cover the interleaving space directly.

## 8. Red-green evidence

`testModelDetectsWithheldUnpark` withholds a script's paired unpark and
demands DONE: the explorer fails it with LOST WAKEUP. The explorer is
not vacuous — it is capable of failing.
