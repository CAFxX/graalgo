# S-02 remediation — rev1107-22-s02 (REVIEW-2026-11-07-22, P1)

Status: **BLOCKED-quota. Fix + tests STAGED here, NOT applied to any repo,
no worktree claimed, no branch created, no commit, no test run.**
Base: master `59f0a247` (verified HEAD at 2026-11-07 ~22:12 CET).
Lane: `rev1107-22-s02` (re-dispatch of morning lane `r1107-07-s02`).
Registry id suggestion: `graalgo-rev1107-22-s02` (no entry written — quota).

## 1. Prior-lane verification (recovery, not duplication)

Checked before starting anything (all match the dispatch brief):

- Registry `~/workspace/system/work-registry/r1107-07-s02.json`: status
  `active`, heartbeat `2026-11-07T07:53:00+01:00` (~14h stale at lane
  start), checkpoint `BLOCKED 2026-11-07 ~07:52Z` — fix + 7 tests staged at
  `~/workspace/s02-staging/`, no worktree, no branch, no test run.
- Branch `wt/graalgo-independent-codebase-review/r1107-07-s02`: exists at
  base `bf9b96ff`, **no commits**.
- `git worktree list | grep s02`: no match — **no worktree**.
- `ps`: no live s02 agent/process (only unrelated sync + r12-runner
  processes). Prior lane is NOT alive. Proceeding under `rev1107-22-s02`
  is correct; no duplication.
- Prior staging survives with exact byte counts as its checkpoint claims:
  `Intrinsics.java.patch` 2193 B, `S02-FIX.md` 9748 B,
  `ZeroSizeSliceTest.java.new` 9395 B (copied here as
  `S02-FIX-prior-lane.md` / `ZeroSizeSliceTest.java.new`).

## 2. Mechanism re-verified from disk truth at HEAD 59f0a247

Read-only reads in `~/workspace/graalgo` (nothing in the main tree touched;
`git status` clean, HEAD `59f0a247088f0e0b6db7e0623076866727dd873c`).

1. `Intrinsics.java:215-248`: `elemSizeBytes == 0` takes the zero-size fast
   path, which NEVER copies old elements (reuse branch :240-243, fresh-alloc
   branch :247-248). ARRAY guard exists :221-226 (bug-042). STRUCT has NO
   equivalent check. Confirmed — finding is verbatim accurate.
2. `elemSizeBytes(GoKind)` :429-443: `default -> 0; // structs computed by
   caller; arrays by caller` — STRUCT reports 0. Confirmed.
3. `structSizeBytes(GoStructDescriptor)` :461: returns
   `structSizeAndAlign(desc)[0]`; empty struct -> 0 (offset 0, maxAlign 1);
   `struct{x int64}` -> 8; `struct{e struct{}}` -> 0; `struct{a [0]int64}`
   -> 0 (0 * 8, padded to align 8). Verified by reading the computation.
   Usable as the guard. Confirmed.
4. Live V1 frontend (`NodeFactory.java:3895-3903`): for STRUCT append,
   `elemSizeBytes = Intrinsics.structSizeBytes(elemDesc)` — the main path is
   safe TODAY. The bug is latent / defense-in-depth, exactly the posture of
   bug-042's ARRAY guard. Confirmed.
5. Public 3-arg convenience overload (`Intrinsics.java:532-536`) passes
   `elemDesc = null` with `elemSizeBytes(elemKind)` (= 0 for STRUCT): a
   LIVE trigger surface — any STRUCT caller silently drops old elements.
   Only current callers are the stale docs snapshot
   (`docs/arch/reviews/phase1-m5-loopcap/snapshot/java/frontend/AppendNode.java:37`)
   and tests; no live STRUCT caller today. Confirmed.
6. `NodeFactoryV2.java:1086` hardcodes `elemKind = INTERFACE` (size 8 via
   `effectiveElemSize()`), so it never enters the zero-size path. NOT a
   STRUCT trigger. Confirmed.
7. `AppendNode.effectiveElemSize()` (:66-68) returns the frontend-supplied
   size when non-zero, else the kind's natural size — for V1 STRUCT appends
   the supplied size is the real struct size. Confirmed.
8. No other live `growslice` caller passes STRUCT + 0 (grep over
   `internal/backend` + repo: only `AppendNode.java` 6-arg calls, the test
   file, and `ReflectLinknames` which reimplements growth itself).
