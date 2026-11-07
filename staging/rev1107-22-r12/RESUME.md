# R-12 remediation lane rev1107-22-r12 — RESUME NOTE (BLOCKED-quota)

Date: 2026-11-07 ~22:12 CET. Lane: rev1107-22-r12. Status: BLOCKED on home quota
(EDQUOT), fix verified + staged, NOT applied, NOT committed, NOT integrated.

## Issue

`tools/godebug-diff/godebug-diff.sh` dead (`phases/phase2` gone; RED reproduced at
HEAD `59f0a247`: `cd` fails line 20 → `build failed`, exit 1) and would dirty the
tracked `tools/godebug-diff/REPORT.md` on success. P1 (REVIEW-2026-11-07-07 R-12,
carried by REVIEW-2026-11-07-22).

## What this lane did

1. Verified the prior lane `rev1107-r12` is dead (registry active, heartbeat
   2026-11-07T10:36:20Z ~11.5h stale, branch at base `bf9b96ff` with no commits,
   no worktree, no live process). Recovery, not duplication.
2. Re-verified the mechanism from disk truth at HEAD `59f0a247` (see EVIDENCE.md):
   RED reproduced, probes exist, SchedMain exists, all three comparator formats
   verified against live sources, root `make build` + RUN_CP equivalent, `.tmp/`
   gitignored, doc ref exists. The morning lane's staged fix is still valid at
   the new HEAD (only intervening commit is B-02, launcher-only).
3. Staged the fixed script here (`godebug-diff.sh`, `cmp`-verified, sha256
   `48d0e0d309def30b8546babb7c5e8d4416faf1114fdfc875dc0dfa5e8e518395`).
   Durable copy: `~/workspace/goals/graalgo-go-toolchain-on-graalvm-truffle/hidden_files/codebase-review/R12-godebug-diff.sh`.
4. Quota probe: real 8 KB home write → `Errno 122 Disk quota exceeded`. Stopped
   per the brief: no worktree, no registry entry, no commit, no repo-tree edits.
   Main-tree `git status` clean.

## Resume steps (successor lane)

1. Probe home with a real 8 KB write; it must succeed (a touch proves nothing).
   Also re-probe `/tmp` writability if staging is needed.
2. Register (suggested id: `graalgo-rev1107-22-r12`), heartbeat hourly.
3. Claim a worktree: `~/workspace/system/bin/wt.py new graalgo rev1107-22-r12 --chat graalgo-independent-codebase-review`
   (branch `wt/graalgo-independent-codebase-review/rev1107-22-r12`). NOTE: the
   branch `wt/graalgo-independent-codebase-review/rev1107-r12` (prior lane) already
   exists at base `bf9b96ff`; the new stream name avoids collision. Do NOT reuse
   the old branch name.
4. In the worktree: `cp /tmp/rev1107-22-r12/godebug-diff.sh tools/godebug-diff/godebug-diff.sh`
   (or the goal-hidden_files copy — same bytes; `cmp` after copying).
   Do NOT copy the tracked REPORT.md anywhere; it stays as the last reviewed
   green run.
5. Verify end-to-end (backoff CLEAR first via `~/workspace/system/bin/load-shed.sh backoff`):
   `./tools/godebug-diff/godebug-diff.sh` — expect `4 passed, 0 failed`, report at
   `tools/godebug-diff/.tmp/REPORT.md`, and `git status` clean (the `.tmp/` report
   is gitignored; `gofrontend`/`overlay.json` are gitignored). First run builds
   the backend (~minutes) — that is the script working as designed. The morning
   lane's 0-byte-output smoke runs are INVALID evidence and must not be cited.
6. Commit with explicit pathspecs on the lane branch; report branch + tip SHA +
   verification evidence; do NOT merge (independent review first).
7. If quota is still closed, do NOT claim a worktree or commit — re-stage in /tmp
   and report BLOCKED-quota again.

## Files staged here

- `godebug-diff.sh` — fixed script (8985 bytes, cmp-verified)
- `EVIDENCE.md` — full verification evidence for this lane
- `RESUME.md` — this file

/tmp is tmpfs and does NOT survive a reboot; the goal-hidden_files copy is the
durable one.
