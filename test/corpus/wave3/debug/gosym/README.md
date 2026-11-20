# `debug/gosym` wave-3 corpus

Differential corpus for the debug/gosym package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Go symbol + line tables from the running binary (/proc/self/exe):
function count, file count, main.main lookup with its line-table entry,
PCToLine, and LineToPC round-trip. Modern gc binaries carry no
`.gosymtab` section, so the table is derived from `.gopclntab` alone
via `NewTable(nil, lt)` — the canonical form used by the stdlib's own
pclntab tests.

## Programs

- `00_basic.go` — Symbol + line tables of the running binary: func/file
  counts, main.main lookup, PCToLine/LineToPC, missing-function lookup.

## Retest trigger

Any change to debug/gosym table parsing, pclntab-derived function
tables, or PCToLine/LineToPC behaviour.