9. No test anywhere pins the ARRAY guard (`SliceGrowthTest` tests only
   `nextSliceCap`). Family gap is real.

## 3. The fix (this lane, applied to the /tmp copy only)

File: `Intrinsics.java.fixed` (HEAD 59f0a247 + guard, 35062 B; HEAD copy
`Intrinsics.java.HEAD-59f0a247` is 34027 B; `diff` shows ONLY the 16-line
insertion at :225-240, immediately after the ARRAY guard, before both
zero-size sub-paths — same placement as the ARRAY guard).

```java
            // S-02 (review-2026-11-07-07): same bug class for STRUCT. A
            // struct also reports elemSizeBytes == 0 (elemSizeBytes returns
            // 0 for STRUCT by design; the caller supplies the ABI size),
            // so a zero elemSizeBytes for a non-empty struct would take
            // this path and silently drop all existing elements on growth.
            // Verify via the descriptor that the struct is genuinely
            // zero-size; a null descriptor cannot be verified, so it fails
            // fast too (the 3-arg convenience overload passes null — its
            // STRUCT callers previously dropped data silently).
            if (elemKind == GoKind.STRUCT) {
                if (elemDesc == null || structSizeBytes(elemDesc) != 0) {
                    throw new IllegalArgumentException(
                            "growslice: STRUCT element with elemSizeBytes=0 but non-zero or "
                                    + "unknown ABI size (frontend bug)");
                }
            }
```

No new bridges/special cases: general kind-level guard mirroring ARRAY.
Cost: one `structSizeBytes` walk per growslice call on the zero-size path
only; non-zero-size elements never enter the branch. `structSizeBytes`
throws `FrontendException` for malformed descriptors — also fail-fast.

## 4. NEW FINDING — the prior lane's patch file is corrupt (do NOT use it)

`~/workspace/s02-staging/Intrinsics.java.patch` (2193 B) is TRUNCATED:
`git apply --check` fails `error: corrupt patch at line 35` — the hunk
declares `@@ -215,11 +215,26 @@` but the trailing context lines are cut
off after the inserted block (file ends mid-hunk, 34 lines, no trailing
context). The FIX TEXT inside it is correct (identical to §3 above), but
the patch artifact itself cannot be applied. This lane REGENERATED a valid
patch — `Intrinsics.java.patch` (1617 B) — verified below. The prior lane's
`S02-FIX.md` and `ZeroSizeSliceTest.java.new` are unaffected (text + test
file verified independently, see §5).

## 5. Tests (staged, verified by reading — NOT run; no worktree/build possible)

`ZeroSizeSliceTest.java.new` (9395 B) vs HEAD test file (3405 B, 79 lines):
purely additive — 119 added lines, 0 removed. Adds 7 tests + `checkThrowsIAE`
helper + 3 descriptors (`NONEMPTY_DESC`, `NESTED_EMPTY_DESC`,
`ZERO_ARRAY_DESC`). Verified:

- All three descriptors use valid `GoStructDescriptor.layout` overloads
  (3-arg, 5-arg, 7-arg — all exist in
  `internal/backend/truffle/.../values/GoStructDescriptor.java:76-131`).
- `GoStruct.zero(descriptor)` allocates a FRESH instance per call
  (`new Object[...]` per invocation), so `Boxing.newArray` fills each
  STRUCT slot with a distinct instance — the positive control
  `testNonEmptyStructGrowsWithCopy`'s identity check
  (`grown.array()[0] == s.array()[0]`) is MEANINGFUL, not vacuous: without
  the copy, grown's slots would be fresh instances. (This was checked
  explicitly because a shared-zero-singleton would have made the test
  tautological.)
- Negative tests (non-empty struct, from nil, null desc, 3-arg overload)
  FAIL before the fix (growslice returns a slice, dropping elements) and
  PASS after it; precision tests (nested-empty, zero-array-field) must NOT
  throw; ARRAY pin passes before and after.
- Test file is registered in `SchedTestRunner` (per prior lane; no runner
  change needed) — re-verify at resume time.

RED→GREEN status: **NOT demonstrated** — no build/test run was possible
(quota blocks worktree claim; see §6). The RED leg is established by
reading (the negative tests call a path that today returns normally), but
the suite has not been executed. The resume lane MUST run the suite and
record actual RED→GREEN evidence; do not claim it from this staging.

## 6. Blocker (exact failing write)

