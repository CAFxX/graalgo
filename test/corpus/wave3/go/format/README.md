# `go/format` wave-3 corpus

Differential corpus for the go/format package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Formatting Go source.

## Programs

- `00_basic.go` — Source on unformatted code, Node on a parsed AST and a single declaration, and error paths.

## Retest trigger

Any change to go/format output.
