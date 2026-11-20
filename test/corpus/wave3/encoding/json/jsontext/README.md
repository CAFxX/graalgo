# `encoding/json/jsontext` wave-3 corpus

Differential corpus for the encoding/json/jsontext package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Syntactic JSON processing.

## Programs

- `00_basic.go` — Token-stream encode/decode, whole-value round-trips, and error paths on malformed input.

## Retest trigger

Any change to encoding/json/jsontext token handling.
