# `runtime/cgo` wave-3 corpus

Differential corpus for the runtime/cgo package (L11, REVIEW-2026-11-18-05).

## Scope boundary

The cgo marker package (no public API).

## Programs

- `00_basic.go` — Blank import; CGO_ENABLED from build info. Ships with 00_basic.scopelimited (no cgo in the frontend).

## Retest trigger

Frontend gains cgo support (see 00_basic.scopelimited).
