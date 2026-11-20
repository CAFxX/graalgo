# `go/scanner` wave-3 corpus

Differential corpus for the go/scanner package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Lexing Go source into a token stream.

## Programs

- `00_basic.go` — Token stream with positions and literals, comment scanning, and error paths on broken source.

## Retest trigger

Any change to go/scanner tokenization, positions, or error reporting.
