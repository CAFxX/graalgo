# REVIEW-2026-11-17-20 P1-3: unwired-stub remediation spec (OWED)

Base: master a0df481b. Inventory: `wsd-stub-inventory.csv` (219 WS-D scope-trap rows, 203 distinct (pkg,funcName) pairs).
Status after this run: 137 rows DONE (redirect-message), 2 rows EXCLUDED (deliberate), 80 pairs OWED (helper+row+tests).

## Per-pair work (one row per owed pair)
For each pair: (1) write a `__`-helper in the synthetic source (real Go body; port gc 1.27.1 where no single-result sibling exists); (2) add the Appendix-M table row in `internal/frontend/mrdesugar_m5.go` (m7/io/os/m8/m9 table as appropriate) with the correct pair type + pair fields in result order; (3) pin it in a wiring test (`synth_m7desugar_test.go` / `synth_compress_desugar_test.go` pattern: table row + helper existence + pair struct fields) AND a behavioural test (corpus program differential vs gc, or a frontend export test). Verify with `go test` in `internal/frontend` (build.sh test) + `make check` subset check (`.github/scripts/check_mr_table_subset.py`) + regeneration no-op (`run_gen_synth.py`).
Candidate helpers below are UNVERIFIED name matches (`__*` funcs in the same file whose name ends with the method name) — verify each against the synthetic source before using; a candidate that is itself a stub does not count as a helper.

