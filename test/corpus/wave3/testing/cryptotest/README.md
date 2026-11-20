# `testing/cryptotest` wave-3 corpus

Differential corpus for the testing/cryptotest package (L11, REVIEW-2026-11-18-05).

## Scope boundary

The deterministic-random test helper (panic program).

## Programs

- `00_basic.go` — SetGlobalRandom panics outside a test binary; the expected first stderr line is in 00_basic.panic.

## Retest trigger

cryptotest gains a non-test-binary API, or the panic message changes.
