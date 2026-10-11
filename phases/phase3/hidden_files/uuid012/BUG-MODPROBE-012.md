# BUG-MODPROBE-012 — case03 hang in UnmarshalText/String path

**Date:** 2026-10-11
**Lane:** m9-uuid012 (worktree `wt/f7467b85-7da6-4da0-b9ec-94631a60dafe/m9-uuid012`)
**Status:** NOT REPRODUCIBLE on base 037af0c — closed without code change
**Base:** m9-uuidnpe @ 037af0c (BUG-MODPROBE-009 NPE fix)

## Reported symptom (pre-037af0c)

uuid case03 (`UnmarshalText` → `String()` → `UnmarshalText`, plus `==` comparisons)
reported as: "produces no output and hits the 300s timeout on both gmp legs."
Not root-caused by the reporting worker.

## Investigation

**Reproduction attempts on 037af0c (all phases rebuilt from this base):**

| Run | GMP | Result |
|-----|-----|--------|
| case03/main.go | 1 | exit 0, byte-identical to gc 1.27.1 |
| case03/main.go | 1 | exit 0, byte-identical to gc 1.27.1 (repeat) |
| case03/main.go | 4 | exit 0, byte-identical to gc 1.27.1 |

Output (all runs):
```
f47ac10b-58cc-0372-8567-0e02b2c3d479
true
true
```

**Conclusion:** The hang does not reproduce on 037af0c. Three successful runs,
both gmp legs, all byte-identical to gc.

## Likely explanation

The hang was reported against the pre-037af0c tree, which had the BUG-MODPROBE-009
var-init misordering (disk multi-result stubs invisible to the dependency
analyzer). That broken state produced the NPE; the reported "hang" was most likely
a side effect of the same broken initialization order (or a probe artifact —
slow JVM startup was observed to exceed 60s timeouts during this investigation).
With 037af0c fixing the ordering, case03 runs cleanly.

## Static verification (no latent hang found)

- `GoEquality.equal`: all loops bounded; `byte[]` has no explicit case but
  `[16]byte` is not carried as `byte[]` (case03 `u2 == u` returns true correctly).
- `ConvertNode.stringToSlice`: bounded loops, correct.
- `RangeStmtNode` reset logic: complete for all six Range*Node types (verified
  in source and bytecode).
- `ForRepeatingNode`: condition evaluation at line 100 cannot spin without
  the body making progress (post/writeback at lines 104+).

## Evidence

- Probe logs: `phases/phase3/hidden_files/uuid012/uuid012-main-gmp1-*.log`,
  `uuid012-main-gmp4-*.log` (all show exit 0, correct stdout)
- Probe scripts: `phases/phase3/hidden_files/uuid012/probe.sh`, `probe4.sh`
- Bisection programs: `phases/phase3/hidden_files/uuid012/bisect/` (superseded —
  real case files used instead)

## Recommendation

Close BUG-MODPROBE-012 as NOT REPRODUCIBLE. If the hang reappears, re-open with
a fresh thread dump (jstack) captured while the hang is in progress.
