# P3-M7 wave-1 corpus: encoding/base32

Differential tests for the synthetic `encoding/base32` package
(`internal/frontend/synth_base32.go`).

## Programs

| File | Coverage |
|------|----------|
| 00_basic.go | Encode/Decode round-trip, HexEncoding, consts, Append variants, NewEncoding |
| 01_padding.go | WithPadding (custom/NoPadding), EncodedLen/DecodedLen |
| 02_adversarial.go | Empty, corrupt input, CorruptInputError, short writes |
| 03_close.go | `NewEncoder` + `Close` with nothing written (nil error, empty output), double `Close` idempotence |

## Limitations

`NewEncoder`/`NewDecoder` are declared (P3-M8) and `encoder.Close` is
real (ported from gc 1.27.1); the streaming `Write`/`Read` methods are
still fail-fast stubs, so no corpus program yet exercises the `Close`
flush path (it needs `Write` to fill the buffer — owed when `Write` is
ported). Multi-result methods (`Decode`, `DecodeString`,
`AppendDecode`) exercise the Appendix M desugar (mrM7).

## Running

`./run_differential.sh` (gc leg runs now; the graalgo leg runs at
integration when the p3frontend wiring lands).
