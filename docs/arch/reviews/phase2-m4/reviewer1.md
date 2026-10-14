# Independent review — ADR-0002 (Phase-2 GC interop), reviewer 1 of 2

**Reviewer:** reviewer 1 (architecture correctness) · **Date:** 2026-10-05
**Scope:** `phases/phase2/adr/ADR-0002-gc-interop.md` + `/tmp/gcspike/Spike.java` +
SPEC.md §3.4 + PLAN.md §3 `[OPEN]` / §2 WS D. Read-only review; no repo file modified.
**Verdict: GO-WITH-FINDINGS** — the three core decisions (D1/D2/D3) survive adversarial
attack, but D4's emulation table hand-waves four real semantic divergences and the
falsifier set is missing a D2 falsifier while F2's remedy clause is incoherent. None of
the findings overturns D1–D3; all have concrete remediation targets below.

## Method (independent, not re-reading the ADR's claims)

1. **Spike re-run from source.** Recompiled `/tmp/gcspike/Spike.java` myself with
   `~/tools/graalvm-jdk-25/bin/javac` into `/tmp/gcspike/recheck/` and ran it first on
   the classpath against the current `phases/phase2/out/classes` +
   `phases/phase1/out/classes` + phase1 lib jars:
   `java --enable-native-access=ALL-UNNAMED -Xss32m -cp … Spike`. Result: **7/7 pass**
   (parked-root survival, deep-leaf survival, no over-retention after exit, finalizer
   run-once-after-unreachability, no premature finalization). The 7/7 claim is confirmed.
2. **D3 audit re-run from scratch**, extended beyond the ADR's patterns: `allocateMemory`
   (0 hits), `sun.misc.Unsafe|jdk.internal|MemorySegment|Arena` (0 hits in `src/`; the
   single hit is `phases/phase1/.../launcher/README.md`, a prose mention — consistent
   with the ADR), plus `ByteBuffer|allocateDirect|MappedByteBuffer|VarHandle`
   (0 hits), `theUnsafe`-style reflective acquisition (0 hits), bit-trick smuggling
   (`doubleToRawLongBits` etc. — all legitimate float/int scalar work in
   `ConstNode`/`Boxing`/`GoFloatFmt`/`Intrinsics`), and long→Object reconstitution
   (none). Carrier audit: every reference-typed `GoKind` routes to `Object[]`/object
   fields — `GoStructDescriptor:66` classifies `inLongs = isIntKind()||isFloatKind()||BOOL`
   (all scalar; `uintptr`→`UINT64` scalar is correct; `unsafe.Pointer`→`UNSAFE_POINTER`
   is objects-carried), `GoStruct` enforces the split with throw-on-mismatch accessors,
   and `GoPointer`/`GoInterface`/`GoFunc`/`GoSlice`/`GoMap`/`GoString` hold references
   only in `Object`/typed-array fields. `UnsafeHandles` is a `HashMap<Long,Object>` —
   JVM heap, strongly held, barriered, no smuggling. **The D3 audit reproduces clean.**
3. **D2 chain audit.** Traced every park path: the only `LockSupport.park()` site in
   phase2 main is `SchedulerCore.java:273` (inside `awaitAck`); both callers
   (`parkCurrent`, `quantumPark`) enqueue a `ResumeToken` with a **strong final**
   `handle` field to the drainer *before* parking. So a parked goroutine's reference
   chain is *structurally* drainer-queue → token → `GoroutineHandle` → `Thread` →
   stack chunks, for **every** park kind (channel, timer, select, quantum, IO) — the
   waiter-queue chains the ADR describes are secondary. `close()` unparks (token holds
   handle) before `recvWaiters.clear()` — no window with an unreferenced parked
   vthread. No JNI/native methods in the tree; `synchronized` appears only in the
   drainer (platform thread), never on a goroutine park path. `GoroutineState.HOLDER`
   is a `ThreadLocal` (a GC root via the thread) and is `remove()`d on exit; the
   exit-time `h ↔ thread ↔ lambda` reference cycle is collectable (tracing handles
   cycles) — consistent with the spike's no-over-retention check.

## Findings

### P1-1. D4 `SetFinalizer` vehicle ("Cleaner per finalizable value") diverges from gc in three observable ways the table does not admit
gc serializes **all** finalizer execution on a single dedicated finalizer goroutine;
`Cleaner.create()` spawns **one thread per Cleaner instance**, so per-value Cleaners
execute finalizers **concurrently** — finalizers that communicate (even via a shared
counter) observably diverge from gc. Second, the cleanup action runs on a JVM Cleaner
thread, **not a goroutine**: a finalizer body that touches scheduler state (channel
ops → `requireHandle()` fail-fast) or expects goroutine semantics breaks. Third,
**resurrection is impossible**: gc hands the finalizer the object, which may keep it
alive (legal, observable); a `Cleaner` action is deliberately denied the referent.
The ADR's "finalizer-ordering note" addresses only ordering (where its
same-non-guarantee reading is fair) and misses all three.
**Remediation target:** P2-M4 D4 implementation section — (a) mandate a **single shared
`Cleaner`** (restores serialization); (b) add explicit accepted-divergence rows for
resurrection-impossible and finalizer-thread-identity (constrain finalizer bodies, or
route them through a scheduler trampoline so they run as real goroutines); the current
"emulate" stance is unsound without these rows.

