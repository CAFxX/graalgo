# RESUME — lane rev1107-22-r1107-02 (R-1107-02: `graalgo vet` multi-file false-fail)

Status: **BLOCKED-quota**. Fix + tests + evidence staged, nothing claimed,
nothing committed, no worktree, no registry entry (home EDQUOT-exhausted at
probe time 2026-11-07 ~22:15 CET; `/var/tmp` 100% full; `/tmp` had 267M free).

Staged in `/tmp/rev1107-22-r1107-02/`:
- `commands.go.patch` — unified diff vs HEAD `59f0a247`, git-style
  `a/cmd/graalgo/commands.go` / `b/cmd/graalgo/commands.go` labels.
  Verified: `patch -p1 --dry-run` applies; result byte-identical to
  staged `commands.go`.
- `commands.go` — the full fixed file (same content as the patch result).
- `commands.go.orig` — pristine copy from HEAD (for reference/diffing).
- `vet_multifile_test.go` — new test file for `cmd/graalgo/` (5 tests).
- `EVIDENCE.md` — mechanism verification, design rationale, what was and
  was not verified, open questions.
- `RESUME.md` — this file.

## Resume steps (on quota recovery)

1. Re-probe home with a real 8 KB write and verify byte count (per the
   quota-probe rule; a touch success proves nothing). If it still fails,
   stay blocked — do not claim anything.
2. Register: suggested id `graalgo-rev1107-22-r1107-02`.
3. Claim a worktree:
   `~/workspace/system/bin/wt.py new graalgo rev1107-22-r1107-02 --chat graalgo-independent-codebase-review`
   Work only there. Never build or commit in the main repo tree.
4. Apply the fix in the worktree (base must still be `59f0a247`; if master
   has moved, re-verify the `cmdVet` lines first):
   ```
   cd <worktree>/cmd/graalgo
   cp /tmp/rev1107-22-r1107-02/commands.go commands.go
   cp /tmp/rev1107-22-r1107-02/vet_multifile_test.go vet_multifile_test.go
   git diff --stat   # expect commands.go modified only
   ```
   (Copying the staged full file is equivalent to applying the patch;
   confirm with `git diff` that the change matches `commands.go.patch`.)
5. Verify RED→GREEN + affected suite (all under `nice -n 10`, backoff
   before heavy steps):
   - RED (before the fix): `git stash` the commands.go change (or check
     out the pristine file) and run the new tests — expect
     `TestVetMultiFilePackageSingleCheck` and
     `TestVetDuplicateFileCheckedOnce` to FAIL (two/one-duplicated
     invocations). Restore the fix.
   - GREEN: `go test ./cmd/graalgo/ -run 'TestVet' -count=1 -v` — all 5
     new tests pass.
   - Affected suite: `go test ./cmd/graalgo/ -count=1` (full package) and
     `go test ./internal/vet/ -count=1`.
   - Per-push gate per repo AGENTS.md §1 if feasible: `make all` (or at
     minimum `make check`).
   - End-to-end spot check (optional but valuable): build the backend and
     run `graalgo vet` on a real two-file package where b.go uses a.go's
     decl — expect exit 0; and on a package with an external `_test.go`
     file — expect exit 0.
6. Commit with explicit pathspecs:
   `git commit cmd/graalgo/commands.go cmd/graalgo/vet_multifile_test.go`
   (never bare `git commit`). Message should cite R-1107-02 and the
   review (REVIEW-2026-11-07-22).
7. Report completion with branch, commits, verification evidence.
   Do NOT integrate/merge — an independent reviewer checks first.

## Standing constraints (from the lane brief)

- No bridges; independent review before merge; worktree charter;
  every command under `nice -n 10`; backoff before heavy work;
  truffle skill for backend work (not needed here — this fix is
  driver-side Go only).
- Do NOT fix anything beyond R-1107-02 in this lane. Open questions in
  EVIDENCE.md (vet.Run multi-package grouping, vet build-flags usage
  text) are coordinator flags, not lane scope.
