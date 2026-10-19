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
| `at04_value_happy.go` | `atomic.Value` happy paths (P1-6): Store/Load/Swap/CompareAndSwap round-trips; gc-defined non-panicking edges (CAS(nil,x) on empty Value stores; typed-nil store legal); `new()` and literal forms. A failed check panics; success is silent, exit 0 |
| `at05_value_store_nil.go` | `Value.Store(nil)` panics: `sync/atomic: store of nil value into Value` |
| `at06_value_store_inconsistent.go` | `Value.Store` of inconsistently typed value panics: `sync/atomic: store of inconsistently typed value into Value` |
| `at07_value_swap_nil.go` | `Value.Swap(nil)` panics: `sync/atomic: swap of nil value into Value` |
| `at08_value_swap_inconsistent.go` | `Value.Swap` of inconsistently typed value panics: `sync/atomic: swap of inconsistently typed value into Value` |
| `at09_value_cas_new_nil.go` | `Value.CompareAndSwap` with nil new panics: `sync/atomic: compare and swap of nil value into Value` |
| `at10_value_cas_old_new_mismatch.go` | `Value.CompareAndSwap` with old/new of different dynamic types panics: `sync/atomic: compare and swap of inconsistently typed values` |
| `at11_value_cas_new_vs_stored.go` | `Value.CompareAndSwap` with new of a different type than stored panics: `sync/atomic: compare and swap of inconsistently typed value into Value` |

## Design notes (atomic.Value, P1-6)

- `atomic.Value` method calls lower to `syncop` wire nodes (synctyp
  `atomic.Value`); the variable's value IS the `AtomicReference` cell
  (ADR-0002 D1), constructed by `syncnew` for `var`/`new()`/literal
  forms. The real method bodies (`sync/atomic/value.go`) drive through
  `unsafe.Pointer` and cannot execute — this restores the P3-M10 design
  on the real-package footing (503608ea retired the synthetic overlay's
  lowering).
- Misuse panics (at05–at11) go uncaught to stderr with exit 2 on both
  runtimes; the differential harness normalizes the trace and compares
  the `panic:` line byte-for-byte against gc 1.27.1. The `.expected`
  files are therefore empty (stdout is empty); the verdict is in the
  exit code and the stderr message.
- `fmt.Print*` is unusable in corpus programs on this master (the
  synthetic fmt bodies fail backend AST building — pre-existing,
  unrelated); the happy-path program (at04) uses check-panics instead
  of printing.

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
