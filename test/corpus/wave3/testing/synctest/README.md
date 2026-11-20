# `testing/synctest` wave-3 corpus

Differential corpus for the testing/synctest package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Running code in a synctest bubble (fake clock).

## Programs

- `00_basic.go` — Goroutines sleep on the fake clock; event order pinned; Wait settles the bubble. Marked .timing. The *testing.T is a zero value.

## Retest trigger

Any change to testing/synctest bubble or clock behaviour.
