# Track 1 — Academic literature survey (PLDI/CGO/OOPSLA/ASPLOS/VEE/ISMM)

**Track:** academic literature. **Date:** 2026-10-04. **Mission:** find optimization ideas in
academic literature that could plausibly beat stock gc **with PGO** on a named benchmark, per the
Phase-5 rule. Backlog exclusions honored (speculative interface devirtualization cascade,
profile-guided struct field reorder/pack, speculative AoS→SoA, type narrowing + bit-packing,
speculative data representation, small string optimization, per-site string interning,
inferred store-once values, idle-time repack/defrag/shrink, adaptive concurrency primitives,
vectorization, always-on profiling, partial escape analysis; also basic inlining, gc's escape
analysis, bounds-check elimination).

Architectural edges referenced below: (E1) descriptor-based value model — layout/storage
per-value or per-allocation-site, not type-global; (E2) user-space goroutine scheduler with
precise runtime signals; (E3) speculative JIT with guards + deoptimization; (E4) always-on
profiler (field-access patterns, array-access patterns, value ranges, representation
stability, store counts); (E5) three execution modes (JIT-only, pure AOT, hybrid AOT+JIT).

Evidence honesty: "paper claims X" ≠ "we verified X". Paper numbers are reported as the
papers' own claims, never as our measurements. All precedents cited with title/venue/year/authors.

---

## Candidates passing the bar

### C1 — Profile-driven allocation-site segregation (behavior-predicted pools)

**Idea (≤3 lines).** Classify each allocation site by predicted object lifetime and
reference frequency (short/long-lived × hot/cold), then route allocations to separate
bump arenas per class. Short-lived cold objects never enter the old generation's scan
set; hot objects cluster together for cache locality. Classification comes from the
always-on profiler (E4), not from a training run, and is re-decided per JIT tier.

**Precedent.** Seidl, M. L. & Zorn, B. G., "Segregating Heap Objects by Reference
Behavior and Lifetime", *ASPLOS-VIII*, 1998, pp. 12–23 (offline ATOM profiling into
four behavioral classes, separate arenas per class; showed heap references are highly
predictable per allocation site). Complementary: Chilimbi, T. M. & Larus, J. R.,
"Using Generational Garbage Collection To Implement Cache-conscious Data Placement",
*ISMM* 1998 (paper claims 20–41% miss-rate reduction and 18–31% program speedup over
plain Cheney copying by placing temporally-affine objects adjacently during GC).

**Why gc doesn't do it.** gc's allocator segregates by size class (mspan/mcache) only;
its escape analysis is a binary stack-vs-heap decision. No notion of predicted lifetime
or reference behavior influences heap placement, and PGO in gc only feeds inlining —
not allocation policy.

**Architectural fit.** (E4) always-on profiler supplies per-site lifetime/reference
predictions with zero training runs — the exact thing Seidl & Zorn lacked (they needed
ATOM instrumentation). (E1) descriptor-based model: the pool choice is a per-site
property of the value descriptor, so the *same type* can live in different pools at
different sites — impossible under gc's type-global layout. (E3) speculation is free
here: a mispredicted site only costs placement quality, never correctness; re-JIT
re-classifies.

**Benchmark sketch.** `encoding/json` decode of a large payload (or an HTTP handler
benchmark): per-request short-lived objects (tokens, small maps, buffers) allocated
into a FIFO/bulk-reclaim arena never scanned by old-gen GC, vs gc+PGO where they all
flow through the shared heap. Win mechanism: reduced GC scan work + better locality,
not faster allocation.

**Open risks.** (1) Phase behavior: site classification must be stable across workload
phases or re-classification churns; (2) pool fragmentation if lifetime prediction is
wrong in the long-lived direction; (3) interplay with the GC — needs a collector that
can treat segregated pools as regions (bulk-free or reduced scan), otherwise the win
evaporates. **Bar check:** (a) semantics-preserving ✓ (placement invisible); (b)
plausible on alloc-heavy named benchmarks ✓; (c) E1+E4 (+E3) ✓; (d) not in backlog ✓.

### C2 — Hardware-monitor-guided software prefetch injection for pointer chases

