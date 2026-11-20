# `image/gif` wave-3 corpus

Differential corpus for the image/gif package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Encoding and decoding GIFs.

## Programs

- `00_basic.go` — Single-frame round-trip with exact pixels, animated EncodeAll, and error paths on truncated/non-GIF data.

## Retest trigger

Any change to image/gif encoding or decoding.
