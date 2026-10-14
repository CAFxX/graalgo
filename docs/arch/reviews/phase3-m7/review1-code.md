# P3-M7 Code Review (reviewer 1 — code)

**Scope:** phase-1 synthetic sources, `mrdesugar_m5.go`, exporter wiring
(`export.go`, `synth_machinery.go`), p3frontend mirrors + drift tests,
wave-1 differential corpus.
**Tree reviewed:** `~/workspace/graalgo-m7-int`, branch `master`,
HEAD `ea95f49` ("P3-M7: fix corpus runners, restore redgreen evidence").
**Method:** adversarial — every functional claim below was verified
empirically (built `/tmp/gofrontend` from the reviewed tree; extracted
synthetic sources and ran them under stock gc 1.27.1 differentially).
**Verdict: REQUEST_CHANGES.**

Note: HEAD moved `2331e20` → `ea95f49` *during* this review (concurrent
worker). All findings below were re-verified against `ea95f49`. The tree
is otherwise clean (`git status`: only untracked `reviews/phase3-m7/`).

---

## 1. Critical: `encoding/csv` and `encoding/gob` are unusable — missing `needErrors` implication

`phases/phase1/frontend/export.go:869-873`:

```go
case "encoding/csv":
    needCsv = true
case "encoding/gob":
    needGob = true
```

The synthetic csv/gob source (`synth_enc_csv_gob.go`) does
`import "errors"` and its package-var initializers call `errors.New`
(e.g. lines 78–86: `ErrBareQuote`, `ErrQuote`, `ErrFieldCount`,
`ErrTrailingComma`, `csvErrInvalidDelim`, `csvEOF`; line 1789
`gobErrEOF`). But unlike `bytes` (line ~855: `needBytes = true;
needErrors = true`) and `strconv` (`needStrconv = true; needErrors =
true`), the csv/gob cases do **not** set `needErrors = true`. The
synthetic `errors` package is therefore never registered in `funcByObj`,
and `importSyntheticPackage` fails while emitting csv's globals:

```
$ gofrontend csvonly.go   # imports only encoding/csv, uses NewReaderString + Writer
gofrontend: gofrontend: call to unknown function New      # exit 2
$ gofrontend gobonly.go   # imports only encoding/gob
gofrontend: gofrontend: call to unknown function New      # exit 2
```

Any program importing *only* `encoding/csv` or *only* `encoding/gob`
(a completely ordinary thing to do) is rejected. The failure message
("unknown function New") gives no hint about the missing implication.
**Fix:** set `needErrors = true` alongside `needCsv`/`needGob`
(the bytes/strconv precedent, including the "NB: requires …"
comments).

(I checked the analogous `encoding/json → bytes` implication: not
needed — `synth_json.go`'s `import "bytes"` is used only in the
`Indent`/`HTMLEscape` *signatures* (`*bytes.Buffer`), never in an
emitted body; `json.Valid`-only programs export fine. Verified
empirically.)

---

## 2. High: csv desugar removed, but the synthetic source still promises it

Commit `ea95f49` deleted the `mrCsvRead`/`mrCsvFieldPos` classification
from `mrdesugar_m5.go` (26-line deletion). That deletion was the right
call for the code as it stood — the classification was broken two ways:

1. `helperName: "csvReadPair"` named the pair *type*, not a helper, so
   every `rec, err := r.Read()` failed with `synthetic helper
   encoding/csv.csvReadPair missing` (the `*types.Func` assertion in
   `desugarMultiResult` cannot succeed on a `TypeName`);
2. `Read` and `ReadAll` shared one plan, but they need different
   helpers *and* different pair types (`__csvRead`/`csvReadPair{rec,
   err}` vs `__csvReadAll`/`csvReadAllPair{recs, err}` — note the field
   is `recs`, not `rec`).

But the removal was not carried through to the synthetic source, which
still promises the desugar in four places:

- `synth_enc_csv_gob.go:22-24` (file header): "their 2-target call sites
  desugar (Appendix M pattern, mrdesugar_m5.go) to the single-result
  `__csvRead`/`__csvReadAll`/`__csvFieldPos` helpers";
- line 694 comment + line 698 panic message: "only the `rec, err :=
  r.Read()` 2-target form desugars";
- line 728 comment + line 730 panic: same for `ReadAll`;
- line 755 comment + line 757 panic: same for `FieldPos`.

All four are now false: the 2-target forms fail fast with the M7 scope
error via `isMultiResultSynthetic`, exactly like every other position.
The `__csvRead` / `__csvReadAll` / `__csvFieldPos` helpers and the
`csvReadPair` / `csvReadAllPair` / `csvFieldPosPair` structs are now
dead code (still correct — my differential test below passes — but
unreachable). **Fix:** update the header + three comment/panic-message
sites to the fail-fast-everywhere wording (or re-implement the desugar
correctly; either way the docs must match the code).

Related dead-definition note: `mrCsvFieldPos` never had a
classification path even before the removal (the kind existed in the
const block and the desugar switch, but no classifier produced it) —
the source's "FieldPos desugars" promise was never true.

---

## 3. High: strconv/json desugar still missing (P3-M7-GAP-1) — sources promise it, corpus needs it

The synthetic sources promise desugars that `mrdesugar_m5.go` does not
implement (no `m7MultiResultFuncs` entries, no classification):

- `synth_strconv.go` header: "Two/four-target call sites desugar via
  mrdesugar_m5.go"; `ParseBool`/`ParseInt`/`ParseUint`/`ParseFloat`/
  `ParseComplex`/`Atoi`/`Unquote`/`UnquoteChar`/`QuotedPrefix` panic
  bodies all read "unreachable (call sites desugar to `__parseIntPub`…)",
  and the helpers exist (`__parseIntPub`, `__parseUintPub`,
  `__atoiPub`, `__parseBool`, `__parseBoolPub`, `__parseFloat`,
  `__parseComplexPub`, `__unquotePub`, `__unquoteChar`, `__quotedPrefix`
  + pair structs);
- `synth_json.go`: `Marshal` → `__marshal`, `MarshalIndent` →
  `__marshalIndent`, `Decoder.Token` → `__decoderToken`,
  `Number.Float64` → `__numberFloat64`, `Number.Int64` →
  `__numberInt64`, `RawMessage.MarshalJSON` →
  `__rawMessageMarshalJSON` — all present as helpers, none wired.

Consequence: `i, err := strconv.ParseInt(s, 10, 64)` and `b, err :=
json.Marshal(v)` fail fast everywhere, and 6 corpus programs
(`strconv/00_basic.go`, `01_quote.go`, `02_adversarial.go`,
`encoding/json/00_marshal.go`, `01_valid_indent.go`,
`02_adversarial.go`) cannot pass the frontend. Commit `ea95f49`'s
message acknowledges this ("tracked as P3-M7-GAP-1 with re-test
trigger"), but **no GAP-1 artifact exists** — nothing under
`requirements/`, `phases/`, or `phases/phase3/hidden_files/`
mentions it. A commit-message-only tracking entry is not tracking.
**Fix:** land the desugar (the helpers are real and my differential
tests below show they are correct) or, at minimum, record GAP-1 as a
proper work item with the re-test trigger, and align the synthetic
source comments/panics with reality in the meantime.

---

## 4. Medium: `json.Marshal(float32)` is wrong

`synth_json.go:1821-1829`:

```go
case float32:
    // float32 is converted to float64 like gc does.
    jff := jsonFormatFloat(float64(v))
```

The comment is incorrect. gc formats float32 with the shortest
representation that round-trips through **float32** (bitSize 32);
formatting the widened float64 value with float64-shortest gives a
longer, wrong string:

```
json.Marshal(float32(3.14))
  gc:    "3.14"
  synth: "3.140000104904175"
