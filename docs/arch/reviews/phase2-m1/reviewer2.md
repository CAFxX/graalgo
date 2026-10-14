# P2-M1 Review — Reviewer 2 (methodology, testing, evidence)

- **Snapshot:** `6995182` ("P2-M1: checkpoint implementation evidence"), branch `master`, repo `~/workspace/graalgo`. Verified via `git log --oneline -1 6995182`. All four commits named in CHECKPOINT.md (`26b5d8b`, `2e5f012`, `b993df7`, `8030950`) exist with the described subjects.
- **Scope:** `phases/phase2/src/test/java/graalgo/sched/SchedulerCoreTest.java` (10 tests), `phases/phase2/README.md`, `phases/phase2/CHECKPOINT.md` (P2-M1 tail entries), and the checkpoint's evidence claims (10/10 unit, 9/9 differential, preemption at `--gmp=1`, Phase-1 208/0).
- **Method:** read-only review against a clean worktree (`git worktree add /tmp/p2m1-r2 6995182`; repo tree never modified). Evidence gathered by:
  1. `make -C phases/phase2 test` in the worktree, 8 full runs (first run: 9/10).
  2. Seven targeted mutations (M1–M7) compiled into full-tree copies and run per-test with a timeout harness: M1 neuter quantum check; M2 drop unpark-before-park stash; M3 swap lane order; M4 neuter `checkInvariants`; M5 reverse ack/unpark order (3 runs); M6 drop `parkWakeAction` execution; M7 no-op `awaitQuiescence`.
  3. Flake forensics: instrumented `SchedulerCore.spawn` wrapper to record throwables; reproduced the 10k failure and established the mechanism arithmetically (see P0-1).
  4. Differential spot-check: 3 corpus programs (`01_hello`, `02_arithmetic`, `36_matrix_mult`) through Phase-1 `Main` vs Phase-2 `SchedMain --gmp=1`, byte-compared.
  5. Preemption capability: wait-loop Go program at `--gmp=1` on the clean build (completes) vs a quantum-neutered build (hangs, killed by timeout).
  6. `make -C phases/phase1 test` re-run in the worktree: **208 tests, 0 failures**; `make check` green in both modules.
  7. Cross-checked every `verifies: GG-XXXX` annotation against the locked requirement statements in `requirements/GG-*.yaml`, and README/CHECKPOINT prose against the code.

- **Verdict: GO-WITH-FINDINGS.** The scheduler is genuinely red-green on its core properties (every targeted break of quantum, cap, wakeup-pairing, lane order, and quiescence-wait is caught), differential parity holds, and the preemption capability is real on the production path. But P0-1 is a real scheduler bug behind a flaky acceptance test: the "10/10 green" evidence did not reproduce on my first run, and the acceptance criterion it rests on ("quiescence probe green at 10k") is not reliably met. **P0-1 must be fixed and the suite re-run green several times before sign-off.** P1 items are coverage/evidence gaps that should be closed in the remediation pass; P2 items are doc/traceability nits.

## Findings

### P0-1 — `awaitQuiescence` fires on transient quiescence: `testQuiescenceAt10k` is flaky and the API contract is broken

**What's wrong.** `SchedulerCore.awaitQuiescence()` waits on a one-shot `CountDownLatch` that is counted down when `liveGoroutines.decrementAndGet() == 0`. If the spawning thread is still issuing spawns, the live count can hit **0 transiently** mid-loop (early-spawned trivial goroutines drain before later spawns are issued). The latch fires and can never un-fire; the post-loop `awaitQuiescence()` then returns immediately with thousands of goroutines still running.

