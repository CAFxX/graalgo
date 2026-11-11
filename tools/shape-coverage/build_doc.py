#!/usr/bin/env python3
"""Build SHAPE_COVERAGE_MATRIX.md from matrix_final.tsv."""
import csv
from collections import Counter, defaultdict

import os
W = os.environ.get("R2P13_STAGE", "/var/tmp/review-1111-14-r2p1-3")
rows = list(csv.DictReader(open(f"{W}/matrix_final.tsv"), delimiter="\t"))
rows.sort(key=lambda r: (r["file"], int(r["line"])))

def md_table(headers, data):
    out = ["| " + " | ".join(headers) + " |",
           "| " + " | ".join("---" for _ in headers) + " |"]
    for row in data:
        out.append("| " + " | ".join(str(c) for c in row) + " |")
    return "\n".join(out)

# per-file summary
byfile = defaultdict(Counter)
for r in rows:
    byfile[r["file"]][r["direct"]] += 1
    byfile[r["file"]]["TOTAL"] += 1
file_rows = []
for f in sorted(byfile):
    c = byfile[f]
    file_rows.append([f, c["TOTAL"], c.get("COVERED", 0), c.get("PANICS", 0),
                      c.get("PANICS-via-helper", 0), c.get("N/A-interface-only", 0)])

doc = []
doc.append("# Shape-coverage matrix: 315 scope-panic traps (R2-P1-3)")
doc.append("")
doc.append("> **Master:** `a0df481b` (R-1108-112). **Generated:** 2026-11-11 by "
           "`gen_matrix.go` + `build_final.py` (staged in `/var/tmp/review-1111-14-r2p1-3/`; "
           "this file is the P1 deliverable — see §9 for the intended repo homes). "
           "**Status:** STATIC INFERENCE — every cell is derived from the pair tables and the "
           "exporter mechanism, not from running the probe corpus. The probe corpus (§7) flips "
           "cells to PROBED. Do not treat a STATIC cell as an observed fact.")
doc.append("")
doc.append("## 1. Count correction: 316 → 315")
doc.append("")
doc.append("The review quoted 316 `panic(\"… not in M[5789] scope …\")` sites. A grep for "
           "`panic(\".*not in M[5789] scope` over `internal/frontend/synth_*.go` returns 316 lines, "
           "but one is a **comment false positive**, not a call:")
doc.append("")
doc.append("```")
doc.append("synth_math.go:29: // Out-of-scope members: exact gc signature + panic(\"not in M7 scope\").")
doc.append("```")
doc.append("")
doc.append("The AST-based generator (`go/parser` over the extracted embedded sources — see §8) finds "
           "**315 real `panic` calls**, and the (file, line) set matches the grep set exactly apart from "
           "that one comment line (verified by `comm` on both sets; zero lines in either direction). "
           "Per-file grep counts in the review brief also drift in 4 files (bytes 12→15, strings 7→11, "
           "mime_multipart 15→16, enc_csv_gob 9→5); the table in §3 is disk truth. "
           "`fail-fast stub` comments: 75 across `internal/frontend/*.go` (68 in `synth_*.go` alone) — "
           "the review's 75 reproduces exactly on the wider scope.")
doc.append("")
doc.append("## 2. Verdict summary (315 rows)")
doc.append("")
doc.append(md_table(["shape", "COVERED", "PANICS", "PANICS-via-helper", "N/A-interface-only", "PROBE-OWED"],
    [["direct (concrete call)", 143, 159, 10, 3, 0],
     ["method-value (`f := x.M; f()`) ", 0, 315, 0, 0, 0],
     ["interface-dispatch", 59, 0, 0, 0, 253],
     ["reflect (`Value.Call` / `MethodByName`)", 0, 0, 0, 0, 315]]))
doc.append("")
doc.append("- **direct:** 143 desugar to a non-panicking `__`-helper; 159 have no table row and hit the "
           "panic body; 10 desugar to a helper that itself panics (§5); 3 are interface-only "
           "(`reflect.Type.FieldByName/FieldByNameFunc/MethodByName` — no concrete call form exists).")
doc.append("- **method-value:** 0 covered. `methodValueNode` (`internal/frontend/export.go:9970`) binds the "
           "method's own funcdecl with no pair-desugar, so invoking the value runs the panic body. "
           "This holds for all 315 rows, including the 143 direct-covered ones.")
