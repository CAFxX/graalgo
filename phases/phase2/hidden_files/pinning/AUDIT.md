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
- **Event boundary (reviewer P2-1):** `jdk.VirtualThreadPinned` fires
  **only on park-while-pinned**. A blocking FFM/JNI downcall that never
  parks pins the carrier with no event (verified: 2 s `sleep(2)` downcall
  → 0 events). The dynamic leg therefore cannot see pin-without-park;
  that class is controlled by the static grep gate (§3) plus the +0
  budget (§5). Division of labor: dynamic leg ⇒ park-while-pinned;
  grep gate ⇒ no-park class.

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
- Blocking sites: `SchedulerCore:481` quiescence wait runs on the main
  thread (and test threads); `TimerHeap:323` on the timer thread;
  `SchedTrace:57` sleep on a spawned platform thread. `GcInterop:247`
  (`Thread.sleep(50)` in `gc()`) executes on the goroutine carrier when
  reached via Go `runtime.GC()` (`GcCallNode`), but `Thread.sleep`
  parks without pinning on JDK 25 — no pin possible.
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

**Intermittency note (reviewer P1-1):** pre-fix, `ensureRegistered()`
was dead code (no production callers), so all 186 child JVMs ran the
event `<clinit>` on goroutine carriers — yet only 13 pins fired (~2–4%
per registration). The pin-on-register is intermittent (plausibly a
JVM-startup race in JFR subsystem init). Consequence: the re-audit "0
pins" alone is weaker evidence than a clean count implies. What proves
the found source is gone is the **deterministic fix** — a probe confirms
4/4 event types register on the calling (startup) thread every time —
plus `JfrRegistrationTest`, which asserts that post-condition in the
suite. The re-audit count is corroboration, not the proof.

**Evidence preservation:** the raw 13-pin stacks from the pre-fix run
were not archived (the report file was overwritten by the re-audit).
The stack shape above is transcribed from the pre-fix report; the
mechanism (not the individual stacks) is what the fix addresses, and
the mechanism is deterministically verified.

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
  `jdk.VirtualThreadPinned`): **0 pin events**
  (`hidden_files/pinning/pinaudit-report.md`).

### Honest coverage accounting (reviewer P1-2)

The report's "186 programs" headline overstates coverage. Of the 186:
- **146 clean runs** (exit 0): full pin evidence, 0 pins.
- **13 panic-runs** (exit non-zero, genuine Go panics): partial pin
  evidence (scheduler ran, program panicked), 0 pins.
- **1 tests leg** (`SchedTestRunner`, exit 0): 162 tests including
  channel/select/timer/GC park paths, 0 pins.
- **26 build failures** (all 12 `corpus/channels/` + all 14
  `corpus/timers/`): the frontend v1 does not support channels/timers/
  select yet, so `SchedMain` exits before any goroutine spawns. These
  contribute **no pin evidence** — their "0 pins" is vacuous.

Honest headline: **146 clean + 13 panic + 1 tests-leg = 160 units with
pin evidence, 0 pins; 26 programs contribute none.** The gap is real:
zero Go-source coverage of channel/select/timer park paths; all of it
comes from the Java-translated tests leg. When the frontend learns
channels/timers/select, a full `make pin-audit` re-run is required
before sign-off of that milestone (reviewer P2-3).

- `make check`: pin-surface grep gate clean; pin-audit smoke
  (`--tests` leg) clean.
- Done-gate 2 (GG-0057 falsifier: unexpected pin event): NOT triggered —
  zero unexpected pins on all units with pin evidence.
