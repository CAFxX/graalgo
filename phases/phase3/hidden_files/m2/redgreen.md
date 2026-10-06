# P3-M2a red-green-first evidence — DIV-M3-1 (timer-channel `len()`/`cap()`)

Date: 2026-10-06. HEAD at start: `e0c07a5f971b9e0f7466333f05f19e03209bc7ea`
(graalgo master). Worker P3-M2a, milestone P3-M2 half-A (sync bindings are
the sibling worker's half; untouched here).

## gc truth (stock gc 1.27.1, `~/tools/go`)

Program: `phases/phase2/corpus/timers/tm15_timer_chan_len_cap.go`
(`go run`, gc 1.27.1), recording the `.expected` file byte-for-byte.

| state                | gc `len(t.C)` | gc `cap(t.C)` |
|----------------------|---------------|---------------|
| timer pre-fire       | 0             | 0             |
| timer fired, undrained | 0           | 0             |
| timer drained        | 0             | 0             |
| ticker fired (ticks buffered) | 0    | 0             |
| ticker stopped       | 0             | 0             |

gc presents timer channels as fully unbuffered: `cap()` diverges too,
not just `len()` — the divergence class is "timer channel shape", exactly
the DIV-M3-1 re-test trigger ("a length-masking wrapper or dedicated
channel flavor"). No escalation: the assumed fix shape (override both to
0) matches gc truth.

## RED: `testTm15TimerChanLenCap`

New differential test
(`TimerDifferentialTest#testTm15TimerChanLenCap`, `verifies: GG-0048`),
mechanical Java translation of `corpus/timers/tm15_timer_chan_len_cap.go`
per the TimerDifferentialTest convention; gc-recorded `.expected` above.
Commit 1 of 2 (red test only, no fix). Run on the unmodified tree:

```
Exception in thread "main" java.lang.AssertionError: tm15
expected:
timer pre-fire: 0 0
timer fired undrained: 0 0
timer drained: 0 0
ticker fired: 0 0
ticker stopped: 0 0
done

actual:
timer pre-fire: 0 1
timer fired undrained: 1 1
timer drained: 0 1
ticker fired: 1 1
ticker stopped: 0 1
done
```

Failing lines are exactly the DIV-M3-1 observables: `cap` reports 1 in
every state, `len` reports 1 while a fire value is buffered. The test is
capable of failing (proven) and passes only when both report 0.

## Fix (commit 2 of 2)

Dedicated channel flavor, per the DIV-M3-1 re-test trigger:

- New `graalgo.runtime.TimerChan extends GoChan` (was `final`; now
  subclassable with the masking contract documented on the `GoChan`
  class javadoc): `super(1, 0L)` real buffer underneath, but
  `buffered()` and `capacity()` overridden to return 0. The mask lives
  in the observation probes only — `trySend`/`tryRecv`/send/recv/close/
  select all use the buffer fields directly, so fire values remain
  deliverable.
- `GoTimer` now builds `new TimerChan()` instead of `new GoChan(1, 0L)`
  (field type `TimerChan`); `channelImpl()` return type stays `GoChan`
  so `TimerHeap` (drain via `tryRecv`, fire via `trySend`) is untouched.
- Verified pre-commit: `buffered()` is called only from `ChanOps.len`
  and `capacity()` only from `ChanOps.cap` (re-grep; the only other
  `buffered()` caller is a `ChannelTest` probe on a plain `GoChan`);
  `TimerHeap`'s `channelImpl()` uses are `tryRecv`/`trySend` only;
  `requireGoChan`'s `instanceof GoChan` still passes for the subclass;
  `ChanOps.len`/`cap` signatures frozen (bodies internal — no version
  bump).
- Docs in the same change: `GoTimer` "Known divergence (P2-1)" javadoc
  → resolved; `DIV-M3-1` marked FIXED in
  `phases/phase2/DIVERGENCES.md` with evidence pointers.

## GREEN

Isolated differential test, post-fix:

```
$ java ... RedRun
RED-RUN PASSED (unexpected)     # label kept for red/green symmetry; exit=0
```

i.e. `testTm15TimerChanLenCap` byte-matches the gc-recorded
`.expected` (`0 0` in all five states).

Full regression, post-fix (exact commands):

```
$ cd phases/phase2 && make test
194 tests, 0 failures
```
(`194 = 193 sign-off count + 1 new testTm15TimerChanLenCap`; includes
`PASS TimerDifferentialTest.testTm15TimerChanLenCap` and all delivery
tests proving the buffered fire value is still reachable: tm02/tm03
recv, tm11 select-after, tm13/tm14 drain-on-stop/reset, TimerTest
ticker/select paths.)

```
$ cd phases/phase1 && make test
208 tests, 0 failures
```

The sibling worker's sync bindings were not touched; no `sync/` or
`research/phase5-opt-survey/` files appear in this change, and no
`phases/phase1/` source was modified (phase-1 suite re-ran only as
regression evidence).

Two commits, no amends:
- `ef830ad`: red test + red half of this evidence (no fix).
- this commit: `TimerChan` fix + green evidence + doc updates
  (`GoTimer` javadoc, `DIV-M3-1` → FIXED in `phases/phase2/DIVERGENCES.md`,
  timer corpus README), with the Phase-3 blocking trigger discharged.
