# RESUME — lane rev1107-22-r01 (R-01 P0 + R-14 P1, fuzz-soak rewire)

## Status: BLOCKED on home quota (EDQUOT) — fix staged, NOT applied

- **Date:** 2026-11-07 ~22:12 CET. **HEAD verified against:** `59f0a247` (main repo, read-only).
- **Prior lane:** `rev1107-r01-r14` is confirmed dead, not duplicated — registry
  `active` but heartbeat = creation `2026-11-07T07:20:46Z` (~15h stale), branch
  `wt/graalgo-independent-codebase-review/rev1107-r01` at base `bf9b96ff` with no
  commits, no worktree, no live process/agent. Its fix (7 files, persisted under
  `~/workspace/goals/graalgo-go-toolchain-on-graalvm-truffle/hidden_files/codebase-review/lane-rev1107-r01/`)
  was adopted byte-identical here (cmp-verified) and extended with one further
  fix (see § What changed). The prior lane's `/tmp/rev1107-r01/` staging is gone
  (tmpfs loss); its `LANE-rev1107-r01-STATUS.md` survives at the codebase-review
  root (the file is NOT in the lane dir, despite the registry checkpoint saying so).
- **Quota:** real 8 KB home write failed `OSError: [Errno 122] Disk quota exceeded`
  at ~22:12 CET (`df` still shows 36G avail on /home — quota, not disk space).
  Per the lane brief: NO worktree claimed, NO registry entry written, NO commit.
  Everything is staged here in `/tmp/rev1107-22-r01/` (tmpfs, 483M free at staging
  time — **re-copy to persistent disk on recovery; /tmp does not survive a reboot**).