**Idea (≤3 lines).** JIT inserts prefetch instructions ahead of pointer-chasing
dereferences (linked lists, trees, AST walks), with distance and site selection driven
by the always-on profiler correlated with hardware miss counters — not by static
heuristics. Prefetch is a pure hint: zero semantic risk, guarded only by a
profitability model.

**Precedent.** Adl-Tabatabai, A.-R., Hudson, R. L., Serrano, M. J. & Subramoney, S.,
"Prefetch Injection Based on Hardware Monitoring and Object Metadata", *PLDI* 2004,
pp. 267–276 (drives prefetch insertion from hardware performance monitors + object
metadata on Java workloads). Luk, C.-K. & Mowry, T. C., "Compiler-based Prefetching
for Recursive Data Structures", *ACM SIGOPS Oper. Syst. Rev.* 30(5), 1996, pp. 222–233
(greedy/history-pointer/data-linearization prefetching; course slides of Mowry's
15-745 report 4–45% speedups on Olden pointer benchmarks for greedy prefetching).
Ancestor: Mowry, T. C., Lam, M. S. & Gupta, A., "Design and Evaluation of a Compiler
Algorithm for Prefetching", *ASPLOS-V* 1992.

**Why gc doesn't do it.** gc emits no prefetch instructions at all; hardware
prefetchers track strides, not pointer chains. gc+PGO has no mechanism to convert
miss profiles into prefetch insertion.

**Architectural fit.** (E4) is the enabler: always-on profiler + hardware counters
attribute L2 misses to chase sites, and value/range profiles pick prefetch distance
per site. (E3): none needed for correctness (hint only); the guard is economic
(disable site if measured useless). (E1): descriptor can carry a "chase stride"
annotation per allocation site.

**Benchmark sketch.** Large treap/B-tree/AST traversal — e.g., a Go `go/ast` walk over
a big syntax tree, or a red-black-tree lookup microbenchmark: pointer-chase latency
dominates, hardware prefetcher can't help, gc+PGO has no answer.

**Open risks.** (1) Modern out-of-order cores with good MLP may absorb much of the
latency — cautionary evidence: Poirier, Rohou & Serrano, "An Attempt to Catch Up with
JIT Compilers: The False Lead of Optimizing Inline Caches", arXiv 2502.20547 (2025),
found removing memory reads often yields *no* wall-clock win on contemporary
architectures; prefetch must be measured, not assumed. (2) Wrong distance wastes
bandwidth and pollutes cache. **Bar check:** (a) ✓ hint-only; (b) ✓ plausible on
pointer-chase benchmarks; (c) E4 ✓; (d) ✓ not in backlog.

### C3 — Speculative per-allocation-site object inlining (field-append)

**Idea (≤3 lines).** When a parent object holds the unique pointer to a freshly
allocated child (e.g., `n.Left = &Node{...}`), inline the child's fields directly into
the parent's allocation: one allocation instead of two, one fewer indirection per
access. Decided per allocation site from profiler-observed uniqueness, with a guard:
if a non-conforming (aliased/shared) allocation ever occurs at that site, deopt to the
boxed layout.

**Precedent.** Dolby, J., "Automatic inline allocation of objects", *PLDI* 1997,
pp. 7–17; Dolby, J. & Chien, A., "An automatic object inlining optimization and its
evaluation", *PLDI* 2000, *SIGPLAN Not.* 35(5), pp. 345–357 (whole-program analysis
inlining uniquely-referenced objects; eliminates allocation + indirection).
Distinguish from Veldema et al.'s "object combining" (appends objects but keeps the
pointer; inlining *replaces* the pointer with fields).

**Why gc doesn't do it.** gc has escape analysis → stack allocation/scalar
replacement, but no *heap* object inlining: a parent and its heap child are always
two allocations with a pointer between them. gc's analysis is per-function and
conservative; it cannot make per-site inline-vs-boxed decisions.

**Architectural fit.** (E1): descriptor-based value model makes inline-vs-boxed a
*per-allocation-site* representation choice for the same type — strictly more general
than Dolby's whole-program type-global decision. (E4): profiler observes that the
child is always freshly allocated at this site and never aliased elsewhere.
(E3): guard + deopt covers the interface-smuggled-alias case. **Go helps here:** no
inheritance means the uniqueness analysis has no subtype-aliasing surprises (the hard
case in OO papers); Go's structural interfaces are the only aliasing leak, and the
guard handles exactly that.

