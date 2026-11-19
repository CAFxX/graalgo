# `go/printer` wave-3 corpus

Differential corpus for the go/printer package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Parse an embedded source, print it back with printer.Config (UseSpaces + TabIndent); output pinned byte-for-byte. Bonus program alongside go/parser (also zero-import). No .scopelimited programs. Only one small function is printed; comment placement and full-file formatting are out of scope.

## Programs

- `00_basic.go` — Parse + printer.Config round-trip, output pinned.

## Retest trigger

go/printer output format changes.
