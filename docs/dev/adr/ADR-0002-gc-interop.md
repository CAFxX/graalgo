# ADR-0002 — Phase-2 GC interop: heap layout, roots, write barriers

**Date:** 2026-10-05
**Status:** accepted 2026-10-05 — both independent ADR reviews returned
GO-WITH-FINDINGS; all findings remediated and delta re-verified by both
reviewers (reviewer1-delta.md, reviewer2-delta.md: DELTA-GO). Implementation
may start.
**Scope:** closes PLAN.md §3 `[OPEN] GC interop heap layout` ("the largest design
unknown in Phase 2"); workstream D (P2-M4).
**Normative basis:** SPEC §3.4 "Value model: Java objects throughout"
`[DECIDED]` (GG-0015, GG-0016); SPEC §4.5 GODEBUG contract (GG-0055, GG-0045).
**Spike evidence:** `phases/phase2/hidden_files/p2-m4-spike/` (canonical;
re-run recipe in its README; provenance: `/tmp/gcspike`, throwaway).
7/7 checks pass, independently re-run by both reviewers.

---

## D1. Heap layout: all Go objects live in the JVM heap; the JVM GC *is* the Go GC

**Decision:** there is no segregated Go heap region and no Go-level
marker/sweeper. Every Go value is a JVM-heap Java object (SPEC §3.4:
descriptor-based structs with `long[]` scalars + `Object[]` references,
slices as typed Java arrays, maps as a hand-written table, `GoString{byte[]}`,
interfaces as `(descriptor, data)` + itab, closures as
`{RootCallTarget, MaterializedFrame|null}`). Reachability, tracing, and
reclamation are the JVM GC's job — exactly, not conservatively.

**Why not a segregated region (rejected alternative A1):** SPEC §3.4 already
rejected off-heap as primary storage (scoped verdict T5 §2.3: "for a managed
Go implementation it reimplements the GC's job worse"). The P2-M4 spike
confirms there is nothing to gain for correctness:
- a goroutine parked indefinitely on a channel `recv` (the real
  `SchedulerCore` + `GoChan` park path, `LockSupport.park()`) keeps a ~20 MB
  object graph alive across forced GCs — roots *are* enumerated from the
  parked vthread's stack through the chain
  `GoChan.recvWaiters → ChanWaiter → GoroutineHandle.thread → vthread stack
  chunks → Java locals/Truffle frames` `[MEASURED: default GC, Java-local
  roots, channel-waiter park path]`;
- a `WeakReference` to a *deep leaf* of that graph also survives — the
  transitive closure is traced exactly, no holes `[MEASURED: same scope]`;
- after the goroutine exits, the graph is collected — scheduler, channel, and
  timer structures do not over-retain dead goroutine stacks (no leak)
  `[MEASURED: same scope]`.

A segregated region would additionally need its own allocator, stack maps,
and a full marker — all duplicated machinery the JVM already provides.
Reviewer 1 attacked D1 with finalizer ordering/serialization, weak pointers,
memstats, pacing, `GC()` blocking, and heap profiles: none overturns the
*layout* decision — all are D4 fidelity issues, which is where they belong.

**Consequence:** P2-M4 does not build a garbage collector. It builds (a) the
Go-*observable* GC surface on JVM primitives (D4), and (b) the verification
that the interop is exact (D2/D3 + differential tests).

## D2. Root enumeration: the JVM's — exact by construction

**Decision:** "exact root enumeration (goroutine stacks, globals, channel/timer
structures)" is satisfied by the JVM GC's own root set. JVM collectors on the
supported configurations trace *typed* references from thread stacks (oop
maps/stack chunks for vthreads), globals (static fields), and all heap
structures — there is no conservative stack scanning to produce false roots,
and the spike's leaf-weakref check shows no missed roots either.

The generalization beyond the spike's channel-park path rests on a
**structural funnel invariant** (reviewer 1's audit), not on the spike alone:
the *only* `LockSupport.park()` site in phase-2 main is
`SchedulerCore.java:273` (inside `awaitAck`); both callers (`parkCurrent`,
`quantumPark`) enqueue a `ResumeToken` with a strong final `handle` field to
the drainer *before* parking; `close()` unparks (token holds the handle)
before clearing waiter queues — so there is no window, on any park kind
(channel, timer, select, quantum, IO), in which a parked vthread is
unreferenced. The chain is structurally
drainer-queue → token → `GoroutineHandle` → `Thread` → stack chunks.
No JNI/native methods exist in the tree; `GoroutineState.HOLDER` is a
`ThreadLocal` (a proper GC root) `remove()`d on exit.

What P2-M4 must guarantee (and test) is that every Go reference the program
can observe is *reachable through JVM-traced references*:
- goroutine stacks: vthread stack chunks via the funnel above;
- globals: Phase-1 package-level variables live in Java statics/objects —
  traced as ordinary roots;
- channel/timer structures: `GoChan` buffers and waiter queues, `TimerHeap`
  entries hold Go values as Java references — traced as ordinary heap.

**Verification backlog (not covered by the spike):** Truffle-frame root
coverage (the spike exercised Java locals; materialized/interpreter frames
need their own weakref-torture case), and re-running the spike per supported
collector (see collector decision under F2).

**Rejected alternative A3 (explicit stack maps + cooperative root
publication):** publishing per-safepoint root buffers from parking vthreads
would duplicate what the JVM already computes from the same stacks, add
per-park overhead on the 10 ms quantum path, and risk *disagreement* bugs
between the two enumerations. Rejected — one source of truth.

## D3. Write barriers: none at the Truffle level — JVM barriers cover every store

**Decision:** no Go write barrier is inserted at the Truffle level. Every Go
reference store in the codebase is a JVM-heap store — Java field, `Object[]`
element, or JDK collection entry — and therefore passes through the JVM GC's
own barriers (G1/ZGC/Shenandoah pre/post barriers as configured). "Free from
JVM GC barriers" is satisfied vacuously: there is *no* Go barrier whose
correctness could depend on JVM-GC internals.

**Audit evidence** (mechanical, re-runnable; independently reproduced by both
reviewers, reviewer 1 with extended patterns):
- `allocateMemory`: 0 hits — no off-heap Go object storage;
- `sun.misc.Unsafe | jdk.internal | MemorySegment | Arena`: 0 hits in
  sources (single hit is a prose README mention);
- no reference-smuggling (bit tricks are legitimate float/int scalar work;
  no long→Object reconstitution);
- carrier split is descriptor-enforced: `GoStructDescriptor` routes every
  reference kind to `Object[]`, `GoStruct` throws on mismatch;
- `unsafe.Pointer` is the `UnsafeHandles` handle table (`HashMap<Long,
  Object>`) per SPEC §3.4 — JVM heap, fully barriered.

**Rejected alternative A2 (Truffle-level barrier + Go remembered set):** a
barrier with no Go heap to remember into is pure overhead duplicating the
JVM's barriers. Rejected.

**Regression guards (two layers):**
1. `make check` grep gate — bans off-heap storage shapes outright:
   `allocateMemory`, `MemorySegment`, `Arena`, `allocateDirect`,
   `theUnsafe`-style reflective acquisition, `import sun.misc.*` /
   `jdk.internal.misc.*`. One line per pattern stating what it guards
   (off-heap storage / barrier bypass / raw-address access). Rationale
   discipline: `Unsafe.allocateInstance` creates an ordinary *heap* object
   (no barrier hole) — it is not listed under the barrier gate.
2. Semantic backstop — a GC-visibility differential per value kind: allocate
   a graph, retain it *only* through the carrier under test, force GC,
   weakref-verify survival (the spike's pattern generalized). A future
   carrier that misclassifies a reference kind into a primitive array is
   invisible to grep and caught here. Wired to falsifier F3.

## D4. Go-observable GC surface: emulated on JVM primitives

The Go program *observes* GC through `runtime`: finalizers, `KeepAlive`,
explicit `GC()`, weak pointers, memstats, heap profiles, and tuning knobs.
Decision per primitive (all rows are P2-M4 implementation work unless noted):

| Primitive | Vehicle | Stance |
|---|---|---|
| `runtime.SetFinalizer` | **single shared** `java.lang.ref.Cleaner` (one thread → restores gc's serialized finalizer execution) | emulate, with accepted divergences (below) |
| `runtime.KeepAlive` | `Reference.reachabilityFence(x)` at the intrinsic | emulate |
| `runtime.GC()` | `System.gc()` + poll GC MXBeans until collection counts advance, bounded timeout; on timeout (explicit GC disabled) return with the divergence documented | documented approximation — callers must not depend on reclamation having completed |
| `weak.Pointer` (Go ≥1.24) | `java.lang.ref.WeakReference` | emulate (frontend scope permitting) |
| `runtime.ReadMemStats` | field-by-field table (P2-M4 deliverable): exact / approximated-with-mechanism / unavailable-with-justification per `MemStats` field | emulate where mappable; the rest → `DIVERGENCES.md` |
| `runtime/pprof` heap profiles | JFR allocation profiling / heap dump | stance TBD in P2-M4 (emulate or unsupported-with-justification) |
| `debug.FreeOSMemory()` | no JVM analog (`System.gc()` is not it) | unsupported-with-justification (default stance; P2-M4 confirms) |
| `GOGC` / `debug.SetMemoryLimit` / `SetGCPercent` | no JVM analog | P2-M5 GODEBUG registry: `unsupported-with-justification` or `no-op` per SPEC §4.5 ("the JVM's GC *is* our GC") |

**`SetFinalizer` accepted divergences** (must be explicit rows, not implied):
- *Resurrection impossible:* gc hands the finalizer the object (may keep it
  alive, observable); a `Cleaner` action is deliberately denied the referent.
- *Finalizer thread identity:* the action runs on the Cleaner's JVM thread,
  not a goroutine — finalizer bodies touching scheduler state fail-fast;
  constrain finalizer bodies, or route them through a scheduler trampoline so
  they run as real goroutines (P2-M4 decides; default: constrain +
  document).
- *Removal:* `SetFinalizer(x, nil)` needs explicit deregistration (Cleaner
  has no unregister — wrapper with a cancelable registration).
- *Residual risk (exporter mapping):* the Java object registered with the
  Cleaner must stay JVM-reachable for exactly as long as Go considers the
  value live — if the exporter boxes/copies such that the wrapper dies early,
  finalizers fire prematurely. The spike cannot catch this (it never goes
  through an exporter). The P2-M4 differential corpus must include a
  finalizer-lifetime test driven through the real `runtime.SetFinalizer`
  exporter, including a remove-and-replace case.
- *`unsafe` interaction:* objects reachable *only* via `UnsafeHandles`
  entries are never collected (strong permanent roots) — `SetFinalizer` on
  such an object silently never runs. P2-M4 records the decision in
  `DIVERGENCES.md` (weak handle entries vs documented never-run).

**Finalizer-ordering note:** Go does not order finalizers of unrelated
objects; JVM `Cleaner` actions have the same non-guarantee — no observable
divergence.

---

## Falsifiers and re-test triggers

- **F1 (layout):** a Go semantic lands that requires object-layout or
  allocation behavior the JVM GC cannot provide (e.g., Go strengthens
  finalizer-ordering guarantees beyond what `Cleaner` offers; true
  raw-address arithmetic escapes the handle table — already a SPEC §3.4
  falsifier for unsafe v1). Then re-open D1.
- **F2 (pauses):** p99/max GC pause — measured via JFR `jdk.GCPhasePause` /
  GC-MXBean notifications on the conformance corpus — breaks the scheduler
  pause budget derived from GG-FAIR-0002, *and* no supported
  collector/tuning closes it → re-open D1. (The old remedy clause,
  "a segregated region would fix it", was incoherent: under D1 all Go values
  live in the JVM heap either way; coherent remedies are collector
  choice/tuning or allocation reduction.)
- **F3 (barriers):** any new store path that bypasses JVM barriers — the
  `make check` grep gate must catch it; if it doesn't, the gate is fixed
  first (escape protocol). The GC-visibility differential is the semantic
  backstop.
- **F4 (roots):** a standing GC-torture differential — weakref-verified
  survival of graphs retained only via parked-goroutine stacks across *all*
  park kinds (channel/timer/select/quantum/IO-enter), run per supported
  collector — whose failure re-opens D2. New park paths must preserve the
  token-before-park funnel invariant (code-review checklist item).
- **Re-test triggers:** new Go release changing finalizer/weak-pointer
  semantics; change of supported collector set; any new `unsafe` surface;
  **cgo/FFM boundary landing** (any design letting native code retain a Go
  reference across a call — breaks D2's all-references-are-JVM-references
  invariant; P2-M7's pinning audit does not cover root visibility).

**Supported collector decision:** default = the JVM default (G1 on GraalVM
25); expected-to-work set = {G1, ZGC}. Shenandoah is excluded: GraalVM
25.0.4.1.1's JIT does not support it ("Shenandoah garbage collector is not
supported by Graal"). Verified 2026-10-05: G1 124/124 PASS, ZGC 124/124 PASS.
(Was: no collector choice recorded anywhere — reviewer 1 P1-5.)

## Consequences

- P2-M4 implementation = D4 surface (per the corrected table above) +
  D2/D3 verification (F4 torture differential; GC-visibility differential
  per value kind; `make check` grep gate) + the `ReadMemStats` field table.
  No allocator, no marker, no stack maps.
- The 10 ms quantum-park path gains zero per-park GC bookkeeping (D2
  rejects cooperative publication) — no new overhead on the hot path.
- `GOMAXPROCS=1` remains observationally identical to Phase 1 (no GC
  behavioral change for single-threaded programs beyond the new intrinsics).
- When promoting the spike's no-over-retention check to a regression test,
  assert against a **named retention policy** ("dead goroutine stacks
  releasable within N ms of exit") and cover accumulation with a soak
  differential vs gc — the spike's ≤20 spin-GCs falsify permanent
  retention only.

## P2-M4 closeout checklist (blocking — sign-off requires all three with evidence files)

1. D4 surface implemented per the corrected table (SetFinalizer incl.
   remove/replace + exporter-lifetime test; KeepAlive litmus under C2;
   GC() MXBean-poll; weak.Pointer; ReadMemStats field table; pprof +
   FreeOSMemory stances). Evidence: implementation + differential tests.
2. Root/barrier differential suite green vs gc (F4 torture across park kinds
   × collectors; GC-visibility per value kind; finalizer-lifetime through
   the exporter; soak differential). Evidence: suite log + corpus JSON,
   0 unexplained divergences.
3. Off-heap grep gate landed in `make check` and green (with per-pattern
   rationale comments). Evidence: `make check` log.

### Scope amendment (2026-10-05, P2-M4 implementation)

The following D4 items are **deferred** from P2-M4 Go-observability (not
from implementation — the Java vehicles exist and are unit-tested):

- **`weak.Pointer` Go call path** (`weak.Make`/`Value`): deferred. The
  exporter does not lower the `weak` package; wiring it requires frontend
  work beyond P2-M4's GC-interop scope. ADR D4 hedged with "frontend scope
  permitting". The `newWeakRef`/`weakRefValue` vehicles are implemented and
  unit-tested (`GcInteropTest.testWeakRefLifecycle`) for future wiring.
- **`runtime.ReadMemStats` Go call path**: deferred. The synthetic `runtime`
  package does not declare it; wiring requires exporter + node work. The
  `memStats()` field table is implemented and unit-tested
  (`GcInteropTest.testMemStatsFields`) for future wiring.

Rationale: P2-M4's blocking goal is the GC-interop *mechanism* (roots,
barriers, finalizer lifetime). The `weak`/`ReadMemStats` *syntax* is
frontend work that does not affect the GC design. Deferring the call paths
does not weaken the F1–F4 falsifiers.

This amendment is recorded here explicitly (not smuggled into a
DIVERGENCES.md footnote) per the review gate requirement.
