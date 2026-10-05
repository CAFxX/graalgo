# ADR-0002 — Phase-2 GC interop: heap layout, roots, write barriers

**Date:** 2026-10-05
**Status:** proposed — under independent review (P2-M4 phase gate; implementation
starts after review sign-off, not after this file lands)
**Scope:** closes PLAN.md §3 `[OPEN] GC interop heap layout` ("the largest design
unknown in Phase 2"); workstream D (P2-M4).
**Normative basis:** SPEC §3.4 "Value model: Java objects throughout"
`[DECIDED]` (GG-0015, GG-0016); SPEC §4.5 GODEBUG contract (GG-0055, GG-0045).
**Spike evidence:** `/tmp/gcspike/Spike.java` (throwaway; 7/7 checks pass —
results recorded below, not the code).

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
  `GoChan.recvWaiters → RecvWaiter → GoroutineHandle.thread → vthread stack
  chunks → Java locals/Truffle frames` `[MEASURED]`;
- a `WeakReference` to a *deep leaf* of that graph also survives — the
  transitive closure is traced exactly, no holes `[MEASURED]`;
- after the goroutine exits, the graph is collected — scheduler, channel, and
  timer structures do not over-retain dead goroutine stacks (no leak)
  `[MEASURED]`.

A segregated region would additionally need its own allocator, stack maps,
and a full marker — all duplicated machinery the JVM already provides.

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

What P2-M4 must guarantee (and test) is that every Go reference the program
can observe is *reachable through JVM-traced references*:
- goroutine stacks: vthread stack chunks, held via `GoroutineHandle.thread`
  from waiter queues / the drainer's lanes — spike-verified `[MEASURED]`;
- globals: Phase-1 package-level variables live in Java statics/objects —
  traced as ordinary roots;
- channel/timer structures: `GoChan` buffers and waiter queues, `TimerHeap`
  entries hold Go values as Java references — traced as ordinary heap.

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

**Audit evidence** (mechanical, re-runnable):
- `grep -rn allocateMemory` over `phases/phase1/src/main` and
  `phases/phase2/src/main`: zero hits — no off-heap Go object storage;
- `grep -rln 'sun.misc.Unsafe\|jdk.internal\|MemorySegment\|Arena'`: zero hits
  in sources (only a README mention and the *name* `UnsafeEscapeException`);
- `unsafe.Pointer` is the `UnsafeHandles` handle table (`HashMap<Long,
  Object>`) per SPEC §3.4 — JVM heap, fully barriered.

**Rejected alternative A2 (Truffle-level barrier + Go remembered set):** a
barrier with no Go heap to remember into is pure overhead duplicating the
JVM's barriers. Rejected.

**Regression guard:** `make check` gains a grep gate — off-heap allocation
APIs (`allocateMemory`, `MemorySegment`, `Arena`, `Unsafe.allocateInstance`
for value storage) are banned in `src/`; any future store path that bypassed
JVM barriers fails the build loudly. (A reviewer asked "what if someone adds
one later" — this is the answer.)

## D4. Go-observable GC surface: emulated on JVM primitives

The Go program *observes* GC through `runtime`: finalizers, `KeepAlive`,
explicit `GC()`, weak pointers, memstats, and tuning knobs. Decision per
primitive:

| Primitive | Vehicle | Stance |
|---|---|---|
| `runtime.SetFinalizer` | `java.lang.ref.Cleaner` per finalizable value | emulate; spike shows run-once-after-unreachability + never-while-reachable `[MEASURED]` |
| `runtime.KeepAlive` | intrinsic taking the value (live through the call) | emulate |
| `runtime.GC()` | `System.gc()` (a hint in gc too; blocks until complete when explicit GC is enabled) | emulate, documented approximation |
| `weak.Pointer` (Go ≥1.24) | `java.lang.ref.WeakReference` | emulate (frontend scope permitting) |
| `runtime.ReadMemStats` | JVM `MemoryMXBean`/GC MXBeans mapped onto `MemStats` fields | emulate, documented approximation |
| `GOGC` / `debug.SetMemoryLimit` / `SetGCPercent` | no JVM analog | P2-M5 GODEBUG registry: `unsupported-with-justification` or `no-op` per SPEC §4.5 stance pattern ("the JVM's GC *is* our GC") |

Finalizer-ordering note: Go does not order finalizers of unrelated objects;
JVM `Cleaner` actions have the same non-guarantee — no observable divergence.

---

## Falsifiers and re-test triggers

- **F1 (layout):** a Go semantic lands that requires object-layout or
  allocation behavior the JVM GC cannot provide (e.g., Go strengthens
  finalizer-ordering guarantees beyond what `Cleaner` offers; true
  raw-address arithmetic escapes the handle table — already a SPEC §3.4
  falsifier for unsafe v1). Then re-open D1.
- **F2 (performance, Phase 5 not Phase 2):** JVM GC pauses break the
  scheduler's latency/overhead budget (GG-FAIR-0002) *and* measurements show a
  segregated region would fix it. Correctness does not depend on this.
- **F3 (barriers):** any new store path that bypasses JVM barriers — the
  `make check` grep gate must catch it; if it doesn't, the gate is fixed
  first (escape protocol).
- **Re-test triggers:** new Go release changing finalizer/weak-pointer
  semantics; change of default JVM GC; any new `unsafe` surface.

## Consequences

- P2-M4 implementation = D4 surface + D2/D3 verification (root-correctness
  and barrier-integrity differential tests vs gc) + the `make check` grep
  gate. No allocator, no marker, no stack maps.
- The 10 ms quantum-park path gains zero per-park GC bookkeeping (D2
  rejects cooperative publication) — no new overhead on the hot path.
- `GOMAXPROCS=1` remains observationally identical to Phase 1 (no GC
  behavioral change for single-threaded programs beyond the new intrinsics).
