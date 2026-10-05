# Reviewer 1 (code correctness) — graalgo Phase-2 M1 (scheduler core)

- **Snapshot:** `6995182` ("P2-M1: checkpoint implementation evidence"), branch `master`, repo `~/workspace/graalgo`. Reviewed in a read-only worktree (`/tmp/p2m1-r1`, since removed); the repo tree was not modified.
- **Scope:** `phases/phase2/src/main/java/` in full — `graalgo/sched/` (`SchedulerCore`, `Drainer`, `GoroutineHandle`, `ResumeToken`), `graalgo/sched/nodes/GoStatementNode.java`, `graalgo/sched/launcher/SchedMain.java`, and the three same-FQN replacements (`graalgo/safepoint/Safepoint.java`, `graalgo/runtime/Scheduler.java`, `graalgo/runtime/GoroutineState.java`); the Phase-1 exporter diff `git show 2e5f012`; design refs `phases/phase2/PLAN.md` §2 WS A and `spikes/02b-userspace-scheduler/REPORT.md` §1.
- **Method:** line-by-line read of all 9 files; built the snapshot and ran `make test` repeatedly (observed a flaky failure — see P0-1); wrote targeted repro harnesses for the quiescence race, the park/unpark wake-action contract, and the spawned-goroutine panic exit code; ran an end-to-end `SchedMain` program; `javap` signature-diffed the three overlay classes against Phase-1 at `6995182~5`; interleaving analysis of every park/unpark/admission path.

## Verdict: NO-GO

Two P0 findings block signoff: (1) the quiescence primitive can return while thousands of goroutines are still live — the milestone's own 10k acceptance probe is flaky by construction and `CHECKPOINT.md`'s "10/10 green" claim does not hold; (2) the `parkCurrent`/`unpark` wake-action contract is deterministically violated on unpark-before-park, with the documented CAS ticket discipline not actually implemented. Five P1s and one P2 below.

---

## P0 findings (must fix before signoff)

### P0-1. `awaitQuiescence` can return while goroutines are still live (one-shot latch)

**Location:** `graalgo/sched/SchedulerCore.java:88-89` (`liveGoroutines`, `quiescenceLatch`), `:243-244` (countDown site), `:281-283` (`awaitQuiescence`); test `SchedulerCoreTest.testQuiescenceAt10k`.

**What's wrong.** The quiescence latch is a one-shot `CountDownLatch(1)` fired the *first* time `liveGoroutines` hits 0. If an external thread is still spawning while earlier goroutines complete, the counter can hit 0 mid-spawn-stream, the latch fires, and a later `awaitQuiescence()` returns immediately with thousands of goroutines still running. The counter/latch never re-arms.

**Why it matters / evidence.** This is not theoretical — I reproduced it:
- `make test` full-suite runs: `testQuiescenceAt10k` failed 2 of 5 runs (`expected=10000 actual=248`, then `actual=693`).
- Targeted repro (10 trials of spawn-10k/await): one trial printed `done=2869 deaths=0 live=7131` — `awaitQuiescence` returned with **7,131 goroutines still live**, zero exceptions. All 10k spawns had returned normally, so no goroutine was lost; the latch simply fired early when the spawner thread fell behind the completing vthreads (trivial bodies complete faster than `Thread.ofVirtual()` creation).
- The milestone's own acceptance probe ("P2-M1 acceptance: quiescence probe green at 10k goroutines", test javadoc) is therefore flaky by construction, and `CHECKPOINT.md`'s "Unit: 10/10 green" evidence claim is not reproducible.

Note: `SchedMain` itself is unaffected (the launcher spawns `main` before awaiting, and all later spawns come from live goroutines, so a 0-crossing there is true quiescence). The broken party is the API contract — "Blocks until every spawned goroutine has finished" — and any external spawner/ embedder using the natural spawn-loop-then-await pattern, including the repo's own test.

