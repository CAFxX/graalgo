# `time/tzdata` wave-3 corpus

Differential corpus for the time/tzdata package (L11, REVIEW-2026-11-18-05).

## Scope boundary

The embedded IANA time-zone database (side-effect import).

## Programs

- `00_basic.go` — LoadLocation for several zones at fixed instants; offsets pinned; bogus-zone error path.

## Retest trigger

Any change to time/tzdata embedding or zone loading.
