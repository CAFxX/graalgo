# `hash/adler32` wave-3 corpus

Differential corpus for the synthetic `hash/adler32` package (pure-Go
port of RFC 1950 Adler-32, value-identical to gc).

## Scope boundary

The whole package is in scope: `Size`, `Checksum`, `New`, and the
digest's `hash.Hash32` + `encoding.BinaryMarshaler`/
`encoding.BinaryUnmarshaler` + `hash.Cloner` surface. No
`.scopelimited` programs.

## Programs

- `00_vectors.go` — published test vectors (empty, "a", "abc",
  "Wikipedia", "123456789"), Size/BlockSize.
- `01_streaming.go` — chunking invariance (1-byte and odd splits),
  Reset, Sum append/BE layout, Write return values, empty Write.
- `02_marshal.go` — marshal mid-stream state, restore and continue,
  AppendBinary, Clone independence, exact corrupt-blob error strings.
- `03_adversarial.go` — nmax=5552 chunking boundaries, 1MB input,
  all-0xff block, Sum idempotence, Reset cycles.
