# Phase-2 divergences from gc

Accepted behavioral divergences between the graalgo Phase-2 runtime and the
stock gc toolchain. Each entry records the divergence, why it is accepted,
and the observable consequence. New divergences are added by the workstream
that introduces them, in the same commit as the implementation.

## P2-M3 (timers/sleep + netpoller deletion, WS C)

### DIV-M3-1: `len()` on a buffered timer channel returns 1 (gc: 0) — FIXED

**Status: FIXED in P3-M2a** (was an accepted Phase-2 divergence;
re-test trigger discharged).

Fix: the timer channel is now the dedicated `TimerChan` flavor of
`GoChan` (`phases/phase2/src/main/java/graalgo/runtime/TimerChan.java`):
the real 1-slot buffer underneath keeps fire values deliverable (recv,
select, close-drain) while `buffered()`/`capacity()` are
overridden to report 0. gc truth established empirically vs gc 1.27.1:
`len()` AND `cap()` report 0 on timer/ticker channels in every state
(pre-fire, fired-undrained, drained, stopped).

Evidence: `phases/phase3/hidden_files/m2/redgreen.md` (red run showing
`cap=1`/`len=1` pre-fix, green post-fix) and the differential timer
corpus: [corpus/timers/tm15_timer_chan_len_cap.go](../../../../test/corpus/phase2/timers/tm15_timer_chan_len_cap.go) (+ gc-recorded
`.expected`) with its Java translation `testTm15TimerChanLenCap`
(`verifies: GG-0048`), re-run green as part of the full phase-2 suite
post-fix.

(The remainder of this entry is the pre-fix record.)

## P2-M4 (GC interop, ADR-0002)

### DIV-M4-1: finalizer runs with a nil argument (no resurrection)

`runtime.SetFinalizer(x, f)`: gc invokes `f(x)` with the original object,
which the finalizer may resurrect by storing it. graalgo invokes `f(nil)`.
A `PhantomReference` action is denied the referent by construction, and the
original Go object cannot be recovered on a pure JVM heap.

Consequence: finalizers that dereference their argument diverge loudly
(nil-pointer panic inside the finalizer, reported on stderr by the finalizer
thread) rather than silently. Finalizers that only perform cleanup keyed by
captured state (close a handle, decrement a counter, signal via a global)
behave identically.

### DIV-M4-2: finalizer runs on a JVM thread, not a goroutine

gc runs finalizers on a dedicated finalizer goroutine with a full Go stack:
channel ops, `go` statements, and blocking syscalls work. graalgo runs them
on the single JVM finalizer thread (`graalgo-finalizer`). Touching
scheduler/channel/timer state from a finalizer body fail-fasts; use a global
flag + poll loop instead (see [corpus/gc/gc01_finalizer.go](../../../../test/corpus/phase2/gc/gc01_finalizer.go)).

Serialization is preserved: at most one finalizer runs at a time, in
enqueue order, like gc.

### DIV-M4-3: `runtime.GC()` is a bounded approximation

gc's `runtime.GC()` blocks until a GC cycle completes. graalgo calls
`System.gc()` then polls GC MXBean collection counts until they advance,
with a 30 s bound. If explicit GC is disabled (`-XX:+DisableExplicitGC`)
the counts never advance and the call returns after the timeout anyway.

Consequence: callers must not depend on reclamation having completed when
`runtime.GC()` returns. The `NumForcedGC` memstat counts invocations, not
completed cycles.

### DIV-M4-4: unmappable `runtime.MemStats` fields are absent — SUPERSEDED

