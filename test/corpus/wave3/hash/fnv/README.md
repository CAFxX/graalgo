# `hash/fnv` wave-3 corpus

Differential corpus for the synthetic `hash/fnv` package (pure-Go
port of FNV-1/FNV-1a 32/64/128, value-identical to gc; the 128-bit
multiply is a hand-rolled 64x64->128 equivalent to math/bits.Mul64).

## Scope boundary

The whole package is in scope: `New32`/`New32a`/`New64`/`New64a`/
`New128`/`New128a` and each sum's `hash.Hash` +
`encoding.BinaryMarshaler`/`encoding.BinaryUnmarshaler` surface. No
`.scopelimited` programs.

## Programs

- `00_vectors.go` — offset-basis vectors (empty input) and "foobar"
  vectors for all six, Size/BlockSize.
- `01_streaming.go` — chunking invariance (1-byte and odd splits),
  Reset, Sum append, 128-bit BE layout.
- `02_marshal.go` — marshal magic bytes and sizes per hash,
  restore-and-continue, exact corrupt-blob error strings, cross-magic
  rejection.
- `03_adversarial.go` — 1-bit avalanche, FNV-1 vs 1a divergence,
  all-256 byte values, 1MB chunked hashing.