- Home quota probe, 2026-11-07 ~22:12 CET: real 8 KB write to
  `~/workspace/.quota-probe-rev1107-22-s02` failed
  `[Errno 122] Disk quota exceeded`. Probe file was NOT left behind (the
  write itself failed; nothing to remove).
- `df`: `/home/hatch` 76% used (150G, 36G avail — quota, not disk, is the
  limit); `/tmp` 483M free (6% used); `/var/tmp` 100% full (1.5M free).
- Load at lane start: CLEAR (`load-shed.sh backoff` exit 0, load 4.79, no
  frozen processes). No heavy work was dispatched (no build/test possible).
- Consequence per brief: NO worktree claimed, NO registry entry written,
  NO commit, NO repo file modified. All staging is in /tmp (writable).
- The prior lane's write wall (python3.12 unable to write regular files)
  was NOT re-tested as a separate condition: the EDQUOT failure alone
  fully explains the block, and home writes fail regardless of tool.

## 7. Resume instructions (for the successor / integrator)

1. Wait for home quota recovery, then re-probe with a real 8 KB home write
   (byte-count verified) BEFORE claiming anything.
2. Register registry id `graalgo-rev1107-22-s02` (suggested) and claim a
   worktree: `wt.py new graalgo rev1107-22-s02 --chat
   graalgo-independent-codebase-review`. NOTE: the prior lane's branch
   `wt/graalgo-independent-codebase-review/r1107-07-s02` exists at base
   `bf9b96ff` with no commits — the new stream uses a FRESH branch; do not
   reuse the stale branch name (and do not touch the stale registry entry
   `r1107-07-s02` except to mark it superseded/done when this lane lands,
   per coordinator decision).
3. In the worktree, apply `Intrinsics.java.patch` from THIS directory
   (the REGENERATED one, 1617 B — NOT the prior lane's 2193 B corrupt
   file) and replace `ZeroSizeSliceTest.java` with `ZeroSizeSliceTest.java.new`
   (verify byte counts after copy: patch 1617 B, test file 9395 B,
   fixed Intrinsics.java 35062 B).
4. Run, in order (each under `nice -n 10`, backoff-gated):
   `make -C internal/backend/runtime test` (SchedTestRunner),
   then `make test-ci` and `make snapshot-check` from the repo root.
   Expect: 5 pre-existing + 7 new ZeroSizeSliceTest tests green; no
   snapshot drift. Record actual RED→GREEN evidence: first run the new
   tests against the UNFIXED tree (expect the 3 negative STRUCT tests to
   fail), then apply the fix and re-run (expect all green).
5. Commit with explicit pathspecs (Intrinsics.java +
   ZeroSizeSliceTest.java only). Do NOT integrate — coordinator owns
   integration + independent re-review of this remediation.
6. Mark registry entry `graalgo-rev1107-22-s02` done when committed (or
   update its checkpoint with the commit SHA); mark the stale
   `r1107-07-s02` entry superseded.
7. House rules: `~/workspace/graalgo/AGENTS.md` read this run (§1-7:
   master integration-only, worktree discipline, correctness contract,
   generality rule, no-debris, nice everything). `make all` is the
   per-push gate; any behavior change must pass `make snapshot-check`
   with no drift.

## 8. Staged files in this directory (/tmp/rev1107-22-s02/)

- `Intrinsics.java.patch` (1617 B) — REGENERATED valid patch; `git apply
  --check` OK; applied file byte-identical to `Intrinsics.java.fixed`
  (verified in `applytest/` scratch tree, since removed from the
  verification path — the `patchwork/` + `applytest/` dirs remain here
  for audit).
- `Intrinsics.java.fixed` (35062 B) — HEAD 59f0a247 + §3 guard.
- `Intrinsics.java.HEAD-59f0a247` (34027 B) — pristine HEAD copy.
- `ZeroSizeSliceTest.java.new` (9395 B) — full new test file (prior lane's,
  verified §5; identical bytes to `~/workspace/s02-staging/` copy).
- `ZeroSizeSliceTest.java.HEAD-59f0a247` (3405 B) — pristine HEAD test file.
- `S02-FIX-prior-lane.md` (9748 B) — prior lane's fix doc (mechanism +
  resume; its patch artifact is superseded by the regenerated one, §4).
- `VERIFICATION.md` (this file) — this lane's evidence + resume note.
- `patchwork/` — intermediate diff scaffolding (audit only).
- `applytest/` — scratch tree where the regenerated patch was
  apply-checked and verified byte-identical (audit only).
