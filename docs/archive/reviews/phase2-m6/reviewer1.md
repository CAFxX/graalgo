# P2-M6 review — reviewer 1 (independent, read-only)

Snapshot: `d61a340` (branch master, `~/workspace/graalgo`). Scope: the six
P2-M6 commits `6f8ca2b..d61a340` vs `38505b3` — new `graalgo.jfr` package,
hookups in `SchedulerCore`/`GoroutineHandle`/`GoChan`/`SelectOp`/`Timers`,
`JfrEventsTest` (5 tests).

**Verdict: GO-WITH-FINDINGS** — 0 P0, 1 P1, 7 P2. The design is sound and the
pairing/rate-control machinery is correct under adversarial interleavings I
could construct. The single P1 is a real double-accounting path in the IO
events (threshold filter is not a guarantee); the fix is a two-line deletion.
No finding touches scheduler safety: with JFR disabled the hookups cannot
change scheduling behavior (verified path by path).

Method: read every new/changed file, traced all park paths, grepped every
`notePark`/`parkResumed`/`awaitAck` call site, and ran two empirical probes
against the prebuilt classes on the pinned GraalVM 25 (in `/tmp`, repo
untouched).

---

## Answers to the 8 review questions

### 1. Start/end pairing — YES, guaranteed on every path I could construct

`spawn` (`SchedulerCore.java:326-368`): the vthread body is
`awaitAck` (initial admission) → `startNanos=now` (337) →
`JfrEvents.goroutineStart(h)` (338) → `body.run()`, all inside one
try whose `finally` (352-368) calls `JfrEvents.goroutineEnd(h, …)` (358)
*before* the EXIT token offer and quiescence accounting.

- **Panic**: caught at 340 → `recordPanic` → finally → end. ✓
- **Goexit**: caught at 343 (non-main); main's Goexit is caught *inside*
  the body lambda (`SchedMain.java:147`) → finally still runs → end. ✓
- **Crash** (`StackOverflowError`, `NoClassDefFoundError`,
  `RuntimeException`): caught at 345/347/351 → finally → end. ✓
- **Uncaught `Error`** (e.g. `AssertionError`, `OutOfMemoryError`,
  `ThreadDeath`): matches *no* catch clause, but `finally` runs on any
  throwable propagation — end is still emitted. ✓ (This is the path most
  likely to have been missed; it is covered by `finally` semantics, not by
  the catch list.)
- **Vthread never admitted** (drainer dead): `awaitAck` at 333 never
  returns → neither start nor end → balanced. ✓
- **`vthread.start()` throws / `GoroutineState.install` throws**: before
  the try / before accounting → neither emitted → balanced. ✓

Residual holes (both P2, finding 3/4): if `goroutineStart`'s own `emit`
throws, the finally emits an *orphan end*; if `goroutineEnd`'s emit throws,
the rest of the finally (quiescence decrement, EXIT token) is skipped.
`Event.commit()` does not throw in practice, so these are robustness nits,
not live bugs.

### 2. Park-reason accuracy — accurate at every site

Exhaustive site list (only `awaitAck` call sites can park, and each is
preceded by exactly one `notePark`):

| Site | Reason | Verdict |
|---|---|---|
| `GoChan.send` (`GoChan.java:316`) | CHANNEL | ✓ send genuinely blocked (waiter enqueued, `trySend` failed) |
| `GoChan.recv` (`GoChan.java:342`) | CHANNEL | ✓ same |
| `SelectOp` claimed-by-other (`SelectOp.java:245`) | SELECT | ✓ park is select machinery; no `ChannelBlock` is deliberate + commented |
| `SelectOp` empty (`SelectOp.java:265`) | SELECT | ✓ genuinely blocks; `channelId=-1` documented |
| `SelectOp` normal (`SelectOp.java:273`) | SELECT | ✓ |
| `Timers.sleep` (`Timers.java:50`) | TIMER | ✓ |
| `quantumPark` (`SchedulerCore.java:207`) | QUANTUM | ✓ covers expiry + voluntary yield, as `ParkReason` javadoc states |
| `ioEnter`/`ioExit` (`SchedulerCore.java:159,177`) | IO | ✓ reason is right; the *accounting* is not (finding 1) |
| `parkCurrent(Runnable)` 1-arg → UNKNOWN | UNKNOWN | ✓ no production callers (only `runtime/Scheduler.park`, itself uncalled in main+test) — honest label |

