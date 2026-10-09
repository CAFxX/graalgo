# `compress/zlib` wave-3 corpus

Differential corpus for the synthetic `compress/zlib` package (pure-Go
port of gc 1.27.1, value-identical to gc).

## Scope boundary

The whole package is in scope: `NewWriter`/`NewWriterLevel`/
`NewWriterLevelDict`, `Writer` (`Write`/`Flush`/`Close`/`Reset`),
`NewReader`/`NewReaderDict`, `reader` (`Read`/`Close`), `Resetter`. No
`.scopelimited` programs.

## Programs

- `00_roundtrip_levels.go` — every level -2..9 × empty/1-byte/60KB;
  dict round-trip; `Flush` without `Close` -> reader sees
  `io.ErrUnexpectedEOF` (flush/close contract).
- `01_corrupt.go` — bad Adler-32 -> "zlib: invalid checksum"; bad header
  check bits -> "zlib: invalid header"; truncated ->
  `io.ErrUnexpectedEOF`.
- `02_adversarial.go` — invalid levels; empty vs nil dict equivalence;
  `Reset` with a new dict; >32KB dictionary (dict identity is the
  Adler-32 of the full bytes — same 64KB dict works, different dict ->
  "zlib: invalid dictionary"); 1-byte reads.
