# `image/jpeg` wave-3 corpus

Differential corpus for the image/jpeg package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Encode a generated gradient, Decode it back, DecodeConfig; bounds + sample pixels compared with a tolerance (JPEG is lossy; exact pixels are not pinned). No external files. No .scopelimited programs. Only baseline JPEG via the std encoder is exercised; progressive/CMYK inputs are out of scope.

## Programs

- `00_roundtrip.go` — Encode gradient, Decode round-trip, bounds + tolerant pixel checks.

## Retest trigger

jpeg encoder/decoder output changes beyond the tolerance.
