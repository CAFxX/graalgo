# `compress/lzw` wave-3 corpus

Differential corpus for the synthetic `compress/lzw` package (pure-Go
port of gc 1.27.1, value-identical to gc).

## Scope boundary

The whole package is in scope: `NewWriter`, `Writer`
(`Write`/`Close`/`Reset`), `NewReader`, `Reader` (`Read`/`Close`/
`Reset`), `Order` (`LSB`/`MSB`). No `.scopelimited` programs.

## Programs

- `00_roundtrip_orders.go` — LSB/MSB × litWidth 2..8 (valid range; 1 is
  rejected — see 01); empty/1-byte/4KB (text + binary) with bytes fitting
  the literal width; compressed output checksummed; garbage input ->
  error on first Read.
- `01_corrupt.go` — truncated mid-code -> `io.ErrUnexpectedEOF`;
  litWidth 0/1/9/100 at construction -> "lzw: litWidth %d out of range"
  on Read; unknown order -> "lzw: unknown order"; writer with bad
  litWidth errors on Write.
- `02_adversarial.go` — 1-byte reads; `Reset` reuse across order
  changes; empty input; `Close` idempotence; byte 0xff with litWidth 8
  (exceeds max literal) -> writer error.
