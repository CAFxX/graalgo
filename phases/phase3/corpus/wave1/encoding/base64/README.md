# P3-M7 wave-1 corpus: encoding/base64

Differential tests for the synthetic `encoding/base64` package
(`phases/phase1/frontend/synth_base64.go`).

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

`./run_differential.sh` (gc leg runs now; the graalgo leg runs at
integration when the p3frontend wiring lands).
