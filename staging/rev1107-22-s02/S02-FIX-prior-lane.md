# S-02 remediation — staged fix (review-2026-11-07-07, P1)

Status: STAGED, NOT YET APPLIED. Worktree claim is blocked by the write
wall (see "Blocker" below); apply the patch below in the worktree once
`wt.py new graalgo r1107-07-s02 --chat graalgo-independent-codebase-review`
succeeds. Base: master bf9b96ff.

## Mechanism verification (disk truth, all read from ~/workspace/graalgo)

1. `internal/backend/runtime/src/main/java/graalgo/runtime/Intrinsics.java:215-248`:
   when `elemSizeBytes == 0` the code takes a zero-size fast path that NEVER
   copies old elements (both the reuse branch :240-243 and the fresh-alloc
   branch :247-248). The ARRAY guard exists at :221-226 (bug-042, fail-fast
   `IllegalArgumentException`). STRUCT has NO equivalent check. Review's
   claim is verbatim accurate.
2. `elemSizeBytes(GoKind)` (:429-443): `default -> 0; // structs computed by
   caller; arrays by caller` — STRUCT reports 0, ARRAY reports 0. Confirmed.
3. `structSizeBytes(GoStructDescriptor)` (:461) exists, returns 0 for the
   empty struct and 8 for `struct{x int64}`. Confirmed usable as the guard.
4. Bug-family hunt (do NOT blindly implement — checked all growslice paths):
   - Live V1 frontend (`internal/backend/truffle/.../frontend/NodeFactory.java:3895-3903`)
     computes `structSizeBytes(elemDesc)` for STRUCT append, so the main path
     is safe TODAY; the bug is latent / defense-in-depth, exactly the posture
     of bug-042's ARRAY guard (also a fail-fast for a frontend-computed value).
   - The PUBLIC 3-arg convenience overload (`Intrinsics.java:532-536`)
     passes `elemSizeBytes(elemKind)` (= 0 for STRUCT) with `elemDesc = null`:
     a live trigger surface — any STRUCT caller of that overload silently
     drops old elements today.
   - `NodeFactoryV2.java:1086` builds `AppendNode` via the short constructor
     (elemSizeBytes=0, desc=null) but hardcodes `elemKind = INTERFACE`
     (size 8), so it does NOT hit the zero-size path. Not a STRUCT trigger.
   - `ReflectLinknames.growSlice` is a separate reimplementation that copies
     elements itself; unaffected.
   - `makeslice` has its own path; unaffected.
   - No test anywhere pins the ARRAY guard (`SliceGrowthTest` only tests
     `nextSliceCap`); the family gap is real. A pinning ARRAY test is added
     alongside the STRUCT tests below.
5. `structSizeBytes` edge cases for the guard: `struct{ struct{} }` -> 0
   (genuinely zero-size, must NOT throw); `struct{ a [0]int64 }` -> 0
   (must NOT throw); `struct{x int64}` -> 8 (must throw). A null descriptor
   cannot be verified -> fail fast too (same fail-fast philosophy as ARRAY,
   which throws unconditionally). No live caller passes STRUCT+null+0
   expecting success (only the stale docs snapshot `AppendNode` uses the
   3-arg overload, and no live STRUCT caller does).

## The fix (Intrinsics.java, inside `if (elemSizeBytes == 0) {`, right after the ARRAY guard)

OLD (lines 215-226, exact text):
```java
        if (elemSizeBytes == 0) {
            // P3 bug-042: the zero-size fast path below skips copying old
            // elements — valid only for truly zero-size types. Arrays are
            // never zero-size (even [0]T has identity); a zero elemSizeBytes
            // for ARRAY means the frontend failed to compute the ABI size.
            // Fail fast instead of silently dropping elements.
            if (elemKind == GoKind.ARRAY) {
                throw new IllegalArgumentException(
                        "growslice: ARRAY element with elemSizeBytes=0 (frontend bug)");
            }
```

NEW — insert immediately after the ARRAY guard's closing brace:
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

Notes:
- Placement BEFORE the reuse/alloc branches covers both zero-size
  sub-paths, exactly like the ARRAY guard.
- Cost: one `structSizeBytes` walk per growslice call on the zero-size
  path only; empty-struct case is an empty loop. No hot-path impact
  (non-zero-size elements never enter this branch).
- `structSizeBytes` throws `FrontendException` for malformed descriptors
  (e.g. STRUCT field without a nested descriptor) — also fail-fast, fine.
- No new bridges/special cases: general kind-level guard, mirroring ARRAY.

## Tests (full file staged as ZeroSizeSliceTest.java.new)