**Suggested fix.** Replace the one-shot latch with a re-armable condition. Minimal change:
```java
// field: replace CountDownLatch with:
private final Object quiescenceLock = new Object();
// in the vthread finally block:
if (liveGoroutines.decrementAndGet() == 0) {
    synchronized (quiescenceLock) { quiescenceLock.notifyAll(); }
}
// awaitQuiescence:
public void awaitQuiescence() throws InterruptedException {
    synchronized (quiescenceLock) {
        while (liveGoroutines.get() != 0) {
            quiescenceLock.wait();
        }
    }
}
```
Every transition to 0 notifies under the same lock that guards the check-then-wait, so no wakeup is missed and early 0-crossings are harmless (a later `awaitQuiescence` re-observes the live count). Document the residual contract: no *external-thread* spawns concurrent with or after `awaitQuiescence` (spawns from live goroutines during the await remain fine). Then re-run the 10k probe in a loop (e.g., 20× full suite) as the signoff evidence.

### P0-2. Wake action silently dropped on unpark-before-park; documented ticket discipline not implemented

**Location:** `graalgo/sched/SchedulerCore.java:164-189` (`parkCurrent`/`unpark`), `graalgo/sched/Drainer.java:route` (`PARKED`/`UNPARK` cases), `graalgo/sched/GoroutineHandle.java:28-33` (`armed` javadoc).

**What's wrong.** Two coupled problems:

(a) **Dropped wake action (deterministic).** `parkCurrent` publishes the wake action *after* arming (`SchedulerCore.java:166-167`: `armPark(h)` then `h.parkWakeAction = onWake`). `unpark` (`:177-188`) reads `h.parkWakeAction` and runs it — but if `unpark` executes before the parker reaches line 167 (including the fully deterministic case of `unpark(h)` called before the goroutine ever parks — the case the docstring explicitly blesses as "Safe to call before the target parks"), the action is `null`, nothing runs, and the drainer's `UNPARK`→pending→`WAKE` matching still wakes the goroutine. The wakeup is delivered but `onWake` **never executes**, violating the contract stated on both methods ("`onWake` runs on the waker thread before the WAKE token is enqueued"; "runs its wake action, then enqueues a WAKE token"). Repro (deterministic, no timing dependence):
```java
GoroutineHandle h = sched.spawn(() -> { sched.parkCurrent(() -> actionRan.set(true)); /* ... */ });
sched.unpark(h);   // before the goroutine parks
// goroutine resumes fine, but actionRan.get() == false — contract violated
```
I ran exactly this: resumed OK, `wake action ran = false`.

(b) **Stale-action / stale-pending hazards from id-keyed, generation-less matching.** Because the action lives on the handle (not in the token) and matching is by handle id only:
- After case (a), the never-consumed action stays in `parkWakeAction`; a later *unrelated* `unpark(h)` will grab and run it with no corresponding park.
- `unpark(h)` on a goroutine that is quantum-parked (RESUME outstanding, never a PARKED) or not parked at all lands in `unparkPending` and spuriously wakes the *next* `parkCurrent`, whenever that happens.
- A double `unpark` for one park wakes the next park — a duplicated wakeup in effect.
- `GoroutineHandle`'s javadoc describes the mechanism that would have prevented all of this — "A waker wins generation `t` via `armed.compareAndSet(t, -2)`" — but `armed` is **write-only in production** (written once in `armPark`, read only by tests). The "armed-ticket discipline" claimed in the class javadocs and `PLAN.md` §2 WS A is documentation fiction; the real mechanism is id-keyed maps.

**Why it matters.** `park`/`unpark` is the blocking primitive M2 channels will build on; the wake action is where a channel rendezvous completes. A silently-dropped or spuriously-fired wake action is a correctness hole in the core, and WS I model-checking would formalize the documented (fictional) discipline.

**Suggested fix.** Make matching generation-scoped and the action atomic with the handshake, e.g.: `unpark` captures the handle's currently-armed generation (or takes it as a parameter at the call site, where the waker knows which park it is waking); the drainer matches `(id, generation)` instead of bare `id`, ignoring stale generations; publish `parkWakeAction` *before* `armPark` and have `unpark` claim it with a CAS so exactly one of (waker runs it / it is never published) happens — or, if the action must be runnable strictly before the WAKE token is enqueued even in the wakeup-first order, move the action into the `PARKED` token and have the drainer hand it to the thread that issued the matching `UNPARK`… that thread is gone, so the honest options are (i) waker-supplied action at `unpark` time, or (ii) weaken the contract to "the action runs at most once, on the waker thread when the waker wins the race, otherwise on first park admission" — but *decide and document it*; the current state (contract promises, code drops) is the worst option. At minimum, key `parked`/`unparkPending` by `(id, generation)` so stale entries can never match a later park.

