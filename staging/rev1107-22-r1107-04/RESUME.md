# RESUME — lane rev1107-22-r1107-04 (R-1107-04, P1)

## Status: FIX VERIFIED RED→GREEN, staged — BLOCKED-quota (NOT applied to any worktree/branch)

- **Issue:** exporter emits 104 invalid wire positions in the `__`-helper funcs of
  the `pkgvar_mrarg` golden; `build.sh check` excluded the golden instead of
  fixing it.
- **Review:** REVIEW-2026-11-07-22 (HEAD `59f0a247`), carried from
  REVIEW-2026-11-07-07 R-1107-04.
- **HEAD verified against:** `59f0a247` (main repo, read-only; master has NOT
  moved since the original lane).
- **Resumed run (2026-11-07 ~22:14–22:30 CET):** the original lane was cancelled
  mid-flight with the patch, the NEW golden, and VERIFY.md NEVER WRITTEN (only
  claimed in the old version of this file). This run found the fix itself intact
  in `fe/`, re-verified the mechanism + RED from disk truth, rebuilt the patched
  binary, verified GREEN, generated `R-1107-04.patch` +
  `pkgvar_mrarg.json.gz.NEW` + `EVIDENCE.md` + `VERIFY.md`, and ran the full
  gate. Full evidence: `EVIDENCE.md`.
- **Quota state:** home STILL EDQUOT-exhausted (real 8 KB home write failed
  `Errno 122 Disk quota exceeded`, re-probed 22:14 CET; `df` shows 36G free —
  it is a quota, not a full disk). `/var/tmp` 100% full. `/tmp` hit 100% during
  this run (512M tmpfs, sibling lanes hold ~450M; this lane holds ~34M).
  Per the lane brief: NO worktree claimed, NO registry entry, NO commit.
  All work is staged in `/tmp/rev1107-22-r1107-04/`.

## Mechanism (re-verified from disk truth, NOT taken on the review's word)

1. Checked-in golden `internal/frontend/testdata/pkgvar_mrarg.json.gz`
   decompressed → validator `tools/validate_frontend_v1.py` reports exactly
   **104 schema errors, exit 1** (log: `validator-red.log`). Breakdown of the
   104: **91 nodes with `pos.line == 0` + 13 nodes with `pos.len < 0`**
   (disjoint sets — the review's "104 nodes carry pos.line == 0" is imprecise;
   104 is the total of both classes). All 104 sit in 11 funcs, ALL named
   `__*` (the disk-MR helpers): `__BoundsDecode` (31), `__Name_ReadVarint` (6),
   `__ArrayType_descriptorSizes` (5), `__ChanType_descriptorSizes` (5),
   `__FuncType_descriptorSizes` (12), `__InterfaceType_descriptorSizes` (12),
   `__MapType_descriptorSizes` (5), `__PtrType_descriptorSizes` (5),
   `__SliceType_descriptorSizes` (5), `__StructType_descriptorSizes` (12),
   `__name_readVarint` (6).
2. Root cause is in `transformMRFunc` (`internal/frontend/mr_disk.go:946-970`)
   + `exporter.pos` (`internal/frontend/export.go:3315`):
   - `transformMRFunc` builds each `__`-helper `*ast.FuncDecl` with fresh
     synthetic nodes at `token.NoPos` (`ast.NewIdent(helper)`, fresh
     `&ast.FuncType{...}`, fresh results `FieldList` whose type is
     `genericRef(pairType)` = `NewIdent`, fresh `namedDecls` idents). It
     REUSES the original `fd.Type.Params` for plain funcs.
   - `exporter.pos` computed `len = End.Offset - Pos.Offset` with no validity
     check. `go/ast` fall-throughs make the endpoints chimeric:
     `FuncDecl.Pos()` → `FuncType.Pos()` → shared original params `(` (real
     position, e.g. `internal/abi/bounds.go:86`, off 2475) while
     `FuncType.End()` → fresh results field → synthetic pair-type ident end
     (a bare fset offset, 17) → **len −2458**. Fully synthetic nodes give
     `Pos()==NoPos` (line 0) with `End()` = rewritten-body end → **len 17940
     at line 0**. Verified against vendored `go/ast` (`FuncDecl.Pos`,
     `FuncType.Pos/End`, `FieldList.Pos/End`, `Field.Pos/End`, `Ident.End`).
   - `exportFuncDecl` (`export.go:5970`) emits every param/result via
     `exportParamVar(fd.Type, v)` → `e.node("param", fd.Type, …)` → `e.pos`,
     so the chimeric `fd.Type` poisons all helper params/results.
