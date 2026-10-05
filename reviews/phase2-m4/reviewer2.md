# Reviewer 2 — P2-M4 ADR-0002 (methodology & evidence honesty)

**Reviewer:** independent reviewer 2 of 2 · **Date:** 2026-10-05
**Scope:** `phases/phase2/adr/ADR-0002-gc-interop.md` (status: proposed),
`/tmp/gcspike/Spike.java` (+ archived copy
`phases/phase2/hidden_files/p2-m4-spike/`), PLAN.md §3 `[OPEN] GC interop heap
layout` and §4 P2-M4 acceptance.
**Mode:** read-only; no repo file modified. Spike independently re-run.

## Verdict: GO-WITH-FINDINGS

The core decisions (D1–D3) are sound, the spike is genuinely falsifiable
(verified, not assumed — see V1/V2 below), the barrier audit greps check out,
and the evidence is durably archived with a working re-run recipe. The ADR is
an honest design resolution, not a scope dodge. But the D4 emulation table
contains three overclaims/aspirations stated as settled fact
(`runtime.GC()` blocking, `KeepAlive` vehicle, `ReadMemStats` mapping), the
`[MEASURED]` tags overreach what the spike exercised, and — most important
procedurally — P2-M4 must not be closed on ADR sign-off alone: every
acceptance item beyond the decision itself is still future work. All six P1s
must be remediated (concrete targets below) before P2-M4 implementation
proceeds / before milestone sign-off.

## Independent verification performed

- **V1 — re-run from the archived artifact** (`hidden_files/p2-m4-spike/Spike.java`,
  md5-identical to `/tmp/gcspike/Spike.java`): `javac` + `java
  --enable-native-access=ALL-UNNAMED -Xss32m` on GraalVM 25.0.4.1.1 with
  `CP=phase2/out/classes:phase1/out/classes:phase1/lib/*`. Result: **7/7
  checks PASS** (`parked-roots: 2020 ms, finalizer: 1752 ms, SPIKE PASS`).
  The README re-run recipe works verbatim. Check count confirmed: 5 in
  `testParkedRoots` + 2 in `testFinalizer` = 7. `~20MB` confirmed:
  20,000 × 1024 B.
- **V2 — negative control (falsifiability proof):** built a variant where the
  goroutine does *not* stash `graph`/`leaf` in `box` and never touches them
  after the park (no post-park use → JIT may drop the locals). The
  "root survived GC while parked" check **correctly FAILED**
  (`AssertionError: SPIKE FAIL`). The test is non-vacuous: it detects absence
  of the root. The parked-root liveness claim is a real experiment, not a
  tautology.
- **V3 — barrier audit re-run:** `grep -rn allocateMemory` over
  `phases/phase1/src/main/java` + `phases/phase2/src/main/java` → 0 hits;
  `sun.misc.Unsafe|jdk.internal|MemorySegment|Arena` → exactly 1 hit, a
  `README.md` under `src/main/java` (not a source file) — matches the ADR's
  "zero hits in sources (only a README mention)". Verified.
- **V4 — mechanism check:** `makeChan(8, 0)` is `(elemSizeBytes=8,
  capacity=0)` → genuinely unbuffered. Park path is real:
  `ChanOps.recv` → `SchedulerCore.parkCurrent` → `awaitAck` →
  `LockSupport.park()` on the goroutine's vthread. Waiter chain
  `GoChan.recvWaiters → ChanWaiter → GoroutineHandle.thread` exists in the
  Phase-2 sources (ADR says `RecvWaiter`; the class is `ChanWaiter` — naming
  nit only).
- **V5 — D4 grounding:** repo-wide search finds **no** `KeepAlive` exporter
  and **no** `reachabilityFence` usage anywhere — the D4 table describes
  intended design, not current behavior.

## Findings

### P1-1 — Cleaner negative check validates the JDK, not the graalgo invariant

The spike's negative check ("finalizer did NOT run while object still
reachable") holds the object in a Java local of the test method — i.e., it
re-verifies `java.lang.ref.Cleaner`'s own contract, which was never in doubt.
The actual design risk for `runtime.SetFinalizer` emulation is the
**exporter mapping**: the Java object registered with the `Cleaner` must stay
JVM-reachable for exactly as long as the Go program considers the value live.
If the exporter boxes, unboxes, or copies the value such that the registered
wrapper can die early while Go still observes the value, finalizers fire
prematurely — and no check in this spike can catch that, because the spike
never goes through an exporter. Related unaddressed semantics:
`SetFinalizer(x, nil)` removal (Cleaner has no unregister — needs a wrapper
with explicit deregistration), and resurrection/re-registration behavior.
**Remediation target:** ADR-0002 D4 row must record these as explicit residual
risks / implementation requirements (not implied by the spike); the P2-M4
differential corpus must include a finalizer-lifetime test driven through the
real `runtime.SetFinalizer` exporter, including a remove-and-replace case.

