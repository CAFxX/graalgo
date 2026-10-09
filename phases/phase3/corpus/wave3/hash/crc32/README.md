# `hash/crc32` wave-3 corpus

Differential corpus for the synthetic `hash/crc32` package (pure-Go
port; the stdlib's slicing-by-8/arch fast paths are not ported — they
are value-preserving, so the simple table walk is gc-identical).

## Scope boundary

The whole package is in scope: `Size`, `IEEE`/`Castagnoli`/`Koopman`,
`Table`, `IEEETable`, `MakeTable`, `New`, `NewIEEE`, `Update`,
`Checksum`, `ChecksumIEEE`, and the digest's `hash.Hash32` +
`encoding.BinaryMarshaler`/`encoding.BinaryUnmarshaler` +
`hash.Cloner` surface. No `.scopelimited` programs.

## Programs

- `00_vectors.go` — published vectors (IEEE/Castagnoli of
  "123456789"), Koopman/custom-poly differential values, table pointer
  identity, Size/BlockSize.
- `01_streaming.go` — Update chaining, per-polynomial chunking
  invariance, Reset, Sum BE layout, Write return values.
- `02_marshal.go` — mid-stream marshal/restore, AppendBinary, Clone,
  exact corrupt-blob error strings incl. table mismatch.
- `03_adversarial.go` — sizes around gc's slicing-by-8 thresholds
  (63..65536), nonzero-crc Update chaining, 100KB Castagnoli bulk,
  Reset cycles.