3. `exporter.pos` is the SINGLE funnel: it is the only `&Pos{` construction
   in non-test frontend code, and every node goes through `e.node()` →
   `e.pos()`. Fixing it fixes all current and future synthetic-node positions.
4. The golden is CURRENT, not stale: a pristine build of HEAD's exporter
   (built in /tmp, see below) re-exports `pkgvar_mrarg.go` byte-identical to
   the checked-in golden (`cmp` clean), and the validator reports the same
   104 errors on the fresh export (`validator-fresh-red.log`). RED reproduced
   at HEAD.
5. Out of scope, noted for a follow-up (NOT fixed here): disk-package
   positions are attributed to `file: 0` (`pkgvar_mrarg.go`, 30 lines) while
   their line/off values come from the disk files (e.g. line 648–649). The
   validator does not check file attribution, and fixing it would mean adding
   disk files to `package.files` (a wire-format change touching the backend).
   The 104 errors fixed here are strictly the `line == 0` / `len < 0` class.

## Fix (staged, not yet applied to the repo)

`R-1107-04.patch` — unified diff, `git apply` at repo root. Three files:

1. **`internal/frontend/export.go` — `exporter.pos` (the fix).** Two guards
   inserted after the file-index/src lookup, before the M8 rune block:
   - `!s.IsValid()` → return the defined synthetic position
     `{File: 0, Line: 1, Col: 1, Off: 0, Len: 0}`.
   - `!t.IsValid() || t.Offset < s.Offset || fset.File(Pos) != fset.File(End)`
     → return a zero-length section at the valid start
     `{File: fileIdx, Line: s.Line, Col: <rune col>, Off: s.Offset, Len: 0}`
     (col recomputed in runes per the M8 rule, so a non-ASCII line cannot push
     the section start past the line's char length).
   Valid nodes are byte-identical: the guards only fire when the old code
   would have emitted an invalid position. `gofmt` clean.
2. **`internal/frontend/build.sh` — re-include the golden.** The `testdata/*.json`
   loop is unchanged; a second loop validates `testdata/*.json.gz` via a
   decompressed temp copy. The old exclusion comment (which documented the
   104 errors as a follow-up) is deleted. `sh -n` clean.
3. **`docs/arch/frontend-v1.md` (§3) — document the synthetic-position rule**
   (required by `TestGolden`'s comment: intentional position-semantics changes
   update this doc in the same change).

Also regenerated: **`testdata/pkgvar_mrarg.json.gz`** (new golden,
`pkgvar_mrarg.json.gz.NEW` in this staging dir — rename on apply). Only
positions change vs the old golden (verify with the diff step below).

## Verification (in /tmp copy of internal/frontend @ HEAD 59f0a247)

- Build env: `GO=~/tools/go/bin/go` (go1.27.1), `GOCACHE`/`GOPATH`/`TMPDIR`
  under `/run/r1107-04-gobuild` (1.6G tmpfs — /tmp was too small for the
  Go `$WORK` dir), `HOME` = staging `fakehome`. All commands `nice -n 10`.
  Backoff checked before each heavy step (`load-shed.sh backoff`).
- RED (pristine): `./gofrontend testdata/pkgvar_mrarg.go` byte-identical to
  checked-in golden; validator → 104 errors, exit 1 (`build-pristine.log`,
  `validator-fresh-red.log`). Re-run this session: `validator-red-rerun.log`
  (104 errors, exit 1; 91 line-0 + 13 negative-len).
- GREEN (patched): rebuilt binary in `fe/` (`build-patched.log`, exit 0);
  fresh export validates **0 errors, exit 0** (`validator-green.log`);
  positions-only vs old golden (non-`pos` trees identical, 263 differing leaf
  paths all inside `pos`); new golden `pkgvar_mrarg.json.gz.NEW`.
- Full gate `./build.sh check` (300.6s, `build-check-green.log`): gofmt ✓,
  vet ✓, go test — exactly ONE failure, `TestSyncPoolRtatomicLowersToSyncop`,
  cause `stat ../../test/differential/corpus/46_sync_pool/main.go: no such
  file or directory` — the `/tmp` layout has no `test/` dir (the file EXISTS in
  the real repo). Controls with the file stubbed in: PATCHED **PASS** and
  PRISTINE **PASS** (`patched-single-test.log`, `pristine-single-test.log`).
  Environmental, not a regression. Check stages after `go test` (aborted by
  `set -e`) were run manually — ALL PASS (`check-stages.log`): vendoring
  identical, validator selftest 23/23, all 5 `.json` goldens valid, NEW
  `.json.gz` golden valid, all 5 fresh exports valid.
- `/tmp` hit 100% mid-run (sibling lanes); a pristine-build attempt failed
  `no space left on device` (`build-pristine-control.log`). 16M freed by
  deleting this lane's own `/tmp/r1107-04-head` extraction after the control
  ran. No sibling files touched.

## How to apply on quota recovery (resume agent)

1. Re-probe home quota with a real 8 KB write; only proceed if it succeeds.
2. Register the lane if not already done (suggested id
   `graalgo-rev1107-22-r1107-04`), claim a worktree:
   `~/workspace/system/bin/wt.py new graalgo rev1107-22-r1107-04 --chat graalgo-independent-codebase-review`
   (base HEAD `59f0a247`; if master has moved, rebase the fix onto the new HEAD
   and re-run verification — a rebase voids any review).
3. In the worktree: `git apply /tmp/rev1107-22-r1107-04/R-1107-04.patch`
   (verify with `git status` — exactly 3 modified files), then copy the new
   golden into place:
   `cp /tmp/rev1107-22-r1107-04/pkgvar_mrarg.json.gz.NEW internal/frontend/testdata/pkgvar_mrarg.json.gz`
4. Verify RED→GREEN from the worktree (backoff CLEAR first, all under
   `nice -n 10`):
   - `cd internal/frontend && ./build.sh check` (runs gofmt, vet, go test,
     vendoring diff gate, validator selftest + all goldens incl. pkgvar_mrarg,
     fresh exports). Must exit 0. NOTE: in the /tmp staging run this aborted
     at one environmental `go test` failure (missing `../../test/` corpus path
     — the file exists in the real repo; both patched and pristine pass the
     test when the file is present). In a real worktree the full check is
     expected green; if it is not, the failure is real and must be diagnosed,
     not waved through.
   - Confirm the golden diff is positions-only: decompress old (git HEAD) and
     new goldens and diff the JSON (only `pos` objects should differ).
   - Confirm no other golden changed: `git status` shows ONLY the 3 patched
     files + the regenerated `pkgvar_mrarg.json.gz`.
5. Commit with explicit pathspecs (never bare `git commit`), then report
   completion with branch, commits, and verification evidence. Do NOT merge —
   an independent reviewer checks the fix before any merge.

## Staged files in /tmp/rev1107-22-r1107-04/

- `R-1107-04.patch` — the fix (3 files; 88 lines). Generated this run via a
  temp git repo (HEAD baseline → patched files); `git apply --check` clean on
  pristine `59f0a247`; applied files byte-identical (`cmp`) to staged `fe/`.
- `pkgvar_mrarg.json.gz.NEW` — regenerated golden (after fix; 219,385 bytes,
  `gzip -n`; round-trip `cmp` clean).
- `EVIDENCE.md` — full verification record for this resumed run.
- `VERIFY.md` — short green-run summary (written this run; the original lane
  never wrote it).
- `validator-red-rerun.log` — RED re-run this session: 104 errors, exit 1.
- `validator-green.log` — GREEN: fresh export valid, exit 0.
- `build-patched.log`, `build-check-green.log`, `check-stages.log`,
  `patched-single-test.log`, `pristine-single-test.log`,
  `build-pristine-control.log` (the /tmp-full incident),
  `pristine-control-test.log` (0 bytes — superseded by `pristine-single-test.log`),
  `export-patched.err` (0 bytes) — this run's logs.
- `pkgvar_mrarg.json` — decompressed checked-in golden (reference).
- `validator-red.log` — validator on checked-in golden: 104 errors, exit 1.
- `validator-fresh-red.log` — validator on pristine fresh export: 104 errors.
- `build-pristine.log` — pristine `build.sh build` output.
- `fe/` — /tmp working copy of `internal/frontend` @ 59f0a247 WITH the fix
  applied (reference only; do NOT copy wholesale — apply the patch instead).
  NOTE: `fe/testdata/pkgvar_mrarg.json.gz` was overwritten with the NEW golden
  for the `build.sh check` run; the pristine copy survives as
  `pkgvar_mrarg.json` (decompressed) and in the main repo.
- `frontend-v1.md` — patched doc copy (same content as in the patch).
- `RESUME.md` — this file.
- `/run/r1107-04-gobuild/` — TRANSIENT Go build scratch (GOCACHE/GOPATH/$WORK);
  safe to delete after verification; NOT part of the deliverable.
- `/tmp/test/differential/corpus/46_sync_pool/main.go` — 1.7 KB corpus stub
  created this run so the single-test controls could run in the `/tmp` layout
  (copy of the real repo file; NOT part of the deliverable).
