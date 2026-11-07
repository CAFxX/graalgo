# S-01 — SelectOp drops closed-send panic: EVIDENCE

- **Lane:** rev1107-22-s01 (RESUMED; original lane cancelled mid-flight, not by a stop decision).
- **Issue:** S-01 (P1), REVIEW-2026-11-07-07 §S-01. **HEAD:** `59f0a247` (verified: `git -C ~/workspace/graalgo rev-parse HEAD`).
- **Verdict: FIX VERIFIED RED→GREEN. BLOCKED-quota for landing (no worktree, no commit).**

## 1. Mechanism re-verified from disk truth at HEAD 59f0a247 (not trusted from the review)

`internal/backend/runtime/src/main/java/graalgo/runtime/SelectOp.java`, `execute()` block-phase sweep:

- `:240` (HEAD): `if (c.send() && ch.isClosed()) { poison = true; }` — the sweep records the poison and **continues**.
- `:277-279` (HEAD): `if (immediate != null) { cleanup(mine); return immediate; }` — a later case completing returns normally with **no poison check**.
- `:297-300` (HEAD): poison honoured only when `mine.isEmpty()`.
- Post-park paths (`:289`, `:317`) check only `resultPoison` (set by `close()` via `setPoison()` for already-enqueued waiters), not the sweep-local `poison`.

So a send case whose channel closed between poll and sweep is silently dropped whenever another case completes or wins after the park. Confirmed live by the RED run below (`returned:1`).

Poll-phase note (known residual, out of scope — see §5).

## 2. Staged patch verified (not trusted from the prior lane)

- `s01-fix.patch` (11,150 B, sha256 `b0955f0e74125084563bd9a8b21b66a670d06c8840adb61c26d8dfb4eb0b7184`)
  applies cleanly against pristine HEAD copies (`git apply --check` exit 0; applied result byte-identical to `apply-check/`).
- Fix (3 files, `internal/backend/runtime/` only; no bridges, no name-keyed cases):
  - `SelectOp.java`: sweep labels `sweep:`; on observing a closed send case, `break sweep`; immediately after the sweep, `if (poison) { cleanup(mine); Panic.throwPanic("send on closed channel"); }` — before the `immediate`/`claimedByOther`/park paths. The old `mine.isEmpty()` poison block is removed (unreachable once the new check precedes it).
  - `SelectSweepPoisonTest.java` (new, 148 lines): `FlipFlopChan` reports open on the first `isClosed()` probe (poll) and closed from the second (sweep); `GoChan.canSend()` verified not to consult `isClosed()` (`GoChan.java:181-184`), so exactly two `isClosed()` calls occur — deterministic in either sweep order. Asserts panic value contains `send on closed channel` and that the panic path leaves zero waiters on both channels.
  - `SchedTestRunner.java`: registers the new test class.
