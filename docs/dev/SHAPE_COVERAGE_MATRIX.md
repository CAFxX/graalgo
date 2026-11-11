# Shape-coverage matrix: 315 scope-panic traps (R2-P1-3)

> **Master:** `a0df481b` (R-1108-112). **Generated:** 2026-11-11 by `gen_matrix.go` + `build_final.py` (staged in `/var/tmp/review-1111-14-r2p1-3/`; this file is the P1 deliverable — see §9 for the intended repo homes). **Status:** STATIC INFERENCE — every cell is derived from the pair tables and the exporter mechanism, not from running the probe corpus. The probe corpus (§7) flips cells to PROBED. Do not treat a STATIC cell as an observed fact.

## 1. Count correction: 316 → 315

The review quoted 316 `panic("… not in M[5789] scope …")` sites. A grep for `panic(".*not in M[5789] scope` over `internal/frontend/synth_*.go` returns 316 lines, but one is a **comment false positive**, not a call:

```
synth_math.go:29: // Out-of-scope members: exact gc signature + panic("not in M7 scope").
```

The AST-based generator (`go/parser` over the extracted embedded sources — see §8) finds **315 real `panic` calls**, and the (file, line) set matches the grep set exactly apart from that one comment line (verified by `comm` on both sets; zero lines in either direction). Per-file grep counts in the review brief also drift in 4 files (bytes 12→15, strings 7→11, mime_multipart 15→16, enc_csv_gob 9→5); the table in §3 is disk truth. `fail-fast stub` comments: 75 across `internal/frontend/*.go` (68 in `synth_*.go` alone) — the review's 75 reproduces exactly on the wider scope.

## 2. Verdict summary (315 rows)

| shape | COVERED | PANICS | PANICS-via-helper | N/A-interface-only | PROBE-OWED |
| --- | --- | --- | --- | --- | --- |
| direct (concrete call) | 143 | 159 | 10 | 3 | 0 |
| method-value (`f := x.M; f()`)  | 0 | 315 | 0 | 0 | 0 |
| interface-dispatch | 59 | 0 | 0 | 0 | 253 |
| reflect (`Value.Call` / `MethodByName`) | 0 | 0 | 0 | 0 | 315 |

- **direct:** 143 desugar to a non-panicking `__`-helper; 159 have no table row and hit the panic body; 10 desugar to a helper that itself panics (§5); 3 are interface-only (`reflect.Type.FieldByName/FieldByNameFunc/MethodByName` — no concrete call form exists).
- **method-value:** 0 covered. `methodValueNode` (`internal/frontend/export.go:9970`) binds the method's own funcdecl with no pair-desugar, so invoking the value runs the panic body. This holds for all 315 rows, including the 143 direct-covered ones.
- **interface-dispatch:** 59 covered via `m8IfaceMultiResultMethods` (name + result-arity structural match); 253 have no matching iface row (probe-owed — the fallback may still fail loud at compile time instead of panicking at runtime); 3 cross-package name collisions are PROBE-OWED-suspect (§6).
- **reflect:** 315 PROBE-OWED. The `runtime.reflectcall` linkname cited by `FuncIds.funcFor` does not exist (review P2-2), so the observed behaviour (compile-time error vs runtime panic) is unknown until the probe runs.
- **Kinds:** 175 methods, 72 package-level funcs, 50 unexported helpers, 18 `__`-helpers. Distinct (pkg, method, recv): 311 — 4 methods carry two panic sites (public body + `__`-helper).

## 3. Direct coverage by file

| file | sites | COVERED | PANICS | via-helper | N/A |
| --- | --- | --- | --- | --- | --- |
| `synth_archive_tar.go` | 31 | 31 | 0 | 0 | 0 |
| `synth_archive_zip.go` | 29 | 28 | 1 | 0 | 0 |
| `synth_ascii85.go` | 1 | 1 | 0 | 0 | 0 |
| `synth_asn1.go` | 5 | 0 | 3 | 2 | 0 |
| `synth_base32.go` | 6 | 3 | 3 | 0 | 0 |
| `synth_base64.go` | 6 | 3 | 3 | 0 | 0 |
| `synth_binary.go` | 6 | 2 | 4 | 0 | 0 |
| `synth_bytes.go` | 15 | 1 | 14 | 0 | 0 |
| `synth_enc_csv_gob.go` | 5 | 0 | 5 | 0 | 0 |
| `synth_hex.go` | 3 | 3 | 0 | 0 | 0 |
| `synth_io.go` | 30 | 17 | 13 | 0 | 0 |
| `synth_iofs.go` | 13 | 0 | 13 | 0 | 0 |
| `synth_json.go` | 11 | 0 | 11 | 0 | 0 |
| `synth_math.go` | 4 | 2 | 2 | 0 | 0 |
| `synth_mime.go` | 12 | 12 | 0 | 0 | 0 |
| `synth_mime_multipart.go` | 16 | 14 | 2 | 0 | 0 |
| `synth_mime_quotedprintable.go` | 4 | 4 | 0 | 0 | 0 |
| `synth_net.go` | 4 | 0 | 4 | 0 | 0 |
| `synth_neturl.go` | 2 | 0 | 2 | 0 | 0 |
| `synth_os.go` | 42 | 7 | 27 | 8 | 0 |
| `synth_path.go` | 1 | 1 | 0 | 0 | 0 |
| `synth_pem.go` | 1 | 1 | 0 | 0 | 0 |
| `synth_reflect.go` | 12 | 0 | 9 | 0 | 3 |
| `synth_singleflight.go` | 3 | 0 | 3 | 0 | 0 |
| `synth_sort.go` | 4 | 0 | 4 | 0 | 0 |
| `synth_strings.go` | 11 | 8 | 3 | 0 | 0 |
| `synth_time_context.go` | 29 | 5 | 24 | 0 | 0 |
| `synth_xml.go` | 9 | 0 | 9 | 0 | 0 |

