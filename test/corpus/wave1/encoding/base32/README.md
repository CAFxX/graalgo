# P3-M7 wave-1 corpus: encoding/base32

Differential tests for the synthetic `encoding/base32` package
(`internal/frontend/synth_base32.go`).

## Programs

| File | Coverage |
|------|----------|
| 00_basic.go | Encode/Decode round-trip, HexEncoding, consts, Append variants, NewEncoding |
| 01_padding.go | WithPadding (custom/NoPadding), EncodedLen/DecodedLen |
| 02_adversarial.go | Empty, corrupt input, CorruptInputError, short writes |

## Limitations

`NewEncoder`/`NewDecoder` (io streaming) are omitted from the synthetic
package (P3-M8). Multi-result methods (`Decode`, `DecodeString`,
`AppendDecode`) exercise the Appendix M desugar (mrM7).

## Running

`make stdlib-gate` (from the repo root; m3-plan D3 deleted the v1 wave runners).