---

## P1 findings (should fix)

### P1-1. Uncaught panic in a spawned goroutine → silent exit 0

**Location:** `graalgo/sched/SchedulerCore.java:228-256` (`spawn` vthread wrapper); `graalgo/sched/launcher/SchedMain.java:131-146`.

**What's wrong.** Only the *main* goroutine's body has panic handling. A `PanicException` (or `StackOverflowError`, or any unexpected `RuntimeException`) in a *spawned* goroutine propagates out of `body.run()`, through the `finally` (which still decrements and offers EXIT), and into the default uncaught-exception handler — stderr gets a raw `PanicException` stack trace and the process exits **0**. Verified end-to-end: `go func() { panic("boom") }()` under `SchedMain` prints `Exception in thread "goroutine-2" graalgo.runtime.PanicException` and `java exit=0`. Go semantics (and Phase-1's own `Main`, which maps main panics to exit 2) demand an uncaught panic *in any goroutine* crash the program with exit status 2.

**Suggested fix.** In `spawn`'s vthread wrapper, catch `PanicException`/`StackOverflowError` (and `RuntimeException` as a bug-catcher) around `body.run()`, record the first failure in an `AtomicReference<Throwable>` on the `SchedulerCore`, and have `SchedMain` check it after `awaitQuiescence()` — printing `panic: <value>` and returning `EXIT_PANIC`, mirroring the main-goroutine path. (`Goexit` in a spawned goroutine correctly terminates just that goroutine today — keep that behavior, don't lump it in.)

### P1-2. `SchedMain` dropped Phase-1's generic `RuntimeException` catch for the main goroutine

**Location:** `graalgo/sched/launcher/SchedMain.java:131-146` vs `phases/phase1/.../graalgo/launcher/Main.java:164-181`.

**What's wrong.** Phase-1 `Main` catches `RuntimeException` → prints `graalgo-run: execution failed:` → `EXIT_INPUT_ERROR` (1), plus the M10 `NoClassDefFoundError | ExceptionInInitializerError` cascade unwrap for stack-overflow masking. `SchedMain` catches only `PanicException`, `Goexit`, `StackOverflowError`. Any other `RuntimeException` escaping main (an implementation bug, e.g. in Truffle node code) kills the main vthread uncaught → quiescence fires → **exit 0**: silent success on an internal error, a parity regression vs Phase-1's exit 1.

**Suggested fix.** Port the two catches verbatim (adapted to the vthread body): `catch (RuntimeException e) { System.err.println("graalgo-run: execution failed: " + e); exitCode[0] = EXIT_INPUT_ERROR; }` and the M10 cascade-unwrap block.

### P1-3. The promised startup fail-fast for the class overlay doesn't exist; wrong order → *silent* corruption

**Location:** `phases/phase2/CHECKPOINT.md:45` ("a startup fail-fast asserts the real `Scheduler` (not the stub) is loaded"); `phases/phase2/Makefile:32` (`RUN_CP`); `graalgo/runtime/Scheduler.java:9-18` (javadoc).

**What's wrong.** No such assertion exists anywhere in `src/main` (grepped). The javadoc instead leans on "the stub's loud `UnsupportedOperationException`" — which is true for `Scheduler.spawn`, but **not** for the other two replacements: with phase1 classes first on the classpath, `Safepoint.poll()` silently becomes a no-op (lost preemption, silent fairness loss) and, worse, `GoroutineState` reverts to the Phase-1 **single static holder** — every goroutine would share one defer chain / panic slot / go-stack with no loud failure at all. The Makefile enforces the order mechanically for `make run`/`make test`, but any hand-rolled `java` invocation (docs, future CI, embedders) can get it wrong silently.

