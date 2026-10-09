# cmp corpus (P3-M9 wave 3, tier-1a)

Differential corpus for the synthetic `cmp` package (phases/phase1/frontend/synth_cmp.go — verbatim gc 1.27.1).

## Coverage
- `01_compare.go`: `cmp.Compare` on ints, strings, floats
- `02_less.go`: `cmp.Less` predicate
- `03_or.go`: `cmp.Or` with spread (v1 requires `...` spread for variadic calls)

## Scope limits
None. All `cmp` functions are single-result and supported.