**Benchmark sketch.** AST-heavy workloads (Go parser producing `go/ast` nodes, then
walked) or a linked-list/node+payload microbenchmark where node and payload are
separate allocations today: removes ~half the allocations and one indirection per
traversal step.

**Open risks.** (1) Inlining bloats the parent's cache footprint when only a few child
fields are hot — interacts with field-affinity analysis (cf. backlog field
reorder/pack; complementary). (2) GC must support variable-shape inlined objects —
descriptor model helps, but it's new machinery. (3) Guard check cost on the
allocating path. **Bar check:** (a) sound speculative story ✓; (b) ✓ plausible on
AST/linked-structure benchmarks; (c) E1+E3+E4 ✓; (d) ✓ not in backlog.

### C4 — Parameter-based value specialization of hot functions

**Idea (≤3 lines).** The profiler observes that a hot function is always (or
overwhelmingly) called with the same parameter values — small enums, mode flags,
constant bounds. JIT-compiles a specialized version with those values constant-folded
(branches folded, dead code eliminated, strength reduction), guarded by a cheap
parameter check that deopts to the generic version on mismatch.

**Precedent.** Costa, I. R. de A. et al., "Just-in-time value specialization",
*Computer Languages, Systems & Structures*, 2014 (parameter-based specialization in
IonMonkey; paper reports ~60% of JS functions are called once or with identical
parameters, and claims +4.83% on the V8 benchmark suite with −18.84% code size).
Classic ancestor: Cooper, K. D., Hall, M. W. & Kennedy, K., procedure cloning —
partitioning call sites by parameter values so each clone optimizes under restricted
inputs. Related JIT evidence: Suganuma et al.'s dynamic code specialization work (IBM
JIT) specializing on "a constant value of a primitive, exact object type, array
length…" observed in profiles.

**Why gc doesn't do it.** gc has no value profiling and no specialization: a function
taking a `mode int` that is always 2 still compiles the fully general body. gc+PGO
only uses profiles for inlining/layout decisions.

**Architectural fit.** (E4): value-range profiles are exactly the planned profiler
output; (E3): guard + deopt is the soundness story. **Go transfer is clean:** Go is
statically typed, so this is pure *value* specialization (no type uncertainty to
manage, unlike the JS original) — simpler guards, fewer deopts. Natural targets:
`mode`/`flags` parameters, fixed `less` functions, format strings, small-int
config enums threaded through hot paths.

**Benchmark sketch.** `text/template` rendering with a fixed data shape, or a hot
numeric loop calling `f(x, order)` with loop-invariant `order`: specialization folds
the dispatch inside the loop body.

**Open risks.** (1) Code bloat if too many specializations are kept; needs a
one-specialization-at-a-time budget (cf. Suganuma's limit of one code duplication per
method). (2) Guard cost on every call — must be a single compare. (3) Value
instability across phases → deopt storms; needs the deopt-budget discipline from the
near-miss NM1. **Bar check:** (a) ✓ guard+deopt; (b) ✓ plausible on config-driven
hot paths; (c) E3+E4 ✓; (d) ✓ not in backlog.

### C5 — GC-cooperative cache-conscious copying with online affinity profiling

**Idea (≤3 lines).** During compacting GC, copy objects with high temporal affinity
adjacently so contemporaneously-accessed objects share cache lines (clustering), and
segregate hot/cold objects into non-conflicting cache regions (coloring). Affinity
comes from a low-overhead online object-access buffer maintained by the always-on
profiler — no training runs.

**Precedent.** Chilimbi, T. M. & Larus, J. R., "Using Generational Garbage Collection
To Implement Cache-conscious Data Placement", *ISMM* 1998 (copying generational GC
reorganizes by temporal affinity; paper claims 20–41% miss-rate reduction and 18–31%
program speedup over plain copying). Static-layout sibling: Chilimbi, Hill & Larus,
"Cache-Conscious Structure Layout", *PLDI* 1999 (ccmalloc/ccmorph: clustering +
coloring for pointer structures; programmer supplies traversal/allocation hints).

