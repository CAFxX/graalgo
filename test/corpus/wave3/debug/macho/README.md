# `debug/macho` wave-3 corpus

Differential corpus for the debug/macho package (L11, REVIEW-2026-11-18-05).

## Scope boundary

A minimal 64-bit Mach-O file built in-memory: header fields,
segment decoding; error paths for a non-Mach-O file and a missing
file.

## Programs

- `00_basic.go` — Minimal Mach-O 64 file built in-memory: header + segment decoding.

## Retest trigger

Any change to debug/macho header parsing, load-command decoding,
or segment reporting.
