# P3-M7 encoding/json: red-green evidence

## What was built

`phases/phase1/frontend/synth_json.go` (const `syntheticJsonSource`):
real-Go implementation of package `encoding/json` with exact gc 1.27.1
public API signatures, pure Go, single import (`bytes`, for the `Indent`
signature only). Clean-room implementation over the any-model
(dynamic types: nil, bool, string, int/uint/float64, []any,
map[string]any, RawMessage, Number).

## Green: exporter + p3frontend wiring (2026-10-07)

**Phase-1 exporter** (`phases/phase1/frontend/`): `export.go`
(overlayImporter, WS G, needJson, qualified-ident),
`synth_machinery.go` (synthetic call paths),
`mrdesugar_m5.go` (`mrJson`/`mrJsonMethod`, `classifyJsonFunc`,
`isJsonDesugarMethod`, plan-arity generalization).

**Key refactor**: the synthetic source contained 14 internal
multi-result functions (v1 cannot emit multi-result functions at all).
All were mechanically refactored to pair-struct returns
(`jsonDecomposePair`, `jsonDecimalPair`, `jsonShortestPair`,
`jsonFormatFloatPair`, `jsonDecodeRunePair`, `jsonValuePair`,
`jsonStringPair`, `jsonReadAllPair`, `jsonEscapePair`), preserving names
and logic. Call sites updated.

**p3frontend** (`phases/phase3/src/p3frontend/`): `synth_json.go`
generated mirror (`syntheticJsonPackage()`), `importer.go` case,
`loader.go` synthetic marker, `json_test.go` drift test.

**Tests** (all PASS 2026-10-07):
- `TestSyntheticJsonM7`, `TestSyntheticJsonArityFailsFast`;
  full `phases/phase1/frontend` suite green.
- `TestJsonSyntheticSignatures`, `TestJsonImporterRegistersEncodingJson`;
  full `p3frontend` suite green.

**Corpus**: `phases/phase3/corpus/wave1/encoding/json/` (3 programs:
marshal/valid/adversarial over the honest any-model subset), with
`run_differential.sh` vs gc 1.27.1. Byte-identical for the supported
subset (map keys sorted, shortest float repr, HTML escaping on).

## Honest scope (M7)

Reachable from v1 user code: `Marshal`, `MarshalIndent`, `Valid`,
`Number` (String/Float64/Int64), `Delim`, `RawMessage`, error types,
`Token`/`Marshaler`/`Unmarshaler` (types only).

Fail-fast (exact gc signatures, panic bodies):
- `Unmarshal`, `Decoder.Decode`, `RawMessage.UnmarshalJSON`: need
  store-through-pointer (WS D).
- `Indent`: needs `*bytes.Buffer` via `&buf` (WS D). **Signature
  corrected** from `([]byte,[]byte,string,string)([]byte,error)` to gc's
  `(*bytes.Buffer,[]byte,string,string) error`.
- `Encoder.Encode`, `jsonReadAll`: need 2-result interface methods (WS D).
- `NewDecoder`/`NewEncoder`: unreachable (need user 2-result Read/Write).

## Carried divergences

- **Error messages**: match gc 1.27.1 (v2/jsontext-based) empirically
  via the differential corpus, not the vendored v1 source. If the
  toolchain changes json error text, the corpus will catch it.
- **NaN payloads**: float formatting uses shortest-repr; NaN marshals
  as an error (like gc). No divergence expected.
- **Streaming**: `Decoder`/`Encoder` types exist with correct signatures
  but cannot be constructed or used in v1 (WS D). Re-test when WS D lands.

## P3-M7-GAP-2: json.Marshal float32 precision (found by code review 2026-10-07)
- `json.Marshal(float32(3.14))` produces `"3.140000104904175"`; gc 1.27.1 produces `"3.14"`.
- Root cause: `synth_json.go:1821-1828` converts float32 to float64 then formats with float64-shortest. gc uses float32-shortest.
- Fix requires float32-specific shortest formatting (not available in synthetic package's imports).
- Re-test trigger: differential corpus with float32 values.

## P3-M7-GAP-3: json.Marshal([]byte) panics (found by code review 2026-10-07)
- `json.Marshal([]byte{1,2,3})` panics; gc base64-encodes to `"AQID"`.
- Root cause: missing case in `marshalValue` for `[]byte`.
- Re-test trigger: corpus with []byte values.

## P3-M7-GAP-4: json.Decoder.Buffered() silent stub (found by code review 2026-10-07)
- `synth_json.go:2291` returns empty instead of buffered data.
- This is a silent stub (not a fail-fast) — violates M7 "no silent stubs" rule.
- Re-test trigger: streaming decoder corpus.
