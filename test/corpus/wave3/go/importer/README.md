# `go/importer` wave-3 corpus

Differential corpus for the go/importer package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Importing a package from source.

## Programs

- `00_basic.go` — The source importer type-checking go/token from $GOROOT/src, scope lookups, and the missing-package error path.

## Retest trigger

Any change to go/importer source importing or scope contents.