### P1-2 — `KeepAlive` row is aspirational and names an insufficient vehicle

"intrinsic taking the value (live through the call)" asserts a guarantee the
design does not establish. A no-op `static void KeepAlive(Object x)` does
**not** guarantee liveness: after inlining, the JIT may consider `x` dead at
any point where it is no longer used — this is precisely why
`Reference.reachabilityFence` exists. The ADR never mentions
`reachabilityFence`. V5 confirms no exporter exists yet, so the row is pure
aspiration stated in implementational language.
**Remediation target:** ADR-0002 must name `Reference.reachabilityFence`
(or an equivalent Truffle-level fence with a liveness argument) as the
vehicle; P2-M4 implementation must add a KeepAlive litmus differential test
(object must survive between last-use and the KeepAlive call site under C2
compilation — the classic reachabilityFence test), run against gc behavior.

### P1-3 — `runtime.GC()` "blocks until complete" overclaims `System.gc()`

The ADR's parenthetical — "a hint in gc too; blocks until complete when
explicit GC is enabled" — misstates the contract. `Runtime.gc()`'s
specification is **best-effort** ("the Java Virtual Machine has made a best
effort to reclaim space"), not completion. Actual behavior is flag- and
collector-dependent: a no-op under `-XX:+DisableExplicitGC`, and concurrent
collectors / `-XX:+ExplicitGCInvokesConcurrent` need not wait for cycle
completion. Go's `runtime.GC()` guarantees blocking until the *current* GC
cycle has completed — strictly stronger. This matters directly for the
acceptance criterion "barrier/root differential vs gc": any differential test
that calls `runtime.GC()` and then asserts reclamation/finalizer progress
will be flaky or wrong under `System.gc()`.
**Remediation target:** rephrase the D4 row as a documented approximation
with the precise divergence ("post-`GC()` heap state is not guaranteed;
callers must not depend on reclamation having completed"); the differential
corpus must not assert post-`GC()` reclamation.

### P1-4 — `ReadMemStats` row punts the actual decision

"JVM `MemoryMXBean`/GC MXBeans mapped onto `MemStats` fields … documented
approximation" names no mapping. A large fraction of `runtime.MemStats` has
**no** MXBean analog: `PauseNs[256]` ring (no per-pause history from
MXBeans), `BySize[61]`, `StackInuse`/`StackSys` (vthread stacks are heap
objects), `MSpan*`/`MCache*`, `GCSys`, `LastGC` (not directly exposed).
Mappable: `HeapAlloc`≈heap used, `HeapSys`/`Sys`≈committed, `NumGC`≈sum of
collection counts, `PauseTotalNs`≈sum of collection times (ms-granular),
`NumForcedGC`≈counted explicit calls. "Documented approximation" is doing all
the work of a decision the ADR defers.
**Remediation target:** P2-M4 implementation must publish the
field-by-field mapping table; every unmappable field gets an explicit
zero/unsupported entry recorded in `DIVERGENCES.md` (per the P2-M10
divergence-list discipline), not silently left.

### P1-5 — `[MEASURED]` tags overreach what the spike exercised

The spike exercises **Java locals on a vthread parked via channel `recv`,
on the default GC configuration**. The ADR's D2 chain claims the
measurement covers "Java locals/**Truffle frames**" and asserts root
enumeration "exact by construction" across "G1/**ZGC/Shenandoah** … as
configured". Truffle frames (materialized or interpreter-loop) were never
exercised — the spike drives `SchedulerCore`/`ChanOps` directly, no Truffle
node runs. Non-default collectors were not tested. The decision (JVM roots)
is still the right one, but the evidence tags claim more than was run.
**Remediation target:** scope the `[MEASURED]` tags to what ran (default-GC,
Java-local roots, channel-waiter park path); add Truffle-frame root coverage
to the D2 verification backlog for implementation; keep the existing
"default-JVM-GC change" re-test trigger (it already exists — do not let it
be dropped as redundant).

### P1-6 — P2-M4 must not close on ADR sign-off alone (procedural)

PLAN.md §4 acceptance: "design spike + ADR first …; exact roots +
Truffle-level write barriers; barrier/root differential vs gc". The ADR
resolves "exact roots" and "write barriers" **by decision** (JVM's, in both
cases — legitimately; see §"Scope-dodge assessment" below). But the
acceptance items that constitute *evidence* — the root/barrier differential
suite vs gc, the `make check` grep gate, the entire D4 surface — are all
explicitly future work ("implementation starts after review sign-off").
The ADR is honest about this; the risk is that the milestone gets signed off
when the reviews land, with the implementation evidence never gated.
Per the carried-over Phase-1 lesson ("no sign-off on stale numbers; every
number cites its evidence file"), a P2-M4 closeout entry citing only this
ADR would be a process escape.
**Remediation target:** the P2-M4 closeout checklist must name three
blocking deliverables with evidence files: (1) D4 surface implemented
(SetFinalizer/KeepAlive/GC/weak/memstats per the corrected table);
(2) root/barrier differential suite green vs gc; (3) the off-heap grep gate
landed in `make check` and green. Milestone sign-off is blocked until all
three cite evidence.

### P2-1 — "20 spin-GCs" is "up to 20, early-exit" (precision)

The no-over-retention loop is `for (i < 20 && refs-not-clear)` — it exits
early, so the evidence is "refs cleared within ≤20 spin-GCs (~4 s)", not
"20 spin-GCs". More substantively: the check falsifies *permanent*
retention of the dead goroutine's stack through the tested paths; it does
not bound heap growth over time (a slow-accumulating leak elsewhere would
not be caught), and a *bounded-delay* retention policy (e.g., a freed-stack
reuse cache) would false-fail it.
**Remediation target:** when promoting this to a regression test, assert
against a named retention policy ("dead goroutine stacks releasable within
N ms of exit"), and cover accumulation with a soak differential vs gc.

### P2-2 — `UnsafeHandles` table is a permanent root; finalizer interaction unnoted

The D3 audit notes `unsafe.Pointer` is a `HashMap<Long, Object>` — a strong,
permanent root for every handle-held object. Consequence the ADR never
states: objects reachable *only* via unsafe handles are never collected, so
`SetFinalizer` on such an object silently never runs. This is a real
Go-observable divergence (gc collects such objects once the last unsafe
use passes, modulo `KeepAlive`).
**Remediation target:** note the interaction in D4/falsifiers; implementation
must decide (document in DIVERGENCES.md): handle-table entries as
`WeakReference`s, or documented "finalizers on handle-held objects never
run".

### P2-3 — Grep gate miscategorizes `Unsafe.allocateInstance`

The ADR's regression guard bans "`Unsafe.allocateInstance` for value
storage" alongside off-heap allocators. `allocateInstance` creates an
ordinary **heap** object — every store into it passes through JVM barriers,
so it is irrelevant to barrier integrity. Banning it may be fine hygiene
(uninitialized instances), but listing it under a *barrier* gate teaches the
wrong invariant to future readers.
**Remediation target:** split the gate rationale: off-heap allocators
(`allocateMemory`, `MemorySegment`, `Arena`) = barrier bypass; anything else
banned goes under a separate, correctly-labeled rule.

### P2-4 — ADR cites the ephemeral spike path as canonical

"Spike evidence: `/tmp/gcspike/Spike.java` (throwaway…)" — the evidence has
in fact been archived at `phases/phase2/hidden_files/p2-m4-spike/`
(checksum-identical, re-run recipe verified working in V1). /tmp is wiped on
VM replacement.
**Remediation target:** one-line fix — cite the archived path as canonical;
keep /tmp mention as provenance only.

## Scope-dodge assessment (brief Q2)

"P2-M4 does not build a garbage collector" is a **legitimate resolution**,
not a dodge, for three reasons: (1) the PLAN.md §3 `[OPEN]` item posed the
barrier question as *where to insert it*, and "nowhere — there is no Go heap
for it to protect" is a valid answer to that question, backed by the audit;
(2) SPEC §3.4 already `[DECIDED]` all-Go-objects-as-Java-objects, which
makes a Go-level marker or Truffle-level barrier duplicated machinery by
construction; (3) PLAN.md explicitly orders "design spike + ADR first, not
code" — this ADR *is* that step. Demanding a Truffle-level barrier anyway
would be process theater: a barrier with no remembered set and no heap to
remember into. The falsifiers (F1–F3) and re-test triggers are the right
shape for a decision of this kind. The genuine risk is procedural only
(P1-6): the decision must not be mistaken for the milestone's completion.

## What remains for P2-M4 to be genuinely done

Per the ADR's own Consequences section (honest) and P1-6 above: (1) D4
surface implementation with the P1-1…P1-4 corrections; (2) root/barrier
differential suite vs gc (must include finalizer-lifetime-through-exporter,
KeepAlive C2 litmus, and a soak test — not just the spike's scenarios);
(3) the `make check` grep gate (with the P2-3 relabeling). None of these
exist yet; the spike + ADR are the design gate, correctly positioned
*before* P2-M8's torture re-run per PLAN.md §7.

## Minor notes (no action required)

- "no over-retention" spin loop: 20 iterations × (System.gc() + 200 ms) is
  adequate for a spike; the failure mode of a too-short window is a
  false *failure*, not a hidden pass — the check errs loud, which is the
  safe direction.
- The leaf-weakref check adds marginal value over the root check on HotSpot
  (transitive tracing holes are not a realistic failure mode), but it is
  non-tautological and cheap — keep it.
- `forceGc()`'s 8 MB pressure allocation is a reasonable hedge against lazy
  collectors; fine for a spike, should not be cargo-culted into the
  regression suite without review.
