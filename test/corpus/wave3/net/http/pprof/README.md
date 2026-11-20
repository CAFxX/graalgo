# `net/http/pprof` wave-3 corpus

Differential corpus for the net/http/pprof package (L11, REVIEW-2026-11-18-05).

## Scope boundary

The pprof handlers (blank import registers them).

## Programs

- `00_basic.go` — Mux resolution of the profile paths; /debug/pprof/cmdline answers 200; index lists the known profiles.

## Retest trigger

Any change to net/http/pprof handler registration.