Reading: `archive/tar` (31/31), `mime` (12/12), `archive/zip` (28/29) and `mime/multipart` (14/16) are essentially direct-covered; `bytes` is the worst ratio (1/15 — only `Buffer.WriteString` has a row; `Peek/ReadByte/ReadRune/ReadBytes/ReadString/WriteRune/ReadAt/Seek/WriteTo/ReadFrom` have none); `encoding/json` (0/11), `encoding/xml` (0/9), `encoding/gob` (0/5), `io/fs` (0/13), `net` (0/4) have no direct coverage at all. By scope: M5 5/29 covered, M7 108/165, M8 29/110, M9 1/11.

## 4. How each column is determined (mechanism, with citations)

All pair tables live in `internal/frontend/mrdesugar_m5.go` (4556 lines): `m5StaticMREntries` (:184, 7 rows), `m7MultiResultFuncs` (:283, 295 rows), `m8MultiResultFuncs` (:686, 322 rows), `osMultiResultFuncs` (:1101, 36 rows), `m9MultiResultFuncs` (:1173, 49 rows), `ioMultiResultFuncs` (:1251, 10 rows), `m8IfaceMultiResultMethods` (:1292, 47 rows). Row shape: `{pkg, name, recv, helper, pair, fields, …}`.

- **direct = COVERED** iff a concrete-table row `(pkg, name, recv)` exists for the method and the named `__`-helper does not itself contain a scope-panic (§5). Join key uses the panic message's qualified name (`archive/tar.Next` → pkg `archive/tar`, name `Next`) and the enclosing func's receiver type; a `pkg.Type` message prefix falls back to `pkg` (`os.File.SyscallConn` → `os`). The 27 fallback joins were each verified by hand: every one names the exact `__`-helper for the enclosing method (e.g. `bytes.Buffer.WriteString` → `__bytesBufferWriteString`).
- **direct = PANICS** iff no concrete row exists: the call site is emitted as a plain call to the panicking funcdecl (the R2-P0-1a pattern). Single-result and void methods (81 + 6 rows) can never have a pair row — they panic on direct call by construction.
- **method-value = PANICS** for all rows. `methodValueNode` (`export.go:9970-10067`) resolves the method and emits a `"methodvalue"` wire node with `obj` = the method's funcID — no `mrPlanForFuncValue` lookup, no `__`-helper substitution. Contrast the named-function value path (`synth_machinery.go:2082`, `export.go:8427`), which DOES lower through `mrPlanForFuncValue` to the pair helper — that path covers `pkg.Fn` values, not bound method values. `mrInfoForFuncValueInit` (`export.go:7530-7585`) likewise handles only `*ast.Ident` and package-qualified `*ast.SelectorExpr`; a receiver-qualified selector (`f := tr.Read`) falls through with `fn == nil`. The snapshot exclusion is the live pin: `test/snapshots/go1.27.1.json:43` and `test/differential/backends-p3.json:107` exclude program `40_method_values` ("backend-specific: method values not in scope").
- **interface-dispatch = COVERED** iff an `m8IfaceMultiResultMethods` row matches on (method name, result count) — the classifier is structural on (name, result count, positional result types) per `classifyIfaceMultiResultCall` (`mrdesugar_m5.go:2610`). Same-package rows and the canonical `io`/`net` families (`Read/Write/ReadByte/WriteTo/ReadFrom/WriteString`) count as COVERED; cross-package name collisions (§6) stay PROBE-OWED. Note the classifier cannot be confirmed from the table alone — the pair field TYPES must align, which only the probe (or a type-checked test) shows.
- **reflect = PROBE-OWED** for all rows. No `reflectcall` desugar exists for these methods; the probe (§7, program R-3) establishes whether the shape fails at compile time (scope error) or at runtime (panic), and with what message.
- **COVERED means "desugars to a non-panicking helper", not "behavior verified".** Helper correctness is the differential corpus's job (wave1/2/3 runners); this matrix tracks panic traps only.

## 5. Helper traps: 10 rows desugar into a panic (direct = PANICS-via-helper)

A table row exists, so a direct call desugars — but the named `__`-helper's own body contains a scope-panic. Retiring these rows means implementing the helper, not adding a row.

| file:line | method | helper (panics at) |
| --- | --- | --- |
| `synth_asn1.go:778` | `encoding/asn1.Marshal` | `__asn1Marshal@synth_asn1.go:778` |
| `synth_asn1.go:807` | `encoding/asn1.Marshal` | `__asn1Marshal@synth_asn1.go:778` |
| `synth_os.go:2375` | `os.File.SyscallConn` | `__fileSyscallConn@synth_os.go:2390` |
| `synth_os.go:2400` | `os.OpenRoot` | `__openRoot@synth_os.go:2411` |
| `synth_os.go:2411` | `os.OpenRoot` | `__openRoot@synth_os.go:2411` |
| `synth_os.go:2456` | `os.Executable` | `__executable@synth_os.go:2468` |
| `synth_os.go:2468` | `os.Executable` | `__executable@synth_os.go:2468` |
| `synth_os.go:2649` | `os.StartProcess` | `__startProcess@synth_os.go:2661` |
| `synth_os.go:2661` | `os.StartProcess` | `__startProcess@synth_os.go:2661` |
| `synth_os.go:2681` | `os.Process.Wait` | `__processWait@synth_os.go:2693` |

The `os` four (`OpenRoot`, `Executable`, `StartProcess`, plus `File.SyscallConn` and `Process.Wait`) are the P1-1 "genuinely-missing" subset (needs-design): no helper implementation exists at all. `encoding/asn1.Marshal` desugars to `__asn1Marshal`, which panics for composite types (needs reflect). The other `__`-helper panic sites (`io/fs`: 12 sites — R2-P0-1b; `os.Root.*` helpers are covered, not traps) are already PANICS rows, not traps.

