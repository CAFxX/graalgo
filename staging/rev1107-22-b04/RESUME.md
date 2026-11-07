# Resume note — lane rev1107-22-b04 (B-04)

Fix + evidence are fully staged and verified RED→GREEN in
`/tmp/rev1107-22-b04/` (see `VERIFICATION.md`). No worktree, no registry
entry, no commit was made: home quota was EDQUOT-exhausted for the whole
run. Do NOT re-do the design or the verification — re-run only the steps
below.

## 0. Preconditions

- Home writes healthy: probe with a REAL 8 KB write and verify its byte
  count (`python3` writing 8192 bytes, `os.path.getsize` == 8192, then
  remove). A 0-byte touch passing proves nothing. If the probe fails, stop
  here and report BLOCKED-quota again — do not claim a worktree.
- Backoff CLEAR (`~/workspace/system/bin/load-shed.sh backoff`, exit 0)
  before every heavy step (build/test). Every command under `nice -n 10`.

## 1. Register + claim

- Registry id: `graalgo-rev1107-22-b04`. Note the prior lane's entry
  `rev1107-b04` is still `resuming` (heartbeat 2026-11-07T12:29Z, dead —
  no worktree, branch at base, no live agent); mark it done/superseded
  when registering the new id so a later sweep does not re-dispatch it.
- `~/workspace/system/bin/wt.py new graalgo rev1107-22-b04 --chat graalgo-independent-codebase-review`
  (branch `wt/graalgo-independent-codebase-review/rev1107-22-b04`).
  Work ONLY in that worktree. Never build or commit in the main repo tree.

## 2. Apply the fix

- `git apply /tmp/rev1107-22-b04/B04.patch` in the worktree.
  Verified to apply cleanly at HEAD `59f0a247`; if master has moved, run
  `git apply --check` first and re-verify the mechanism (the patch touches
  `GoOs.java`, `GoNet.java`, `GoNetpoll.java`, `IfaceTestRunner.java`, plus
  new `GoOsResetTest.java`).
- Staged patch is byte-identical to the prior lane's
  `.../hidden_files/codebase-review/lane-rev1107-b04/B04.patch`.

## 3. Verify (in the worktree, after backoff CLEAR)

- `make -C internal/backend/interop test` — must exit 0; includes the two
  new `GoOsResetTest` tests plus the existing `GoNetpollTest`/`GoOsErrnoTest`
  suites. (This lane's scratch evidence: RED→GREEN on the new tests
  standalone, plus a full-suite green-vs-red comparison — 133 vs 131 PASS,
  identical 59 pre-existing/environmental failures, zero regressions.
  The worktree run is still required: it rebuilds stale runtime/truffle
  classes — see below — and runs under the real harness.)
- `make -C internal/backend/interop check`.
- `make snapshot-check` (repo AGENTS.md §3 behavior-change gate; the fix
  only closes previously-leaked handles / clears previously-stale state,
  but run the gate anyway).
- Known wrinkle: prebuilt `runtime/out/classes` in the main tree is stale
  vs `59f0a247` (missing `OsSurfaceInitializer.class` from B-02). `make`
  rebuilds the runtime first via its `.built` dependency — expect the
  worktree build to handle it; do NOT hand-copy class files.

## 4. Finish

- Commit with explicit pathspecs (never bare `git commit`).
- Report completion with branch, commits, verification evidence.
  Do NOT integrate/merge — an independent reviewer checks this lane before
  any merge (standing order).