Added to `internal/backend/runtime/src/test/java/graalgo/runtime/ZeroSizeSliceTest.java`
(registered in `SchedTestRunner`, so `make -C internal/backend/runtime test`
runs them; no runner change needed). All use the existing `check` helper
plus a new `checkThrowsIAE` helper. Each negative test FAILS before the fix
(no exception thrown) and PASSES after it:

1. `testNonEmptyStructZeroSizeFailsFast` — makeslice(STRUCT, `struct{x int64}`,
   2, 2) then growslice(..., 3, 0, false) must throw IAE. The data-loss repro
   (old elements present).
2. `testNonEmptyStructZeroSizeFailsFastFromNil` — growslice(null, STRUCT,
   non-empty desc, 1, 0, false) must throw IAE.
3. `testStructNullDescZeroSizeFailsFast` — growslice(null, STRUCT, null, 1, 0,
   false) AND the 3-arg overload growslice(null, STRUCT, 1) must throw IAE.
4. `testZeroSizeNestedStructStillFastPath` — `struct{ e struct{} }` (size 0)
   growslice with 0 must NOT throw; len=3 cap=3. Guards against over-firing.
5. `testZeroArrayFieldStructStillFastPath` — `struct{ a [0]int64 }` (size 0)
   growslice with 0 must NOT throw; len=3 cap=3.
6. `testNonEmptyStructGrowsWithCopy` (positive) — makeslice 2/2, growslice
   with correct size 8 to len 3: len 3, cap >= 3, and old slots preserved by
   identity (`grown.array()[0] == s.array()[0]`). Passes before and after;
   guards the fix against breaking the normal path.
7. `testArrayZeroSizeFailsFast` — pins the bug-042 ARRAY guard (no test
   covered it): growslice(null, ARRAY, null, 1, 0, false) must throw IAE.
   Passes before and after.

Expected suite result after fix: all 5 pre-existing ZeroSizeSliceTest tests
still pass + 7 new tests pass. Full verification commands (run in worktree):
`make -C internal/backend/runtime test`, then `make test-ci` and
`make snapshot-check` from the repo root.

## Blocker (exact failing writes)

- `wt.py new graalgo r1107-07-s02 --chat graalgo-independent-codebase-review`
  fails in `save_registry` (`json.dump` -> `fp.write`): `PermissionError:
  [Errno 1] Operation not permitted` writing `registry.json.tmp`
  (registry is 465,307 bytes). Retried 07:33Z and 07:47Z.
- `git worktree add --detach /tmp/.wt-probe-s02 HEAD` fails: `fatal: could
  not write to '.git/worktrees/-wt-probe-s02/locked': Operation not permitted`.
  No partial dir left behind (verified).
- `git hash-object -w` (home repo) fails: `fatal: unable to write loose
  object file: Operation not permitted` — even with GIT_OBJECT_DIRECTORY
  pointed at /tmp, and even while `head -c 465307` to /tmp succeeds in the
  same second. `git status` fails on `.git/index.lock` write.
- Root cause (probed 07:33-07:47Z): the `python3.12` binary cannot write to
  ANY regular file in this exec sandbox (fails even 1 byte via raw
  `os.open`+`os.write`, even to an inherited bash-opened fd, even after a
  60s sleep), while `head`/`tee`/`dd`/`perl`/`git init` (in /tmp) and
  `muse.write`/`muse.edit` CAN write (flapping for exec-spawned shell
  tools). So every python-based tool (`wt.py`, `workreg.py`) is
  write-blocked, and git writes to the home repo are blocked; file-tool
  writes work everywhere. Workaround used: work-registry entry written via
  `muse.write` (`~/workspace/system/work-registry/r1107-07-s02.json`);
  fix+tests staged here via `muse.write`.
- Nothing was faked: no worktree claimed, no branch created, no file in the
  repo modified, no test run (no build possible without a worktree; a
  scratch build was deliberately NOT attempted — too much flap-dependent
  machinery for a lane whose brief allows BLOCKED with staged evidence).

## Resume instructions for the successor/integrator

1. Retry `wt.py new graalgo r1107-07-s02 --chat graalgo-independent-codebase-review`
   until the python write block clears.
2. In the worktree, apply `Intrinsics.java.patch` (or the old/new blocks above
   via `muse.edit`) and replace `ZeroSizeSliceTest.java` with
   `ZeroSizeSliceTest.java.new` (verify byte counts after copy).
3. Run: `make -C internal/backend/runtime test` (SchedTestRunner),
   `make test-ci`, `make snapshot-check`. Expect: 5 old + 7 new
   ZeroSizeSliceTest tests green; no snapshot drift.
4. Commit with explicit pathspecs (Intrinsics.java + ZeroSizeSliceTest.java
   only). Do NOT integrate — coordinator owns integration + independent
   re-review of this remediation.
5. Mark work-registry entry `r1107-07-s02` done when committed (or update its
   checkpoint with the commit SHA).
