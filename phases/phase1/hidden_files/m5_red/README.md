# M5 Red-Green Mutation Evidence

## Mutant 1: Reversed defer order (LIFO → FIFO)
**File**: `src/main/java/graalgo/runtime/DeferChain.java`
**Mutation**: In `unwindReturning`, changed `chain.pop()` to `chain.removeLast()` (FIFO instead of LIFO).
**Expected**: pn01_defer_lifo fails (order=123 instead of 321).
**Result**: `order=123` — test fails as expected. Log: `mutant1_fifo.log`.
**Restored**: Yes.

## Mutant 2: recover() always returns nil
**File**: `src/main/java/graalgo/runtime/Panic.java`
**Mutation**: In `recover()`, added `if (true) return null;` at the start.
**Expected**: pn07_recover_stops fails (panic not recovered, program crashes instead of stage=7123).
**Result**: `panic: x` (unrecovered) — test fails as expected. Log: `mutant2_recover_nil.log`.
**Restored**: Yes.

Both mutants are killed by the PanicCorpusTest suite, demonstrating the tests are non-tautological.
