# P2-M7 pinning audit (WS G, GG-0057)

Done-gate 2: `jdk.VirtualThreadPinned` shows zero unexpected pins on the
conformance corpus.

## 1. Methodology

- Event: `jdk.VirtualThreadPinned` (JDK; fires on park-while-pinned),
  enabled per child JVM via inline `-XX:StartFlightRecording` settings
  (`jdk.VirtualThreadPinned#enabled=true`,
  `jdk.VirtualThreadPinned#stackTrace=true`). A bare custom `.jfc`
  silently enables nothing — do not use (P2-M7 spike).
- Tool: `graalgo.sched.audit.PinAudit`
  (`--corpus DIR... [--tests] [--allow FILE] [--gmp N] [--out report.md]`).
  Each program runs in a child JVM; each recording is parsed with
  `jdk.jfr.consumer.RecordingFile`. Exit 0 iff zero unexpected pins.
  Note: `SchedMain` calls `System.exit()`, so the recording must be
  JVM-managed (`-XX:` flag); an in-JVM `Recording` wrapper never dumps.
- Positive control (pipeline proven): JNI `GetPrimitiveArrayCritical` +
  `LockSupport.parkNanos` on a virtual thread → exactly 1 event with full
  stack (`VirtualThread.postPinnedEvent` → `doPark` → `parkWhileCritical`).
  Fixture: `src/test/resources/pinproof.jfr`; `PinAuditTest` asserts
  detection, allowlist-EXPECTED, and allowlist-mismatch-UNEXPECTED.
- Bar: ZERO unexpected pins. Expected = allowlist match (budgeted
  off-carrier FFM/file-I/O). Default allowlist is empty: graalgo has no
  JNI/FFM on goroutine paths (see §5).

## 2. Platform finding: JEP 491 (JDK 25)

On GraalVM 25 (= JDK 25), `synchronized` blocks/methods and
`Object.wait()` NO LONGER pin virtual threads. Verified empirically with
`-Djdk.tracePinnedThreads=full`: synchronized+park and blocking
`PipedInputStream.read` are silent. A 2 s blocking FFM `sleep(3)` downcall
also emits no pin event (no park involved). The remaining pinning class is
park-while-in-JNI-critical (or equivalent native). This collapses the audit
surface to: JNI/FFM on goroutine paths, plus JVM-internal pinned parks.

## 3. Static audit (all green)

- All scheduler parks occur outside monitors: `GoChan.send`/`recv` (park
  after monitor exit), `SelectOp` (3 park sites, all outside
  `synchronized(ch)`), `Timers.sleep` (no monitor held), `quantumPark`
  (via `Safepoint.poll`; no monitor on the path).
- `synchronized` in phase-1 runtime (`UnsafeHandles.alloc/get/free`,
  `Intrinsics.fastrand64`, probe classes) is short and non-blocking —
  no park/sleep/wait/I-O inside any monitor on a goroutine path.
- Blocking sites are platform threads only: `SchedulerCore:481`
  quiescence wait (main thread), `TimerHeap:323` (timer thread),
  `GcInterop:247` sleep (called from `gc()`, platform),
  `SchedTrace:57` sleep (platform).
- `make check` pin-surface grep gate: no `native` method declarations, no
  `java.lang.foreign` imports in `src/main` (phase 1 + phase 2).

## 4. Dynamic audit — finding and fix

First full run (186 programs: 148 phase-1 corpus + 37 phase-2 corpus +
`SchedTestRunner` under JFR; GOMAXPROCS=2): **13 UNEXPECTED pins**, all
with the same root cause — lazy `<clinit>` of graalgo's own JFR event
classes (`GoroutineStartEvent` ×5, `GoroutineEndEvent` ×5,
`GoroutineParkEvent` ×3) on goroutine carrier threads. Stack shape:

```
jdk.jfr.internal.MetadataRepository.register
jdk.jfr.FlightRecorder.register
graalgo.jfr.GoroutineEndEvent.<clinit>
graalgo.jfr.JfrEvents.goroutineEnd
graalgo.sched.SchedulerCore.lambda$spawn$0
```

Root cause: `JfrEvents.ensureRegistered()` referenced class literals,
which do NOT trigger `<clinit>` (JLS 12.4.1) — verified with a probe
(`Foo.class` never runs `<clinit>`; `FlightRecorder.getEventTypes()` shows
0 graalgo types after load-only, 1 after `initialize=true`). The one-time
`FlightRecorder.register` (which parks while pinned) therefore fired on
the first goroutine to emit each event type.

Fix (commit `0dd2e2f`):
1. `ensureRegistered()` now forces `<clinit>` via each event class's own
   static `enabled()` (each declares it, touching the subclass-static
   prototype — invoking a subclass-declared static forces initialization).
2. `SchedulerCore` constructor calls `JfrEvents.ensureRegistered()` on the
   startup thread, before the first spawn.

Regression tests:
- `JfrRegistrationTest` (first in `SchedTestRunner`): asserts all 4
  graalgo event types are registered after `ensureRegistered()`. Red-green
  verified: FAILS on the old class-literal version, passes on the fix.
- `PinAudit` itself (`make pin-audit`): the dynamic gate.

Re-audit after fix: **0 pins** (see `pinaudit-report.md`).

## 5. cgo/FFM + blocking file-I/O budget

- Current production code has no FFM/JNI and no blocking file-I/O on
  goroutine paths: zero `native` declarations, zero `java.lang.foreign`
  imports in phase-1 + phase-2 `src/main` (grep gate in `make check`);
  the netpoller was deleted (GG-0018); `ioEnter`/`ioExit` have no
  production callers yet.
- Budgeted +k carriers for pinned blocking I/O: **+0** — there is nothing
  to budget. No allowlist entries exist.
- Mechanism ready for future FFM/file-I/O paths:
  `SchedulerCore.ioEnter()`/`ioExit()` releases the in-flight slot around
  the blocking call so the scheduler admits another goroutine; the carrier
  stays pinned only transiently (option C: dumb-pool capacity, fairness is
  drainer-owned). When such a path lands, its pin events must be
  allowlisted in `PinAudit --allow` with the measured +k documented here,
  or the audit fails closed.

## 6. Results

- `make pin-audit` (full corpus + test suite under
  `jdk.VirtualThreadPinned`): 186 programs, **0 pin events**
  (`hidden_files/pinning/pinaudit-report.md`).
- `make check`: pin-surface grep gate clean.
- Done-gate 2 (GG-0057 falsifier: unexpected pin event): NOT triggered —
  zero unexpected pins.
