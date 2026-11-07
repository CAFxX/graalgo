# R-1107-06 — Evidence (rev1107-22-r1107-06, RESUMED lane)

HEAD verified: `59f0a247` (main repo `~/workspace/graalgo`, read-only).
Fix status: STAGED, RED→GREEN verified. BLOCKED-quota (no worktree/commit).
Staged: `/tmp/rev1107-22-r1107-06/` — `R-1107-06.patch`,
`work-patched/` (fixed tree), `work-pristine/` (HEAD + new test file),
`EVIDENCE.md` (this file), `RESUME.md`, `adr/ADR-0004-m4-freeze-exceptions.md`.

## 1. Mechanism re-verified from disk truth at HEAD 59f0a247

- Stale message present verbatim: `internal/frontend/export.go:6269`
  (in `exportLinknameDecl`):
  `//go:linkname on generic func %s is not in M4 scope`.
- The guard is unreachable dead code: `exportFuncDecl` has exactly 2
  callers (`export.go:2431` user funcs loop, `synth_machinery.go:1417`
  synth loop), and BOTH skip templates (`if fi.isTemplate { continue }`)
  before calling it. Bodyless decls route to `exportLinknameDecl`
  (`export.go:6017`), so a bodyless template could only reach the guard
  via a future caller.
- gc ground truth, verified live with go1.27.1 (`~/tools/go/bin/go`,
  GOCACHE on /tmp, since removed): bodyless generic func, with or
  without `//go:linkname` → `generic function is missing function body`;
  bodyless method on a generic type → `missing function body`.
  There is no gc-valid program with this construct, so this is a
  gc-fidelity rejection, not a milestone gap.
- Stdlib scan (`~/tools/go/src`, 348 files contain `go:linkname`): no
  `//go:linkname` is immediately followed by a generic func/method
  declaration (regex hits were `[]byte` params, not type params). Only
  generic methods found are bodied test helpers in
  `runtime/traceback_test.go`.
- `work-pristine/{export.go,mr_disk.go,synth_machinery.go}` are
  byte-identical to HEAD. NOTE: `work-pristine/` also already contains
  the NEW `linkname_generic_test.go` (not in HEAD) — the original lane
  snapshotted pristine after writing the test. The patch therefore adds
  the test as a new file vs HEAD.

## 2. Fix (adopted from staged work, verified — not re-derived)

`internal/frontend/`, 3 modified files + 1 new test file. No bridges.
- `export.go`: new `missingGenericBodyError` (gc wording; funcs with own
  type params → "generic function is missing function body", methods
  whose genericity comes from the receiver → "missing function body");
  both template skip points reject bodyless templates with it instead
  of silently skipping (user funcs loop ~2426, synth loop via
  `synth_machinery.go` ~1410); the dead M4 guard in
  `exportLinknameDecl` now returns the same gc error for any template.
- `mr_disk.go`: comment-only relabel of the generic-linkname note.
- `linkname_generic_test.go` (NEW, 4 tests): generic func + linkname,
  generic func without linkname, bodyless method on generic type (the
  live gap), called generic func. All assert gc wording and assert the
  error does NOT contain "M4 scope".
- The only remaining "M4 scope" in `export.go` is `:8581`, an unrelated
  sync/atomic scope error — not this finding's message.

## 3. RED → GREEN (go1.27.1, `go test -vet=off`, GOCACHE/TMPDIR on /run)

Build env note: home quota EDQUOT + /tmp full (see §5); the suite ran
from the `/tmp` staging copies with `GOCACHE=/run/r1107-06/cache`
`TMPDIR=/run/r1107-06/tmp` (`/run` 1.2G free). Load backoff CLEAR (2.76)
before the heavy steps.
- RED (work-pristine): 3/4 pass, `TestLinknameGenericMethodRejected`
  FAILS — "ExportFile accepted bodyless method on generic type" (the
  live gap: silently skipped). Exactly as the test header predicts.
- GREEN (work-patched): 4/4 PASS.
- Full package suite: pristine full suite fails ONLY on
  `TestLinknameGenericMethodRejected` (97s run, 1 FAIL line);
  patched full suite PASSES ENTIRELY on re-run (149s, 0 FAIL lines).
  The first patched full-suite run showed one failure,
  `TestSyncPoolRtatomicLowersToSyncop` (missing repo-relative fixture
  `../../test/differential/corpus/46_sync_pool/main.go` in the
  standalone staging copy) — it did NOT reproduce on re-run in the
  identical patched tree, and it passed in the pristine full suite:
  flaky/order-dependent in the staging environment, not a regression.
  Logs: `/run/r1107-06/pristine-full-suite.log`,
  `/run/r1107-06/patched-full-suite.log` (tmpfs — re-run in the
  worktree on recovery for the durable record).
- gofmt: `export.go`, `mr_disk.go`, test file clean. `synth_machinery.go`
  is flagged by gofmt in BOTH pristine and patched (pre-existing at
  HEAD; the added hunk introduces no new gofmt diff) — deliberately
  not reformatted, to keep the patch minimal.

## 4. Patch integrity

`R-1107-06.patch` (9,927 B, sha256
`30c73828a4542300f168955b593aedc4970d5c34e25cb4a110eb5f1fa4a51408`):
applies cleanly with `-p1` from repo root / `-p3` from
`internal/frontend`; applied tree is byte-identical to `work-patched/`
(`diff -r` empty). Verified in `/run/r1107-06/applytest` (deleted
after verification; /tmp had no room).

## 5. Quota / environment (BLOCKED-quota)

- Real 8 KB home write probe failed `Errno 122 Disk quota exceeded`
  (~22:14 CET, `~/quota-probe-r1107-06`). No worktree claimed, no
  registry entry, no commit, no push. Main tree verified clean
  (`git status` empty).
- /tmp filled to 100% during this run (other lanes' build caches grew;
  `rev1107-22-r06` 84M, `rev1107-22-f1` 88M, etc.). First full-test
  attempt died ENOSPC on /tmp; rerun succeeded via /run. Keep new
  writes small; do NOT copy the repo tree.
- `/dev/null` anomaly (REVIEW-20) not re-checked this run.
- Daily-log append not attempted (home EDQUOT); goal entry recorded
  by this lane's resume run instead (see RESUME.md).

## 6. Still owed (resume lane, on quota recovery)

Apply `R-1107-06.patch` in worktree `rev1107-22-r1107-06`, re-run the
4 new tests + full `internal/frontend` suite IN THE REPO TREE (so the
sync-pool fixture resolves), `make check` if feasible, commit with
explicit pathspecs, report branch + SHA, do NOT integrate. Independent
review required before any merge.
