# `runtime/trace` wave-3 corpus

Differential corpus for the runtime/trace package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Execution tracing to a buffer.

## Programs

- `00_basic.go` — Start/Stop, IsEnabled, second-Start error path, region + log; only sizes and booleans printed.

## Retest trigger

Any change to runtime/trace behaviour.
