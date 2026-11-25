# P3-M7 wave-1 corpus: encoding/base64

Differential tests for the synthetic `encoding/base64` package
(`internal/frontend/synth_base64.go`).

## Programs

| File | Coverage |
|------|----------|
| 00_basic.go | Encode/Decode round-trip, URLEncoding, Raw variants, Append, NewEncoding |
| 01_strict.go | Strict(), WithPadding, custom alphabet |
| 02_adversarial.go | Empty, corrupt input, CorruptInputError, bad padding |

## Limitations

`NewEncoder`/`NewDecoder` (io streaming) are omitted from the synthetic
package (P3-M8). Multi-result methods (`Decode`, `DecodeString`,
`AppendDecode`) exercise the Appendix M desugar (mrM7).

## Running

`make stdlib-gate` (from the repo root; m3-plan D3 deleted the v1 wave runners).