```

Verified by extracting `syntheticJsonSource` and running
`__marshal` under stock gc 1.27.1. **Fix:** a float32-shortest path
(`jsonFormatFloat32`), or convert-then-format with 32-bit precision.

---

## 5. Medium: `json.Marshal([]byte)` panics where gc returns data

`marshalValue` (`synth_json.go:1802-1908`) has no `[]byte` case; a
`[]byte` value falls to `default:` and panics with "not in M7 scope
(needs reflect, P3-M8)". gc base64-encodes `[]byte` as a JSON string
(`json.Marshal([]byte("aGVsbG8="))` → `"YUdWc2JHOD0="`) — no reflect
involved, and `[]byte` is squarely inside the package's documented
dynamic value model. A program marshaling bytes gets a panic instead
of a JSON string. **Fix:** add the `[]byte` → base64-string case
(gc semantics; no reflect needed).

---

## 6. Medium: `json.Decoder.Buffered()` is a silent wrong-data stub

`synth_json.go:2289-2292`:

```go
// Buffered returns a reader of the data remaining in the Decoder's buffer.
func (dec *Decoder) Buffered() jsonReader {
    // Simplified: return empty.
    return &jsonEmptyReader{}
}
```

gc returns the buffered-but-unconsumed data; the synthetic silently
returns an empty reader. No panic, no "not in M7 scope" message — just
wrong data. This is the one true silent stub in the wave-1 sources
(all 83 other exported panic-body decls I enumerated carry explicit
scope reasons). **Fix:** fail fast with a named scope error, or
implement it (the decoder demonstrably buffers: `InputOffset`,
`More` exist).

---

## 7. Medium: `__asn1Marshal` panics on scalar-but-unsupported types where gc returns an error

`synth_asn1.go:748` (end of `__asn1Marshal`): after the scalar cases,
unsupported types (e.g. `float64`) hit
`panic("encoding/asn1: Marshal: not in M7 scope (needs reflect for
composite types, big.Int and named types; …)")`. But gc's
`asn1.Marshal(float64(1.5))` does not need reflect to *reject* — it
returns `(nil, StructuralError{...})`. A program doing `b, err :=
asn1.Marshal(v); if err != nil { … }` gets a panic instead of a
handled error. The panic message is also misleading for this case
(reflect is not what a float64 rejection needs). **Fix:** return
`asn1MarshalPair{err: StructuralError{…}}` for the scalar-rejectable
types instead of panicking (keep the panic, if at all, for genuinely
reflect-shaped values).

---

## 8. Medium: promoted methods fail at call sites (exporter gap, breaks corpus)

`resolveMethodCall` (`export.go:2448`) resolves only via `methodIndex`
(direct methods). Promoted methods through embedding are not found:

```
$ gofrontend promote.go   # o.Hello() where Hello is promoted from embedded inner
gofrontend: gofrontend: method Hello not found on outer      # exit 2
```

The type *tables* handle promotion (`methodTableEntries`, export.go
~5318: "covers promoted/embedded methods, which methodIndex lacks"),
but call-site resolution does not. This breaks the synthetic
`encoding/binary` package itself: `nativeEndian` embeds `littleEndian`
(`synth_binary.go:224`), so `binary.NativeEndian.PutUint16(ne, 0x1234)`
fails the same way — and with it corpus
`encoding/binary/01_byteorder.go`. The corpus author clearly expected
promotion to work. **Fix:** resolve via `types.NewMethodSet` (as the
tables do) with the existing `Selection.Index` adaptation path, or
document the limitation and fix the corpus.

---

## 9. Low (scope, but corpus-visible): no implicit `&`/deref on method receivers

