# RESUME NOTE — lane rev1107-22-r11 (R-11 torture battery dead)

Status: BLOCKED-quota. Fix verified against HEAD disk truth and STAGED here;
nothing claimed, committed, or registered (home EDQUOT-exhausted at staging
time, 2026-11-07 ~22:1x CET; see VERIFICATION.md for the probe evidence).

## What is staged in /tmp/rev1107-22-r11/

- `9dc7bca1-torture.patch` — the fix (649 lines, 10 files). Source commit
  `9dc7bca1` on
  `remotes/origin/wt/graalgo-independent-codebase-review/review-1105-toolrot-torture`
  ("Repoint tools/torture wrappers onto current backend layout (C-P1-E)").
  `git apply --check` against HEAD `59f0a247`: CLEAN.
- `RED-evidence.txt` — RED demonstration at HEAD: all four dead-CP entries
  MISSING; live `java -cp <dead CP> TortureBattery` →
  `ClassNotFoundException`; `hidden_files` OUTDIR/CORPUS refs in all three
  scripts; zero Makefile/CI invocations; rv1107-03 dead (branch at base,
  no worktree, heartbeat = creation 06:15); own registry only a 0-byte
  `.tmp` (`graalgo-r1107-07-r11.json.tmp`).
- `VERIFICATION.md` — fix-verification checklist vs HEAD + known gaps.
- `RESUME.md` — this file.

## Resume steps (on quota recovery)

1. Quota probe first: real 8 KB home write, verify byte count. Do NOT
   proceed on a touch-only probe.
2. Register: suggested id `graalgo-rev1107-22-r11`, chat
   `graalgo-independent-codebase-review`, heartbeat hourly.
3. Claim worktree: `~/workspace/system/bin/wt.py new graalgo rev1107-22-r11
   --chat graalgo-independent-codebase-review` (branch
   `wt/graalgo-independent-codebase-review/rev1107-22-r11`). Work only
   there; never build/commit in the main repo tree.
4. Apply the fix in the worktree (preferred): `git cherry-pick -n 9dc7bca1`
   (the commit contains ONLY the 10 torture files — verified via
   `git show 9dc7bca1 --stat`). Fallback: `git apply
   /tmp/rev1107-22-r11/9dc7bca1-torture.patch`. If HEAD has moved and the
   cherry-pick conflicts, resolve in favor of the 9dc7bca1 versions for the
   three scripts + corpus + README, and re-check the Makefile hunk against
   the current `internal/backend/runtime/Makefile`.
   NOTE: /tmp is tmpfs — if the staging dir is gone (reboot), regenerate
   the patch with `git diff 4a494cad 9dc7bca1 -- tools/torture
   internal/backend/runtime/Makefile docs/REPO_STRUCTURE.md` from the main
   repo (read-only).
5. Verify RED→GREEN (all under `nice -n 10`, `load-shed.sh backoff` before
   each heavy step):
   - Build: `make -C internal/backend/runtime main` (+ truffle/interop via
     the new make prerequisites) and `internal/frontend/build.sh` for the
     gofrontend binary (scripts fail fast without it).
   - `make -C internal/backend/runtime torture` — expect 11–12/12 legs;
     F4_q10ms is a KNOWN-flaky leg (tail-driven mean-hop miss, Nov 5:
     11/12; needs scheduler-owner diagnosis, not a repointing defect).
     Record the actual leg table; do NOT weaken the leg to make it green.
   - `make -C internal/backend/runtime gmp-matrix` — expect 8/8 (Nov 5:
     7/8 with one env-only failure, both sides re-ran clean).
   - `make -C internal/backend/runtime stress-differential` — expect 4/4.
   - Run `check_markdown_links.py` over the new README (it adds links).
   - `git status` must show no `hidden_files/` creation and no changes
     outside the 10 files.
6. Commit with EXPLICIT pathspecs (the 10 files), push the branch.
7. Open question for the coordinator (do not decide alone): CI/root-make
   wiring — new scheduled workflow `torture.yml` (weekly, modeled on
   `stdlib-gate.yml`) vs extending root `make test` (already 30+ min).
   Battery is ~10 min; F4_q10ms flakiness must have a decided gate
   semantics first. The staged patch intentionally does NOT add unverified
   CI YAML.
8. Report completion (branch, commits, verification evidence) for
   independent review before merge. Do NOT integrate.

## Standing constraints (from the lane brief)

- No bridges; independent review before merge; worktree charter; every
  command under `nice -n 10`; backoff before heavy steps; truffle skill
  for backend work (not needed for this script-only fix).
- The `review-1105-toolrot-torture` registry entry is `done` but its
  branch was never integrated and never independently reviewed; this lane's
  rebased/cherry-picked result still needs the independent review gate.
- /dev/null is currently a regular file (see VERIFICATION.md) — avoid
  `>/dev/null` redirects in verification commands.