**Why gc doesn't do it.** Go's GC is non-moving by design (no object relocation, to
keep pointer identity and cgo interop simple) — so there is *no* GC copy pass in
which to reorganize layout. This candidate exists only because graalgo owns its
collector.

**Architectural fit.** (E4): the 1998 paper's key weakness was needing compiler
instrumentation for the access buffer; graalgo's always-on profiler is the native
implementation of exactly that sensor. (E1): per-object placement decisions during
copy fit the descriptor model (placement policy per allocation site, not per type).

**Benchmark sketch.** Pointer-tree/graph traversal over a large live heap — e.g., a
big treap lookup benchmark or GC-heavy graph workload: after a few collections the
hot working set becomes cache-dense, vs gc+PGO where layout is allocation-order
forever.

**Open risks.** **Conditional candidate:** requires graalgo's GC to be moving/
compacting at least for some heap region — if the collector stays non-moving like
Go's, this is dead on arrival; confirm before investing. (2) Copying cost vs locality
benefit must be measured per workload; (3) affinity staleness across phases.
**Bar check:** (a) ✓ semantics-preserving; (b) ✓ plausible on traversal benchmarks;
(c) E4 (+E1) ✓; (d) distinct from backlog ✓ — backlog has *field* reorder/pack
(intra-object) and *idle-time* repack/defrag/shrink (memory footprint); this is
*inter-object* hot-path placement during GC. Flag the proximity anyway.

### C6 — Lifetime-bounded pool allocation with bulk free (goroutine-scoped regions)

**Idea (≤3 lines).** Compiler identifies disjoint heap data structures (à la Lattner)
whose lifetimes are bounded by a lexical/goroutine scope; allocates each into its own
pool, and reclaims the *whole pool at once* when the scope ends — individual free/GC
operations for those objects disappear entirely. The user-space scheduler (E2) supplies
the scope signal: true goroutine death/idleness.

**Precedent.** Lattner, C. & Adve, V., "Automatic Pool Allocation: Improving
Performance by Controlling Data Structure Layout in the Heap", *PLDI* 2005
(context-sensitive pointer analysis → disjoint pools; per-pool optimizations include
eliminating individual frees via whole-pool reclamation; paper shows reduced working
sets). Region lineage: Grossman, D. et al., "Region-Based Memory Management in
Cyclone", *PLDI* 2002 (lexically-scoped growable regions; requires language support —
ours is automatic + speculative, no annotations); classic: Tofte, M. & Talpin, J.-P.,
region inference for ML (*POPL* 1994).

**Why gc doesn't do it.** gc's escape analysis is stack-vs-heap only; heap objects
with scope-bounded lifetimes still pay full GC tracking. There is no region/pool
middle ground and no bulk-free path.

**Architectural fit.** (E2): the user-space scheduler knows *true* goroutine
termination and idleness — the natural scope signal for per-request/per-task pools
(e.g., everything allocated while handling one HTTP request dies with the handler).
(E4): profiler confirms the "all die together" hypothesis before promoting a pool to
bulk-free. (E3): guard — a write barrier (or remembered-set check) catches an object
escaping its pool's lifetime; on violation, deopt that pool back to GC-managed.
**Go helps:** no inheritance dramatically simplifies Lattner's context-sensitive
points-to analysis (the OO papers fight class-hierarchy imprecision); the remaining
precision leak is interface values, handled by the guard.

**Benchmark sketch.** HTTP server / RPC handler benchmark (per-request allocation
storm: headers, JSON nodes, buffers): bulk-free at handler exit vs gc+PGO scanning
every short-lived object. Also compiler-like passes (AST per file).

**Open risks.** (1) Pointer-analysis precision at scale — Lattner's analysis is fast
but unification-based; interface-heavy code may collapse pools. (2) Bulk-free safety
proof must be airtight or guarded; a missed escape is a use-after-free, not a perf
bug. (3) Pool-per-structure memory overhead. **Bar check:** (a) sound speculative
story (guarded) ✓; (b) ✓ plausible on request-scoped benchmarks; (c) E1+E2+E3+E4 ✓;
(d) ✓ not in backlog (idle-time repack is post-hoc; this is allocation-time scoping).

