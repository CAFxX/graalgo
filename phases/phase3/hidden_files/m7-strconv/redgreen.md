# P3-M7 strconv: red-green evidence

## What was built

`phases/phase1/frontend/synth_strconv.go` (committed as `12e5fcd` on
`m7-math`): real-Go implementation of package `strconv` with exact gc
1.27.1 public API signatures (~111 exported symbols), pure Go, single
import (`errors`). Ports gc's `strconv/*.go` + `internal/strconv/*.go`
algorithms to frontend/v1-supported constructs.

Work file: `phases/phase3/hidden_files/m7-strconv/work/strconv.go`
(development source; the committed const is generated from it).

## Green: differential vs gc 1.27.1

Harness: `phases/phase3/hidden_files/m7-strconv/work/difftest/main.go`
(compares stdlib `strconv` vs the port on identical inputs; fails on any
mismatch).

Result: **ALL DIFFERENTIAL CHECKS PASSED** (2026-10-07).

Coverage:
- **ParseInt/ParseUint**: 40+ targeted cases (MaxInt64±1, MaxUint64±1,
  base prefixes 0x/0o/0b, underscores incl. invalid `1__0`/`_1`/`0x_1`,
  bit sizes 0/8/16/32/64, invalid base/bitSize) — values AND error
  messages AND `*NumError` type all match.
- **Atoi**: 12 cases incl. overflow, `0x10` (base-10 only), underscores.
- **FormatInt/FormatUint/AppendInt/AppendUint**: bases 2/8/10/16/36,
  MinInt64, MaxUint64 — byte-identical.
- **Itoa**: incl. `1<<62`.
- **ParseBool/FormatBool/AppendBool**: all 8 spellings + invalid.
- **ParseFloat**: 45 targeted strings (subnormals `5e-324`,
  `2.4703282292062327e-324`, MaxFloat64±1ulp, `1e309` overflow,
  hex floats `0x1.8p1`, `0x1p-2`, underscores, `+nan` (gc quirk:
  syntax error — preserved), `Inf`/`Infinity` variants) × bitSizes
  32/64 — **bit-exact** (compared via `math.Float64bits`).
- **ParseFloat fuzz**: 20,000 random float64 bit patterns (biased to
  exponent-only and near-1.0) round-tripped through shortest `FormatFloat`
  — bit-exact.
- **FormatFloat**: 18 values × 8 formats (`b/e/E/f/g/G/x/X`) × 7
  precisions × bitSizes 32/64 — byte-identical, incl. `x`/`X` hex,
  subnormals, `1e21`/`1e22` transition, Inf/NaN.
- **FormatFloat fuzz**: 5,000 random bit patterns × 5 formats ×
  precisions -1/rand(20) × bitSizes 32/64 — byte-identical.
- **ParseComplex/FormatComplex**: 16 strings × 64/128, 5 values ×
  formats — match (NaN-aware comparison).
- **Quote family**: 16 strings (control chars, `\x7f`, `é`, `→`,
  `日本語`, BOM, lone surrogate bytes, backtick, `\\`) ×
  Quote/QuoteToASCII/QuoteToGraphic + Append variants — byte-identical.
- **QuoteRune family**: 11 runes (incl. `0x110000`, `-1`, `0xD800`,
  `U+2028`) × 6 functions — byte-identical.
- **CanBackquote**: all quote cases — match.
- **Unquote/QuotedPrefix**: 30 adversarial strings (bad escapes,
  `\x1`, `\U00110000`, `\400`, unterminated, trailing junk, `\r` in
  raw strings, `"\x80"`) — values match; errors are `== ErrSyntax`
  (public var, so `errors.Is(err, strconv.ErrSyntax)` holds).
- **UnquoteChar**: 18 cases × quote contexts — all 4 results match.
- **IsPrint/IsGraphic**: full sweep U+0000–U+2FFF + spot checks to
  U+10FFFF — match.
- **errors.Is/As**: `*NumError` type, `.Err`/`.Unwrap()` chain verified.

## Red (mutation evidence): the tests are not tautological

1. **NaN payload**: first run FAILED on `ParseFloat("NaN")` —
   port returned `0xfff8000000000000` (hardware `0/0`), gc returns
   `0x7ff8000000000001`. Fixed via `-(z/z)` (deterministic positive
   QNaN); 1-bit residual payload difference documented as carried
   divergence (unobservable via strconv API). The test caught a real
   behavioral difference.
2. **Value-semantics refactor**: converting `*decimal`/`*scale`/
   `*floatInfo` to values (v1 has no `&`) initially broke
   `decodeRuneInString` call sites (regex captured `//` comments into
   composite literals) — `go build` failed; fixed. The difftest then
   confirmed bit-exactness was preserved through the refactor.
