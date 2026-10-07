# P3-M8 wave 2 — staged patches for the `io` package

All patches are canonical unified diffs (`git apply --check` clean as of
2026-10-07). **DO NOT APPLY until rebase** (R9 staging discipline).
Apply in numeric order; 06 needs 01+02+04, 07 needs 01+02, 05 needs 01+02.

| # | File | Target | What changes |
|---|------|--------|--------------|
| 01 | `phases/phase1/frontend/synth_machinery.go` | registration | Add `"io": syntheticIoSource` to the synthetic-source map (with the P3-M8 comment: full io.go+multi.go+pipe.go; io/fs, io/ioutil out; imports synthetic errors + the M2 sync surface). |
| 02 | `phases/phase1/frontend/export.go` | import overlay | Allow-list `"io"` in the WS G overlay comment; `case "io"` in overlayImporter → `getSyntheticPackage("io")`; `needIo` flag + loader (imports synthetic `errors` first, then `io`). |
| 03 | `phases/phase1/frontend/synth_machinery.go` | multi-result scope | `isMultiResultSynthetic`: treat io's 21 fail-fast members as synthetic multi-result (named M8 scope error at export); add the M8 milestone comment naming Copy/CopyN/CopyBuffer/WriteString/ReadAtLeast/ReadFull/ReadAll/teeReader.Read/discard.ReadFrom/nopCloserWriterTo.WriteTo/multiReader.Read/WriteTo/multiWriter.Write/WriteString/LimitedReader.Read/SectionReader.Read/ReadAt/OffsetWriter.Write/WriteAt. |
| 04 | `phases/phase1/frontend/mrdesugar_m5.go` | desugar table | 11 Appendix M entries — ONLY members with real `__`-helpers (verified name/field-for-field against synth_io.go / synth_strings.go): io Pipe, PipeReader.Read, PipeWriter.Write, SectionReader.Seek, SectionReader.Outer, OffsetWriter.Seek; strings Reader.Read/ReadAt/ReadByte/ReadRune/Seek. Everything else stays fail-fast (WS D), NOT tabled. |
| 05 | `phases/phase1/frontend/synth_bytes.go` | M7 trigger | `import "io"`; delete byteReader/byteWriter stand-ins; Buffer.ReadFrom/Buffer.WriteTo/Reader.WriteTo take io.Reader/io.Writer with exact gc signatures; bodies stay fail-fast (WS D); header comment updated. |
| 06 | `phases/phase1/frontend/synth_strings.go`, `export.go` | M7 trigger | `import ("errors"; "io")`; Reader.Read/ReadAt/ReadByte/ReadRune/Seek get REAL `__`-helpers + pair structs (gc 1.27.1 verbatim; utf8.DecodeRuneInString → stringsDecodeRune); Reader.WriteTo and Replacer.WriteString DECLARED with exact gc signatures + fail-fast bodies (were: omitted); export.go `case "strings"` also sets needErrors. |
| 07 | `phases/phase1/frontend/synth_base32.go`, `phases/phase3/src/p3frontend/enccore_test.go`, `phases/phase3/src/p3frontend/synth_enc_core.go` | M7 trigger | `import "io"`; encoder/decoder types (exact gc shapes) + real NewEncoder/NewDecoder + fail-fast Write/Close/Read; drift test: drop the 2 base32 omission rows, importer resolves io/errors/sync; mirror gains ioStreamingIfaces (io.Writer/Reader/WriteCloser) + NewEncoder/NewDecoder. |
| 08 | `phases/phase1/frontend/synth_xml.go` | M7 trigger | Comment-only: Reader/Writer stand-ins STAY (migration needs 2-result interface dispatch — WS D, not just io); Decoder.CharsetReader stays omitted (declaring without calling would silently drop charsets); re-test triggers re-narrowed from "io lands" to "WS D lands". |

## Deliberate deviation from the task text (coordinator decision needed)

The task said the Copy family etc. "need mrM7-style desugar entries". They do
NOT get entries: a desugar entry without a real `__`-helper would compile and
then panic at runtime — strictly worse than the export-time named scope error
(cf. the asn1.Unmarshal precedent). The table carries only members with real
helpers (patch 04); everything else fails fast at export via patch 03.
Re-test trigger for the whole fail-fast set: WS D (multi-result calls).
