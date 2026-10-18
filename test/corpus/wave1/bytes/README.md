# P3-M7 wave-1 corpus: bytes

Differential tests for the synthetic `bytes` package
(`internal/frontend/synth_bytes.go`).

## Programs

| File | Coverage |
|------|----------|
| 00_basic.go | Equal/Compare/Count/Contains/ContainsAny/ContainsRune/HasPrefix/HasSuffix/EqualFold/Clone, MinRead, ErrTooLarge |
| 01_index.go | Index/IndexByte/IndexRune/IndexAny, LastIndex/LastIndexByte/LastIndexAny, empty/nil edges |
| 02_split_join.go | Split/SplitN/SplitAfter/SplitAfterN/Fields/Join/Replace/ReplaceAll/Repeat |
| 03_case.go | ToUpper/ToLower (ASCII), ToValidUTF8 (valid input) |
| 04_trim.go | Trim/TrimLeft/TrimRight/TrimSpace/TrimPrefix/TrimSuffix |
| 05_buffer.go | Buffer read-only methods: NewBuffer/NewBufferString/Len/Cap/Available/Bytes/String/Grow/AvailableBuffer |
| 06_reader.go | Reader single-result methods: NewReader/Len/Size/Reset |
| 07_adversarial.go | Empty, nil, unicode edge cases, malformed UTF-8, huge inputs |
| 08_combined.go | Trim+ToUpper+ReplaceAll, Split+Join, Fields+Join, Replace+ToUpper, Buffer Grow+Bytes, SplitAfter+TrimPrefix, Reader Reset |
| 09_panic_repeat.go | Repeat negative count panics |
| 10_panic_buffer.go | Buffer.Grow negative count panics (ErrTooLarge) |

## Limitations

Multi-result functions/methods (Cut, CutPrefix, CutSuffix, CutLast,
Buffer.Write/WriteString/WriteByte/WriteRune/ReadFrom/WriteTo/Read/ReadByte/
ReadRune/ReadBytes/ReadString/Peek, Reader.Read/ReadAt/ReadByte/ReadRune/
Seek/WriteTo) fail fast at export time under WS D and are not exercised
here. They have exact gc signatures and real implementations in the
synthetic source, but the exporter rejects calls until WS D lands.

The 8 pinned omissions (iter.Seq-based Lines/SplitSeq/SplitAfterSeq/
FieldsSeq/FieldsFuncSeq, unicode.SpecialCase-based ToUpperSpecial/
ToLowerSpecial/ToTitleSpecial) are not exported at all — fail fast by
design, re-test when those waves land.

## Runtime limitations discovered (v1)

The following crash the Truffle runtime with
`ClassCastException: Integer cannot be cast to Long` (repros were captured
during the M7 milestone in the since-deleted
`phases/phase3/hidden_files/m7-tmp/bytes/repro/` scratch). They are runtime bugs,
not port bugs — the synthetic source is gc-faithful (see redgreen.md) —
so the corpus avoids them:

1. **Invoking a func value** (user closure, named func, or a package func
   passed as a value): ContainsFunc, IndexFunc, LastIndexFunc, FieldsFunc,
   Map, TrimFunc, TrimLeftFunc, TrimRightFunc, and — via Map — ToTitle and
   Title, plus the non-ASCII fallback of ToUpper/ToLower. Passing the func
   is fine; *calling* it crashes.
2. **`string([]rune)` conversion.**
3. **Buffer methods writing the int8 `lastRead` field**: Truncate, Reset,
   Next (same known limitation as strings, cf. m7-strings/redgreen.md §2).
4. **Split/SplitAfter with an empty separator** (explode path) and
   **Replace with an empty `old`** (same degenerate insertion path).
5. **Unicode case-folding/space table paths**: EqualFold on non-ASCII
   input (fold table), TrimSpace on non-ASCII whitespace (space table),
   IndexAny with a non-ASCII chars string.
6. **ToValidUTF8/Runes on invalid UTF-8 input** (valid input works for
   ToValidUTF8); **Runes on any non-empty input**.

Re-test triggers: a runtime fix for func-value invocation re-enables
group 1 (8 funcs + ToTitle/Title + non-ASCII ToUpper/ToLower); a fix for
sub-64-bit integer handling re-enables groups 3 and 5; fixes for the
conversion, explode, and invalid-input paths re-enable groups 2, 4 and 6.

## Running

```bash
./run_differential.sh
# or
../run_wave.sh bytes
```
