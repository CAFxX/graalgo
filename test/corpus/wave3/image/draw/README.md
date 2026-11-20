# `image/draw` wave-3 corpus

Differential corpus for the image/draw package (L11, REVIEW-2026-11-18-05).

## Scope boundary

Compositing images.

## Programs

- `00_basic.go` — Draw with Src and Over, DrawMask with an alpha mask, Floyd-Steinberg dithering; resulting pixels printed.

## Retest trigger

Any change to image/draw compositing or dithering.
