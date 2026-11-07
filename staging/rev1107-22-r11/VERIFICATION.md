# R-11 fix verification (lane rev1107-22-r11, HEAD 59f0a247)

Fix source: commit `9dc7bca1` ("Repoint tools/torture wrappers onto current
backend layout (C-P1-E)", branch
`remotes/origin/wt/graalgo-independent-codebase-review/review-1105-toolrot-torture`,
registry `review-1105-toolrot-torture.json` status done, "NOT integrated,
awaits independent review"). The commit touches exactly 10 files, all
torture-related; it is NOT an ancestor of HEAD 59f0a247. This lane adopts
that fix rather than duplicating it, after verifying it against HEAD disk
truth (below). Staged patch: `9dc7bca1-torture.patch`
(`git diff 4a494cad 9dc7bca1 -- tools/torture
internal/backend/runtime/Makefile docs/REPO_STRUCTURE.md`, 649 lines).
`git apply --check` against HEAD 59f0a247: CLEAN (rc=0).

## Fix-verification checklist (all read-only, HEAD 59f0a247)

1. Battery classpath mirrors runtime TEST_CP exactly.
   Fixed `run_battery.sh:30`:
   `internal/backend/runtime/out/test-classes:internal/backend/runtime/out/classes:internal/backend/truffle/out/classes:internal/backend/interop/out/classes:internal/backend/truffle/lib/*`
   vs runtime Makefile `TEST_CP = $(OUT_TEST):$(RUN_CP)`,
   `RUN_CP = $(OUT_MAIN):$(TRUFFLE_CLASSES):$(INTEROP_CLASSES):$(LIB)`.
   All five dirs exist at HEAD (verified `ls -d`); `truffle/lib/` holds the
   same seven 25.0.4.1.1 jars the dead CP named. Order matches (test, main,
   truffle stubs, interop, jars). TortureBattery class present:
   `internal/backend/runtime/out/test-classes/graalgo/sched/torture/TortureBattery.class`.
2. F1–F6 legs exist: `TortureBattery.java:560-565` dispatches F1/F3/F4/F5/F5b/F6.
3. Differential scripts use the product launcher, not dead SchedMain.
   `internal/backend/graalgo-sync-run` exists, executable, passes `"$@"`
   to `P3SyncMain`; `P3SyncMain.java:162-168,220-221,520` parses `--gmp=N`
   and builds `SchedulerCore(gmp, ...)`. README + script headers document
   WHY SchedMain is not used (its Truffle NodeFactory lacks
   ifacemethod/makechan hooks) — the "which runner owns F1–F6" design
   question is answered: TortureBattery in-JVM for F1–F6, P3SyncMain for
   Go differentials.
4. Corpus is committed, deterministic, within toolchain gaps.
   Four new files under `tools/torture/corpus/`: `gmp_compute.go`,
   `gmp_mixed.go`, `cancel_broadcast.go`, `goroutine_storm.go`. All use only
   goroutines/channels/`println` (stderr on both gc and graalgo); sums are
   order-independent so `exact` byte comparison is schedule-independent.
   Renames from the dead names are documented (`ctx_cancel` /
   `rt_finalizer_storm` referenced programs not executable on this base).
5. No `hidden_files/` or `phases/` paths survive in the fixed scripts.
   Post-patch grep of all three scripts at 9dc7bca1: zero hits for
   `phase1|phases/|hidden_files` outside comments explaining the SchedMain
   non-use. Logs go to `internal/backend/runtime/out/torture/<name>/`
   (gitignored via `out/`; AGENTS.md §5 no-debris rule respected).
6. Fail-fast checks present: java/go/gofrontend/launcher/class-dirs all
   checked executable/existing before any leg runs; `set -euo pipefail`
   with the display-only `grep | tail` pipeline guarded (`|| true`).
   `_JAVA_OPTIONS=-Xmx1g` notice stripped (retried 3x, 5s; never aborts).
7. Makefile changes match HEAD context (confirmed by clean apply):
   `$(TRUFFLE_CLASSES)/.built` rule (`make -C ../truffle main` — truffle
   `main: $(RUNTIME_CLASSES)/.built $(OUT_MAIN)/.built $(GOFRONTEND)`, no
   cycle: runtime `.built` is already a `torture` prerequisite); `torture`
   gains the truffle prerequisite; new `gmp-matrix` and
   `stress-differential` targets build interop first.
8. REPO_STRUCTURE.md one-line doc update (torture entry names the committed
   corpus); all relative links resolve from their locations
   (`[corpus](corpus/)`, `[internal/frontend/build.sh](../../internal/frontend/build.sh)`,
   `[no-debris rule](../../AGENTS.md#5-no-debris-rule)` — AGENTS.md §5 is
   "No-debris rule", anchor correct).
9. Prior-lane verification (Nov 5 base 4a494cad, from the commit message):
   stress 4/4 PASS (twice); gmp-matrix 7/8 (one env-only failure, both
   sides re-ran clean byte-identical); battery 11/12 — only F4_q10ms fails
   (mean hop ~150–170us vs 100us budget, p50 ~6us, tail-driven, reproduces
   on an idle box; needs scheduler-owner diagnosis, NOT a repointing
   defect). Full evidence cited:
   `~/workspace/system/boot-recovery/review-1105-toolrot-torture-REPORT.md`
   (home file; not re-read this run — quota).

## Known gaps / NOT fixed by this staged patch

- CI/root-make wiring (finding's "nothing invokes the scripts" half is
  only partly closed): the patch adds `make torture` / `make gmp-matrix` /
  `make stress-differential` targets in `internal/backend/runtime/Makefile`,
  but no CI job and no root `make test` target calls them. Root `make test`
  (Makefile:241) runs truffle/runtime/interop `test` only; workflows are
  `ci.yml`, `conformance.yml`, `fuzz-soak.yml` (nightly 01:00 UTC),
  `markdown-links.yml`, `stdlib-gate.yml` (weekly Sun 03:00 UTC). Proposal
  for the resume lane + coordinator: new scheduled workflow `torture.yml`
  (weekly, modeled on `stdlib-gate.yml`: checkout, GraalVM 25, Go 1.27.1,
  `make -C internal/backend/runtime torture gmp-matrix
  stress-differential`) OR extend root `make test`. Battery takes ~10 min;
  F4_q10ms is a known-flaky leg — decide the gate semantics (fail the
  workflow on any leg FAIL, or record F4_q10ms as known-flaky) before
  wiring it into a gate. Do NOT silently add `|| true`.
- Re-verification on current HEAD is OWED: the Nov 5 verification predates
  HEAD by 2 days (`59f0a247` B-02 os-surface init in `Main.java`,
  `bf9b96ff` frontend-validator wiring). B-02 touches the plain launcher,
  not P3SyncMain, so drift risk is low — but RED→GREEN must be re-run by
  the resume lane: `make -C internal/backend/runtime torture`,
  `gmp-matrix`, `stress-differential` (backoff-gated, nice -n 10).
- `gofrontend` binary absent in this checkout (build artifact, expected);
  scripts fail fast on it. Resume lane builds via
  `internal/frontend/build.sh` / root `make build` first.
- /dev/null is a REGULAR FILE (94 bytes, 2026-11-07 21:15), not a character
  device — pre-existing anomaly also noted by REVIEW-20, not re-checked
  until this run. Anything redirecting to /dev/null writes that file.
  Avoid `>/dev/null` in verification commands until fixed (out of scope).

## Quota state at staging time

- Home EDQUOT-exhausted: real 8 KB write to `~/quota-probe-rev1107-22-r11.bin`
  failed `Disk quota exceeded` (2026-11-07 ~22:1x CET). Device has 36G free
  (`df /home/hatch`) — quota limit, not device-full.
- /var/tmp 100% full (1.5M free). /tmp 331M avail, writable (probe 8 KB OK).
- Per the lane brief: NO worktree claimed, NO registry entry written, NO
  commit. Fix + evidence + resume note staged here in /tmp only.