### P1-2. D4 `runtime.GC()`: "blocks until complete when explicit GC is enabled" is collector-dependent and unevidenced
`System.gc()` blocking-until-complete holds for STW collections, not for concurrent
ones: on ZGC/Shenandoah (both named in the ADR as "as configured") `System.gc()`
initiates a concurrent cycle and returns. The ADR asserts a semantic it has no
mechanism for, and no supported-collector set is defined anywhere (see P2-2), so the
qualifier "when explicit GC is enabled" is doing unverified work. A test doing
`GC()` then reading weak refs / memstats diverges by collector.
**Remediation target:** D4 table row — either implement wait-for-completion (e.g.
`System.gc()` + poll GC MXBeans until collection counts advance, with timeout), or
downgrade the row to "non-blocking hint, documented approximation" and say so.

### P1-3. D4 `ReadMemStats`: "mapped onto MemStats fields" with no field-by-field table hides unmappable fields
`PauseNs[256]` (the 256-pause ring — observable, used by real latency trackers),
`HeapObjects`, `BySize`, and cumulative `TotalAlloc` have **no** `MemoryMXBean`/GC-MXBean
analog. (`PauseNs` is emulable via a `NotificationListener` ring on GC MXBeans;
`HeapObjects`/`BySize` are not.) The row's "documented approximation" cannot be
evaluated — or differentially tested — without the mapping.
**Remediation target:** P2-M4 must publish the field table (exact / approximated-with-
mechanism / unavailable-with-justification per `MemStats` field) as part of the D4
implementation; the ADR should require it.

### P1-4. D4 surface enumeration is incomplete: heap profiles and `debug.FreeOSMemory` are missing
`runtime/pprof` heap profiles (`MemProfileRate`, `MemProfile`) are a Go-observable GC
surface with no row in the table; `debug.FreeOSMemory()` has no JVM analog either.
Both need explicit stances (emulate via JFR allocation profiling / heap dump, or
unsupported-with-justification) — the table as written implies a completeness it does
not have.
**Remediation target:** D4 table — add both rows before P2-M4 implementation starts.

### P1-5. F2's remedy clause is incoherent and it names no measurement vehicle
"JVM GC pauses break the scheduler's latency/overhead budget (GG-FAIR-0002) *and*
measurements show a segregated region would fix it": a segregated *Go-value* region
cannot fix pauses — under D1 all Go values live in the JVM heap either way, and
scheduler structures (drainer queues, tokens, handles) are JVM-heap objects regardless.
The coherent remedies for a pause problem are collector choice/tuning (ZGC/Shenandoah)
or allocation reduction, and **no collector choice is recorded anywhere** — D1 says
"the JVM GC is the Go GC" without saying which one. F2 also never says how the trigger
would be measured.
**Remediation target:** rewrite F2 as "p99/max GC pause (measured via JFR
`jdk.GCPhasePause` / GC-MXBean notifications on the conformance corpus) breaks the
scheduler pause budget derived from GG-FAIR-0002, *and* no supported collector/tuning
closes it → re-open D1"; record the default collector decision in the ADR (or an
explicit `[OPEN]`).

### P1-6. F3's grep gate is necessary but insufficient in two directions
(a) The pattern list misses shapes: reflective `Unsafe` acquisition
(`getDeclaredField("theUnsafe")` need never spell `sun.misc.Unsafe` outside a string
literal the current patterns *do* catch only if fully qualified — add `theUnsafe`);
direct-`ByteBuffer` off-heap *Go* storage (`allocateDirect` — not a reference-barrier
hole since byte buffers can't hold refs, but it *is* off-heap Go-object storage under
D1 and invisible to memstats accounting). (b) No grep can catch a **logic** error:
a future value carrier that misclassifies a reference kind into a primitive array is
invisible to string patterns. The ADR already promises "barrier/root-correctness
differential vs gc" in Consequences — but F3's text leans entirely on the grep gate.
**Remediation target:** extend the gate patterns (`theUnsafe`, `allocateDirect`, ban
`import sun.misc.*`/`jdk.internal.misc.*` outright); wire the promised GC-visibility
differential (allocate graph → retain *only* through the carrier under test → force GC
→ weakref-verify survival, per value kind) to F3 explicitly as the semantic backstop.