## 6. Cross-package iface collisions: 3 PROBE-OWED-suspect rows

| file:line | method | matched iface row |
| --- | --- | --- |
| `synth_asn1.go:818` | `encoding/asn1.Unmarshal` | `m8IfaceMultiResultMethods:crypto/elliptic.Unmarshal` |
| `synth_neturl.go:195` | `url.URL.MarshalBinary` | `m8IfaceMultiResultMethods:hash.MarshalBinary` |
| `synth_neturl.go:199` | `url.URL.AppendBinary` | `m8IfaceMultiResultMethods:hash.AppendBinary` |

The iface classifier matches structurally by name, so `encoding/asn1.Unmarshal` matches the `crypto/elliptic` Unmarshal row and `url.URL.AppendBinary/MarshalBinary` match the `hash` rows. Whether the pair types align (and the desugar is therefore correct, a wrong-output bug, or a compile-time error) is probe-owed. The 6 `(unreachable: desugared)` rows (`reflect.Type.FieldByName/FieldByNameFunc/MethodByName`, `reflect.Value.FieldByIndexErr/TryRecv`, `reflect.Select`) are author claims that desugar makes the body unreachable — the probe must confirm each; a single observed panic falsifies the claim for that row.

## 7. Probe corpus (flips STATIC cells to PROBED)

Run under `nice -n 10` with `load-shed.sh backoff` gating; each program is a differential program (gc leg + graalgo leg). Programs live in the resume lane's worktree until placed under `test/corpus/wave2/shape-coverage/` (see §8). Expected outcomes below are STATIC predictions — a mismatch is a finding, not a probe failure.

| id | shape × method | program sketch | STATIC prediction |
| --- | --- | --- | --- |
| P-MV-1 | method-value × `archive/tar.Reader.Read` (direct-covered) | `tr := tar.NewReader(...); f := tr.Read; n, err := f(p)` | runtime panic `archive/tar.Read: not in M7 scope …` |
| P-MV-2 | method-value × `bytes.Buffer.WriteString` (direct-covered) | `b := &bytes.Buffer{}; f := b.WriteString; f("x")` | runtime panic `bytes.Buffer.WriteString: not in M7 scope …` |
| P-MV-3 | method-value × `os.File.SyscallConn` (helper-trap) | `f := file.SyscallConn; f()` | runtime panic (helper trap — same message as direct) |
| P-IF-1 | interface-dispatch × `archive/tar.Reader.Next` (no iface row) | `var x interface{ Next() (*tar.Header, error) } = tr; x.Next()` | runtime panic `archive/tar.Next: not in M7 scope …` (or compile-time scope error — record which) |
| P-IF-2 | interface-dispatch × `bytes.Buffer.Read` via `io.Reader` (iface row exists) | `var r io.Reader = &bytes.Buffer{}; r.Read(p)` — control: must NOT panic | no panic; reads via `__bytesBufferRead` pair helper |
| P-IF-3 | interface-dispatch × `encoding/asn1.Unmarshal` (cross-pkg suspect) | `var u interface{ Unmarshal([]byte, any) error } = ...` — see §6 | PROBE-OWED: record panic / wrong-output / correct |
| P-RF-1 | reflect × `archive/tar.Reader.Read` | `m := reflect.ValueOf(tr).MethodByName("Read"); m.Call(...)` | PROBE-OWED: compile-time scope error vs runtime panic — record which + message |
| P-RF-2 | reflect × `reflect.Type.FieldByName` (unreachable claim) | `t := reflect.TypeOf(0); t.FieldByName("X")` — direct call through interface type | STATIC claim: no panic (desugared). One observed panic falsifies the claim |
| P-DR-1 | direct × `bytes.Buffer.Peek` (no row; control) | `b.Peek(1)` | runtime panic `bytes.Buffer.Peek: not in M7 scope …` |
| P-DR-2 | direct × `encoding/asn1.Marshal` (helper trap; control) | `asn1.Marshal(v)` | runtime panic via `__asn1Marshal` |
| P-DR-3 | direct × `os.Root.Open` (covered; control) | `root.Open("f")` | no panic (desugars to `__open`) |

## 8. Negative E2E tests (one per uncovered shape class)

Vehicle: the wave2 runner's `.divergent` + `.panic` markers (`test/corpus/wave2/run_wave.sh:146-165` — exit comparison skipped by design for divergent programs; stdout must still match; graalgo's first filtered stderr line must equal the `.panic` file). A `.divergent` without `.panic` fails the harness, so every negative test pins its message. Home: new dir `test/corpus/wave2/shape-coverage/` with a thin `run_differential.sh` (`exec ../run_wave.sh shape-coverage`, following `test/corpus/wave2/reflect/run_differential.sh`). Wave1/wave3 runners do NOT support `.divergent` — do not place these tests there.

### T-MV — method-value shape (`01_method_value_tar_read.go`)

```go
package main

import (
	"archive/tar"
	"bytes"
)

func main() {
	// Direct call desugars (table row exists); the method value must fail loud.
	tr := tar.NewReader(bytes.NewReader(nil))
	f := tr.Read
	_, _ = f(make([]byte, 8))
}
```

`.divergent` (empty marker) + `.panic`:

```
panic: archive/tar.Read: not in M7 scope (multi-result calls need WS D; re-test when it lands)
```

### T-IF — interface-dispatch shape (`02_iface_dispatch_tar_next.go`)

```go
package main

import (
	"archive/tar"
	"bytes"
)

func main() {
	// No iface-table row for Next: interface dispatch must fail loud.
	tr := tar.NewReader(bytes.NewReader(nil))
	var x interface{ Next() (*tar.Header, error) } = tr
	_, _ = x.Next()
}
```

`.divergent` + `.panic` (exact first stderr line TBD by probe P-IF-1 — if the probe shows a compile-time scope error instead of a runtime panic, this test becomes a compile-error test and the matrix cell flips to COMPILE-ERROR):

