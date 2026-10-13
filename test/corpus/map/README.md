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
| `mp04_nil_map_inner.go` | nil-map write inside `helper()`: innermost frame attributed to the panicking statement, not the def line (P1-2) |

## Design notes

- Iteration order is unspecified in Go: the corpus never asserts an
  order. `mp01`/`mp02` assert order-independent properties (counts,
  sums, set membership).
- `mp03`/`mp04` are panic programs: `.panic` (graalgo's expected
  normalized stderr) and `.panic.gc` (gc's normalized stderr) hold the
  full tracebacks. The runner compares the FULL normalized stderr —
  gc vs graalgo live, plus graalgo vs the checked-in `.panic` anchor
  (P3-M4 remediation P2-4: first-line-only comparison could not catch
  a wrong innermost frame line, cf. P1-2). Stdout (printed before the
  panic) is compared byte-identical.