- **Also observed:** `/dev/null` is a regular empty file, not a character device
  (REVIEW-20's anomaly persists) — avoid `> /dev/null` redirections in this lane's
  commands; `/var/tmp` is 100% full (1.5M free) — do NOT use it for soak output.

## What changed (9 files, repo-relative paths under this staging dir)

Adopted from the prior lane (byte-identical, cmp-verified), all under `tools/fuzz-soak/`:

1. `harness/run_batch.py` — `FUZZ = tools/fuzz-soak`; build is `make -C {GR} build`;
   ensures gofrontend exists (conditional `make generate`); fails the batch when
   `results.jsonl` is short or every record is `HARNESS_ERROR`.
2. `harness/diff_one.sh` — GR derived from script location; launcher is
   `internal/backend/graalgo-sync-run` (no more `phases/` classpath, no more
   unquoted `ls` of jars); `JAVA_BIN` honored via `GRAALVM_HOME` derivation;
   `TMPDIR` defaults to `$WORKDIR/jvm-tmp`; missing launcher emits a
   `HARNESS_ERROR` record.
3. `harness/triage.py` — GR derived from file location, `FUZZ` repointed;
   `HARNESS_ERROR` now preserves `(stdout+stderr)[-500:]` instead of `""`.
4. `gha/driver/fuzz_driver.py` — `FUZZ = $GRAALGO_HOME/tools/fuzz-soak`.
5. `run_soak.sh` — stale `phases/` comments fixed; single-batch mode asserts
   `results.jsonl` non-empty; overnight loop counts failed batches and programs
   actually run; `soak-report.json` gains `programs_run` + `failed_batches`;
   exits 1 when zero programs ran in any batch. **Plus this lane's new fix:**
   `minimize_findings` now calls triage.py as `--prog <file> --out
   <minimized>/<file>.go --workdir <batchdir>/triage-work` (see § New finding).
6. `gha/Dockerfile` — slim-tarball layout updated to the new tree; pre-build is
   `make generate build` (was `make -C phases/phase3 main`).
7. `gha/README.md` — slim-tarball build command updated to the new layout.

Added by this lane (doc-only, same stale-`phases/` family):

8. `tools/fuzz-soak/DESIGN.md` — §4 slim-tarball layout line reworded to the new
   tree (was `phase1/{src,lib,frontend}, phase2/src, phase3/src`).
9. `.github/workflows/fuzz-soak.yml` — header comments repointed to
   `tools/fuzz-soak/DESIGN.md` / `tools/fuzz-soak/findings/` (workflow body
   already calls the live `./tools/fuzz-soak/run_soak.sh`; no behavior change).

SHA-256 of every staged file: `evidence/staged-sha256.txt`. Full diff vs HEAD:
`evidence/diff-vs-head.txt` (417 lines). Staged tree grep: zero `phases/` and zero
`graalgo-m9` references.

## New finding (this lane, not in the prior lane's fix)

`run_soak.sh`'s `minimize_findings` called triage.py in a way that can never
work: `triage.py <prog> --out <minimized-dir>` — but triage.py's CLI is
`--prog FILE --out FILE` (no positional, `--out` is a single minimized *file*).
Verified live: the old shape exits argparse error 2 (`required: --prog`);
the new shape parses and reaches `minimize()`. Consequence of the old shape:
every triage call failed; overnight mode swallowed it via `|| true` and
single-batch mode via the heredoc's unconditional exit 0 — **no finding was
ever minimized, silently, in either mode**. Fixed in the staged `run_soak.sh`
(§5 above). `aggregate.py` and `gen/generator.py` were swept and have no
stale refs; no other callers of `triage.py`/`run_batch.py` exist outside
`tools/fuzz-soak/`.

## Mechanism re-verified from disk truth at HEAD 59f0a247 (this lane)

- `phases/` does not exist; `tools/fuzz-soak/harness/run_batch.py:21,44`
  (`FUZZ = phases/phase3/fuzzing`, `make -C {GR}/phases/phase3 main`) — dead.
- RED demo (evidence/red-demo.log): HEAD `run_batch.py --seeds 0:1` exits 1 with
  `BUILD FAILED / make: *** .../phases/phase3: No such file or directory`,
  no `results.jsonl` produced. HEAD `run_soak.sh:112-115,133-134` would swallow
  this per batch and exit 0 with `diverged_total: 0` (read, not executed).
- Live build: root `Makefile:128 build` → `internal/backend/{truffle,runtime,
  interop}`; live launcher: `internal/backend/graalgo-sync-run` →
  `graalgo.p3sync.launcher.P3SyncMain` (classpath interop:runtime:truffle
  `out/classes` + `truffle/lib/*.jar`, load-bearing order); live generator:
  `tools/fuzz-soak/gen/generator.py`; gofrontend built by `make generate`
  (`internal/frontend/build.sh build` → `internal/frontend/gofrontend`).
- No bridges: the fix reuses the maintained `graalgo-sync-run` wrapper instead
  of reconstructing a classpath; no synthetic packages, no name-keyed cases,
  no overlays. Classification logic in `diff_one.sh` and the generator path
  are unchanged.

## Evidence (all in evidence/)

- `syntax-check.log` — `bash -n` both .sh OK; `py_compile` all 3 .py OK;
  `fuzz-soak.yml` parses as YAML.
- `red-demo.log` — HEAD run_batch RED (above).
- `triage-invocation.log` — old shape exit 2 (argparse), new shape parses.
- `gate-test.log` — verbatim `run_batch.py` fail-loudly block: empty/short/
  all-HARNESS_ERROR → exit 1; real batch (PASS + WRONG_OUTPUT) → exit 0.
- `soak-gate-test.log` — verbatim `batch_programs_run` (missing/empty → 0,
  2-line → 2) and final-report block: zero programs → exit 1 with
  `programs_run: 0`; real batch → exit 0, `programs_run: 2, diverged_total: 1`.
- `diff-vs-head.txt`, `staged-sha256.txt` — the exact change.

## Resume steps (when a real home write succeeds)

1. Probe home with a real 8 KB write and verify byte count. If it fails, STOP
   and re-report BLOCKED-quota; do not claim anything.
2. Copy this staging dir to persistent disk first (it lives on tmpfs).
3. Register the lane (suggested id `graalgo-rev1107-22-r01`; do NOT reuse
   `rev1107-r01-r14` — that entry stays with the dead prior lane for the
   coordinator to close). Claim a worktree:
   `~/workspace/system/bin/wt.py new graalgo rev1107-22-r01 --chat graalgo-independent-codebase-review`
   (new stream — the old `rev1107-r01` branch at base `bf9b96ff` is untouched).
4. Copy the 9 staged files into their repo locations in the worktree.
   `git status --porcelain` must show ONLY those 9 files. If `make generate`
   dirties `synth_*.go` (generator drift), that is pre-existing drift, NOT this
   lane — stop and report instead of committing it.
5. Commit with explicit pathspecs (never bare `git commit`).
6. Verify (every command under `nice -n 10`; `load-shed.sh backoff` before
   each heavy step):
   a. `make -C <worktree> build` then `make -C <worktree> generate` — both green.
   b. `run_soak.sh --seeds 0:2 --out <OUT>` with `<OUT>` on persistent disk
      OUTSIDE the repo (e.g. `~/workspace/.tmp/soak-r01-test`; NOT `/var/tmp`,
      100% full; NOT `/tmp`, 512M tmpfs): must execute 2 programs
      (`results.jsonl` 2 records, no `HARNESS_ERROR`), and a diverged program
      must be minimized into `batch-0/minimized/<file>.go` (triage fix).
   c. Fail-loudly: point `GR` at a broken build and confirm nonzero exit +
      `soak-report.json` with `programs_run: 0`.
   d. Re-run the evidence scripts in `evidence/` against the committed files.
7. Report branch + tip SHA + evidence; STOP. Do NOT integrate/merge —
   independent review first.

## Open items for the reviewer / coordinator

- The prior lane's registry entry `rev1107-r01-r14` is `active` with a
  creation-time heartbeat; its work is adopted here. Coordinator should close
  it as superseded when this lane lands (do not close it before — it is the
  only record of the prior lane's 07:20 staging).
- The old branch `wt/graalgo-independent-codebase-review/rev1107-r01`
  (base `bf9b96ff`, no commits) is orphaned; coordinator cleanup.
- `graalgo-sync-run`'s own header comment still cites the deleted
  `phases/phase1/frontend` gofrontend location (stale comment, launcher
  behavior correct) — out of this lane's scope, flagged for a doc lane.
- Full GREEN (2-program soak run) could NOT be executed in this run:
  it needs `make build` + `make generate` in a worktree, which needs home
  writes. The gates above were verified in isolation on verbatim code.