doc.append("- **interface-dispatch:** 59 covered via `m8IfaceMultiResultMethods` (name + result-arity "
           "structural match); 253 have no matching iface row (probe-owed — the fallback may still "
           "fail loud at compile time instead of panicking at runtime); 3 cross-package name collisions "
           "are PROBE-OWED-suspect (§6).")
doc.append("- **reflect:** 315 PROBE-OWED. The `runtime.reflectcall` linkname cited by "
           "`FuncIds.funcFor` does not exist (review P2-2), so the observed behaviour (compile-time "
           "error vs runtime panic) is unknown until the probe runs.")
doc.append("- **Kinds:** 175 methods, 72 package-level funcs, 50 unexported helpers, 18 `__`-helpers. "
           "Distinct (pkg, method, recv): 311 — 4 methods carry two panic sites (public body + `__`-helper).")
doc.append("")
doc.append("## 3. Direct coverage by file")
doc.append("")
doc.append(md_table(["file", "sites", "COVERED", "PANICS", "via-helper", "N/A"],
                    [[f"`{r[0]}`"] + r[1:] for r in file_rows]))
doc.append("")
doc.append("Reading: `archive/tar` (31/31), `mime` (12/12), `archive/zip` (28/29) and "
           "`mime/multipart` (14/16) are essentially direct-covered; `bytes` is the worst ratio "
           "(1/15 — only `Buffer.WriteString` has a row; `Peek/ReadByte/ReadRune/ReadBytes/ReadString/"
           "WriteRune/ReadAt/Seek/WriteTo/ReadFrom` have none); `encoding/json` (0/11), "
           "`encoding/xml` (0/9), `encoding/gob` (0/5), `io/fs` (0/13), `net` (0/4) have no direct "
           "coverage at all. By scope: M5 5/29 covered, M7 108/165, M8 29/110, M9 1/11.")
doc.append("")
doc.append("## 4. How each column is determined (mechanism, with citations)")
doc.append("")
doc.append("All pair tables live in `internal/frontend/mrdesugar_m5.go` (4556 lines): "
           "`m5StaticMREntries` (:184, 7 rows), `m7MultiResultFuncs` (:283, 295 rows), "
           "`m8MultiResultFuncs` (:686, 322 rows), `osMultiResultFuncs` (:1101, 36 rows), "
           "`m9MultiResultFuncs` (:1173, 49 rows), `ioMultiResultFuncs` (:1251, 10 rows), "
           "`m8IfaceMultiResultMethods` (:1292, 47 rows). Row shape: "
           "`{pkg, name, recv, helper, pair, fields, …}`.")
doc.append("")
doc.append("- **direct = COVERED** iff a concrete-table row `(pkg, name, recv)` exists for the method "
           "and the named `__`-helper does not itself contain a scope-panic (§5). Join key uses the "
           "panic message's qualified name (`archive/tar.Next` → pkg `archive/tar`, name `Next`) and "
           "the enclosing func's receiver type; a `pkg.Type` message prefix falls back to `pkg` "
           "(`os.File.SyscallConn` → `os`). The 27 fallback joins were each verified by hand: every "
           "one names the exact `__`-helper for the enclosing method "
           "(e.g. `bytes.Buffer.WriteString` → `__bytesBufferWriteString`).")
doc.append("- **direct = PANICS** iff no concrete row exists: the call site is emitted as a plain call "
           "to the panicking funcdecl (the R2-P0-1a pattern). Single-result and void methods "
           "(81 + 6 rows) can never have a pair row — they panic on direct call by construction.")
doc.append("- **method-value = PANICS** for all rows. `methodValueNode` (`export.go:9970-10067`) resolves "
           "the method and emits a `\"methodvalue\"` wire node with `obj` = the method's funcID — no "
           "`mrPlanForFuncValue` lookup, no `__`-helper substitution. Contrast the named-function "
           "value path (`synth_machinery.go:2082`, `export.go:8427`), which DOES lower through "
           "`mrPlanForFuncValue` to the pair helper — that path covers `pkg.Fn` values, not bound "
           "method values. `mrInfoForFuncValueInit` (`export.go:7530-7585`) likewise handles only "
           "`*ast.Ident` and package-qualified `*ast.SelectorExpr`; a receiver-qualified selector "
           "(`f := tr.Read`) falls through with `fn == nil`. The snapshot exclusion is the live pin: "
           "`test/snapshots/go1.27.1.json:43` and `test/differential/backends-p3.json:107` exclude "
           "program `40_method_values` (\"backend-specific: method values not in scope\").")
