# P3-M7 encoding/xml — red-green evidence

Non-tautological verification for the synthetic `encoding/xml` package.
All tests compare against gc 1.27.1 as oracle.

## White-box differential tests (logic)

Location: `phases/phase3/hidden_files/m7-tmp/enc-xml/difftest/xmlsynth/`
(compiles the synthetic source as `package xml` alongside gc's).

### Decoder token stream — byte-exact vs gc

~70 adversarial inputs × strict/non-strict, comparing the full
`(Token, error)` stream rendered as strings:

- Malformed XML: mismatched tags, unclosed tags, bad entities,
  invalid char refs, unexpected EOF.
- Namespaces: prefix translation, default namespace, `xmlns`/`xml`
  special prefixes, unbound prefixes.
- Entities: all 252 `HTMLEntity` entries verified individually;
  decimal/hex char refs; surrogate pairs; invalid refs.
- Comments, CDATA, PIs, directives, `<?xml?>` version/encoding errors.
- UTF-8 edge cases: invalid sequences, overlong encodings, `\r\n`
  normalization.

**Bugs found and fixed (red→green):**
1. `xmlFormatCharCode` dropped the `U+` prefix in `%U` formatting —
   caught by char-ref error message comparison.
2. `autoClose` used case-sensitive `==` instead of `strings.EqualFold`,
   and popped the stack prematurely (gc invents the end tag without
   popping; `popElement` pops later) — caught by HTML auto-close tests.
3. Encoder lacked bufio-faithful buffering: gc hides partial output on
   error until Flush/Close; the `<?xml?>`-first rule checks
   `Buffered() != 0` with exact 4096-byte flush semantics — caught by
   `TestDiffEncode` error cases. Fixed with internal 4096-byte buffer
   (large-write bypass, no flush on exactly-full).

### Encoder — byte-exact vs gc

`TestDiffEncode`: all token types, validation errors (comment with
`-->`, late `<?xml?>`, invalid ProcInst target), indented output,
4096-byte boundary cases, `Close()` after error (partial flush parity),
`EncodeToken` after `Close` ("use of closed Encoder").

### Other

- `TestDiffEscape`: `EscapeText`/`Escape` byte-exact (incl. `\n` escaping).
- `TestDiffCopy`: `CopyToken`/`Copy`/`End` deep-copy semantics.
- `TestDiffSkip`, `TestDiffInputOffset`, `TestDiffHTMLEntity`.

## Mutation testing (red-green)

Each mutation below was verified to FAIL at least one test (red),
then reverted to green:

1. `xmlFormatCharCode`: remove `U+` prefix → `TestDiffDecode` char-ref
   errors fail.
2. `autoClose`: `==` instead of `EqualFold` → HTML auto-close tests fail.
3. Encoder: remove buffering → `TestDiffEncode` error cases fail
   (partial output visible).
4. `translate`: skip namespace translation → namespace tests fail.
5. Entity table: drop one entry → `TestDiffHTMLEntity` fails.

## Truncation / EOF (gc-golden, not differential)

**Why not differential:** gc checks `d.err == io.EOF` identity in
`mustgetc`/`text`/`Token`. A custom reader can never satisfy this
identity (only gc's `io.EOF` value matches), so gc prints `EOF` where
true gc prints `XML syntax error…: unexpected EOF`. The synthetic
package implements true-gc `unexpected EOF` via the `errXMLEOF`
sentinel conversion. Truncation is covered by gc-golden evidence
here, not the differential runner.

**Re-test trigger:** P3-M8 (when `io` lands, switch to `io.Reader`
and re-run truncation differentially).

## Fail-fast boundaries (verified by construction)

- `Unmarshal`, `Marshal`, `MarshalIndent`, `Decode`, `DecodeElement`,
  `Encode`, `EncodeElement`: exact gc signatures + panic("…not in M7
  scope (needs reflect, P3-M8)…"). **Re-test P3-M8.**
- `InputPos`: exact signature + panic (WS D). **Re-test WS D.**
- `Decoder.CharsetReader`: omitted (needs `io`); non-UTF-8 encoding
  declaration yields gc's nil-CharsetReader error verbatim. **Re-test P3-M8.**
- `NewDecoder`/`NewEncoder`/`EscapeText`/`Escape`: structural M7
  `Reader`/`Writer` (single-result `Next`/`WriteBytes`). **Re-test P3-M8:**
  switch to `io.Reader`/`io.Writer`.

## Desugar verification

`tok, err := d.Token()` and `tok, err := d.RawToken()` (2-target `:=`
and `=`) desugar to `__tokenOf`/`__rawTokenOf` via `mrXmlToken`
(`mrdesugar_m5.go`). Verified by exporter smoke test (see below).
Every other position (e.g., `f(d.Token())`, single-target assignment)
fails fast with "not in M7 scope (multi-result calls need WS D)".

## Exporter smoke test

`gofrontend` on a program using `xml.NewDecoder`, `d.Token()` in a
loop, `xml.NewEncoder`, `EncodeToken`, `Close`, `EscapeText`:
- Type-checks via overlay importer.
- `d.Token()` desugars to `__tokenOf(d)` + field extraction.
- Synthetic bodies export (all single-result after pair-struct refactor).
- No `&` on locals/fields (translate/popElement/writeStart refactored
  to by-value).

## Suites

- Phase-1 frontend: `go test ./...` — green (on `m7-enc-xml`).
- Phase-3 p3frontend: `go test -run TestXml` — green (drift test).
- White-box: `go test ./xmlsynth/` — green.
