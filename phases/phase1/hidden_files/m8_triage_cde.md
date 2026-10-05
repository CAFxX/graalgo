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
**ALL FIXED (M8 closeout, commit 95b20a2).** Red-green:
`corpus/builtins/bi22_float_edge.go` (gc-identical).

**Correction to the Class D analysis above:** The claim "Go's fmt has
special handling for negative zero" was wrong. Direct measurement shows:
(1) the `-0.0` literal is **+0.0** in gc (`math.Signbit(-0.0)==false`;
go/types folds it to +0.0); (2) true -0.0 (via `math.Copysign(0,-1)` or
unary minus of a variable) prints as **-0** in gc's fmt. The real bugs
were: (a) UnaryExpr never constant-folded, producing IEEE -0.0 at runtime
instead of go/types' +0.0; (b) float `==` used `Double.compare` (total
ordering) instead of IEEE semantics. Both fixed; the triage doc's
"normalizes to 0" premise is retracted.

Class C: nil slice -> `[]`, nil map -> `map[]` (was `<nil>`). Class E:
`%f` of 1e-300 -> `0.000000` (was `Digits < 0` crash from MathContext
with non-positive precision).
