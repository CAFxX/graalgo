# S-01 — RESUME note (lane rev1107-22-s01)

**State: FIX VERIFIED RED→GREEN, staged, BLOCKED-quota. Nothing is committed, no worktree exists, no registry entry exists.**

## What is done

1. Mechanism re-verified from disk truth at HEAD `59f0a247` (see `EVIDENCE.md` §1).
2. Prior lane's `s01-fix.patch` verified: applies cleanly to pristine HEAD copies, applied output byte-identical to `apply-check/`. Patch is sound — reuse it, do not regenerate.
3. RED→GREEN verified this run (`logs/red.log`, `logs/green.log`): pristine code returns case 1 (`returned:1`); fixed code panics with `send on closed channel`.
4. Targeted regression verified (`logs/red-regression.log` vs `logs/green-regression.log`): 96 tests — RED 1 failure (exactly the new test), GREEN 0 failures.
5. Quota gate failed (real 8 KB home write, EDQUOT, ~22:26 CET): no worktree, no commit. `/tmp` was 100% full mid-run (sibling lanes) and recovered to 16M free by end of run.

## Resume steps (on a successful real 8 KB home write)

1. Probe home quota with a real 8 KB write and verify byte count. Do NOT proceed on a touch-only probe.
2. Register `graalgo-rev1107-22-s01` in the work registry; claim worktree `rev1107-22-s01` via `wt.py new graalgo rev1107-22-s01 --chat graalgo-independent-codebase-review` (or the coordinator's chat id).
3. In the worktree (base HEAD `59f0a247` — re-verify `git rev-parse HEAD`; if master has moved, re-verify the patch applies and the mechanism is unchanged before applying):
   `git apply /tmp/rev1107-22-s01/s01-fix.patch` (from the repo root; `-p1` paths are repo-relative).
4. Verify: `git apply --check` first, then run the full runtime suite — `make -C internal/backend/runtime test` (or `make test` at the runtime module), backoff-gated. Expect the new `SelectSweepPoisonTest` green and no regressions. `make test-ci` + `make snapshot-check` if feasible.
5. `gofmt` check is N/A (Java); `javac` already ran with `-Xlint:deprecation -Werror` clean in the overlay compiles.
6. Commit with explicit pathspecs (the 3 files only), report branch + SHA, do NOT integrate/merge. Independent review + CI on the exact SHA still owed.
7. Close the stale prior-lane state if any exists (none found for S-01 — no prior registry entry/branch).

## Do NOT

- Do not regenerate the patch or the test — both are verified; re-verify only if HEAD moved.
- Do not run the full suite in `/tmp` — the gofrontend-dependent tests need temp space and a real build; do it in the worktree.
- Do not "fix" the poll-phase ready-beats-poison behaviour as part of S-01 (see `EVIDENCE.md` §5.2 — separate design decision).
- Do not delete `build/graalgo/` or other lanes' `/tmp/rev1107-22-*/` dirs to free space.

## If /tmp staging is lost (reboot)

The patch is reproducible only from this staging dir — there is no other copy. If it is gone, regenerate from `EVIDENCE.md` §2 (file list + fix description) against the then-current HEAD, re-verify the mechanism, and re-run RED→GREEN with `run-verify.sh` (also staged here; recreate from the method description if lost).
