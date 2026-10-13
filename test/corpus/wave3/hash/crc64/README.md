# `hash/crc64` wave-3 corpus

Differential corpus for the synthetic `hash/crc64` package (pure-Go
port; the stdlib's slicing-by-8 fast path is not ported — it is
value-preserving, so the simple table walk is gc-identical).

## Scope boundary

The whole package is in scope: `Size`, `ISO`/`ECMA`, `Table`,
`MakeTable`, `New`, `Update`, `Checksum`, and the digest's
`hash.Hash64` + `encoding.BinaryMarshaler`/`encoding.BinaryUnmarshaler`
+ `hash.Cloner` surface. No `.scopelimited` programs.

## Programs

- `00_vectors.go` — published vectors (ECMA/ISO of "123456789"),
  custom-poly differential value, table pointer identity,
  Size/BlockSize.
- `01_streaming.go` — Update chaining, per-polynomial chunking
  invariance, Reset, Sum BE layout, Write return values.
- `02_marshal.go` — mid-stream marshal/restore, AppendBinary, Clone,
  exact corrupt-blob error strings incl. table mismatch.
- `03_adversarial.go` — sizes around gc's 64-byte slicing-by-8
  threshold, nonzero-crc Update chaining, 100KB bulk, Reset cycles.
