# P3-M7 wave-1 corpus: encoding/binary

Differential tests for the synthetic `encoding/binary` package
(`internal/frontend/synth_binary.go`).

## Programs

| File | Coverage |
|------|----------|
| 00_varint.go | Uvarint/Varint round-trips, Put/Append, MaxVarintLen consts, truncation/overflow |
| 01_byteorder.go | LittleEndian/BigEndian/NativeEndian Uint/Put/Append, AppendByteOrder |

## Limitations

`Append`, `Encode`, `Decode`, `Size` need `reflect` and fail fast at
export time (named WS D error); they have exact gc signatures but are
not exercised here. `Uvarint`/`Varint` (multi-result) exercise the
Appendix M desugar (mrM7).

## Running

`make stdlib-gate` (from the repo root; m3-plan D3 deleted the v1 wave runners).
