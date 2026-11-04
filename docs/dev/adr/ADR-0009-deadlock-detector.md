# ADR-0009 — Deadlock detection: watchdog now, full gc `checkdead` parity later

**Date:** 2026-11-04
**Status:** accepted — watchdog phase implemented (lane L09, review
2026-11-04 P1-11); full-detector phase is design only, see §5.
**Scope:** `SchedulerCore` deadlock watchdog
(`internal/backend/runtime/src/main/java/graalgo/sched/SchedulerCore.java`),
`Drainer` snapshot, `TimerHeap` wake sources, launcher wiring
(`SchedMain`, `P3SyncMain` + `P3TimeRuntime` drain break), netpoll
wake-source hook (`GoNetpoll.registeredDescCount`).
**Normative basis:** gc `runtime.checkdead` semantics
(`src/runtime/proc.go`: fatal "all goroutines are asleep - deadlock!"
when every goroutine is waiting and no timer, netpoll, or pending
work can wake one); review finding P1-11 (review 2026-11-04).

## 1. Context

Before this change, a guest program whose goroutines all ended up
parked (channel/select/mutex waits with no possible waker) hung the
launcher forever: `P3SyncMain` awaited quiescence, which blocks until
the live count hits zero, and only the main-called-`Goexit` case
produced a fatal. `SelectOp` even noted "the deadlock detector
(P2-M10) owns it" — i.e. the detector did not exist. gc fatals in
this situation (exit code 2, "fatal error: all goroutines are asleep
- deadlock!" plus a goroutine dump). The gap was a silent infinite
hang, the worst failure mode for a `go` replacement: no output, no
exit code, no diagnosis.

## 2. Decision: a polling watchdog on scheduler state (implemented)

The watchdog is a daemon thread owned by `SchedulerCore`, enabled
explicitly by the launchers (`enableDeadlockWatchdog()`, default
100&nbsp;ms poll, 3 consecutive identical candidate snapshots).
It fires exactly when the launcher's wait is stuck:

- live goroutines &gt; 0, and
- drainer snapshot: in-flight == 0, parked == live, blocking-I/O == 0,
  all three lanes empty, inbox empty, and
- `TimerHeap.pendingWakeSources()` == 0 (pending entries *plus*
  callbacks currently executing on the timer thread — a fired
  `AfterFunc` callback may still send; its heap entry is already gone,
  so entry count alone would false-positive during a slow callback), and
- external wake sources == 0 (interop launcher registers the
  netpoll-registered-fd count; a registered fd may still deliver a
  readiness event).

On firing, the verdict is sticky: `deadlockDetected()` is true,
`deadlockReport()` holds the gc-style header plus one
`goroutine <id> [<park reason>]` line per parked goroutine, and
`awaitQuiescenceOrDeadlock()` returns false so the launcher prints
the report and exits 2. `P3TimeRuntime.awaitBackgroundDrain` breaks
out on the verdict instead of looping forever (a deadlocked program
never drains). The watchdog is OFF by default so library/test
embeddings that park everything and wake from a test thread see no
behavior change unless they opt in.

Why polling, and why the stability window: the scheduler has no
single "last goroutine parked" event that is also atomic with the
timer/netpoll state, and adding one would put detector logic on the
drainer's hot path. Polling keeps the drainer untouched except for a
lock-held snapshot read. The stability window (identical snapshot,
including the monotonic admissions counter, on consecutive polls)
closes the two polling gaps: (a) a token taken from the inbox but not
yet routed is invisible to one snapshot — it resolves in
microseconds, never across the window; (b) a wakeup path that is slow
but registered (timer callback, netpoll event) is covered by its
wake-source count, not by timing. A false positive requires a wakeup
that is *both* unregistered *and* slower than the window — §5 makes
registration the rule for any future wakeup path.

Deliberate scope decision (OPEN QUESTION, see §6): the watchdog
fires whenever the launcher's wait is stuck, including after main
has returned with only parked non-main goroutines left. gc would
have exited 0 at main return; the launcher today waits for every
goroutine, so under its contract a stuck wait *is* the deadlock.
Converting that hang into a (technically exit-code-divergent) fatal
is strictly better than hanging, and the case vanishes if
main-return semantics ever change to gc parity.

## 3. What the watchdog deliberately does NOT cover

- A goroutine admitted but blocked on a Java monitor / JVM lock /
  unregistered blocking call still counts as in-flight, so the
  watchdog conservatively never fires (false negative, never false
  positive). Any such path that can block indefinitely must either
  go through `parkCurrent`/`ioEnter` or register a wake source.
- Goroutine stacks: the fatal report lists parked goroutine ids and
  park reasons, not Go stack traces. Full per-goroutine Go stacks
  need the runtime's stack-capture machinery for *other* goroutines,
  which does not exist yet (§5).
- `select` with a timer case, `time.Sleep`, tickers: covered by the
  timer wake-source count (a pending timer blocks the verdict; once
  it fires without waking anyone, the next poll fatals).
- Programs that legitimately park everything with a timer or I/O
  outstanding: never fatal while the wake source is outstanding
  (tests: `DeadlockWatchdogTest`).

## 4. Consequences

- Deadlocked guest programs now terminate with gc's fatal message
  and exit code 2 instead of hanging forever, via both launchers
  (`graalgo-run` and `graalgo-sync-run`).
- No behavior change for any program that currently terminates:
  the watchdog can only fire when the launcher would otherwise wait
  forever (all live goroutines parked, no wake source). The only
  behavior change is hang → fatal, plus the main-returned case in §2.
- The drainer gains a lock-held snapshot read (no hot-path change);
  the timer heap gains an in-flight-callback counter (one
  increment/decrement per callback fire, off the goroutine path).

## 5. Design for the full detector (not yet implemented)

The watchdog is phase 1. A gc-parity `checkdead` needs, in order:

1. **Event-driven trigger.** Replace (or augment) polling with a
   drainer-side trigger: when a park makes parked == live and the
   wake-source counts are zero, run the check synchronously instead
   of waiting for the next poll. This removes the (bounded, ~300&nbsp;ms
   default) detection latency and the stability-window reasoning for
   the common case; polling stays as a backstop for paths that bypass
   the drainer.
2. **Wait-graph verification.** Before firing, verify from the
   wait graph that no parked goroutine *can* be woken: channel
   waiter lists, mutex/semaphore holders, select cases. This turns
   the current "no registered wake source" heuristic into a proof,
   and is what lets the detector distinguish "all parked, but a
   sender exists on another channel" from a true deadlock. It
   requires the channel/sync primitives to expose waiter snapshots
   (they already maintain waiter queues; the work is a consistent
   snapshot protocol, not new data).
3. **Full goroutine dumps.** Capture a Go-level stack for every
   live goroutine at fatal time (gc prints all stacks, not just the
   parked set). Depends on cross-goroutine stack capture in the
   Truffle runtime; until then the id+reason report stands.
4. **Syscall/blocking-call accounting.** Any blocking path that
   does not go through `ioEnter`/`parkCurrent` must be inventoried
   and either converted or registered as a wake source; otherwise
   the event-driven trigger in (1) risks false positives that the
   polling watchdog's conservatism currently avoids. The inventory
   is the gating work for (1).
5. **Main-return semantics.** Decide, as its own item, whether the
   launcher adopts gc parity (exit when main returns, abandoning
   parked goroutines) or keeps wait-for-all. That decision settles
   the §2 open question; the watchdog needs no change either way
   except deleting the main-returned case from its scope if parity
   is adopted.

## 6. Open questions (for the coordinator / independent reviewer)

1. **Main-returned + parked-others: fatal 2, or exit 0?** The
   watchdog as implemented fatals (see §2). If the project wants gc
   parity here, that is a launcher-semantics change (exit on main
   return), not a watchdog change — but it should be decided
   explicitly, because the watchdog's current behavior bakes in the
   wait-for-all contract.
2. **Default tuning.** 100&nbsp;ms × 3 polls (~300&nbsp;ms to fatal)
   is a guess, not a measurement: fast enough to feel instant
   against a hang, slow enough to absorb any unregistered wakeup
   path. If a corpus program ever false-positives, the first knob
   is the stability window, and the real fix is registering the
   missing wake source.
3. **Should test harnesses enable the watchdog by default?** The
   runtime test runner does not (tests park everything deliberately
   and wake from test threads). The conformance/diff harness, which
   runs guest programs the way users do, should enable it — a hung
   corpus program currently burns a timeout instead of reporting a
   deadlock. That wiring is follow-up work, not part of this lane.
