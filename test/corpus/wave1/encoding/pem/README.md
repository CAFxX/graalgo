# P3-M7 wave-1 corpus: encoding/pem

Differential tests for the synthetic `encoding/pem` package
(`internal/frontend/synth_pem.go`).

## Programs

| File | Coverage |
|------|----------|
| 00_basic.go | EncodeToMemory/Decode round-trip, Block fields, multi-block |
| 01_adversarial.go | Non-PEM input, empty, leading garbage, headerless, empty bytes |

## Notes

`Decode` returns `(p *Block, rest []byte)` — a multi-result pair with a
non-error second result, exercising the Appendix M desugar (mrM7). The
synthetic source imports synthetic `encoding/base64` (uses
`base64.StdEncoding` for the payload and `base64.CorruptInputError`).

## Running

`./run_differential.sh` (gc leg runs now; the graalgo leg runs at
integration when the p3frontend wiring lands).
