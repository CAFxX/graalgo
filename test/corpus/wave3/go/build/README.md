# `go/build` wave-3 corpus

Differential corpus for the go/build package (L11, REVIEW-2026-11-18-05).

## Scope boundary

The default build context and package discovery.

## Programs

- `00_basic.go` — Default context fields, ImportDir on a generated package, Import with FindOnly, IsLocalImport, and MatchFile.

## Retest trigger

Any change to go/build package discovery, context defaults, or build-tag matching.
