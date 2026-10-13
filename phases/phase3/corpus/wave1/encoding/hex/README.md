# P3-M7 wave-1 corpus: encoding/hex

Differential tests for the synthetic `encoding/hex` package
(`internal/frontend/synth_hex.go`).

## Programs

| File | Coverage |
|------|----------|
| 00_basic.go | Encode/Decode round-trip, Append variants, Dump, length helpers |
| 01_adversarial.go | Empty, ErrLength, InvalidByteError, uppercase, short writes |

## Limitations

`NewEncoder`/`NewDecoder`/`Dumper` (io streaming) are omitted from the
synthetic package (P3-M8). Multi-result funcs (`Decode`, `AppendDecode`,
`DecodeString`) exercise the Appendix M desugar (mrM7).

## Running

`./run_differential.sh` (gc leg runs now; the graalgo leg runs at
integration when the p3frontend wiring lands).
