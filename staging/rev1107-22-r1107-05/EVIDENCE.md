# EVIDENCE — rev1107-22-r1107-05 (R-1107-05, P1)

Lane: 276→318 synth fail-fast stubs re-test (internal/frontend).
Base: HEAD `59f0a247` (main repo `~/workspace/graalgo`, read-only).
Status: **fix verified RED→GREEN; BLOCKED-quota (no worktree, no commit/push).**
Staged patch: `r1107-05.patch` (applies cleanly on pristine `59f0a247`,
verified identical to the tested tree — see §6).

## 1. Counts re-derived at HEAD 59f0a247 (review's Correction confirmed)

- `panic("…not in M[0-9]…")` sites, `internal/frontend/`: 316 by strict
  same-line grep (review states 318; the 2-site gap is multi-line/pattern
  edge cases — the "276" headline is not reproducible either way).
- Exact string `multi-result calls need WS D`: 169 (matches the review).
- `panic(` sites in `synth_*.go`: 1656 (matches SCAN-2143).
- `synth_*.go` files: 140 (matches the review's correction).

## 2. Mechanism re-verified from disk truth (do NOT delete the stubs)

The 745 WS-D-family panic sites (audit3) are NOT dead code in general:

- A table-covered stub's panic body is the **live fail-fast for
  non-2-target call positions**. The exporter desugars 2-target calls to
  the `__`-helper (`mrdesugar_m5.go` tables); every other position links
  to the panic body and fails loudly at runtime
  (`export.go:9556` `isMultiResultSyntheticFailFast` + `synth_machinery.go:2228`).
  Precedent: `bytes.Buffer.Read` is table-covered AND still carries its
  panic body (`synth_bytes.go`), while its test calls the stub "retired"
  (`TestSyntheticBytesBufferReadDesugar`).
- "Retirement" therefore means **table row + real helper**, never deleting
  the panic body. A mass relabel of the 745 "re-test when WS D lands"
  strings was rejected: no behavior gain, and the strings are baked into
  wire goldens (churn). The remaining 743 stubs' stale labels are a
  follow-up for the coordinator, not this lane.

## 3. Triage of the 110 table-uncovered stubs (audit4.py / audit4.json)

- 43 with ≥1 REAL like-named `__`-helper in file (table-row candidates —
  each still needs per-site verification; loose name matches included).
- 21 with helpers all PANIC-ONLY — live gaps, incl. the io/fs cluster
  (`__fsReadFile/__fsReadDir/__fsStat/__fsReadLink/__fsLstat`, interface
  dispatch) and `math/big` `__mrbyteReader_ReadByte`.
- 46 with no like-named helper — genuine unimplemented surface
  (e.g. `bytes.Buffer.Peek/ReadByte/ReadRune/ReadString/Seek`,
  `encoding/json.Unmarshal`, `sort.Find`, `asn1.Unmarshal` — deliberately
  excluded per the mrdesugar header).

## 4. The fix: P0-2 traps retired (the one re-test trigger that fired)

`__fileReadFrom` / `__fileWriteTo` (`synth_os.go`) panicked with the
named re-test triggers "mrM8 desugar for io.ReadAll / io.WriteString".
Both callees are now tabled with real helpers:

- `io.ReadAll` — m8 table `mrdesugar_m5.go:1258`, helper `__readAll`
  (real implementation, `synth_io.go:824`).
- `io.WriteString` — m8 table `mrdesugar_m5.go:1252`, helper
  `__writeString` (real implementation, `synth_io.go:308`).
- 2-target `io.ReadAll` / `io.WriteString` calls inside synth bodies
  already desugar (precedent: `synth_archive_tar.go:2311,2738`,
  `synth_enc_csv_gob.go:441`, `synth_archive_zip.go:1432+`).
- `os.File.ReadFrom` / `WriteTo` were already in `osMultiResultFuncs`
  (`mrdesugar_m5.go:1131-1132`); only the helper bodies were traps.
- `os.File.Write` (`__fileWrite`, real) and `os.File.Read`
  (`__fileRead`, real) are tabled, so the itab dispatch inside
  `__readAll(f)` resolves.

Change (`r1107-05.patch`, 3 files):

1. `internal/frontend/synth_os.go` — `__fileReadFrom` now reads with
   `io.ReadAll(r)` (2-target, desugars) and writes via the concrete
   `__fileWrite` helper (established idiom, cf. `__fileReadAll`);
   `__fileWriteTo` now reads with `io.ReadAll(f)` and writes with
   `io.WriteString(w, string(b))` (the documented D4 shape, restored).
   Partial data is written even when the read errors (gc semantics:
   `io.ReadAll` returns data read before the error); a write error
   takes precedence over the read error. The exported `ReadFrom` /
   `WriteTo` panic bodies are untouched (fail-fast for non-2-target
   positions).
2. `internal/frontend/synth_io.go` — D6 comment updated: the
   `__fileReadFrom` / `__fileWriteTo` named-trap claim is retired; the
   `copyBuffer` fast-path arms are STILL omitted because the other
   WriterTo/ReaderFrom implementations (bytes.Buffer, discard,
   multiReader, nopCloserWriterTo, strings.Reader) remain fail-fast —
   restoring the arms would route `io.Copy` into those fail-fasts.
   The arms' re-test trigger is now "those implementations become
   real", not interface dispatch (which has landed via
   `m8IfaceMultiResultMethods`).
