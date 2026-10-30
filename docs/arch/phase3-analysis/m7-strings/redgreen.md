# P3-M7 strings: red-green evidence

Non-tautological verification that the synthetic `strings` package is a
real port, not a stub.

## 1. gc differential of extracted synthetic source (pre-exporter)

The `syntheticStringsSource` const was extracted and compiled under stock
gc 1.27.1 alongside the real `strings` package. Two harnesses in
`phases/phase3/hidden_files/m7-tmp/strings/synthcheck/`:

- **Fixed battery** (`cmd/`): ~200 cases covering every exported function,
  Builder/Replacer/Reader methods, unicode case mapping (including
  astral-plane runes, Turkish İ, German ß, fi ligature), malformed UTF-8,
  empty inputs. **ALL SYNTHETIC PORTS MATCH gc.**
- **Adversarial fuzz** (`cmd/fuzz`): 4000 iterations with random malformed
  UTF-8 (truncated sequences, surrogates, overlong encodings),
  random unicode (including astral), and random ASCII. **ALL MATCH gc.**

### Real bug caught: DecodeLastRune back-scan

The differential caught a genuine porting bug: my `DecodeLastRune`
back-scan only broke on ASCII bytes, but gc breaks on any rune-start byte
(0x00-0x7F or 0xC0-0xFF). For input like `"\xff\x41"` (invalid byte
followed by 'A'), my version scanned past the 0xFF and returned the wrong
result; gc correctly returns RuneError. Fixed by matching gc's
`!utf8.RuneStart` break condition. The fuzz battery now passes.

## 2. End-to-end via graalgo exporter

`t01.go` smoke program (ToUpper, Count, Index, LastIndex, Fields, Join,
ReplaceAll, EqualFold, Compare, TrimSpace, Repeat, Builder, Reader,
Replacer, Map, ContainsRune, IndexAny) produces **byte-identical output**
under gc 1.27.1 and graalgo P3SyncMain.

### Runtime limitations discovered (v1, documented in source)

1. **No pointer comparison** (WS D): `copyCheck` uses interface
   comparison (`any(b.addr) != any(b)`) instead of `b.addr != b`.
2. **Structs with sub-64-bit integer fields crash** (ClassCastException):
   `stringsCaseRange`, `stringsRuneSize`, `stringsToResult` use int64
   fields with explicit conversions.
3. **Index assignment through pointer-to-array** (WS E):
   `stringsByteReplacer` wraps `[256]byte` in a struct.
4. **`new()` doesn't initialize array fields**: use `&T{}` instead.
5. **`copy` from nil slice NPEs**: guarded in `Builder.grow`.
6. **Multi-result calls fail fast** (WS D): Cut family, Builder.Write/
   WriteRune/WriteString, Reader.Read/ReadAt/ReadByte/ReadRune/Seek.

## 3. Drift test

`phases/phase3/src/p3frontend/strings_test.go`:
- `TestStringsSyntheticSignatures`: mirror matches phase-1 source
  method-by-method (50 funcs, Builder/Replacer/Reader).
- `TestStringsOmissionSet`: pins the 10 deliberate omissions with reasons.
- `TestStringsImportResolvesSynthetic`: loader resolves as synthetic.

## Re-test triggers

- **WS D lands**: re-test multi-result Cut family, Builder.Write/
  WriteRune/WriteString, Reader.Read/ReadAt/ReadByte/ReadRune/Seek.
- **unicode wave**: re-test ToUpperSpecial/ToLowerSpecial/ToTitleSpecial,
  Title (needs unicode tables).
- **iter wave**: re-test Lines/SplitSeq/SplitAfterSeq/FieldsSeq/FieldsFuncSeq.
- **io wave**: re-test Replacer.WriteString, Reader.WriteTo.
- **Go version change**: re-run `gen_tables.py`, diff tables.