`base32.StdEncoding.WithPadding('*')` (pointer → value-receiver
method) and `var b strings.Builder; b.WriteByte('h')` (value →
pointer-receiver method) fail with "method call … needs an implicit
address-of or deref on the receiver (WS D)". This kills 4 corpus
programs (`encoding/base32/01_padding.go`,
`encoding/base64/01_strict.go`, `strings/03_builder.go`,
`strings/07_combined.go`). If this stays out of scope, the corpus
must not use these patterns (they are the most ordinary Go method
calls imaginable — a corpus that can't express them is weak).

Also noted: `exportMultiResultDesugar` handles `:=` and `=` but not
`var d, err = time.ParseDuration(s)` (GenDecl form) — fails fast
instead of desugaring. The Appendix M design documents only the two
assignment forms, so this is a documented limitation, but it is a
common Go form worth a re-test trigger.

---

## 10. Test quality

### 10.1 Corpus: 20 of 68 programs cannot pass the frontend

Empirical sweep (every `corpus/wave1/**/[0-9][0-9]_*.go` through the
built exporter at `ea95f49`): 48 pass, 20 fail with export scope
errors — i.e. they can never run the graalgo leg differentially:

| programs | cause | whose bug |
|---|---|---|
| `encoding/asn1/00_basic,01_adversarial` | uses `asn1.MarshalWithParams` (documented out-of-scope, needs reflect) | corpus |
| `encoding/base32/01_padding`, `encoding/base64/01_strict` | implicit deref on method receiver (§9) | exporter scope / corpus |
| `encoding/binary/01_byteorder` | promoted method `nativeEndian.PutUint16` (§8) | exporter |
| `encoding/csv/00_basic` | uses `csv.NewReader` — absent by design (io out of scope); should use `NewReaderString` | corpus |
| `encoding/gob/00_basic` | uses `gob.NewEncoder` — absent by design; should use `NewBufferEncoder` | corpus |
| `encoding/json/00_marshal,01_valid_indent,02_adversarial` | GAP-1 (§3) | exporter |
| `encoding/xml/00_token_basic,01_token_adversarial,02_encode,03_escape_copy` | dual-mode `Read(p []byte) (int, error)` / `Write` method *declarations* rejected by frontend scope — the dual-mode corpus design cannot work: the same file must compile under gc (needs `Read`/`Write`) and the frontend (forbids 2-result declarations) | corpus/harness design |
| `strconv/00_basic,01_quote,02_adversarial` | GAP-1 (§3) | exporter |
| `strings/03_builder,07_combined` | implicit `&` on `WriteByte` (§9) | exporter scope / corpus |

Additionally `run_wave.sh` hardcodes `GR="$HOME/workspace/graalgo`
— the corpus committed in *this* repo runs against a *different*
checkout (`ea95f49` fixed per-package runner args but not `$GR`).

### 10.2 csv/gob drift test is tautological

`phases/phase3/src/p3frontend/enccsvgob_test.go` never references the
phase-1 source (zero occurrences of `phase1SyntheticPackageForTest`);
it asserts the mirror's *own* method names exist. Every other
package's drift test (`bytes`, `strings`, `sort`, `math`, `strconv`,
`xml`, `json`, `errors`, `enccore`) type-checks the real phase-1
const and compares mirror-vs-source — genuinely capable of failing.
The csv/gob test would pass with a completely drifted mirror.
**Fix:** mirror the `phase1SyntheticPackageForTest` pattern.

### 10.3 Pair-struct contracts unpinned for strconv/json/xml

`enccore_test.go:TestEncCoreSyntheticSignatures` pins the Appendix M
pair/triple structs positionally (field names *and* types) — exactly
the contract the desugar relies on. The strconv/json/xml drift tests
pin exported signatures but not their pair structs
(`parseIntPair`, `marshalPair`, `tokenPair`, …), even though GAP-1's
future desugar will extract by those fields. **Fix:** add the
pair-struct checks (the enccore test is the template).

### 10.4 What's good (kept honest)

- `TestEnccoreDesugarShapes`
  (`synth_enccore_export_test.go:150-198`) is non-tautological: exports
  real programs and asserts the wire doc contains the helper call and
  field extractions. Passes at HEAD.
- Drift tests (except csv/gob) compare against the actual phase-1
  const — verified they read
  `../../../phase1/frontend/<srcFile>` and type-check it.

---

