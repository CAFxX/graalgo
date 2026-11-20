# `debug/plan9obj` wave-3 corpus

Differential corpus for the debug/plan9obj package (L11, REVIEW-2026-11-18-05).

## Scope boundary

A minimal Plan 9 386 a.out file built in-memory: header fields,
section names/sizes/offsets and data; error path for a bad magic.

## Programs

- `00_basic.go` — Minimal Plan 9 386 a.out built in-memory: header + sections + data.

## Retest trigger

Any change to debug/plan9obj header parsing, section derivation,
or magic validation.
