# EVIDENCE — lane rev1107-22-r1107-04 (R-1107-04, P1) — RESUMED run 2026-11-07 ~22:14 CET

## Verdict: FIX VERIFIED RED→GREEN — staged, BLOCKED on home quota (no worktree, no commit)

- RED (pristine HEAD): validator on the checked-in golden → 104 errors, exit 1.
- GREEN (patched): fresh export validates 0 errors, exit 0; all remaining gate
  stages pass (see §5). The single `go test` failure in the full-suite run is
  environmental (missing corpus path in the `/tmp` layout), proven by controls
  that pass on BOTH patched and pristine code once the file exists.

## 1. What was resumed

- Original lane cancelled mid-flight (coordinator run ended, NOT a stop decision).
  Staged dir `/tmp/rev1107-22-r1107-04/` survived with: `RESUME.md`, `frontend-v1.md`
  (patched doc), `fe/` (working copy of `internal/frontend` @ HEAD with the fix
  applied), `pkgvar_mrarg.json` (decompressed checked-in golden), `validator-red.log`,
  `validator-fresh-red.log`, `build-pristine.log` (0 bytes), `export-pristine.err`
  (0 bytes), `pkgvar_mrarg.json` (4.6 MB).
- MISSING on resume (never written before cancellation): `R-1107-04.patch`,
  `pkgvar_mrarg.json.gz.NEW`, `VERIFY.md`. This resumed run generated all three
  from the staged `fe/` tree (see §4, §6) — the fix itself was NOT re-invented;
  it was verified against disk truth and completed.

## 2. Mechanism re-verified from disk truth at HEAD 59f0a247 (this run)

- HEAD: `59f0a247 B-02: plain launcher initializes the Go os surface
  (REVIEW-2026-11-07-07)`; master has NOT moved since the original lane.
  Main repo `~/workspace/graalgo` read-only throughout (no edits, no worktree).
- RED re-run (this run): `python3 tools/validate_frontend_v1.py pkgvar_mrarg.json`
  → **104 schema errors, exit 1** (`validator-red-rerun.log`). Breakdown:
  91× `pos.line: line is 1-based` + 13× `pos.len: must be non-negative int`
  (first error: `funcs[8].fields.params[0].pos.len: must be non-negative int,
  got -2458`). Independent JSON walk of the checked-in golden counts exactly
  **104** nodes with `line == 0` or `len < 0` — matches the validator.
  (The review's "104 nodes carry pos.line == 0" is imprecise; 104 is the total
  of both classes, as the original lane noted.)
- Fix present in staged `fe/`: `exporter.pos` (`export.go:3315-3380`) has the two
  guards (`!s.IsValid()` → `{File:0, Line:1, Col:1, Off:0, Len:0}`;
  unusable end → zero-length section at the valid start, rune-based col).
  Tree diff `fe/` vs HEAD `internal/frontend`: ONLY `export.go` and `build.sh`
  differ (plus build artifacts `overlay.json`, `p3frontend/.tmp`, `gofrontend`).
  Doc diff `frontend-v1.md` vs HEAD `docs/arch/frontend-v1.md`: +10 lines (§3,
  synthetic-position rule). `gofmt -l export.go` clean; `sh -n build.sh` clean.

## 3. RED → GREEN (fresh export, patched binary built in /tmp)

- Env: `GO=~/tools/go/bin/go` (go1.27.1), `GOCACHE`/`GOPATH`/`TMPDIR` under
  `/run/r1107-04-gobuild` (1.6G tmpfs; reused from the original lane),
  `HOME` = staging `fakehome`. All commands `nice -n 10`; backoff CLEAR before
  each heavy step (load 1.65, then 2.34).
- Patched build: `./build.sh build` in `fe/` → exit 0 (`build-patched.log`).
  Note: the pre-existing `fe/gofrontend` binary (21:26) predated the patched
  `export.go` (21:27), so it was rebuilt — the green evidence below is from
  the rebuilt binary (22:16).
- Fresh export: `./gofrontend testdata/pkgvar_mrarg.go` → exit 0, 4,619,894 bytes
  (`/run/r1107-04-gobuild/fresh-pkgvar_mrarg.json`).
- GREEN: `python3 tools/validate_frontend_v1.py fresh-pkgvar_mrarg.json` →
  **exit 0, "valid frontend/v1"** (`validator-green.log`).
- Positions-only check (old golden vs fresh export): non-`pos` trees identical;
  263 differing leaf paths, ALL inside `pos` objects; 0 invalid positions in
  the new export. New golden `pkgvar_mrarg.json.gz.NEW` (219,385 bytes,
  `gzip -n`), round-trip `gzip -dc | cmp` clean.

