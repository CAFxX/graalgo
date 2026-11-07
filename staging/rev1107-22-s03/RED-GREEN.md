# S-03 — RED→GREEN evidence (lane rev1107-22-s03, 2026-11-07 ~22:15 CET)

## Mechanism re-verified from disk truth (HEAD `59f0a247`, main repo, read-only)

- `GoPool.local()` (`GoPool.java:120-128` at HEAD) adds every calling
  thread's `Local` to the strong `IdentityHashMap`-backed `allLocals`
  set and never removes it. No removal path existed anywhere in the file.
- `SchedulerCore.spawn` (`SchedulerCore.java:367`) creates one
  `Thread.ofVirtual()` per goroutine, so one `Local` is retained per
  goroutine ever touching a shared `sync.Pool`.
- The class doc's claim ("bounded in practice by the scheduler's
  carrier pool") was wrong: carriers are bounded, goroutines are not.
- `get()`'s slow path iterated `allLocals` twice under the pool lock,
  so slow-path cost grew linearly with total goroutines ever.
- See `VERIFICATION.md` for the full disk-truth notes.

## RED (original code, no fix)

Probe `src/RedProbe.java` (reflection read of private `allLocals`,
changes nothing) against the original `GoPool.java`:

```
RED-PROBE registeredLocals=200 (putters=200)
RED-PROBE drained=200
RED-PROBE verdict: BUG REPRODUCED — registry retains 200 entries for 200 dead threads
```
exit 1. Full log: `evidence/red-probe.log`. (200 short-lived virtual
threads, one `put` each, all joined before measuring.)

The new unit test `testDeadThreadLocalsAreReaped` cannot compile
against the original code (it uses the fix's package-private
`registeredLocalsForTest()` accessor), so the probe is the RED leg
for the registry-size assertion; the multiset half is covered by the
pre-existing `testCrossThreadSteal` / `testConcurrentNoLoss`.

## GREEN (fixed code)

`GoPoolTest` (6 pre-existing + 1 new test), reflective runner
`src/RunPoolTests.java`, GraalVM JDK 25.0.4, `javac -Xlint:deprecation
-Werror` (the module's pinned flags) — compiles clean. Suite run
3x during development + 1x on the final files:

```
PASS testPutGetRoundTrip
PASS testPutNilNoOp
PASS testNewFuncSlot
PASS testCrossThreadSteal
PASS testDeadThreadLocalsAreReaped
PASS testConcurrentNoLoss
PASS testSpillBounded
passed=7 failed=0
```
Full logs: `evidence/green-suite.log` (3 runs), `evidence/green-suite-final.log`.

Same probe against the fixed code (`evidence/red-probe-fixed.log`):

```
RED-PROBE registeredLocals=9 (putters=200)
RED-PROBE drained=200
RED-PROBE verdict: bounded
```
exit 0. The 9 is the pre-drain straggler count (threads alive at the
last registration reap); the drain's slow-path reap drops it to 1,
which is what `testDeadThreadLocalsAreReaped` asserts — all 200
items still drain, none lost to reaping.

## What the fix does (staged files at this directory's top level)

`GoPool.java` (diff: `evidence/GoPool.diff`):

1. `Local` records its owner as a `WeakReference<Thread>` (strong
   refs would keep dead goroutines' `Thread` + `ThreadLocal` map
   alive — the very leak being fixed). Dead = cleared ref or
   `!isAlive()`; a dead owner can never touch its Local again and
   thieves only touch it under the pool lock, so draining under the
   lock is race-free.
2. `reapDeadLocalsLocked()`: drops dead owners' Locals, draining
   their `private` + `shared` items into the global queue (same
   victim-overflow discipline as `put`'s spill), so dead threads'
   items stay retrievable — the existing `testCrossThreadSteal`
   semantics are preserved, not weakened.
3. Reap triggers: every slow-path `get` (unconditional — the slow
   path already scans the registry twice); every 1024th pool op per
   thread via an owner-confined counter (no lock, no volatile on the
   fast path); amortized on registration (at most one scan per 64
   registrations once the set is ≥ 64).
4. Class doc corrected: the registry is bounded by live threads
   plus threads that died since the last reap.
5. Package-private `registeredLocalsForTest()` accessor for the test.

`GoPoolTest.java` (diff: `evidence/GoPoolTest.diff`): new
`testDeadThreadLocalsAreReaped` — 200 short-lived virtual threads
put one item each and die; the drain recovers all 200 items and the
registry holds only the draining thread's Local (== 1).

Public API unchanged (`put`/`get`/`getNewFunc`/`setNewFunc`); the
only callers (`SyncOpNode`, `SyncNewNode`, `NodeFactory`) are
unaffected. `put()`'s spill path is byte-identical.

## Honest residual (documented in the code, `reapDeadLocalsLocked` doc)

A pool that is never touched again after its goroutines die keeps
their Locals until next touched or collected — no pool operation
runs to reap them, and the lock-free fast path is not polled for
that. Any subsequent pool operation by a live thread reaps them
(slow-path get, 1024th-op check, or registration). A background
reaper (e.g. `Cleaner` on the owning thread) would close this but
would retain the pool itself while any touching thread lives and
adds a JVM background thread; judged not worth it for a
never-touched-again pool. The review's scenario (a live pool used
by 1M short-lived goroutines) is fixed: the next touch reaps all.

## NOT done (quota-blocked — see RESUME.md)

- No worktree claimed, no registry entry, no commit (home EDQUOT).
- The module suite (`make test-ci` / runtime tests via the module
  Makefile) and the differential `snapshot-check` were NOT run —
  only the standalone `javac` + `GoPoolTest` evidence above, in
  `/tmp`. The resume lane must run the full affected suite in the
  worktree before reporting done.
