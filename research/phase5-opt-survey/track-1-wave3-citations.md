# Track 1, Wave 3 — Forward-citation sweep of the wave-1 seed corpus

**Worker:** W1 (T1). **Date:** 2026-10-04. **Mission:** for each wave-1 seed paper
(Dolby 1997/2000, Lattner&Adve 2005, Chilimbi&Larus 1998 ISMM, Seidl&Zorn 1998 ASPLOS,
Shuf et al. 2002 POPL, Artigas et al. 2000 ICS, Adl-Tabatabai et al. 2004 PLDI,
Luk&Mowry 1996), find citing papers (2016–2026, especially VEE/PACT/ASPLOS/PLDI/CGO/
OOPSLA/ISMM), and mine optimization ideas that transfer to graalgo.
**Bar/buckets/edges:** same as the standing survey rules ([SURVEY.md](SURVEY.md) "Method and rules").
SURV-001…059 already exist — do not re-propose. IDs assigned at synthesis (candidates
start unnumbered, marked raw/vetted).
**Evidence honesty:** every number is the cited paper's claim ("paper claims X"),
never ours. Source: Semantic Scholar citation graph + abstract/PDF inspection via
text fetches.

## Seed list

| # | Seed paper | Citation-count note |
|---|-----------|---------------------|
| S1 | Dolby, "Automatic inline allocation of objects", PLDI 1997 | — |
| S2 | Dolby & Chien, "An automatic object inlining optimization and its evaluation", PLDI 2000 | — |
| S3 | Lattner & Adve, "Automatic Pool Allocation: Improving Performance by Controlling Data Structure Layout in the Heap", PLDI 2005 | — |
| S4 | Chilimbi & Larus, "Using Generational Garbage Collection To Implement Cache-conscious Data Placement", ISMM 1998 | — |
| S5 | Seidl & Zorn, "Segregating Heap Objects by Reference Behavior and Lifetime", ASPLOS-VIII 1998 | — |
| S6 | Shuf, Gupta, Bordawekar & Singh, "Exploiting Prolific Types for Memory Management and Optimizations", POPL 2002 | — |
| S7 | Artigas, Gupta, Midkiff & Moreira, "Automatic Loop Transformations and Parallelization for Java", ICS 2000 | — |
| S8 | Adl-Tabatabai, Hudson, Serrano & Subramoney, "Prefetch Injection Based on Hardware Monitoring and Object Metadata", PLDI 2004 | — |
| S9 | Luk & Mowry, "Compiler-based Prefetching for Recursive Data Structures", SIGOPS 1996 | — |
| S10 | Chilimbi, Hill & Larus, "Cache-Conscious Structure Layout", PLDI 1999 | (secondary seed; ccmalloc/ccmorph lineage cited from S4's discussion) |

*(per-seed sections appended below as research completes)*

## Per-seed findings

### S1/S2 — Dolby 1997 / Dolby & Chien 2000 (automatic object inlining)

**Citing papers scanned:** ~12 result entries reviewed across 2 searches; 1 PDF fetched
and reference-verified; 1 citing paper worth mining.

**Worth mining.** Bruno, R., Jovanovic, V., Wimmer, C. & Alonso, G.,
"Compiler-Assisted Object Inlining with Value Fields", *PLDI* 2021 (14 pp.,
DOI 10.1145/3453483.3454034). **Forward citation verified:** the paper's
reference list contains [19] "Julian Dolby. 1997. Automatic Inline Allocation of
Objects. (PLDI '97)" and [20] "Julian Dolby and Andrew Chien. 2000. An Automatic
Object Inlining Optimization and Its Evaluation. (PLDI '00)" (verified by text
search of the PDF). Idea: *value fields* — fields marked (by developer or
inference) as having value semantics are payload-inlined into the parent object
at type level, exploiting the closed-world assumption of GraalVM Native Image
(no guards needed — static soundness). Inlining is done early in the pipeline
so method inlining + escape analysis further optimize the inlined accesses.
Paper claims (their measurements, not ours): up to 3× throughput on OrientDB
graph-database requests, up to 40% memory-footprint reduction on Apache Spark
analytics, up to 35% GC-pause reduction on Micronaut/Spring Boot microservices;
motivating figure: on the 6.3M-entry IMDB dataset, object headers + references
account for up to 54% (HotSpot) / 41% (Native Image) of memory. Distinct from
SURV-025 (which is speculative *per-allocation-site* inlining with guard/deopt):
this is *declared/inferred value semantics + closed-world static inlining* with
no deopt machinery — the pure-AOT counterpart. → candidate C-W3-1.

**Lineage note.** Wimmer & Mössenböck's dynamic object inlining for HotSpot
(read barriers + GC colocation; paper claims 9% avg / 51% max on SPECjvm98) is
the older forward branch of S1/S2 — predates 2016, cited by the PLDI'21 paper
as [38]; recorded for lineage only.

### S3 — Lattner & Adve 2005 (automatic pool allocation)

**Citing papers scanned:** ~15 result entries reviewed across 3 searches; 1 PDF
fetched and reference-checked. **No direct citing paper of Lattner&Adve 2005
found in 2016–2026 with new measured optimizations** — the live branches are:

- **GoFree (lineage-adjacent, does NOT cite Lattner directly).** Peng, H., Zhang,
  Y., Ernst, M. D., Chen, J. & Ding, B., "GoFree: Reducing Garbage Collection
  via Compiler-Inserted Freeing", *CGO* 2025, pp. 675–688
  (DOI 10.1145/3696443.3708925). Go-targeted compiler-inserted explicit freeing:
  a new O(N²) escape analysis identifies short-lived heap objects that can be
  safely deallocated; a `tcfree` runtime primitive does best-effort freeing
  (reverts the thread-local allocator pointer for small objects; 2-step async
  for large; returns early — leaving the object for GC — whenever deallocation
  would be costly or racy). Paper claims (their measurements): on 6 open-source
  Go programs, 14% of allocated heap memory deallocated at run time, −7% GC
  frequency, −13% GC time, −2% wall-clock, −4% heap size, no observable compile-time
  slowdown. §2.2 cites the *region-based memory management* family
  ([1,6,25,39]: Elsman 2003 GC-safety for regions, Boyapati ownership types,
  Hicks/Wang/Cheney) and states "our ideas apply to any runtime supporting
  region-based or explicit deallocation" — i.e., it positions itself in the
  region/free lineage that S3 belongs to, but **does not cite Lattner&Adve
  2005** (verified by text search of the PDF). → candidate C-W3-3 (speculative
  variant; GoFree itself is static).
- Elsman, M., "Garbage-Collection Safety for Region-Based Type-Polymorphic
  Programs", *PLDI* 2024; Elsman, "Parallelism in a Region Inference Context",
  *PLDI* 2023 (MLKit/ML lineage — functional-language region inference; Go
  transfer is weak; recorded for lineage only, not mined).
- Rinard, M., "Region Inference for an Object-Oriented Language", *PLDI* 2004
  (OO descendant of the region line; predates window).

### S4 — Chilimbi & Larus 1998, ISMM (cache-conscious data placement in generational GC)

**Citing papers scanned:** ~14 result entries reviewed across 2 searches.
**Negative:** no 2016–2026 managed-runtime citing paper found that continues
GC-time cache-conscious placement with measurements. The live continuation of
this line is in C/C++ allocators (ccmalloc/ccmorph, e.g. US patent 6,321,240)
and the data-structure-aware GC work filed under S6. SURV-039 stands on the
original precedent; recorded as first-class negative (see §Negatives).

### S5 — Seidl & Zorn 1998, ASPLOS (segregating heap objects by reference behavior and lifetime)

**Citing papers scanned:** ~10 result entries reviewed across 2 searches; 1
paper abstract-verified. **No direct 2016+ citing paper found**; the live
branches of the per-allocation-site lifetime-prediction lineage are:

- **Memento Mori (lineage-adjacent; direct citation of Seidl&Zorn not
  verified).** Stanton, M. & Titzer, B. L., "Memento Mori: Dynamic
  Allocation-site-based Optimizations", *ISMM* 2015, pp. 105–117. Allocation
  *mementos* (small objects placed next to young-gen objects, living only until
  the next GC) tie objects back to allocation sites with negligible space
  overhead; the GC/runtime uses the temporal feedback for **pretenuring,
  pretransitioning, and presizing** — fully dynamic, per-site, in production V8
  (shipped since Chrome 26). The paper positions itself against prior *online
  object-lifetime feedback* work ("previous work [17,23] has primarily focused
  on allocation sampling to gather limited online object lifetime feedback").
  → **not a new candidate**: presizing is SURV-017 (Memento Mori is the ABLE
  precedent), lifetime→placement is SURV-026 (pretenuring as complementary
  angle); the memento *mechanism* is filed as an E4-sensor idea under §4 of the
  survey. Pretransitioning (representation evolution) is dynamic-language
  specific; its Go analog is subsumed by the backlog's speculative data
  representation.
- GoFree (S3 section) is the other live branch — EA-based rather than
  profile-based lifetime reasoning.

### S6 — Shuf et al. 2002, POPL (prolific types)

**Citing papers scanned:** ~10 result entries reviewed across 2 searches; 1 PDF
fetched and reference-verified; 1 citing paper worth mining.

**Worth mining.** Cohen, N. & Petrank, E., "Data Structure Aware Garbage
Collector", *ISMM* 2015, pp. 28–40 (DOI 10.1145/2754169.2754176). **Forward
citation verified:** the paper cites [25] "Y. Shuf, M. Gupta, R. Bordawekar,
and J. P. Singh. Exploiting Prolific Types…" in its related work (verified by
text search of the PDF). Idea: a tiny program↔collector interface — the program
declares which classes are data-structure *node* classes and *notifies* the
collector when nodes are removed from their structures; the DSA collector
segregates node classes separately (locality, load balancing) and treats
removed-but-reachable nodes with reduced tracing priority. Wrong/missing hints
are performance-only, never correctness. Implementation in JikesRVM + Immix
adaptation; paper claims (their measurements): KittyCache 40–45% GC time / 4–20%
overall (8 lines changed), pseudo-SPECjbb2005 24–28% GC time / 2.8–6% overall
(4 lines), HSQLDB 75–76% GC time / 31–32% overall (3 lines), JikesRVM itself
11–16% GC time / 1–2% overall (8 lines). The transfer insight for graalgo: the
*programmer* notifications can be replaced by the *always-on profiler* (E4)
inferring node classes and removal sites automatically — no source changes.
→ candidate C-W3-4.

### S7 — Artigas et al. 2000, ICS (safe regions + alias versioning for Java)

**Citing papers scanned:** ~12 result entries reviewed across 2 searches.
**Negative:** no 2016–2026 citing papers found in managed-language JITs. The
idea already became SURV-010 (versioned loop-nest restructuring); the modern
loop-versioning literature lives in LLVM/GCC polyhedral work (T5's track).
Speculative *parallelization* (TLS/Jrpm) surfaced only as old work and theses
— no recent measured precedent → not proposed (near-miss NM-W3-4).

### S8 — Adl-Tabatabai et al. 2004, PLDI (hardware-monitor-guided prefetch injection)

**Citing papers scanned:** ~8 result entries reviewed. **Negative:** no
2016–2026 citing work on hardware-monitor-guided *compiler-inserted* prefetch
for managed runtimes found. The prefetch literature moved to hardware (neural/
ML prefetchers, e.g. the arXiv:1804.00478-style work) and static
compiler pragmas. SURV-029 stands on the original precedent; recorded as
first-class negative.

### S9 — Luk & Mowry 1996 (compiler prefetching for recursive data structures)

**Citing papers scanned:** ~8 result entries reviewed. **Negative:** same as
S8 — the modern descendants are Roth & Sohi's jump-pointer prefetching
(ISCA'99, hardware/cooperative) and hardware prefetchers, not managed-JIT
compiler insertion. No new candidate; SURV-029's precedent discussion already
covers the lineage.

### S10 — Chilimbi, Hill & Larus 1999, PLDI (ccmalloc/ccmorph; secondary seed)

Covered jointly with S4: no new managed-runtime citing papers 2016–2026.
Recorded under S4's negative.

---

## Candidates (raw — IDs assigned at synthesis)

### C-W3-1 — Closed-world value-field object inlining (pure-AOT) [vetted]

**The idea.** Fields declared (or inferred) as having *value semantics* get
their payload inlined into the parent object at the type level, in pure-AOT
mode under the closed-world assumption. No guards, no deopt — sound statically:
the whole-program view proves no identity exposure and no polymorphic child
types. Inlining runs early in the pipeline so method inlining + escape analysis
compound on the flattened accesses.

**Transfer to graalgo.** E5 (pure-AOT mode supplies the closed-world
assertion; `plugin`-free builds are statically detectable) + E1 (inline-vs-boxed
as a per-field representation decision in the descriptor model).

**Citable precedent.** Bruno, Jovanovic, Wimmer & Alonso, "Compiler-Assisted
Object Inlining with Value Fields", *PLDI* 2021 — **verified forward citation**
of Dolby 1997 [19] and Dolby & Chien 2000 [20]. Paper claims: up to 3×
throughput (OrientDB graph DB), up to 40% footprint reduction (Apache Spark),
up to 35% GC-pause reduction (Micronaut/Spring Boot); headers+references up to
54% of memory on the 6.3M-entry IMDB load.

**Bar check.** (a) semantics soundness ✓ — closed-world static proof, no
speculation; value-semantics fields have no observable identity. (b) named
benchmark plausibly beating gc+PGO ✓ — Go serialization/data-path workloads
(e.g. protobuf/JSON-heavy services, AST-like domain graphs) where
headers+references dominate; precedent's 3×/40% numbers are the plausibility
anchor. (c) ≥1 edge ✓ — E5+E1. (d) not in SURV-001…059 or backlog ✓ —
distinct from SURV-025, which is *speculative per-allocation-site* inlining
with guard/deopt; this is the *static closed-world* twin (no runtime cost
whatsoever). (e) gc doesn't do it ✓ — gc has no object inlining at all; its
escape analysis only scalar-replaces stack-bound objects.

**Buckets.** Candidate: **THINKING** (not built). Precedent: **THINKING**
(published PLDI'21 research, implemented as a GraalVM Native Image 20.3 build
phase — a research prototype, not a shipped product feature).

### C-W3-2 — Dominance-based allocation folding [vetted]

**The idea.** When the JIT proves allocation B is dominated by allocation A in
the same compiled region, fold both into a single *allocation group*: one
bump-pointer bounds check instead of two, guaranteed contiguity of the folded
objects, and — the deeper win — statically removable write barriers for
reference stores *within* the group (same generation, known layout). Unlike
object inlining, it requires no ownership/connectivity relation between the
objects.

**Transfer to graalgo.** E1 (the allocation group is a per-site/region
representation decision in the descriptor model; write-barrier elimination is
a property of the group descriptor).

**Citable precedent.** Clifford, Payer, Starzinger & Titzer, "Allocation
Folding Based on Dominance", *ISMM* 2014 — shipped in V8 (production).
Paper claims (their measurements): "strong performance improvement" on Octane
and Kraken, plus large impact on four hand-selected benchmarks (JPEGEncoder,
NBody, Soft3D, Textwriter), even with heap fragmentation. Lineage: the abstract
explicitly positions the work against "object inlining, object fusing, and
object colocation" (the Dolby S1/S2 family); reference list not verified
(PDF fetch throttled) — filed as lineage-adjacent, not a verified citation.

**Bar check.** (a) ✓ — pure layout/allocation transformation, semantics
preserved by the dominance safety analysis. (b) ✓ — alloc-heavy named
benchmarks (e.g. `encoding/json` decode, protobuf unmarshal) where allocation
bounds-check + write-barrier traffic is measurable; precedent's Octane/Kraken
wins are the anchor. (c) ✓ — E1. (d) ✓ — distinct from SURV-025 (object
inlining needs parent-child uniqueness; folding needs none), SURV-048
(append-chain fusion), SURV-058 (loop slab batching of same-type allocs —
folding is heterogeneous dominated allocs in straight-line code). (e) ✓ —
gc folds nothing and eliminates no write barriers this way.

**Buckets.** Candidate: **THINKING**. Precedent: **ABLE** (shipped in V8 per
the paper; current-V8 status not re-verified — noted).

### C-W3-3 — Speculative compiler-inserted explicit freeing (tcfree-style + guard/deopt) [vetted]

**The idea.** GoFree's static result first: an O(N²) escape analysis finds
heap objects with provably bounded lifetimes and the compiler inserts
`tcfree` calls at end-of-scope — a best-effort free primitive (thread-local
bump-pointer revert for small objects; 2-step async for large; fails safe to
GC whenever freeing would be costly or racy). graalgo's candidate goes one
step further: where static EA *bails* (imprecise points-to, interface
smuggling), the always-on profiler (E4) identifies last-use sites dynamically
and the JIT inserts *guarded* frees — a cheap liveness/escape check deopts to
GC-managed on violation. Static where provable (GoFree's result), speculative
where merely predictable.

**Transfer to graalgo.** E3 (guard + deopt is the soundness story for the
speculative frees) + E4 (profiler finds last-use/free sites static EA can't
prove).

**Citable precedent.** Peng, Zhang, Ernst, Chen & Ding, "GoFree: Reducing
Garbage Collection via Compiler-Inserted Freeing", *CGO* 2025 — region
lineage: §2.2 cites region-based memory management [1,6,25,39] (Elsman,
Boyapati, Hicks et al.) and states the ideas "apply to any runtime supporting
region-based or explicit deallocation"; **does not cite Lattner&Adve 2005
directly** (verified by text search) — filed as lineage-adjacent to S3, not a
verified forward citation. Paper claims (their measurements, Go programs):
14% of allocated heap deallocated at run time, −7% GC frequency, −13% GC time,
−2% wall-clock, −4% heap size, no observable compile-time slowdown, <2000 lines
of compiler+runtime changes.

**Bar check.** (a) ✓ — GoFree's fail-safe primitive (GC sweeps whatever tcfree
skips) plus our guard/deopt for the speculative sites. (b) ✓ — the precedent's
own Go measurements are the plausibility anchor; speculative coverage extends
it to EA-imprecise code. (c) ✓ — E3+E4. (d) ✓ — distinct from SURV-027
(bulk-free *regions* — whole-pool reclamation vs per-object free insertion),
SURV-003 (scoped arenas), SURV-056 (dead-object *slot reuse* bypasses the
allocator; explicit freeing *returns* memory to it — complementary, flagged
for the coordinator's boundary review). (e) ✓ — gc has no explicit
deallocation path at all.

**Buckets.** Candidate: **THINKING**. Precedent: **THINKING** (published
CGO'25 research, open-sourced; not shipped in gc).

### C-W3-4 — Profiler-inferred data-structure-aware GC (automatic DSA) [raw]

**The idea.** Cohen & Petrank's DSA collector, minus the programmer: the
always-on profiler (E4) infers which types are data-structure *node* classes
and which program points are *removal* sites; the GC segregates node classes
into dedicated areas (locality + tracing load balance) and deprioritizes
tracing of removed-but-still-reachable nodes. Inference errors are
performance-only by construction (the paper's design already guarantees wrong
hints can't cause incorrect reclamation — we keep that invariant for inferred
hints).

**Transfer to graalgo.** E4 (the profiler *is* the mechanism — it replaces the
paper's 3–8 lines of manual annotations); E1 (per-structure segregation policy
in the descriptor model).

**Citable precedent.** Cohen, N. & Petrank, E., "Data Structure Aware Garbage
Collector", *ISMM* 2015 — **verified forward citation** of Shuf et al. 2002
[25] in related work. Paper claims (their measurements, JikesRVM): KittyCache
40–45% GC time / 4–20% overall; pseudo-SPECjbb2005 24–28% GC time / 2.8–6%
overall; HSQLDB 75–76% GC time / 31–32% overall; JikesRVM-on-DaCapo 11–16% GC
time / 1–2% overall; non-DSA programs show negligible overhead (one class-load
conditional + one per-GC-cycle conditional).

**Bar check.** (a) ✓ — wrong-hint-safety is the paper's core design property;
inferred hints keep it. (b) ✓ — cache/map/tree-heavy named benchmarks (Go
equivalents of the paper's KittyCache/HSQLDB/SPECjbb shape); precedent's
up-to-32% overall numbers are the anchor. (c) ✓ — E4 (+E1). (d) ⚠ boundary
question for the coordinator — proximity to SURV-028 (prolific-type-driven
allocation specialization): SURV-028 is *type-keyed allocation policy*
(recycling, compact tags); this is *data-structure-instance segregation +
removal-event liveness*. Related but arguably distinct axes (instance
structure vs type frequency); marked raw pending the boundary call. (e) ✓ —
gc has no data-structure-aware collection policy.

**Buckets.** Candidate: **THINKING**. Precedent: **THINKING** (published
ISMM'15 research, JikesRVM prototype).

---

## Near-misses (surveyed, not proposed)

- **NM-W3-1 — Hardware-monitor-guided prefetch (S8).** No 2016–2026
  managed-runtime citing work found; the prefetch literature moved to hardware
  (neural/ML prefetchers) and static pragmas. SURV-029 stands on the original
  precedent. Fails the evidence bar for any *new* claim.
- **NM-W3-2 — Cache-conscious GC copying (S4).** No 2016–2026 managed-runtime
  descendants with measurements; the line continues only in C/C++ allocators.
  SURV-039 stands. Fails evidence bar.
- **NM-W3-3 — Artigas-style safe-region/alias-versioning (S7).** No recent
  managed-language citing papers; already SURV-010; modern loop versioning is
  LLVM/GCC polyhedral (T5's track). Nothing new to propose.
- **NM-W3-4 — Thread-level speculation for managed languages.** Only old work
  (Jrpm, theses) surfaced — no recent measured precedent; fails evidence bar.
  Not proposed (would also need a semantics story for Go's memory model).
- **NM-W3-5 — Memento Mori pretransitioning for Go.** Representation
  transitions are dynamic-language-specific; the Go analog is subsumed by the
  backlog's speculative data representation. Memento Mori's presizing →
  SURV-017's ABLE precedent; its pretenuring → complementary angle on
  SURV-026; its memento *mechanism* → E4-sensor idea for §4.
- **NM-W3-6 — GoFree as a direct S3 forward citation.** GoFree does not cite
  Lattner&Adve 2005 (verified); it cites the region-MM family instead. Filed
  honestly as lineage-adjacent.

## First-class negative results

1. **S4 (Chilimbi&Larus ISMM'98):** zero 2016–2026 managed-runtime citing
   papers found across two targeted searches. GC-time cache-conscious
   placement appears to be a dead research line in managed runtimes.
2. **S7 (Artigas et al. ICS'00):** zero 2016–2026 managed-language-JIT citing
   papers found. The versioning idea survives only in static-compiler
   polyhedral work.
3. **S8 (Adl-Tabatabai et al. PLDI'04):** zero 2016–2026 citing papers on
   hardware-monitor-guided compiler prefetch insertion found.
4. **S9 (Luk&Mowry):** modern descendants are hardware/cooperative
   (Roth&Sohi jump-pointer prefetching) — no managed-JIT compiler-insertion
   papers 2016–2026.
5. **S5 (Seidl&Zorn):** no direct 2016+ citing paper found; the live branches
   are Memento Mori (JS/V8, 2015, ABLE) and GoFree (Go, 2025, EA-based).
6. **Methodology note (honesty):** Semantic Scholar's API returned HTTP 429
   and the page-fetch service was throttled during this sweep, so direct
   "cited-by" enumeration was not possible. Forward-citation discovery was done
   via targeted web search plus PDF text-fetch verification of reference lists
   (Dolby refs in PLDI'21 [19][20]; Shuf ref in DSA-GC [25]; absence of a
   Lattner ref in GoFree). Citation claims above say exactly what was verified.

## Evidence honesty

Every performance number in this file is the cited paper's own claim
("paper claims X"), never ours and never measured by us. Candidate buckets
apply to the candidate and its precedent separately, per the program-owner
refinement. New SURV- IDs are assigned by the coordinator at synthesis;
candidates above are unnumbered (C-W3-1…4, three vetted + one raw).

## Sweep stats

- Searches run: ~15 targeted searches; ~120 result entries triaged; ~14
  entries read in depth.
- PDFs fetched and text-verified: 3 (PLDI'21 value fields; DSA GC ISMM'15;
  GoFree CGO'25). 1 fetch failed (allocation-folding PDF — throttled; its
  abstract is quoted from the publisher page).
- Reference lists verified by in-document text search: PLDI'21→Dolby'97/Chien'00;
  DSA-GC→Shuf'02; GoFree↛Lattner'05 (absent), GoFree→region-MM family (present).
- Citing papers worth mining: 4 (PLDI'21, ISMM'14 allocation folding, CGO'25
  GoFree, ISMM'15 DSA GC) + 1 ABLE mechanism paper (ISMM'15 Memento Mori,
  filed as precedent/angle, not a candidate).
- Raw candidates: 4 (3 vetted, 1 raw pending SURV-028 boundary review).
- Also spotted for other tracks (not mined here): "GraalNN: Context-Sensitive
  Static Profiling with Graph Neural Networks" (Milikic et al., CGO'25) and
  "Improving Native-Image Startup Performance" (Basso et al., CGO'25) — T2/T5
  may want them.
