# `image/png` wave-3 corpus

Differential corpus for the image/png package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Encoding and decoding PNGs.

## Programs

- `00_basic.go` — RGBA gradient round-trip with exact pixels, compression-level byte counts, and error paths.

## Retest trigger

Any change to image/png encoding or decoding.
