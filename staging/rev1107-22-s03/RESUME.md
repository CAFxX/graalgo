# RESUME — lane rev1107-22-s03 (S-03, GoPool unbounded registry)

## Status: fix complete + verified standalone, BLOCKED on home quota

- Home quota EDQUOT-exhausted at lane start (real 8 KB home write
  failed `Disk quota exceeded` ~22:13 CET, 2026-11-07; `/var/tmp`
  100% full). Per the brief: NO worktree claimed, NO registry entry
  written, NO commit made. Everything is staged here in
  `/tmp/rev1107-22-s03/` (tmpfs, 483M free at staging time).
- Also noted: `/dev/null` is a regular 74-byte file, not a character
  device (same anomaly REVIEW-2026-11-07-20 noted) — redirect with
  care until fixed.

## Staged files (in /tmp/rev1107-22-s03/)

| Path | What |
|---|---|
| `GoPool.java` | Fixed file — install over `internal/backend/runtime/src/main/java/graalgo/runtime/sync/GoPool.java` |
| `GoPoolTest.java` | Updated tests — install over `internal/backend/runtime/src/test/java/graalgo/runtime/sync/GoPoolTest.java` |
| `GoPool.java.orig` / `GoPoolTest.java.orig` | Pristine copies from HEAD `59f0a247` (for diffing) |
| `evidence/GoPool.diff`, `evidence/GoPoolTest.diff` | Unified diffs vs the `.orig` files |
| `evidence/RED-GREEN.md` | Full RED→GREEN evidence + design + honest residual |
| `evidence/red-probe.log` | RED: registry=200 for 200 dead threads, exit 1 |
| `evidence/red-probe-fixed.log` | Probe vs fixed code: registry=9 pre-drain, drained=200, exit 0 |
| `evidence/green-suite.log` | GREEN: 7/7, 3 consecutive runs |
| `evidence/green-suite-final.log` | GREEN on the final staged files: 7/7 |
| `VERIFICATION.md` | Mechanism re-verified from disk truth at HEAD `59f0a247` |
| `src/RunPoolTests.java`, `src/RedProbe.java` | Standalone runner + RED probe (source) |
| `red/`, `green/`, `probe-fixed/` | Exact compile trees used for the evidence |

## Resume steps (on quota recovery)

1. Probe home with a real 8 KB write, verify byte count. Only
   proceed if it succeeds.
2. Register: suggested id `graalgo-rev1107-22-s03` (work registry).
3. Claim worktree: `~/workspace/system/bin/wt.py new graalgo
   rev1107-22-s03 --chat graalgo-independent-codebase-review`
   (branch `wt/graalgo-independent-codebase-review/rev1107-22-s03`).
   Base must be `59f0a247` or its successor — if master has moved,
   re-verify the mechanism at the new HEAD first; the staged files
   apply cleanly only if `GoPool.java`/`GoPoolTest.java` are unchanged
   since `59f0a247` (check with `git diff` after copying).
4. Copy the two staged files into the worktree (verify byte counts
   with `wc -c` after copying: `GoPool.java` 15350 bytes,
   `GoPoolTest.java` 7582 bytes at staging time — re-check if the
   files were touched since).
5. Run `~/workspace/system/bin/load-shed.sh backoff` before any heavy
   step; all commands under `nice -n 10`.
6. Verify in the worktree (NOT done by this lane — standalone
   evidence only, see `evidence/RED-GREEN.md`):
   - the runtime module suite (`make test-ci` or the runtime
     module's test target — must include `GoPoolTest` via
     `SchedTestRunner`, which already lists it);
   - `make snapshot-check` / the differential corpus for
     `sync.Pool` (`test/differential/corpus/46_sync_pool`) — the
     change is behavior-preserving by design (items drained, not
     dropped), but the differential probe is the repo's contract
     for behavior-affecting changes (graalgo AGENTS.md §3);
   - `make check` (grep/format gates) for the touched files.
7. Commit with explicit pathspecs (never bare `git commit`):
   the two Java files only. Suggested message:
   `P1 (REVIEW-2026-11-07-07 S-03): reap dead-thread Locals in GoPool`
   with a body noting the reaping triggers and the documented
   residual (never-touched-again pool).
8. Report completion with branch, commit, and verification evidence.
   Do NOT integrate — independent review first (standing order).

## Design reminder for the reviewer

- Reaping drains dead threads' items to the global queue: no item
  loss, `testCrossThreadSteal` semantics preserved.
- Triggers: every slow-path `get`; every 1024th pool op per live
  thread (owner-confined counter, no lock on the fast path);
  amortized on registration.
- Documented residual: a pool never touched again after its
  goroutines die retains their Locals until next touched or
  collected (see `evidence/RED-GREEN.md`).
