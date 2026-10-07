# Red-Green Evidence — P3-M7 m7-wenc-core (8 encoding/* primitives)

Branch `m7-wenc-core`. Every test below was demonstrated capable of
failing: each red was observed (not hypothesized) by breaking the
corresponding code and re-running.

## 1. Phase-1 exporter tests (`phases/phase1/frontend/synth_enccore_export_test.go`)

### TestEnccoreExportPerPackage (8 subtests, one per package)

- **What:** exports a program importing the package; asserts the
  expected synthetic members appear (`base64.StdEncoding`,
  `binary.LittleEndian`, `hex.ErrLength`, `asn1.TagNull`, …) and the
  io-only members do NOT (`base32.NewEncoder`).
- **Red observed:** with the package missing from `isMultiResultSynthetic`
  / `exportSyntheticPkgCall`, `base64.StdEncoding` failed to resolve
  ("undefined: base64.StdEncoding") — the pre-fix exporter state.
- **Green:** all 8 export; synthetic members present; streaming APIs
  absent.

### TestEnccoreDesugarWires

- **What:** 2-target `enc.Decode(dst, src)` / `hex.DecodeString`,
  3-target `ascii85.Decode`, non-error-pair `pem.Decode`, and the
  `=`-form `n = enc.Decode(dst, src)` all export (desugar fires);
  the desugared forms do NOT appear in the output (no `__` helpers,
  no pair structs leak).
- **Red observed (non-tautological):** the table initially listed a
  `base32AppendPair` for `AppendDecode` (copied from a WIP sketch) —
  but the source's `AppendDecode` returns `base32DecodeStringPair`,
  so the test failed on the pair-struct name. The failure caught a
  real table error before it shipped.
- **Green:** all desugar shapes export; no `__`-helper or pair-struct
  references in the emitted code.

### TestEnccoreFailFast

- **What:** `binary.Append`, `asn1.Unmarshal` (2-target), and an
  interface method (`encoding.BinaryMarshaler.MarshalBinary`) fail
  with the named M7 errors; asserts the error STRINGS
  ("binary.Append is not in M7 scope", etc.).
- **Red observed:** before `binary` was added to the exportSelector
  qualified-ident allow-list, `binary.LittleEndian` failed with
  "undefined" instead of reaching the fail-fast — the test
  distinguishes "not wired" from "wired and failing fast".
- **Green:** all three fail with the exact named errors.

### TestEnccoreUserMethodsUnaffected

- **What:** a user type with a method named `Decode` does NOT get the
  Appendix M desugar (the classifier keys on the declaring package +
  receiver named type).
- **Red observed:** an early classifier keyed on method name alone
  desugared the user method — the test caught it; the fix (package +
  receiver keying) is what ships.
- **Green:** user `Decode` untouched; synthetic `Decode` desugared in
  the same program.

### TestStringsImportStillWorks (regression)

- **What:** `strings.ToUpper` exports (guards the pre-existing bug fix:
  `"strings"` was in the WS G allow-list but missing from the
  importer-selection list and `exportSyntheticPkgCall`, so NO program
  importing `strings` could export at all).
- **Red observed:** on the pre-fix tree this test fails with
  "undefined: strings.ToUpper" — reproduced before applying the fix.
- **Green:** exports.

## 2. p3frontend drift tests (`phases/phase3/src/p3frontend/enccore_test.go`)

### TestEncCoreSyntheticSignatures (8 subtests)

- **What:** mirror vs the REAL phase-1 synthetic source: exported
  scope names + rendered types, package-level funcs, named-type method
  sets, and the unexported Appendix M pair/triple structs (field names
  AND types, positionally — the desugar extracts by field index).
- **Red observed:** 6 of 8 packages passed on the first mirror draft.
  base32/base64 failed — not a mirror bug but a test bug: the test
  checked ALL methods including the unexported `decode` (an
  implementation detail the mirror correctly omits). The failure forced
  the exported-methods-only rule (the bytes_test precedent), which also
  correctly covers binary's unexported endian types (their EXPORTED
  methods are user-visible via the package vars). A second red: the
  test's pair-struct table is verified against the source, so a typo'd
  pair name fails fast instead of silently passing.
- **Green:** all 8 packages match declaration-by-declaration.

### TestEncCoreOmissionsPinned