3. `internal/frontend/synth_os_export_test.go` (new) —
   `TestSyntheticOsFileReadFromWriteToDesugar`, modelled on
   `TestSyntheticBytesBufferReadDesugar`.

## 5. RED→GREEN (scratch copy, GOCACHE/TMPDIR on /scratch-r1107-05)

Scratch: exact copy of `internal/frontend` at `59f0a247` (verified
`diff -rq` identical before patching); `go` = `~/tools/go/bin/go`
(go1.27.1); `GOCACHE`/`TMPDIR` on `/` (overlay, 4.5G free) because
home quota is EDQUOT-exhausted and /tmp + /var/tmp are 100% full.
Load gate CLEAR before the heavy steps.

- RED (pre-fix, test only, scratch): `go test -run
  TestSyntheticOsFileReadFromWriteToDesugar -count=1 -v .` →
  **FAIL** (1.46s): `wire still contains the P0-2 trap "multi-result
  io.ReadAll needs the mrM8 desugar"`.
- GREEN (post-fix, scratch): same command → **PASS** (2.97s).
- Baseline: `TestSyntheticBytesBufferReadDesugar` PASS (3.01s) at HEAD.
- Targeted regression: `go test -run
  'TestSyntheticBytes|TestBufioSplit|TestSyntheticOs' -count=1 .` →
  **ok** (13.28s). `go vet .` → clean (rc=0).
- `gofmt -l` on the three touched files → clean.
- Full frontend package suite (scratch, post-fix): `go test -count=1 .`
  → 95.2s, exactly ONE failure: `TestSyncPoolRtatomicLowersToSyncop`
  (`atomic_rtatomic_r2_test.go:64`, `stat
  ../../test/differential/corpus/46_sync_pool/main.go: no such file or
  directory`). PROVEN environmental, not a regression: the same test
  fails identically on a PRISTINE HEAD copy in the same scratch layout
  (the scratch holds only `internal/frontend`, so the repo-relative
  corpus path does not exist there). All other tests pass.

## 6. Patch staging + apply verification

- `r1107-05.patch` (201 lines, 7827 bytes) generated by `diff -u`
  between `git show 59f0a247:` originals and the patched scratch files.
- Verified: `git apply --check` clean on a pristine HEAD copy of
  `internal/frontend`; after applying, `diff -rq` against the tested
  scratch tree → identical.
- Apply at repo root on a worktree at `59f0a247`: `git apply
  /tmp/rev1107-22-r1107-05/r1107-05.patch` (paths are
  `a|b/internal/frontend/...`).

## 7. What was NOT done / residual verification for the integrator

- Runtime differential for `os.File.ReadFrom` / `WriteTo` (gate1 /
  snapshot-check + a corpus program exercising ReadFrom/WriteTo
  end-to-end under graalgo vs gc): NOT run here — needs a worktree +
  backend build, quota-blocked. The frontend wire test proves the
  desugar fires and the helpers are real; the runtime path
  (`__readAll(f)` → itab → `__fileRead`) is the same machinery the
  `bytes.Buffer` precedent exercises, but a runtime differential is
  still owed before merge.
- `make check` / `make all` gates: NOT run (worktree-blocked).
  Full-suite note: the single `TestSyncPoolRtatomicLowersToSyncop`
  failure is a scratch-layout artifact (missing repo-relative corpus
  path), reproduced on pristine HEAD — in a real worktree it should
  pass; re-run the full suite there.
- The other 108 uncovered stubs (audit4): triaged, not fixed — each
  needs its own per-site verification (table row, pair type, method
  wiring, runtime test). The io/fs panic-only-helper cluster is the
  largest coherent follow-up.
- The 743 remaining "re-test when WS D lands" labels: intentionally
  left; mass relabel is a coordinator decision (golden churn).

## 8. Quota / environment notes

- Home quota probe (real 8 KB write to `~/workspace/`): FAILED
  EDQUOT at run start — no worktree claimed, nothing committed or
  pushed, main repo untouched (read-only git + reads).
- /tmp hit 100% full during the run (88K free at worst), later 20M
  free; /var/tmp 100% full throughout. All heavy state (scratch repo
  copy, GOCACHE, TMPDIR) lives on `/scratch-r1107-05` (overlay /,
  4.5G free) and must be removed by the resuming agent after
  integration (it is NOT part of the deliverable).
- `gen_patch.py` (0 bytes, staged by the original lane) was never
  written; the patch was generated by `diff -u` instead (see §6).
