# P3-M4 `sync/atomic` corpus

Go programs exercising the P3-M4 `sync/atomic` typed wrappers
(ADR-0004 D2, GG-0020), differential vs stock gc 1.27.1.

## Programs

| Program | Covers |
|---|---|
| `at00_ops.go` | All six types × five ops (Load/Store/Add/Swap/CompareAndSwap), sequential, deterministic values |
| `at01_contention.go` | 8 goroutines × 1000 `Add` = 8000; 4 goroutines × 1000 CAS-loop increments = 4000 |
| `at02_types.go` | Zero value usable; `new()` form; value-copy semantics (`b := a` copies, by-value param copies) |
| `at03_atomic_pointers.go` | Pointer aliasing: `*atomic.Int64` param, pointer copy, interface-held pointer (P0-1); by-value still clones |

## Design notes

- The wrappers are the only Go-expressible atomic surface: `&x`/`*p`
  are WS D scope errors, so the package-level `atomic.AddInt64(&x, …)`
  functions are defined (exact gc signatures) but rejected at call
  sites. The corpus covers the typed wrappers.
- `atomic.Int64` as a struct field, slice element, or array element is
  not in M4 scope (exporter scope error); the corpus does not use
  those shapes.
- Cell value semantics: assignment and by-value parameters clone the
  cell (Go value copy); closures capture by reference (sharing works
  for contention).
