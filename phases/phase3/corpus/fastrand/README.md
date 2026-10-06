# P3-M4 fastrand corpus

Go programs exercising `runtime.fastrand` / `runtime.fastrandn`
(GG-0020), differential vs stock gc 1.27.1.

## Programs

| Program | Covers |
|---|---|
| `fr00_range.go` | `fastrandn(n) ∈ [0,n)` for 10000 draws × several n; `fastrand()` full-range |
| `fr01_uniformity.go` | 100000 draws of `fastrandn(1000)`: all 10 bucket counts within [5000,15000] (wide bounds — a property, not a sequence) |
| `fr02_variety.go` | 10000 draws contain >9000 distinct values (not degenerate) |

## Design notes

- Random *sequences* are not comparable across runtimes (different
  PRNG state/algorithms): the corpus asserts statistical properties
  (range, rough uniformity, variety) with wide bounds so the tests are
  stable, never exact sequences.
- `//go:linkname` access to `runtime.fastrand`/`runtime.fastrandn`
  requires `import _ "unsafe"` (gc rule); the corpus programs include it.