doc.append("- **interface-dispatch = COVERED** iff an `m8IfaceMultiResultMethods` row matches on "
           "(method name, result count) — the classifier is structural on (name, result count, "
           "positional result types) per `classifyIfaceMultiResultCall` (`mrdesugar_m5.go:2610`). "
           "Same-package rows and the canonical `io`/`net` families (`Read/Write/ReadByte/WriteTo/"
           "ReadFrom/WriteString`) count as COVERED; cross-package name collisions (§6) stay "
           "PROBE-OWED. Note the classifier cannot be confirmed from the table alone — the pair "
           "field TYPES must align, which only the probe (or a type-checked test) shows.")
doc.append("- **reflect = PROBE-OWED** for all rows. No `reflectcall` desugar exists for these methods; "
           "the probe (§7, program R-3) establishes whether the shape fails at compile time "
           "(scope error) or at runtime (panic), and with what message.")
doc.append("- **COVERED means \"desugars to a non-panicking helper\", not \"behavior verified\".** "
           "Helper correctness is the differential corpus's job (wave1/2/3 runners); this matrix "
           "tracks panic traps only.")
doc.append("")
doc.append("## 5. Helper traps: 10 rows desugar into a panic (direct = PANICS-via-helper)")
doc.append("")
doc.append("A table row exists, so a direct call desugars — but the named `__`-helper's own body "
           "contains a scope-panic. Retiring these rows means implementing the helper, not adding a row.")
doc.append("")
trap_rows = [r for r in rows if r["direct"] == "PANICS-via-helper"]
doc.append(md_table(["file:line", "method", "helper (panics at)"],
    [[f"`{r['file']}:{r['line']}`", f"`{r['pkg']}.{r['method']}`", f"`{r['helperTrap']}`"] for r in trap_rows]))
doc.append("")
doc.append("The `os` four (`OpenRoot`, `Executable`, `StartProcess`, plus `File.SyscallConn` and "
           "`Process.Wait`) are the P1-1 \"genuinely-missing\" subset (needs-design): no helper "
           "implementation exists at all. `encoding/asn1.Marshal` desugars to `__asn1Marshal`, which "
           "panics for composite types (needs reflect). The other `__`-helper panic sites "
           "(`io/fs`: 12 sites — R2-P0-1b; `os.Root.*` helpers are covered, not traps) are already "
           "PANICS rows, not traps.")
doc.append("")
doc.append("## 6. Cross-package iface collisions: 3 PROBE-OWED-suspect rows")
doc.append("")
cross = [r for r in rows if r["ifaceDispatch"] == "PROBE-OWED-cross-pkg"]
doc.append(md_table(["file:line", "method", "matched iface row"],
    [[f"`{r['file']}:{r['line']}`", f"`{r['pkg']}.{r['method']}`", f"`{r['ifaceVia']}`"] for r in cross]))
doc.append("")
doc.append("The iface classifier matches structurally by name, so `encoding/asn1.Unmarshal` matches the "
           "`crypto/elliptic` Unmarshal row and `url.URL.AppendBinary/MarshalBinary` match the `hash` "
           "rows. Whether the pair types align (and the desugar is therefore correct, a wrong-output "
           "bug, or a compile-time error) is probe-owed. The 6 `(unreachable: desugared)` rows "
           "(`reflect.Type.FieldByName/FieldByNameFunc/MethodByName`, `reflect.Value.FieldByIndexErr/"
           "TryRecv`, `reflect.Select`) are author claims that desugar makes the body unreachable — "
           "the probe must confirm each; a single observed panic falsifies the claim for that row.")
doc.append("")
doc.append("## 7. Probe corpus (flips STATIC cells to PROBED)")
doc.append("")
doc.append("Run under `nice -n 10` with `load-shed.sh backoff` gating; each program is a differential "
           "program (gc leg + graalgo leg). Programs live in the resume lane's worktree until placed "
           "under `test/corpus/wave2/shape-coverage/` (see §8). Expected outcomes below are STATIC "
           "predictions — a mismatch is a finding, not a probe failure.")
