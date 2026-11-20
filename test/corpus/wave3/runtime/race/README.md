# `runtime/race` wave-3 corpus

Differential corpus for the runtime/race package (L11, REVIEW-2026-11-18-05).

## Scope boundary

The race detector package (no public interface).

## Programs

- `00_basic.go` — Blank import; the pin is the import itself plus a deterministic computation. Ships with 00_basic.scopelimited.

## Retest trigger

Frontend/runtime gains race-detector support (see 00_basic.scopelimited).
