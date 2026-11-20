# `go/build/constraint` wave-3 corpus

Differential corpus for the go/build/constraint package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Parsing and evaluating build constraints.

## Programs

- `00_basic.go` — Parse of //go:build and legacy // +build lines, String round-trip, Eval against tag sets, IsGoBuild/IsPlusBuild, and error paths.

## Retest trigger

Any change to go/build/constraint parsing or evaluation.
