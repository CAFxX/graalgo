# `runtime/pprof` wave-3 corpus

Differential corpus for the runtime/pprof package (L11, REVIEW-2026-11-18-05).

## Scope boundary

The profile registry.

## Programs

- `00_basic.go` — Profile names and counts (presence only), Lookup of known/unknown profiles, Labels via pprof.Do.

## Retest trigger

Any change to runtime/pprof profiles.