doc.append("")
doc.append(md_table(["id", "shape × method", "program sketch", "STATIC prediction"],
    [["P-MV-1", "method-value × `archive/tar.Reader.Read` (direct-covered)",
      "`tr := tar.NewReader(...); f := tr.Read; n, err := f(p)`",
      "runtime panic `archive/tar.Read: not in M7 scope …`"],
     ["P-MV-2", "method-value × `bytes.Buffer.WriteString` (direct-covered)",
      "`b := &bytes.Buffer{}; f := b.WriteString; f(\"x\")`",
      "runtime panic `bytes.Buffer.WriteString: not in M7 scope …`"],
     ["P-MV-3", "method-value × `os.File.SyscallConn` (helper-trap)",
      "`f := file.SyscallConn; f()`",
      "runtime panic (helper trap — same message as direct)"],
     ["P-IF-1", "interface-dispatch × `archive/tar.Reader.Next` (no iface row)",
      "`var x interface{ Next() (*tar.Header, error) } = tr; x.Next()`",
      "runtime panic `archive/tar.Next: not in M7 scope …` (or compile-time scope error — record which)"],
     ["P-IF-2", "interface-dispatch × `bytes.Buffer.Read` via `io.Reader` (iface row exists)",
      "`var r io.Reader = &bytes.Buffer{}; r.Read(p)` — control: must NOT panic",
      "no panic; reads via `__bytesBufferRead` pair helper"],
     ["P-IF-3", "interface-dispatch × `encoding/asn1.Unmarshal` (cross-pkg suspect)",
      "`var u interface{ Unmarshal([]byte, any) error } = ...` — see §6",
      "PROBE-OWED: record panic / wrong-output / correct"],
     ["P-RF-1", "reflect × `archive/tar.Reader.Read`",
      "`m := reflect.ValueOf(tr).MethodByName(\"Read\"); m.Call(...)`",
      "PROBE-OWED: compile-time scope error vs runtime panic — record which + message"],
     ["P-RF-2", "reflect × `reflect.Type.FieldByName` (unreachable claim)",
      "`t := reflect.TypeOf(0); t.FieldByName(\"X\")` — direct call through interface type",
      "STATIC claim: no panic (desugared). One observed panic falsifies the claim"],
     ["P-DR-1", "direct × `bytes.Buffer.Peek` (no row; control)",
      "`b.Peek(1)`", "runtime panic `bytes.Buffer.Peek: not in M7 scope …`"],
     ["P-DR-2", "direct × `encoding/asn1.Marshal` (helper trap; control)",
      "`asn1.Marshal(v)`", "runtime panic via `__asn1Marshal`"],
     ["P-DR-3", "direct × `os.Root.Open` (covered; control)",
      "`root.Open(\"f\")`", "no panic (desugars to `__open`)"]]))
doc.append("")
doc.append("## 8. Negative E2E tests (one per uncovered shape class)")
doc.append("")
doc.append("Vehicle: the wave2 runner's `.divergent` + `.panic` markers "
           "(`test/corpus/wave2/run_wave.sh:146-165` — exit comparison skipped by design for divergent "
           "programs; stdout must still match; graalgo's first filtered stderr line must equal the "
           "`.panic` file). A `.divergent` without `.panic` fails the harness, so every negative test "
           "pins its message. Home: new dir `test/corpus/wave2/shape-coverage/` with a thin "
           "`run_differential.sh` (`exec ../run_wave.sh shape-coverage`, following "
           "`test/corpus/wave2/reflect/run_differential.sh`). Wave1/wave3 runners do NOT support "
           "`.divergent` — do not place these tests there.")