- Correctness notes (verified by reading, not just by the test): no self-claim is held when poison is observed (the poison branch precedes `tryClaimSelf`, and every prior iteration releases its claim); `mine` waiters from earlier iterations are removed by `cleanup(mine)` before the panic (asserted by the test); `poison` + `immediate`/`claimedByOther` cannot co-occur (loop exits on either, poison breaks immediately) — the new check's placement before all exit paths is still the safe order.
- NOT relied on: staged `build/graalgo/out/classes` (incomplete — 317 classes vs 337 in the repo's prebuilt `out/classes`; missing `Fmt`, `FmtScan`, etc.). All verification compiled fresh overlays against the repo's prebuilt HEAD classes.

## 3. RED→GREEN (this run, GraalVM JDK 25.0.4, `nice -n 10`, backoff CLEAR)

Method: single-file `javac` overlays shadowing the repo's prebuilt HEAD classes
(`run-verify.sh`; pristine `SelectOp.java` vs patched `SelectOp.java`, same new test both sides). Repo tree untouched (read-only; `git status` clean at end).

- **RED** (`logs/red.log`): pristine code — `FAIL ... got: returned:1`, `1 tests, 1 failures`, exit 1. The select returned the other case; the poison was dropped exactly as filed.
- **GREEN** (`logs/green.log`): fixed code — `PASS SelectSweepPoisonTest.testSweepPoisonPanicsInsteadOfReturningOtherCase`, `1 tests, 0 failures`, exit 0.

## 4. Regression — targeted blast radius, RED baseline vs GREEN (`run-regression.sh`)

Classes: `SelectSweepPoisonTest`, `SelectSelfClaimTest`, `ChannelTest`, `ChannelDifferentialTest`, `SchedulerCoreTest`, `IoSlotsTest`, `TimerTest`. Run from `internal/backend/runtime/` (the differential tests resolve `../../../test/corpus/...` relative to CWD — an earlier run from `/tmp` failed all 12 differential tests on `NoSuchFileException`; rerun from the module dir, environmental only).

- **RED baseline** (`logs/red-regression.log`): 96 tests, **1 failure** — exactly the new test.
- **GREEN** (`logs/green-regression.log`): 96 tests, **0 failures**.
- Delta is exactly the new test; no regressions in the select/channel/scheduler blast radius.

## 5. Known residuals / owed work (for the reviewer / resume lane)

1. **Full `SchedTestRunner` suite owed in a worktree** (incl. `GcDifferentialTest`, `GoDebugTest`, `SyncStressTest`, `WakeupRaceStressTest`, `JfrParkCostTest`, `GoPoolTest`). Not runnable here: `/tmp` was 100% full during this run (sibling lanes) and home quota is EDQUOT-exhausted, so no worktree and no temp space for gofrontend-dependent tests. Run `make test` in the worktree on recovery.
2. **Poll-phase ready-beats-poison is unchanged** (deliberate, pre-existing): in phase 1 (`:174-194`), a ready case wins over a poisoned send case; gc scans in random order so it panics only if it encounters the closed send first — i.e. gc itself is order-nondeterministic there. The staged fix covers the finding as filed (mid-sweep poison). Changing the poll would need a random-order scan with poison-checked-during-scan — a separate design decision, not part of S-01.
3. `/dev/null` is still a regular file (68 bytes, 22:24) — the REVIEW-20 anomaly persists; no lane action taken.
4. Quota probe failed EDQUOT at ~22:26 CET (real 8 KB home write, `Disk quota exceeded`): no worktree claimed, no registry entry, no commit, no repo-tree edits. Daily-log append not attempted (same EDQUOT).

## 6. Staged files (`/tmp/rev1107-22-s01/`)

| File | Bytes | Note |
|---|---|---|
| `s01-fix.patch` | 11,150 | sha256 `b0955f0e…b7184`; applies cleanly at HEAD 59f0a247 |
| `EVIDENCE.md` | this file | |
| `RESUME.md` | — | resume steps |
| `logs/red.log` | 658 | RED: `returned:1`, exit 1 |
| `logs/green.log` | 121 | GREEN: PASS, exit 0 |
| `logs/red-regression.log` | 5,237 | 96 tests, 1 failure (the new test) |
| `logs/green-regression.log` | 4,698 | 96 tests, 0 failures |
| `apply-check/` | — | patch-applied files, byte-identical to patch output |
| `pristine/` | — | HEAD copies of `SelectOp.java`, `SchedTestRunner.java` |
| `build/` | — | prior lane's partial repo copy + `SingleTestRunner` (reused, not relied on for classes) |
| `run-verify.sh`, `run-regression.sh` | — | reproducible RED/GREEN + regression drivers |
| `red/`, `green/`, `verify/` | — | overlay class dirs + apply-check workdir |

**Caveat:** `/tmp` is tmpfs — staging is lost on reboot. The patch is the durable artifact; it regenerates the fix from HEAD `59f0a247` (re-verify at the then-current HEAD first).
