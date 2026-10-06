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

...to be written...

## GREEN

...to be written...