doc.append("")
doc.append("### T-MV — method-value shape (`01_method_value_tar_read.go`)")
doc.append("")
doc.append("```go")
doc.append("package main")
doc.append("")
doc.append("import (")
doc.append("\t\"archive/tar\"")
doc.append("\t\"bytes\"")
doc.append(")")
doc.append("")
doc.append("func main() {")
doc.append("\t// Direct call desugars (table row exists); the method value must fail loud.")
doc.append("\ttr := tar.NewReader(bytes.NewReader(nil))")
doc.append("\tf := tr.Read")
doc.append("\t_, _ = f(make([]byte, 8))")
doc.append("}")
doc.append("```")
doc.append("")
doc.append("`.divergent` (empty marker) + `.panic`:")
doc.append("")
doc.append("```")
doc.append("panic: archive/tar.Read: not in M7 scope (multi-result calls need WS D; re-test when it lands)")
doc.append("```")
doc.append("")
doc.append("### T-IF — interface-dispatch shape (`02_iface_dispatch_tar_next.go`)")
doc.append("")
doc.append("```go")
doc.append("package main")
doc.append("")
doc.append("import (")
doc.append("\t\"archive/tar\"")
doc.append("\t\"bytes\"")
doc.append(")")
doc.append("")
doc.append("func main() {")
doc.append("\t// No iface-table row for Next: interface dispatch must fail loud.")
doc.append("\ttr := tar.NewReader(bytes.NewReader(nil))")
doc.append("\tvar x interface{ Next() (*tar.Header, error) } = tr")
doc.append("\t_, _ = x.Next()")
doc.append("}")
doc.append("```")
doc.append("")
doc.append("`.divergent` + `.panic` (exact first stderr line TBD by probe P-IF-1 — if the probe shows "
           "a compile-time scope error instead of a runtime panic, this test becomes a compile-error "
           "test and the matrix cell flips to COMPILE-ERROR):")
doc.append("")
doc.append("```")
doc.append("panic: archive/tar.Next: not in M7 scope (multi-result calls need WS D; re-test when it lands)")
doc.append("```")
doc.append("")
doc.append("### T-RF — reflect shape (`03_reflect_tar_read.go`)")
doc.append("")
doc.append("```go")
doc.append("package main")
doc.append("")
doc.append("import (")
doc.append("\t\"archive/tar\"")
doc.append("\t\"bytes\"")
doc.append("\t\"fmt\"")
doc.append("\t\"reflect\"")
doc.append(")")
doc.append("")
doc.append("func main() {")
doc.append("\t// Reflect call into a panicking method must fail loud, not silently misbehave.")
doc.append("\ttr := tar.NewReader(bytes.NewReader(nil))")
doc.append("\tm := reflect.ValueOf(tr).MethodByName(\"Read\")")
doc.append("\tm.Call([]reflect.Value{reflect.ValueOf(make([]byte, 8))})")
doc.append("}")
doc.append("```")
doc.append("")
doc.append("`.divergent` + `.panic`: exact expected output TBD by probe P-RF-1 (compile-time scope "
           "error vs runtime panic — whichever it is, the test pins it; a silent success or a "
           "different message fails the test).")
doc.append("")
doc.append("## 9. Repo homes (proposed; resume lane confirms)")
doc.append("")
doc.append("- This document → [SHAPE_COVERAGE_MATRIX.md](SHAPE_COVERAGE_MATRIX.md) (living doc; updated on every row retirement).")
doc.append("- Machine-readable matrix → [shape_coverage_matrix.tsv](../../internal/frontend/shape_coverage_matrix.tsv) (same columns as "
           "`matrix_final.tsv`; regenerated by the generator, never hand-edited).")
doc.append("- Generator → [gen_matrix.go](../../tools/shape-coverage/gen_matrix.go) + [build_final.py](../../tools/shape-coverage/build_final.py) + [build_doc.py](../../tools/shape-coverage/build_doc.py) (read-only; "
           "regenerate after any synth/table change and diff the TSV — a diff is a finding).")
doc.append("- Negative tests → [shape-coverage/](../../test/corpus/wave2/shape-coverage/) (§8).")
doc.append("- Probe programs → same dir as `P-*.go` scratch (not committed) or under `test/corpus/wave2/shape-coverage/` "
           "as probe programs once their outcomes are pinned.")
doc.append("")
doc.append("## 10. Retirement protocol (for sibling lanes)")
doc.append("")
doc.append("Sibling lanes (`review-1111-14-r2p0-1a`, `review-1111-14-r2p0-1b`, "
           "`review-1111-14-p1-1-stubs`) own per-method implementations; this lane owns the matrix + "
           "negative tests. A row retires when: (1) the panic is deleted (implementation or real "
           "desugar in the generator — never a hand-edit of generated files); (2) a differential "
           "program proving the method works in the retired shape is added to the corpus; (3) the "
           "matrix row flips to COVERED-probed and this doc + the TSV are regenerated in the same "
           "commit. The negative tests in §8 flip to positive differential tests (or are deleted) "
           "when their shape class gains a general mechanism — never silently.")
