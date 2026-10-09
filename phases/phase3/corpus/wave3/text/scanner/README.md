# `text/scanner` wave-3 corpus

Differential corpus for the synthetic `text/scanner` package.

## Scope boundary

Deviations (see `synth_textscanner.go` header):
- `unicode/utf8` inlined as `__ts*` helpers.
- `unicode.IsLetter`/`IsDigit`: ASCII-ONLY. Non-ASCII letters/digits are
  not recognized. Retest trigger: full unicode tables (tier1a).

## Programs

- `00_basic.go` — Scan Go-like source, TokenText, Position.

## Results

TBD (2026-10-09).