3. **`err != errRange`**: comparing `error` interface with `parseError`
   (int) needs an interface conversion the v1 backend cannot build
   (`binary.fields: interface conversion needs type ids`). Fixed via
   package-level `var errRangeError error = errRange`. Caught by
   end-to-end export, not by gc differential (gc allows it).
4. **In-function `const`**: v1 rejects `const` declarations inside
   function bodies (`__parseUint` had `const maxUint64`). Fixed by
   precomputing. Caught by export.

## Carried divergences

1. **NaN payload 1 bit**: synthetic `nanF64()` = `0x7ff8000000000000`;
   gc = `0x7ff8000000000001`. Unobservable through strconv's API
   (comparisons, `FormatFloat` → `"NaN"`, float32 conversion all
   payload-insensitive). Re-test trigger: if a bit-exact NaN source
   becomes available in v1 scope.
2. **Branch**: work done on `m7-math`, not `master` (repo was on
   `m7-math` at task start; brief said `master`/`a4e9aea`).

## Wiring: phase-1 exporter + p3frontend (P3-M7 wiring worker, 2026-10-07)

The exporter integration is LANDED on branch `m7-wstr-json`:

**Phase-1 exporter** (`phases/phase1/frontend/`):
- `export.go`: `overlayImporter` cases for `"strconv"`/`"encoding/json"`;
  WS G allow-list extended; `needStrconv`/`needJson` import scanning;
  `importSyntheticPackage` selection; qualified-ident routing for
  `strconv.ErrSyntax`/`ErrRange` vars and `IntSize` const.
- `synth_machinery.go`: `exportSyntheticPkgCall`,
  `isMultiResultSynthetic`, `multiResultSyntheticError` path lists
  extended with both packages.
- `mrdesugar_m5.go`: new kinds `mrStrconv`, `mrJson`, `mrJsonMethod`;
  `classifyStrconvFunc` (ParseUint→`__parseUintPub`/parseUintPair etc.,
  UnquoteChar is 4-result); `classifyJsonFunc`
  (Marshal/MarshalIndent→`__marshal`/`__marshalIndent`); `isJsonDesugarMethod`
  (Number.Float64/Int64, Decoder.Token, RawMessage.MarshalJSON);
  plan-arity generalization for UnquoteChar's 4 results.

**p3frontend** (`phases/phase3/src/p3frontend/`):
- `synth_strconv.go`, `synth_json.go`: generated mirrors
  (`syntheticStrconvPackage()`, `syntheticJsonPackage()`) pinning exact
  signatures incl. `IntSize` const and unexported `jsonReader`/`jsonWriter`
  interfaces (for signature rendering).
- `importer.go`: cases for `"strconv"`, `"encoding/json"`.
- `loader.go`: synthetic markers for both paths.
- `strconv_test.go`, `json_test.go`: drift tests (incl. `*types.Const`
  case for `IntSize`).

**Tests** (all PASS 2026-10-07):
- Phase-1: `TestSyntheticStrconvM7`, `TestSyntheticStrconvArityFailsFast`,
  `TestSyntheticJsonM7`, `TestSyntheticJsonArityFailsFast`;
  full `phases/phase1/frontend` suite green.
- p3frontend: `TestStrconvSyntheticSignatures`,
  `TestJsonSyntheticSignatures`, plus importer spot-checks;
  full `p3frontend` suite green.

**Corpus**: `phases/phase3/corpus/wave1/strconv/` (3 programs:
parse/format/quote/adversarial) and
`phases/phase3/corpus/wave1/encoding/json/` (3 programs: marshal/valid/
adversarial over the any-model honest subset), each with
`run_differential.sh` vs gc 1.27.1.

## Carried divergences

- **NaN payloads**: `ParseFloat("NaN")` etc. produce a canonical NaN;
  gc may preserve payload bits in some paths. The differential uses
  NaN-aware comparison (both are NaN); bit-exact NaN payloads are not
  guaranteed. (Pre-existing, from the strconv worker.)
- **`json.Indent` signature corrected**: the synthetic source had
  `func Indent(dst []byte, src []byte, prefix, indent string) ([]byte, error)`;
  gc 1.27.1 is `func Indent(dst *bytes.Buffer, src []byte, prefix, indent string) error`.
  Fixed to the gc signature (fail-fast body; unreachable in v1 — needs `&buf`).
- **`json.Unmarshal`/`Decoder.Decode` fail-fast**: need
  store-through-pointer (`*p = val`), which frontend/v1 cannot emit (WS D).
- **`json.NewDecoder`/`NewEncoder` unreachable**: need user-defined
  2-result Read/Write methods (WS D).
- **`json.Encoder.Encode`/`jsonReadAll` fail-fast**: need 2-result
  interface methods (WS D).
