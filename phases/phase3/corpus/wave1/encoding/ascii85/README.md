# P3-M7 wave-1 corpus: encoding/ascii85

Differential tests for the synthetic `encoding/ascii85` package
(`phases/phase1/frontend/synth_ascii85.go`).

## Programs

| File | Coverage |
|------|----------|
| 00_basic.go | Encode/Decode round-trip, MaxEncodedLen, short input, empty |
| 01_adversarial.go | Corrupt input, CorruptInputError, z/y shortcuts, flush, overflow |

## Limitations

`NewEncoder`/`NewDecoder` (io streaming) are omitted from the synthetic
package (P3-M8). `Decode` is 3-result and exercises the Appendix M
triple desugar (mrM7).

## Running

`./run_differential.sh` (gc leg runs now; the graalgo leg runs at
integration when the p3frontend wiring lands).
