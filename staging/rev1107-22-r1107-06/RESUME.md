# RESUME — rev1107-22-r1107-06 (R-1107-06, P1)

Status: fix STAGED + RED→GREEN verified. BLOCKED-quota — no worktree,
no registry entry, no commit, no push. Do NOT start from clean.

## What is done

- Mechanism re-verified at HEAD `59f0a247` (see EVIDENCE.md §1).
- Fix adopted from the original lane's staged `work-patched/` (verified,
  not blindly trusted): 3 files + 1 new test file.
- RED→GREEN: pristine 3/4 (method test fails = live gap), patched 4/4.
  Full suite: pristine fails ONLY on the method test; patched full
  suite PASSES on re-run (first patched run had one flaky
  TestSyncPoolRtatomicLowersToSyncop failure that did not reproduce —
  see EVIDENCE.md §3). Full-suite logs live on /run (tmpfs); re-run
  in the worktree for the durable record.
- `R-1107-06.patch` (9,927 B) applies cleanly (`-p1` repo root) and
  reproduces `work-patched/` byte-for-byte.

## Staged files (`/tmp/rev1107-22-r1107-06/`)

- `R-1107-06.patch` — the fix (sha256 in EVIDENCE.md §4).
- `work-patched/` — fixed `internal/frontend/` tree (16M).
- `work-pristine/` — HEAD files + new test file (16M). NOT pure HEAD:
  it already contains `linkname_generic_test.go`.
- `EVIDENCE.md`, `RESUME.md` (this file), `adr/ADR-0004-m4-freeze-exceptions.md`.
- WARNING: /tmp is tmpfs, 100% full at handoff — copy this dir to
  persistent disk on quota recovery BEFORE reboot; it does not survive
  one. If staging is lost, the patch regenerates from the diffs in
  EVIDENCE.md (export.go/mr_disk.go/synth_machinery.go hunks +
  linkname_generic_test.go as published in the lane's final report).

## Resume steps (on a successful real 8 KB home write)

1. Register `graalgo-rev1107-22-r1107-06`; claim worktree
   `rev1107-22-r1107-06` (`wt.py new graalgo rev1107-22-r1107-06
   --chat graalgo-independent-codebase-review`).
2. `git apply R-1107-06.patch` from the repo root; verify
   `git status` shows exactly 4 files (3 modified + 1 new).
3. Backoff-gated: `go test ./internal/frontend/ -run
   'TestLinknameGeneric|TestBodylessGeneric' -v` (expect 4/4), then
   full `go test ./internal/frontend/` (expect all pass IN THE REPO
   TREE, incl. TestSyncPoolRtatomicLowersToSyncop), then `make check`
   if feasible.
4. Commit with explicit pathspecs; report branch + SHA; do NOT merge.
   Independent review + CI on the exact SHA still owed.

## Environment notes

- Quota probe failed EDQUOT ~22:14 CET; /tmp 100% full; /var/tmp 100%
  full. Build cache workaround that worked: `GOCACHE` + `TMPDIR` on
  `/run` (1.2G free at handoff).
- `synth_machinery.go` is gofmt-dirty at HEAD (pre-existing); do not
  reformat it — the patch intentionally keeps the diff minimal.
