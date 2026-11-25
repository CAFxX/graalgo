# P3-M7 wave-1 corpus: encoding/ascii85

Differential tests for the synthetic `encoding/ascii85` package
(`internal/frontend/synth_ascii85.go`).

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

`make stdlib-gate` (from the repo root; m3-plan D3 deleted the v1 wave runners).