| # | file:line | pkg | func | kind | scope | iface | candidate helper(s) in source |
|---:|---|---|---|---|---|---|---|
| 1 | synth_asn1.go:818 | encoding/asn1 | Unmarshal | pkg-func | M7 | PROBE-OWED-cross-pkg via m8IfaceMultiResultMethods:crypto/elliptic.Unmarshal | — |
| 2 | synth_base32.go:244 | base32.encoder | Write | method | M8 | COVERED via m8IfaceMultiResultMethods:io.Write | — |
| 3 | synth_base32.go:249 | base32.encoder | Close | method | M8 | PROBE-OWED | — |
| 4 | synth_base32.go:271 | base32.decoder | Read | method | M8 | COVERED via m8IfaceMultiResultMethods:io.Read | — |
| 5 | synth_base64.go:625 | encoder | Write | method | M7 | COVERED via m8IfaceMultiResultMethods:io.Write | `__encoderWrite` |
| 6 | synth_base64.go:708 | newlineFilteringReader | Read | method | M7 | COVERED via m8IfaceMultiResultMethods:io.Read | `__decoderRead`, `__newlineFilteringReaderRead` |
| 7 | synth_base64.go:758 | decoder | Read | method | M7 | COVERED via m8IfaceMultiResultMethods:io.Read | `__decoderRead`, `__newlineFilteringReaderRead` |
| 8 | synth_bytes.go:9935 | bytes.Buffer | Peek | method | M7 | PROBE-OWED | — |
| 9 | synth_bytes.go:10105 | bytes.Buffer | ReadFrom | method | M8 | COVERED via m8IfaceMultiResultMethods:io.ReadFrom | — |
| 10 | synth_bytes.go:10144 | bytes.Buffer | WriteTo | method | M8 | COVERED via m8IfaceMultiResultMethods:io.WriteTo | — |
| 11 | synth_bytes.go:10169 | bytes.Buffer | WriteRune | method | M7 | PROBE-OWED | — |
| 12 | synth_bytes.go:10227 | bytes.Buffer | ReadByte | method | M7 | COVERED via m8IfaceMultiResultMethods:io.ReadByte | — |
| 13 | synth_bytes.go:10239 | bytes.Buffer | ReadRune | method | M7 | PROBE-OWED | — |
| 14 | synth_bytes.go:10285 | bytes.Buffer | ReadBytes | method | M7 | PROBE-OWED | — |
| 15 | synth_bytes.go:10293 | bytes.Buffer | readSlice | unexported-helper | M7 | PROBE-OWED | — |
| 16 | synth_bytes.go:10306 | bytes.Buffer | ReadString | method | M7 | PROBE-OWED | — |
| 17 | synth_bytes.go:10380 | bytes.Reader | ReadAt | method | M7 | PROBE-OWED | — |
| 18 | synth_bytes.go:10388 | bytes.Reader | ReadByte | method | M7 | COVERED via m8IfaceMultiResultMethods:io.ReadByte | — |
| 19 | synth_bytes.go:10406 | bytes.Reader | ReadRune | method | M7 | PROBE-OWED | — |
| 20 | synth_bytes.go:10430 | bytes.Reader | Seek | method | M7 | PROBE-OWED | — |
| 21 | synth_bytes.go:10439 | bytes.Reader | WriteTo | method | M8 | COVERED via m8IfaceMultiResultMethods:io.WriteTo | — |
| 22 | synth_enc_csv_gob.go:732 | Reader | Read | method | M7 | PROBE-OWED | `__csvRead` |
| 23 | synth_enc_csv_gob.go:767 | Reader | ReadAll | method | M7 | PROBE-OWED | `__csvReadAll` |
| 24 | synth_enc_csv_gob.go:794 | Reader | FieldPos | method | M7 | PROBE-OWED | `__csvFieldPos` |
| 25 | synth_io.go:572 | io.SectionReader | Read | method | M8 | COVERED via m8IfaceMultiResultMethods:io.Read | `__limitedReaderRead`, `__pipeRead`, `__pipeReaderRead` |
| 26 | synth_io.go:615 | io.SectionReader | ReadAt | method | M8 | PROBE-OWED | — |
| 27 | synth_io.go:663 | io.OffsetWriter | Write | method | M8 | COVERED via m8IfaceMultiResultMethods:io.Write | `__discardWrite`, `__pipeWrite`, `__pipeWriterWrite` |
| 28 | synth_io.go:671 | io.OffsetWriter | WriteAt | method | M8 | PROBE-OWED | — |
| 29 | synth_io.go:723 | io.teeReader | Read | method | M8 | COVERED via m8IfaceMultiResultMethods:io.Read | `__limitedReaderRead`, `__pipeRead`, `__pipeReaderRead` |
| 30 | synth_io.go:771 | io.discard | ReadFrom | method | M8 | COVERED via m8IfaceMultiResultMethods:io.ReadFrom | — |
| 31 | synth_io.go:802 | io.nopCloserWriterTo | WriteTo | method | M8 | COVERED via m8IfaceMultiResultMethods:io.WriteTo | — |
| 32 | synth_io.go:869 | io.eofReader | Read | method | M8 | COVERED via m8IfaceMultiResultMethods:io.Read | `__limitedReaderRead`, `__pipeRead`, `__pipeReaderRead` |
| 33 | synth_io.go:881 | io.multiReader | Read | method | M8 | COVERED via m8IfaceMultiResultMethods:io.Read | `__limitedReaderRead`, `__pipeRead`, `__pipeReaderRead` |
| 34 | synth_io.go:889 | io.multiReader | WriteTo | method | M8 | COVERED via m8IfaceMultiResultMethods:io.WriteTo | — |
| 35 | synth_io.go:913 | io.multiWriter | Write | method | M8 | COVERED via m8IfaceMultiResultMethods:io.Write | `__discardWrite`, `__pipeWrite`, `__pipeWriterWrite` |
| 36 | synth_io.go:994 | io.pipe | read | unexported-helper | M8 | PROBE-OWED | `__limitedReaderRead`, `__pipeRead`, `__pipeReaderRead` |
| 37 | synth_io.go:1036 | io.pipe | write | unexported-helper | M8 | PROBE-OWED | `__discardWrite`, `__pipeWrite`, `__pipeWriterWrite` |
| 38 | synth_iofs.go:489 | fs | __fsReadFile | __-helper | M8 | PROBE-OWED | `__fsReadFile` |
| 39 | synth_iofs.go:504 | fs | __fsReadDir | __-helper | M8 | PROBE-OWED | `__fsReadDir` |
| 40 | synth_iofs.go:529 | fs | __fsStat | __-helper | M8 | PROBE-OWED | `__fsStat` |
| 41 | synth_iofs.go:892 | fs | __fsGlobWithLimit | __-helper | M8 | PROBE-OWED | `__fsGlobWithLimit` |
| 42 | synth_iofs.go:923 | fs | WalkDir | pkg-func | M8 | PROBE-OWED | `__fsWalkDir` |
| 43 | synth_iofs.go:970 | fs | __fsSub | __-helper | M8 | PROBE-OWED | `__fsSub` |
| 44 | synth_iofs.go:1019 | fs | __subFSOpen | __-helper | M8 | PROBE-OWED | `__subFSOpen` |
| 45 | synth_iofs.go:1027 | fs | __subFSReadDir | __-helper | M8 | PROBE-OWED | `__subFSReadDir` |
| 46 | synth_iofs.go:1035 | fs | __subFSReadFile | __-helper | M8 | PROBE-OWED | `__subFSReadFile` |
| 47 | synth_iofs.go:1043 | fs | __subFSGlob | __-helper | M8 | PROBE-OWED | `__subFSGlob` |
| 48 | synth_iofs.go:1051 | fs | __subFSSub | __-helper | M8 | PROBE-OWED | `__subFSSub` |
| 49 | synth_iofs.go:1092 | fs | __fsReadLink | __-helper | M8 | PROBE-OWED | `__fsReadLink` |
| 50 | synth_iofs.go:1105 | fs | __fsLstat | __-helper | M8 | PROBE-OWED | `__fsLstat` |
| 51 | synth_json.go:1200 | json.RawMessage | UnmarshalJSON | method | M7 | PROBE-OWED | — |
| 52 | synth_json.go:1956 | json | Unmarshal | pkg-func | M7 | PROBE-OWED | — |
| 53 | synth_json.go:2133 | json | marshalValue | unexported-helper | M7 | PROBE-OWED | — |
| 54 | synth_json.go:2415 | json.Decoder | Decode | method | M7 | PROBE-OWED | — |
| 55 | synth_json.go:2578 | json.Decoder | Buffered | method | M7 | PROBE-OWED | — |
| 56 | synth_json.go:2595 | json | jsonReadAll | unexported-helper | M7 | PROBE-OWED | — |
| 57 | synth_json.go:2633 | json.Encoder | Encode | method | M7 | PROBE-OWED | — |
| 58 | synth_json.go:2656 | json | Indent | pkg-func | M7 | PROBE-OWED | `__marshalIndent` |
| 59 | synth_json.go:2663 | json | Compact | pkg-func | M7 | PROBE-OWED | — |
| 60 | synth_json.go:2667 | json | HTMLEscape | pkg-func | M7 | PROBE-OWED | — |
| 61 | synth_math.go:3722 | math | Modf | pkg-func | M7 | PROBE-OWED | `__modf` |
| 62 | synth_math.go:3730 | math | Lgamma | pkg-func | M7 | PROBE-OWED | — |
| 63 | synth_neturl.go:195 | url.URL | MarshalBinary | method | M9 | PROBE-OWED-cross-pkg via m8IfaceMultiResultMethods:hash.MarshalBinary | — |
| 64 | synth_neturl.go:199 | url.URL | AppendBinary | method | M9 | PROBE-OWED-cross-pkg via m8IfaceMultiResultMethods:hash.AppendBinary | — |
| 65 | synth_os.go:2008 | os | Expand | pkg-func | M8 | PROBE-OWED | — |
| 66 | synth_os.go:2013 | os | getShellName | unexported-helper | M8 | PROBE-OWED | — |
| 67 | synth_reflect.go:2006 | reflect.Value | Seq | method | M8 | PROBE-OWED | — |
| 68 | synth_singleflight.go:62 | singleflight.Group | Do | method | M9 | PROBE-OWED | — |
| 69 | synth_singleflight.go:68 | singleflight.Group | DoChan | method | M9 | PROBE-OWED | — |
| 70 | synth_singleflight.go:73 | singleflight.Group | doCall | unexported-helper | M9 | PROBE-OWED | — |
| 71 | synth_sort.go:254 | sort | Find | pkg-func | M7 | PROBE-OWED | — |
| 72 | synth_strings.go:2487 | strings.Reader | WriteTo | method | M8 | COVERED via m8IfaceMultiResultMethods:io.WriteTo | — |
| 73 | synth_strings.go:2541 | strings.Replacer | WriteString | method | M8 | COVERED via m8IfaceMultiResultMethods:io.WriteString | `__builderWriteString` |
| 74 | synth_time_context.go:1216 | emptyCtx | Deadline | method | M5 | PROBE-OWED | `__withDeadline` |
| 75 | synth_time_context.go:1277 | cancelCtx | Deadline | method | M5 | PROBE-OWED | `__withDeadline` |
| 76 | synth_time_context.go:1454 | timerCtx | Deadline | method | M5 | PROBE-OWED | `__withDeadline` |
| 77 | synth_time_context.go:1565 | valueCtx | Deadline | method | M5 | PROBE-OWED | `__withDeadline` |
| 78 | synth_time_context.go:1645 | withoutCancelCtx | Deadline | method | M5 | PROBE-OWED | `__withDeadline` |
| 79 | synth_xml.go:337 | xml | NewTokenDecoder | pkg-func | M7 | PROBE-OWED | — |
| 80 | synth_xml.go:730 | xml.Decoder | InputPos | method | M7 | PROBE-OWED | — |