```
panic: archive/tar.Next: not in M7 scope (multi-result calls need WS D; re-test when it lands)
```

### T-RF — reflect shape (`03_reflect_tar_read.go`)

```go
package main

import (
	"archive/tar"
	"bytes"
	"fmt"
	"reflect"
)

func main() {
	// Reflect call into a panicking method must fail loud, not silently misbehave.
	tr := tar.NewReader(bytes.NewReader(nil))
	m := reflect.ValueOf(tr).MethodByName("Read")
	m.Call([]reflect.Value{reflect.ValueOf(make([]byte, 8))})
}
```

`.divergent` + `.panic`: exact expected output TBD by probe P-RF-1 (compile-time scope error vs runtime panic — whichever it is, the test pins it; a silent success or a different message fails the test).

## 9. Repo homes (proposed; resume lane confirms)

- This document → [SHAPE_COVERAGE_MATRIX.md](SHAPE_COVERAGE_MATRIX.md) (living doc; updated on every row retirement).
- Machine-readable matrix → [shape_coverage_matrix.tsv](../../internal/frontend/shape_coverage_matrix.tsv) (same columns as `matrix_final.tsv`; regenerated by the generator, never hand-edited).
- Generator → [gen_matrix.go](../../tools/shape-coverage/gen_matrix.go) + [build_final.py](../../tools/shape-coverage/build_final.py) + [build_doc.py](../../tools/shape-coverage/build_doc.py) (read-only; regenerate after any synth/table change and diff the TSV — a diff is a finding).
- Negative tests → [shape-coverage/](../../test/corpus/wave2/shape-coverage/) (§8).
- Probe programs → same dir as `P-*.go` scratch (not committed) or under `test/corpus/wave2/shape-coverage/` as probe programs once their outcomes are pinned.

## 10. Retirement protocol (for sibling lanes)

Sibling lanes (`review-1111-14-r2p0-1a`, `review-1111-14-r2p0-1b`, `review-1111-14-p1-1-stubs`) own per-method implementations; this lane owns the matrix + negative tests. A row retires when: (1) the panic is deleted (implementation or real desugar in the generator — never a hand-edit of generated files); (2) a differential program proving the method works in the retired shape is added to the corpus; (3) the matrix row flips to COVERED-probed and this doc + the TSV are regenerated in the same commit. The negative tests in §8 flip to positive differential tests (or are deleted) when their shape class gains a general mechanism — never silently.

## 11. Open questions / needs-design

1. **General method-value mechanism:** eta-expand a bound method value to the pair helper (the named-function precedent in `mrPlanForFuncValue`) instead of binding the panicking funcdecl. Design owed: receiver capture + pair-returning closure + call-site desugar through `userMRVars`. Until it lands, all 315 method-value cells stay PANICS.
2. **Reflect calls:** no `reflectcall` desugar exists; decide compile-time scope error vs runtime panic per method (probe P-RF-1/P-RF-2 first).
3. **Interface fallback for unlisted methods:** `classifyIfaceFallbackMultiResultCall` exists — determine its exact behaviour for methods with no iface row (probe P-IF-1).
4. **`os.Root.*` COVERED rows** desugar to the shared `__open/__create/...` helpers — verify the root restriction survives the desugar (differential test owed; not a matrix question, flagged for the R2-P0-1b / os lane).
5. **The 6 `(unreachable: desugared)` claims** (§6) are author assertions, not observations — each needs a probe run before its direct cell can be trusted.

## 12. Full matrix (315 rows)

Columns: `direct` ∈ {COVERED, PANICS, PANICS-via-helper, N/A-interface-only}; `methodValue` = PANICS (all rows — §4); `ifaceDispatch` ∈ {COVERED, PROBE-OWED, PROBE-OWED-cross-pkg}; `reflect` = PROBE-OWED (all rows — §4). `kind`: method / pkg-func / unexported-helper / __-helper. `table`: the pair-table row matched, or empty. `unreach?`: message claims `(unreachable: desugared)`. All verdicts STATIC unless the probe corpus has flipped them (none yet).

