# Track 2 — Industry JVMs: HotSpot, GraalVM, OpenJ9, Azul

**Mission:** what production JVMs do that stock Go gc fundamentally can't; judge what transfers to Go-on-Truffle.
Phase-5 rule: every claim must beat stock gc **with PGO** on a named benchmark, or it's struck.
Edges: (1) descriptor-based value model; (2) user-space goroutine scheduler w/ precise signals; (3) speculative JIT + guards/deopt; (4) always-on profiler; (5) three execution modes.

Do NOT re-propose (note complementary angles only): speculative interface devirtualization cascade; profile-guided struct field reorder/pack; speculative AoS→SoA; type narrowing + bit-packing; speculative data representation (map specialization, unboxing); small string optimization; dynamic per-site string interning; inferred store-once values; idle-time repack/defrag/shrink; adaptive concurrency primitives; vectorization; always-on profiling; partial escape analysis (graal-style); don't list gc basics (inlining, gc's escape analysis, bounds-check elimination).

> **Status:** drafting — incremental writes; see `## Draft log` at bottom for progress.

## HotSpot

### [ATTEMPTED] Lock evolution: what actually replaced biased locking

**Bucket: ATTEMPTED** for biased locking itself; **ABLE** for its replacements.
JEP 374 disabled+deprecated biased locking in JDK 15; the flags were obsoleted in JDK 18
([openjdk.org/jeps/374](https://openjdk.org/jeps/374);
[Oracle JDK 18 release notes](https://www.oracle.com/java/technologies/javase/18-relnote-issues.html)).
**Why it failed (the valuable part):** per JEP 374, biased locking's complexity was
invasive across the synchronization subsystem and other HotSpot components, while the
payoff shrank — uncontended `synchronized` is a small fraction of most workloads, and
revocation safepoints on contention were expensive. The lesson: per-thread bias *state*
with global revocation doesn't pay.
**Our architecture routes around it:** we don't need bias state at all — a speculative
JIT (edge 3) can emit an uncontended-mutex fast path under a guard and deopt on
contention, getting the biased-locking benefit with zero revocation machinery.
What replaced it (**ABLE**) is a two-level scheme documented in JEP 450: **lightweight locking**
(atomic flip of header tag bits `01`→`00`, no extra data structures, no stack) for the
uncontended/no-wait/no-JNI case, falling back to **monitor locking** (`10`) on contention
([openjdk.org/jeps/450](https://openjdk.org/jeps/450), "Locking" section). Legacy
stack-locking (copy header to thread stack) was dropped as incompatible with compact
headers — itself a mini-ATTEMPTED. The Loom-era follow-up is JEP 491 (JDK 24): monitors were re-associated from the
carrier thread to the virtual thread so `synchronized` no longer pins
([Oracle JDK 24 release notes](https://www.oracle.com/java/technologies/javase/24all-relnotes.html)).

- **Transfer verdict:** mostly *negative*. Go's `sync.Mutex` fast path is already an
  atomic-CAS fast path — the direct analog of HotSpot's lightweight locking — and
  goroutines never pin an OS thread across a mutex wait, so the JEP 491 class of fix has
  no analog problem. What does NOT transfer: biased locking itself (Go has no object
  header to bias, and per-goroutine bias revocation was the complexity that killed it —
  a cautionary tale for any per-goroutine representation bias in our descriptor model).
- The surviving compiler-side idea — **lock coarsening** (merge adjacent same-object
  critical sections) and lock elision via escape analysis — is covered under Graal below
  (GR-79526).

### [ABLE] C2 loop optimizations: strip mining, range-check smearing, superword

- **Loop strip mining** (`-XX:LoopStripMiningIter`, `UseCountedLoopSafepoints`): C2 splits a
  counted loop into outer strips of ~1000 iterations with the safepoint poll on the outer
  back-edge, so the inner loop runs poll-free. Evidence of it being load-bearing:
  franz1981's JMH reproducer showed block-frequency ratings 39.8 vs 5.8 and ~9%
  throughput swings (8151 vs 7453 ns/op) depending on whether strip mining was on
  ([github.com/franz1981/c2-writestring-spill-jmh](https://github.com/franz1981/c2-writestring-spill-jmh)).
  **Transfer verdict: near-miss (ABLE, not a candidate).** Go's async preemption (signal-based, stack-growth
  checks) is not safepoint-poll-based in the same way; strip mining optimizes a cost our
  runtime doesn't pay in the same form. No named-benchmark win vs gc+PGO is plausible.
- **Range-check smearing** (`ifnode.cpp`, `allow_range_check_smearing`): a dominated range
  check is removed when a series of dominating checks cover its range; deliberately
  delayed until after loop opts so RCE gets first chance
  ([builds.shipilev.net ifnode.cpp](https://builds.shipilev.net/patch-openjdk-valhalla-lworld/src/hotspot/share/opto/ifnode.cpp.rhs.html)).
  **Transfer verdict: subsumed (ABLE)** — gc already does bounds-check elimination (excluded
  from this survey); smearing is a subsumed detail of that.
- **Superword auto-vectorization**: C2's SLP pass vectorizes across *unrolled* loop
  bodies (`superword.cpp`); it interacts with strip mining (JDK-8247838 crash: null
  `_next` after strip mining — [bugs.java.com](https://bugs.java.com/bugdatabase/view_bug?bug_id=8247838)).
  **Transfer verdict: complementary angle (ABLE) for the backlog vectorization track** — two
  process notes: (1) vectorize over the unrolled body shape, not the source loop shape;
  (2) the vectorizer and the loop-restructuring passes must be tested *jointly* (their
  interaction was a real crash bug).

### [ABLE] C2 escape analysis + scalar replacement (Choi99)

HotSpot's EA is *flow-insensitive* ([Choi99], per
[Oracle's performance-enhancements doc](https://docs.oracle.com/en/java/javase/11/vm/java-hotspot-virtual-machine-performance-enhancements.html)):
`NoEscape` → scalar replacement, non-`GlobalEscape` → lock elision; it does *not* do
stack allocation. **Transfer verdict: subsumed (ABLE).** gc's EA is comparable/better, and
Graal-style *partial* EA (sinking allocations into cold branches) is already in the
backlog — this survey adds nothing over either.

### [ABLE] Barrier designs (G1 late expansion, ZGC load barriers, Shenandoah, C4 LVB)

- JEP 475 (JDK 24) moved G1 barrier expansion from early to late in the C2 pipeline to
  simplify the implementation
  ([Oracle JDK 24 release notes](https://www.oracle.com/java/technologies/javase/24all-relnotes.html)).
  Design note worth stealing for our GC work, not a program optimization.
- ZGC: colored pointers (4 metadata bits) + load barrier that tests color and
  self-heals on the slow path; generational mode is default since JDK 23.
- Shenandoah: SATB + Brooks forwarding pointers (read barrier per access — Shipilëv's
  observation: "RBs are cheap, but there are lots of them; the observed overhead depends
  heavily on the optimizer's ability to eliminate, hoist and coalesce barriers"
  ([shipilev.net](https://shipilev.net/talks/devoxx-Nov2017-shenandoah.pdf))).
- Azul C4: Loaded Value Barrier — read barrier *immediately after the reference load*,
  before any use/propagation, which gives it access to the source address and enables
  self-healing; claimed "significantly more efficient" than Brooks/Baker barriers —
  **vendor claim** ([azul.com C4 paper](https://www.azul.com/sites/default/files/images/c4_paper_acm.pdf)).
- **Transfer verdict: GC-subsystem, not Phase-5 program optimizations.** None of these
  beats gc+PGO on a *named program benchmark*; they are runtime choices. The one
  transferable lesson (for whoever owns GC barriers): co-design the barrier with the
  JIT's ability to sink/hoist/eliminate it — a barrier the compiler can't optimize
  around is a throughput tax, and Shipilëv's slide is the canonical statement of that.

## GraalVM

### [THINKING] Cross-run speculation-profile cache (ReadyNow-style)

**Bucket: ABLE** (precedents shipped) with a **THINKING** tail (JEP 544 — plan around it).
Idea (≤3 lines): persist the always-on profiler's per-site speculation decisions
(monomorphic/polymorphic shapes, value ranges, representation stability) across process
runs in a version-keyed cache. On startup the speculative JIT arms day-0 compilations
with yesterday's speculations instead of re-warming; a guard+deopt fallback covers
behavior drift.
Precedent (citable): OpenJ9's shared-class-cache AOT + JITServer server-side AOT cache
(`-XX:+JITServerUseAOTCache`; methods serialized, cached, reused across clients —
[eclipse.dev/openj9/docs/jitserver](https://eclipse.dev/openj9/docs/jitserver/)); Azul
ReadyNow "reuses compiler profiling from previous runs" (Azul Platform Prime materials —
**vendor claim**); HotSpot Leyden JEP 483 (AOT class loading & linking, JDK 24), JEP 515
(AOT method profiling, JDK 25), JEP 516 (AOT object caching, JDK 26) delivered
([openjdk.org/projects/leyden](https://openjdk.org/projects/leyden/)); JEP 544 (AOT
*code* compilation) proposed — THINKING, not shipped.
Why gc doesn't do it: gc's PGO consumes profiles *statically at build time*; it cannot
make speculative decisions at run time because it has no guard+deopt machinery. A
cross-run cache of *speculations* (not just profiles) is only actionable with deopt.
Architectural fit: edges 4 (always-on profiler produces the data) + 5 (hybrid AOT+JIT
consumes it; pure-AOT mode can pre-bake the highest-confidence speculations) + 3
(deopt covers drift).
Benchmark sketch: server-process restart / autoscale scenario (e.g. a Go HTTP service
benchmarked with `wrk`/`hey`): time-to-peak-throughput and requests served in the first
60 s vs stock gc+PGO binary. gc+PGO starts at its static optimum immediately but can
never speculate; we start near-peak *and* keep speculating. Also: `go build`-style
short-lived CLI workloads where JIT warmup dominates.
Open risks: cache invalidation on code/config change (Leyden's own risk note: a cache
built on one training run may miss production edge cases); poisoning from
unrepresentative training runs; the guard/deopt path must be genuinely cheap or drift
kills the win. Distinguish from Leyden JEP 544: they cache *code*; we cache
*speculation decisions + profiles* — composable, not competing; design the format so a
future Leyden-style code cache can sit underneath.

### [ABLE] Per-allocation-site compact headers (Lilliput lesson)

**Bucket: ABLE** (JEP 450 JDK 24 experimental → JEP 519 JDK 25 product → JEP 534 JDK 27
default; 4-byte header JEP draft is **THINKING**).
Idea: Lilliput's founding observation — <1% of objects are ever locked, headers are
12 bytes of every object, average object 32–64 bytes so >20% of live data is header —
motivated shrinking headers 96→64 bits with 22-bit compressed class pointers
([openjdk.org/jeps/450](https://openjdk.org/jeps/450),
[openjdk.org/jeps/519](https://openjdk.org/jeps/519); Oracle SPECjbb2015 **vendor
claim**: 22% less heap, 8% less CPU, 15% fewer GCs). Our analog: with the
descriptor-based value model (edge 1), the "header" (type descriptor pointer, GC
metadata) need not be type-global — choose per allocation site between full and
compact descriptors, driven by the profiler's representation-stability signal, with
speculative narrowing + deopt.
Precedent: JEP 450/519/534 above; JEP draft "4-byte Object Headers" (JEP 8349069 —
THINKING).
Why gc doesn't do it: Go values carry a full `*rtype` word; the compiler has no
per-site representation freedom and no speculation to fall back on. gc can't narrow a
descriptor speculatively.
Architectural fit: edge 1 (descriptor-based value model — representation ≠ semantics
is *the* enabler) + edge 4 (representation-stability profiling picks sites).
Benchmark sketch: allocation-heavy workloads — `encoding/json` decode benchmarks,
`Benchmark` in `net/http` request parsing; measure heap footprint + GC CPU vs gc+PGO.
Complementary, not duplicative, of backlog's "type narrowing + bit-packing": that item
packs *fields*; this one shrinks *headers/descriptors* (orthogonal bytes).
Open risks: descriptor narrowing interacts with reflection and `unsafe`; every
narrowed site needs a deopt story for `reflect.TypeOf` on the value. GC must
understand both descriptor widths (barrier co-design — see the Shenandoah lesson in
the HotSpot section).

### [ABLE] Priority inlining + method path duplication

**Bucket: ABLE** (GraalVM 25.3: GR-77137 `PriorityInliningPhase` — "extensive analysis
of the call graph when making inlining decisions", now the default inliner; plus
`MethodDuplicationPhase`, "duplicates selected control-flow paths within a method to
expose further optimization opportunities"; GraalVM 25.5 added more low-tier phases —
per the Graal `compiler/CHANGELOG.md`).
Idea: replace bytecode-size/inlining-budget heuristics with a *simulation* of inlining
benefit over the call graph (using profile data), and duplicate hot control-flow
paths inside a method so downstream opts (GVN, EA, vectorizer) see straight-line
code. gc's inliner is budget-based with PGO hot-edge tweaks — it never simulates the
*consequence* of inlining, and it never duplicates paths.
Precedent: Graal changelog entries above (ABLE, shipped).
Why gc doesn't do it: gc has no call-graph benefit simulator; PGO only re-weights
existing heuristics. Path duplication is a code-size gamble gc won't take without
deopt to retreat.
Architectural fit: edges 3 (speculative: duplicate speculatively, deopt if the cold
path was actually hot) + 4 (profiler feeds the benefit model).
Benchmark sketch: deep-call-chain workloads where inlining decisions dominate —
compiling a large Go module (`go build ./...` on kubernetes-scale repos) or an HTTP
router benchmark with layered middleware; must beat gc **with PGO** (PGO already fixes
the easy hot-edge cases, so the win must come from non-obvious simulation wins).
Open risks: compile-time cost of simulation (budget it; cap by method); code bloat
from duplication hurting i-cache — needs the profiler's hot/cold split to be right.

### [ABLE] Optimistic aliasing speculation (guard + deopt)

**Bucket: ABLE** (Graal release notes: "Strengthened the optimistic aliasing analysis
by adding a non-speculative mode. (Improves loop vectorization.)" —
[docs.oracle.com GraalVM release notes](https://docs.oracle.com/en/graalvm/jdk/17/docs/release-notes/)).
Idea: where static alias analysis bails (two slices *might* overlap), speculate
no-alias, emit the vectorized/aggressively-scheduled code under a cheap guard, deopt
on actual aliasing. gc's vectorizer and load/store motion must stay conservative
because gc cannot deopt — so any *may*-alias kills the optimization for everyone.
Precedent: Graal's optimistic aliasing analysis (ABLE).
Why gc doesn't do it: no guards, no deopt — speculation is not expressible. Go's
simplicity *helps* here: value semantics and no inheritance mean the may-alias sets
are smaller and cheaper to guard than in Java/C++.
Architectural fit: edge 3 (speculative JIT + guards/deopt is the whole trick) + edge 1
(descriptor model can carry an "alias class" tag per value to make guards cheap).
Benchmark sketch: slice kernels where inputs may alias — e.g. `bytes`-package-style
transforms, image-resize-like stencils, `crypto/sha256`-adjacent block loops called
with overlapping src/dst; compare vs gc+PGO on throughput. The honest bar: measure
the guard overhead on the *aliased* (deopt) path too — a benchmark that never aliases
overstates the win.
Open risks: guard placement (per-loop vs per-call); false-sharing-style perf
pathologies when the guard itself becomes the bottleneck; interplay with the
vectorization track (this is its *enabler*, keep them in one design doc).

### [ABLE] Lock coarsening across simple control flow

**Bucket: ABLE** (C2 classic lock coarsening; GraalVM 25.5 GR-79526: "Extended lock
elimination to coarsen locks across simple control flow and eliminate nested locking
of the same object" — shipped 2026, i.e. even mature JITs still find this
profitable).
Idea: after inlining, recognize adjacent `mu.Lock(); ...; mu.Unlock()` regions over
the *same* `sync.Mutex` (proved by points-to/escape analysis) with no observable
operations between them, and merge into one critical section; drop nested
lock/unlock of the same mutex. Distinct from backlog's "adaptive concurrency
primitives" (runtime adaptation) — this is a *static* compiler transform.
Precedent: C2 lock coarsening (ABLE, decades old); GR-79526 (ABLE, 2026).
Why gc doesn't do it: `sync.Mutex` is a library type; gc never sees lock/unlock as
compiler IR across statements, and proving "same mutex, nothing observable between"
needs the inliner + alias analysis working together. Go's no-inheritance helps the
points-to proof.
Architectural fit: edge 3 (speculative: if the "same mutex" proof is profile-based
rather than static, guard + deopt).
Benchmark sketch: fine-grained-locking microbenchmarks (mutex ping-pong, sharded
map with per-shard mutex, a KV-store op path doing lock/unlock per field). Must beat
gc+PGO — plausible because gc has no such transform at all.
Open risks: **semantics minefield** — `defer mu.Unlock()` patterns, panics (merged
region changes what a panic observes as locked), lock *ordering* with other mutexes
(coarsening across an acquisition of mutex B while holding A changes deadlock
behavior — must prove no interleaving lock ops). Condition variables / `sync.Cond`
wait inside the region forbid merging. This needs a conservative legality checker;
start with straight-line same-function regions only. Narrow but real — keep as
candidate with the legality work itemized, or demote to near-miss if the legality
proof proves too expensive.

### Truffle specialization discipline (complementary angle, not a candidate)

**Bucket: ABLE.** Truffle's production discipline for speculative specialization —
bounded polymorphic inline caches (DSL `limit=`, uncached fallback on overflow),
guards checked before each specialization, `Node#reportPolymorphicSpecialize` /
`@ReportPolymorphism` to trigger recompilation policy, and *splitting* (clone a
callee per call-site context to keep PICs monomorphic), partial evaluation once the
AST stabilizes ([oracle/graal BytecodeDSL docs](https://github.com/oracle/graal/blob/master/truffle/docs/bytecode_dsl/BytecodeDSL.md);
[m-sp.org Truffle handout](http://m-sp.org/downloads/truffle_handout.pdf)) — is the
reference design for the backlog's speculative interface-devirtualization cascade.
Two concrete transfers: (1) **bounded PICs with explicit overflow policy** — never an
unbounded megamorphic cache; (2) **splitting** as the answer to megamorphic interface
call sites (clone the caller per receiver shape) rather than giving up. Go helps:
interface method sets are small and statically known, so PIC limits can be tighter
than for dynamic languages.

## OpenJ9

### [ABLE] AOT model + JITServer: precedents for the cross-run speculation cache

**Bucket: ABLE** (all shipped). OpenJ9's AOT story is the most complete cross-run
compilation reuse in any production JVM:
- **Shared class cache (SCC) + dynamic AOT**: AOT-compiled method bodies stored in a
  shared cache, reused across JVM invocations; more aggressive under
  `-Xtune:virtualized` ([eclipse.dev/openj9/docs/xtunevirtualized](https://eclipse.dev/openj9/docs/xtunevirtualized/)).
- **JITServer**: remote JIT — clients (`-XX:+UseJITServer`) ship compilation requests
  to a server; the server keeps per-client session caches (classes, hierarchy,
  profiles) and a **server-side AOT cache** (`-XX:+JITServerUseAOTCache`) of
  serialized compiled methods reused across clients; cheap compilations stay local via
  `enableJITServerHeuristics`
  ([eclipse.dev/openj9/docs/jitserver](https://eclipse.dev/openj9/docs/jitserver/),
  [JITServer tuning](https://eclipse-openj9.github.io/openj9-docs/jitserver_tuning/)).
- **Transfer:** these are the ABLE precedents behind the GraalVM-section candidate
  "Cross-run speculation-profile cache". Two design details worth copying:
  (1) JITServer's *cheap-compiles-stay-local* heuristic — our cache lookup must be
  cheaper than the compilation it avoids, with a measured cutoff; (2) named AOT
  caches per client profile (`-XX:JITServerAOTCacheName`) — our cache should be keyed
  by workload profile, not just binary hash, since the same binary serves different
  traffic shapes.

### [ABLE] `-Xtune:virtualized`: idle-aware compilation policy (near-miss)

**Bucket: ABLE** (shipped). Makes the JIT "more conservative with inlining and
recompilation decisions" and slows GC heap expansion when CPU-constrained/idle —
explicitly trading a small throughput loss for lower CPU consumption
([eclipse.dev/openj9/docs/xtunevirtualized](https://eclipse.dev/openj9/docs/xtunevirtualized/)).
**Transfer verdict: near-miss, not a candidate.** The *policy* idea maps to our edge 2
(user-space scheduler with true-idleness signals) + edge 5 (execution modes): a
scheduler-aware JIT/GC policy (compile aggressively when truly idle, back off when
saturated) is a natural design point for us. But it doesn't beat gc+PGO on a named
benchmark — it's a utilization policy, and our three execution modes already cover
the deployment-shape concern. Keep as a design note for the JIT policy doc, not a
Phase-5 candidate.

### [ABLE] Balanced/GC policies (surveyed, nothing applicable)

**Bucket: ABLE.** OpenJ9's `balanced` (region-based, age-based evacuation) and other
GC policies are runtime configuration, not per-program optimizations. They don't beat
gc+PGO on a named program benchmark; collector choice is orthogonal to Phase 5's
program-optimization candidates. One line, as instructed.

## Azul

### [ABLE] C4 pauseless collector + LVB (surveyed; GC subsystem)

**Bucket: ABLE** (shipping in Zing/Platform Prime). C4's Loaded Value Barrier sits
*immediately after the reference load*, giving it access to the source address for
self-healing — claimed more efficient than Brooks/Baker barriers (**vendor claim**,
[azul.com C4 paper](https://www.azul.com/sites/default/files/images/c4_paper_acm.pdf)).
**Transfer verdict:** GC subsystem, not a Phase-5 program optimization (see the
barrier-design note in the HotSpot section). Nothing applicable beyond the co-design
lesson.

### [ABLE] Falcon LLVM-based JIT (architecture note, not a candidate)

**Bucket: ABLE** (shipping per Azul's 2024 Platform Prime materials; 2026 status
unverified — flagged). Falcon translates bytecode to LLVM IR before machine code;
Azul claims "improved speculative optimizations" and 20–50% faster code vs OpenJDK
(**vendor claims**, not our measurements —
[enterprisetimes.co.uk](https://www.enterprisetimes.co.uk/2024/10/18/azul-platform-prime-37-faster-than-other-openjdk-distributions/)).
**Transfer verdict:** an LLVM-backed optimizing tier is an *architecture* choice, not
a per-benchmark candidate — and it validates our speculative-JIT direction rather
than competing with it. The interesting transferable sub-claim is "better *speculative*
optimizations": LLVM's mature pass pipeline (GVN, loop opts, vectorizer) applied
*under speculation with deopt* is exactly our edge-3 story; note it as supporting
evidence that speculation + industrial-strength mid-end passes compose.

### [ABLE] ReadyNow + Optimizer Hub (precedents; near-miss for remote JIT)

**Bucket: ABLE.** ReadyNow = cross-run profile/code reuse (feeds the
cross-run speculation-cache candidate as a precedent). Optimizer Hub = centralized
remote JIT compilation service ("slashes JIT CPU workload on each JVM", caches
compilations across JVMs — **vendor claims**). **Transfer verdict for remote JIT:
near-miss.** Offloading compilation CPU is real, but it doesn't beat gc+PGO on a
*named program benchmark* — it changes *where* compilation happens, not the code
quality; our pure-AOT mode already addresses the "no JIT CPU on the serving box"
deployment shape more directly.

## .NET (Native/AOT)

### [ABLE] Tiered compilation + Dynamic PGO + guarded devirtualization

**Bucket: ABLE** (shipped; Dynamic PGO on by default since .NET 8,
`DOTNET_TieredPGO`). Tier 0 (quick JIT, no inlining) instruments: branch behavior,
concrete types at call sites/casts; Tier 1 (RyuJIT) recompiles hot methods using the
profile — notably **guarded devirtualization**: `if (obj.GetType() == typeof(Dog))
((Dog)obj).Speak(); else obj.Speak();` — direct call on the hot path, original
virtual call as inline fallback
([devblogs.microsoft.com/dotnet — Performance Improvements in .NET 11](https://devblogs.microsoft.com/dotnet/performance-improvements-in-net-11/);
[learn.microsoft.com — compilation settings](https://learn.microsoft.com/en-us/dotnet/core/runtime-config/compilation)).
.NET 9 extended PGO to type-check/cast fast paths; .NET 10 release notes show PGO
closing a 5.5×→1× gap on interface-dispatch enumeration
([github.com/dotnet/core release notes](https://github.com/dotnet/core/blob/main/release-notes/10.0/preview/preview2/runtime.md)).
**Transfer:** two lessons for the backlog devirtualization cascade —
(1) .NET's GDV keeps the fallback *inline* (code bloat, and no re-specialization
when the profile flips without a full recompile); our edge-3 design should do
**guard+deopt** instead (**WOULD-LIKE** relative to .NET — see below), which is
cheaper on the hot path and re-specializes cleanly;
(2) .NET profiles *casts* too, not just calls — our always-on profiler (edge 4)
should record type-test outcomes at `.(type)` assertions/casts, a currently
under-instrumented site in most Go JIT sketches.

### [WOULD-LIKE] Guard+deopt guarded devirtualization (our edge over .NET's GDV)

**Bucket: WOULD-LIKE** (from .NET's perspective — their GDV has no deopt; from ours
it's a design requirement on the backlog item). .NET emits the uncommon path inline
next to the devirtualized fast path; when the type distribution shifts, the method
must be fully recompiled to re-specialize, and the inline fallback permanently taxes
code size. With speculative JIT + deopt (edge 3), the guard can deopt to the
interpreter/baseline and re-specialize on the new distribution — smaller hot code,
adaptive without full recompiles. No citable .NET RFC for this was found; classify
as wishlist/design-gap honestly, and treat it as a *requirement* on our
devirtualization design rather than a standalone candidate.

### [ABLE] Loop cloning for bounds checks (surveyed, subsumed)

**Bucket: ABLE** (shipped; .NET 11 extended cloning to `!=`-terminated loops —
[devblogs.microsoft.com/dotnet](https://devblogs.microsoft.com/dotnet/performance-improvements-in-net-11/)).
Duplicate the loop into a bounds-check-free fast path + checked fallback. **Transfer
verdict: subsumed** — gc's BCE covers the easy cases; cloning is a BCE *technique*,
and our speculative JIT can clone under an optimistic no-alias/range guard (see the
Graal optimistic-aliasing candidate) rather than needing .NET's static legality
proof. One process note worth stealing: .NET's range-analysis work (inequality
assertions, same-block assertion propagation, bitwise bounds, constant-index
coalescing) is a checklist for our BCE's proof repertoire.

### [ABLE] ReadyToRun / composite partial R2R (precedent for cross-run cache)

**Bucket: ABLE** (shipped). R2R ships MSIL *alongside* precompiled native code; hot
methods get re-JITed by tiered compilation; composite partial R2R precompiles only
profile-identified methods cross-assembly
([learn.microsoft.com — MAUI runtimes](https://learn.microsoft.com/en-us/dotnet/maui/deployment/runtimes-compilation?view=net-maui-10.0)).
**Transfer:** precedent #3 for the cross-run speculation-cache candidate, with the
key design property — **graceful degradation**: precompiled code is a hint, the JIT
stays authoritative. Our cache should have the same property (stale speculation ⇒
deopt, never wrong code).

### [ABLE] NativeAOT deployment constraints (design validation, not a candidate)

**Bucket: ABLE** (shipped since .NET 7). NativeAOT's documented limits — no
`Reflection.Emit`, no dynamic loading, trim-safety required
([learn.microsoft.com](https://learn.microsoft.com/en-us/dotnet/maui/deployment/runtimes-compilation?view=net-maui-10.0))
— are empirical validation of our **three execution modes** (edge 5): a pure-AOT
mode that forbids dynamic codegen needs a hybrid fallback for the long tail of Go
programs (`plugin`, reflective factories). Not a candidate; a design constraint
confirmed by a shipped system.

## THINKING (roadmaps, RFCs, active proposals — plan around / preempt)

### [THINKING] Identity-freedom analysis → header-free flattened values (Valhalla lesson)

**Bucket: THINKING.** JEP 401 (Value Objects) is in preview for JDK 28 (integrated
Aug 2026; disabled by default), with JEP 539 (strict field initialization) as its
JVM-level complement; five Valhalla feature sets include null-restricted storage
and flattened layouts for identity-free objects
([openjdk/valhalla-docs](https://github.com/openjdk/valhalla-docs/blob/HEAD/site/_index.md);
[manorrock.com](https://www.manorrock.com/blog/2026/09/17/pulse_on_java_august_2026.html)).
Idea (≤3 lines): Valhalla's core lesson is a *language-design* one — prove a value
has no identity and the runtime gets representation freedom for free (no header, no
pointer, flatten into parent). Our analog without a language change: a whole-program
**identity-freedom analysis** for Go struct types (no `==` on pointers to it, no
`sync.Mutex`/`atomic`/`noCopy` inside, address never taken observably, never used as
a map key by pointer) → allocate header-free and flatten into parents/slices at
chosen sites, *statically* (no guard needed when proven; speculative + deopt where
only profile-likely).
Precedent: JEP 401/539 (THINKING, preview).
Why gc doesn't do it: gc never questions identity — every addressable value gets the
full treatment. Proving identity-freedom needs whole-program reasoning, which Go's
*simplicity* (no inheritance → simpler whole-program reasoning, per the track brief)
makes cheaper than in Java.
Architectural fit: edge 1 (descriptor model: identity-free ⇒ narrowest descriptor,
possibly zero-word header) + edge 3 (speculative where unproven).
Benchmark sketch: map/slice-heavy workloads with small struct values (e.g. JSON
decode into small structs, metric-label sets); measure allocation rate + footprint
vs gc+PGO. Complementary to backlog's "speculative data representation": that item
specializes *containers*; this one removes identity from *values* — the static-proof
variant is cheaper than speculation wherever the analysis closes.
Open risks: `reflect` and `unsafe` can observe identity the analysis can't see —
needs a conservative bailout list; interface boxing reintroduces identity (a
flattened value boxed into an interface needs materialization — the standard
Valhalla problem, unsolved cheaply anywhere).

### [THINKING] Leyden JEP 544: AOT code compilation (preempt, don't duplicate)

**Bucket: THINKING** (proposed by John Rose; not shipped; Leyden page lists "Ahead-of-Time
Code Compilation" as in-progress —
[openjdk.org/projects/leyden](https://openjdk.org/projects/leyden/)).
Delivered Leyden pieces are ABLE: JEP 483 (AOT class loading & linking, JDK 24),
JEP 514/515 (CLI ergonomics, AOT method profiling, JDK 25), JEP 516 (AOT object
caching with any GC, JDK 26). JEP 544 would store *optimized native code* from
training runs in the AOT cache (secondary report: 65–80% startup-time cuts in
microservice-like settings — **unverified secondary claim**,
[news.lavx.hu](https://news.lavx.hu/article/jep-544-brings-ahead-of-time-code-compilation-to-java-hotspot)).
**Plan-around:** if HotSpot ships cached *code*, our cross-run cache should store
*speculation decisions + profiles* (composable underneath a code cache, not
competing), and our format should be forward-compatible with ingesting a
Leyden-style training run as one more profile source. Don't build a second AOT code
cache; build the speculation layer Leyden doesn't have.

### [THINKING] 4-byte object headers (JEP draft)

**Bucket: THINKING** (JEP draft 8349069: "4-byte Object Headers (Experimental)").
If Lilliput goes 8→4 bytes, the per-site compact-header candidate's payoff roughly
doubles — keep the candidate's design parameterized by header width rather than
hardcoding today's 8/12-byte layouts.

### Crema / Terminus — no verifiable public record

**Bucket: n/a.** Web search (2026-10-04) found no verifiable public record of
OpenJDK Projects "Crema" or "Terminus". If these are internal/partner codenames,
the coordinator should supply a pointer; this track did not invent content for
them.

## Surveyed, nothing applicable

- C2 flow-insensitive EA (Choi99): subsumed by gc's EA + backlog partial-EA.
- Range-check smearing / .NET loop cloning / range-analysis repertoire: subsumed by gc's BCE (steal .NET's proof checklist only).
- Loop strip mining (C2, Graal `StripMineCountedLoops`): optimizes safepoint polls; no Go analog cost — near-miss, not applicable.
- Biased locking: ATTEMPTED, dead; lesson recorded (no per-thread bias state; use guard+deopt).
- Lightweight locking / JEP 491 unpinning: Go already equivalent (mutex fast path; goroutines don't pin) — nothing to transfer.
- G1/ZGC/Shenandoah/C4/OpenJ9-balanced barrier & policy designs: GC subsystem, not per-program Phase-5 optimizations.
- ZGC colored pointers: needs address-space tricks incompatible with Go's runtime model.
- JITServer / Azul Optimizer Hub remote JIT: changes where compilation happens, not code quality; covered by our execution modes — near-miss.
- `-Xtune:virtualized`: utilization policy, no named-benchmark win vs gc+PGO — design note only.
- Azul Falcon as architecture: validates speculation + strong mid-end, not a per-benchmark candidate.
- .NET NativeAOT constraints: validate our three execution modes; not a candidate.
- Graal loop inversion (≤30% microbench — vendor claim): loop-shape transform, subsumed by the vectorization track.
- Superword SLP: vectorization-track process notes only (vectorize the unrolled shape; test vectorizer × loop passes jointly).

## Bucket tally

- **ABLE:** 14 (biased-locking replacements, C2 loop opts ×3, C2 EA, barrier designs, Graal PEA/vectorization/Truffle/strip-mining/low-tier phases, OpenJ9 AOT+JITServer+Xtune+balanced, Azul C4/Falcon/ReadyNow/Hub, .NET tiered-PGO/GDV/loop-cloning/R2R/NativeAOT, Leyden delivered JEPs 483/514/515/516, Lilliput JEP 450/519/534)
- **THINKING:** 4 (Valhalla JEP 401/539, Leyden JEP 544, 4-byte headers draft, + cross-run cache's THINKING tail)
- **ATTEMPTED:** 3 (biased locking — complexity vs payoff; Intel TSX lock elision — hardware deprecated RTM, don't build on fusable-off silicon features; HotSpot stack-locking — dropped for compact headers)
- **WOULD-LIKE:** 1 (guard+deopt GDV — .NET's inline-fallback GDV has no deopt; our design requirement)

**Candidates passing the bar (6):** cross-run speculation-profile cache; per-site compact
headers; priority inlining + path duplication; optimistic aliasing speculation;
lock coarsening across control flow; identity-freedom analysis → flattened values.
**Near-misses (5):** loop strip mining; remote JIT (JITServer/Optimizer Hub);
`-Xtune:virtualized` policy; Falcon-as-architecture; biased-locking-style fast paths
(superseded by guard+deopt lesson).

## Draft log
- 2026-10-04 ~08:40 CEST: skeleton created.
- 2026-10-04 ~09:00 CEST: HotSpot + GraalVM sections drafted (research: JEP 374/450/491/519/534, strip mining, range-check smearing, superword, C2 EA, barrier designs, Graal changelog GR-77137/GR-79526, Truffle docs).
- 2026-10-04 ~09:15 CEST: owner refinement applied — .NET Native/AOT added; all items bucketed (ABLE/THINKING/ATTEMPTED/WOULD-LIKE); THINKING section (Valhalla JEP 401, Leyden JEP 544, 4-byte headers); Crema/Terminus flagged as unverifiable; negative-results and tally written. Track complete.
