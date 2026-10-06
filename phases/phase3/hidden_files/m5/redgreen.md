# P3-M5 red-green evidence

## Java section (Java runtime worker)

Unit suite: `phases/phase3/src/test/java/graalgo/p3time/TimeChanLenCapTest.java`
(registered in `IfaceTestRunner`; `make -C phases/phase3 test` → 33 tests,
0 failures, 2026-10-06, stable across 4 consecutive runs).

### DIV-M3-1 unit regression (`testTimerChanLenCapAllStates`)

Asserts `buffered()==0 && capacity()==0` on the user-visible `TimeChan`
in every state: pre-fire, fired-undrained, drained, stopped — while the
fire value remains deliverable (`recv` yields a `GoStruct` Time whose
unixnano is ~now). The corpus tc07 covers this end to end; this pins it
at the unit level.

- RED (2026-10-06): temporarily changed `TimeChan` to extend plain
  `GoChan(1)` (1-slot buffer, no `TimerChan` masking — i.e. the pre-M2a
  shape) → `testTimerChanLenCapAllStates` and
  `testTickerThreeTicksAndStop` FAIL (`buffered()==1` after fire).
  Restored → green.

### Stop/Reset gc-parity table (same test + ticker/stop tests)

- Stop on fired-undrained → true (Go 1.23+ `timerchandrain` parity);
  second Stop → false; Reset on stopped → false (re-arms); Stop on
  fired+drained → false; Stop before fire → true with no later delivery;
  ticker: 3 Time ticks then Stop → true, no 4th tick.
- RED (2026-10-06): removed the translated-channel drain from
  `GoTimerT.stop()` (heap truth only) →
  `testTimerChanLenCapAllStates` FAILS on "stop on fired-undrained ->
  true" (the heap had already handed the value to the translator, so
  without the user-channel drain Stop wrongly reports false).
  Restored → green.

### Quiescence-safety (no parked-translator hang)

`awaitQuiescence` waits for *every* spawned goroutine, so each test
asserts `P3TimeRuntime.backgroundLive()==0` after `stop()`/shutdown:
`testStopBeforeFirePreventsDelivery`, `testTickerThreeTicksAndStop`,
`testShutdownBackgroundEndsLiveTicker` (a never-stopped ticker is shut
down via `P3TimeRuntime.shutdownBackground()`, the launcher's phase-1
protocol — then `awaitQuiescence` completes).

- BUG FOUND BY THE TESTS (2026-10-06, fixed before commit):
  `testStopBeforeFirePreventsDelivery` hung: `stop()` installed a *fresh*
  `done` channel, but a translator that had not yet read the old `done`
  picked up the fresh (open, never-to-be-closed) one and parked forever.
  Fix: `stop()` closes `done` without replacing it; `reset()` does
  close-then-replace atomically under the translator lock, so a parked
  translator is always on the current done or a closed one it is about to
  wake from — never on an open, abandoned done. Regression covered by the
  `backgroundLive()==0` assertions (they timed out pre-fix).

### AfterFunc

The `AfterFunc` fire-spawns-goroutine path needs a `GoFunc` (Truffle call
target) — not constructible at unit level. Covered end to end by the
corpus (exporter worker's section below).

## Go section (exporter worker — owner: exporter worker)

*(append here; do not rewrite the Java section above)*