| file:line | scope | method | kind | nRes | table | direct | iface | reflect | unreach? |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `synth_archive_tar.go:612` | M7 | `archive/tar.allowedFormats` | unexported-helper | 3 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:824` | M7 | `archive/tar.FileInfoHeader` | pkg-func | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:1123` | M7 | `archive/tar.computeChecksum` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:1284` | M7 | `archive/tar.Next` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:1402` | M7 | `archive/tar.next` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:1503` | M7 | `archive/tar.readGNUSparsePAXHeaders` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:1604` | M7 | `archive/tar.parsePAX` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:1739` | M7 | `archive/tar.readHeader` | unexported-helper | 3 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:1808` | M7 | `archive/tar.readOldGNUSparseMap` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:1898` | M7 | `archive/tar.readGNUSparseMap1x0` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:1948` | M7 | `archive/tar.readGNUSparseMap0x1` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:1967` | M7 | `archive/tar.appendSparseEntry` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:2000` | M7 | `archive/tar.Read` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_archive_tar.go:2033` | M7 | `archive/tar.writeTo` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:2071` | M7 | `archive/tar.Read` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_archive_tar.go:2087` | M7 | `archive/tar.WriteTo` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_archive_tar.go:2162` | M7 | `archive/tar.Read` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_archive_tar.go:2229` | M7 | `archive/tar.WriteTo` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_archive_tar.go:2269` | M7 | `archive/tar.mustReadFull` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:2297` | M7 | `archive/tar.tryReadFull` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:2319` | M7 | `archive/tar.readSpecialFile` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:2844` | M7 | `archive/tar.splitUSTARPath` | unexported-helper | 3 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:2874` | M7 | `archive/tar.Write` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_archive_tar.go:2907` | M7 | `archive/tar.readFrom` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:2970` | M7 | `archive/tar.Write` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_archive_tar.go:2986` | M7 | `archive/tar.ReadFrom` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_archive_tar.go:3061` | M7 | `archive/tar.Write` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_archive_tar.go:3130` | M7 | `archive/tar.ReadFrom` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_archive_tar.go:3425` | M7 | `archive/tar.parsePAXTime` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:3499` | M7 | `archive/tar.parsePAXRecord` | unexported-helper | 4 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_tar.go:3531` | M7 | `archive/tar.formatPAXRecord` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:120` | M7 | `archive/zip.OpenReader` | pkg-func | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:154` | M7 | `archive/zip.NewReader` | pkg-func | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:273` | M7 | `archive/zip.DataOffset` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:328` | M7 | `archive/zip.Open` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:351` | M7 | `archive/zip.OpenRaw` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:371` | M7 | `archive/zip.Read` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_archive_zip.go:402` | M7 | `archive/zip.Stat` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:456` | M7 | `archive/zip.Read` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_archive_zip.go:489` | M7 | `archive/zip.findBodyOffset` | unexported-helper | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:777` | M7 | `archive/zip.readDirectoryEnd` | unexported-helper | 3 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:815` | M7 | `archive/zip.findDirectory64End` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:939` | M7 | `archive/zip.stat` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:974` | M7 | `archive/zip.Info` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:1123` | M7 | `archive/zip.Open` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:1146` | M7 | `archive/zip.split` | unexported-helper | 3 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:1217` | M7 | `archive/zip.Stat` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:1233` | M7 | `archive/zip.Read` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_archive_zip.go:1274` | M7 | `archive/zip.ReadDir` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:1530` | M7 | `archive/zip.Create` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:1566` | M7 | `archive/zip.detectUTF8` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:1717` | M7 | `archive/zip.CreateHeader` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:1856` | M7 | `archive/zip.CreateRaw` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:1986` | M7 | `archive/zip.Write` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_archive_zip.go:2072` | M7 | `archive/zip.Write` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_archive_zip.go:2328` | M7 | `archive/zip.Info` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:2369` | M7 | `archive/zip.FileInfoHeader` | pkg-func | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:2442` | M7 | `archive/zip.timeToMsDosTime` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_archive_zip.go:2659` | M7 | `archive/zip.Write` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_archive_zip.go:2713` | M7 | `archive/zip.Read` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_ascii85.go:204` | M7 | `encoding/ascii85.Decode` | pkg-func | 3 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_asn1.go:778` | M7 | `encoding/asn1.Marshal` | __-helper | 1 | m7MultiResultFuncs | PANICS-via-helper | PROBE-OWED | PROBE-OWED | no |
| `synth_asn1.go:807` | M7 | `encoding/asn1.Marshal` | pkg-func | 2 | m7MultiResultFuncs | PANICS-via-helper | PROBE-OWED | PROBE-OWED | no |
| `synth_asn1.go:818` | M7 | `encoding/asn1.Unmarshal` | pkg-func | 2 | — | PANICS | PROBE-OWED-cross-pkg | PROBE-OWED | no |
| `synth_asn1.go:826` | M7 | `encoding/asn1.MarshalWithParams` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_asn1.go:834` | M7 | `encoding/asn1.UnmarshalWithParams` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_base32.go:244` | M8 | `base32.encoder.Write` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_base32.go:249` | M8 | `base32.encoder.Close` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_base32.go:271` | M8 | `base32.decoder.Read` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_base32.go:469` | M7 | `encoding/base32.Decode` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_base32.go:476` | M7 | `encoding/base32.DecodeString` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_base32.go:483` | M7 | `encoding/base32.AppendDecode` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_base64.go:569` | M7 | `encoding/base64.Decode` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_base64.go:576` | M7 | `encoding/base64.DecodeString` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_base64.go:583` | M7 | `encoding/base64.AppendDecode` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_base64.go:625` | M7 | `encoder.Write` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_base64.go:708` | M7 | `newlineFilteringReader.Read` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_base64.go:758` | M7 | `decoder.Read` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_binary.go:298` | M7 | `encoding/binary.Uvarint` | pkg-func | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_binary.go:340` | M7 | `encoding/binary.Varint` | pkg-func | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_binary.go:348` | M7 | `encoding/binary.Append` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_binary.go:356` | M7 | `encoding/binary.Encode` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_binary.go:364` | M7 | `encoding/binary.Decode` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_binary.go:491` | M7 | `encoding/binary.Size` | pkg-func | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_bytes.go:9935` | M7 | `bytes.Buffer.Peek` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_bytes.go:10070` | M7 | `bytes.Buffer.WriteString` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_bytes.go:10105` | M8 | `bytes.Buffer.ReadFrom` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_bytes.go:10144` | M8 | `bytes.Buffer.WriteTo` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_bytes.go:10169` | M7 | `bytes.Buffer.WriteRune` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_bytes.go:10227` | M7 | `bytes.Buffer.ReadByte` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_bytes.go:10239` | M7 | `bytes.Buffer.ReadRune` | method | 3 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_bytes.go:10285` | M7 | `bytes.Buffer.ReadBytes` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_bytes.go:10293` | M7 | `bytes.Buffer.readSlice` | unexported-helper | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_bytes.go:10306` | M7 | `bytes.Buffer.ReadString` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_bytes.go:10380` | M7 | `bytes.Reader.ReadAt` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_bytes.go:10388` | M7 | `bytes.Reader.ReadByte` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_bytes.go:10406` | M7 | `bytes.Reader.ReadRune` | method | 3 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_bytes.go:10430` | M7 | `bytes.Reader.Seek` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_bytes.go:10439` | M8 | `bytes.Reader.WriteTo` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_enc_csv_gob.go:732` | M7 | `Reader.Read is` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_enc_csv_gob.go:767` | M7 | `Reader.ReadAll is` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_enc_csv_gob.go:794` | M7 | `Reader.FieldPos is` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_enc_csv_gob.go:1418` | M7 | `gob.Register` | pkg-func | 0 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_enc_csv_gob.go:1434` | M7 | `gob.RegisterName` | pkg-func | 0 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_hex.go:232` | M7 | `encoding/hex.Decode` | pkg-func | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_hex.go:239` | M7 | `encoding/hex.AppendDecode` | pkg-func | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_hex.go:253` | M7 | `encoding/hex.DecodeString` | pkg-func | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_io.go:260` | M8 | `io.WriteString` | pkg-func | 2 | ioMultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_io.go:322` | M8 | `io.ReadAtLeast` | pkg-func | 2 | ioMultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_io.go:357` | M8 | `io.ReadFull` | pkg-func | 2 | ioMultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_io.go:378` | M8 | `io.CopyN` | pkg-func | 2 | ioMultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_io.go:409` | M8 | `io.Copy` | pkg-func | 2 | ioMultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_io.go:431` | M8 | `io.CopyBuffer` | pkg-func | 2 | ioMultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_io.go:518` | M8 | `io.LimitedReader.Read` | method | 2 | ioMultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_io.go:572` | M8 | `io.SectionReader.Read` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_io.go:581` | M8 | `io.SectionReader.Seek` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_io.go:615` | M8 | `io.SectionReader.ReadAt` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_io.go:629` | M8 | `io.SectionReader.Outer` | method | 3 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_io.go:663` | M8 | `io.OffsetWriter.Write` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_io.go:671` | M8 | `io.OffsetWriter.WriteAt` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_io.go:677` | M8 | `io.OffsetWriter.Seek` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_io.go:723` | M8 | `io.teeReader.Read` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_io.go:743` | M8 | `io.discard.Write` | method | 2 | ioMultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_io.go:757` | M8 | `io.discard.WriteString` | method | 2 | ioMultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_io.go:771` | M8 | `io.discard.ReadFrom` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_io.go:802` | M8 | `io.nopCloserWriterTo.WriteTo` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_io.go:813` | M8 | `io.ReadAll` | pkg-func | 2 | ioMultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_io.go:869` | M8 | `io.eofReader.Read` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_io.go:881` | M8 | `io.multiReader.Read` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_io.go:889` | M8 | `io.multiReader.WriteTo` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_io.go:913` | M8 | `io.multiWriter.Write` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_io.go:923` | M8 | `io.multiWriter.WriteString` | method | 2 | ioMultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_io.go:994` | M8 | `io.pipe.read` | unexported-helper | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_io.go:1036` | M8 | `io.pipe.write` | unexported-helper | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_io.go:1112` | M8 | `io.PipeReader.Read` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_io.go:1154` | M8 | `io.PipeWriter.Write` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_io.go:1204` | M8 | `io.Pipe` | pkg-func | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_iofs.go:489` | M8 | `fs.__fsReadFile` | __-helper | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_iofs.go:504` | M8 | `fs.__fsReadDir` | __-helper | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_iofs.go:529` | M8 | `fs.__fsStat` | __-helper | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_iofs.go:892` | M8 | `fs.__fsGlobWithLimit` | __-helper | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_iofs.go:923` | M8 | `fs.WalkDir` | pkg-func | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_iofs.go:970` | M8 | `fs.__fsSub` | __-helper | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_iofs.go:1019` | M8 | `fs.__subFSOpen` | __-helper | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_iofs.go:1027` | M8 | `fs.__subFSReadDir` | __-helper | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_iofs.go:1035` | M8 | `fs.__subFSReadFile` | __-helper | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_iofs.go:1043` | M8 | `fs.__subFSGlob` | __-helper | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_iofs.go:1051` | M8 | `fs.__subFSSub` | __-helper | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_iofs.go:1092` | M8 | `fs.__fsReadLink` | __-helper | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_iofs.go:1105` | M8 | `fs.__fsLstat` | __-helper | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_json.go:1200` | M7 | `json.RawMessage.UnmarshalJSON` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_json.go:1956` | M7 | `json.Unmarshal` | pkg-func | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_json.go:2133` | M7 | `json.Marshaler` | unexported-helper | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_json.go:2143` | M7 | `.not in M7 scope (needs reflect, P3-M8)` | unexported-helper | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_json.go:2415` | M7 | `json.Decoder.Decode` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_json.go:2578` | M7 | `json.Decoder.Buffered` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_json.go:2595` | M7 | `json.jsonReadAll` | unexported-helper | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_json.go:2633` | M7 | `json.Encoder.Encode` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_json.go:2656` | M7 | `json.Indent` | pkg-func | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_json.go:2663` | M7 | `json.Compact` | pkg-func | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_json.go:2667` | M7 | `json.HTMLEscape` | pkg-func | 0 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_math.go:3713` | M7 | `math.Frexp` | pkg-func | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_math.go:3722` | M7 | `math.Modf` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_math.go:3730` | M7 | `math.Lgamma` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_math.go:3737` | M7 | `math.Sincos` | pkg-func | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime.go:269` | M7 | `mime.ParseMediaType` | pkg-func | 3 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime.go:305` | M7 | `mime.decode2231Enc` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime.go:330` | M7 | `mime.consumeToken` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime.go:388` | M7 | `mime.consumeValue` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime.go:431` | M7 | `mime.consumeMediaParam` | unexported-helper | 3 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime.go:478` | M7 | `mime.percentHexUnescape` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime.go:754` | M7 | `mime.Decode` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime.go:842` | M7 | `mime.DecodeHeader` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime.go:865` | M7 | `mime.decode` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime.go:956` | M7 | `mime.qDecode` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime.go:981` | M7 | `mime.readHexByte` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime.go:1006` | M7 | `mime.fromHex` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime_multipart.go:181` | M7 | `mime/multipart.Read` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_mime_multipart.go:214` | M7 | `mime/multipart.newPart` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime_multipart.go:247` | M7 | `mime/multipart.Read` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_mime_multipart.go:300` | M7 | `mime/multipart.Read` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_mime_multipart.go:366` | M7 | `mime/multipart.scanUntilBoundary` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime_multipart.go:479` | M7 | `mime/multipart.NextPart` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime_multipart.go:500` | M7 | `mime/multipart.NextRawPart` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime_multipart.go:572` | M7 | `mime/multipart.nextPart` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime_multipart.go:660` | M7 | `mime/multipart.ReadForm` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime_multipart.go:855` | M7 | `mime/multipart.readForm` | unexported-helper | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_mime_multipart.go:938` | M7 | `mime/multipart.Open` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime_multipart.go:1101` | M7 | `mime/multipart.CreatePart` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime_multipart.go:1142` | M7 | `mime/multipart.CreateFormFile` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime_multipart.go:1163` | M7 | `mime/multipart.CreateFormField` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime_multipart.go:1237` | M7 | `mime/multipart.Write` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_mime_multipart.go:1249` | M9 | `multipart.readMIMEHeader` | unexported-helper | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_mime_quotedprintable.go:65` | M7 | `mime/quotedprintable.fromHex` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime_quotedprintable.go:92` | M7 | `mime/quotedprintable.readHexByte` | unexported-helper | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_mime_quotedprintable.go:193` | M7 | `mime/quotedprintable.Read` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_mime_quotedprintable.go:270` | M7 | `mime/quotedprintable.Write` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_net.go:648` | M9 | `net.LookupHost` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_net.go:653` | M9 | `net.LookupIP` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_net.go:658` | M9 | `net.LookupPort` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_net.go:663` | M9 | `net.InterfaceAddrs` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_neturl.go:195` | M9 | `url.URL.MarshalBinary` | method | 2 | — | PANICS | PROBE-OWED-cross-pkg | PROBE-OWED | no |
| `synth_neturl.go:199` | M9 | `url.URL.AppendBinary` | method | 2 | — | PANICS | PROBE-OWED-cross-pkg | PROBE-OWED | no |
| `synth_os.go:1616` | M8 | `os.Chown` | pkg-func | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:1622` | M8 | `os.Lchown` | pkg-func | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:1628` | M8 | `os.File.Chown` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:1667` | M8 | `os.File.SetDeadline` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2008` | M8 | `os.Expand` | pkg-func | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2013` | M8 | `os.getShellName` | unexported-helper | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2375` | M8 | `os.File.SyscallConn` | method | 2 | osMultiResultFuncs | PANICS-via-helper | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2390` | M8 | `os.File.SyscallConn` | __-helper | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2400` | M8 | `os.OpenRoot` | pkg-func | 2 | osMultiResultFuncs | PANICS-via-helper | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2411` | M8 | `os.OpenRoot` | __-helper | 1 | osMultiResultFuncs | PANICS-via-helper | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2416` | M8 | `os.Root.Close` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2419` | M8 | `os.Root.Open` | method | 2 | osMultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2422` | M8 | `os.Root.Create` | method | 2 | osMultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2425` | M8 | `os.Root.OpenFile` | method | 2 | osMultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2428` | M8 | `os.Root.Stat` | method | 2 | osMultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2431` | M8 | `os.Root.Lstat` | method | 2 | osMultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2434` | M8 | `os.Root.Mkdir` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2437` | M8 | `os.Root.Remove` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2440` | M8 | `os.Root.Rename` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2443` | M8 | `os.Root.ReadFile` | method | 2 | osMultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2446` | M8 | `os.Root.WriteFile` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2449` | M8 | `os.Root.ReadDir` | method | 2 | osMultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2456` | M8 | `os.Executable` | pkg-func | 2 | osMultiResultFuncs | PANICS-via-helper | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2468` | M8 | `os.Executable` | __-helper | 1 | osMultiResultFuncs | PANICS-via-helper | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2601` | M8 | `os.CopyFS` | pkg-func | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2649` | M8 | `os.StartProcess` | pkg-func | 2 | osMultiResultFuncs | PANICS-via-helper | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2661` | M8 | `os.StartProcess` | __-helper | 1 | osMultiResultFuncs | PANICS-via-helper | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2666` | M8 | `os.FindProcess` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2671` | M8 | `os.Process.Signal` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2676` | M8 | `os.Process.Kill` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2681` | M8 | `os.Process.Wait` | method | 2 | osMultiResultFuncs | PANICS-via-helper | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2693` | M8 | `os.Process.Wait` | __-helper | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2698` | M8 | `os.Process.Release` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2706` | M8 | `os.ProcessState.String` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2711` | M8 | `os.ProcessState.Exited` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2716` | M8 | `os.ProcessState.Success` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2722` | M8 | `os.ProcessState.Sys` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2727` | M8 | `os.ProcessState.SysUsage` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2732` | M8 | `os.ProcessState.Pid` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2737` | M8 | `os.ProcessState.ExitCode` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2742` | M8 | `os.ProcessState.UserTime` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_os.go:2747` | M8 | `os.ProcessState.SystemTime` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_path.go:270` | M9 | `path.Split` | pkg-func | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_pem.go:438` | M7 | `encoding/pem.Decode` | pkg-func | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_reflect.go:728` | M8 | `reflect.Type.FieldByName` | method | 2 | — | N/A-interface-only | COVERED | PROBE-OWED | yes |
| `synth_reflect.go:732` | M8 | `reflect.Type.FieldByNameFunc` | method | 2 | — | N/A-interface-only | COVERED | PROBE-OWED | yes |
| `synth_reflect.go:744` | M8 | `reflect.Type.MethodByName` | method | 2 | — | N/A-interface-only | COVERED | PROBE-OWED | yes |
| `synth_reflect.go:1069` | M8 | `reflect.TypeAssert` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_reflect.go:1179` | M8 | `reflect.Value.FieldByIndexErr` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | yes |
| `synth_reflect.go:1643` | M8 | `reflect.Value.TryRecv` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | yes |
| `synth_reflect.go:1660` | M8 | `reflect.Select` | pkg-func | 3 | — | PANICS | PROBE-OWED | PROBE-OWED | yes |
| `synth_reflect.go:1771` | M8 | `reflect.SliceAt` | pkg-func | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_reflect.go:1907` | M8 | `reflect.Value.Pointer` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_reflect.go:1912` | M8 | `reflect.Value.UnsafeAddr` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_reflect.go:1917` | M8 | `reflect.Value.InterfaceData` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_reflect.go:2006` | M8 | `reflect.Value.Seq` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_singleflight.go:62` | M9 | `singleflight.Group.Do` | method | 3 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_singleflight.go:68` | M9 | `singleflight.Group.DoChan` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_singleflight.go:73` | M9 | `singleflight.Group.doCall` | unexported-helper | 0 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_sort.go:254` | M7 | `sort.Find` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_sort.go:290` | M7 | `sort.Slice` | pkg-func | 0 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_sort.go:300` | M7 | `sort.SliceStable` | pkg-func | 0 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_sort.go:310` | M7 | `sort.SliceIsSorted` | pkg-func | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_strings.go:1735` | M7 | `strings.Title` | pkg-func | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_strings.go:2209` | M7 | `strings.Builder.Write` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_strings.go:2223` | M7 | `strings.Builder.WriteRune` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_strings.go:2229` | M7 | `strings.Builder.WriteString` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_strings.go:2312` | M8 | `strings.Reader.Read` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_strings.go:2343` | M8 | `strings.Reader.ReadAt` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_strings.go:2378` | M8 | `strings.Reader.ReadByte` | method | 2 | m7MultiResultFuncs | COVERED | COVERED | PROBE-OWED | no |
| `synth_strings.go:2413` | M8 | `strings.Reader.ReadRune` | method | 3 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_strings.go:2453` | M8 | `strings.Reader.Seek` | method | 2 | m7MultiResultFuncs | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_strings.go:2487` | M8 | `strings.Reader.WriteTo` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_strings.go:2541` | M8 | `strings.Replacer.WriteString` | method | 2 | — | PANICS | COVERED | PROBE-OWED | no |
| `synth_time_context.go:462` | M5 | `time.ParseDuration` | pkg-func | 2 | m5StaticMREntries | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:877` | M5 | `time.Time.GoString` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:879` | M5 | `time.Time.AddDate` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:882` | M5 | `time.Time.Date` | method | 3 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:884` | M5 | `time.Time.Year` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:885` | M5 | `time.Time.Month` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:886` | M5 | `time.Time.Day` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:887` | M5 | `time.Time.Weekday` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:889` | M5 | `time.Time.Clock` | method | 3 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:891` | M5 | `time.Time.Hour` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:892` | M5 | `time.Time.Minute` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:893` | M5 | `time.Time.Second` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:894` | M5 | `time.Time.Nanosecond` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:895` | M5 | `time.Time.YearDay` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:899` | M5 | `time.Time.In` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:901` | M5 | `time.Time.Location` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:903` | M5 | `time.Time.Zone` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:925` | M5 | `time.Parse` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:1216` | M5 | `emptyCtx.Deadline` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:1277` | M5 | `cancelCtx.Deadline` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:1378` | M5 | `context.WithCancel` | pkg-func | 2 | m5StaticMREntries | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:1382` | M5 | `context.WithCancelCause` | pkg-func | 2 | m5StaticMREntries | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:1454` | M5 | `timerCtx.Deadline` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:1508` | M5 | `context.WithDeadline` | pkg-func | 2 | m5StaticMREntries | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:1512` | M5 | `context.WithTimeout` | pkg-func | 2 | m5StaticMREntries | COVERED | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:1516` | M5 | `context.WithDeadlineCause` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:1520` | M5 | `context.WithTimeoutCause` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:1565` | M5 | `valueCtx.Deadline` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_time_context.go:1645` | M5 | `withoutCancelCtx.Deadline` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_xml.go:337` | M7 | `xml.NewTokenDecoder` | pkg-func | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_xml.go:730` | M7 | `xml.Decoder.InputPos` | method | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_xml.go:2390` | M7 | `xml.Unmarshal` | pkg-func | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_xml.go:2396` | M7 | `xml.Decoder.Decode` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_xml.go:2402` | M7 | `xml.Decoder.DecodeElement` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_xml.go:2408` | M7 | `xml.Marshal` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_xml.go:2415` | M7 | `xml.MarshalIndent` | pkg-func | 2 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_xml.go:2420` | M7 | `xml.Encoder.Encode` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |
| `synth_xml.go:2426` | M7 | `xml.Encoder.EncodeElement` | method | 1 | — | PANICS | PROBE-OWED | PROBE-OWED | no |

## Appendix: validation log

- `grep -c 'panic(".*not in M[5789] scope' internal/frontend/synth_*.go` = 316 lines; AST generator finds 315 real calls; set difference is exactly `synth_math.go:29` (comment).
- Per-scope (panic calls): M5 29, M7 165, M8 110, M9 11.
- `fail-fast stub` comments: 75 in `internal/frontend/*.go`, 68 in `synth_*.go`.
- Pair tables parsed: 766 rows (m5 7, m7 295, m8 322, os 36, m9 49, io 10, iface 47).
- 27 dotted-pkg fallback joins verified by hand (exact `__`-helper names).
- 0 rows with `funcName == <top-level>`; 0 rows inside a `FuncLit`; 0 duplicate (file, line).
- Quota was EDQUOT-closed for the whole lane session (see lane report): this matrix was generated read-only and staged in `/var/tmp/review-1111-14-r2p1-3/`; the resume runner re-runs the generator in the worktree once quota opens and must reproduce these numbers exactly on master `a0df481b` before writing anything.
