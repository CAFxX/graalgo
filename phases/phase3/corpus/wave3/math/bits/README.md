# `math/bits` wave-3 corpus

Differential corpus for the synthetic `math/bits` package (full port).

## Scope boundary

The whole package is in scope. Deviations (see `synth_mathbits.go` header):
- `overflowError`/`divideError` linkname vars → string panics with exact gc messages.
- 12 multi-result funcs via Appendix M.
- `ReverseBytes32` rewritten with explicit bytes (frontend bug workaround).

## Programs

- `00_leading_trailing.go` — LeadingZeros/TrailingZeros across widths.
- `01_onescount.go` — OnesCount/OnesCount8 tables.
- `02_rotate_reverse.go` — RotateLeft/Reverse/ReverseBytes.
- `03_add_sub.go` — Add/Sub with carry/borrow (2-target desugar).
- `04_mul_div.go` — Mul/Div/Rem (2-target desugar).
- `05_div_panic.go` — Div by zero panic (`.panic` marker).
- `06_div_overflow_panic.go` — Div overflow panic (`.panic` marker).
- `07_adversarial.go` — Composition properties, bit walks.

## Results

16 passed, 0 scope-limited, 0 failed (2026-10-09).