**Status: SUPERSEDED by PKG-051 (2026-10-16).** `runtime.ReadMemStats` is
now a live Go call path: the `runtimereadmemstats` additive wire node
([`graalgo.rtmemstats`](../../../../internal/backend/interop/src/main/java/graalgo/rtmemstats/))
populates the full `runtime.MemStats` struct from JVM MXBean telemetry,
including the fields this entry recorded as absent. The "absence, not
fabrication" stance is replaced by "present with documented deviations" —
the field-level deviations are recorded in [DIV-M4-11](#div-m4-11-runtimereadmemstats-totalalloc-is-live-heap-used-not-cumulative-mallocsfrees-always-0),
which is the authoritative statement for this surface. (The remainder of
this entry is the pre-PKG-051 record.)

`GcInterop.memStats()` returned only fields with a JVM MXBean analog:
`HeapAlloc` (heap used), `HeapSys` (heap committed), `Sys`
(heap+non-heap committed), `NumGC`, `PauseTotalNs`, `NumForcedGC`,
`PauseNsLast` (most recent pause, ns). The allocator-internals fields
(`BySize`, `HeapObjects`, `TotalAlloc`, `Mallocs`, `Frees`, `HeapIdle`,
`HeapInuse`, `HeapReleased`, `StackInuse`, `StackSys`, `MSpanInuse`,
`MSpanSys`, `MCacheInuse`, `MCacheSys`, `BuckHashSys`, `GCSys`, `OtherSys`,
`NextGC`, `LastGC`, `GCCPUFraction`) had no MXBean analog and were absent
from the map (not zero — absent, so consumers could distinguish).

Rationale: a JVM heap has no spans/mcaches; fabricating the numbers would
be worse than omitting them.

### DIV-M4-5: `weak.Pointer` has no runtime finalizer interaction

Go's `weak.Pointer` integrates with the runtime's finalizer ordering.
graalgo's vehicle is a plain `java.lang.ref.WeakReference`; finalizer
ordering relative to weak clearing follows JVM semantics (phantoms enqueue
after weak clearing), which matches gc's observable order in practice but
is not contractual.

**Scope note (P2-M4):** `weak.Pointer` and `runtime.ReadMemStats` have no Go
call path in P2-M4 — the `gcinterop:` marker dispatch handles only
`SetFinalizer`/`KeepAlive`/`GC`, and the synthetic `runtime` package
declares only those three (ADR-0002: "frontend scope permitting" for
`weak`). `newWeakRef`/`weakRefValue`/`memStats()` are implemented and
unit-tested as vehicles for future wiring, not live surface.

### DIV-M4-6: `SetFinalizer` on an `unsafe.Pointer`-held object silently never runs

`unsafe.Pointer` values lower to `UnsafeHandles` — a `HashMap<Long,Object>`
of strong JVM references (SPEC §3.4). An object reachable *only* through
`UnsafeHandles` is a permanent strong root: it is never collected, so a
`SetFinalizer` registered on it never fires, silently. (ADR-0002 `*unsafe*`
interaction bullet; recorded here per its requirement.)

This is inherent to the handle design, not a bug: the handles exist
precisely to keep the referent alive across the "pointer as integer"
boundary. Programs that convert pointers to integers and back must not
expect finalization while a handle is live.

### DIV-M4-7: `runtime.SetFinalizer` on nil is `IllegalArgumentException`, not fatal

gc raises a **fatal** `runtime.SetFinalizer: pointer not in allocated block`
for a nil pointer argument. graalgo throws `IllegalArgumentException`
("nil object"), which escapes the interpreter as a Java exception rather
than a Go fatal. Loud, but a different mechanism and message.

### DIV-M4-8: `pprof` heap profiles out of scope for P2-M4

`net/http/pprof` heap profiles (`/debug/pprof/heap`) have no P2-M4
vehicle. A faithful heap profile would require walking the JVM heap with
Go-type attribution; the JVMTI/`jmap` surface exposes JVM types, not Go
types. Stance: out of scope; revisit when the object model carries enough
type metadata for attribution (Phase 4+).

### DIV-M4-9: `debug.FreeOSMemory` unsupported (no JVM analog)

### DIV-M4-10: finalizers run only after an explicit `runtime.GC()` (registration pins the object) — FIXED

**Status: FIXED 2026-10-18** (was an accepted Phase-2 divergence; re-test
trigger discharged by R-P0-3, review lane `review-finalizer`).

Fix: the `PINNED` strong set is gone from
[`GcInterop`](../../../../internal/backend/runtime/src/main/java/graalgo/runtime/GcInterop.java).
Registration is now only the `Cleaner` phantom plus the weak `BY_OBJ` map
(plus a `reachabilityFence` across registration). A phantom keeps no
strong reference, so a finalizable object becomes collectable as soon as
the Go program drops it — no explicit `runtime.GC()` needed — and
`runtime.GC()` no longer touches finalizer state, matching gc (whose
`runtime.GC()` never releases registrations). The old register/cancel
liveness rationale was re-examined and rejected: interpreter frame slots
are real GC roots, and past the last use Go's own contract
(`runtime.KeepAlive`) applies, same as under gc.

Evidence: rewritten corpus
[`gc07_finalizer_ungated.go`](../../../../test/corpus/phase2/gc/gc07_finalizer_ungated.go)
(now differential — finalizer runs under allocation pressure with no
explicit `runtime.GC()`, matching the gc-recorded `.expected`) plus
`GcInteropTest.testFinalizerRunsWithoutExplicitGc` /
`testFinalizerNotFiredEarlyByGc`.

**Note (2026-10-18):** while re-recording, found that gc1.27.1 never runs
finalizers for tiny-allocated objects (< 16 bytes, noscan) — verified
empirically; non-tiny objects finalize promptly. The JVM has no tiny
allocator, so graalgo finalizes such objects where gc does not (graalgo
is more faithful to the documented "unreachable ⇒ finalizable"
contract here). The gc07 struct carries a pointer field to stay out of
the tiny allocator on the gc side. gc01's `.expected` predates this
finding (tiny `T`, gc-recorded as "finalized"); re-recording it is a
deliberate baseline decision, out of scope for this fix.

(The remainder of this entry is the pre-fix record.)

`GcInterop.setFinalizer` keeps every registered object in a static strong
set (`PINNED`) until the Go program calls `runtime.GC()` (or cancels via
`SetFinalizer(x, nil)`). The pin bridges the register/cancel liveness
window, but it also means: a program that registers a finalizer, drops
the object, and never calls `runtime.GC()` sees the finalizer **never
run** — and the object retained **forever** (`PINNED` grows unboundedly,
a genuine memory leak). gc runs finalizers when the object becomes
unreachable; no explicit GC call is needed.

Consequence: Go programs that rely on prompt finalization without an
explicit GC must be restructured (call `runtime.GC()` at quiescent
points, or use explicit cleanup). Finalizers that do run behave per
DIV-M4-1/DIV-M4-2.

**Re-test trigger:** if the pinning design changes (e.g. per-object
reachability fences at the Go call sites instead of the global set),
re-run the GC differential corpus and drop this entry.

`debug.FreeOSMemory()` forces a GC *and* returns memory to the OS. There is
no JVM analog: `System.gc()` suggests collection but does not compel the OS
to take pages back (heap shrinking is the collector's decision). Per ADR-0002
D4 ("no JVM analog (`System.gc()` is not it)"), the stance is
unsupported-with-justification. No Go call path is wired in P2-M4.

### DIV-M4-11: `runtimereadmemstats` `TotalAlloc` is live heap-used, not cumulative; `Mallocs`/`Frees` always 0

**This is the loudest accepted divergence on the `runtime.ReadMemStats`
surface — read it before writing any memory-accounting code against
graalgo.**

`fillMemStats` (PKG-051, `runtimereadmemstats` wire node) maps the JVM
heap MXBean onto `runtime.MemStats` as documented in the field-mapping
JavaDoc of
[`RtMemStats.fillMemStats`](../../../../internal/backend/interop/src/main/java/graalgo/rtmemstats/RtMemStats.java).
Two fields break gc's contract in ways that are easy to misuse silently:

1. **`TotalAlloc` is current heap-used, NOT cumulative.** gc defines
   `TotalAlloc` as the cumulative count of bytes allocated for heap
   objects — monotonic, never decreasing when objects are freed (gc
   `src/runtime/mstats.go`). The JVM exposes no cumulative allocation
   counter, so graalgo reports the live `heapUsed` figure instead: it is
   equal to `Alloc`/`HeapAlloc`/`HeapInuse` at every sample and can
   **decrease** after a GC cycle. The standard gc idiom
   `m2.TotalAlloc - m1.TotalAlloc` ("bytes allocated between the two
   calls") is **meaningless** on this backend — it measures the *change in
   live heap*, not allocation volume. Deliberately *not* "fixed" with a
   monotonic max-seen accumulator: a number that merely never decreases
   would still be wrong (it is not allocation volume) while looking more
   trustworthy — more misleading, not less. Pinned behavior is asserted by
   `RtMemStatsTest.testTotalAllocIsLiveHeapUsed`.

2. **`Mallocs`/`Frees` are pinned at 0, always.** The JVM has no
   allocation-count counters. Consequence:
   **`testing.AllocsPerRun` always returns 0 on this backend** (it is
   computed as `m2.Mallocs - m1.Mallocs` per gc
   `src/testing/allocs.go`), so any test asserting "this code allocates
   fewer than N objects" **passes vacuously** — the assertion proves
   nothing. Pinned behavior is asserted by
   `RtMemStatsTest.testMallocsFreesAreAlwaysZero`.

Rationale: the quantities are unmeasurable on the JVM; the fix is
honesty, not approximation. Every other field's approximation is listed
in the `fillMemStats` JavaDoc mapping; fields the JVM cannot observe at
all report 0 and are enumerated there.

**Re-test trigger:** if the JVM ever exposes cumulative allocation
telemetry usable from `java.lang.management` (or JVMTI agent support
lands in this backend), re-check whether `TotalAlloc`/`Mallocs`/`Frees`
can be reported with the real gc semantics, and update this entry.

## Template for new entries

### DIV-<M>-<N>: short title

What diverges, why it is accepted, and the observable consequence.

## P2-M5 (GODEBUG surface triage, WS E)

### DIV-M5-1: unknown GODEBUG knob is a hard error

gc silently ignores GODEBUG settings it does not recognize. graalgo
exits 2 with `graalgo-run: fatal error: unknown GODEBUG setting "..."`.
Rationale: a typo (`GODEBUG=schedtace=100`) silently doing nothing is a
worse failure mode than failing loud at startup; the full supported
registry is published in `phases/phase2/GODEBUG.md`.

### DIV-M5-2: malformed GODEBUG value is a hard error

gc keeps the old value when a GODEBUG value does not parse as an int32.
graalgo exits 2. Same rationale as DIV-M5-1.

### DIV-M5-3: bare GODEBUG field without '=' is a hard error

gc skips fields without `=`. graalgo exits 2 suggesting `field=1`.
Same rationale as DIV-M5-1.

### DIV-M5-4: emulated trace formats are graalgo-shaped

`GODEBUG=schedtrace` prints `SCHED <ms>ms: goroutines=N cap=C
carriers=K` (gc prints P/M fields that have no counterpart in the
park/queue/drainer design); `GODEBUG=gctrace` prints per-cycle lines
with JVM-heap numbers (gc prints allocator-level detail). The emulated
observable is "a periodic/per-cycle scheduler/GC line on stderr",
verified differential by `tools/godebug-diff` (4/4 green). Byte-identity
is not attempted.

### DIV-M5-5: effective behavior permanently equals a non-default value

For five NO-OP knobs the runtime cannot exhibit gc's default behavior,
so the effective behavior always equals the noted non-default value
regardless of what is set (even the default):

| knob | gc default | graalgo effective | why |
|---|---|---|---|
| `asyncpreemptoff` | 0 | 1 | no signal-based async preemption exists |
| `dontfreezetheworld` | 0 | 1 | no stop-the-world phase exists to freeze |
| `containermaxprocs` | 1 | 0 | no container CPU-quota detection; never applied |
| `updatemaxprocs` | 1 | 0 | same |
| `tracebacklabels` | 1 | 0 | goroutine labels cannot exist (no pprof label API) |

These stay NO-OP (accepted, ignored) rather than UNSUPPORTED because
denying gc's *default* value would be hostile for zero benefit: the
program cannot get the default behavior either way, and every setting
is observably identical inside this runtime. The triage principle is:
deny when the user would observe missing output/behavior they asked
for (UNSUPPORTED); accept when the knob cannot have observable effect
here (NO-OP) — and document the effective value here.

## P2-M6 (JFR events, WS F)

### DIV-M6-1: `runtime/trace` has no JFR mapping (JFR is the native surface)

gc's `runtime/trace` has two layers: (a) goroutine/channel/mutex-level
events (`GoStart`/`GoEnd`/`GoBlockSend`/`GoBlockRecv`/`GoBlockSelect`/
`GoUnblock`), and (b) the gc runtime's internal machine state (P, M, G
transitions, STW phases) plus the trace binary format and the
`go tool trace` UI. Layer (a) maps directly — this milestone *is* that
mapping under JFR-native names (`graalgo.GoroutineStart/End`,
`graalgo.GoroutinePark` with reason + duration, `graalgo.ChannelBlock`).
Layer (b) has no counterpart: goroutines are Loom virtual threads, so
there are no Ps or Ms to trace, and `go tool trace` compatibility is
not claimed. `trace.WithRegion`-style span annotations are likewise out
of scope (no JFR equivalent is emitted).

Consequence: `runtime/trace` is not in the supported surface — a Go
program importing it fails to build loudly (it is not silently
ignored). Operators use `jfr print` / JMC on a recording with
`graalgo.*` enabled instead (SPEC §3.10).

## P2-M1/M2/M7/M8/M9 — no accepted divergences

P2-M1 (scheduler core), P2-M2 (channels + select), P2-M7 (pinning audit),
P2-M8 (preemption-torture), and P2-M9 (model-checking + race stress)
introduced **no accepted behavioral divergences**: every differential
corpus ran green vs gc 1.27.1 (M2: 12/12; M3: 14/14; M8: GOMAXPROCS 8/8
byte-identical, torture 12/12; M5: godebug-diff 4/4).

## GG-0048 (scheduling-observable divergence list — done-gate 5)

SPEC §7.2 defines the preemption-equivalence bar: the differential oracle
must hold these scheduling observables equivalent vs gc. Exact preemption
*timing* is excluded (Go's memory model promises no preemption timing).
Per-entry disposition for the closeout (P2-M10):

### (i) Fairness — no goroutine starvation under contention, bounded window: CLOSED

- Torture F1 legs (production path): 5 legs, 17k–53k samples each
  (36,757 / 16,771 / 25,024 / 52,672 / 41,397), CV 0.001–0.018 — every
  spinner admitted, no starvation ([hidden_files/torture/REPORT.md](../../spikes/01-asmcgo/REPORT.md) §1).
- Select fairness: differential 12/12 vs gc (P2-M2 sign-off).
- No accepted divergence; no trigger needed.

### (ii) Starvation-freedom — every runnable goroutine eventually runs: CLOSED

- F3 quiescence: 2000/2000 admitted; extended F3: 10000/10000, max/mean
  1.01 (REPORT.md §1).
- F6 lost-wakeup: 32 pairs × 200, 0 errors, 0 lost wakeups; provenance
  direct=12829, stashed=3.
- Model-checking (P2-M9): 12/12 exhaustive scripts (max 12,365 states),
  no lost wakeup; red-green guard fails closed.
- No accepted divergence; no trigger needed.

### (iii) Latency distribution of wakeups within a documented tolerance band: ACCEPTED-WITH-TOLERANCE

- Observable is the *distribution*, not bit-identical timings (SPEC §7.2).
- Overhead budget: 34.7µs mean hop at 10ms quantum vs 100µs budget
  (1%-of-quantum) — 2.9x margin, PASS (REPORT.md §4).
- F4_q1ms leg: 48.9µs > 10µs budget — INFO-FAIL per spike precedent
  (falsifier fires only at 10ms; structural drainer/FJP cost, documented
  in spike 2b §2/F4). Not a divergence from gc semantics — a
  performance-budget note.
- **Re-test trigger:** re-measure on quiet server hardware before any
  production performance claim (standing trigger since spike 2b).

### (iv) `GOMAXPROCS=n` behavior incl. pinned-carrier matrix: CLOSED

- Go-level matrix {1,2,4,N} × {compute, heterogeneous-compute}: 8/8 cells
  byte-identical vs gc (REPORT.md §3).
- F5 cap legs: maxTrueParallel=1 at gmp=1 (0 violations), =2 at gmp=2;
  F5b cap-exhaustion: slots released on NIO block (5.9M spinner iters);
  gmp=32 clamp leg confirms oversubscription becomes drainer-FIFO
  time-slicing.
- Coverage gaps recorded honestly (REPORT.md §3): channel-heavy matrix is
  Java-level (F1, genuine unbuffered ping-pong) not a {1,2,4,N} matrix;
  `gmp_mixed.go` is heterogeneous compute, not I/O+compute. Neither is a
  divergence — both are scope notes with the same re-test trigger.
- **Re-test trigger:** when Go channel/timer ops are in Go-source scope,
  run the channel-heavy matrix differentially as Go programs.

### (v) `runtime.Gosched` and preemption-point responsiveness: CLOSED

- Preemption-point responsiveness: F1 quantum legs at 1ms/100µs/10ms —
  all PASS with tight CV; no goroutine holds a carrier past its quantum.
- Cooperative yield: `Scheduler.yield()` is the Gosched vehicle,
  exercised by `JfrEventsTest.testHotLoopParkFilter` (yielders drive the
  deterministic 1-in-1024 quantum sampling).
- Exact preemption timing excluded per SPEC §7.2; no divergence claimed.

**Net: GG-0048 list is closed — four entries CLOSED with zero accepted
divergences, one (latency distribution) ACCEPTED-WITH-TOLERANCE under the
documented band, with re-test triggers for the two standing hardware/
scope notes. Done-gate 5 evidence: this file (versioned) +
[hidden_files/torture/REPORT.md](../../spikes/01-asmcgo/REPORT.md) + [hidden_files/modelcheck/REPORT.md](../../spikes/01-asmcgo/REPORT.md).