No site passes a wrong reason. `parkCurrent(Runnable, ParkReason)` is the
only reason-carrying entry and every production caller supplies the
matching constant.

### 3. Enablement cache — staleness argument is *safe* but the doc overstates it

`parkEnabled` (`JfrEvents.java:104-111`): single global volatile
`parkEnabledCache`, refreshed when the handle's post-`armPark` seq hits a
1024-boundary or on the handle's first park.

- **No lost-or-duplicated event via interleaving**: `parkResumed`
  (`JfrEvents.java:142-158`) is single-consumer — it nulls `parkReason`
  on first call and `awaitAck` invokes it exactly once per park, on the
  same vthread that called `notePark` (all fields volatile,
  `GoroutineHandle.java:58-59,73`). A concurrent refresh by another handle
  can only write the *current* value of the JVM-global `enabled()`
  predicate; last-writer-wins is linearizable enough for observability.
  `commit()` with no active recording is a specified no-op, so
  stale-true at commit time is harmless.
- **Genuine loss is confined to the documented window**: recording starts
  mid-park → `parkStartNanos==0` → dropped; recording stops mid-park →
  refresh (or no-op commit) → dropped. Both are inherent to any
  start-timestamp design.
- **Doc overstatement (P2, finding 5)**: "staleness is milliseconds on a
  busy scheduler" — the refresh is per-*handle* (1024 of *its* parks), so
  a rarely-parking handle (e.g. a timer sleeper) has an *unbounded*
  staleness window. A 1-hour sleep that starts before a recording begins
  is invisible for its whole duration even with the recording active
  throughout. Arguably correct (can't time-travel the start), but the doc
  should say "bounded by the handle's park rate", not "milliseconds".
- **Sampling determinism — confirmed**: `armPark` increments seq *before*
  `notePark` reads it, and nothing touches `parkSeq` between `notePark`
  and `parkResumed` (drainer never does; the vthread is parked). In
  `testHotLoopParkFilter`: spawn leaves seq=1, 5000 yields observe
  seq=2..5001, boundaries at 1024/2048/3072/4096 → exactly 4 sampled
  quantum parks per goroutine = 16 total. The test's 8..32 bound is
  generous but the mechanism is deterministic. (Bound could be tightened —
  finding 8.)

### 4. ioEnter/ioExit — YES, double-accounting is reachable (P1, finding 1)

`ioEnter` (159) and `ioExit` (177) both `notePark(h, ParkReason.IO)`;
`parkResumed` commits any non-quantum park ≥ `PARK_THRESHOLD_NANOS`
(100µs); `ioExit` then calls `ioBlocked` (182) with the *full* interval.

The design claims the handshake is "filtered" (`JfrEvents.ioBlocked`
javadoc: "the microsecond slot-release handshake which `parkResumed`
filters"). **The filter is a duration threshold, not a guarantee.** The
handshake is a full drainer round-trip (offer + WAKE-lane admission); under
drainer contention it can exceed 100µs, yielding up to **three overlapping
`GoroutinePark` IO events for one syscall** (IO_ENTER handshake +
IO_EXIT handshake + full duration), with the handshake durations strictly
contained in the `ioBlocked` interval — i.e. the same nanoseconds counted
twice/thrice. Additionally the handshake wait is *scheduler latency*,
mislabeled as IO. Fix: delete the two `notePark` calls — `ioBlocked` is
already the authoritative IO event, and without a noted reason
`parkResumed` becomes a no-op across those awaits.

`ioStartNanos` ordering: set in `ioEnter` (157), read in `ioExit` (182),
same vthread, program order; re-entrant `ioEnter` and unpaired `ioExit`
fail fast *before* the write/read respectively. ✓ (Nit: the write is an
unconditional `nanoTime()` even when JFR is disabled — finding 6.)

### 5. Thread-safety — clean

All new handle fields (`parkReason`, `parkStartNanos`, `ioStartNanos`,
`jfrCacheInit`, `startNanos` — `GoroutineHandle.java:58-80`) are written
and read exclusively by the owning vthread (`notePark`/`parkResumed`/
`ioEnter`/`ioExit`/spawn-body/finally all run on it via `requireHandle`);
`volatile` is belt-and-braces. The drainer never touches them (verified by
grep — no references in `Drainer.java`); a waker (`unpark`) never touches
them. `parkEnabledCache` races are benign (above). No cross-thread
publication bug exists in this milestone.

### 6. `@Enabled(false)` opt-in — correct, and verified empirically

Opt-in is the right semantic for high-rate scheduler events (otherwise any
ambient recording would silently capture them — the red-green
`testNoEventsWhenDisabled` proves the point). I verified the override
mechanism on the pinned GraalVM 25 with a probe (`/tmp/jfrprobe`,
throwaway): `new Recording().enable("graalgo.GoroutinePark")` **before**
the event class is loaded does *not* throw, and once the class loads the
event reports `enabled()==true` with the recording running — i.e.
enable-by-name resolves lazily and reliably overrides `@Enabled(false)`.
(The old-JDK `IllegalArgumentException`-for-unknown-names behavior does not
apply here; `ensureRegistered()` remains good hygiene and the test's use of
it is harmless.) No finding.

### 7. ChannelBlock/park correlation — holds in the block→park direction

- `GoChan.send/recv`: `channelBlock` (315/341) is followed
  *unconditionally* by `parkCurrent` — the instructions between cannot
  throw (`requireHandle`/`armPark`/`notePark` are infallible;
  `Drainer.offer` → unbounded `LinkedBlockingQueue.offer`, never throws).
  Even if a waker already dequeued the waiter, the park still happens
  (stashed UNPARK) — so block and park stay paired.
- Reverse direction: every CHANNEL/TIMER park is preceded by its
  `channelBlock` (or correctly has none, for TIMER/QUANTUM/IO/UNKNOWN).
  One deliberate exception: `SelectOp` claimed-by-other (245) parks with
  SELECT reason and *no* `ChannelBlock` (commented as intentional — the
  result was already decided). Under contention that park could exceed
  100µs and commit an uncorrelated SELECT park event. Documented tradeoff;
  P2 (finding 7) only because a consumer joining block→park will see a
  park without a block.
- Design note (positive): `ChannelBlock` fires *before* parking, so a
  goroutine that never resumes (deadlock) still leaves a trace. Good.

### 8. Disabled-path overhead/behavior — contract holds, no behavior change

- Per-park disabled cost: `notePark` (volatile write + 2 volatile reads +
  branch + volatile write) + `parkResumed` (volatile read + write + read,
  short-circuit return) + one `parkEnabled` refresh per 1024 parks.
  Measured 60ns/park in EVENTS.md (< 1 `nanoTime()`); the javadoc's
  "~10ns" is stale (finding 5).
- No scheduler behavior change when disabled: reason-tagging is pure
  field writes; `channelBlock`/`goroutineStart`/`goroutineEnd`/`ioBlocked`
  are guarded by live `enabled()` checks; `channelId` is a creation-time
  `AtomicLong` increment (not per-op); non-blocking channel fast paths
  never touch JFR; `ensureRegistered()` is not called in production (no
  class-load side effects on the scheduler path).
- The 1-arg `parkCurrent` still exists and delegates; `armPark`/ticket
  discipline untouched.

---

## Findings

### P1 (should fix)

**1. IO handshake double-accounting via threshold-only filtering.**
`SchedulerCore.java:159,177` `notePark(h, ParkReason.IO)` + 
...[truncated 4925 chars]