### C7 — Prolific-type-driven allocation specialization (type-segregated nursery + recycling)

**Idea (≤3 lines).** The profiler identifies *prolific types* (types accounting for
most allocations) per workload phase; those types get segregated nurseries,
free-list recycling, and compact type-ID tagging, while non-prolific types use the
default path. Type-aware policy replaces gc's type-blind size-class allocation for
the types that matter.

**Precedent.** Shuf, Y., Gupta, M., Bordawekar, R. & Singh, J. P., "Exploiting
Prolific Types for Memory Management and Optimizations", *POPL* 2002 (Jalapeño VM;
paper claims: type-based collector cuts GC time up to 15% vs analogous generational
collector with fewer write barriers; short type pointers cut heap requirements up to
16%; plus prolific-object recycling and co-allocation; evaluated on SPECjvm98 and
SPECjbb2000).

**Why gc doesn't do it.** gc's spans segregate by size class only; allocation policy
is type-blind, there is no object recycling, and type metadata (itab/mtype) is not
exploited for layout or GC policy. gc+PGO does not feed type-frequency profiles back
into the allocator.

**Architectural fit.** (E4): always-on profiler maintains the prolific-type set per
phase — the paper used offline profiling; online re-detection handles phase changes.
(E1): per-type allocation policy and representation (recycled vs bump, compact type
tags) is exactly "layout decisions not type-global"... refined: policy *keyed by*
type but *chosen per workload*, and per-site overrides stay possible. **Go helps
decisively here:** in the Java original, a "prolific type" suffers subtype
polymorphism (many dynamic types hide behind one static type); Go has no
inheritance, so an allocated concrete type is *exactly* known — type-based
segregation and recycling are precise where the OO version was approximate.

**Benchmark sketch.** Workloads dominated by one allocation type: `encoding/json`
decode (token/small-map churn), `net/http` buffer churn, or a protobuf-heavy RPC
benchmark — prolific `[]byte`/node types get recycled instead of reallocated.

**Open risks.** (1) Phase changes invalidate the prolific set — needs cheap
re-detection and policy switch. (2) Recycling correctness: a recycled object must be
provably dead; conservative liveness or guarded recycling only. (3) Overlaps C1's
axis (per-site vs per-type) — they compose, but evaluate separately to avoid double
counting. **Bar check:** (a) ✓ (conservative or guarded); (b) ✓ plausible on
alloc-dominated named benchmarks; (c) E4+E1 ✓; (d) ✓ not in backlog.

### C8 — Safe-region / alias-versioned loop-nest transformation for managed code

**Idea (≤3 lines).** Duplicate hot loop nests into a "safe" version (implicit panic
checks hoisted/proven, aggressive interchange/tiling/fusion applied) and an "unsafe"
conservative version; a cheap runtime test (bounds + nil + a pointer-inequality alias
test) selects the safe version per invocation. Unlocks loop transformations that
gc's conservative alias analysis and mandatory panic checks block.

**Precedent.** Artigas, P. V., Gupta, M., Midkiff, S. P. & Moreira, J. E.,
"Automatic Loop Transformations and Parallelization for Java", *ICS* 2000 (IBM;
safe-region creation by duplicating loop nests and stripping checks under a runtime
test + *alias versioning* via cheap pointer comparisons; paper claims 80–100% of
highly-tuned Fortran performance on numerical Java benchmarks, up to 3.8×
parallelization speedup on 4 CPUs).

**Why gc doesn't do it.** gc performs loop optimizations (unrolling, some
interchange?) conservatively: Go's mandatory bounds/nil panics and gc's alias
analysis block most loop-nest restructuring (interchange, tiling, fusion across
potentially-aliasing slices). gc has no loop versioning with runtime alias tests and
no PGO-driven nest selection.

**Architectural fit.** (E3): runtime-test-guarded versioning is speculation in its
mildest form (test-then-dispatch, no deopt needed — or deopt as fallback). (E4):
trip-count and aliasing profiles pick *which* nests to version and which
transformation to apply. **Go helps:** no exceptions-as-control-flow means fewer
implicit edges to version around than Java (the paper's main enemy was Java's precise
exception model); Go slices carry exact bounds, making the hoisted bounds test cheap
and exact.

