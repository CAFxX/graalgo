# P3-M4 map corpus

Go programs exercising map operations (GG-0020), differential vs stock
gc 1.27.1.

## Programs

| Program | Covers |
|---|---|
| `mp00_basic.go` | insert/get/delete/len, comma-ok, missing-key zero, overwrite |
| `mp01_iteration.go` | iteration visits every key (order-independent: count + set membership + canonical-order lookup) |
| `mp02_delete_during_range.go` | delete-current-key during range (safe); visit-count + key-sum + empty afterwards |
| `mp03_nil_map.go` | nil map: read/delete are no-ops; write panics `panic: assignment to entry in nil map` |

## Design notes

- Iteration order is unspecified in Go: the corpus never asserts an
  order. `mp01`/`mp02` assert order-independent properties (counts,
  sums, set membership).
- `mp03` is a panic program: `.panic` (graalgo's expected first
  stderr line) and `.panic.gc` (gc's) are both
  `panic: assignment to entry in nil map`; stdout (printed before the
  panic) is compared byte-identical.
