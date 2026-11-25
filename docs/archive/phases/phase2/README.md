# Phase 2 — scheduler core (P2-M1, WS A)

User-space goroutine scheduler (option C, spike 2b PASS): goroutine =
virtual thread + scheduler token; quantum-based preemption via
`Safepoint.poll()`; single platform-thread drainer; in-flight cap =
`min(GOMAXPROCS, carrier parallelism)` (P2-D6).

## Layout

```
phases/phase2/
  Makefile                  own build module (mirrors Phase-1 layout)
  src/main/java/graalgo/sched/        NEW: scheduler core
    SchedulerCore.java      spawn/park/unpark, quantum, cap, tickets
    GoroutineHandle.java    vthread + parkSeq/armed/acked + deadline
    ResumeToken.java        INIT/RESUME/WAKE/EXIT/PARKED/UNPARK tokens
    Drainer.java            platform-thread drainer, 3 lanes
    nodes/GoStatementNode.java        the `go` statement
    launcher/SchedMain.java           graalgo-run --gmp=N entry point
  src/main/java/graalgo/safepoint/Safepoint.java   \
  src/main/java/graalgo/runtime/Scheduler.java      } same-FQN replacements
  src/main/java/graalgo/runtime/GoroutineState.java /
  src/test/java/graalgo/sched/        unit tests + SchedTestRunner
```

## The overlay mechanism (P2-D8)

Phase 1 is consumed as a FROZEN dependency (`../phase1/out/classes` on the
compile classpath; its sources are never modified by Phase-2 work — the one
documented exception is the exporter's `go`-statement emission, a separate
Phase-1 commit under the freeze change policy).

Three Phase-1 classes are explicitly designated "Phase 2 owns" in their own
javadoc (`Safepoint`: "Phase 2 gives it semantics"; `Scheduler`: "Phase 2
owns goroutine scheduling"; `GoroutineState`: "Phase 2 replaces the static
holder"). This module provides same-FQN replacements with **byte-identical
signatures** and real bodies. Method bodies are INTERNAL per freeze §3.5, so
no ADR/version bump is required; `FreezeSignatureTest` keeps passing.

The runtime classpath puts `phase2/out/classes` FIRST (`RUN_CP` in the
Makefile). Every call compiled into the Phase-1 AST (`Safepoint.poll()`,
`GoroutineState.current()`, …) therefore resolves to the Phase-2
implementation. If phase1 classes ever come first, the stubs' loud
`UnsupportedOperationException`s fire — a fail-fast, never silent.

`Safepoint.poll()` with no installed scheduler is a no-op, so scheduler-less
Phase-1 runs keep Phase-1 semantics even with the replacement on the
classpath (the Phase-1 208-test suite stays green).

## Protocol (spike 2b, reimplemented)

- Quantum (10 ms default, P2-D5): the vthread checks its deadline in
  `poll()`; on expiry it parks indefinitely (`LockSupport.park`, P2-D2)
  and enqueues a RESUME token.
- Drainer: blocking take → route to lane → admit while `inflight < cap` →
  hard-assert `inflight <= cap` (GG-SCHED-0003). Lane order per GG-WAKE-0003:
  wake > init > run. Non-terminating (interrupt → swallow-and-continue).
- Armed ticket (P2-D7): every park reserves a generation BEFORE becoming
  visible; admission does ack-before-unpark (GG-WAKE-0001). The drainer
  matches PARKED/UNPARK by handle, tolerating both orders.
- Cap = min(GOMAXPROCS, carrier parallelism) (P2-D6).

## Closed-world spawn contract (reviewer1 P0-1)

`awaitQuiescence()` blocks until the live-goroutine count hits zero. The
spawn set must therefore be CLOSED before awaiting: every spawn must
originate from a live goroutine, or from threads that finished spawning
before the await call. A spawn stream issued directly from the awaiting
thread (e.g. a 10k-spawn loop in a test) can transiently hit zero
mid-stream and return early — that is a caller bug, not a scheduler bug.
Bulk spawns go through a spawner goroutine, which stays live until the
last spawn is issued.

## Failure discipline (reviewer1 P1-1/P1-4, P2)

- An uncaught Go panic in ANY goroutine is recorded and printed as
  `panic: <value>`; the launcher exits 2 (gc parity). `Goexit` kills just
  that goroutine. Internal `RuntimeException`s and stack overflows are
  recorded loudly; the launcher exits 1.
- The vthread is started BEFORE accounting (live count, INIT token): if
  `start()` throws, no slot is leaked.
- A violated drainer invariant (or any drainer bug) halts the JVM loudly
  (`halt(1)` after printing). A dead drainer thread would hang every
  parked goroutine forever with no diagnostic — the non-terminating
  contract forbids silent death.

## Overlay fail-fast (reviewer1 P1-3)

`SchedMain` asserts at startup that the Phase-2 `Scheduler` replacement
is loaded (`isPhase2()`, a Phase-2-only method absent on the Phase-1
stub). Wrong classpath order (phase1 classes first) fails immediately
with a clear message instead of silently sharing one `GoroutineState`
across goroutines.