**Benchmark sketch.** Matrix multiply / transpose / stencil kernels — e.g., a Go
`BenchmarkMatMul` (gonum-style) with unfavorable loop order, or image-processing
convolutions: interchanged/tiled nest vs gc+PGO's conservative nest. This is also the
natural on-ramp for the in-scope vectorization work (better nests vectorize better).

**Open risks.** (1) Test cost vs nest size — only pays for large trip counts;
profiler must gate. (2) Code bloat from duplicated nests. (3) Go's slice-header
indirection (ptr/len/cap) complicates the alias test vs Java arrays — needs design.
**Bar check:** (a) ✓ (test-then-dispatch preserves semantics); (b) ✓ plausible on
numerical kernels; (c) E3+E4 ✓; (d) ✓ not in backlog (backlog has vectorization and
bounds-check elimination, not versioned loop-nest restructuring).

---

## Near-misses (surveyed, do not propose as candidates)

- **NM1 — Tiered deoptimization targets.** Deopt to a *less-optimized compiled tier*
  instead of all the way to the interpreter/baseline (cf. OpenJDK JEP draft "Ahead of
  Time Compilation for the Java Virtual Machine" discussion of deopt costs; mechanism
  originates in Hölzle, Chambers & Ungar, "Debugging Optimized Code with Dynamic
  Deoptimization", *PLDI* 1992). This is a *mitigation* for speculation-heavy
  designs, not itself a win vs gc+PGO on any benchmark — it only matters if C3/C4/C8
  speculate aggressively. Relevant as a design constraint on the deopt discipline
  (also note: "Understanding and Finding JIT Compiler Performance Bugs",
  arXiv:2603.06551 (2026), finds speculation/deopt failures are ~28.8% of JIT
  performance bugs — budget speculation accordingly). Not a candidate: fails bar (b).
- **NM2 — Hot/cold field splitting of structs.** Splitting a struct into hot/cold
  sub-objects (indirection for cold fields) is *adjacent to* backlog's profile-guided
  field reorder/pack but distinct (adds a pointer). Survey found no clean academic
  precedent with measured numbers (industry does it in places, e.g., JVM code-cache
  hot/cold splitting is *code*, not data). Do not propose: fails evidence bar; note
  as complementary angle to backlog field layout only.
- **NM3 — Semi-manual annotation-driven AoS→SoA.** Radtke & Weinzierl, "Compiler
  support for semi-manual AoS-to-SoA conversions with data views", arXiv:2405.12507
  (2024) (C++ attribute annotations; compiler converts layout around kernels;
  demonstrated on SPH). Backlog already covers *speculative* AoS→SoA; the annotation
  angle is complementary at most (programmer hints as an additional signal), not a
  new candidate.

## Surveyed, nothing applicable

- Trace-based JIT type specialization for dynamic languages (e.g., Gal et al.-style
  tracing JITs, 2009): specializes on *type uncertainty*, which Go doesn't have —
  no transfer story.
- ABCD-style array-bounds-check elimination (Bodik, Gupta & Sarkar, *PLDI* 2000):
  excluded by brief — gc already does bounds-check elimination.
- Region-based *compilation* units (Suganuma, Yasue & Nakatani, "A region-based
  compilation technique for a Java just-in-time compiler", *PLDI* 2003) and adaptive
  recompilation with OSR (Fink & Qian, *CGO* 2003): compilation infrastructure, not
  benchmark-winning optimizations vs gc+PGO.
- Classical scalar optimizations (value numbering, PRE, LCM): gc does them; no
  academic delta to exploit.
- Hardware value prediction / runahead: hardware techniques, not compiler
  optimizations for this project.
- Deegen (Xu & Kjolstad, arXiv:2411.11469, 2024): VM-generator for dynamic
  languages — left to Track 3 (dynamic-language runtimes); noted here only to avoid
  double-survey.
- Object combining (Veldema et al., keeps the pointer, separate deref-elimination
  pass): strictly weaker transfer than C3's inlining; folded into C3's precedent
  discussion.
- Cyclone-style *language-level* regions (Grossman et al., *PLDI* 2002): requires
  source annotations — rejected as a language change; only its automatic/speculative
  descendant (C6) is proposed.