## 11. Adversarial verification performed (all empirical)

- **Desugar table**: every `m7MultiResultFuncs` entry checked
  against its synthetic source (helper name, pair type, field
  names/order) — all match. End-to-end export of
  `base64.Decode`/`DecodeString`, `binary.Uvarint`,
  `ascii85.Decode` (3-target), `hex.DecodeString` produces correct
  `__p5mr_N` temps with fields extracted in signature order
  (`n,err` / `b,err` / `x,n` / `ndst,nsrc,err` / `b,err`).
- **Wiring**: all 18 packages present in all three exporter sites
  (`overlayImporter` export.go:206-271, need-flags + import calls
  export.go:838-1013) and in the p3frontend importer
  (`importer.go:98-209`); synthetic cross-package imports
  (csv/gob→errors, json→bytes, pem→base64, bytes/strconv→errors)
  enumerated against the need-flag implications — csv/gob→errors
  missing (§1).
- **Behavioral differential** (synthetic sources extracted and run
  under stock gc 1.27.1):
  - csv `__csvRead`/`__csvFieldPos`/Writer: identical to gc on 17
    adversarial inputs (quotes, embedded newlines, CRLF, bare quotes,
    field-count mismatch, empty fields);
  - strconv `__parseIntPub`/`__parseUintPub`/`__parseFloat`/
    `__atoiPub`/`__parseBool`: identical values *and* error strings
    (bases 0/2/8/10/16, bit sizes 8/32/64, underscores, hex floats,
    subnormals, MaxFloat±1ulp, `+nan` quirk);
  - asn1 `__asn1Marshal` scalars (int/bool/string/nil): byte-identical
    DER;
  - json `__marshal`: all model values identical **except** float32
    (§4) and `[]byte` (§5).
- **API shapes** vs pinned `~/workspace/graalgo-stdlib/go1.27.1`:
  csv/hex/ascii85 match modulo documented io omissions
  (`NewReader`/`NewWriter`/`NewEncoder`/`NewDecoder`/`Dumper`);
  M7 extensions (`NewReaderString`, `NewBufferWriter`, `Bytes`)
  are the only extras.
- **Stub hunt**: 83 exported panic-body decls enumerated — all carry
  explicit "not in M7 scope" reasons except `json.Buffered` (§6);
  no other silent stubs; small non-panic bodies are all legitimate
  delegations.

---

## 12. Recommended fix order

1. `export.go`: add `needErrors = true` for `encoding/csv` and
   `encoding/gob` (§1) — one line each; un-breaks two packages.
2. `synth_enc_csv_gob.go`: align the four stale desugar promises
   with the fail-fast-everywhere reality (§2), or re-implement the
   csv desugar correctly (distinct helpers/pairs for Read vs ReadAll,
   plus FieldPos classification).
3. `synth_json.go`: fix float32 formatting (§4); add `[]byte` case
   (§5); make `Buffered` fail fast or real (§6).
4. `synth_asn1.go`: return `StructuralError` for scalar-rejectable
   types instead of panicking (§7).
5. Record P3-M7-GAP-1 as a real work item (or land the
   strconv/json desugar — the helpers are correct per §11).
6. `export.go:resolveMethodCall`: support promoted methods (§8);
   decide the implicit `&`/deref scope (§9) and fix the corpus to
   match.
7. Corpus: rewrite the 20 unrunnable programs (or mark them
   out-of-scope with reason codes); fix `run_wave.sh`'s `$GR`;
   fix the csv/gob drift test (§10.2); pin strconv/json/xml pair
   structs (§10.3).

---

## 13. Process note (shared tree)

HEAD moved `2331e20` → `ea95f49` mid-review (concurrent integration
worker). To A/B a failing test I ran `git stash push` /
`git stash pop` on `phases/phase1/frontend/mrdesugar_m5.go`; the
tree is clean afterwards (`git status`: only untracked
`reviews/phase3-m7/`), HEAD `ea95f49`, and all findings above were
re-verified post-move. No files were left modified.
