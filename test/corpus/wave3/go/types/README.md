# `go/types` wave-3 corpus

Differential corpus for the go/types package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Type-checking embedded source.

## Programs

- `00_basic.go` — Check on declarations, generics, and methods; expression types; constructed types; the ill-typed error path.

## Retest trigger

Any change to go/types checking results or type printing.
