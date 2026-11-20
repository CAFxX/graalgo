# `go/doc` wave-3 corpus

Differential corpus for the go/doc package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Extracting documentation from package source.

## Programs

- `00_basic.go` — NewFromFiles listings (funcs, types, consts, methods) with synopses, and the AllDecls mode.

## Retest trigger

Any change to go/doc extraction, listings, or synopses.
