# `debug/pe` wave-3 corpus

Differential corpus for the debug/pe package (L11, REVIEW-2026-11-18-05).

## Scope boundary

A minimal PE32+ file built in-memory: COFF header fields, optional
header, section listing and data; error paths for a non-PE file and a
missing file.

## Programs

- `00_basic.go` — Minimal PE32+ file built in-memory: COFF/optional header + section data.

## Retest trigger

Any change to debug/pe header parsing, section enumeration, or
imported-symbol reporting.
