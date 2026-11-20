# ADR-0016 — Direct handoff to the woken goroutine (gc's runnext) and a bounded handoff spin

**Date:** 2026-11-20
**Status:** accepted 2026-11-20 (lane sched-handoff, work package WP-SCHED).
No requirement changes; SPEC §3.5 gains the "Direct handoff" paragraph.
**Scope:** [SPEC.md](../../arch/SPEC.md) §3.5 (token queues and the drainer,
park/unpark safety); requirements `GG-SCHED-0001`, `GG-SCHED-0003`,
`GG-WAKE-0001`–`0003`, `GG-FAIR-0001`–`0003` (unchanged, re-verified);
[RECOVERY.md](../../arch/RECOVERY.md) WP-SCHED;
[performance.md](../../arch/performance.md) §3.1.

## Context

Every wake went through the single drainer thread: the waker offered an
UNPARK token, the woken goroutine's PARKED token released its slot, and the
drainer re-admitted it and unparked its virtual thread from a platform
thread — an external submission to the carrier pool and, usually, a carrier
wake-up. A channel round trip cost ~4 µs; `chanpingpong` ran 26× gc. gc
avoids this with `runnext`: `ready()` puts the woken G in the current P's
runnext, and when the waker blocks the P runs it next, inheriting the time
slice.

## Decision

1. **Deferred wake.** `SchedulerCore.unpark(g)` called by a goroutine of the
   same scheduler records `g` as the caller's `runnext` instead of offering
   UNPARK (a second wake flushes the first, as gc kicks the old runnext to
   the queue). Wakes from non-goroutines (timer heap, netpoller, cleaner)
   go through the drainer as before.
2. **Handoff park.** If the waker parks (`parkCurrent`) while `runnext` is
   set, it offers one HANDOFF token (parker, generation, target), then sets
   the target's `handoff` flag, then `LockSupport.unpark`s it — the token
   strictly before the flag (the model check finds the reverse order), and
   the flag before the unpark (GG-WAKE-0001). The target inherits the
   waker's quantum deadline, so a ping-ponging pair yields the slot at the
   quantum like one goroutine (GG-SCHED-0001; starvation freedom).
3. **Drainer bookkeeping.** HANDOFF leaves the in-flight count unchanged
   (the slot moves), books the parker like PARKED, and books the target as
   admitted: removed from the parked record, or — when the target's own
   park token is still in the inbox — remembered in `handedOff`, so that
   token only releases (PARKED) or passes on (HANDOFF) the target's old
   slot. In every interleaving the drainer counts at least the goroutines
   actually running on slots, so the cap assert still bounds real
   parallelism (GG-SCHED-0003).
4. **Flush points.** The deferred wake becomes an ordinary UNPARK on the
   waker's quantum park, `ioEnter`, exit, a second wake, process exit, and
   at the first full poll after a 20 µs window (polls stay full while a
   wake is deferred), so the woken goroutine is delayed by at most the
   window while its waker keeps running — and every waker still offers its
   wakee's UNPARK before its own park/exit token (the deadlock check's
   race-freedom argument).
5. **Handoff spin.** After a handoff park the waiter spins up to 5 µs for the
   answer before unmounting, only while the drainer last saw idle slots and
   with no more spinners than idle slots (gc's Ms spin only while Ps are
   idle).
6. **Switch.** `-Dgraalgo.sched.handoff=false` restores the drainer-only
   path (A/B measurement on one build, diagnosis).

## Consequences

- `chanpingpong` best 10.5 → 1.13 ms (gc 0.31), steady median ~50 → ~2 ms;
  `workerpool`, `mutex` unchanged (performance.md §3.1).
- Lane order (GG-WAKE-0003) governs drainer admissions only; a handoff runs
  the target on the waker's slot, ahead of queued goroutines, bounded by
  the inherited quantum — the same trade gc's runnext makes.
- Verified by `WakeupModelTest` (HANDOFF scripts; slot-accounting check
  after every action; seeded flag-before-token and ignore-`handedOff` bugs
  found), `WakeupRaceStressTest.testHandoffPairDoesNotStarveSpinner` (fails
  on a handoff that grants a fresh quantum) and the torture battery
  (F1/F3/F4/F5/F6).
- Residual: a goroutine that wakes another and then busy-waits in Java code
  with no poll and no park (runtime code only; Go code always polls) would
  hold the wake indefinitely; no such runtime path exists today.
