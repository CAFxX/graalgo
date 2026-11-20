# `debug/dwarf` wave-3 corpus

Differential corpus for the debug/dwarf package (L11, REVIEW-2026-11-18-05).

## Scope boundary

DWARF from the running binary: total entry count, per-tag counts,
first-entry tag/offset; error path on a stripped binary.

## Programs

- `00_basic.go` — DWARF entries from the running binary: counts by tag; stripped-binary error.

## Retest trigger

Any change to debug/dwarf entry parsing, tag reporting, or the
stripped-binary error.
