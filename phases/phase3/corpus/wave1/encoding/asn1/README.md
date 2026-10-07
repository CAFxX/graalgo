# P3-M7 wave-1 corpus: encoding/asn1

Differential tests for the synthetic `encoding/asn1` package
(`phases/phase1/frontend/synth_asn1.go`).

## Programs

| File | Coverage |
|------|----------|
| 00_basic.go | Marshal (int/string/struct/params), BitString, ObjectIdentifier, Enumerated, Flag, RawValue, consts, error types |
| 01_adversarial.go | StructuralError on unsupported types, bad params, nesting, bools, empty OID |

## Limitations

`Unmarshal`/`UnmarshalWithParams` fail fast at export time (decoding
stores through the recipient pointer — v1-unexpressible, WS D); they
have exact gc signatures but are not exercised here. `Marshal`/
`MarshalWithParams` (multi-result) exercise the Appendix M desugar
(mrM7).

## Running

`./run_differential.sh` (gc leg runs now; the graalgo leg runs at
integration when the p3frontend wiring lands).
