# M8 Triage: Divergence Classes C, D, E (Review #2 finding #1)

**Found by:** M8 fuzzer (multiple seeds)
**Status:** Pre-existing (not M8 regressions)
**Triage date:** 2026-10-05

## Class C: Nil slice/map %v prints <nil> (P1, silent wrong output)

**Repro:**
```go
var s []int
var m map[string]int
fmt.Printf("%v\n", s)  // gc: [], graalgo: <nil>
fmt.Printf("%v\n", m)  // gc: map[], graalgo: <nil>
```

**Root cause:** Fmt prints nil slices/maps as `<nil>` (Go's nil interface/pointer
format) instead of `[]`/`map[]` (Go's empty-but-typed composite format).

**Re-test trigger:** Fix Fmt to distinguish nil slice/map (print `[]`/`map[]`)
from nil pointer/interface (print `<nil>`).

## Class D: -0.0 prints as -0 (P1, silent wrong output)

**Repro:**
```go
fmt.Printf("%f\n", -0.0)  // gc: 0.000000, graalgo: -0.000000
fmt.Printf("%v\n", -0.0)  // gc: 0, graalgo: -0
```

**Root cause:** Go's fmt has special handling for negative zero — it prints
as `0`, not `-0`. Graalgo's float formatter preserves the sign bit.

**Re-test trigger:** Fix GoFloatFmt to normalize -0.0 to 0.0 for %f/%v/%g.

## Class E: %f of tiny floats crashes (P1, crash)

**Repro:**
```go
fmt.Printf("%f\n", 1e-300)  // gc: 0.000000, graalgo: IllegalArgumentException: Digits < 0
```

**Root cause:** The float-to-decimal conversion underflows for subnormal/tiny
values, producing a negative digit count.

**Re-test trigger:** Fix the decimal conversion to handle tiny values (clamp to 0).

## Disposition
All three are real bugs in the v1 subset (fmt is in scope). Documented with
repros and re-test triggers. Do not block done-gate 2 (soak is operational;
triage loop is now keeping up).
