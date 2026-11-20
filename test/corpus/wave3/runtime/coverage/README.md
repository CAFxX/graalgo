# `runtime/coverage` wave-3 corpus

Differential corpus for the runtime/coverage package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Coverage-data writing without a coverage build.

## Programs

- `00_basic.go` — WriteCounters/WriteMeta/ClearCounters and the Dir variants all report errors; only error booleans printed.

## Retest trigger

Any change to runtime/coverage error behaviour.