**Evidence.** First `make test` run in the worktree: `FAIL testQuiescenceAt10k: expected=10000 actual=620`. A later run: `actual=870`. Instrumented rerun showed `done=295, runnableCount=9705`, **zero** throwables escaping the spawn wrapper, and `done + runnable == 10000` exactly — the only consistent account is a mid-loop transient zero firing the latch (295 decrements cannot return 0 from a count of 10000 unless the other 9705 increments hadn't happened yet). 2 of 8 full-suite runs failed this way (~25%; 5/5 isolated single-test runs passed — the flake is timing-sensitive, worse on a warmed-up JVM). `testSpawnAndJoin` carries the same latent race (it just rarely loses at n=50).

**Why it matters.** (1) The P2-M1 acceptance criterion "quiescence probe green at 10k goroutines" is not reliably satisfied, and the checkpoint's "10/10 green" claim is not robustly reproducible — I could not reproduce it on my first attempt. (2) It is a scheduler bug, not just a test bug: the javadoc promises "Blocks until every spawned goroutine has finished", which is false under the canonical spawn-loop-then-await usage. (SchedMain's own usage is safe in practice — the launcher thread spawns once and the live main goroutine keeps the count ≥ 1 — but the API is a trap for any non-goroutine spawner.)

**Concrete fix.** Replace the one-shot latch with a monitor + re-check loop: `awaitQuiescence()` waits while `liveGoroutines.get() > 0` with `notifyAll()` on every decrement, all under one lock; a transient zero with no waiter present is then harmless. Document the residual contract: the caller must guarantee no concurrent spawns *while awaiting* (inherently racy otherwise — only live goroutines or the awaiting thread can spawn, so state this). Add a regression test that fails on the old code: spawn N trivial goroutines in a tight loop on the test thread with a small cap and assert `done == N` after await, run in a loop of several iterations (the old latch fails it within a few full-suite runs; alternatively inject a slow-spawn to deterministically force the transient zero).

### P1-2 — `parkCurrent(Runnable onWake)`: the wake action has zero test coverage

**What's wrong.** `unpark()` runs `h.parkWakeAction` on the waker thread before enqueueing the WAKE token. Mutation M6 (delete `action.run()`, keep the null-out) leaves **all 10 tests green**. No test parks with a non-null action, so a silently dropped wake action — the kind of bug that would break every M2 channel waiter built on this hook — is invisible to the suite.

**Why it matters.** `parkWakeAction` is the documented handoff point for blocking-op wakeups (the M2 channel design will depend on it). Untested callback execution is exactly the class of bug the "tests must be capable of failing" bar exists to catch.

**Concrete fix.** Add `testParkWakeActionRuns`: spawn a goroutine that `parkCurrent(action)` where action appends to a list; wait for armed; `unpark(h)`; assert the action ran **before** the goroutine resumed (e.g., action records a nanoTime, goroutine records its resume nanoTime, assert ordering). Red-green check: M6 must fail it.

### P1-3 — Preemption evidence as recorded is ambiguous and unauditable

**What's wrong.** CHECKPOINT.md: "Preemption: 2-goroutine busy-loop completes at `--gmp=1` (impossible without quantum expiry), output matches gc." Whether completion proves preemption depends entirely on the program shape, which is neither described nor archived:
- *Wait-loop shape* (`go func(){ boundedWork(); stop=true }(); for !stop {}`): completion genuinely requires preemption — I verified this shape completes at `--gmp=1` on the clean build (97s, prints `done`) and **hangs** on the quantum-neutered build (killed by timeout, exit 124). Valid proof.
- *Bounded/bounded shape* (`go spin(); spin()`): completes with or without preemption (main finishes, frees the slot, worker runs). "Output matches gc" does not discriminate either — gc prints `done` for both shapes.
"No such program exists in the repo; there is no run log. A reviewer cannot determine which shape was used, so the claim is unfalsifiable as recorded.

**Why it matters.** This is the milestone's headline preemption evidence. Unverifiable evidence is not evidence; the next reviewer (or the program owner at gate review) cannot re-check it.

