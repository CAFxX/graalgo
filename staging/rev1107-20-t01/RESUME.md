# RESUME NOTE — lane rev1107-20-t01 (R20-T01, P1)

Status: FIX STAGED, NOT LANDED. Home disk quota was CLOSED for the whole
lane (real 8 KB home write failed `Disk quota exceeded` at ~20:31 CET
2026-11-07; `/var/tmp` 100% full; `/tmp` 44M free at handoff). No worktree
was claimed, no file under `~/workspace` was written, no commit exists.
Do NOT integrate from this note — apply the patch in a worktree first.

## What is staged

- `/tmp/rev1107-20-t01/rev1107-20-t01.patch` — the complete fix (5 files
  under `test/differential/`, +94/−0 content lines). Verified: `patch -p1`
  from the repo root applies cleanly to pristine HEAD and reproduces
  `/tmp/rev1107-20-t01/work/` byte-identical.
- `/tmp/rev1107-20-t01/EVIDENCE.md` — full RED/GREEN record (read first).
- `/tmp/rev1107-20-t01/work/` — fixed tree (patch source of truth).
- `/tmp/rev1107-20-t01/pristine/` — untouched `test/differential` (diff base).
- `/tmp/rev1107-20-t01/red/` — pristine + new tests (RED reproduction).
- `/tmp/rev1107-20-t01/fixtures/` — CLI fixtures for the empty-dir /
  all-excluded runs.

## Self-gating resume (run in order; stop at the first failing gate)

1. Quota gate (do NOT proceed until this passes): real home write probe —
   `head -c 8192 /dev/zero > ~/workspace/graalgo/.quota-probe-resume &&
   stat -c %s ~/workspace/graalgo/.quota-probe-resume` must print 8192;
   then `rm` the probe. A 0-byte touch passing proves nothing; a
   `/var/tmp` probe proves nothing (tmpfs, separate quota).
2. Claim worktree: `~/workspace/system/bin/wt.py new graalgo
   rev1107-20-t01 --chat graalgo-independent-codebase-review`
   (branch `wt/graalgo-independent-codebase-review/rev1107-20-t01`).
   Work ONLY there; never edit/build/commit in the main tree.
3. Apply: `cd <worktree> && patch -p1 <
   /tmp/rev1107-20-t01/rev1107-20-t01.patch` (or `git apply`).
   Verify `git status` shows exactly the 5 patched files.
4. Re-verify from the applied patch (not from /tmp/work):
   `go test ./cmd/... ./internal/... -count=1` inside
   `test/differential/` — all 5 packages must pass, including the 3 new
   tests (`TestRunEmptyKeptSet`, `TestRunNoFiles`,
   `TestSnapshotRecord_ZeroKeptSet`). Also `gofmt -l cmd internal` clean,
   `go vet ./...` clean.
5. Register work: `python3 ~/workspace/system/bin/workreg.py register
   --id rev1107-20-t01 --chat graalgo-independent-codebase-review --kind
   subagent --description "R20-T01: ..." --checkpoint <this file>` (the
   lane's attempt failed on EDQUOT; retry once quota is open).
6. Report completion (branch + SHA) to the coordinator for INDEPENDENT
   REVIEW. Do NOT integrate yourself: `wt.py integrate` is the
   coordinator's call after a clean review. If quota is still closed at
   any point, stand down again — do not spin or poll.

## Scope guardrails for the resume agent

- Do NOT touch `cmdRun` / `loadCorpus` (REVIEW-18 TL-03 owns that leg).
- Do NOT "fix" `snapshot check` (already guarded).
- `scan` modes intentionally unguarded (informational, not a gate).
- Reviewer note: the fix guards BOTH `typeparam.Run` (shared choke point)
  and both commands (brief-literal defense in depth). The command-level
  checks are unreachable while Run guards; if the independent reviewer
  prefers Run-only, dropping the two 3-line command guards is safe — the
  unit tests pin the Run guard, and the snapshot-record test pins its own.
- Base: patch was generated against the `test/differential` tree as read
  2026-11-07 ~20:30 CET (HEAD 59f0a247 per the lane brief). If master has
  moved, apply, resolve any fuzz by hand (the hunks are small insertions),
  and re-run step 4 before reporting.