doc.append("")
doc.append("## 11. Open questions / needs-design")
doc.append("")
doc.append("1. **General method-value mechanism:** eta-expand a bound method value to the pair helper "
           "(the named-function precedent in `mrPlanForFuncValue`) instead of binding the panicking "
           "funcdecl. Design owed: receiver capture + pair-returning closure + call-site desugar "
           "through `userMRVars`. Until it lands, all 315 method-value cells stay PANICS.")
doc.append("2. **Reflect calls:** no `reflectcall` desugar exists; decide compile-time scope error vs "
           "runtime panic per method (probe P-RF-1/P-RF-2 first).")
doc.append("3. **Interface fallback for unlisted methods:** `classifyIfaceFallbackMultiResultCall` "
           "exists — determine its exact behaviour for methods with no iface row (probe P-IF-1).")
doc.append("4. **`os.Root.*` COVERED rows** desugar to the shared `__open/__create/...` helpers — "
           "verify the root restriction survives the desugar (differential test owed; not a matrix "
           "question, flagged for the R2-P0-1b / os lane).")
doc.append("5. **The 6 `(unreachable: desugared)` claims** (§6) are author assertions, not observations — "
           "each needs a probe run before its direct cell can be trusted.")
doc.append("")
doc.append("## 12. Full matrix (315 rows)")
doc.append("")
doc.append("Columns: `direct` ∈ {COVERED, PANICS, PANICS-via-helper, N/A-interface-only}; "
           "`methodValue` = PANICS (all rows — §4); `ifaceDispatch` ∈ {COVERED, PROBE-OWED, "
           "PROBE-OWED-cross-pkg}; `reflect` = PROBE-OWED (all rows — §4). `kind`: method / pkg-func / "
           "unexported-helper / __-helper. `table`: the pair-table row matched, or empty. "
           "`unreach?`: message claims `(unreachable: desugared)`. All verdicts STATIC unless the "
           "probe corpus has flipped them (none yet).")
doc.append("")
rows_md = md_table(["file:line", "scope", "method", "kind", "nRes", "table", "direct", "iface", "reflect", "unreach?"],
    [[f"`{r['file']}:{r['line']}`", r["scope"], f"`{r['pkg']}.{r['method']}`",
      r["kind"], r["nResults"], r["table"] or "—", r["direct"], r["ifaceDispatch"],
      r["reflect"], r["unreachableClaim"]] for r in rows])
doc.append(rows_md)
doc.append("")
doc.append("## Appendix: validation log")
doc.append("")
doc.append("- `grep -c 'panic(\".*not in M[5789] scope' internal/frontend/synth_*.go` = 316 lines; "
           "AST generator finds 315 real calls; set difference is exactly `synth_math.go:29` (comment).")
doc.append("- Per-scope (panic calls): M5 29, M7 165, M8 110, M9 11.")
doc.append("- `fail-fast stub` comments: 75 in `internal/frontend/*.go`, 68 in `synth_*.go`.")
doc.append("- Pair tables parsed: 766 rows (m5 7, m7 295, m8 322, os 36, m9 49, io 10, iface 47).")
doc.append("- 27 dotted-pkg fallback joins verified by hand (exact `__`-helper names).")
doc.append("- 0 rows with `funcName == <top-level>`; 0 rows inside a `FuncLit`; 0 duplicate (file, line).")
doc.append("- Quota was EDQUOT-closed for the whole lane session (see lane report): this matrix was "
           "generated read-only and staged in `/var/tmp/review-1111-14-r2p1-3/`; the resume runner "
           "re-runs the generator in the worktree once quota opens and must reproduce these numbers "
           "exactly on master `a0df481b` before writing anything.")
doc.append("")

open(f"{W}/SHAPE_COVERAGE_MATRIX.md", "w").write("\n".join(doc))
print("doc lines:", len(doc))
print("doc bytes:", sum(len(x)+1 for x in doc))
