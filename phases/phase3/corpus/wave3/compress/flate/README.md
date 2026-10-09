# `compress/flate` wave-3 corpus

Differential corpus for the synthetic `compress/flate` package (pure-Go
port of gc 1.27.1's DEFLATE implementation, value-identical to gc).

## Scope boundary

The whole package is in scope: `NewWriter`/`NewWriterDict` (all levels),
`Writer` (`Write`/`Flush`/`Close`/`Reset`), `NewReader`/`NewReaderDict`,
`Reader` (`Read`/`Close`), `Resetter`. No `.scopelimited` programs.

## Programs

- `00_roundtrip_levels.go` — every level -2..9 × 0/1/64KB inputs (zeros,
  text, deterministic pseudo-random); compressed output checksummed
  (encoder byte-identity); `NewReader` round-trip; mid-stream `Flush`.
- `01_streaming.go` — 1-byte writes/reads; 3-byte-chunk source; `Reset`
  reuse of Writer/Reader across streams (via `flate.Resetter`).
- `02_dict.go` — 32KB preset dictionary round-trip; wrong dictionary ->
  silently wrong output (raw deflate has no integrity check — pinned as
  divergence, not an error); nil-dict equivalence.
- `03_corrupt.go` — truncated -> `io.ErrUnexpectedEOF` (`==`); garbage ->
  exact `flate:` error strings; empty input -> `io.ErrUnexpectedEOF`
  (a stream with no blocks is truncated).
- `04_adversarial.go` — invalid levels (-3, 99) -> error; 1MB repeated
  byte at HuffmanOnly; all-256-byte-values at every level; Write after
  Close -> error; double Close -> nil.
