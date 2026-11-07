# RESUME — lane rev1107-22-r07r08 (R-07 + R-08), ~10–20 min once quota clears

## Status

Fix is staged, `git apply --check` CLEAN, and RED→GREEN-proven at stub level
(see `EVIDENCE.md`). Lane ended BLOCKED on home quota (real 8 KB probe failed
EDQUOT ~22:15 CET 2026-11-07). Nothing was claimed, registered, or committed.

## Prerequisites (in order — do not skip)

1. **Quota probe FIRST:** real 8 KB write on home, verify byte count
   (e.g. `dd if=/dev/zero of=~/probe-resume bs=8192 count=1 && wc -c
   ~/probe-resume && rm ~/probe-resume`). If it fails, STOP and stay
   BLOCKED — do not claim a worktree or write registry entries.
2. Backoff check before any heavy step: `~/workspace/system/bin/load-shed.sh
   backoff` (exit 0 = CLEAR). The verification below is a real `go build` +
   pilot runs — heavy.
3. Note `/dev/null` is a regular file in this sandbox (see EVIDENCE.md §6);
   avoid relying on it in commands.

## Steps

1. Register: suggested id `graalgo-rev1107-22-r07r08`
   (workreg.py register; description: R-07+R-08 Makefile fix).
   Also mark the dead prior R-08 entry `graalgo-rev1107-r08.json` done/lost
   once this fix lands, so R-08 is not dispatched a third time.
2. Claim a worktree (new stream; the stale `creating` entry
   `graalgo/rev1107-r08` is a DIFFERENT stream and does not block this):
   `~/workspace/system/bin/wt.py new graalgo rev1107-22-r07r08 --chat
   graalgo-independent-codebase-review`
   Base: `59f0a247`. Work ONLY in the worktree; never build/commit in main.
3. Copy `FIX.patch` into the worktree and `git apply FIX.patch`
   (verified CLEAN against HEAD `59f0a247`; `git status` must show only
   `Makefile` modified).
4. Verify for real (all under `nice -n 10`, backoff-gated):
   a. R-07: `make graalgo` twice — the second run MUST rebuild (recipe
      output both times). Then `touch` a driver source change (or just
      re-run) and confirm `make test-regression` rebuilds the driver
      (at least confirm the rebuild happens; full regression suite is
      long — run it if the box allows, else the double-make + a
      driver-source touch is the mechanism proof).
   b. `make clean` removes `./graalgo`; `make graalgo` rebuilds it after.
   c. R-08: stub `tools/gate1/run.py` to exit 1 for one package (save the
      real one first, restore after): `make gate1-pilot GATE1_PKGS=errors`
      must exit non-zero; with the stub passing, exit 0. Then run the REAL
      pilot (`make gate1-pilot`) if feasible and record the verdicts — note
      the pilot may legitimately fail on real divergences; that is the gate
      working, not a regression (report the verdicts).
   d. `make all` green locally (per graalgo AGENTS.md §1: push only after
      `make all` is green). If `make all` is too heavy for the window, run
      at minimum the affected gates + `make check` and say so in the report.
   e. Confirm `git status` shows ONLY `Makefile` modified (no stray
      `graalgo` binary, no `.tmp` files — the binary is gitignored, but
      check).
5. Commit with an explicit pathspec (never bare `git commit`):
   `git commit Makefile -m "Makefile: make graalgo .PHONY and gate1-pilot fail on divergence (R-07, R-08)"`
   with body:
   ```
   R-07: graalgo had no prerequisites and was not .PHONY, so after the
   first build `make graalgo` was a no-op and test-regression tested a
   stale driver binary. Mark graalgo, gate1-pilot and gate1 .PHONY;
   make clean remove ./graalgo.

   R-08: gate1-pilot swallowed every per-package failure with `|| true`
   and printed success unconditionally. Accumulate failures and exit 1
   when any package's run.py exits non-zero, still running all packages
   first.
   ```
   Push immediately; watch CI to verdict (green or explained failure).
6. Mark the registry entry done (`workreg.py done --id
   graalgo-rev1107-22-r07r08`), then `wt.py integrate` is the
   COORDINATOR's call after independent review — do NOT merge yourself.
7. Report completion with: branch, commit SHA, verification evidence
   (real-build + pilot + `make all`/CI results), and the R-02 composition
   note (both-legs-fail still PASS until R-02 lands; this pilot will fail
   it automatically once R-02 does).

## Standing constraints (from the lane brief)

- No bridges; independent review before merge; worktree charter; every
  command under `nice -n 10`; backoff before heavy steps; truffle skill for
  backend work (not needed here — Makefile only).
- Do NOT fix anything beyond R-07/R-08 in this lane. If the real pilot
  surfaces divergences, report them — do not expand scope to fix them.