- **What:** the 9 io-streaming omissions (`NewEncoder`/`NewDecoder`/
  `Dumper`) are absent from BOTH the mirror and the phase-1 source;
  each carries a reason code from the exclusions.yaml vocabulary
  (validated against the vocabulary).
- **Red observed:** the first draft wrongly listed `asn1.Unmarshal`
  as an omission (UNSUPPORTED_FEATURE) — but Unmarshal is IN the API
  on both sides (exact signature, fail-fast body); only its DESUGAR is
  omitted. The test failed, forcing the correction: the omission list
  now covers only the 9 io-streaming APIs, with a comment pinning where
  the Unmarshal-desugar decision is tested (phase-1
  `TestEnccoreFailFast`). A bogus reason code also fails the vocabulary
  check.
- **Green:** all omissions pinned with valid reason codes.

### TestEncCoreImportResolvesSynthetic / TestEncCoreImporterReturnsMirror

- **What:** the loader resolves all 8 as `kindSynthetic` (never
  GOROOT); the importer serves each mirror; importing `encoding/pem`
  caches `encoding/base64` (the synthetic→synthetic dependency).
- **Red observed:** before the loader list was updated, `encoding/pem`
  resolved to `kindStdlib` — the test failed. The pem/base64 caching
  assertion failed when the importer case built pem without ensuring
  base64.
- **Green:** all resolve synthetic; mirrors served; base64 cached.

## 3. Pre-existing p3frontend blockers (fixed to unblock the suite)

These were not in the 8-package scope but blocked EVERY p3frontend
test (the package did not build on master, so no test ever ran there).
Each fix is against the phase-1 synthetic source as authority:

1. `synth_xml.go`: 5 unused `types.NewNamed` vars → `_ =`
   (never compiled). **Red:** `go build` failed.
2. `synth_strings.go`: `p[1].SetName` — `*types.Var` has no `SetName`
   in go1.27 → name the param directly. **Red:** `go build` failed.
3. `synth_strings.go`: `NewSignatureType` recv args were
   `*types.Pointer`, need `*types.Var` → wrapped in `NewParam`.
   **Red:** `go build` failed.
4. `synth_strings.go` / `synth_xml.go` / `synth_bytes.go`: `byte`
   rendered as `uint8` (`types.Typ[types.Byte]`) vs source's `byte`;
   func params named `r` vs source's unnamed. **Red:** drift tests
   failed with `[]uint8` vs `[]byte`, `func(r rune)` vs `func(rune)`.
5. `synth_xml.go`: `Decoder` missing exported fields (Strict, …),
   `SyntaxError` missing `Line`, Token-typed positions used `any`.
   **Red:** drift tests failed field-by-field.
6. `export_test.go`: `TestStdlibScopeError` expected `strings` to be
   stdlib — stale since strings became synthetic → switched to `os`.
   **Red:** kind mismatch.

After these, the FULL p3frontend suite is green (was unbuildable).

## 4. Differential corpus (`phases/phase3/corpus/wave1/encoding/*/`)

17 programs across the 7 function-bearing packages (`encoding`
top-level has no functions — no corpus dir). Each program:

- compiles and runs clean under stock gc 1.27.1 (verified 2026-10-07;
  4 initial program bugs fixed: short-buffer panics, 2-value
  assignment mismatch);
- uses only exporter-supported constructs (multi-result calls use the
  `:=`/`=` desugar forms; no fail-fast members);
- ships with `run_differential.sh` (thin wrapper over `run_wave.sh`)
  and a `README.md` documenting coverage and limitations.

The graalgo leg cannot run pre-integration (the runner hardcodes
`GR=$HOME/workspace/graalgo`, whose p3frontend lacks this wiring);
it runs at integration. The gc leg is the executable specification
until then.

## 5. Full-suite results (2026-10-07)

- `phases/phase1/frontend`: `go test .` → **ok** (all packages,
  incl. the 5 new enc-core tests).
- `phases/phase3/src/p3frontend`: `go test -count=1 .` → **ok**
  (~103s; all drift tests green, incl. the 4 new enc-core tests).

## Carried divergences

- `exportSyntheticPkgCall` intentionally does NOT include
  `encoding/xml` (pre-existing; `xml.CopyToken` etc. still fail
  "selector call not supported") — out of scope, noted for the
  xml-wave owner.
- The `encoding` top-level package has no package-level funcs — no
  desugar entries, no corpus dir; interface methods fail fast.
- Corpus graalgo legs are integration-gated (runner hardcodes the
  integration worktree path).
