# `uuid` wave-3 corpus

Differential corpus for the uuid package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Parsing, formatting, and comparing UUIDs.

## Programs

- `00_basic.go` — Parse/MustParse round-trips, Nil/Max, MarshalText/UnmarshalText, Compare, error paths; random UUIDs pinned by version/variant nibbles only.

## Retest trigger

Any change to uuid parsing, formatting, or comparison.