**Concrete fix.** Archive the demo program (use the wait-loop shape) plus a one-paragraph rationale of *why* that shape is a valid falsifier (main's wait loop can only terminate via the worker, which can only run via preemption at cap=1), and a run log, under `phases/phase2/` (e.g. `evidence/`). Re-record the checkpoint bullet to name the program and its shape.

### P1-4 — `yieldCurrent()` (public API, frozen-surface wiring) has no test

**What's wrong.** `SchedulerCore.yieldCurrent()` is public and is the body of the frozen `graalgo.runtime.Scheduler` replacement's yield path (`Scheduler.java:34`), i.e. the exact method M2+ guest code will call. Zero tests reference it (grep). A `yieldCurrent` degraded to a no-op would pass the entire suite.

**Why it matters.** Same argument as P1-2: public scheduler API on the frozen cross-module surface with no red-green coverage.

**Concrete fix.** Add `testYieldCurrentParks`: cap=1, spawn a goroutine that calls `sched.yieldCurrent()` in a loop until a flag is set, spawn a one-shot that sets the flag; assert the one-shot runs (it can only be admitted if the yielder actually parks). Red-green check against a no-op `yieldCurrent`.

### P1-5 — `verifies: GG-SCHED-0001` on `testSpawnAndJoin` / `testQuiescenceAt10k` is inaccurate

**What's wrong.** GG-SCHED-0001's locked statement is: "On quantum expiry at a Safepoint.poll() …, the virtual thread SHALL park indefinitely via LockSupport.park() and enqueue a resume token…; the default quantum SHALL be 10ms." Neither test exercises quantum expiry at all — they test spawn/admission and quiescence scale. Per PLAN.md's activation policy, a GG ID goes active "exactly when its first real test lands" — the tests that genuinely exercise GG-SCHED-0001's falsifier are `testQuantumExpiryPreempts` and `testSafepointPollWiring`. Spawn/admission as such has no locked SHALL among the five WS-A IDs (closest is GG-SCHED-0002's drainer admission discipline).

**Why it matters.** Requirement traceability: a reader auditing "GG-SCHED-0001 verified by" gets pointed at tests that cannot falsify the requirement. If those were ever the *only* tests, the ID would be marked active without ever being tested.

**Concrete fix.** Re-annotate `testSpawnAndJoin`/`testQuiescenceAt10k` (e.g. `verifies: GG-SCHED-0002` for the admission-discipline aspect, or no GG annotation with a note), leaving GG-SCHED-0001's annotation on the two preemption tests.

### P2-6 — `testAckBeforeUnparkStress` detects the reversed ack/unpark order only probabilistically

**What's wrong.** Mutation M5 (unpark *before* `acked.set`, reversing GG-WAKE-0001's mandated order) was caught in 2 of 3 stress runs (`cycle 29: wakeup lost`, `cycle 98: wakeup lost`) but passed once. The class javadoc claims "every test fails against a broken scheduler … (lost wakeups)" without qualification. The pairing logic is well covered; the *ordering* SHALL ("the acked flag SHALL be set before unpark") has no deterministic test — the 200-cycle stress wins the race most runs, not every run.

**Why it matters.** A single green run does not prove the ordering is correct; the red-green claim overstates what the test guarantees.

**Concrete fix.** Qualify the javadoc ("probabilistically: reversed order fails most runs"). If a deterministic ordering test is feasible (e.g. a white-box `admit`-order unit test asserting `acked` is set before `unpark` is invoked, via a listener hook), add it; otherwise record the probabilistic nature explicitly.

### P2-7 — `GoroutineHandle.armed` javadoc describes a protocol that doesn't exist

**What's wrong.** `GoroutineHandle.java:31`: "A waker wins generation `t` via `armed.compareAndSet(t, -2)`." No such CAS exists anywhere in the codebase — `armed` is only ever `set` (in `armPark`) and `get` (in tests). In production it is write-only: a test-observability hook, not protocol state. The tests' "wait until `armed >= 0`" pattern leans on a field whose documented protocol is fictional.

**Why it matters.** Doc/code contradiction in the concurrency protocol — the exact place where stale docs cause the next bug (someone will "rely on" the CAS that isn't there).

**Concrete fix.** Rewrite the `armed` javadoc to describe reality: set once per park reservation before the waiter becomes visible; read by tests to observe "goroutine reached parkCurrent". Delete the `compareAndSet(t, -2)` sentence.

### P2-8 — README intro vs protocol section disagree on the cap

**What's wrong.** README intro: "in-flight cap = `GOMAXPROCS`." Protocol section (correctly, matching `SchedulerCore`): "Cap = min(GOMAXPROCS, carrier parallelism) (P2-D6)."

**Concrete fix.** Align the intro with the protocol section.

### P2-9 — "9/9 differential" names no programs and archives no script

**What's wrong.** The checkpoint claims 9/9 single-goroutine corpus programs byte-identical Phase-1 vs `--gmp=1`, but the 9 programs are unnamed and no differential script/log is archived. I spot-checked 3 (`01_hello`, `02_arithmetic`, `36_matrix_mult` — the last exercises loop back-edge `Safepoint.poll` on the scheduler path): all byte-identical, so the claim is *plausible*, but only partially re-verifiable.

**Concrete fix.** Name the 9 programs and archive the differential script + outputs (same treatment as P1-3).

### P2-10 — GG-SCHED-0002's non-terminating drainer has no test; minor untested surface

**What's wrong.** GG-SCHED-0002 explicitly requires: "The drainer SHALL be non-terminating: it SHALL NOT exit on InterruptedException (swallow-and-continue…)". The `drainLoop` catch-and-continue has no test (the drainer thread is private; no hook exists). Likewise untested: `SchedulerCore.current()` fail-fast when nothing installed, the `min(GOMAXPROCS, carriers)` clamp, `spawnGoFunc` null-check, `start()` idempotency.

**Why it matters.** A SHALL with no test is a SHALL on paper only; the interrupt-swallow is precisely the kind of "obviously correct" code that regresses silently.

**Concrete fix.** Add a package-visible test hook exposing the drainer thread (or an interrupt-injection point), interrupt it mid-run, assert draining continues. Add small unit tests for the `current()` fail-fast and the cap clamp.

## What checked out clean

1. **Red-green honesty (core properties):** M1 (no quantum check) → `testQuantumExpiryPreempts` fails with "one-shot goroutine starved" ✓; M2 (drop unpark-before-park stash) → `testUnparkBeforePark` fails with "unpark-before-park was not lost" ✓; M3 (lane swap) → `testDrainerLaneOrder` fails with "WAKE before INIT" ✓; M4 (neutered `checkInvariants`) → `testCapExceededHardAsserts` fails ✓; M7 (no-op `awaitQuiescence`) → both quiescence tests fail ✓. The suite is not tautological on any of these axes.
2. **Differential parity:** 3/3 spot-checked corpus programs byte-identical Phase-1 vs `--gmp=1`.
3. **Preemption capability:** real on the production path (wait-loop shape completes at `--gmp=1`, hangs with quantum neutered).
4. **Phase-1 208/0 after exporter change:** independently re-run in the worktree — 208 tests, 0 failures. `make check` green in both modules.
5. **Doc/code consistency:** README's protocol section matches the implementation (quantum/park token, lane order wake>init>run, ack-before-unpark, cap formula); all checkpoint commits exist with the stated subjects; the evidence bullet list accurately describes the 10 tests.

## Cleanup

Worktree `/tmp/p2m1-r2` removed via `git worktree remove --force`; scratch dirs `/tmp/dbg`, `/tmp/preempt` deleted; `/tmp/p1suite.log` left in /tmp (ephemeral). No files in `~/workspace/graalgo` were modified — the only deliverable is this report.