## Notes

1. **fs (13 pairs, synth_iofs.go): the hard class.** The `__fs*` helpers exist only as stubs and there are NO table rows for the public functions (`io/fs.ReadFile/ReadDir/Stat/Sub/Glob/WalkDir` — verified by grep of `mrdesugar_m5.go`: only `ReadLink`, `Lstat`, `DirEntry.Info` have rows). The public functions' messages already claim `unreachable (Appendix-M desugar did not fire; re-test when WS D lands)`, which is FALSE today (no row exists, so calls reach the body). The helper bodies must be written in real Go using interface dispatch (now expressible via the P3-M9 generic interface multi-result path, `desugarIfaceMultiResult`); the public-function messages must be revisited once the rows land (they become true redirect-form rows). Do NOT add the rows without the helper bodies.
2. **base32.encoder.Close (synth_base32.go:249): OVERLAP — do not duplicate.** Commit `400daa1e` on branch `wt/graalgo-independent-codebase-review/review-22-g-12` already ports Close to real Go (single-result; mislabeled as WS-D). That branch is unmerged. Coordinate with the coordinator before touching Close; if that branch lands first, this pair drops out of the owed set.
3. **21 owed rows are interface-dispatch COVERED** (`ifaceDispatch=COVERED` via `m8IfaceMultiResultMethods`): direct calls panic but interface calls already desugar. The helper+row work is still owed for the direct shape; do not mistake interface coverage for done.
4. **3 owed rows are PROBE-OWED-cross-pkg** (asn1.Unmarshal, url.URL MarshalBinary/AppendBinary): the interface table matches a DIFFERENT package's method (crypto/elliptic.Unmarshal, hash.MarshalBinary/AppendBinary). Treat as suspect until probed; do not copy the cross-package row.
5. **Deliberate exclusions (do NOT remediate):** `archive/zip.findBodyOffset` and `mime/multipart.readForm` — see `.github/scripts/check_mr_table_subset.py` `verify_exclusions()` (unexported; internal calls already rewritten to the `__`-helper). They keep the scope message by design; the generator exception lives in `gen_synth_archive_mime.py` (`REDIRECT_EXCLUDED`).
6. **Brief-vs-truth count discrepancy:** the brief says 134 wired / 69 unwired; disk truth (R2-P1-3 matrix, verified by grep) is 121 table-wired pairs / 82 unwired pairs (203 total, matching). Delta is exactly 13 = the fs pairs (see note 1): the review's split appears to take the fs public functions' `unreachable` messages at face value. This spec follows disk truth.
7. **Stale redirect-with-tail rows (family find, NOT in the 203):** 20 `panic(... unreachable (Appendix-M desugar did not fire; re-test when WS D lands))` rows in `synth_filepath.go` (7) and `synth_iofs.go` (13) still carry the stale WS-D tail although WS D has landed; the 10 fs ones (note 1) additionally lack table rows. Left untouched in this run; dispatch as a follow-up message-only pass.

