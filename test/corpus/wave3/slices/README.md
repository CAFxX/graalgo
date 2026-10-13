# slices corpus (P3-M9 wave 3, tier-1a)

Differential corpus for the synthetic `slices` package (internal/frontend/synth_slices.go — gc 1.27.1 adapted for v1).

## Coverage
- `01_sort.go`: `slices.Sort` on ints, strings, edge cases
- `02_sortfunc.go`: `slices.SortFunc` with custom comparator
- `03_contains.go`: `slices.Contains`, `slices.Index`
- `04_insert_delete.go`: `slices.Insert`, `slices.Delete`, `slices.Replace` (spread form)
- `05_iterators.go`: `slices.Values`, `slices.All`, `slices.Sorted`

## Scope limits
- `slices.BinarySearch`, `slices.BinarySearchFunc`: return `(int, bool)` — v1 cannot emit multi-result functions (P3-M5). **Retest trigger:** WS D (multi-result support) lands.
- Internal sort helpers (`choosePivot*`, `partition*`, `order2*`) were refactored to single-result by dropping pure optimizations (sortedHint, alreadyPartitioned fast paths). Sort correctness is preserved; performance may differ from gc on already-sorted/reverse-sorted inputs.

## v1 adaptations (in synth_slices.go)
- `Insert`/`Replace` rewritten snapshot-based (no unsafe overlap detection).
- `bits.Len` → local `__slicesBitLen`; `Repeat` overflow check without `bits.Mul`.
- `xorshift.Next()` inlined (v1: no `&` on ident, no multi-result methods).
- `median*` swap-count `*int` param removed (v1: no `&` on ident).
