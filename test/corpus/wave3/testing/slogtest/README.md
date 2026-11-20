# `testing/slogtest` wave-3 corpus

Differential corpus for the testing/slogtest package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Testing a slog.Handler against the case suite.

## Programs

- `00_basic.go` — A recording handler stores records as nested maps; TestHandler checks the whole suite; verdict + record count printed.

## Retest trigger

Any change to testing/slogtest cases or checks.