**Suggested fix.** Add the fail-fast `CHECKPOINT.md` already promises, in `SchedMain.run` before installing the scheduler — no signature change needed (freeze-safe):
```java
private static void assertPhase2Overlay() {
    // All three same-FQN replacements ship in phase2/out/classes; if the
    // Phase-1 classes won the classpath race, GoroutineState would silently
    // share one static holder across goroutines.
    String loc = graalgo.runtime.Scheduler.class.getProtectionDomain()
            .getCodeSource().getLocation().toString();
    if (!loc.contains("phase2")) {
        throw new IllegalStateException(
            "phase2 overlay not first on classpath (loaded graalgo.runtime.Scheduler from " + loc + ")");
    }
}
```
(One check suffices for all three — they ship in the same output dir.) Alternatively, downgrade the checkpoint claim — but the check is ~8 lines, so add it.

### P1-4. A `checkInvariants` violation kills the drainer thread → the exact hang the design claims to prevent

**Location:** `graalgo/sched/Drainer.java:80-96` (`drainLoop`), `:166-176` (`checkInvariants`).

**What's wrong.** The class javadoc sells the drainer as non-terminating: "a stray interrupt is swallowed and the loop continues (otherwise every parked goroutine would hang forever with no recovery)". But `checkInvariants()` throwing `AssertionError` propagates straight out of `drainLoop`, **killing the drainer thread** — and then every parked goroutine hangs forever with no diagnostic, `awaitQuiescence` blocks forever, and the user sees a silent hang instead of the promised "loud AssertionError". I verified the accounting is balanced on every path I could trace (so the trigger should be unreachable today), but the failure *mode* contradicts the contract, and any future token kind (M2/M3) that unbalances `inflight` turns a bug into a permanent silent hang.

**Suggested fix.** Wrap the per-iteration work: on `Throwable`, record it (e.g., `AtomicReference<Throwable> drainerDeath`), wake `awaitQuiescence` with the error, and fail the process loudly (`System.err` + `Runtime.halt(1)` or rethrow on a non-daemon thread) — never leave parked goroutines hanging.

### P1-5. Class/instance docs describe the unimplemented CAS discipline (also see P0-2b)

**Location:** `graalgo/sched/GoroutineHandle.java:28-33`; `graalgo/sched/SchedulerCore.java:28-34` ("Armed-ticket discipline (P2-D7, GG-WAKE-0001)…"); `phases/phase2/PLAN.md` §2 WS A ("per-vthread armed ticket + generation counter (ack-before-unpark)").

**What's wrong.** As established in P0-2: `armed` is never read by production code; no CAS happens; matching is id-keyed. A reader (or the WS I model-checking workstream) formalizing "GG-WAKE-0001" from these docs would verify a mechanism that isn't in the binary. The *generation counter* half is real (`parkSeq` → token generation → `acked`); the *armed ticket* half is not.