### P1-7. There is no D2 falsifier — the "exact by construction" claim has no standing tripwire
F1 covers layout, F2 perf, F3 barriers. Nothing re-opens D2 if a program-reachable Go
reference proves invisible to the JVM GC (premature collection) — e.g. a future park
path that bypasses the token funnel (my audit shows all *current* paths funnel, but
that invariant is not stated, let alone guarded), or a JDK upgrade changing
vthread stack-chunk tracing.
**Remediation target:** add **F4 (roots):** a standing GC-torture differential —
weakref-verified survival of graphs retained only via parked-goroutine stacks across
*all* park kinds (channel/timer/select/quantum/IO-enter), run per supported collector —
whose failure re-opens D2. Assert the funnel invariant (token-before-park) in code
review checklists for any new park path.

### P2-1. D2's argument overclaims spike coverage; the stronger argument is unstated
The ADR attaches "`[MEASURED]`" to the channel-recv park path only, then generalizes
to "vthread stack chunks … spike-verified". The generalization is actually *true*, but
its basis is not the spike — it is the structural funnel invariant from my audit
(single `LockSupport.park()` site; token holding the handle enqueued before every
park). The ADR should argue from the invariant, with the spike as an instance.
**Remediation target:** D2 section — state the invariant ("no park without a prior
handle-holding token enqueue; verified by the single park site at
`SchedulerCore.java:273`") and either extend the spike to quantum/timer/select parks
or cite the invariant as the general case.

### P2-2. "Supported configurations" is undefined; the spike ran on one collector
D2 says "on the supported configurations" and D3 names "G1/ZGC/Shenandoah … as
configured", but no document enumerates which collectors (or JDK builds) graalgo
supports, and my spike re-run used the JVM default (G1). Root-enumeration behavior
(stack-chunk tracing, `System.gc()` semantics per P1-2) is collector-sensitive.
**Remediation target:** ADR or PLAN — enumerate supported collectors; re-run the spike
per collector; add collector change to the re-test triggers.

### P2-3. Re-test triggers do not name FFI/cgo landing
"Any new `unsafe` surface" does not obviously cover the future cgo/FFM boundary, where
native code holding raw pointers is precisely the shape that breaks D2's
all-references-are-JVM-references invariant. P2-M7's pinning audit is about *pinning*,
not root visibility — it will not catch this.
**Remediation target:** re-test triggers — add "cgo/FFM boundary landing (any design
letting native code retain a Go reference across a call)".

### P2-4. `KeepAlive` vehicle unnamed
"Intrinsic taking the value (live through the call)" is vague about defeating
dead-local elimination; the exact vehicle is `Reference.reachabilityFence`.
**Remediation target:** D4 table — name `reachabilityFence`.

### P2-5. Grep-gate rationale nit
`Unsafe.allocateInstance` is banned "for value storage", but `allocateInstance` creates
a *heap* object (no constructor run) — it is not a write-barrier bypass. Banning it is
harmless; the stated rationale is muddled and will confuse the next reader about what
the gate guards.
**Remediation target:** `make check` gate comments — one line per pattern stating what
it guards (off-heap storage / barrier bypass / raw-address access).

## What holds up (for the record)

- **D1 is sound.** The rejected alternative (segregated region) is correctly dismissed:
  it would duplicate allocator, stack maps, and marker for no correctness gain, and the
  spike confirms the JVM GC traces parked-goroutine graphs exactly with no
  over-retention. None of my D1 attacks (finalizer ordering/serialization, weak
  pointers, memstats, pacing, `GC()` blocking, heap profiles) overturns the heap-layout
  decision — they are D4 fidelity issues, which is exactly where the ADR puts them.
  The findings above make D4 honest; they do not move D1.
- **D2's core claim survives attack.** I found no path in the current tree where a
  program-reachable Go reference is invisible to the JVM GC: no JNI, no off-heap, no
  long-smuggled references, descriptor-enforced carrier split, ThreadLocals as proper
  roots, and the park funnel invariant (P2-1) is stronger than the ADR's own telling.
  Deciding D2 now is **not** premature w.r.t. the P2-M7 pinning audit — but only if
  P2-3's trigger is added, since FFI is the shape that could break it later.
- **D3's audit reproduces clean** with extended patterns, and the "no Truffle-level
  barrier" conclusion follows: every store is a JVM-heap store through JVM barriers.
- **F1 and F3 are honest** as far as they go; F3 needs the P1-6 hardening.

## Note for reviewer 2 (avoid overlap)

I covered architecture correctness per the brief. Suggested split: reviewer 2 takes
SPEC/PLAN cross-consistency (does WS D's "Go write barriers at the Truffle level"
wording in PLAN §2 contradict D3's "no Truffle-level barrier"? — it reads stale),
the GODEBUG-registry handoff to P2-M5, and whether the D4 rows are implementable
within P2-M4's milestone scope.
