# Finding: graalgo extremely slow on fuzz seed 0 (2026-10-10)

**Category:** PERFORMANCE (not hang)
**Reproducer:** `hang_seed0.go` (renamed; actually slow, not hung)
**Seed:** 0

## Summary

The program runs in 0.020s under gc 1.27.1 but graalgo had not completed
after 25s (producing output slowly, making progress). This is a severe
performance pathology, not an infinite loop.

## Characterization

- Frontend completes normally.
- Execution makes progress (output appears) but is >1000x slower than gc.
- Program features: nested for loops, struct literals, switch, float ops,
  maps. No goroutines, no recursion.

## Status

OPEN — needs performance analysis (likely Truffle interpretation overhead
or a deopt/megamorphic pathology).

## Note

Originally classified as HANG (120s timeout). Reclassified as PERFORMANCE
after observing output progress. The fuzzer's timeout-based hang detection
cannot distinguish slow from hung; future work: progress monitoring.