**Suggested fix.** Either implement generation-scoped matching (preferred — it's also the P0-2 fix) or rewrite the three doc sites to describe the actual map-based mechanism precisely, including the (currently hazardous) semantics of `unparkPending` and the wake-action lifecycle.

---

## P2 findings (nits)

### P2-1. `vthread.start()` failure leaks an in-flight slot and a live-goroutine count

**Location:** `graalgo/sched/SchedulerCore.java:228-251` (`spawn`).

**What's wrong.** `spawn` does `liveGoroutines.incrementAndGet()` (`:229`), then `drainer.offer(INIT)` (`:250`), then `vthread.start()` (`:251`). If `start()` throws (resource exhaustion — plausible exactly when spawning 10k goroutines), the increment is never paired with a decrement, and worse, the drainer will admit the orphaned `INIT` (`inflight++`) with no `EXIT` ever arriving — a permanent in-flight slot leak that shrinks the effective cap by one per failed spawn, plus a quiescence hang.

**Suggested fix.** Start the vthread *before* offering `INIT` (admission of a not-yet-started thread is already safe: `acked` is checked before parking and the `LockSupport` permit is sticky), or wrap `start()` in try/catch that decrements and offers a compensating `EXIT`.

---

## Explicitly checked — no finding

1. **Lost wakeup (delivery) on all four token paths** — traced every interleaving: quantum `RESUME`, `INIT` admission, and `PARKED`/`UNPARK` in both orders are sound. Ack-before-unpark is correctly ordered in `Drainer.admit` (`acked.set` precedes `unpark`), the waiter's `awaitAck` checks the condition before parking so early unparks are absorbed by the `LockSupport` permit, and spurious wakeups re-park on `acked < t`. No lost or duplicated *wakeup* found (the P0-2 issue is the wake *action*, not delivery).
2. **Cap assert airtightness** — every production mutation of `inflight` happens on the drainer thread under `lock` inside `route`/`admit`, and `checkInvariants` runs after each `route`+`admit` pair in `drainLoop` (and in `processOneForTest`). Only `admit` increments, gated by `while (inflight < cap)`; decrements are exactly paired (one per admission via `RESUME`/`PARKED`/`EXIT`; a vthread cannot emit two park tokens without an intervening admission since `awaitAck` blocks). `inflight` cannot exceed `cap` on any path; `setInflightForTest` is test-only. `maxInflightObserved() == 2` held at 10k spawns in every passing run.
3. **Overlay signature identity** — `javap` diff of phase2 vs Phase-1 (`6995182~5`) for all three classes: public (and package-private) signatures identical; the only delta is the expected `static {}` initializer for the `ThreadLocal` (implementation, not API). `FreezeSignatureTest` compatibility holds by construction.
4. **Classpath ordering** — enforced mechanically: `RUN_CP = out/classes:phase1/out/classes:…` (phase2 first) is the single classpath used by `make run` and `make test`. (The missing *fail-fast* for ad-hoc invocations is P1-3.)
5. **`GoroutineState` ThreadLocal correctness** — no vthread reuse (one fresh `Thread.ofVirtual()` per `spawn`), so no cross-goroutine leakage; `install`/`restore` nesting is correct (`spawn` installs on a fresh thread with `prev == null`; `GoRootNode`'s own install nests inside and restores); the drainer/launcher threads see `null` exactly as Phase-1 code expects outside Go execution. No path returns the wrong goroutine's state.
6. **`GoStatementNode`** — nil function → `Panic.throwPanic(GoErrors.nilDereference())` (correct Go semantics); non-`GoFunc` → loud `FrontendException`; args > 0 → fail-fast `FrontendException` (M1 zero-arg scope, documented); source sections set via the same `createSection(line, col, len)` idiom as Phase-1's `NodeFactory`; the function value is evaluated in the *spawning* goroutine before `Scheduler.spawn` (correct Go evaluation order); `@Child` adoption is standard.
7. **Exporter diff (`2e5f012`)** — minimal (16 lines in `export.go`, 1 in `main.go`) and justified: additive `"gostmt"` wire kind reusing `exportCall`, `toolVersion` 0.1.0→0.2.0 per freeze §5, wire stays v1. `e.node` always sets `Pos`, so `GoStatementNode.build`'s `pos` lookup is safe. No NodeFactory change needed (public `registerStmtKind` hook exists and is used). No finding.
8. **`SchedMain`** — spawn-before-await ordering correct; `--gmp` validation sane (`<1` rejected, default = carriers, effective `min(gmp, carriers)`); `InterruptedException` during quiescence re-interrupts and returns `EXIT_INPUT_ERROR`; exit-code table matches Phase-1 (0/1/2); `--check` short-circuits before scheduler install; `install(prev)` restored in `finally`. No finding beyond P1-2.
9. **Drainer liveness basics** — blocking `take` outside the lock (no lock-holding block); `InterruptedException` swallowed with loop-continue (non-terminating against stray interrupts, as designed); daemon platform thread; lane order wake > init > run per GG-WAKE-0003 as specified. No finding.

---

## Notes on method / provenance

- `make test` at the snapshot: 10/10 on most runs, but `testQuiescenceAt10k` failed 2/5 full-suite runs (P0-1). Any signoff evidence should re-run the suite in a loop (≥20×), not once.
- Repro harnesses used: `RunOne` (5×10k spawn/await — all passed in isolation, pointing at cross-test/load timing rather than a deterministic drainer bug), `RunTwo` (10 trials with death capture — the trial that failed showed `live=7131, deaths=0`, proving early latch, not lost goroutines), `RunThree` (deterministic wake-action drop, P0-2), and an end-to-end `SchedMain` run of `go func() { panic("boom") }()` (exit 0, P1-1).
- The worktree `/tmp/p2m1-r1` was removed after the review; `/tmp/Run*.java` repros were scratch only.
