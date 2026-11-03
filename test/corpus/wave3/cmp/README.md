# cmp corpus (P3-M9 wave 3, tier-1a)

Differential corpus for the `cmp` package. Since 2026-11-03 (P1-2 lane)
`cmp` is the REAL GOROOT package resolved via the disk importer
(`internal/frontend/diskimport.go`); the synthetic package
(`internal/frontend/synth_cmp.go` — verbatim gc 1.27.1) is retired.

## Coverage
- `01_compare.go`: `cmp.Compare` on ints, strings, floats
- `02_less.go`: `cmp.Less` predicate
- `03_or.go`: `cmp.Or` with spread (v1 requires `...` spread for variadic calls)
- `04_nan_zero.go`: NaN ordering, -0.0 handling, generic instantiation
  across ordered types (the P1-2 conversion proof)

## Scope limits
None. All `cmp` functions are single-result and supported.
