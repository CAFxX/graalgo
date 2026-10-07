# P3-M7 wave-1 `sort` corpus

Go programs exercising the wave-1 synthetic `sort` package
(`phases/phase1/frontend/synth_sort.go`: faithful pdqsort/stable port +
search; `Find`/`Slice`/`SliceStable`/`SliceIsSorted` fail-fast),
differential vs stock gc 1.27.1. Run via `run_differential.sh` (thin
wrapper over `corpus/wave1/run_wave.sh`; conventions:
`corpus/wave1/README.md`).

## Programs

| Program | Covers |
|---|---|
| `so00_sort_ints.go` | `Sort` on `IntSlice`, empty/single, method form |
| `so01_stable.go` | `Stable` stability with duplicate keys |
| `so02_reverse.go` | `Sort(Reverse(...))`, double reverse, strings |
| `so03_search.go` | `Search`, `SearchInts/Float64s/Strings`, method forms |
| `so04_convenience.go` | `Ints/Float64s/Strings`, `IntsAreSorted/...` |
| `so05_nan.go` | adversarial float64: NaN sorts first, search over NaNs |
| `so06_adversarial_shapes.go` | sorted / reverse / all-equal / duplicates-heavy / sawtooth |
| `so07_large.go` | 5000-element fixed-seed LCG shuffle, full `Sort` |
| `so08_custom_iface.go` | custom `Interface`s, `Reverse` composition |
| `so09_combined.go` | sort + errors: priority errors, stable sort, `Join`, `Is` |

`so06`'s duplicates-heavy `Sort` output is byte-compared against gc's
pdqsort — a faithful-port check on tie-breaking (pdqsort is
deterministic; the synthetic port must reproduce it exactly).

## Known divergences (fail-fast, never silent)

- `sort.Find`: exact gc 1.27.1 signature, panic body — v1 cannot emit or
  call multi-result functions (WS D). No corpus program calls it;
  re-test when WS D lands.
- `sort.Slice` / `sort.SliceStable` / `sort.SliceIsSorted`: exact gc
  signatures, panic bodies — they need `reflect`. Re-test when reflect
  lands.
- The synthetic `reverse` delegates explicitly instead of embedding
  `Interface` (unobservable through the public API).