## 4. Patch

- `R-1107-04.patch` (88 lines, 3 files): `internal/frontend/export.go`,
  `internal/frontend/build.sh`, `docs/arch/frontend-v1.md`.
- Generated via a temp git repo (HEAD baseline → patched files) so paths are
  repo-relative `a/…`/`b/…` with no mode changes. Verified:
  `git apply --check` clean on a pristine `git archive 59f0a247` extraction,
  and applied files are byte-identical (`cmp`) to the staged `fe/` files.
- `build.sh` change: deletes the `pkgvar_mrarg` exclusion comment + loop skip,
  adds a `.json.gz` validation loop (decompressed temp copy). The golden is now
  checked like every other golden.

## 5. Full gate (`./build.sh check` in `fe/` with the NEW golden installed)

- `./build.sh check` ran 300.6s and ABORTED at `go test` (`set -e`):
  exactly ONE test failed — `TestSyncPoolRtatomicLowersToSyncop` —
  `ExportFile(46_sync_pool): gofrontend: stat
  ../../test/differential/corpus/46_sync_pool/main.go: no such file or
  directory` (`build-check-green.log`). Every other test passed.
- Root cause is the `/tmp` staging layout, NOT the fix: `fe/` is a standalone
  copy, so `../../test/...` does not exist there. The path DOES exist in the
  real repo (`~/workspace/graalgo/test/differential/corpus/46_sync_pool/main.go`).
- Controls (corpus file stubbed into the layout, 1.7 KB — `/tmp/test/...` for
  `fe/`, `/tmp/r1107-04-head/test/...` for the pristine tree):
  - PATCHED single test → **PASS** (5.09s, `patched-single-test.log`).
  - PRISTINE single test → **PASS** (5.09s, `pristine-single-test.log`).
  Both pass once the file exists; both fail identically without it. The test
  never reaches exporter code — it stats the corpus file first.
- The check stages after `go test` never ran (aborted), so they were run
  manually in `fe/` (`check-stages.log`) — ALL PASS:
  - vendoring diff gate: vendored `go/` tree byte-identical to GOROOT `src/go`.
  - validator selftest: all 23 checks ok, exit 0.
  - all 5 checked-in `.json` goldens: valid.
  - NEW `.json.gz` golden (decompressed): valid.
  - fresh exports of all 5 programs (straight01/02, methodvalue01,
    timeintr_value, mr_return_named): valid.
- Expected outcome in a real worktree (where `test/differential/` exists):
  full `build.sh check` green. The resume agent re-runs it there per RESUME.md.
- Incidents during this run: `/tmp` hit 100% (512M tmpfs, sibling lanes hold
  ~450M; this lane holds 34M) — a pristine-build attempt failed with
  `no space left on device` writing the 13.5 MB `gofrontend`
  (`build-pristine-control.log`). Freed 16M by deleting this lane's own
  `/tmp/r1107-04-head` extraction after the control ran. No sibling files
  touched.

## 6. Quota / staging status

- Home quota STILL EDQUOT-exhausted at resume (real 8 KB home write failed
  `Errno 122 Disk quota exceeded`, 22:14 CET; `df` shows 36G free — quota, not
  disk full). `/var/tmp` 100% full. `/tmp` 79% full (110M free); `/run` 12% used.
- Per the brief: NO worktree claimed, NO registry entry, NO commit, NO push.
  All deliverables staged in `/tmp/rev1107-22-r1107-04/`:
  `R-1107-04.patch`, `pkgvar_mrarg.json.gz.NEW`, `EVIDENCE.md` (this file),
  `RESUME.md` (updated), `VERIFY.md` (short green-run summary),
  `validator-red-rerun.log`, `validator-green.log`,
  `build-patched.log`, `build-check-green.log`, `check-stages.log`,
  `patched-single-test.log`, `pristine-single-test.log`,
  `pristine-control-test.log` (0 bytes — superseded by `pristine-single-test.log`),
  `build-pristine-control.log` (the /tmp-full incident), `export-patched.err`,
  plus the original lane's `fe/`, `frontend-v1.md`, `pkgvar_mrarg.json`,
  `validator-red.log`, `validator-fresh-red.log`.
- On quota recovery, the resume agent applies the patch per `RESUME.md` §"How to
  apply" (re-probe quota first; base HEAD `59f0a247`; if master moved, rebase
  + re-verify — a rebase voids any review).

## 7. Out of scope (carried from the original lane, NOT fixed here)

- Disk-package positions attributed to `file: 0` while line/off come from disk
  files; fixing it would add disk files to `package.files` (wire-format change
  touching the backend). The validator does not check file attribution.
