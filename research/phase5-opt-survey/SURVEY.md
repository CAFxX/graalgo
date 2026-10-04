# Phase-5 Optimization Survey (living document)

**Program:** graalgo — Go toolchain on GraalVM/Truffle. **Track:** Phase-5 background survey.
**Status:** wave 1 synthesized 2026-10-04; this document is LIVING — waves continue indefinitely
until the program owner says stop or Phase 5 begins. Never treat it as finished.
**Coordinator:** `graalgo-opt-survey` (work registry; hourly heartbeat; checkpoints to disk).
**Survey dir:** `~/workspace/graalgo/research/phase5-opt-survey/`
(per-track notes: `track-1-academic.md`, `track-2-jvms.md`, `track-3-dynamic-runtimes.md`,
`track-4-go-adjacent.md`, `track-5-native-compilers.md`).

## Method and rules (standing)

- **Phase-5 rule:** every claimed optimization must beat stock gc **with PGO** on a named
  benchmark, or the claim is struck. Pure-parity work (warmup, compile latency) is not a
  candidate — it is filed as enabling infrastructure or a near-miss.
- **Candidate bar (all must hold, or the failing leg is named):** (a) semantics-preserving
  for Go, or a sound guard/deopt story; (b) a named benchmark where it plausibly beats
  gc+PGO; (c) at least one architectural edge; (d) not already in the Phase-5 backlog
  (PHASES.md §7) and not something gc already does well (basic inlining, gc's escape
  analysis, bounds-check elimination are excluded).
- **Buckets** (per program-owner refinement): **ABLE** = shipped in production today;
  **THINKING** = roadmaps/RFCs/active proposals, not yet shipped; **ATTEMPTED** = tried and
  abandoned/reverted, WITH the reason (the failure reason names the obstacle; stated
  explicitly when our architecture routes around it); **WOULD-LIKE** = wishlist writeups,
  "we wish the runtime did X". Each candidate carries its own bucket plus its precedent's
  bucket — they compose (e.g. a WOULD-LIKE candidate on an ABLE precedent).
- **Evidence honesty:** "paper claims X" ≠ "we verified X". All performance numbers below
  are the cited source's claims, never our measurements. Prototype-gated items are marked.
- **This survey proposes; it never disposes.** Do NOT edit SPEC.md, PHASES.md, or the
  requirements registry — backlog promotion is a separate decision.

**Architectural edges** (cite ≥1 per candidate): (E1) descriptor-based value model —
representation ≠ semantics, per-value/per-allocation-site layout decisions; (E2) user-space
goroutine scheduler with precise signals (holder-running, waiter counts, true idleness);
(E3) speculative JIT with guards + deoptimization; (E4) planned always-on profiler
(field-access + array-access + value-range + representation-stability + store-count
profiles); (E5) three execution modes (JIT-only, pure AOT, hybrid AOT+JIT).

**How to read the ranking:** tiers ordered by expected value × feasibility as judged from
wave-1 evidence. Ranks are the coordinator's synthesis, not measurements; re-rank when
wave 2+ brings evidence. IDs are stable (`SURV-###`) — new candidates get new IDs, never
renumbered. Merges across tracks are noted so traceability survives.

---

## §1 Ranked candidates (waves 1–6) — 77 active ranked (SURV-001…078 minus struck SURV-077; tiers 7/39/17/14)

### Tier 1 — build first (highest expected value × feasibility)

#### SURV-001 — Closed-world whole-program AOT devirtualization + inlining
**Bucket:** WOULD-LIKE (precedents: **ABLE** — GCC `ipa-devirt`; **ABLE** — HHVM
repo-authoritative + HHBBC; **ATTEMPTED** — HPHPc, whose obstacle is absent in Go).
**Idea.** Pure-AOT mode with a build-time closed-world assertion (no `plugin` import —
statically detectable): whole-program analysis enumerates all interface implementations
(Go cannot create new named types at runtime), devirtualizes every monomorphic interface
call with no guards, inlines cross-package, drops dead methods. Sound static analysis,
not speculation.
**Precedent.** GCC Internals: `ipa-devirt` "performs speculative devirtualization based on
the type inheritance graph" (plus `-fdevirtualize -fdevirtualize-speculatively` at `-O2`);
HHVM repo-authoritative mode "assumes that no code exists outside of what's already in
the currently-loaded repo" (+ HHBBC whole-program bytecode optimization); Swift SE-0193
cross-module inlining/specialization as the mechanism shape. HPHPc (PHP→C++) was abandoned
because eval/dynamic includes defeated whole-program compilation — that obstacle does not
exist in statically-typed Go; the only hole is `plugin`, which is statically detectable.
**Why gc doesn't do it.** Separate compilation (one package at a time); the runtime must
support `plugin`; the linker has no whole-program view. gc's devirtualization is
per-call-site static proof or frozen PGO profile (`devirtualize/pgo.go`) — it cannot know
"this interface has exactly one implementation in the final binary."
**Architectural fit.** E5 (pure-AOT mode) + E1. Composes with the backlog's speculative
devirtualization cascade: AOT proves the monomorphic sites statically, the JIT speculates
on the rest in hybrid mode.
**Benchmark sketch.** `net/http` JSON API server (Handler chains, io.Reader/Writer
indirection); `database/sql` with driver indirection; any interface-heavy service binary.
graalgo-AOT vs gc+PGO. Claim dies if AOT devirt ≤ gc+PGO devirt.
**Open risks.** `plugin` users must get a loud build error, never a silent pessimize;
`reflect`-constructed types must be visible to the analysis or force a conservative bail;
cross-package inlining needs code-bloat budgets.
**Tracks:** T3-C6, T5 (merged — same mechanism family; T3's framing is the
repo-authoritative analog, T5's is the ipa-devirt analog).

#### SURV-002 — Speculative per-call-site escape analysis behind interface calls
**Bucket:** WOULD-LIKE (no direct precedent — the obstacle is structural to AOT; guard
mechanics from **ABLE** JSC/.NET speculation discipline).
**Idea.** `w.Write(b)` with `w io.Writer`: gc heap-allocates `b` because *some*
implementation could retain it. With the always-on profiler we know the hot concrete
types at the site and prove *those* don't retain the argument; compile a fast path that
stack-allocates `b` and dispatches to the known types (type-check cascade), falling back
to the generic heap path for unseen types (recompile/extend — a guard, not UB).
**Precedent.** golang/go#72036 (OPEN): even with PGO devirtualization rewriting
`w.Write(b)` into a type-asserted fast path, "the byte slice still must be heap allocated
given the function can be called with something other than `GoodWriter`" — the issue
names the exact open-world obstacle a speculative guard discharges. Machinery cousin:
golang/go#77088 (parameterized escape summaries for the inliner).
**Why gc doesn't do it.** The set of possible implementations is open; AOT must be sound
for all of them, so the conservative answer (heap) is the only answer.
**Architectural fit.** E3 (guard on the dynamic-type set + fallback; recompile when a new
type appears — the textbook JIT speculation win gc structurally cannot do) + E4 (profiler
supplies the hot-type set and per-type non-retention evidence). Complements (does not
duplicate) the backlog's devirtualization cascade: that item devirtualizes the *call*;
this one stack-allocates the *argument* using the same type knowledge.
**Benchmark sketch.** `io.Writer`-style hot paths: JSON/log encoders writing into an
`io.Writer`, `fmt.Fprintf` wrappers, `hash.Hash` writes. Measure allocs/op → 0 on the
fast path.
**Open risks.** Non-retention must be proven per concrete callee (needs an escape summary
per type); the type-check cascade cost must stay below the alloc savings (cap the
hot-type set, e.g. ≤3); adversarial type churn could ping-pong recompiles (rate-limit).
**Tracks:** T4-C4.

#### SURV-003 — Scoped arenas for dynamic-size non-escaping allocations
**Bucket:** WOULD-LIKE (precedent: open issue, no shipped analog in a Go toolchain).
**Idea.** `make([]T, n)` with non-constant `n` that provably doesn't escape is
heap-allocated by gc for lack of `alloca`. Allocate it from a per-goroutine scoped arena
(bump pointer, freed at function return — explicit scope, not the Go stack, so no
interaction with stack growth/copying), skipping both the heap alloc and the write
barrier. Lifetime bounded by the frame; arena freed on return/panic via existing
defer/panic machinery. A size guard falls back to heap above a threshold.
**Precedent.** golang/go#20533 (OPEN since 2017): "improve escape analysis of
`make([]T, n)` where n is non-constant"; motivating workload named in the issue: **a
bytecode interpreter's operand stack** (heap alloc + write barrier per function entry).
The issue suggests decoupling "escapes to heap" from "requires a write barrier", or
`alloca` — never built.
**Why gc doesn't do it.** gc's stack-frame layout is fixed-size; true `alloca` conflicts
with stack copying/growth bookkeeping. The obstacle is frame-layout rigidity in the AOT
pipeline.
**Architectural fit.** E1 (per-allocation-site layout: this site's backing array lives in
the arena, invisible to the type system) + E2 (arena lifetime tied to goroutine
scheduling; precise "function returned" signal; arena recycled across calls on the same
goroutine). A JIT owning frame layout can do what AOT gc cannot.
**Benchmark sketch.** The issue's own motivating workload: bytecode interpreter with a
non-constant-size operand stack. Also: `bytes.Buffer`-style growth in parsers, JSON
decoders with dynamic scratch.
**Open risks.** Panic paths must free the arena (hook into panic unwinding); recursion
depth × arena size bounded by guard; must not regress when `n` is huge (size-guard
fallback is a natural JIT branch).
**Tracks:** T4-C2. (Distinct from SURV-027: that is region inference for disjoint
structures; this is per-function-scope arenas for dynamic-size non-escaping allocs.)

#### SURV-004 — Speculative monomorphic func-value call sites
**Bucket:** WOULD-LIKE (precedents: **ABLE** — V8 call inline caches; **ABLE** — .NET
dynamic-PGO guarded devirtualization).
**Idea.** At indirect-call sites through func values, the JIT caches the observed target.
Guard = one pointer compare on the func value; on hit, direct-call and inline the body.
Miss → generic indirect call (diamond speculation, no deopt) or deopt + reprofile.
**Precedent.** V8 inline caches for calls (uninitialized→monomorphic→polymorphic→
megamorphic); .NET dynamic PGO emitting a type-checked direct call with the virtual call
as fallback ([.NET performance blog](https://devblogs.microsoft.com/dotnet/performance-improvements-in-net-11/)).
**Transfer note.** A Go func value's identity IS the guard (one pointer compare; no shape
polymorphism). This is NOT the backlog's interface devirtualization (different dispatch
mechanism: func values, not itabs).
**Why gc doesn't do it.** No speculation infrastructure; an indirect call through a func
value is opaque to the optimizer; PGO cannot invent guards.
**Architectural fit.** E3.
**Benchmark sketch.** `sort.Slice` / `slices.SortFunc` with a fixed comparator closure in
a tight loop — comparator inlined, call overhead gone, loop optimizable. Secondary:
`strings.Map`-style mapper funcs.
**Open risks.** Megamorphic sites (per-request-varying handler funcs) must not pessimize —
polymorphism cap + fallback; guard placement must not blow inlining budgets.
**Tracks:** T3-C1.

#### SURV-005 — PGO-driven code layout (hot/cold splitting, block placement, function reordering)
**Bucket:** WOULD-LIKE (precedents: **ABLE** — LLVM `-block-placement`; **ABLE** — GCC
`-freorder-blocks-and-partition -freorder-functions` at `-O2`; **ABLE** — Clang
`-fbasic-block-sections`).
**Idea.** In AOT mode, use the always-on profiler's block/function hotness to (1) split
cold blocks out of hot functions, (2) lay out basic blocks by profile, (3) order
functions hot→cold. gc emits one text section per function in compile order; PGO never
touches layout.
**Precedent.** LLVM "Profile Guided Basic Block Placement"; GCC Optimize Options; Clang
basic-block sections. **Verified** from gc 1.27.1 source: gc's PGO feeds exactly two
consumers — inlining budgets and interface devirtualization. No PGO block layout, no PGO
unrolling, no value profiling.
**Why gc doesn't do it.** gc's backend has `likelyadjust` (static heuristics) and
`layout` passes but no profile input to either; PGO stops at the inliner.
**Architectural fit.** E4 (always-on profiler gives block hotness for free — LLVM/GCC
need a separate instrumented training run) + E5 (pure-AOT and hybrid image builds).
**Benchmark sketch.** i-cache-sensitive large binary: build `cmd/compile` with
graalgo-AOT vs `go build`, measure wall time of a fixed compile workload and `perf
stat` i-cache miss rate; plus a branchy server benchmark (`net/http` hello-world
throughput). The comparison vs gc+PGO is clean — layout is the one PGO dimension gc
leaves entirely on the table.
**Open risks.** Profile staleness across runs (re-profile continuously, re-layout at
rebuild); cold-start code bloat from splitting.
**Tracks:** T5.

#### SURV-006 — Priority inlining + method path duplication
**Bucket:** WOULD-LIKE / adapt (precedent: **ABLE** — GraalVM 25.3 `PriorityInliningPhase`
GR-77137 "extensive analysis of the call graph when making inlining decisions", now the
default inliner; **ABLE** — GraalVM `MethodDuplicationPhase` "duplicates selected
control-flow paths within a method to expose further optimization opportunities";
GraalVM 25.5 extended low-tier phases — per Graal `compiler/CHANGELOG.md`).
**Idea.** Replace bytecode-size/inlining-budget heuristics with a *simulation* of
inlining benefit over the call graph (using profile data), and duplicate hot
control-flow paths inside a method so downstream opts (GVN, EA, vectorizer) see
straight-line code. gc's inliner is budget-based with PGO hot-edge tweaks — it never
simulates the *consequence* of inlining, and never duplicates paths.
**Why gc doesn't do it.** No call-graph benefit simulator; PGO only re-weights existing
heuristics. Path duplication is a code-size gamble gc won't take without deopt to
retreat.
**Architectural fit.** E3 (speculative: duplicate speculatively, deopt if the cold path
was actually hot) + E4 (profiler feeds the benefit model).
**Benchmark sketch.** Deep-call-chain workloads where inlining decisions dominate —
compiling a large Go module, or an HTTP router benchmark with layered middleware; must
beat gc **with PGO** (PGO already fixes the easy hot-edge cases; the win must come from
non-obvious simulation wins).
**Open risks.** Compile-time cost of simulation (budget it; cap by method); code bloat
from duplication hurting i-cache — needs the profiler's hot/cold split to be right.
**Tracks:** T2.

#### SURV-041 — Profile-guided per-spawn-site initial goroutine stack size
**Bucket:** THINKING (precedent: open issue with fleet data, no shipped design).
**Idea.** The always-on profiler maintains per-`go`-statement stack-use histograms
(max/typical); new goroutines start with a right-sized stack drawn from a cached
pool of sized stacks instead of the fixed 8 KB-then-copy-grow. Pure runtime policy —
no code change; growth still works as the fallback, so there is no soundness risk
(stack size is unobservable).
**Precedent.** golang/go#77893 — fleet-measured: `runtime.newstack`/`copystack`
costs **3.9% core-weighted** vs 7.3% for GC; per-service P50 12 KB vs 4 KB start.
Family: #18138 (per-spawn-site stack statistics, 2016), #27345.
**Why gc doesn't do it.** gc on master *does* have an adaptive global
starting-stack heuristic (randall77, #77893) — the gap is narrower than first
stated: gc's heuristic is global, mean-based, sampled at GC time, and the
thread shows exactly those three properties failing (dormant-goroutine skew
per thepudds, universal bimodality per geekswaroop, one-GC-cycle feedback lag
on bursty populations). What gc still lacks — and no Go-team CL proposes (all
five 2026 CLs on #77893 are unmerged as of 2026-10-04) — is per-spawn-site
sizing. (All numbers above are the issue reporter's fleet data, not our
measurements.)
**Architectural fit.** E4 (the profiler *is* the mechanism) + E2 (the scheduler
owns spawn).
**Benchmark sketch.** Fan-out/fan-in spawn-heavy server workload; metrics =
copystack CPU + tail latency. Claim dies if right-sizing ≤ fixed-start on
spawn-heavy shapes.
**Open risks.** Histogram staleness under phase change (decay the histograms);
pool fragmentation across sizes. **Obstacle catalog from #77893 (T4 wave 3):**
(a) randall77's rare-large-stack contamination objection — cap pooled sizes and
shrink aggressively; per-site histograms are immune by construction; (b) rate-
limit downward histogram adjustments (one size class per GC) against bursty-
population oscillation; (c) rabbbit's recency-reuse fleet data (~4% → ~0.2%
copystack with a trivial patch) is the feasibility anchor — recency alone
works, per-site histograms should work better; (d) histogram key = (spawn-site
PC, callee address) per CAFxX's refinement (handles `workgroup.Go`/`errgroup.Go`
fan-out where the PC alone is multi-modal), falling back to PC-only.
**Tracks:** T4-C-J (wave 2).

### Tier 2 — strong; some conditionality

#### SURV-007 — Aggregate copy elision via field-use projection
**Bucket:** WOULD-LIKE (precedent: open Go issues; no shipped analog).
**Idea.** gc copies whole aggregates by value then reads a few fields: (1) call sites
using only some fields of a by-value struct return (`h.Value().typ` → whole 40-byte
struct copied = 6 MOVUPS to read 1 byte); (2) `for _, v := range sliceOfStructs` loops
copying each element per iteration even if the body touches one field (documented ~2x
cost on trivial bodies). The JIT computes the field-use projection at the hot use site
and materializes only the used fields — per-call-site callee specialization for (1)
(no deopt: projection is a static property of the call site), loop-by-index rewrite
loading only used fields for (2).
**Precedent.** golang/go#77720 (CL 748200 fixed only the *small*-load case);
golang/go#33838 ("unnecessary array copying in for range loop", 2019, still open);
golang/go#31588 ("I could imagine the compiler looking at the contents, establishing
that only one item is being accessed, and just copying that one item"; related #24416);
golang/go#79819 (maintainer: "the compiler could do a better job here. It seems like a
pretty low priority case though").
**Why gc doesn't do it.** AOT ABI fixes callee return layouts (needs inlining or a second
callee version = code bloat without usage data); range-value copy elision is judged "low
priority" — the obstacle is cost/benefit in a static compiler, not soundness.
**Architectural fit.** E1 (per-call-site / per-loop representation decisions, not
type-global) + E4 (profiler identifies hot projection sites) + E3 (JIT clones only hot
sites — pay-as-you-go code, flipping gc's cost/benefit). gc+PGO has profile data but no
per-site variant mechanism.
**Benchmark sketch.** `unique.Handle[T].Value().field` hot loop (#77720); `for _, v :=
range structs` with single-field body vs. `for i := range` rewrite (#31588's ~2x case).
**Open risks.** Proving no other observable effects (address-taken values, reflection,
`v` escaping the loop body); generics per-instantiation projections; must not fight the
inliner (don't project what's already scalarized); range case must preserve "evaluate
once" semantics for the range expression.
**Tracks:** T4-C3.

#### SURV-008 — Optimistic aliasing speculation
**Bucket:** WOULD-LIKE (precedent: **ABLE** — Graal's optimistic aliasing analysis).
**Idea.** Where static alias analysis bails (two slices *might* overlap), speculate
no-alias, emit the vectorized/aggressively-scheduled code under a cheap guard, deopt on
actual aliasing. gc's load/store motion must stay conservative because gc cannot deopt —
so any *may*-alias kills the optimization for everyone.
**Precedent.** Graal release notes: "Strengthened the optimistic aliasing analysis by
adding a non-speculative mode. (Improves loop vectorization.)"
**Why gc doesn't do it.** No guards, no deopt — speculation is not expressible. Go's
simplicity *helps* here: value semantics and no inheritance mean the may-alias sets are
smaller and cheaper to guard than in Java/C++.
**Architectural fit.** E3 (the whole trick) + E1 (descriptor model can carry an "alias
class" tag per value to make guards cheap). This is the *enabler* for the backlog
vectorization item — keep them in one design doc.
**Benchmark sketch.** Slice kernels where inputs may alias — `bytes`-package-style
transforms, image-resize-like stencils, `crypto/sha256`-adjacent block loops called with
overlapping src/dst; compare vs gc+PGO on throughput. Honest bar: measure the guard
overhead on the *aliased* (deopt) path too — a benchmark that never aliases overstates
the win.
**Open risks.** Guard placement (per-loop vs per-call); false-sharing-style perf
pathologies when the guard itself becomes the bottleneck.
**Tracks:** T2.

#### SURV-009 — Value-profile-driven arithmetic specialization
**Bucket:** WOULD-LIKE (precedents: **ABLE** — CPython PEP 659 specialization families
with guards and de-specialization; **ABLE** — JSC value profiling feeding speculation).
**Idea.** The always-on profiler records *value* facts at hot integer ops (divisor
constant, shift-amount constant, operand ranges). The JIT emits strength-reduced code
(div-by-profile-constant → multiply-by-magic; `x << k`) behind a value guard; miss →
generic op (diamond, no deopt). Saturating-counter de-specialization policy on repeated
failure.
**Precedent.** [PEP 659](https://peps.python.org/pep-0659/) `BINARY_OP` specialization
families; [JSC speculation blog](https://webkit.org/blog/10308/speculation-in-javascriptcore/).
**Transfer note.** Go is statically typed: we speculate on *values*, never types —
simpler guards than the dynamic-language originals.
**Why gc doesn't do it.** Strength reduction requires compile-time-constant divisors;
profile-constants are invisible to AOT.
**Architectural fit.** E3 + E4 (value-range profiling).
**Benchmark sketch.** Hash-table probe `h % nbuckets` with runtime-sized tables; image
row-stride division `i / stride`; any hot 64-bit div/mod by a runtime-stable value
(div ~30–90 cycles → multiply ~3 cycles).
**Open risks.** Guard-branch mispredict cost if the value is less stable than profiled;
magic-number codegen must be proved exact (differential tests against runtime div).
**Tracks:** T3-C2.

#### SURV-010 — Versioned loop-nest restructuring (safe/unsafe multi-versioning + transform suite)
**Bucket:** WOULD-LIKE (precedents: **ABLE** — LLVM LoopVectorize's runtime-check
multi-versioning; **ABLE** — GCC `-O3` loop flags; **THINKING** — Artigas et al. safe
regions, ICS 2000).
**Idea.** gc performs loop optimizations conservatively: mandatory bounds/nil panics
and gc's alias analysis block most loop-nest restructuring (interchange, tiling, fusion
across potentially-aliasing slices, unswitching, unroll-and-jam, strength reduction of
indexed addressing, peeling). Version the nest: a "safe" version (checks
hoisted/proven, aggressive interchange/tiling/fusion/unswitch applied) and a conservative
version, selected per invocation by a cheap runtime test (bounds + nil + pointer
inequality). When the vectorizer can't prove no-aliasing or exact trip counts, emit
vector + scalar versions under the same guard instead of giving up.
**Precedent.** Artigas, P. V., Gupta, M., Midkiff, S. P. & Moreira, J. E., "Automatic
Loop Transformations and Parallelization for Java", *ICS* 2000 (safe-region creation by
duplicating loop nests and stripping checks under a runtime test + *alias versioning*
via cheap pointer comparisons; paper claims 80–100% of highly-tuned Fortran performance
on numerical Java benchmarks, up to 3.8× parallelization speedup on 4 CPUs). LLVM
LoopVectorize's "runtime memory checks" when aliasing can't be disproven (shipped
mainstream behavior). GCC `-O3` enables `-fpeel-loops -funswitch-loops
-floop-unroll-and-jam -floop-interchange`.
**Transfer note.** Go helps: no exceptions-as-control-flow (the ICS paper's main enemy
was Java's precise exception model); Go slices carry exact bounds, making the hoisted
bounds test cheap and exact. gc has no unroller at all (verified in SSA pass list) and
no loop versioning with runtime alias tests.
**Why gc doesn't do it.** Deliberate compile-time/simplicity tradeoff; no trip-count
knowledge at compile time; no versioning machinery.
**Architectural fit.** E3 (test-then-dispatch is speculation in its mildest form; JIT
mode can deopt-and-recompile instead of keeping both versions) + E4 (trip-count and
aliasing profiles pick *which* nests to version — static compilers must guess, which is
where speculative JIT beats `-O3`-style heuristics structurally).
**Benchmark sketch.** Matrix multiply / transpose / stencil kernels — a Go
`BenchmarkMatMul` (gonum-style) with unfavorable loop order, image-processing
convolutions; `bench_simd` shapes (saxpy, dot product over `[]float64` with unknown
aliasing). Claim dies if ≤1.2× gc+PGO. Also the natural on-ramp for backlog
vectorization (better nests vectorize better).
**Open risks.** Test cost vs nest size — only pays for large trip counts; profiler must
gate. Code bloat from duplicated nests. Go's slice-header indirection complicates the
alias test vs Java arrays — needs design. Unrolling without trip-count evidence bloats
i-cache (the classic `-O3` regret).
**Tracks:** T1-C8, T5 (merged — T1-C8 is the safe/unsafe versioning mechanism, T5's
multi-versioning is the vectorizer application, T5's loop-nest suite is the transform
repertoire).

#### SURV-011 — Parameter-based value specialization of hot functions
**Bucket:** WOULD-LIKE (precedent: Costa, I. R. de A. et al., "Just-in-time value
specialization", 2014 — IonMonkey parameter-based specialization; paper reports ~60% of
JS functions called once or with identical parameters, claims +4.83% on the V8 suite;
classic ancestor: Cooper, Hall & Kennedy procedure cloning).
**Idea.** The profiler observes a hot function always (or overwhelmingly) called with the
same parameter values — small enums, mode flags, constant bounds. JIT-compiles a
specialized version with those values constant-folded (branches folded, dead code
eliminated, strength reduction), guarded by a cheap parameter check that deopts to the
generic version on mismatch.
**Transfer note.** Go is statically typed: pure *value* specialization, no type
uncertainty to manage — simpler guards, fewer deopts than the JS original.
**Why gc doesn't do it.** No value profiling and no specialization: a function taking a
`mode int` that is always 2 still compiles the fully general body. gc+PGO only uses
profiles for inlining/layout.
**Architectural fit.** E4 (value-range profiles are exactly the planned profiler output)
+ E3 (guard + deopt).
**Benchmark sketch.** `text/template` rendering with a fixed data shape; a hot numeric
loop calling `f(x, order)` with loop-invariant `order` (specialization folds the
dispatch inside the loop body).
**Open risks.** Code bloat if too many specializations kept — one-specialization-at-a-
time budget (cf. Suganuma's limit of one code duplication per method). Guard cost on
every call — must be a single compare. Value instability across phases → deopt storms;
needs a deopt budget discipline.
**Tracks:** T1-C4.

#### SURV-012 — Interprocedural const-arg specialization / function cloning
**Bucket:** WOULD-LIKE (precedent: **ABLE** — GCC `ipa-cp` at `-O3`, incl.
`-fipa-cp-clone`; LLVM `-ipsccp` propagates but doesn't clone).
**Idea.** When whole-program or profile evidence shows a function is always called with
the same constant argument values, clone it and specialize the clone: fold the
constants, delete dead branches, shrink the body. In JIT mode specialize speculatively
with a guard on the argument value + deopt; in AOT mode specialize only
provably-constant call sites (closed world).
**Precedent.** GCC Internals: ipa-cp "can also do partial specialization" for functions
"always invoked with some arguments with the same known constant values."
**Why gc doesn't do it.** No interprocedural constant propagation at all (verified: no
ipsccp in SSA passes) and no function cloning — inlining is gc's only cross-call
optimization. PGO never feeds value profiles.
**Architectural fit.** E4 (always-on profiler records argument-value distributions per
call site — GCC needs `-fprofile-values` training runs; we get value profiles free) +
E3. Disjoint from the backlog: "speculative data representation" specializes *data*;
this specializes *code* on observed argument values. Related to SURV-011 (runtime-value
specialization) but distinct: this is the *static/AOT* variant driven by whole-program
constness, SURV-011 is the *JIT* variant on observed values — they compose.
**Benchmark sketch.** Templating/formatting hot paths with effectively-constant
arguments (`text/template` execution with a fixed template; `fmt.Fprintf` with a
constant format string in a loop): specialized clone vs gc+PGO.
**Open risks.** Clone explosion (cap clones per function, GCC's discipline);
value-profile staleness — guard, don't assume, in JIT mode.
**Tracks:** T5.

#### SURV-013 — Speculative loop unswitching / branch hoisting with OSR exit
**Bucket:** WOULD-LIKE (precedents: **ABLE** — JSC OSR-exit speculation; **ABLE** — YJIT
lazy basic-block versioning).
**Idea.** Profile says a branch inside a hot loop is never (or rarely) taken — feature
flags, `if err != nil`, debug checks. The JIT hoists the check to a single loop-entry
guard; the loop body becomes branchless. Guard failure → OSR exit to the unoptimized
version. With no control-flow merge, downstream checks become redundant (JSC's key
OSR-exit property).
**Precedent.** [JSC speculation blog](https://webkit.org/blog/10308/speculation-in-javascriptcore/)
("OSR speculation has higher benefit"; after a hoisted check the compiler treats the
fact as established); YJIT's Lazy Basic Block Versioning.
**Transfer note.** We speculate on *branch outcomes*, not types. Distinct from
SURV-010's unswitch: this is the OSR-exit *mechanism* (sound without keeping fallback
paths in hot code); SURV-010 is the transform repertoire. They compose — this is
arguably the enabling mechanism.
**Why gc doesn't do it.** PGO can lay out cold blocks but cannot delete the branch;
nothing justifies elimination without deopt.
**Architectural fit.** E3 + E4 (branch profiles). Requires real OSR-exit machinery —
filed as enabling infrastructure (§4).
**Benchmark sketch.** Request-handler loops with feature-flag branches; checksum/hash
loops with never-taken error checks; batch loops with per-item `if debug` logging
checks.
**Open risks.** Phase changes in branch behavior cause exit storms → re-versioning
policy with hysteresis (ties to SURV-037); needs the deopt/OSR machinery to exist
first.
**Tracks:** T3-C5.

#### SURV-014 — Cross-run speculation-profile cache
**Bucket:** WOULD-LIKE (precedents: **ABLE** — OpenJ9 shared-class-cache AOT + JITServer
server-side AOT cache; **ABLE** — Azul ReadyNow "reuses compiler profiling from
previous runs" (**vendor claim**); **THINKING** — Leyden JEP 544 AOT *code* compilation,
proposed not shipped; delivered Leyden JEPs 483/514/515/516 are ABLE).
**Idea.** Persist the always-on profiler's per-site speculation decisions
(monomorphic/polymorphic shapes, value ranges, representation stability) across process
runs in a version-keyed cache. On startup the speculative JIT arms day-0 compilations
with yesterday's speculations instead of re-warming; a guard+deopt fallback covers
behavior drift.
**Why gc doesn't do it.** gc's PGO consumes profiles *statically at build time*; it
cannot make speculative decisions at run time because it has no guard+deopt machinery.
A cross-run cache of *speculations* (not just profiles) is only actionable with deopt.
**Architectural fit.** E4 (profiler produces the data) + E5 (hybrid AOT+JIT consumes it;
pure-AOT can pre-bake the highest-confidence speculations) + E3 (deopt covers drift).
**Benchmark sketch.** Server-process restart / autoscale scenario (Go HTTP service with
`wrk`/`hey`): time-to-peak-throughput and requests served in the first 60 s vs stock
gc+PGO binary. gc+PGO starts at its static optimum immediately but can never speculate;
we start near-peak *and* keep speculating. Also `go build`-style short-lived CLI
workloads where JIT warmup dominates.
**Open risks.** Cache invalidation on code/config change (Leyden's own risk note: a
cache built on one training run may miss production edge cases); poisoning from
unrepresentative training runs; the guard/deopt path must be genuinely cheap or drift
kills the win. Design the format so a future Leyden-style code cache can sit underneath
— we cache *speculation decisions + profiles*, not code (composable, not competing).
**Tracks:** T2.

#### SURV-015 — Map read-modify-write fusion via borrowed interior pointer
**Bucket:** WOULD-LIKE (precedent: **THINKING** — Swift SE-0474 yielding accessors,
accepted, behind `-enable-experimental-feature CoroutineAccessors`).
**Idea.** `v := m[k]; …; m[k] = v+delta` with no intervening map mutation is two hash
lookups today. The JIT fuses them: one lookup yields a borrowed interior pointer held
across the region; the write goes in place. Sound when the JIT proves (or guards,
SURV-018-style on `(B,count)`) no intervening mutation — and within one goroutine Go
maps cannot be touched concurrently without a fatal error anyway.
**Precedent.** Swift SE-0474 `yielding mutate` / `yielding borrow` — "lend" access to a
value without get-copy-set.
**Transfer note.** No language change needed (unlike Swift's coroutine machinery) — the
JIT implements it as a fused intrinsic. Complementary to the backlog's map
specialization (representation vs access path) and to SURV-018 (slot caching).
**Why gc doesn't do it.** Map elements are not addressable; each access is an
independent opaque runtime call; no cross-access fusion.
**Architectural fit.** E1 + E3.
**Benchmark sketch.** Histogram loops (`freq[w]++`); accumulator maps (`totals[k] +=
delta`); counter patterns.
**Open risks.** The no-intervening-mutation proof must be airtight — calls in between
that could touch the map force a guard or a bail; the borrowed pointer must not escape
the region (interior pointer is GC-safe as long as the map SSA value keeps it alive).
**Tracks:** T3-C9.

#### SURV-016 — Speculative generic-instantiation specialization
**Bucket:** WOULD-LIKE (precedents: **ABLE** — Swift generic specialization + SE-0460
`@specialized`; **THINKING** — Valhalla "Parametric JVM" (JEP draft 8261529: "specializing
and optimizing generic class and method parameterizations at run time"); design
direction: Rust RFC 2000 const generics).
**Idea.** Go generics compile via GC-shape stenciling (one version per shape) + runtime
dictionaries. The JIT additionally creates per-concrete-type versions at hot call sites
for *observed* type args, guarded on the type argument, with the body specialized and
inlined — e.g. devirtualizing dictionary-passed method calls on type parameters.
Profile data = automatic `@specialized`.
**Precedent.** Swift: "if the generic function's definition is visible to the optimizer
and the concrete type is known, the compiler emits a version specialized to the specific
type"; rustc monomorphizes every instantiation (the AOT extreme).
**Transfer note.** The guard is on the concrete type argument — no witness-table
machinery transfers.
**Why gc doesn't do it.** Stenciling is per GC-shape, not per exact type; no guards
exist; cross-shape method-call devirtualization is impossible.
**Architectural fit.** E3 + E5.
**Benchmark sketch.** Generic numeric kernels over `constraints.Integer`/`Float` with
runtime-chosen element type; `slices.SortFunc` on concrete element types with inlined
comparators; generic container hot paths (e.g. a generic LRU with `comparable` K).
**Open risks.** Code bloat — cap versions per site, share with AOT-stenciled versions
when identical; type-polymorphic sites fall back to the stenciled version.
**Tracks:** T3-C8, T5 (merged — same idea; T5's framing is the rustc/monomorphization
contrast, T3's the Swift-observed-types framing).

#### SURV-042 — Interface equality fast path via reflexive-type bit
**Bucket:** THINKING (precedent: Go-team-endorsed direction, unbuilt).
**Idea.** Carry a reflexive bit in the type descriptor (type contains no NaN-like
values where pointer-identity does not imply equality — gc already computes this
for map keys). Interface `==`: if both types reflexive and data pointers equal →
equal, skipping the recursive type-driven comparison. Covers both the
exponential-blowup case under pointer sharing and the general fast path.
**Precedent.** golang/go#62488 — the mechanism proposed by randall77 (Go team)
himself; open, NeedsInvestigation, unimplemented.
**Why gc doesn't do it.** Unbuilt; the recursive comparison is the status quo.
**Architectural fit.** E1 — the bit lives in the descriptor; representation≠
semantics.
**Benchmark sketch.** `map[any]V` lookup/insert with shared keys;
`reflect.DeepEqual`-style traversals, testify-style assertions.
**Open risks.** The NaN caveat is exactly what the bit handles — the bit
computation must be exact.
**Tracks:** T4-C-C (wave 2).

#### SURV-043 — Interface round-trip conversion elision (`any→T→any`)
**Bucket:** WOULD-LIKE.
**Idea.** Dataflow-aware interface conversion: a value asserted out of an
interface and converted back to (a supertype of) the same interface with no
intervening mutation reuses the original (type, data) words — no `convT*` call,
no allocation. The method-ful case falls out of the same rule.
**Precedent.** golang/go#64541 (open): `s,ok:=v.(string); sink(s)` allocates in
`convTstring` (67.47 ns/op, 1 alloc — reporter's numbers) vs `sink(v)` at
5.3 ns/op, 0 alloc; golang/go#24582 (open, 2018): the compiler avoids the alloc
for `interface{}→string` but not when the target interface has methods.
**Why gc doesn't do it.** Conversion is a runtime call that always materializes;
the method-ful path was never optimized.
**Architectural fit.** E1 — interface word reuse is a representation decision.
**Benchmark sketch.** `any`-heavy code: serialization, `fmt` paths.
**Open risks.** Must prove no intervening mutation or replacement of the value;
method-set compatibility for the method-ful case.
**Tracks:** T4-C-M (wave 2).

#### SURV-044 — Callee-side concrete-type specialization of interface parameters
**Bucket:** WOULD-LIKE.
**Idea.** PGO-driven (or closed-world-AOT-proven) cloning of hot functions on the
observed concrete types of their interface parameters: the clone takes `T`
directly — no interface materialization at the boundary, no dispatch inside —
guarded on the type set (E3) or proven closed-world (E5).
**Precedent.** golang/go#19165 (open since 2017); the issue itself notes the
code-bloat guard needed (specialize only when the body is small / methods inline).
**Why gc doesn't do it.** Unbuilt; per-package compilation cannot see call-site
type sets.
**Architectural fit.** E3 (+E5 proof variant). Composes with the devirtualization
cascade (which works the call-site side) and SURV-012 (const-*value*
specialization); SURV-004 is func-*value* sites, SURV-011 *value* specialization,
SURV-016 generic type-args — this is concrete-type specialization of
*parameters*, uncovered. **Boundary with SURV-012 (wave-3 review): kept
separate, deliberately.** SURV-012 triggers on constant *values* and folds them;
this triggers on closed *type sets* and changes the boundary (clone takes T
unboxed — no interface materialization). Different trigger, mechanism, and guard
story; a clone can be both type- and const-specialized, i.e. they compose, not
merge.
**Benchmark sketch.** `sort.Sort` with `sort.Interface` (the issue's example);
any hot interface-parameter function.
**Open risks.** Code bloat — the issue's own guard (small bodies only).
**Tracks:** T4-C-F (wave 2).

#### SURV-045 — Speculative batched channel send in fill/drain loops
**Bucket:** WOULD-LIKE.
**Idea.** At hot `for … { ch <- e }` / `<-ch` loop sites, JIT-version the loop to
transfer elements in batches under a single hchan lock acquisition. Two tiers:
(1) the EA-proven unshared-channel case reduces to inlining+EA (already in the
aggressive-EA backlog — not new); (2) the novel tier is speculative batching
under an observed-exclusive-access guard (per-channel epoch/sender-count at
chunk entry, slow path on violation).
**Precedent.** golang/go#51553 (open, NeedsDecision — filed by the program owner;
bcmills' comment defines the tier split); reporter's ~9× gap (272 vs 30.1 ns/op)
is the ceiling. All numbers are the reporter's, not ours.
**Why gc doesn't do it.** Unbuilt; Go team skeptical of the idiom's frequency
(ianlancetaylor) — gate on E4 hotness data.
**Architectural fit.** E3 (per-site versioning + guard) + E4 (hot-loop detection).
Distinct from SURV-032 (select lowering — different op).
**Benchmark sketch.** Worker-pool work-distribution handoff loops.
**Open risks.** Batching changes multi-sender interleaving granularity — the
guard is semantic load-bearing, not an optimization hint; batch only while
exclusive access holds. The frequency question the Go team raised.
**Tracks:** T4-C-B (wave 2).

#### SURV-046 — Zeroing elision for provably fully-overwritten allocations
**Bucket:** WOULD-LIKE.
**Idea.** At hot allocation sites (E4-gated), prove every byte is written before
any read (loop-aware: init loops count) and allocate unzeroed instead of
`mallocgc`-with-zero. The proof is static per compiled version; fall back to
zeroing where unproven — deopt-free.
**Precedent.** golang/go#24926 (open, 2019; the issue sketches the SSA pass
shape); family #57153 (memclr for overwritten slices), #57000 (hybrid clearing
from prove results).
**Why gc doesn't do it.** Unbuilt; `needzero` is conservative.
**Architectural fit.** E4 (hot-site gating — the analysis is only worth it where
zeroing is a measured fraction) + E1 (zeroed vs unzeroed as a representation
choice).
**Benchmark sketch.** Init-heavy allocation: the issue's `[1024]int` init loop
(saves a full 8 KB memset per alloc).
**Open risks.** The all-bytes-written proof must be airtight — partial init is a
soundness hole; loop-carried proof complexity.
**Tracks:** T4-C-I (wave 2).

#### SURV-047 — Profile-guided format-call specialization
**Bucket:** WOULD-LIKE.
**Idea.** At hot `fmt.Sprintf`/`Errorf` sites where the format string is
profile-constant (very often source-constant), JIT-specialize: parse the format
once, emit a direct conversion chain (string concat / strconv.AppendInt / …)
with no reflect, no arg boxing, no format re-parsing. Guard on format-string
identity; deopt to the generic path otherwise.
**Precedent.** golang/go#81230 (closed, unimplemented — discussion drifted to
partial evaluation; randall77's closing suggestion was a `go fix`-style source
rewrite, i.e. gc won't do it in the compiler). This is the JIT analogue of what
the issue asked gc to do statically.
**Why gc doesn't do it.** Closed as compiler work; the team prefers source
rewrites.
**Architectural fit.** E3 (per-site guard + deopt) + E4 (hot-site gating).
**Benchmark sketch.** Logging/serialization-shaped benchmarks (fmt is famously
reflect+alloc heavy).
**Open risks.** Verbatim `%v`/width/precision semantics must be reproduced
exactly — the load-bearing correctness work.
**Tracks:** T4-C-O (wave 2).

#### SURV-048 — Append-chain fusion (incl. cross-inline-boundary)
**Bucket:** WOULD-LIKE.
**Idea.** Fuse `xs=append(xs,v1); xs=append(xs,v2)` → `xs=append(xs,v1,v2)`: one
growth check, one call sequence. In the JIT, fuse *after inlining* so chains
formed across function boundaries fuse too — the case gc's textual form can
never see.
**Precedent.** golang/go#25828 (open, 2018; reporter's benchmark 164 ns/op +
3 allocs → 61.7 ns/op + 1 alloc); #46079 (append-make→make+copy idiom), #26734.
**Why gc doesn't do it.** Unbuilt; textual form can't see cross-boundary chains.
**Architectural fit.** E3 + E4 (rank chains by hotness). Complements SURV-017
(fuse first, then pre-size once).
**Benchmark sketch.** Reporter's BenchmarkAppend shape + real sites in
net/http, cmd/vet.
**Open risks.** Growth semantics (capacity growth sequence) must be exactly
preserved.
**Tracks:** T4-C-A (wave 2).

#### SURV-049 — Assign-side map string-key allocation elision
**Bucket:** WOULD-LIKE.
**Idea.** Two layers: (1) static — at `mapassign` with a string key, probe with a
non-allocating key (the lookup path already avoids allocation via scratch
buffer); allocate the key string *only on insert*, reusing the resident key on
update. (2) speculative — per-site inline cache on "key already present" (ties
to SURV-018's slot-caching family, but for dynamic keys).
**Precedent.** golang/go#55930 (open; 29.83 ns/op, 1 alloc/iter — reporter's
numbers); #45045 made the naive fix harder (cited in the issue).
**Why gc doesn't do it.** Unbuilt.
**Architectural fit.** E1 (key representation) + E3 (speculative variant).
Distinct from SURV-018 (constant-key slot caching).
**Benchmark sketch.** The issue's loop benchmark; map-heavy update workloads.
**Open risks.** Key-identity semantics: must not alias caller memory that could
mutate — copy on insert, reuse only the resident key.
**Tracks:** T4-C-E (wave 2).

#### SURV-050 — Speculative inline-stack goroutine spawn
**Bucket:** THINKING (precedent: aclements-originated speculative idea, unbuilt).
**Idea.** For spawn sites where the profiler says "child is short-lived and the
parent blocks/waits" (the overwhelmingly common fan-out shape), skip stack
allocation entirely: the child runs on the parent's stack tail. Guard: if the
parent is rescheduled while the child still runs there, relocate the child using
the existing stack-copy machinery. Deopt-free at runtime — relocation is the
fallback path.
**Precedent.** golang/go#27345 (open): "speculative idea from discussion with
@aclements" — "tracking some basic statistics on the fly to decide whether to do
this" (the issue itself asks for the E4 mechanism).
**Why gc doesn't do it.** Unbuilt, speculative.
**Architectural fit.** E2 (we know exactly when the parent resumes — our spawn
path is ours to redesign) + E4 (spawn-site lifetime stats are the arming
signal). Composes with SURV-041 (right-size the borrowed region).
**Benchmark sketch.** ParallelizeUntil-shaped microbenchmarks;
request-per-goroutine servers — kills both the alloc and the first growth.
**Open risks.** Relocation path must be airtight; parent-resume detection cost.
**Tracks:** T4-C-K (wave 2).

#### SURV-051 — Concatenation-chain fusion (n-way string concat)
**Bucket:** THINKING (precedent: Go team discussed, no general form landed).
**Idea.** Fuse `s1+s2+…+sn` chains (post-inlining) into a single allocation of
the total length with unrolled part copies — the compile-time generalization of
arity specialization. Subsumes the N={2,3,4,5} runtime-specialization debate: do
it in the JIT with exact N known.
**Precedent.** golang/go#27801 (open; CL 123256 discussion; arity N=2 measured
−27.95% — reporter's numbers).
**Why gc doesn't do it.** Only partial arity specialization discussed.
**Architectural fit.** E3 (JIT arity knowledge). Backlog has SSO + interning, not
concat fusion.
**Benchmark sketch.** Concat2 + `a+b+c+d` chains (logging, SQL building).
**Open risks.** None significant — pure reassociation with exact length math.
**Tracks:** T4-C-H (wave 2).

#### SURV-052 — Miss-against-own-cache → downgrade, don't enlarge (speculation policy)
**Bucket:** WOULD-LIKE (precedent: **ABLE** — V8).
**Idea.** *Policy* governing speculative sites (SURV-004, interface dispatch):
when a guard fails at a site whose cache already contains the failing case, the
correct response is to collapse the speculation (megamorphic/generic) — not to
add another ladder rung. A miss contradicting the cache's own contents is
evidence the cache is wrong-headed, not evidence to enlarge it.
**Precedent.** V8 `src/ic/ic.cc:755-766` ("not progressing in the lattice" →
MEGAMORPHIC); `src/ic/ic.cc:779-790` deopt-loop breakers (dict-rehash miss →
megamorphic "to prevent deopt loops"; prototype-chain failure → overwrite the
handler in place).
**Why gc doesn't do it.** gc has no such sites (no speculation at all).
**Architectural fit.** E3. Prevents pathological ladder growth at flapping sites.
**Benchmark sketch.** Same as the governed sites (sort.Slice comparator,
config-map lookups); measured as "no ladder-growth pathology at flapping
sites".
**Open risks.** None — the fallback is the sound baseline.
**Tracks:** T3 wave-2 S1 (policy).

#### SURV-053 — No-evidence → generic, named reason (speculation policy)
**Bucket:** WOULD-LIKE (precedent: **ABLE** — V8).
**Idea.** *Policy* for all speculative candidates: only speculate where a profile
fact exists; elsewhere emit the unspecialized op with *no deopt point at all* —
and record *which evidence was missing* at the site (V8's
`kInsufficientTypeFeedbackFor*` naming). Makes "profile coverage" a first-class,
auditable quantity; speculation is never inserted blindly.
**Precedent.** V8 `src/compiler/js-type-hint-lowering.cc` (`DeoptimizeReason::
kInsufficientTypeFeedbackFor{Unary,Binary,Compare}Operation`, `...ForCall`,
`...ForConstruct`, `...ForGenericNamedAccess/KeyedAccess`).
**Why gc doesn't do it.** No speculation at all.
**Architectural fit.** E3+E4. Guards every speculative candidate against
over-speculation regressions.
**Benchmark sketch.** "No slowdown on unprofiled code" across the candidate
suite.
**Open risks.** None.
**Tracks:** T3 wave-2 S1 (policy).

#### SURV-054 — Kinded OSR exits with monotone kill (speculation policy)
**Bucket:** WOULD-LIKE (precedent: **ABLE** — JSC).
**Idea.** *Policy*: tag every OSR exit/deopt with a kind (value-assumption,
branch, representation, hoisting, inadequate-coverage, uncountable-noise); feed
per-(site,kind) counts to the recompiler; a `HoistingFailed`-style kind
*permanently* disables that speculation at the site; `Uncountable` kinds never
feed the counters.
**Precedent.** JSC `bytecode/ExitKind.h` (`HoistingFailed` — "assume hoisting is
a bad idea"; `Uncountable` — "we should not count it"); `dfg/DFGOSRExit.cpp`
failure-address profiling (the exit records the offending value's structure ID —
the recompiler learns *what* broke, not just that something broke).
**Why gc doesn't do it.** No OSR.
**Architectural fit.** E3+E4. Prevents exit storms and hoisting oscillation
(governs SURV-013 loop unswitching, SURV-037 rejit).
**Benchmark sketch.** Phase-change scenarios; measured as bounded deopt rates.
**Open risks.** Counter storage cost — bounded via lossy tables (cf. PyPy S4.1).
**Tracks:** T3 wave-2 S2 (policy).

#### SURV-055 — Guard-failure budgets with decay, one-shot retrace (speculation policy)
**Bucket:** WOULD-LIKE (precedent: **ABLE** — PyPy).
**Idea.** *Policy*: per-site speculation gets a failure budget in a bounded lossy
table; bridge/recompile at N failures; all counters *decay* on GC/time (phase
adaptation); at most one re-speculation per site per epoch — further oscillation
falls back to the unspecialized path permanently (PyPy's preamble jump).
**Precedent.** PyPy `rpython/jit/metainterp/counter.py` (lossy 5-way fractional
timetable; bridge at 200 failures), `retrace_limit=0`, `decay=40` per 32 minor
GCs, blackhole interpreter for hostile loops.
**Why gc doesn't do it.** No rejit.
**Architectural fit.** E3+E4. This is what prevents rejit storms (governs
SURV-037 phase-adaptive recompilation).
**Benchmark sketch.** API gateway with shifting endpoint mix (phase change).
**Open risks.** Tuning N and decay for our guard costs — prototype-gated numbers.
**Tracks:** T3 wave-2 S4 (policy).

#### SURV-056 — Speculative dead-object slot reuse (allocator-bypass)
**Bucket:** THINKING (precedent: gc's `runtimefreegc` is merged-behind-flag —
active experiment, static form only).
**Idea.** N per-iteration allocations → one long-lived slot, skipping
malloc/free entirely: guard on profiler-observed deadness (the slot is dead at
the reuse point); deopt/materialize on violation. **The static sub-case is
conceded as raced:** gc's CL 710015 (alias analysis + `growsliceNoAlias`
auto-free of append-growth intermediates) covers it — do not claim it.
**Precedent.** golang/go#74299 (tracking issue), design
go.dev/design/74299-runtime-freegc, CL 710015, CL 673695
(`runtime.freegc`/`freegcTracked`); soundness bugs #80697 (open — alias
analysis prematurely commits NoAlias on multi-assign append) and #79909 (fixed
via CL 789060) show static casework getting harder as coverage grows — the
argument for speculation over more static cases. GoFree (CGO 2025) independently
validated the family (their claim: −13% GC time on 6 programs — source claim,
not ours).
**Why gc doesn't do OUR form.** AOT-only and conservative-by-construction
("unknown IR ⇒ alias"); free-early still pays malloc/free per iteration
(allocator round-trip retained); the experiment is disabled by default in Go
1.26 (CL 720120); the per-iteration same-site `make` case is explicitly "the
current plan, compiler not done" in their issue.
**Architectural fit.** E3 (guard/deopt where static analysis bails — something
gc structurally cannot do) + E1 (descriptor-driven cross-type slot reuse).
**Benchmark sketch.** Per-iteration temporaries on the non-subsumed shapes
(same-site `make` in loops); the issue's own growslice patterns only where
CL 710015 provably doesn't apply.
**Open risks.** Liveness proof is the hard part — the guard carries it;
profile-dependent retention branches need the profile (our backlog item already
names this: "speculation covers profile-dependent retention branches").
**Tracks:** T4 wave-2 deadobj investigation. Re-scopes the backlog item to lead
with the speculative form.

#### SURV-060 — Guarded read-only borrow elision (copy → borrow under write-barrier guard)
**Bucket:** WOULD-LIKE (precedents: **WOULD-LIKE** — golang/go#19817 open since
2017, unbuilt; golang/go#75620 proposal, unbuilt; #53643 `Clone` same family).
**Idea.** `a := *p` (or `slices.Clone(s)` of a provably-used-verbatim slice)
where the destination is only ever loaded becomes a *guarded borrow* of the
source: at elision the JIT records (object, borrow-epoch); the write barrier
bumps the epoch on any write to the object; loads of the borrowed destination
(or a safepoint) check the epoch and deopt if the object was mutated.
**Why the naive static form is unsound** (struck to §5): #19817's
"copy isn't atomic either" argument covers only the racy case. Race-free
counterexample: a write HB-ordered after the copy point but before the last
load — all accesses race-free, yet the borrow observes the post-write value
the copy could never see. The write-free window must hold across *all*
happens-before paths (incl. other goroutines) — an undecidable static proof,
which is the real reason gc never built it. The guard converts that proof into
cheap speculation.
**Precedent.** #19817 (`[1000][1000]int` sum — million-int copy replaced by
direct access); #75620's honest framing (input/output never mutated, capacity
unobservable, element addresses unobserved — the actual proof obligations).
**Why gc doesn't do it.** AOT-only; the write-free proof is cross-goroutine
and undecidable in general; gc has no write-barrier-driven deopt machinery for
this.
**Architectural fit.** E1 (representation≠semantics: a "value" may be a guarded
borrow) + E3 (write-barrier epoch guard + deopt). Read analogue of SURV-056
(dead-object *slot reuse* — the write analogue; this is the read analogue).
Distinct from SURV-007 (field projection), SURV-024 (dead-source move elision),
and SURV-031 (the statically-sound special case: Go strings are immutable, so
`[]byte(s)` borrows need no guard).
**Benchmark sketch.** #19817's large-struct copy loops; reflect.Select-shaped
`slices.Clone`-of-literal sites; defensive-copy idioms in hot paths.
**Open risks.** Guard placement: epoch check per load is the cost to measure;
safepoint-only checks trade detection latency for a wider vulnerable window
(deopt still sound — any observed mutation invalidates). Borrow-epoch write
barrier overhead on hot mutators.
**Tracks:** T4-C-G (wave 2 raw → wave-3 soundness re-verification).

#### SURV-061 — Closed-world value-field object inlining (pure-AOT)
**Bucket:** THINKING (precedent: **THINKING** — Bruno et al., PLDI'21, research
prototype on GraalVM Native Image, not a shipped product feature).
**Idea.** Fields declared (or inferred) as having value semantics get their
payload inlined at the *type* level in pure-AOT mode under the closed-world
assertion: `p.child *Node` → the `Node` payload lives inside the parent
allocation — one allocation instead of two, one fewer indirection, no header
for the child. No guards, no deopt: the whole-program view proves no identity
exposure of the child and no polymorphic child types. Runs early in the
pipeline so method inlining + escape analysis compound on the flattened
accesses.
**Precedent.** Bruno, Jovanovic, Wimmer & Alonso, "Compiler-Assisted Object
Inlining with Value Fields", *PLDI* 2021 — verified forward citation of Dolby
1997 / Dolby & Chien 2000. Paper claims (their measurements, not ours): up to
3× throughput (OrientDB graph DB), up to 40% footprint reduction (Apache
Spark), up to 35% GC-pause reduction (microservices); headers+references up to
54% of memory on a 6.3M-entry dataset.
**Why gc doesn't do it.** gc has no object inlining at all; escape analysis
only scalar-replaces stack-bound objects.
**Architectural fit.** E5 (pure-AOT mode supplies the closed-world assertion;
`plugin`-free builds are statically detectable) + E1 (inline-vs-boxed as a
per-field representation decision). The *static closed-world twin* of
SURV-025 (which is speculative *per-allocation-site* inlining with guard/deopt
— this costs nothing at runtime). **Go caveat:** Go has no per-object headers,
so the Java footprint numbers don't transfer 1:1 — the win here is
pointer→value flattening on pointer-heavy graphs (ASTs, linked structures,
decode trees), not header elimination.
**Benchmark sketch.** Pointer-heavy domain graphs (JSON/protobuf decode trees,
AST walks): flattened vs gc+PGO; metrics = allocation count + indirection
traffic.
**Open risks.** Value-semantics declaration/inference: developer annotation vs
whole-program inference of "no identity exposure"; interface-typed child fields
defeat the proof (guard → falls back to SURV-025's speculative form).
**Tracks:** T1 wave 3 (forward-citation sweep).

#### SURV-062 — Dominance-based allocation folding
**Bucket:** THINKING (precedent: **ABLE** — Clifford et al., ISMM'14, shipped
in V8; current-V8 status not re-verified).
**Idea.** When the JIT proves allocation B is dominated by allocation A in the
same compiled region, fold both into a single *allocation group*: one
bump-pointer bounds check instead of two, guaranteed contiguity, and —
statically removable write barriers for reference stores *within* the group
(same generation, known layout). Unlike object inlining, it needs no
ownership/connectivity relation between the objects.
**Precedent.** Clifford, Payer, Starzinger & Titzer, "Allocation Folding Based
on Dominance", *ISMM* 2014 — lineage-adjacent to Dolby's object inlining (the
paper positions itself against it). Paper claims (their measurements): strong
gains on Octane/Kraken plus four hand-selected benchmarks, even with heap
fragmentation.
**Why gc doesn't do it.** gc folds nothing and eliminates no write barriers
this way.
**Architectural fit.** E1 (the allocation group is a per-region representation
decision; write-barrier elimination is a property of the group descriptor).
Distinct from SURV-025 (inlining needs parent-child uniqueness; folding needs
none), SURV-048 (append-chain fusion), SURV-058 (loop slab batching of
same-type allocs — folding is heterogeneous dominated allocs in straight-line
code).
**Benchmark sketch.** Alloc-heavy straight-line builders (`encoding/json`
decode, protobuf unmarshal): bounds-check + write-barrier traffic vs gc+PGO.
**Open risks.** Dominance must survive inlining (recompute post-inline);
group size caps to avoid pathological region growth.
**Tracks:** T1 wave 3 (forward-citation sweep).

#### SURV-063 — Speculative compiler-inserted explicit freeing (tcfree + guard/deopt)
**Bucket:** THINKING (precedent: **THINKING** — GoFree, CGO'25, open-sourced
research, not shipped in gc).
**Idea.** GoFree's static result first: an O(N²) escape analysis finds heap
objects with provably bounded lifetimes; the compiler inserts `tcfree` calls
at end-of-scope — a best-effort free primitive (thread-local bump-pointer
revert for small objects; 2-step async for large; fails safe to GC whenever
freeing would be costly or racy). graalgo goes one step further: where static
EA *bails* (imprecise points-to, interface smuggling), the always-on profiler
(E4) identifies last-use sites dynamically and the JIT inserts *guarded* frees
— a cheap liveness/escape check deopts to GC-managed on violation. Static
where provable (GoFree's result), speculative where merely predictable.
**Precedent.** Peng et al., "GoFree: Reducing Garbage Collection via
Compiler-Inserted Freeing", *CGO* 2025. Paper claims (their measurements on 6
open-source Go programs, not ours): 14% of allocated heap deallocated at run
time, −7% GC frequency, −13% GC time, −2% wall-clock, −4% heap size, no
observable compile-time slowdown, <2000 lines of compiler+runtime changes.
**Why gc doesn't do it.** gc has no explicit deallocation path at all; the
`runtimefreegc` experiment frees *early* but still through the GC.
**Architectural fit.** E3 (guard + deopt is the soundness story for the
speculative frees) + E4 (profiler finds last-use/free sites static EA can't
prove). **Boundary:** SURV-056 (dead-object *slot reuse*) bypasses the
allocator; this *returns* memory to it — complementary, not overlapping.
Distinct from SURV-027 (bulk-free *regions* — whole-pool reclamation vs
per-object free insertion) and SURV-003 (scoped arenas).
**Benchmark sketch.** The precedent's own Go programs; plus EA-imprecise
(interface-heavy) variants where the speculative form extends coverage:
GC-time + wall-clock vs gc+PGO.
**Open risks.** Guard cost at every speculative free site; tcfree's fail-safe
path must never regress the normal alloc path (GoFree's own zero-regression
bar); finalizers/specials excluded (same as runtimefreegc's obstacle list).
**Tracks:** T1 wave 3 (forward-citation sweep).

#### SURV-065 — Devirt-triggered JIT re-pipelining
**Bucket:** THINKING (precedent: **ABLE** — LLVM's `devirt<4>` repeater wraps
the entire CGSCC inline+simplify pipeline on every O2/O3 compile).
**Idea.** When a JIT compile (or recompile) converts an interface/indirect
call to a direct call — via guard, profile, or closed-world proof — re-run
the mid-level pipeline (inline the newly-direct callee → const/copy-prop →
simplify) on the affected callers, bounded (≤3 re-pipelines per compile,
mirroring LLVM's ≤4). LLVM proves the pattern pays in an *AOT* compiler with
static devirt only; in a JIT with guards it is strictly more powerful because
the devirtualization *event* is observable at compile time.
**Precedent.** LLVM `PassBuilderPipelines.cpp`: "We wrap the CGSCC pipeline in
a devirtualization repeater… to try and catch knock-on inlining or function
attrs opportunities" — change-triggered re-runs, bounded, not fixpoint.
**Why gc doesn't do it.** AOT single pass; gc structurally cannot re-pipeline
on a devirt event.
**Architectural fit.** E3 (the re-pipeline is what converts a devirtualization
*event* into its full payoff — it multiplies the backlog's devirt cascade).
Distinct from the cascade itself: the cascade produces the event; this is the
*scheduling policy* that harvests it.
**Benchmark sketch.** Interface-heavy dispatch (JSON/codec dispatch,
sort.Interface, http handler chains): with vs without re-pipelining, vs
gc+PGO.
**Open risks.** Re-pipeline cost vs win needs a budget (inline-budget
discipline); megamorphic sites must not trigger it (type-count cap, cf. T3
wave-2 speculation policies P1–P5).
**Tracks:** T5 wave 3 (pipeline-order walk).

#### SURV-066 — Side-exit chain depth cap with progress proof
**Bucket:** WOULD-LIKE (precedent: **ABLE** — CPython tier-2 optimizer).
**Idea.** Cap speculative side-exit chains at 4 links (CPython's
`MAX_CHAIN_DEPTH`); refuse to extend a chain unless the first and Nth links
provably make forward progress — a *progress proof*, not a counter. A chain
that can't show progress at depth 4 is refused; the site falls back to the
baseline version. This is the deopt-loop breaker for the speculation-policy
family: V8's downgrade and PyPy's retrace cap are counters; CPython's is a
proof.
**Precedent.** CPython `Python/optimizer.c:127-133`,
`pycore_optimizer.h:165` — "We can only insert the executor into the bytecode
if this is true, since a deopt won't infinitely re-enter the executor."
**Why gc doesn't do it.** gc has no side exits, no speculative chains.
**Architectural fit.** E3 (bounds worst-case chain explosion on the
loop-unswitching and rejit chains). Distinct from SURV-054 (which *kinds*
exits) and SURV-055 (which *budgets* failures) — this caps chain *depth*
with a progress requirement.
**Benchmark sketch.** Speculative loop-unswitching sites (SURV-013) and
rejit chains (SURV-037) under phase change: bounded worst-case deopt storms
vs unbounded.
**Open risks.** Progress-proof formulation for our guard kinds; the cap (4)
is CPython's number — ours needs calibration, not copying.
**Tracks:** T3 wave 3 (Cinder/CPython source pass).

#### SURV-067 — Bounded-tolerance guards for mutable global state
**Bucket:** WOULD-LIKE (precedent: **ABLE** — CPython tier-2 optimizer).
**Idea.** Guard hoisted loads of mutable-but-rarely-changed state with a
mutation counter; tolerate up to N mutations (CPython: 6 globals / 3
builtins, via dict mutation watchers), and invalidate the specialization only
when the cap is hit. Don't deopt on the *first* change. The Go analog is
package-level variables and other process-lifetime-but-mutable state read in
hot loops (config structs, feature flags as *data*).
**Precedent.** CPython `pycore_optimizer.h:101-102`
(`_Py_MAX_ALLOWED_BUILTINS_MODIFICATIONS 3`,
`_Py_MAX_ALLOWED_GLOBALS_MODIFICATIONS 6`), `optimizer_analysis.c`
mutation-gated folding, watcher invalidation on cap breach.
**Why gc doesn't do it.** gc must reload package-level state every time; it
has no guard machinery to hoist it.
**Architectural fit.** E3 (guard + invalidation) + E4 (the mutation profile
is the signal that sets the tolerance).
**Benchmark sketch.** Request-handler hot loop reading a package-level config
/ feature flag: hoisted-with-tolerance vs reload-every-iteration, vs gc+PGO.
**Open risks.** Mutation-counter overhead on the watched state; cap choice
needs calibration per program (E4 data, not a constant).
**Tracks:** T3 wave 3 (Cinder/CPython source pass).

#### SURV-068 — Fact-annotated runtime: declared-immutable ranges the JIT trusts guard-free
**Bucket:** WOULD-LIKE (precedent: **ATTEMPTED** — Pyston's nitrous
`JIT_IS_CONST`/`addJitConst` knowledge lattice never shipped; the *mechanism*
is small and its obstacle — LLVM JIT cost/complexity — doesn't transfer to a
fact registry).
**Idea.** A runtime-maintained registry of `(address, size, property)` facts
(const / nonzero / known-value); the JIT's knowledge lattice const-folds
loads through declared ranges with **no guard at all**. Go instances:
itab/method-table pointers in interface dispatch, map type descriptors,
immortal string payloads — process-lifetime invariants the runtime already
knows. Violation ⇒ fact withdrawn ⇒ recompile (version-tagged facts).
**Precedent.** Pyston nitrous `optimization_hooks.h` / `facts.cpp`:
`addJitConst(addr, size, flags)` with a `Knowledge` lattice keyed by
`Location`, scoped by dominating `Domain`; immortal tuple payloads marked
`MARKCONST` const-folded guard-free.
**Why gc doesn't do it.** gc re-loads type descriptors and itabs every time;
it has no declared-facts channel from the runtime to the compiler.
**Architectural fit.** E1 (the registry is a descriptor-model fact source) +
E3. Composes with the devirt cascade: itab loads folded guard-free make the
monomorphic fast path cheaper to reach.
**Benchmark sketch.** Interface-heavy dispatch (`net/http` handler chains):
itab-load folded vs reloaded, vs gc+PGO; map access through `hmap` type
pointers.
**Open risks.** Fact granularity (per-range vs per-type); withdrawal protocol
must be airtight — a stale const fact is a miscompile, not a missed
optimization.
**Tracks:** T3 wave 3 (Pyston source pass).

#### SURV-069 — Convergent retranslation: widen the guard, don't retry the speculation
**Bucket:** WOULD-LIKE (precedent: **ABLE** — HHVM production).
**Idea.** On repeated guard failure at a site, retranslate with the guard
*relaxed to the profile-weighted observed type set* (monotone widening; dead
blocks deleted/merged) instead of deopt-and-retry or exponential backoff.
Retranslation converges because relaxation is monotone — the third policy in
the P3/P5 family (kinded exits / failure budgets): generalize rather than
back off.
**Precedent.** HHVM `region-guards.cpp` `relaxIsProfitable`: weight-profile
the types at each guard; relax to the wider `DataTypeCategory` iff the
widened type strictly covers more profile weight (`PGORelaxPercent=100`
default); retranslation chains carry `relaxed/deleted/merged` block states.
**Why gc doesn't do it.** gc cannot retranslate at all.
**Architectural fit.** E3 + E4 (the profile-weighted type set is the widening
target). **Open design choice now explicit:** HHVM widens; JSC/PyPy/
RaptorJIT back off — our policy must pick per site kind, not globally.
**Benchmark sketch.** Func-value sites (SURV-004) and generic-instantiation
sites (SURV-016) under type churn (API-gateway phase change): widening
converges where backoff just delays.
**Open risks.** Widening too fast loses the specialization win — the
`relaxIsProfitable` weight test needs our own calibration; interaction with
the deopt-budget discipline (§4).
**Tracks:** T3 wave 3 (HHVM source pass).

#### SURV-070 — Skewed-dispatch speculation at polymorphic sites
**Bucket:** WOULD-LIKE (precedents: **ABLE** — ZJIT skewed variants in CRuby
4.0; **ABLE** — .NET's 75% GDV chain gate).
**Idea.** At call sites whose profile is polymorphic-but-skewed (dominant
class ≥75% of observations), emit guard-dominant-class + inlined direct call
with the generic dispatch as fallback — *not* a ladder, not megamorphic-
give-up. This extends SURV-004 (monomorphic) and the backlog's devirt cascade
into the realistic middle: most hot polymorphic sites have a winner.
**Precedent.** ZJIT `hir.rs` `ReceiverTypeResolution::SkewedPolymorphic` /
`SkewedMegamorphic` (the latter detected but not yet optimized — the
frontier is visible); `SKEW_THRESHOLD = 0.75` (`distribution.rs:77`). The
threshold **converges independently** with .NET's GDV chain gate (75%) and
JSC's 2:1 double-vote: three runtimes, same dominance number.
**Why gc doesn't do it.** gc+PGO can't guard; its devirtualization is static
proof or frozen profile.
**Architectural fit.** E3 + E4 (the skew profile is the arming signal).
Distinct from SURV-004 (stops at monomorphic) and .NET's ladder (covers ≤5
types without a dominance rule) — nothing proposes dominant-class speculation
with a generic fallback at poly sites.
**Benchmark sketch.** `io.Writer` sites dominated by one impl (log-to-file
hot path), `fmt.Fprintf` to a dominant writer: guard-dominant vs ladder vs
generic dispatch, vs gc+PGO.
**Open risks.** Skew drift (the 75% winner changes) — the guard carries it,
but re-specialization cadence needs the failure-budget policy (SURV-055);
`SkewedMegamorphic` is unoptimized even in ZJIT — cap the ambition at skewed-
poly first.
**Tracks:** T3 wave 3 (ZJIT source pass).

#### SURV-071 — Borrow-scope copy elimination
**Bucket:** WOULD-LIKE (precedent: **ABLE** — Swift's SemanticARC,
`CopyValueOpts.cpp`, shipped in swiftc).
**Idea.** With borrow/ownership facts, eliminate a value copy when every use
is a non-consuming borrow within the borrow scope; bail on any unknown
consuming use; scope the first version to the provably-safe subset (Swift:
guaranteed parameters only — "we only perform this for guaranteed parameters
since this enables us to avoid doing the linear lifetime check"). Go targets:
by-value struct copies in hot calls, interface boxing of values that are only
borrowed, slice-header copies.
**Precedent.** Swift `CopyValueOpts::performGuaranteedCopyValueOptimization`:
find all borrow introducers, build the `OwnershipLiveRange`, drop the
`copy_value` if no unknown consuming use exists.
**Why gc doesn't do it.** gc copies unconditionally; it has no borrow-scope
analysis.
**Architectural fit.** E1 (borrow facts as descriptor properties) + E3 (the
speculative extension: guard the borrow scope, deopt on a consuming use the
analysis couldn't see). Distinct from SURV-007 (field projection), SURV-024
(dead-source move), SURV-060 (guarded *read* borrow of a copy — this
eliminates the copy itself under borrow-scope proof).
**Benchmark sketch.** `encoding/json` marshal of large value structs; hot
functions taking big structs by value: copy-eliminated vs gc+PGO.
**Open risks.** Borrow-scope inference precision in Go (no language-level
borrows — inferred from use patterns); the speculative extension needs the
guard story designed, not assumed.
**Tracks:** T3 wave 3 (Swift compiler source pass).

#### SURV-072 — Idiom-aware region-formation gates for nested loops
**Bucket:** WOULD-LIKE (precedent: **ABLE** — RaptorJIT/LuaJIT production).
**Idea.** When forming speculative regions, compile nested loops as separate
*linked* regions by default; unroll/speculatively-inline the inner loop only
if it is provably short (RaptorJIT: ≤24 IR insns) *and* low-trip-count;
exclude idiom-rare patterns from speculation budgets (there: tail-call loops;
Go analogs: `defer` in hot loops, `recover` paths), with a global runaway
cap as the backstop. Budgets are spent where the language idiom puts the
heat.
**Precedent.** RaptorJIT `rec_loop_interp` (`lj_record.c:582-600`):
abort-and-wait for the inner loop's own trace; unroll into the root trace
only if it repeatedly failed to loop back (penalty ≥ 2×`PENALTY_MIN`) *and*
the prefix is short. Plus the per-pc doubling penalty cache
(`PENALTY_MIN 72` → `PENALTY_MAX 6000000`, 64 slots) and 1s wall-clock
hotcount decay — concrete numbers for SURV-055's failure-budget design.
**Why gc doesn't do it.** gc unrolls by static heuristics only; no
speculative regions exist to form.
**Architectural fit.** E3. Distinct from SURV-033 (which *versions* loops —
this specifies the *formation* policy for nested loops) and SURV-010 (the
transform suite).
**Benchmark sketch.** Nested-loop kernels (matmul, image convolution):
linked-regions vs speculative-unroll-everything — bounds code-cache blowup,
vs gc+PGO.
**Open risks.** The 24-insn / penalty numbers are LuaJIT's — ours need
calibration; idiom-rarity is language-specific and needs E4 data, not
folklore.
**Tracks:** T3 wave 3 (RaptorJIT source pass).

#### SURV-074 — Speculative contention-free lock fast path
**Bucket:** WOULD-LIKE (precedent: **ABLE** — GraalPy `GilNode` ships it:
profile says single-threaded → no lock/unlock boundary call emitted; deopt
if a second thread starts. Related **ATTEMPTED** — HotSpot biased locking,
removed JDK 18/JEP 374: its obstacle was the global revocation protocol at
safepoints; this routes around it with per-site guard+deopt, no global
protocol).
**Idea.** At a `sync.Mutex` (or channel-op) site whose contention profile
says "never contended / effectively single-goroutine" (the overwhelmingly
common shape: per-connection state guarded by a mutex touched by one
goroutine), emit a fast path that skips the atomic CAS and records
ownership via a cheap epoch/goroutine check; arrival of a second contender
invalidates — deopt to the real lock path.
**Why gc doesn't do it.** gc always emits the atomic CAS; it has no
contention profile and no speculative machinery.
**Architectural fit.** E2 (the scheduler knows goroutine identity and can
cheaply signal "second contender arrived") + E3 (guard+deopt) + E4
(contention profile is the arming signal). Distinct from SURV-023 (static
coarsening of adjacent same-mutex regions — nothing speculative).
**Benchmark sketch.** Mutex-protected per-connection/per-request hot paths
in `net/http`-shaped servers: contention-free fast path vs gc+PGO.
**Open risks.** The epoch check must stay well under the CAS it replaces;
false-invalidation storms on genuinely-shared mutexes need the contention
profile to be conservative; needs a contention-counting profiler (E4 gap).
**Tracks:** T3 wave 3 (GraalPy/TruffleRuby source pass, W8b).

#### SURV-075 — GraalNN-style ML-inferred static branch profiles as the E5 pure-AOT profile source
**Bucket:** WOULD-LIKE (precedent: **ABLE** — shipped in production GraalVM:
GNN control-split profile inference default at `-O3` in GraalVM for JDK 25,
`-O3` in JDK 24; a separate XGBoost call-count profiler classifies methods
cold/regular/hot in JDK 25 — Oracle docs, all fetched 2026-10-04).
**Idea.** Adopt ML-inferred control-split branch probabilities as the day-0
static profile source for E5 pure-AOT Go images. GraalNN's two-stage GNN
(context-insensitive at parse, context-sensitive per-inlining-site during
inlining) is trained once and ships with the compiler — statically
generalizable, no per-program training run. It predicts branch probabilities
only (call counts → the XGBoost model; trip counts unverified either way).
Paper claims (Milikic/Cugurovic/Jovanovic, CGO'25, DOI
10.1145/3696443.3708958): 10.13% runtime speedup on industry-standard
benchmarks, +2.5% over existing static profiling, +2.94% binary size (their
numbers; benchmarks unnamed in accessible sources).
**Why gc doesn't do it.** gc has no static ML profiler — only hand-written
branch heuristics; its PGO needs a training run per deployment.
**Architectural fit.** This is the missing profile source for E5: the E4
always-on profiler only exists at runtime; a pure-AOT image has no dynamic
profiles. ML-inferred profiles feed SURV-005 (layout), SURV-006 (inlining
simulation), SURV-010/SURV-013 (never-taken branch picking), and are the
static-profile floor under SURV-014's day-0 cache. Real `--pgo` profiles still
dominate when present (Graal auto-disables ML inference then).
**Benchmark sketch.** Go-AOT images on Go-shaped benchmarks: ML-inferred vs
no-profile vs gc+PGO; the load-bearing measurement is the Go-idiom
generalization gap (error-branch-heavy code vs the Java-trained prior).
**Open risks.** (1) Generalization: model trained on Java workloads; Go idioms
(per-call `if err != nil`, defer/panic, select dispatch) shift the
branch-prior distribution — unverified, flagged not hand-waved. (2) Paywall:
mechanism detail beyond abstract+docs unverified (ACM DL 403). (3) GraalNN
"not available in GraalVM Community Edition" per JDK 24 docs — non-blocking
only while graalgo pins Oracle GraalVM LTS.
**Wave-5 update — measurement protocol designed** (`track-surv075-wave5.md`):
the load-bearing Go-idiom generalization question is now a concrete three-stage
protocol reachable *before* any graalgo prototype exists. Stage 0 (gc-only):
corpus of 9 named benchmarks + 1 stretch (etcd, cockroachdb, gVisor, esbuild,
tile38 via `golang.org/x/benchmarks` Sweet harnesses + root macro-benches +
BenchmarkGames Go entries as the error-light contrast set); 8-shape branch
taxonomy (S1 `if err != nil` flagged highest-risk — Java has exceptions, not
error returns); ground truth from Go coverage counters; arm B = gc's
`likelyadjust.go` heuristics (verified purely structural in local Go 1.27.1
source, with a fitted 3-value probability mapping for fair calibration); arm C
(gc+PGO) as end-to-end ceiling only. Stage 1: Java-trained surrogate model
applied to Go CFGs — the actual generalization test. Stage 2: direct
Oracle-GraalNN-on-Go confirmation once E5 exists. Metrics: M1 per-shape
calibration (Brier/ECE); M2 consumer-impact simulators needing no compiler
(layout fall-through fraction, inlining-budget misallocation, wrong-leg
unswitch rate); M3 end-to-end. Kill triggers armed: K1 (surrogate Brier worse
than fitted heuristics on ≥2 of 5 Go-idiom shapes → stays WOULD-LIKE); K2
(≥1% M2-composite regression or >2% layout loss on error-heavy members →
"day-0 default" dead, drops to THINKING); K3 (S1 MAE >5pp at >99% not-taken
ground truth → scope change: train our own Go GNN); K4 anti-rigging control
(failure on the BenchmarkGames contrast set = methodology suspect, not a
SURV-075 kill). Promotion bar: ≥1.5% M2 geomean win, no shape worse by >1pp,
no member >2% end-to-end regression; beating gc+PGO explicitly never required.
**XGBoost/GraalCC verdict (wave 5): NOT extractable** — the JDK 25 call-count
profiler (`-H:+MLCallCountProfileInference`, "GraalCC") classifies methods
cold/regular/hot for sizing; it is docs-only (Oracle), with zero trace in the
public oracle/graal repo at HEAD (verified by tree-listing; grep.app was
rate-limited — the claim rests on tree names + spot-checked option-declaration
files, and a documented `-H:` option must be declared by name). Usable-as-flag
in E5 builds (graalgo pins Oracle LTS) ≠ extractable. Additionally granularity-
killed vs SURV-006 regardless: per-method binary cold/hot is too coarse for
per-call-site inlining-benefit simulation — GraalCC is at most a size-flag
on/off experiment, never a SURV-006 profile source.
**Tracks:** wave 4, CGO'25 pair (T2/T5 lane) → `track-cgo25-wave4.md`;
wave 5, protocol design + GraalCC verdict → `track-surv075-wave5.md`.

#### SURV-077 — [STRUCK, wave 5] Speculative declared-type-exact array-store check removal for Go slices
**Status:** STRUCK (wave 5) — the direct analog is vacuous: Go has no array
covariance, so the declared element type is always exact and no store check
exists on the store path (proven by disassembly: `s[i] = v` into `[]any`
lowers to `convTstring` boxing + bounds-check branch + type-word store +
write-barrier branch — no type check anywhere). 11 candidate Go checks vetted
against the pattern's four requirements (hot check / declared-type-implied
property / guard cheaper than check / no gc duplication); every one kills
(guard-is-the-check, no declared-type property, or already covered by gc's
prove/loopbce/nilcheckelim). One real-cost shape spun off as a §3 angle on
SURV-070 (assertion-site dominant-type guard, devirt lane). Full reason in
§5. **Tracks:** wave 4, T2 re-sweep → `track-resweep-wave4.md`; wave 5,
mechanism vetting → `track-surv077-wave5.md`.

### Tier 3 — solid niche / conditional

#### SURV-017 — Allocation-site capacity speculation (profile-observed pre-sizing)
**Bucket:** WOULD-LIKE (precedent: **ABLE** — V8 per-allocation-site backing-store
policy).
**Idea.** Profiler records observed max length per allocation site
(`make`/`append`/literals). The JIT pre-sizes backing stores to the observed max; if
exceeded, normal growth takes over — semantics-preserving, no deopt needed.
**Precedent.** [V8 elements kinds](https://github.com/v8/v8.dev/blob/HEAD/src/blog/elements-kinds.md):
per-array backing-store policy decided per allocation site, generalizing one-way on
violation.
**Transfer note.** Go element types are static — no SMI→double→dictionary lattice, no
holes. What transfers is per-allocation-site (not per-type) representation decisions +
one-way generalization on violation.
**Why gc doesn't do it.** Allocation sizing has no profile-feedback path; growth policy
is fixed in the runtime.
**Architectural fit.** E1 (per-site representation) + E4 (length profiles).
**Benchmark sketch.** `encoding/json` unmarshal into `map[string]any` (many small
maps); `net/http` header parsing; append-loops with stable small N.
**Open risks.** Over-allocation on unrepresentative profiles (memory bloat) — needs
hysteresis (size at p99, not max) and a shrink-back policy.
**Tracks:** T3-C3.

#### SURV-018 — Inline-cached map slot lookup for constant keys
**Bucket:** WOULD-LIKE (precedent: **ABLE** — the inline-cache discipline in
V8/JSC/CPython).
**Idea.** For repeated `m[K]` with constant K, cache (map → bucket, slot). Guard: `B`
and `count` unchanged since specialization + tophash/key revalidation at the slot.
Hit → direct value load, no hash walk. Miss → generic lookup (diamond; optionally
re-specialize).
**Precedent.** The inline-cache discipline generally — cache + guard + miss fallback
(V8 IC states; PEP 659 `LOAD_ATTR_*` families).
**Transfer note.** No hidden classes / type-version tags — Go maps have no shape
polymorphism; the guard is on *table state* (B, count) plus slot key revalidation.
Sound because Go never moves entries except on grow (B change ⇒ guard fails) and
delete marks the slot (key check fails ⇒ fallback).
**Why gc doesn't do it.** Map access is an opaque runtime call; no per-site caching;
no guard infrastructure.
**Architectural fit.** E3 + E4. Complementary to the backlog's map specialization
(representation vs access path) and to SURV-015 (fusion).
**Benchmark sketch.** Config/metadata lookups (`cfg["timeout"]`) in request hot paths;
repeated header/field extraction loops.
**Open risks.** Best for integer keys / short string keys (key-revalidation cost); must
not regress megamorphic-key sites — per-site polymorphism cap.
**Tracks:** T3-C4.

#### SURV-019 — Per-site fat closures for method values (descriptor-based closure representation)
**Bucket:** WOULD-LIKE (precedent: open Go proposal; no shipped Go analog).
**Idea.** `f := ptr.Method` allocates a closure object on the heap. Represent the
closure at hot creation sites as a 2-word value `{method descriptor, receiver}`
(identical shape to a single-method interface) — no allocation, LEAQ+MOV to
materialize. The descriptor (in .rodata) carries the method PC plus the metadata
reflect needs, so `reflect.ValueOf(f)` still works by consulting the descriptor.
**Precedent.** golang/go#80537 (open proposal, `GOEXPERIMENT=2pointers-closures`,
explicitly targeted at the **typescript-go** compiler workload).
**Why gc doesn't do it.** It changes `unsafe.Sizeof(func())` — an ABI break gc cannot
make: the representation of `func` is type-global. The obstacle named is ABI rigidity,
not the idea's merit.
**Architectural fit.** E1 (representation ≠ semantics: the JIT uses the fat
representation per allocation site / per value and converts at ABI boundaries, getting
the optimization without the global ABI break) + E4 (profiler finds hot method-value
sites). gc+PGO cannot vary representation per site.
**Benchmark sketch.** typescript-go compile workload (the proposal's named target); any
codebase with hot `x.Method` captures (e.g. `http.HandlerFunc`-style adapters, sort
comparators).
**Open risks.** Calling convention for the fat closure (descriptor-driven dispatch vs.
adapter thunk); reflect metadata completeness (`reflect.Value` semantics of method
values); GC must understand the descriptor word (or keep it out of scanned memory —
.rodata pointer is fine).
**Tracks:** T4-C5.

#### SURV-020 — Speculative niche-filling for interface values
**Bucket:** WOULD-LIKE, prototype-gated (precedent: **ABLE** — rustc niche-filling;
**THINKING** — RFC 3204 reference niches, implemented behind `-Z reference-niches`
rust-lang/rust#113166).
**Idea.** Rust packs enum discriminants into *invalid bit patterns* ("niches") of
payloads: `Option<&T>` is the size of `&T`. Go's analogs: interface values `(tab,
data)` and `(T, bool)` / `(T, error)` return pairs. Under the descriptor model (E1),
*speculatively* represent hot interface shapes using pointer niches (alignment spare
bits, null-data-word encodings) with a guard + deopt — instead of the fixed 2-word
iface — where profiling shows the niche holds.
**Precedent.** rustc layout computation tracks `largest_niche` per type (`valid_range`
+ `largest_niche` in layout dumps; Rust Reference `rustc_layout_scalar_valid_range_*`);
RFC 3204 extends niches to references.
**Why gc doesn't do it.** Go's nil-able reference types already get the null-pointer
optimization by language design; gc has no invalid-bit-pattern analysis for anything
else, and the interface representation is fixed at two words, type-globally.
**Architectural fit.** E1 (per-allocation-site representation is the exact precondition
niche-filling needs) + E3 (Go's typed-nil semantics make *static* niche-filling
unsound — a nil data word with non-nil tab is a non-nil interface — so speculation with
guard+deopt is mandatory, not optional) + E4 (representation-stability signal). Complementary angle to backlog "type narrowing + bit-packing": that item covers observed *value ranges*; niches cover *invalid patterns* — same family, distinct mechanism.
**Benchmark sketch.** Error-return-heavy call chains (nil-error fast path): branch-miss
reduction on the nil check; plus footprint on interface-heavy slices.
**Open risks.** Guard cost may eat the win; complexity vs payoff unclear — **mark
speculative until a prototype shows >noise**; the niche must be re-validated on every
deopt.
**Tracks:** T5.

#### SURV-021 — Per-allocation-site compact headers
**Bucket:** WOULD-LIKE (precedent: **ABLE** — JEP 450 JDK 24 experimental → JEP 519 JDK
25 product → JEP 534 JDK 27 default; **THINKING** — JEP draft 8349069 "4-byte Object
Headers").
**Idea.** Lilliput's founding observation — <1% of objects are ever locked, headers are
12 bytes of every object, average object 32–64 bytes so >20% of live data is header —
motivated shrinking headers 96→64 bits with 22-bit compressed class pointers (Oracle
SPECjbb2015 **vendor claim**: 22% less heap, 8% less CPU, 15% fewer GCs). Our analog:
with the descriptor model, the "header" (type descriptor pointer, GC metadata) need not
be type-global — choose per allocation site between full and compact descriptors,
driven by the profiler's representation-stability signal, with speculative narrowing +
deopt.
**Precedent.** [JEP 450](https://openjdk.org/jeps/450), [JEP 519](https://openjdk.org/jeps/519),
JEP 534; JEP draft 8349069 (if Lilliput goes 8→4 bytes, this candidate's payoff roughly
doubles — keep the design parameterized by header width).
**Why gc doesn't do it.** Go values carry a full `*rtype` word; the compiler has no
per-site representation freedom and no speculation to fall back on.
**Architectural fit.** E1 (representation ≠ semantics is *the* enabler) + E4.
Complementary, not duplicative, of backlog "type narrowing + bit-packing": that item
packs *fields*; this one shrinks *headers/descriptors* (orthogonal bytes).
**Benchmark sketch.** Allocation-heavy workloads — `encoding/json` decode benchmarks,
`net/http` request parsing; measure heap footprint + GC CPU vs gc+PGO.
**Open risks.** Descriptor narrowing interacts with reflection and `unsafe`; every
narrowed site needs a deopt story for `reflect.TypeOf` on the value. GC must understand
both descriptor widths (barrier co-design).
**Tracks:** T2.

#### SURV-022 — Identity-freedom analysis → header-free flattened values
**Bucket:** WOULD-LIKE (precedent: **THINKING** — JEP 401 Value Objects, preview for JDK
28 (integrated Aug 2026, disabled by default); JEP 539 strict field initialization).
**Idea.** Valhalla's core lesson is a *language-design* one — prove a value has no
identity and the runtime gets representation freedom for free (no header, no pointer,
flatten into parent). Our analog without a language change: a whole-program
**identity-freedom analysis** for Go struct types (no `==` on pointers to it, no
`sync.Mutex`/`atomic`/`noCopy` inside, address never taken observably, never used as a
map key by pointer) → allocate header-free and flatten into parents/slices at chosen
sites, *statically* (no guard needed when proven; speculative + deopt where only
profile-likely).
**Why gc doesn't do it.** gc never questions identity — every addressable value gets
the full treatment. Proving identity-freedom needs whole-program reasoning, which Go's
simplicity (no inheritance) makes cheaper than in Java.
**Architectural fit.** E1 (identity-free ⇒ narrowest descriptor, possibly zero-word
header) + E3 (speculative where unproven).
**Benchmark sketch.** Map/slice-heavy workloads with small struct values (JSON decode
into small structs, metric-label sets); allocation rate + footprint vs gc+PGO.
Complementary to backlog "speculative data representation": that item specializes
*containers*; this one removes identity from *values* — the static-proof variant is
cheaper than speculation wherever the analysis closes.
**Open risks.** `reflect` and `unsafe` can observe identity the analysis can't see —
conservative bailout list; interface boxing reintroduces identity (a flattened value
boxed into an interface needs materialization — the standard Valhalla problem, unsolved
cheaply anywhere).
**Tracks:** T2.

#### SURV-023 — Lock coarsening across simple control flow
**Bucket:** WOULD-LIKE (precedent: **ABLE** — C2 classic lock coarsening; **ABLE** —
GraalVM 25.5 GR-79526 "Extended lock elimination to coarsen locks across simple control
flow and eliminate nested locking of the same object", shipped 2026 — even mature JITs
still find this profitable).
**Idea.** After inlining, recognize adjacent `mu.Lock(); …; mu.Unlock()` regions over
the *same* `sync.Mutex` (proved by points-to/escape analysis) with no observable
operations between them, and merge into one critical section; drop nested lock/unlock
of the same mutex. Distinct from backlog "adaptive concurrency primitives" (runtime
adaptation) — this is a *static* compiler transform.
**Why gc doesn't do it.** `sync.Mutex` is a library type; gc never sees lock/unlock as
compiler IR across statements, and proving "same mutex, nothing observable between"
needs the inliner + alias analysis working together. Go's no-inheritance helps the
points-to proof.
**Architectural fit.** E3 (speculative: if the "same mutex" proof is profile-based
rather than static, guard + deopt).
**Benchmark sketch.** Fine-grained-locking microbenchmarks (mutex ping-pong, sharded
map with per-shard mutex, a KV-store op path doing lock/unlock per field). Must beat
gc+PGO — plausible because gc has no such transform at all.
**Open risks.** **Semantics minefield** — `defer mu.Unlock()` patterns, panics (merged
region changes what a panic observes as locked), lock *ordering* with other mutexes
(coarsening across an acquisition of mutex B while holding A changes deadlock behavior
— must prove no interleaving lock ops). `sync.Cond` wait inside the region forbids
merging. Needs a conservative legality checker; start with straight-line same-function
regions only. Narrow but real — keep as candidate with the legality work itemized, or
demote if the legality proof proves too expensive.
**Tracks:** T2.

#### SURV-024 — Move/copy elision for provably-dead sources
**Bucket:** WOULD-LIKE (precedent: **ABLE** as language feature — Swift SE-0390
"Noncopyable structs and enums", accepted, implemented Swift 5.9; SE-0377 ownership
modifiers as companion contract).
**Idea.** Swift made move-only types a language feature; C++ has move semantics. Go
copies on assignment, always. Transfer as a *runtime-level* optimization, no language
change: when liveness analysis proves — or profiling + a guard observes — that a
copied value's source is dead after the copy, convert copy→move. For heap-allocated
values that means pointer swizzling instead of memcpy; for `copy(dst, src)` and
append-growth, eliding the memmove when the source is uniquely referenced and dead.
**Why gc doesn't do it.** Go has no move semantics in the language; gc emits memmove
for struct copies and `copy()` always copies. Liveness-driven copy→move conversion
exists nowhere in the pipeline.
**Architectural fit.** E1 (in the descriptor model a "move" is just a representation
change — the model already knows value lifetimes) + E3 (speculative variant — guard on
uniqueness via the descriptor's reference tracking, deopt to a copy on violation).
The *constructive* twin of the backlog's "idle-time repack": instead of repacking
later, never copy in the first place.
**Benchmark sketch.** Append-growth loops over large element types; `copy(dst, src)`
where src dies; large struct by-value returns in a hot loop: graalgo-JIT vs gc+PGO.
Honest target: the `append`/`copy` builtins (Go style discourages huge by-value
structs, so general copy elision may be thin — claim dies if within noise).
**Open risks.** Write barriers: moving pointer-containing values still needs barriers
unless the source slot is dead *and* cleared — the proof bar is high.
`unsafe`/`reflect` aliasing defeats static proof → the speculative path is the
realistic one.
**Tracks:** T5.

#### SURV-025 — Speculative per-allocation-site object inlining (field-append)
**Bucket:** WOULD-LIKE (precedent: Dolby, J., "Automatic inline allocation of objects",
*PLDI* 1997; Dolby, J. & Chien, A., "An automatic object inlining optimization and its
evaluation", *PLDI* 2000 — whole-program analysis inlining uniquely-referenced objects;
eliminates allocation + indirection).
**Idea.** When a parent object holds the unique pointer to a freshly allocated child
(e.g. `n.Left = &Node{…}`), inline the child's fields directly into the parent's
allocation: one allocation instead of two, one fewer indirection per access. Decided
per allocation site from profiler-observed uniqueness, with a guard: if a
non-conforming (aliased/shared) allocation ever occurs at that site, deopt to the boxed
layout.
**Transfer note.** Inlining *replaces* the pointer with fields (strictly stronger than
Veldema et al.'s "object combining", which appends objects but keeps the pointer).
E1 makes inline-vs-boxed a *per-allocation-site* choice for the same type — strictly
more general than Dolby's whole-program type-global decision. **Go helps:** no
inheritance means the uniqueness analysis has no subtype-aliasing surprises (the hard
case in OO papers); Go's structural interfaces are the only aliasing leak, and the
guard handles exactly that.
**Why gc doesn't do it.** gc has EA → stack allocation/scalar replacement, but no
*heap* object inlining: a parent and its heap child are always two allocations with a
pointer between them. gc's analysis is per-function and conservative.
**Architectural fit.** E1 + E4 (profiler observes the child is always freshly allocated
at this site and never aliased elsewhere) + E3 (guard + deopt for the
interface-smuggled-alias case).
**Benchmark sketch.** AST-heavy workloads (Go parser producing `go/ast` nodes, then
walked) or a linked-list/node+payload microbenchmark: removes ~half the allocations and
one indirection per traversal step.
**Open risks.** Inlining bloats the parent's cache footprint when only a few child
fields are hot — interacts with field-affinity analysis (complementary to backlog
field reorder/pack). GC must support variable-shape inlined objects — descriptor model
helps, but it's new machinery. Guard check cost on the allocating path.
**Tracks:** T1-C3.

#### SURV-026 — Profile-driven allocation-site segregation (behavior-predicted pools)
**Bucket:** WOULD-LIKE (precedent: Seidl, M. L. & Zorn, B. G., "Segregating Heap
Objects by Reference Behavior and Lifetime", *ASPLOS-VIII*, 1998 — offline ATOM
profiling into four behavioral classes, separate arenas per class; heap references
highly predictable per allocation site. Complementary: Chilimbi, T. M. & Larus, J. R.,
"Using Generational Garbage Collection To Implement Cache-conscious Data Placement",
*ISMM* 1998 — paper claims 20–41% miss-rate reduction and 18–31% program speedup over
plain Cheney copying by placing temporally-affine objects adjacently).
**Idea.** Classify each allocation site by predicted object lifetime and reference
frequency (short/long-lived × hot/cold), then route allocations to separate bump arenas
per class. Short-lived cold objects never enter the old generation's scan set; hot
objects cluster for cache locality. Classification from the always-on profiler, not a
training run; re-decided per JIT tier.
**Why gc doesn't do it.** gc's allocator segregates by size class (mspan/mcache) only;
its escape analysis is a binary stack-vs-heap decision. No predicted lifetime or
reference behavior influences heap placement, and PGO in gc only feeds inlining — not
allocation policy.
**Architectural fit.** E4 (zero-training-run per-site lifetime/reference predictions —
the exact thing Seidl & Zorn lacked) + E1 (pool choice is a per-site property of the
value descriptor — the *same type* can live in different pools at different sites,
impossible under gc's type-global layout) + E3 (misprediction only costs placement
quality, never correctness; re-JIT re-classifies).
**Benchmark sketch.** `encoding/json` decode of a large payload (or an HTTP handler
benchmark): per-request short-lived objects (tokens, small maps, buffers) into a
FIFO/bulk-reclaim arena never scanned by old-gen GC, vs gc+PGO where they flow through
the shared heap. Win mechanism: reduced GC scan work + locality, not faster allocation.
**Open risks.** Phase behavior: site classification must be stable across workload
phases or re-classification churns; pool fragmentation if lifetime prediction is wrong
in the long-lived direction; needs a collector that treats segregated pools as regions
(bulk-free or reduced scan), otherwise the win evaporates.
**Tracks:** T1-C1.

#### SURV-027 — Lifetime-bounded pool allocation with bulk free (goroutine-scoped regions)
**Bucket:** WOULD-LIKE (precedent: Lattner, C. & Adve, V., "Automatic Pool Allocation",
*PLDI* 2005 — context-sensitive pointer analysis → disjoint pools; per-pool
optimizations incl. eliminating individual frees via whole-pool reclamation; paper
shows reduced working sets. Region lineage: Grossman et al., "Region-Based Memory
Management in Cyclone", *PLDI* 2002 (lexically-scoped regions; requires annotations —
ours is automatic + speculative); Tofte & Talpin, region inference for ML, *POPL*
1994).
**Idea.** Compiler identifies disjoint heap data structures (à la Lattner) whose
lifetimes are bounded by a lexical/goroutine scope; allocates each into its own pool,
and reclaims the *whole pool at once* when the scope ends — individual free/GC
operations for those objects disappear entirely. E2 supplies the scope signal: true
goroutine death/idleness.
**Why gc doesn't do it.** gc's escape analysis is stack-vs-heap only; heap objects with
scope-bounded lifetimes still pay full GC tracking. No region/pool middle ground, no
bulk-free path.
**Architectural fit.** E2 (the user-space scheduler knows *true* goroutine termination
and idleness — the natural scope signal for per-request/per-task pools) + E4 (profiler
confirms the "all die together" hypothesis before promoting a pool to bulk-free) + E3
(guard — a write barrier / remembered-set check catches an object escaping its pool's
lifetime; on violation, deopt that pool back to GC-managed) + E1. **Go helps:** no
inheritance dramatically simplifies Lattner's context-sensitive points-to analysis (the
OO papers fight class-hierarchy imprecision); the remaining precision leak is interface
values, handled by the guard. Distinct from backlog "idle-time repack/defrag/shrink"
(post-hoc) — this is allocation-time scoping.
**Benchmark sketch.** HTTP server / RPC handler benchmark (per-request allocation
storm: headers, JSON nodes, buffers): bulk-free at handler exit vs gc+PGO scanning
every short-lived object. Also compiler-like passes (AST per file).
**Open risks.** Pointer-analysis precision at scale — interface-heavy code may collapse
pools. Bulk-free safety proof must be airtight or guarded; a missed escape is a
use-after-free, not a perf bug. Pool-per-structure memory overhead.
**Tracks:** T1-C6.

#### SURV-028 — Prolific-type-driven allocation specialization
**Bucket:** WOULD-LIKE (precedent: Shuf, Y., Gupta, M., Bordawekar, R. & Singh, J. P.,
"Exploiting Prolific Types for Memory Management and Optimizations", *POPL* 2002 —
Jalapeño VM; paper claims: type-based collector cuts GC time up to 15% vs analogous
generational collector with fewer write barriers; short type pointers cut heap
requirements up to 16%; plus prolific-object recycling and co-allocation; evaluated on
SPECjvm98 and SPECjbb2000).
**Idea.** The profiler identifies *prolific types* (types accounting for most
allocations) per workload phase; those types get segregated nurseries, free-list
recycling, and compact type-ID tagging, while non-prolific types use the default path.
Type-aware policy replaces gc's type-blind size-class allocation for the types that
matter.
**Why gc doesn't do it.** gc's spans segregate by size class only; allocation policy is
type-blind, no object recycling, and type metadata is not exploited for layout or GC
policy. gc+PGO does not feed type-frequency profiles back into the allocator.
**Architectural fit.** E4 (always-on profiler maintains the prolific-type set per phase
— the paper used offline profiling; online re-detection handles phase changes) + E1.
**Go helps decisively:** in the Java original, a "prolific type" suffers subtype
polymorphism (many dynamic types hide behind one static type); Go has no inheritance,
so an allocated concrete type is *exactly* known — type-based segregation and recycling
are precise where the OO version was approximate.
**Benchmark sketch.** Workloads dominated by one allocation type: `encoding/json`
decode (token/small-map churn), `net/http` buffer churn, protobuf-heavy RPC — prolific
`[]byte`/node types get recycled instead of reallocated.
**Open risks.** Phase changes invalidate the prolific set — cheap re-detection and
policy switch. Recycling correctness: a recycled object must be provably dead;
conservative liveness or guarded recycling only. Overlaps SURV-026's axis (per-site vs
per-type) — they compose, but evaluate separately to avoid double counting.
**Tracks:** T1-C7.

#### SURV-057 — Map-copy range idiom → bulk clone with presizing
**Bucket:** WOULD-LIKE.
**Idea.** Recognize the `m2:=make(map[K]V); for k,v := range m1 { m2[k]=v }`
idiom (post-inlining, so caller/callee splits count) and lower to: presized
`makemap` (hint = len(m1)) + tight assign loop using the inlined fast-path
assign — skipping per-element `mapassign` call overhead and growth repetitions.
(Full memcpy is unsound without seed coupling — the issue notes the seed-leak
concern; presize+fast-loop avoids it.)
**Precedent.** golang/go#26951 (open); gogrep census: the pattern occurs *more*
often in std than the map-clear idiom (#20138, which gc did optimize).
**Why gc doesn't do it.** Unbuilt.
**Architectural fit.** E3 (idiom recognition across inline boundaries in the
JIT). Distinct from SURV-015/018 (access-path opts, not bulk copy).
**Benchmark sketch.** `maps.Clone`-shaped workloads.
**Open risks.** Seed/iteration coupling — the presize+loop form sidesteps the
memcpy soundness issue.
**Tracks:** T4-C-D (wave 2).

#### SURV-058 — Loop allocation batching (slab for N same-type allocs)
**Bucket:** WOULD-LIKE.
**Idea.** At hot loop allocation sites allocating the same type (E4-gated),
allocate one slab and carve N objects (bump-pointer within the slab) instead of
N `mallocgc` calls. Batches *sibling* allocations while preserving each object's
identity — distinct from SURV-025 (object inlining into parent) and SURV-003
(arenas).
**Precedent.** golang/go#60879 (open, FeatureRequest; reporter's benchmark:
−83.93% at N=32 — reporter's numbers, not ours; community confirms it "comes up
regularly" in real code).
**Why gc doesn't do it.** Unbuilt; no design exists.
**Architectural fit.** E1 (N logical objects, one physical slab) + E4
(hot-site gating). **Prototype-gated:** the load-bearing design work is
per-object GC pointer maps inside the slab.
**Benchmark sketch.** Tree/graph builders that `new(Node)` in a loop.
**Open risks.** The slab's interior GC maps are the design risk; phase changes
in the allocated type mix.
**Tracks:** T4-C-L (wave 2).

#### SURV-059 — Non-work-conserving scheduler wake policy
**Bucket:** WOULD-LIKE.
**Idea.** Scheduler *policy*, not a code transform: don't wake a sleeping worker
for work predicted (E4) to be shorter than the wake+sleep cycle — queue it for
an already-running worker instead, with hysteresis and a starvation bound. Only
possible because we own the scheduler.
**Precedent.** golang/go#54622 (open, NeedsInvestigation): models the three
costs (wake / wait-for-running-thread / run) and argues waking is heavyweight
enough that short work shouldn't always trigger it; cites #18237, #21827,
#32113.
**Why gc doesn't do it.** Unbuilt; Go's scheduler is work-conserving by design.
**Architectural fit.** E2 (true-idleness + holder/waiter signals are exactly the
inputs the policy needs) + E4. The backlog's "adaptive concurrency primitives"
is about *locks*, not wake policy — distinct.
**Benchmark sketch.** Bursty RPC-shaped load; metric = CPU per request at fixed
latency.
**Open risks.** Starvation-bound design; wake-prediction accuracy; the issue is
still at NeedsInvestigation — measurement-gated before commitment.
**Tracks:** T4-C-N (wave 2).

#### SURV-064 — Profiler-inferred data-structure-aware GC (automatic DSA)
**Bucket:** THINKING (precedent: **THINKING** — Cohen & Petrank, ISMM'15,
JikesRVM prototype; verified forward citation of Shuf et al. 2002).
**Idea.** Cohen & Petrank's DSA collector, minus the programmer: the always-on
profiler (E4) infers which types are data-structure *node* classes and which
program points are *removal* sites; the GC segregates node classes into
dedicated areas (locality + tracing load balance) and deprioritizes tracing of
removed-but-still-reachable nodes. Inference errors are performance-only by
construction — the paper's design already guarantees wrong hints can't cause
incorrect reclamation, and inferred hints keep that invariant.
**Precedent.** Cohen & Petrank, "Data Structure Aware Garbage Collector",
*ISMM* 2015. Paper claims (their measurements, not ours): KittyCache 40–45%
GC time / 4–20% overall (8 lines of annotations), HSQLDB 75–76% GC time /
31–32% overall (3 lines); non-DSA programs show negligible overhead.
**Why gc doesn't do it.** gc has no data-structure-aware collection policy.
**Architectural fit.** E4 (the profiler *is* the mechanism — it replaces the
paper's manual annotations) + E1 (per-structure segregation policy).
**Boundary with SURV-028 (wave-3 review): ranked separate.** SURV-028 is
*type-keyed allocation policy* (recycling, compact tags — the alloc side);
this is *instance-structure segregation + removal-event liveness* (the
collection side). Related axes, different mechanism.
**Benchmark sketch.** Cache/map/tree-heavy programs (Go equivalents of the
paper's KittyCache/HSQLDB/SPECjbb shape): GC time + overall wall-clock vs
gc+PGO.
**Open risks.** Prototype-gated: inference quality (node-class detection from
profiles) is unproven; GC-subsystem coupling is deep — this is the most
invasive of the wave-3 candidates.
**Tracks:** T1 wave 3 (forward-citation sweep).

#### SURV-078 — LLVM O3-only pass set as JIT optimizing-tier adoption targets
**Bucket:** WOULD-LIKE (precedent: **ABLE** — all four run in every LLVM O3
compile; verified in `PassBuilderPipelines.cpp` @ `5e1ed67`, 2026-09-29 —
source-level verification, more authoritative than a dump for gating
questions; local `opt` install was infeasible this wave).
**Idea.** The verified O3-only delta set: ArgumentPromotionPass (gated on O3
with the verbatim FIXME "It isn't at all clear why this should be limited to
O3" — still open and untracked in 2026, dates to the 2020 new-PM port
D78512), nontrivial loop unswitching (3 sites), CallSiteSplittingPass,
ControlHeightReductionPass (on by default at O3). Adoption candidates for our
JIT's optimizing tier. Arg promotion maps directly onto T6 (wave 3): promote
scalarizable args at inline time rather than gating on an optimization
level; at O3 it runs *inside* LLVM's `devirt<4>` repeater, re-running on
every devirt-triggered iteration. Also verified: unroll-threshold and inline-
param deltas (wave 3, confirmed), and the tiering surprise that
DataflowConstProp needs mir-opt-level ≥3 (absent at default `-O2`) and
MultipleReturnTerminators ≥4 (never by default) — a cautionary data point on
named pass vs actually-running pass.
**Why gc doesn't do it.** gc has no arg promotion and no unswitching; its
pipeline is fixed.
**Architectural fit.** E3. Prototype-gated: compile-time cost at JIT scale
needs measurement; arg promotion interacts with our calling-convention work.
**Benchmark sketch.** Call-heavy and loop-heavy workloads vs gc+PGO, with
compile-time budget measured alongside.
**Open risks.** The FIXME's gating is admitted-historical rather than
principled — the transfer is safe; the economics (compile time in a JIT) are
the prototype question.
**Tracks:** wave 4, T5 verification → `track-5-wave4-verify.md`.

### Tier 4 — narrow, prototype-gated, or infrastructure-dependent

#### SURV-029 — Hardware-monitor-guided software prefetch injection for pointer chases
**Bucket:** WOULD-LIKE (precedent: Adl-Tabatabai, A.-R., Hudson, R. L., Serrano, M. J.
& Subramoney, S., "Prefetch Injection Based on Hardware Monitoring and Object
Metadata", *PLDI* 2004 — hardware-monitor-driven prefetch insertion on Java workloads.
Ancestor: Luk, C.-K. & Mowry, T. C., "Compiler-based Prefetching for Recursive Data
Structures", 1996 (greedy/history-pointer prefetching; Mowry's 15-745 course slides
report 4–45% speedups on Olden pointer benchmarks for greedy prefetching); Mowry, Lam &
Gupta, *ASPLOS-V* 1992).
**Idea.** JIT inserts prefetch instructions ahead of pointer-chasing dereferences
(linked lists, trees, AST walks), with distance and site selection driven by the
always-on profiler correlated with hardware miss counters — not static heuristics.
Prefetch is a pure hint: zero semantic risk, guarded only by a profitability model.
**Cautionary evidence (honesty).** Poirier, Rohou & Serrano, "An Attempt to Catch Up
with JIT Compilers: The False Lead of Optimizing Inline Caches", arXiv 2502.20547
(2025), found removing memory reads often yields *no* wall-clock win on contemporary
architectures — prefetch must be measured, not assumed; modern out-of-order cores with
good MLP may absorb the latency.
**Why gc doesn't do it.** gc emits no prefetch instructions at all; hardware
prefetchers track strides, not pointer chains. gc+PGO has no mechanism to convert miss
profiles into prefetch insertion.
**Architectural fit.** E4 (profiler + hardware counters attribute L2 misses to chase
sites; value/range profiles pick prefetch distance per site). E3: not needed for
correctness (hint only); the guard is economic (disable site if measured useless). E1:
descriptor can carry a "chase stride" annotation per allocation site.
**Benchmark sketch.** Large treap/B-tree/AST traversal — a Go `go/ast` walk over a big
syntax tree, or a red-black-tree lookup microbenchmark: pointer-chase latency dominates,
hardware prefetcher can't help, gc+PGO has no answer.
**Open risks.** Wrong distance wastes bandwidth and pollutes cache; see the cautionary
evidence above — prototype-gated on measured wins.
**Tracks:** T1-C2.

#### SURV-030 — Rich LLVM attribute emission from Go language semantics
**Bucket:** WOULD-LIKE, conditional (precedent: **ABLE** — rustc as existence proof that
a frontend feeds LLVM rich attributes from language semantics; primary: LLVM LangRef
attribute semantics — `dereferenceable` ⇒ speculative loads legal, `nonnull`,
`noalias` — reviews.llvm.org/D61652, D95238).
**Idea.** **If** graalgo's AOT backend lowers Go to LLVM IR, annotate it the way rustc
does: `nonnull`, `dereferenceable(N)`, `noundef`, `noalias`/`nocapture`, `readonly` on
pointers and calls, derived from Go type facts + escape analysis — pointers from
`new`/`make` are nonnull+dereferenceable; map/slice headers are nonnull; EA-proven
non-escaping params get `noalias`. These attributes unlock LLVM's GVN/LICM/DSE/SROA on
pointer-heavy code.
**Why gc doesn't do it.** gc has no LLVM layer at all; its backend consumes its own
internal alias info and never reifies facts as optimizer-visible attributes.
**Architectural fit.** E5 (AOT-on-LLVM) + E1 (the descriptor model's per-site
representation knowledge is exactly the feedstock for precise attributes — a descriptor
stating "this value is a non-nil map" justifies `nonnull`+`dereferenceable` on the hmap
pointer, knowledge gc's type-global lowering cannot express).
**Benchmark sketch.** Pointer-chasing data-structure workload (ordered map / tree
insert+lookup mix): graalgo-AOT-on-LLVM with attributes vs the same pipeline with
attributes stripped vs gc+PGO. Claim dies if the attribute delta is noise — this
isolates the transfer from the backend change.
**Open risks.** Attribute soundness is UB-on-violation: every attribute needs a proof
(type fact or EA), never a hope. `unsafe.Pointer` and `reflect` can invalidate static
facts — restrict to compiler-derived facts. LLVM attribute semantics evolve across
versions (D95238 changed dereferenceable/noundef interaction) — pin the LLVM version
and its LangRef. **Conditional on the AOT backend choice — do not invest until the
backend is LLVM.**
**Tracks:** T5.

#### SURV-031 — Read-only `[]byte(s)` copy elision (borrow the string buffer)
**Bucket:** WOULD-LIKE (precedent: **ABLE** — TinyGo's `StringToBytes` transform pass,
tinygo-org/tinygo PR #3923: "convert any read-only use of a `runtime.stringToBytes`
call to use the original string buffer instead", plus the follow-up soundness fix;
v0.34 changelog: "transform: optimize range over `[]byte(string)`").
**Idea.** `[]byte(s)` always allocates + copies in gc, even when the resulting slice is
only ever read (never written, never retained past the string's lifetime). At hot
conversion sites where the JIT can prove read-only use (all uses are loads, no escape
of a mutable reference), alias the string's buffer directly: zero alloc, zero copy.
**Soundness caveat (learned from TinyGo's bugfix).** Any write through the slice must be
detected — the proof must cover all uses including inlined callees; fall back to the
copying conversion where the proof fails. gc only elides at blessed sites for the
*other* direction (`m[string(b)]`, `switch string(b)`, comparisons);
golang/go#26498 (constant-folding of conversions, still NeedsInvestigation) shows even
the constant case is unbuilt.
**Why gc doesn't do it.** No general read-only-use analysis for the conversion result;
the team's posture (per #26498 discussion) is caution about the sharp edges (read-only
memory faults on write). The obstacle is analysis precision, which a JIT with per-site
profiling and exact use-tracking can afford.
**Architectural fit.** E1 (per-allocation-site representation: this site's `[]byte` is
an immutable alias view, not an owned copy — invisible to the type system) + E4
(profiler finds hot conversion sites so the analysis cost is spent where it pays).
**Benchmark sketch.** `h.Write([]byte(s))`-style hashing/encoding loops over strings
(hash.Hash has no WriteString); `for range []byte(s)` byte-scan loops. Measure
allocs/op 1→0 and memcpy elimination.
**Open risks.** Write-detection must be airtight (TinyGo shipped this bug once — needs
a red-green regression test); string data may live in read-only pages (faults on missed
writes are unrecoverable); GC liveness: the slice must keep the string alive (pointer is
enough); must not apply when the slice is passed to code that may write (e.g.
`io.Reader`-shaped APIs that fill the buffer — those are writes).
**Tracks:** T4-C7.

#### SURV-032 — JIT-specialized multiway select lowering
**Bucket:** WOULD-LIKE (precedent: golang/go#19331 (2017) — still the old lowering per
2026-10-04 fetch; gccgo already uses the switch form, per @ianlancetaylor).
**Idea.** gc lowers multiway `select` to the returns-twice
`selectsend/selectrecv/selectgo` dance (setjmp/longjmp-like), which pessimizes the CFG
and blocks SSA optimization. The JIT compiles hot `select` statements into a bespoke
fast path: inline the buffered-channel readiness checks and the lock-ordering for the
statement's fixed case set, falling back to the generic runtime select when channels
are unbuffered/contended or the case set's behavior changes.
**Why gc doesn't do it.** The proposal's own cons: switch lowering uses binary search vs.
select's direct PC jump; AOT must handle the fully general case (any channel
combination, blocking). The obstacle is generality without usage data.
**Architectural fit.** E2 (user-space scheduler signals: waiter counts and channel
buffer states tell the JIT which cases are actually contended — specialize the observed
shape) + E3 (guard + fallback to the generic select) + E4 (profiler finds hot
selects). Complements the backlog's "adaptive concurrency primitives" (that item is
about primitive behavior; this is about select *code generation*).
**Benchmark sketch.** Channel fan-in/fan-out dispatch loops (pub/sub routers, `net`
poller-adjacent multiplexers); measure a hot 3–4-case select in a tight loop.
**Open risks.** Select fairness semantics (pseudo-random case choice) must be preserved
exactly; lock-ordering for multi-channel select must remain deadlock-free; fallback must
be provably taken whenever the observed shape is violated.
**Tracks:** T4-C6.

#### SURV-033 — Speculative bounds-check hoisting via loop versioning
**Bucket:** WOULD-LIKE (precedent: golang/go#47529, declined `//go:noboundcheck`
(+1:0/−1:11) and duplicate #66318 — the decline calculus changes, see below).
**Idea.** For hot loops where gc's BCE leaves per-iteration checks (index expressions
the SSA BCE can't prove in-range), the JIT compiles a versioned loop: one guard up
front proving `i < len(s)` for the whole trip count (a single range check on the loop
bounds), then a check-free body; on guard failure, deopt to the checked version. No
user directive needed.
**Why gc doesn't do it.** The decline was aimed at the *user-facing unsafe directive*,
not at eliminating the checks: "the individual directive has already been proposed and
declined." An AOT compiler can't insert a speculative guard with a deopt fallback — the
obstacle the issue names is ergonomic/safety, i.e. exactly what a JIT guard removes.
gc+PGO cannot do this: PGO has no deopt mechanism.
**Architectural fit.** E3 + E4 (profiler finds the hot loops where BCE fails). Ranked
Tier 4, not higher, because gc's BCE is already strong — this only fires where
`-d=ssa/check_bce/debug=1` shows un-eliminated checks.
**Benchmark sketch.** Tight numeric loops over slices with non-trivial index arithmetic
(stencil/convolution kernels, `image/draw`-style loops; the #21735 thread cites an 11%
image/draw regression from a single extra LEA — same workload family is check-
sensitive).
**Open risks.** Guard cost must be amortized (trip-count ≫ 1; profiler must confirm);
deopt path must be cold-verified; only version loops where BCE demonstrably fails.
**Tracks:** T4-C1.

#### SURV-034 — Whole-program identical code folding (ICF)
**Bucket:** WOULD-LIKE (precedent: **ABLE** — GCC `ipa-icf` at `-O2`; **ABLE** — LLVM
`-mergefunc`).
**Idea.** After whole-program analysis, fold functions (and read-only data) with exactly
identical semantics into one copy. Saves text size and i-cache.
**Precedent.** GCC Internals: ipa-icf discovers "functions and read-only variables that
have exactly the same semantics."
**Why gc doesn't do it.** Verified: no ICF/mergefunc anywhere in `cmd/compile` or
`cmd/link`. Go generics *increase* the duplication surface: gcshape stenciling emits
one instantiation per shape, and identical shapes across packages are never folded.
**Architectural fit.** E5 (pure-AOT closed-world build is the natural place — the whole
program is visible at image-build time, exactly the condition ICF needs; also applies
to hybrid images).
**Benchmark sketch.** Binary size + i-cache misses on a large generics-heavy binary
(a program instantiating the same generic containers/algorithms across many packages):
graalgo-AOT with ICF vs `go build`. Metric: text size and `perf stat` i-cache miss rate
on a fixed workload. **Primarily a size/i-cache play, not peak throughput — say so up
front.**
**Open risks.** Folding interacts with function-identity-sensitive code
(`reflect.ValueOf(f).Pointer()`); Go spec says func values are only comparable to nil,
but `reflect` exposes PCs — must exclude identity-observed functions (GCC's ipa-icf has
the same caveat class).
**Tracks:** T5.

#### SURV-035 — Machine outlining for code size
**Bucket:** WOULD-LIKE (precedent: **ABLE** — LLVM MachineOutliner, shipped;
reviews.llvm.org/D26872 (original), D45916 (enabled by default under `-Oz` for
AArch64)).
**Idea.** Dedupe repeated machine-instruction sequences *across functions* into shared
outlined functions (called, not inlined): shrinks text, improves i-cache density. Go
binaries are a prime target — gc emits verbose repeated sequences (prologues,
bounds-check idioms, write-barrier sequences) per function with no cross-function
machine-level dedup. (Distinct from ICF: outlining works at the *instruction-sequence*
level within function bodies, ICF at the *whole-function* level.)
**Why gc doesn't do it.** gc's backend has no cross-function machine-level dedup pass;
identical sequences are emitted per function.
**Architectural fit.** E5 (AOT) + E4 (profile-guided outlining — outline cold/repeated
sequences, keep hot sequences inline to avoid call/ret on hot paths).
**Benchmark sketch.** Text size + `perf stat` i-cache miss rate on a large Go binary
(e.g. `cmd/compile` itself) and startup page-fault count: graalgo-AOT with outlining
vs `go build`. **Primarily size/i-cache, not peak throughput.**
**Open risks.** Outlining on a hot path is a pessimization — the hotness gate is
load-bearing; interacts with PGO block layout (outline after layout, or re-verify).
**Tracks:** T5.

#### SURV-036 — Interprocedural purity / readonly deduction
**Bucket:** WOULD-LIKE (precedent: **ABLE** — GCC `ipa-pure-const`; **ABLE** — LLVM
`-function-attrs` "Deduce function attributes").
**Idea.** Whole-program deduction of which functions are pure/const (no memory writes,
no reads of mutable globals) and which pointer params are readonly; feeds DCE of
calls, LICM hoisting of calls, and store elimination.
**Why gc doesn't do it.** No interprocedural attribute deduction (verified in pass
list); purity knowledge is limited to compiler intrinsics and builtins. A pure helper
called in a loop cannot be hoisted by gc's licm.
**Architectural fit.** E5 (AOT closed world); in JIT mode, purity can be *speculative*
(more honestly: derive from closed-world analysis of the compiled method set, which the
JIT also has). Small, composable, low-risk.
**Benchmark sketch.** Loop calling a pure helper (a small pure hash or `math`
function) where hoisting/DCE across the call matters: graalgo vs gc+PGO. **Expect
single-digit percent — honesty: this is an enabler for the loop suite, not a
headline.**
**Open risks.** Go's memory model + write barriers: a "pure" function may still trigger
write barriers on stack growth — purity must be defined modulo runtime-internal
effects. Keep the bar high to stay sound.
**Tracks:** T5.

#### SURV-037 — Phase-adaptive recompilation (rejit on profile shift)
**Bucket:** WOULD-LIKE (precedents: **ABLE** — .NET tiered compilation rejit;
**ABLE** — JSC re-optimization triggered by OSR-exit counts, thresholds doubling on
each recompile).
**Idea.** In JIT mode, keep Tier0-style lightweight probes live after Tier1
compilation. When the profile shifts significantly (new hot path, changed branch
behavior, new receiver types), rejit with fresh speculation. gc+PGO is frozen at build
time and cannot follow phase changes — this is a win *only* a live JIT can claim.
**Why gc doesn't do it.** AOT + static PGO; no runtime code regeneration exists.
**Architectural fit.** E3 + E5 (JIT mode). This is a *policy* on top of the other
speculative candidates, not a standalone optimization — ranked Tier 4 accordingly.
**Benchmark sketch.** Long-running server with shifting workload phases — an API gateway
whose hot endpoint mix changes diurnally, or a batch pipeline with distinct phases.
Measure: stale-PGO gc vs rejitting JIT across a phase change.
**Open risks.** Rejit storms on flapping profiles → hysteresis + cooldowns required
(adopt JSC's double-thresholds-on-recompile policy); code-version memory churn.
**Tracks:** T3-C7.

#### SURV-038 — Profile-driven vector-plan selection
**Bucket:** WOULD-LIKE (precedent: LLVM's own vectorizer wishlist — **THINKING**:
VPlan seminar paper summarizing Florian Hahn's directions: tail-loop folding by
default, reviving outer-loop vectorization, merging SLP with VPlan, vector-predication
(VP intrinsics) roadmap).
**Idea.** The structural complaint underlying LLVM's vectorizer wishlist: LLVM picks
vectorization *plans* with a static cost model, blind to actual trip counts. Our JIT
observes real trip-count distributions (E4) and can select vector plans per observed
distribution — or version loops per plan (ties to SURV-010). Static compilers *wish*
for this information; we have it.
**Why gc doesn't do it.** No vectorizer (backlog) and no value profiling; static cost
models are the only tool.
**Architectural fit.** E4 + E3. Complementary angle for the backlog vectorization item
(which covers *that* we vectorize; this covers *how we choose plans better than LLVM's
static cost model*).
**Benchmark sketch.** Odd/short-trip-count loops over slices (the tail-folding case):
plan selected on observed counts vs LLVM-style static choice. Prototype-gated.
**Tracks:** T5.

#### SURV-039 — GC-cooperative cache-conscious copying with online affinity profiling
**Bucket:** WOULD-LIKE, **CONDITIONAL** (precedent: Chilimbi, T. M. & Larus, J. R.,
"Using Generational Garbage Collection To Implement Cache-conscious Data Placement",
*ISMM* 1998 — paper claims 20–41% miss-rate reduction and 18–31% program speedup over
plain Cheney copying by placing temporally-affine objects adjacently).
**Idea.** During compacting GC, copy objects with high temporal affinity adjacently so
contemporaneously-accessed objects share cache lines (clustering), and segregate
hot/cold objects into non-conflicting cache regions (coloring). Affinity from a
low-overhead online object-access buffer maintained by the always-on profiler — no
training runs.
**Why gc doesn't do it.** Go's GC is non-moving by design (no object relocation, to
keep pointer identity and cgo interop simple) — so there is *no* GC copy pass in which
to reorganize layout. **This candidate exists only if graalgo's collector is
moving/compacting for at least some heap region — if the collector stays non-moving
like Go's, this is dead on arrival; confirm before investing.**
**Architectural fit.** E4 (the 1998 paper's key weakness was needing compiler
instrumentation for the access buffer; the always-on profiler is the native
implementation of exactly that sensor) + E1 (per-object placement decisions during
copy fit the descriptor model).
**Benchmark sketch.** Pointer-tree/graph traversal over a large live heap — a big
treap lookup benchmark or GC-heavy graph workload: after a few collections the hot
working set becomes cache-dense, vs gc+PGO where layout is allocation-order forever.
**Open risks.** Conditional on moving GC (above). Copying cost vs locality benefit must
be measured per workload; affinity staleness across phases. Distinct from backlog:
backlog has *field* reorder/pack (intra-object) and *idle-time* repack/defrag/shrink
(footprint); this is *inter-object* hot-path placement during GC — flag the proximity.
**Tracks:** T1-C5.

#### SURV-040 — Post-link code re-layout, Propeller/BOLT style
**Bucket:** WOULD-LIKE (precedent: **ABLE** — Facebook BOLT (open source); LLVM
Propeller — post-link optimizer using profiles, used in production at Google/Meta).
**Idea.** After linking the AOT binary, re-optimize function/basic-block order using
perf-counter profiles (LBR) — a post-link pass independent of the compiler's own
layout. Complements SURV-005; catches cases the compiler's function-at-a-time view
misses.
**Why gc doesn't do it.** No post-link step exists in the Go toolchain; cmd/link emits
final layout.
**Architectural fit.** E5 (pure-AOT) + E4 (supplies the profiles without a training
run). Closed-world AOT binaries are ideal BOLT inputs (static, self-contained).
**Benchmark sketch.** Same as SURV-005: `cmd/compile` build workload + large server
binary; metric: instructions-per-cycle and i-TLB misses via `perf stat`.
**Open risks.** Toolchain complexity (needs perf/LBR access at build or deploy time);
benefit concentrates in >10 MB text binaries — small Go CLIs may see nothing. Wave-2:
verify Propeller's current LLVM docs URL.
**Tracks:** T5.

#### SURV-073 — Patch-in-place guard self-downgrade on assumption invalidation
**Bucket:** WOULD-LIKE (precedent: **ABLE** — ZJIT `zjit/src/invariants.rs`:
broken assumptions rewrite the guard site to `jmp side_exit`; no deopt, no
version discarded).
**Idea.** When a registered assumption breaks (runtime hook enabled;
profiler attached; `GODEBUG` switch flipped; new `plugin` implementation
appears), rewrite the guard site in place to jump to the fallback instead
of deopting or discarding the compiled version. ZJIT's invariant taxonomy
is the model for the registry: language facts (method sets — trivially
stable in Go), runtime facts (CPU profiler, traceback capture, `GODEBUG`
switches), allocation facts (fast path assumes no allocation observer).
Note the split: assumption invalidation takes the patched guard; *value*
guard failures still take side exits.
**Why gc doesn't do it.** gc has no speculation to invalidate — there is
nothing to downgrade.
**Architectural fit.** Infrastructure-dependent on the §4.1/§4.5
invalidation registry — this is the invalidation *action* alternative to
deopt-discard for it. Framing caveat: its win is over *deopt-based*
invalidation, not over gc+PGO; benchmark it as "cost of being wrong once"
(workloads with rare invalidation events: config reload under SURV-067,
profiler attach/detach, plugin load).
**Benchmark sketch.** Rare-invalidation workloads: steady-state cost of
patch-in-place vs deopt-and-recompile across profiler attach/detach
cycles.
**Open risks.** Code-patching safety on target ISAs (i-cache coherence,
concurrent execution); the downgrade is permanent — the fast path never
returns without recompilation, so it needs a re-arm policy.
**Tracks:** T3 wave 3 (ZJIT source pass, W6b).

---

#### SURV-076 — Profile-guided first-use-order binary layout for Go-AOT images (cold-start only)
**Bucket:** WOULD-LIKE (precedent: **THINKING** — Basso, Prokopec, Rosà,
Binder, "Improving Native-Image Startup Performance", CGO'25; authors' 13-pp
PDF read 2026-10-04, DOI unverifiable — recorded as a negative, not invented;
research prototype on Graal+Native Image, not shipped).
**Idea.** Reorder binary sections by first-execution order from an instrumented
training run: CU-level code ordering in `.text` + pre-init-data ordering (the
Go analog of `.svm_heap`: package-level vars, interned strings, reflect/type
metadata). Paper's measured claims (Are-We-Fast-Yet, caches dropped):
1.58×/1.66× `.text` page-fault reduction (SSD/NFS), ~1.40–1.41× heap-snapshot
reduction via structural-hash/heap-path object ID schemes, 1.65×/1.68× combined
page-fault reduction, 1.59×/1.58× end-to-end speedup (their numbers). Go
transfer is *simpler* than the paper: Go AOT builds are deterministic, so the
heap-path ID machinery (root-path hashing) is over-engineering — encounter-order
mapping suffices. The paper's IR-level path-profiling tracer (runs even during
VM init, no heap access on the instrumentation path) is the methodology to copy.
**Why gc doesn't do it.** gc does no binary re-layout at all.
**Architectural fit.** Enabling infrastructure for cold-start latency
(FaaS/container cold pages over dropped-cache SSD/NFS), NOT a peak-throughput
candidate — the paper itself shows heap ordering *regressing* long-running
Havlak 0.97–0.98×: startup-optimal layout is steady-state-pessimal, so this
must be a cold-start-gated layout, never the default. The
training-run-dependent dual of SURV-075's statically-generalizable profiles; a
cross-run sibling of SURV-014's cache.
**Benchmark sketch.** Cold-start time (page faults, time-to-first-request) on
FaaS-shaped Go workloads vs default layout; steady-state regression check
(Havlak-style) as a kill gate.
**Open risks.** Needs a representative training run (profile-staleness risk);
pays only when cold pages dominate; no evidence on Go or non-Java heaps.
**Tracks:** wave 4, CGO'25 pair (T2/T5 lane) → `track-cgo25-wave4.md`.

## §2 Attempted elsewhere — failure reasons as design constraints

**ATTEMPTED** items, consolidated across tracks. Each names the obstacle; each says
explicitly whether our architecture routes around it. These are not candidates — they
are the reasons-shaped knowledge that keeps us from re-learning them.

1. **Biased locking (HotSpot) — removed in JDK 18+ (JEP 374).** Failure reason:
   complexity vs payoff — per-thread bias state, revocation storms, and the rise of
   uncontended fast paths made the machinery not worth it. **Routing:** Go has no object
   headers to bias and goroutines never pin an OS thread across a wait; the lesson that
   transfers is "use guard+deopt, not per-thread bias state" — our speculation discipline
   is the cheaper mechanism. (T2)
2. **Intel TSX lock elision — hardware deprecated RTM.** Failure reason: the silicon
   feature was fused off / deprecated — building on it was building on sand.
   **Routing:** don't build on fusable-off silicon features. (T2)
3. **HotSpot stack-locking — dropped for compact headers (Lilliput).** The lightweight
   locking design was superseded; lesson folded into SURV-021's header design. (T2)
4. **Graphite (GCC polyhedral loop optimization) — effectively unmaintained.** Failure
   reason: polyhedral optimization needs precise dependence analysis on affine loop
   bounds; real loops are rarely cleanly affine, profitability heuristics were brittle,
   compile-time cost high for single-digit wins on non-hand-tuned code. The obstacle was
   *static legibility of loop bounds*, not the math. **Routing:** partial — E3+E4: a
   speculative JIT observes *actual* trip counts, bounds, and dependence behavior at run
   time, exactly the information Graphite couldn't get statically. But if the loop isn't
   hot enough to justify polyhedral compile time in a JIT, the economics still fail.
   **Verdict: do NOT resurrect Graphite-as-a-framework; DO treat "runtime-known bounds
   enable dependence-proof loop restructuring" as a JIT-mode-only technique with a hard
   hotness gate.** The failure reason transfers as a design constraint, not a veto. (T5)
5. **Placement-new saga (Rust RFC 469 box_syntax / RFC 809 / RFC 1228).** Failure
   reason: `box_syntax` shipped as surface syntax without the semantics it was meant to
   serve; it blocked better designs and became dead weight (`box_patterns` removal,
   rust-lang/rust#156749, May 2026). The obstacle was design-incompleteness, not
   implementation difficulty. **Routing:** yes — we get placement-new's *benefit*
   (construct directly in the final representation/location) with no language syntax at
   all: the descriptor model + PEA materialize a value directly in its final form.
   **Process lesson:** never add surface syntax ahead of the optimization it serves. (T5)
6. **Specialization (Rust RFC 1210) — accepted, never stabilized.** Failure reason:
   soundness of impl selection under lifetime erasure — choosing an impl by lifetime
   bounds requires lifetime knowledge at codegen time, which erasure forbids; the
   `min_specialization` subset remains nightly-only with blocking issues
   (rust-lang/rust#149257, #147507, #103708, #150387, #157731; tracking #31844).
   **Routing:** completely — our per-call-site specialization (SURV-016) is *code
   versioning*, not trait/impl resolution: no coherence, no impl selection, no
   lifetime-dependent dispatch. The obstacle does not exist in our mechanism. This is
   why the candidate is framed as specialization-of-code, never as a language feature. (T5)
7. **HPHPc (PHP→C++ transpiler) — abandoned.** Failure reason: dynamic-language
   features (eval, dynamic includes) defeated static whole-program compilation.
   **Routing:** yes — statically-typed Go has no such hole; the only hole is `plugin`,
   statically detectable. Revived as SURV-001. (T3)
8. **TraceMonkey (SpiderMonkey) — abandoned for method JITs.** Failure reason: trace
   explosion at control-flow merges; JägerMonkey → IonMonkey won. **Routing:** none
   needed — we wouldn't build a tracer either; the lesson is "method-based speculative
   JIT, not tracing." (T3)
9. **LuaJIT trace compiler — same lesson as TraceMonkey** for speculation; its DynASM
   is a compile-*latency* mechanism, not a throughput technique. (T3)
10. **Ruby MJIT (external C compiler as JIT backend) — abandoned for in-process YJIT.**
    Failure reason: batch compilation through an external compiler couldn't deliver
    profile feedback or warmup. **Lesson for our JIT-mode architecture: the JIT must be
    in-process with profile feedback, not a batch backend.** (T3)
11. **JSC LLVM-backed FTL — abandoned for the B3 backend.** Failure reason: LLVM compile
    times exceeded JIT budgets. Lesson for SURV-037's rejit budgets, not a candidate
    (our backend, Graal, is already fixed). (T3)
12. **LLVM `-bb-vectorize` — removed from LLVM itself.** The pass was pulled upstream;
    don't resurrect it; vectorization goes through the backlog item + SURV-010/038. (T5)
13. **golang/go#47529 (`//go:noboundcheck`) — declined (+1:0/−1:11).** Failure reason:
    the Go team rejects a *user-facing unsafety directive* for skipping checks. The
    decline was aimed at the directive, not the checks. **Routing:** yes — a
    compiler-inserted speculative guard + deopt (SURV-033) is semantics-preserving and
    needs no directive; the obstacle the issue names (ergonomic/safety) is exactly what
    a JIT guard removes. (T4)
14. **golang/go#41948 — selectgo: don't lock all channels — ATTEMPTED, abandoned.**
    Dmitry's CL 7570; randall77: "it is tricky to get right. We never convinced
    ourselves it was correct." Obstacle: the lock-ordering / deadlock-avoidance
    proof across channels — a fundamental correctness proof, not a missing profile
    signal. **Routing:** do not resurrect as stated; the motivation (select
    overhead) is addressed by SURV-032 (JIT-specialized select lowering). (T4 wave 2)
15. **golang/go#21536 — `//go:inline` directive — DECLINED (rsc).** josharian:
    "Go eschews knobs. Compiler directives are widely disliked." The decline
    targets the *user-facing knob*, not inlining itself. **Routing:** PGO-driven
    inline policy (SURV-006 priority inlining) — the profiler, not the
    programmer, decides. No directive needed. (T4 wave 2)
16. **runtimefreegc obstacle list (golang/go#74299 family) — active experiment,
    instructive failures.** B1: #80697 — alias analysis prematurely commits
    NoAlias on multi-assignment `r, old = append(r, n), r` → use-after-free
    class bug (open, resolution unknown). B2: #79909 — range-over-slice retains
    the backing store → wrong results (fixed via CL 789060). Design-doc
    obstacles: concurrent-GC cooperation, finalizers/specials excluded,
    `freegc` keeping pointers alive (liveness pessimization), 16B–32KiB size
    window, zero-regression bar on the normal alloc path. **Routing:** static
    casework gets harder as coverage grows (B1/B2) — the argument for our
    speculative form (SURV-056) over more static cases; the conceded static
    sub-case is recorded there. (T4 wave 2)
17. **Cinder HIR JIT + Static Python — ATTEMPTED, abandoned (absent from the
    live tree, 2026-10-04).** Meta's parallel optimizing JIT and typed dialect
    are gone from `facebookincubator/cinder`; the fork now rides upstream
    CPython's PEP 744 tier-2. Obstacle: maintaining a second JIT and a second
    language against upstream velocity. **Routing:** the obstacle does not
    transfer to us (one language, one backend), but the *lesson* does — don't
    build a second language or a second backend; speculate inside the one
    language. Our plan already does this. (T3 wave 3)
18. **Pyston LLVM JIT + nitrous/pystol partial evaluation — ATTEMPTED, never
    shipped (repo frozen 2024-08-12; `pyston/llvm/` empty).** Obstacle:
    JIT cost/complexity and tracking upstream CPython — same family as
    item 17. **Routing:** what ships is `pyston_lite` (copy-and-patch +
    quickening); the transferable slivers are policy data (counted-failure
    inline caches → SURV-052 angle; AOT speculation hints → SURV-004 angle)
    and the fact-registry mechanism (SURV-068), not the JIT architecture.
    (T3 wave 3) **Wave-4 confirmation:** the repo now reads "(No longer
    maintained)"; ecosyste.ms (synced 2026-10-04) shows 0 commits and 0
    committers in the past year, last commit 2024-08-12 — the LLVM line is
    dead, not dormant. T3 re-survey trigger ("Pyston commits resume")
    remains armed but un-fired.
19. **Sorbet Compiler (Stripe) — ATTEMPTED, experimental typed-Ruby AOT
    abandoned (announced 2021; `compiler/` absent from `sorbet/sorbet` by
    2026, no deprecation note).** Mechanism: type-annotated IR → LLVM →
    native `.so` Ruby extensions; VM callback for uncompilable features;
    per-file `# compiled: true` opt-in with a patched require path. Observed
    failure mode: deployment friction (dual artifacts, per-file markers,
    patched require path) + parallel-backend maintenance cost — same family
    as items 17 (Cinder HIR) and 18 (Pyston LLVM). **Routing:** hardens the
    deployment bar for any Go AOT mode: one build flag, never per-file
    markers or dual artifacts (SURV-001 already does this via the `plugin`
    check); the module-granular closed-world *assertion shape* is
    transferable as a SURV-001 deployment angle (§3). (T3 wave 3)

---

## §3 Complementary angles on existing backlog items (not new candidates)

Transfers that strengthen — not duplicate — the Phase-5 backlog (PHASES.md §7):

- **Devirtualization cascade.** .NET's guarded devirtualization uses an *inlined
  fallback* (branch to the virtual call) rather than deopt — cheaper when the guard is
  predictable-but-not-certain; V8/JSC use a mono→poly→megamorphic PIC ladder — the
  cascade's fallback chain should probably be a **ladder, not a cliff**. Truffle's
  production discipline: bounded PICs with explicit overflow policy + **splitting**
  (clone a callee per call-site context to keep PICs monomorphic) as the answer to
  megamorphic interface call sites. Go helps: interface method sets are small and
  statically known, so PIC limits can be tighter than for dynamic languages. (T2, T3)
- **Map specialization.** SURV-015 (fusion) and SURV-018 (slot caching) are access-path
  complements (representation vs access) — design them together. (T3)
- **Always-on profiling.** JSC's "inline caches *are* the profiler" (negative-cost
  profiling — the IC miss path records types with no extra instructions); PEP 659's
  counter policy (adaptive decrement, saturating counters, de-specialize on repeated
  failure) as the profiler→JIT gating policy; GCC's `-fprofile-values` perennial
  complaint (stale/missing training profiles) is exactly what the always-on profiler
  dissolves — file as the enabling argument for SURV-012. (T3, T5)
- **Speculative data representation.** V8's elements kinds are the precedent for
  *per-allocation-site* (not per-type) representation decisions with one-way
  generalization on violation. (T3)
- **Adaptive concurrency primitives.** Dynamic runtimes have no scheduler signals — our
  E2 (lock-holder-running, waiter counts, true idleness) is unique to us; JVM biased
  locking is not the model (see §2.1). (T3)
- **Small string optimization.** Swift's tagged 15-byte small strings are ABLE in
  production — use as the design reference, not a research item. (T3)
- **Partial escape analysis.** LLVM's `-sink` (move computations into the branches that
  use them) is the *scalar* version of the transform; Graal does the allocation version
  — our PEA should implement both, and `-sink`'s cost model (sink only if it doesn't
  duplicate into multiple hot successors) is directly reusable. (T5)
- **Vectorization.** SURV-008 (optimistic aliasing) is its *enabler* — keep in one
  design doc; SURV-010 is the versioning mechanism; SURV-038 is plan selection; JEP 417
  (Vector API, incubator) is the explicit-API alternative to auto-vectorization. (T2, T5)
- **Type narrowing + bit-packing.** SURV-020's niche-filling covers *invalid patterns*
  vs the backlog's *value ranges* — same family, distinct mechanism. (T5)
- **Idle-time repack/defrag/shrink.** SURV-024 (move elision) is its *constructive*
  twin: never copy in the first place. SURV-027 (pool allocation) is allocation-time
  scoping vs idle-time post-hoc — distinct. (T5, T1)
- **Inferred store-once / object inlining.** SURV-025's field-append interacts with
  field-affinity analysis (cf. backlog field reorder/pack — complementary). (T1)
- **Aggressive optimistic escape analysis (backlog).** TinyGo v0.34 models
  `string([]byte)` / `string([]rune)` conversions as non-capturing — conversion
  builtins must be modeled as non-capturing in our EA too (TinyGo's sweep found
  no EA precision beyond gc's; its edge is whole-program scope, supporting
  SURV-001's closed-world mode, not a new candidate). (T4 wave 2)
- **Escape analysis (backlog).** golang/go#75273 (FROZEN parameter-level
  `noescape` annotations) → complementary *signal*: annotations are facts,
  speculation is betting — the JIT can trust-but-verify annotations as an
  additional EA input behind a guard. (T4 wave 2)
- **Type narrowing + bit-packing (backlog).** golang/go#26680 (THINKING, low-bits
  interface type packing; the author was pessimistic, but the reported "savings
  are there" — the obstacle is Go's cultural aversion to bit-packing, which
  doesn't apply to our descriptor model) → SURV-020's design space gains the
  type-word low-bits channel. (T4 wave 2)
- **Aggressive optimistic EA / map specialization (backlog).** golang/go#64900
  (126 ns/op → 0.9 ns/op from eliminating interfaceboxing in a map-heavy path)
  → supporting evidence for SURV-032's specialized runtime paths. (T4 wave 2)
- **Map specialization (backlog).** golang/go#64703 → precedent for SURV-017
  (profile-observed pre-sizing). (T4 wave 2)
- **Devirtualization cascade (backlog).** golang/go#17133 (`x.Test` on concrete
  types through interface calls — "not clear whether such idioms can be
  rewritten" to avoid `convT64`) → motivating example for SURV-015 and the
  cascade's boundary-materialization elimination. (T4 wave 2)
- **Speculative devirtualization cascade (backlog) + SURV-009/SURV-033.** JSC's
  violation-bitset profiling discipline (S2.5: profile with the check on,
  speculate only on clean bitsets, first violation reinstates the check
  permanently) → the *profiling discipline behind* the speculation: absence of
  evidence is the arming signal, not just a fallback trigger. (T3 wave 2)
- **Devirtualization cascade (backlog).** .NET's GDV ladder policy (cap 5,
  CoreCLR default 1 / NativeAOT default 3, 75% chain gate, single-word guard +
  GDV diamond with direct-call fallback — no deopt) → production numbers for
  the cascade's fallback-chain design; NativeAOT's exact-class ladder (no
  fallback — one concrete type IS the class) supports SURV-001's hybrid
  framing: closed-world proofs get no-guard ladders. (T3 wave 2)
- **Backlog SURV-038 (SIMD codegen gap).** Stapelberg's TurboPFor/SIMD post
  (explicit-SIMD-API vs auto-vectorization; gc's index-rederivation gap is
  backend codegen our Graal backend handles) → SURV-038's framing. Boreham's
  loser-trees and Phil Pearl's interface-cost posts → supporting evidence for
  the devirt cascade (the concrete workloads devirt pays for). (T4 wave 2)
- **SURV-009 (arithmetic specialization).** Graal just shipped the *static*
  version of this transform for the unsigned domain (GR-78795: magic-number
  div/mod by *constant* in `OptimizeDivPhase`, GraalVM 25.4). Validates the
  transform's production value; fixes the *unsigned* sub-case, which
  SURV-009's writeup should explicitly include. Sharpens our edge: the
  frontier is no longer magic-number division per se but the
  *profile-constant* (not compile-time-constant) divisor behind a
  guard+deopt — territory a static compiler cannot reach. (T2 wave 3)
- **SURV-006 (priority inlining).** Two adjacent signals from Graal 25.x: (1)
  25.0.3 feeds call-target specialization *back into* the inliner — a
  feedback edge our adaptive-inlining policy should copy deliberately
  (speculation decision → re-run inlining budget assessment, not just arm a
  guard); (2) `DuplicationPhase` (GR-79029) graduated to community-default
  status — duplication-to-expose-optimization is becoming table-stakes, so
  our differentiation sits in *what the duplicates enable* (guarded
  specialization), not duplication itself. (T2 wave 3)
- **SURV-014 (cross-run speculation cache).** Leyden's *iterative training*
  (JEP 544 exploration piece: an AOT cache as input to a second training run)
  is the exact precedent for a **layered** cross-run cache: framework base
  cache from CI/nightly training + app-layer deltas from production runs,
  version-keyed per layer, composable rather than monolithic. Leyden's
  portability-vs-micro-arch tradeoff piece also suggests making the consumer
  mode developer-selectable (conservative/portable vs aggressive), not fixing
  one policy. (T2 wave 3)
- **SURV-022 (identity-freedom → flattened values).** Valhalla failure lesson:
  early community measurement shows `System.arraycopy` 8–20× slower than a
  plain loop on null-restricted flat value arrays — flattening a layout
  without updating every bulk primitive that touches it produces cliffs. The
  flattening transform must enumerate all bulk primitives (memmove/memclr, GC
  copy paths, write barriers) or SURV-022 stays prototype-gated behind a
  no-bulk-path-regression bar. (JEP 539 strict field initialization is the
  JVM's mechanism for reads-before-init protection; our analog is
  identity-freedom + definite-assignment → zero-init elision.) (T2 wave 3)
- **SURV-001 (closed-world AOT).** Crema (oracle/graal#11327, now verified
  THINKING) defines the open-world fallback ladder our hybrid mode (E5)
  should mirror for dynamic-loading edge cases: AOT → interpreter → (future)
  JIT for dynamically-loaded bytecode. The `plugin`-hole story stays, but
  the ladder is the product shape. Terminus (self-hosting Native Image) is
  architecture hygiene — no optimization transfer; recorded so future waves
  don't re-flag it. (T2 wave 3)
- **SURV-045 (speculative batched channel send).** #51553 comment-depth (T4
  wave 3): (a) the program owner's bulk-send runtime prototype hit **up to
  20×** uncontended (reporter's number) — the item is "prototyped, never
  submitted," not merely wished for; (b) mpx's scope narrowing — target
  *ongoing* batch transfers in hot loops, not fill-setup (the JIT versions
  only profiler-proven-hot loops, answering ianlancetaylor's frequency
  skepticism by construction); (c) robpike's "performance variability with
  data type" names the constraint the bulk path must satisfy (type-uniform
  cost model, no per-type performance cliffs), and the blocking-vs-non-
  blocking question is the observability boundary the guard defines: batch
  boundaries must never be observable to a concurrent receiver — batch only
  while exclusive access holds. No ATTEMPTED lesson (prototype never
  submitted).
- **SURV-058 (loop allocation batching).** #60879 (T4 wave 3): zero Go-team
  engagement in 3 years, but the single comment is a real-world frequency
  sighting (finnishtable; programmers hand-applying the unsound
  `&batch[i]` variant because the win is too large to ignore) — corroborates
  the "real pattern" claim beyond unmarshaling microbenchmarks. The issue's
  per-P bump-allocator alternative is raw/unvetted (fails bar leg (b)).
- **SURV-024 (move/copy elision).** Rust Project Goals 2026 include "MIR move
  elimination" (THINKING) — the copy-elision family (CopyProp,
  DeadStoreElimination, DestinationPropagation, RenameReturnPlace) is now an
  explicit rustc goal; our descriptor model could carry "this slot is dead
  after this move" facts to eliminate copies speculatively. Not a ranked
  candidate: gc's copy/NRVO behavior makes the marginal win unclear —
  prototype-gated spike. (T5 wave 3) **Wave-4 status:** the goal is now
  *Accepted* for 2026 (rust-lang/goals#396; champions Amanieu/lang,
  RalfJung/opsem, dianqk), RFC #3943 open under active review (the pass is
  "technically unsound" pending the RFC's new `move` semantics per Amanieu,
  2026-06-22), prototype #157943 in flight behind `-Z mir-move-elimination`
  (split into 7 PRs; #163336 TailCopyToMove merged 2026-09-30 — rustc master
  HEAD `4ddbc06`, position #36 after DestinationPropagation; remaining PRs
  #163335/59/37/38/39/40 pending; Miri work uncovered semantics issues).
  Try-perf aggregates: instruction-count −0.3…−0.7%, cycles −2.2…−2.5%,
  bootstrap −3.2% (their numbers, with sharp per-benchmark regressions —
  real but young). **Transfer ruling (wave 4):** the *static* soundness core
  does NOT transfer — Go has no borrow-checker-style single-ownership proof
  (§5 negative); only the guarded speculative variant (SURV-024's E3 arm) is
  viable, keyed on escape facts (address-taken + escaping via
  interface/closure/unsafe/reflect vetoes elision). Rust's frequency argument
  is partly idiom-driven (builder/`Box::new` patterns); in Go the fat part is
  the `copy`/`append` builtins, not general struct unification.
  **Prototype-gated spike designed** (`track-rustgoals-wave4.md` §3):
  Stage-0 headroom measurement first (dead-source-unique fraction of copied
  bytes; KILL if <10% or headroom <1% of wall time), then a guarded prototype
  on JIT descriptor facts + guard framework vs gc+PGO (append-growth,
  `copy`-dies microbench, one real workload; 5 runs median+IQR; >2% win bar);
  kill triggers: Stage-0 headroom, no-win, guard-cost >50% of win, and an
  adversarial soundness suite (unsafe/reflect/closure — the #157943
  rustlantis lesson ported in).
- **SURV-003 (scoped arenas).** Rust's `Allocator` trait stabilized Sep 2026
  (rust-lang/rust#156882, MVP; remainder → `allocator_ext`) — THINKING→ABLE in
  progress. Go has no allocator hooks, so our arenas stay JIT-internal, but
  Rust's stabilized shape (Layout-parameterized alloc/dealloc, fallible
  allocation via `Result` instead of OOM-panic, `Allocator` superseding
  `GlobalAlloc`) is the reference design for our internal arena ABI; validates
  the industry direction. (T5 wave 3)
- **Backlog devirt cascade + SURV-001/SURV-004.** LLVM runs IPSCCP +
  called-value-propagation + globalopt *before* the inliner (PassBuilder
  comment: IPSCCP "opens opportunities for globalopt and inlining") — our
  analog: run a cheap whole-unit constant/type-fact propagation to resolve
  monomorphic interface targets *before* inline budgeting, so the budget is
  spent on already-direct calls. Ordering detail, not a new candidate.
  (T5 wave 3)
- **Backlog loop specialization.** LLVM's scalar sandwich (canonicalize → loop
  transforms → re-canonicalize → loop idiom/indvars → scalar again), with
  vectorization deferred to a *separate later phase* — our single-compile JIT
  should structure explicit sub-phases and schedule speculative loop
  specialization (guard insertion for trip-count/alignment) in the
  post-inline phase where scalar facts are settled, not interleaved with
  inlining. (T5 wave 3)
- **SURV-006 (priority inlining).** LLVM's O2/O3 delta shows
  `ArgumentPromotionPass` gated at O3 with an upstream FIXME questioning the
  gating — promote scalarizable args *at inline time* in our JIT rather than
  as a separate level-gated phase. LLVM Discourse Jun-2024 RFC on
  alwaysinline legality (don't inline when the result is broken code) is the
  same evidence class: our JIT inliner needs a legality gate against
  callee-body facts the frontend can't see. (T5 wave 3)
- **Backlog vectorization.** VPlan's "plan first, transform second"
  architecture (active, still transitional — standalone cost model not done,
  outer-loop vectorization experimental): build 2–3 *speculative* vector
  plans per hot loop, cost them with profiler data (trip-count/alignment
  distributions) instead of a static cost model, guard-and-execute the
  winner, deopt on drift. We're ahead of LLVM's static position here, not
  behind it. (T5 wave 3)
- **SURV-053 (no-evidence → generic).** CPython's confidence-product gate
  (`CONFIDENCE_CUTOFF 333/1000`; per-branch confidence multiplied by
  `bitcount/16`) is the quantitative form of "no-evidence → generic": gate
  *region formation* on the joint confidence, gate *site speculation* on
  evidence existence. (T3 wave 3)
- **SURV-052 (miss-against-own-cache → downgrade).** pyston_lite's counted
  failure caches (thresholds 2–15 by site kind, forgiveness resets on
  success, JIT fast path requires `num_failed == 0`) are the policy numbers
  for the downgrade rule — not a single-strike rule. (T3 wave 3)
- **SURV-004 (interface devirtualization).** Pyston `CallMethodHint`'s one
  check at the top over a `(type, attr, meth)` hint chain — hoist the guard
  to the region entry instead of guarding each call. ZJIT's inline knobs
  (threshold 30 / budget 200 / depth 10) as calibration data for
  guard+inline budgets. (T3 wave 3)
- **SURV-001 (closed-world devirt).** Swift's whole-module CHA-driven
  `Devirtualizer.cpp` + `GenericSpecializer`/`EagerSpecializer`
  prespecialization — the AOT whole-module mechanism shape for the
  closed-world half. (T3 wave 3)
- **SURV-055 (guard-failure budgets).** RaptorJIT's per-pc doubling penalty
  cache (72 → 6M, 64 slots) + 1s wall-clock hotcount decay — a concrete
  instantiation of the failure-budget/decay design with wall-clock rather
  than GC cadence. (T3 wave 3)
- **Cross-runtime convergence (policy calibration).** Dominance threshold
  for "speculate on the winner" lands at **≥75%** independently in three
  runtimes (ZJIT `SKEW_THRESHOLD=0.75`, .NET GDV chain gate 75%, JSC 2:1
  double-vote); polymorphism caps land at **4–6** (V8: 4, .NET: 5, HHVM
  call checks: 6). Use as priors for SURV-052/SURV-070 thresholds, not as
  copied constants. Guard-failure response ladder is now fully mapped (V8
  megamorphic; JSC kinded exit + doubling backoff; PyPy bridge @200 +
  one-shot retrace; RaptorJIT side trace then self-link blacklist; CPython
  chain depth 4 + progress proof; HHVM guard *widening*) — widen-vs-backoff
  is the open design choice, and SURV-069 picks widening. (T3 wave 3)
- **SURV-009 / SURV-033 (arithmetic specialization).** RaptorJIT's narrowing
  rationale (`lj_opt_narrow.c`): never narrow general arithmetic eagerly —
  it turns a data-flow dependency into data-flow + control and is
  counter-productive on superscalar CPUs; narrow only where demand-driven
  or predictive (index expressions, induction variables, bit ops), with a
  bounded backprop budget (`NARROW_MAX_BACKPROP=100`). The SURV-009 sketch
  must answer this counter-consideration. (T3 wave 3)
- **SURV-055 (guard-failure budgets).** RaptorJIT's full `JIT_PARAMDEF`
  numbers: hotexit=10 → side compilation, tryside=4 attempts, 1s wall-clock
  evidence decay (`hotcount_decay` resets all hotcounts every second of wall
  clock — phase-change adaptation on wall-clock time, not GC cadence),
  maxside=1000 variants per root, SNAPCOUNT_DONE saturation. Wall-clock
  (vs GC-event) decay is the datum the policy table didn't already name.
  (T3 wave 3)
- **SURV-070 / devirt cascade (ladder ordering).** Swift's
  `Devirtualize.cpp` ordering rule: test the exact static type first, then
  breadth-first over the hierarchy; profile-ordered in the PGO case —
  converges with .NET's 75% chain gate and HHVM's `PGORelaxPercent`. The
  ladder-ordering policy no existing candidate specifies. (T3 wave 3)
- **SURV-067 (bounded-tolerance guards).** Swift's `LetPropertiesOpts` is
  the *static* sibling: where closed-world analysis proves write-once-with-
  constant (package-level `var` with a single initializer, struct fields
  assigned once in the constructor), fold loads guard-free; reserve
  SURV-067's mutation-counter guards for the unprovable rest. Note its
  soundness caveat (layout exposure defeating "all initializers seen") as
  a warning for our own closed-world proofs. (T3 wave 3)
- **C1/C4/P1 (interface-dispatch and map-slot ICs).** TruffleRuby's policy
  discipline: per-*kind* IC limits (dispatch vs method-lookup vs yield —
  different site kinds have different guard costs and hit rates) rather
  than one global cap; `@InliningCutoff` on the megamorphic fallback so the
  slow path is fenced off from the inliner. Both apply directly. (T3 wave 3)
- **C7/SURV-037 (phase-adaptive rejit).** Maglev's Turbolev mode: Maglev's
  fast graph builder + speculatively-optimized IR is reused as the
  *frontend* for the Turboshaft optimizing backend — speculation decisions
  made once, shared across tiers instead of re-derived. For JIT tiering: a
  single speculation frontend feeding both the quick and deep tiers keeps
  guard placement consistent and amortizes profiling cost. (T3 wave 3)
- **SURV-001 (closed-world AOT).** Sorbet's per-file `# compiled: true`
  opt-in as a module-granular closed-world assertion shape — weaker
  assertion, smaller blast radius, deployable incrementally; consider a
  per-package flag form of SURV-001. (T3 wave 3)
- **SURV-005 / SURV-006 / SURV-010 / SURV-013 / SURV-014 (AOT profile
  consumers).** GraalNN (CGO'25, WOULD-LIKE → SURV-075) is the day-0 static
  profile source the E5 pure-AOT mode was missing: ML-inferred
  control-split branch probabilities feed SURV-005's layout, SURV-006's
  inlining simulation, SURV-010/SURV-013's never-taken-branch picking, and
  are the static floor under SURV-014's cross-run cache before it warms.
  Sharp divider for the survey: GraalNN is *statically generalizable*
  (trained once, ships with the compiler) vs Basso's *training-run-dependent*
  startup layout — the two AOT profile-source shapes. (wave 4, CGO'25 pair)
- **SURV-023 (lock coarsening).** Graal 25.5's GR-79526 extension (coarsen
  across simple control flow + eliminate nested locking of the same object)
  now covers exactly SURV-023's stated scope — Graal has converged on the
  design; SURV-023's remaining edge is the goroutine-mutex/gc-specific
  context (lock fast path, contention profiles), not the CFG shape. File as
  convergence confirmation, not expansion. (wave 4, T2 re-sweep)
- **SURV-008 (optimistic aliasing).** Graal 25.5 keeps investing in the
  load/store-elimination frontier (GR-79594 write sinking; GR-79585
  loop-carried reads → value phis) — adjacent confirmation, nothing to
  transfer beyond SURV-008's existing scope. (wave 4, T2 re-sweep)
- **SURV-009 (arithmetic specialization).** GR-79031 (exact-arithmetic
  optimization via loop-limit deopt) is Graal's *static-exactness* version;
  our edge remains the *profile-driven* variant — file the deopt-backed
  exactness shape as the mechanism to copy if profiling ever fails to fire.
  (wave 4, T2 re-sweep)
- **Backlog JIT pipeline design (§4.11 correction).** rustc's 2022 two-list
  tiering (`mir_opt_level > 0` → full list) is GONE: tiering is now per-pass
  `policy()` objects with `mir_opt_level` 1 at `-Copt-level=0`, 2 otherwise;
  `DataflowConstProp` needs ≥3 (absent at default `-O2`), `MultipleReturn
  Terminators` needs ≥4 (never by default). §4.11's transfer advice stands
  but should cite the policy-object pattern, not the list pattern — and the
  "named pass vs actually-running pass" surprise is a design constraint:
  name every tier boundary as an auditable list of *what actually runs*.
  (wave 4, T5 verification)
- **SURV-006 (priority inlining) / devirt cascade.** LLVM's arg-promotion at
  O3 runs *inside* the `devirt<4>` repeater (PassBuilderPipelines.cpp
  @`5e1ed67`, lines 1034–1038) — devirt-triggered inlining iterations
  re-promote args each round; our devirt-triggered re-pipelining (SURV-065)
  should re-run its arg-promotion step the same way. (wave 4, T5
  verification)
- **SURV-070 (skewed-dispatch speculation) — assertion-site sibling (spun off
  from the SURV-077 vetting).** At hot iface→iface assertion/conversion sites
  `x.(I2)` with a skewed dynamic-type profile (e.g. `fmt`'s per-operand
  `Formatter`/`Stringer` checks, `errors.As` traversals), guard
  `dynamic-type(x) == T0` with one type-word compare; on hit load the
  precomputed constant itab(T0, I2), skipping gc's inline cache probe (atomic
  load + hash loop, ~order-10 cycles per hit — author's estimate, not
  measured) and the `getitab` path; on miss fall back to the existing probe
  path (diamond, no deopt strictly required). This is profile-guided
  concrete-type speculation — the devirt lane, NOT SURV-077's declared-type
  pattern. No site overlap with SURV-070 (assertion sites vs call sites);
  shared guard-budget infra at most (coordinate with SURV-055/SURV-070 if
  pursued). Filed as an angle, not a new ID: quantitative hotness is
  unverified and the per-site win is estimated-small — prototype-gated at
  best. (wave 5, SURV-077 vetting)
- **SURV-024 (move/copy elision) — priors refresh.** #163336 TailCopyToMove
  stays merged (no revert); #163338 MoveElimination still OPEN, gated on
  #163337 (under active review through 2026-10-03); #163335/59/39/40 also
  open. New try-perf numbers on #163359 (ZST-prep PR, their claims): instr
  −0.4% all-primary (31 improved −0.6% mean vs 5 regressed +1.0% mean),
  cycles 0, RSS −0.6%, binary −0.9%, bootstrap +0.10% (noise) — mildly
  strengthen wave-4's "real but young" characterization; no newer run on
  #157943 itself. goals#396 still Accepted; RFC #3943 still Open (RalfJung
  engaging on #163340 tail-call semantics — the "technically unsound"
  caveat stands). **Transfer ruling unchanged:** §5 static-core strike
  stands; only the guarded speculative arm (E3) is viable. Wave-6 trip
  conditions: #163338 merges, a try-perf run lands on #163338, or RFC #3943
  is accepted. (wave 5, #157943 watch)
- **SURV-075 — protocol is the new state of the open measurement.** See the
  wave-5 update on SURV-075's entry: three-stage protocol with armed kill
  triggers K1–K4 and a promotion bar (≥1.5% M2 geomean, no shape >1pp worse).
  Stage 0 is gc-only and constructible today — executing it is a
  program-owner decision (prototype-adjacent measurement work, not survey
  work). (wave 5, SURV-075 protocol)
- **SURV-006 — GraalCC verdict is negative.** See SURV-075's wave-5 update:
  the XGBoost call-count profiler is docs-only and not extractable, and
  per-method binary cold/hot is too coarse for SURV-006's per-call-site
  inlining simulation regardless. (wave 5, SURV-075 protocol)
- **SURV-061 (value-field inlining / Valhalla flattening line).** EA report
  (JVM Weekly vol. 192, **source claim**) of `System.arraycopy` running
  8–20× slower than a plain loop on null-restricted flat value arrays —
  watch context for the flattening line: JVMs are still finding performance
  cliffs in flat-layout primitives, which is exactly where our flattening
  assumptions need re-validation when the JVM side moves. (wave 5, T2
  re-sweep)
- **Leyden JEP 544 — Candidate → Targeted for JDK 28.** Direct fetch of
  openjdk.org/jeps/544 (2026-10-04): Status=Targeted, Release=28, Owner=John
  Rose; transition happened within ~the last 10 days (InfoWorld ~10 days old
  still said "proposed"), well ahead of the expected Dec 2026–Jan 2027
  ramp-down window. Wave-4's Proposed→Target trigger is satisfied; the new
  watch is ramp-down survival to 28 GA (~March 2027). No new technique — file
  as a status movement, not a candidate. (wave 5, T2 re-sweep)
- **Leyden JEP 544 — wave-6 direct re-verification BLOCKED (check limitation).**
  Wave 6 could not re-fetch openjdk.org/jeps/544 directly (HTTP 403,
  tool-level block; not retried through another path — recorded as a gap,
  not as a fact). Last directly-verified state stands at wave 5
  (Targeted, Release 28). Fresh secondary sources (InfoWorld, JVM Weekly
  vol. 192, LavX) show **no Targeted→Integrated movement and no ramp-down
  notes**. The wave-7 worker re-verifies the JEP page directly; the
  ramp-down survival watch (Dec 2026–Jan 2027 ramp-down, GA ~Mar 2027)
  is unchanged. (wave 6, T2 re-sweep)
- **Azul Cloud Native Compiler — watch note only (T2).** Azul's Oct-1
  announcement claims Cloud Native Compiler delivers 2x–5x faster warm-up
  vs standard OpenJDK by sharing fully optimized compiled code across a
  JVM fleet (**their claim, unverified**). Pre-existing product capability,
  no published technique disclosed — there is no transferable mechanism
  to evaluate, so this is a watch note, not a candidate. Watch trigger for
  later waves: any published technique or independent measurement. (wave 6,
  T2 re-sweep)
- **SURV-070 assertion-site sibling — hotness now MEASURED, not
  bar-clearing (no new ID).** Wave-6 measurement (Go 1.27.1, linux/amd64,
  AMD EPYC 9D25 — our numbers): assertion sites ARE hot in fmt/error
  shapes (`fmt.(*pp).printArg` 33.05% cum, `errors.as` 27.12% cum), but
  the itab-materialization residual is sub-1% (`runtime.getitab` 0.85%
  cum) — the sites are hot, the probe path is not. A monomorphic
  `x.(narrowIface)` site costs **4.78 ns/op** on gc's per-site inline-cache
  hit path; the dominant-type guard would replace the site-cache probe
  (atomic load + hash&mask) with a type-word compare + constant itab load —
  saving at most a couple of cycles on the dominant path, and only with
  profile infra (E4) + guard-budget coordination (SURV-055/SURV-070) in
  place. The cache-thrash case (3 types rotating: 10.94 ns/op,
  typeAssert+getitab ≈ 20% of site samples) is exactly the case a
  dominant-profile guard does NOT help. The §3 angle stands with hotness
  measured; re-open only if a workload shows dominant-profile assertion
  sites >5% of wall time (none found). (wave 6, C4 hotness verification)
- **SURV-024 (#157943) — 0/3 trip conditions fired; ruling unchanged.**
  #163338 still OPEN (gated on #163337, `S-waiting-on-author`, last
  activity author's 2026-09-30 force-push); no try-perf run on #163338
  (timeline has exactly one comment — the author's 2026-09-25 LLM
  disclosure); RFC #3943 still Open (newest review comment 2026-09-30;
  the 2026-10-02 `updated_at` bump is review-churn; no FCP). Siblings
  unchanged (#163336 stays merged; #163337/35/39/40 open). The #163359
  try-perf numbers (their claims: instr −0.4%, RSS −0.6%, binary −0.9%)
  remain the newest data, already ingested in wave 5. §5 static-core
  strike stands; guarded-speculative arm (E3) only. Method note for wave 7:
  GitHub API state/timeline endpoints proved faster and more precise than
  page fetches for these binary checks. (wave 6, #157943 watch)
- **SURV-075 — Stage-0 execution decision still pending.** Wave 6
  re-checked memory, the survey dir, and goal files for a program-owner
  decision to execute the designed gc-only protocol: none recorded. The
  protocol (K1–K4 armed, ≥1.5% M2 geomean bar) remains designed and
  constructible; execution is not survey work. (wave 6, owner-decision
  check)
- **T3/T2/T4 wave-6 negatives are all negative.** 0/6 standing T3 triggers
  fired (Pyston pyston_main still Aug 12 2024 / "(No longer maintained)";
  TruffleRuby Options.java has no new speculat/guard option; GraalPy
  CHANGELOG latest commit Aug 18 2026, §25.4 unchanged vs pin 178ea261 —
  wave-7 method refinement: check BOTH file-history and raw-vs-pin);
  Graal 25.x changelog byte-identical (25.5 tops, no new EA/inlining
  material); JEP 401 unchanged (Integrated, Release 28); OpenJ9 v0.62.0
  housekeeping only; #24314 static (updated 2022-07-13, 8 comments);
  #77893's five CLs all still NEW (SURV-041's gap does not narrow);
  label-probe watches (#81887, #81846/#81755, #81957, #81968) zero
  activity. (wave 6, re-sweep)

---

## §4 Enabling infrastructure (not ranked; required by candidates)

1. **Assumption-dependency / invalidation framework.** V8 tracks "dependent code" per
   assumption and deoptimizes all of it when a prototype/map changes; .NET rejits "when
   new interface implementations arrive on scene". We need the same registry: every
   speculative compilation records its assumptions (func-var target, value facts, branch
   facts, type-arg facts); any violation invalidates exactly the dependent set. Required
   by SURV-004/009/013/016; SURV-037's rejit is the policy on top. CPython's tier-2
   gives the concrete lossy design: bloom-filter dependency sets per executor
   (`_Py_BloomFilter_Add` on each depended-on code object;
   `_Py_Executors_InvalidateDependency` drops every executor whose filter *may*
   contain the mutated object) — over-invalidation is the safe direction, O(1)
   dependency recording. (T3; T3 wave 3)
   *Invalidation-action note:* the default action is deopt of the dependent
   set; SURV-073 (Tier 4, ZJIT `invariants.rs`) offers the cheaper
   alternative — patch the guard site in place to `jmp` the fallback,
   keeping the compiled version. Use it where the invalidated assumption
   is rare (profiler attach/detach, `GODEBUG` flips, plugin loads).
2. **OSR-exit machinery.** V8, JSC, .NET, and Pyston (backedge-trip-count OSR entries)
   all have it; it is what makes speculation *sound without* keeping fallback paths in
   hot code. Required by SURV-013/037 (and the loop-backedge OSR parity case). (T3)
3. **Cross-run cache format** (SURV-014): store *speculation decisions + profiles*,
   not code — composable underneath a future Leyden-style code cache; forward-compatible
   with ingesting a Leyden-style training run as one more profile source. (T2)
4. **Deopt-budget discipline.** arXiv:2603.06551 (2026), "Understanding and Finding JIT
   Compiler Performance Bugs", finds speculation/deopt failures are ~28.8% of JIT
   performance bugs — budget speculation accordingly. Tiered deopt targets (deopt to a
   *less-optimized compiled tier*, not all the way to baseline — Hölzle, Chambers &
   Ungar, *PLDI* 1992) as a mitigation design constraint, not a candidate. (T1)
5. **Invalidation-registry shape.** V8's CompilationDependencies registry (S1.6) +
   PyPy's quasiimmut mechanism (S4.5): every assumption carries its own dependency
   list; invalidation walks only the dependents. For Go the granularity must cover
   *representation* facts (field constness, map B), not just values. Supporting
   evidence for the §4 assumption-dependency framework. (T3 wave 2)
6. **Profiler liveness gating (E4 design requirements).** JSC:
   desiredProfileLivenessRate 0.75 / fullness 0.35 (S2.7) — gate tier-up/rejit on
   profile freshness, not just counter magnitude. .NET warnings (S3.6): profile
   loop bodies via OSR-entry points (the hottest code is otherwise invisible to
   method-entry probes), degrade by *skipping sites* when budgets exhaust (never
   degrading the site), treat phase changes as first-class. (T3 wave 2)
7. **Training-data inspectability (SURV-014 design requirement).** Leyden's
   "inspectability of training data" exploration piece (JEP 544, Jan 2026) maps
   to a requirement: the cross-run cache must ship with a dump/inspect tool
   (what was cached, from which run, which decisions were invalidated and
   why) — without it, a misbehaving speculation cache is undebuggable. (T2
   wave 3)
8. **Deopt-safe frame-thaw discipline (E2/E3 interplay).** JDK-8377715: thawing
   a virtual-thread frame could undo a deopt (fixed JDK 27, April 2026) — the
   same hazard class exists for goroutine frame migration across deopt points
   in our user-space scheduler. Deopt + scheduler interplay needs an explicit
   correctness protocol, not just an optimization design. (T2 wave 3)
9. **Late atomic lowering of high-level control (rustc R2).** Keep
   defer/panic paths and goroutine spawn points as first-class terminators
   *through* optimization; lower them in a late, dedicated, atomic phase
   (rustc's `ElaborateDrops` runs post-borrowck with its surrounding passes
   "executed together"; optimizations run on pre-elaboration MIR). Optimizing
   half-lowered control flow is where miscompiles hide. (T5 wave 3)
10. **Bracketed structure-destroying lowerings (rustc R3).** Any lowering that
   destroys high-level structure (closure/goroutine-entry reshaping) gets the
   rustc `StateTransform` treatment: cheap opts before, full opts after —
   not an unlabeled middle of the pipeline. (T5 wave 3) **Wave-4 correction:**
   rustc's `StateTransform` has *lost* its optimization bracket (master HEAD
   `4ddbc06`: the 2022 pre-opts are gone; `Inline` now runs after it in the
   main pipeline) — the R3 precedent describes a structure that no longer
   exists. The underlying principle (don't optimize half-lowered control
   flow) still holds via R2 (§4.9); cite R2, not R3. (wave 4, T5
   verification)
11. **Named tier boundaries (rustc R4).** The JIT's baseline tier runs a
   correctness-floor subset (canonicalization + validation); the optimizing
   tier runs the full list — and the boundary is a named, reviewable list
   (rustc's `mir_opt_level == 0` subset + always-run `pre_codegen_cleanup`),
   not an emergent property of flag checks. (T5 wave 3) **Wave-4 correction:**
   rustc's mechanism changed from a hardcoded two-list to per-pass
   `policy()` objects (`mir_opt_level` 1 at `-Copt-level=0`, 2 otherwise;
   `DataflowConstProp` ≥3, `MultipleReturnTerminators` ≥4). Cite the
   policy-object pattern, not the list pattern; and treat "named pass vs
   actually-running pass" as a design constraint — name every tier boundary
   as an auditable list of *what actually runs*. (wave 4, T5 verification)
12. **Guard-fact materialization at phase boundaries (LLVM T4 + rustc R5).** A
   dedicated JIT step converts guard-derived facts (observed type sets, value
   ranges, alignment, non-nil) into optimizer-visible form immediately before
   the phases that consume them (loop opts, devirt, inlining), with a matching
   cleanup dropping facts invalidated by deopt — the *scheduling* half of the
   §4 assumption-dependency framework (LLVM: `constraint-elimination` before
   loop opts, `drop-unnecessary-assumes` before vectorize; rustc re-runs named
   pass instances like `SimplifyCfg("after-const-prop")` at pipeline points).
   (T5 wave 3)
13. **Unsafe-point GC contract decision (#15397 design note).** aclements'
   mechanism for ephemeral-slicing guards — compiler-marked *unsafe points*
   (1–2-instruction windows where a manufactured pointer may be live but the
   GC promises not to look) — is a GC-subsystem contract decision for graalgo,
   not a Phase-5 candidate. gc chose "safe points everywhere," permanently
   closing the cheap route; our GC design must decide whether marked unsafe
   points exist. Adjacent #24314 (write-barrier omission on self-subslice)
   remains open and unowned — wave-4 watch. (T4 wave 3)
14. **Fast-tier bailout discipline (Maglev).** A fast JIT tier must *refuse*
    to compile what it can't compile well (Maglev's `BuildAbort` early bailout
    on unsupported bytecodes) rather than emit slow generic code — the tier's
    compile-time budget is the constraint, not peak code quality. If graalgo
    ever grows a fast JIT tier (E5), this is its bailout-list design; the
    Turbolev shape (fast graph-builder frontend feeding the optimizing
    backend) reuses speculation work across tiers. (T3 wave 3)
15. **AOT profile-source duality (GraalNN static vs training-run-dependent).**
    The E5 pure-AOT mode needs a day-0 profile source, and there are exactly
    two shapes: *statically generalizable* (GraalNN — trained once, ships
    with the compiler; branch probabilities for control splits; auto-disabled
    when real `--pgo` is given) and *training-run-dependent* (per-program
    instrumented run → trace → optimized build; staleness risk). Design
    requirement: the AOT pipeline must accept both as *one* profile-source
    interface (SURV-014's cross-run cache is the natural sink), with the
    static source as the floor and training-run profiles overriding where
    fresh. Required by SURV-075 (static source), SURV-005/SURV-006/SURV-010/
    SURV-013 (consumers), SURV-014 (cache). (wave 4, CGO'25 pair)
16. **Cold-start binary layout (Basso).** First-use-order section layout
    (CU-level `.text` ordering + pre-init-data ordering) is cold-start-only
    enabling infrastructure: it must be a *gated layout variant*, never the
    default, because startup-optimal layout is steady-state-pessimal (the
    paper's own Havlak 0.97–0.98× regress). Go simplification vs the paper:
    deterministic builds → encounter-order object mapping suffices, no
    heap-path ID machinery. Required by SURV-076. (wave 4, CGO'25 pair)

---

## §5 Surveyed, nothing applicable (negative results — first-class)

*Consolidated across tracks so nobody re-surveys. Each names what was surveyed and why
it doesn't transfer.*

**From T1 (academic):**
- Trace-based JIT type specialization (Gal et al.-style, 2009): specializes on *type
  uncertainty*, which Go doesn't have — no transfer story.
- ABCD-style bounds-check elimination (Bodik, Gupta & Sarkar, *PLDI* 2000): gc already
  does BCE — excluded by brief.
- Region-based *compilation* units (Suganuma, Yasue & Nakatani, *PLDI* 2003) and adaptive
  recompilation with OSR (Fink & Qian, *CGO* 2003): compilation infrastructure, not
  benchmark-winning optimizations vs gc+PGO.
- Classical scalar optimizations (value numbering, PRE, LCM): gc does them; no academic
  delta to exploit.
- Hardware value prediction / runahead: hardware techniques, not compiler optimizations
  for this project.
- Deegen (Xu & Kjolstad, arXiv:2411.11469, 2024): VM-generator for dynamic languages —
  dynamic-runtime track's remit.
- Object combining (Veldema et al., keeps the pointer): strictly weaker than SURV-025's
  inlining; folded into its precedent discussion.
- Cyclone-style *language-level* regions (Grossman et al., *PLDI* 2002): requires
  source annotations — rejected as a language change; only its automatic/speculative
  descendant (SURV-027) is proposed.
- Hot/cold field *splitting* of structs (indirection for cold fields): adjacent to
  backlog field reorder/pack but no clean academic precedent with measured numbers —
  fails the evidence bar; complementary angle at most.
- Semi-manual annotation-driven AoS→SoA (Radtke & Weinzierl, arXiv:2405.12507, 2024):
  backlog already covers *speculative* AoS→SoA; the annotation angle is complementary
  at most.
- **Forward-citation sweep wave 3 (dead research lines in managed runtimes,
  2016–2026 citing papers):** Chilimbi&Larus ISMM'98 (GC-time cache-conscious
  placement — zero managed-runtime citers; the line continues only in C/C++
  allocators); Artigas et al. ICS'00 (zero managed-JIT citers; survives only as
  SURV-010 / LLVM polyhedral); Adl-Tabatabai et al. PLDI'04 (hardware-monitor
  prefetch — zero citers; literature moved to hardware prefetchers); Luk&Mowry
  '96 (same); Seidl&Zorn ASPLOS'98 (no direct 2016+ citer — live branches are
  Memento Mori ISMM'15 (ABLE, filed under SURV-017/E4) and GoFree CGO'25).
  SURV-029/SURV-039 stand on their original precedents. (T1 wave 3)
- **Memento Mori pretransitioning for Go — near-miss:** representation
  transitions are dynamic-language-specific; the Go analog is subsumed by the
  backlog's speculative data representation. Presizing → SURV-017's ABLE
  precedent; pretenuring → complementary angle on SURV-026; the memento
  *mechanism* → E4-sensor idea for §4. (T1 wave 3)

**From T2 (JVMs):**
- C2 flow-insensitive EA (Choi99): subsumed by gc's EA + backlog partial-EA.
- Range-check smearing / .NET loop cloning / range-analysis repertoire: subsumed by gc's
  BCE (steal .NET's proof checklist only).
- Loop strip mining (C2, Graal `StripMineCountedLoops`): optimizes safepoint polls; no
  Go analog cost — near-miss.
- Lightweight locking / JEP 491 unpinning: Go already equivalent (mutex fast path;
  goroutines don't pin) — nothing to transfer.
- G1/ZGC/Shenandoah/C4/OpenJ9-balanced barrier & policy designs: GC subsystem, not
  per-program Phase-5 optimizations.
- ZGC colored pointers: address-space tricks incompatible with Go's runtime model.
- JITServer / Azul Optimizer Hub remote JIT: changes where compilation happens, not code
  quality — near-miss.
- `-Xtune:virtualized`: utilization policy, no named-benchmark win vs gc+PGO.
- Azul Falcon as architecture: validates speculation + strong mid-end, not a
  per-benchmark candidate.
- .NET NativeAOT constraints: validate our three execution modes; not a candidate.
- Graal loop inversion (≤30% microbench — vendor claim): loop-shape transform, subsumed
  by the vectorization track.
- Superword SLP: vectorization-track process notes only.
- Crema / Terminus: **no verifiable public record found** (web search 2026-10-04) — do
  not cite; re-check in a later wave. **Update (wave 3, 2026-10-04): both verified.**
  Crema → THINKING (oracle/graal#11327 + crema-onboarding.md; JIT-for-dynamic-bytecode
  is the stated future; contributes the §3 open-world ladder angle on SURV-001);
  Terminus → THINKING (oracle/graal#12236 + in-tree docs; self-hosting architecture
  hygiene, no optimization transfer). The re-check queue item is retired.
- **Graal 25.0.3–25.0.4 re-sweep (wave 3): no new EA capability** (the PEA change
  was fixpoint-efficiency only — compile-time scalability, not a new capability),
  no new speculation policy; only the specialization→inlining feedback edge and
  GR-78795/GR-79029 (filed as §3 angles on SURV-006/SURV-009).
- **Leyden JEP 544 (wave 3):** still Candidate for JDK 28 (not Proposed-to-Target);
  no successor number; no shipped condenser APIs beyond SURV-014's existing
  coverage. Remains THINKING.
- **Valhalla JEP 401 (wave 3):** no 2026 movement on primitive classes / unified
  primitives; the arraycopy-on-flat-arrays 8–20× regression is a flattening
  failure lesson (filed on SURV-022), not a candidate.
- **TruffleRuby/GraalPy (wave 3, project level):** no JIT/speculation features in
  2026 — language work is compatibility, native C-API interop, platform support.
  Deferred to W4's source passes for anything below changelog granularity.

**From T3 (dynamic runtimes):**
- V8 hidden classes / transition trees: solve the dynamic-shape problem; Go field
  layout is static. Nothing to transfer.
- V8 elements-kinds lattice (PACKED_SMI → PACKED_DOUBLE → HOLEY → DICTIONARY): Go
  slice element types are static; no holes, no dictionary mode. Transferable sliver
  (per-allocation-site sizing) is SURV-017.
- V8 Smi tagging as the default integer representation: Go integers are already unboxed
  machine words; the `any`-payload sliver is the backlog's unboxing item.
- V8 pointer compression / Orinoco GC internals: GC and memory-encoding design, not
  speculation.
- Cinder Static Python boundary checks: "checks at the boundary, raw offsets inside"
  assumes a dynamic interior; Go is statically typed — no dynamic boundary to verify.
- Swift CoW for large Go array values: solves a problem Go's slice model mostly avoids
  (slices already share); large arrays passed by value are rare in idiomatic Go. The
  *principle* (representation ≠ semantics) feeds E1's descriptor design instead.
- Swift `_read`/`_modify` coroutine accessors as language features: Go needs no
  language change (SURV-015 does it as a JIT intrinsic).
- Sparkplug/Maglev-style baseline tiers as a throughput story: warmup/startup latency
  only; cannot beat AOT gc on throughput.
- Copy-and-patch / DynASM template JIT (CPython 3.13+, LuaJIT): compile-*latency*
  technique; worse code than an optimizing backend by design.
- JSC-style polyvariant inlining as a standalone claim: marginal vs gc+PGO inlining;
  the valuable part (different *value* assumptions per version) is SURV-013/016.
- HHVM region-based JIT compilation: ≈ Graal's inlining scope; no distinct transfer
  beyond what Truffle/Graal already does.
- .NET deferred compilation: warmup-only; no steady-state claim.
- Type-switch monomorphic fast path: gc's type-switch lowering is already decent;
  speculative version adds only branch-ordering + I-cache effects over gc+PGO —
  marginal; struck as standalone.
- Pyston-style runtime-IR inlining (embed runtime IR in the binary): overlaps backlog
  adaptive-concurrency and map-specialization items; the distinct sliver (inlining
  `growslice`/map helpers with caller profile context) has no clean standalone
  benchmark. Revisit if those backlog items need it.
- YJIT-style C-method inlining for runtime helpers: gc already intrinsifies the hot
  runtime helpers; remaining gap is small.
- Loop-backedge OSR as a standalone win: parity-enabling infrastructure for JIT mode,
  not a win *over* gc+PGO. Keep as required machinery (§4.2), not a ranked candidate.
- Maglev docs as a speculation-policy source: the public docs
  (`docs/compiler/maglev/`, 192 lines) carry zero speculation policy — no
  thresholds, caps, counters, or retry rules; the policy lives in
  TurboFan/IC code (mined in wave 2). Do not re-survey the docs for policy;
  go to the code. (T3 wave 3 re-run)
- Swift `@_specialize(<type list>)`: programmer-directed eager
  specialization — a language feature, not an optimization discovery; Go
  generics already monomorphize per instantiation. Nothing to transfer.
  (T3 wave 3 re-run)
- TruffleRuby per-kind IC numeric defaults: live in the Truffle framework's
  `OptionsCatalog`, not in the sparse language checkout — not claimed, not
  transferred. Mine the framework dep if needed later. (T3 wave 3 re-run)
- Sorbet *current* tree: checker-only (refines the wave-3 W9 negative — the
  compiler experiment is history, see §2 item 19). (T3 wave 3 re-run)

**From T4 (Go-adjacent):**
- TinyGo: optimizations are size/MCU-oriented (LLVM `-opt=z` default, whole-program
  compile, itab-less interfaces = *slower* dispatch than gc, compile-time global init =
  startup-only win); no PGO; no claim of beating gc on server benchmarks. The one
  transferable transform (read-only `[]byte(s)` elision) is SURV-031. llgo
  (xgo-dev/llgo): active (7,460 commits, LLVM 22, Go 1.27 syntax) but goal is
  C/Python/JS interop; goroutines are 1:1 OS threads; their 68-case benchstat suite
  uses Go as the baseline with no beating-gc claims; no PGO. gccgo: stale frontend,
  no PGO-for-Go; its only documented better lowering (multiway select as a switch) is
  SURV-032's precedent. Original go-llvm/llgo is **dead** (LLVM 2022 RFC to remove the
  Go bindings).
- String concatenation: gc already folds `s1+s2+…` chains into one `concatstrings`
  call; loop `s += x` is algorithmic (quadratic), not a compiler gap.
- Atomics (`sync/atomic`): no optimization proposals found — only historic bugs;
  gc already intrinsifies atomics to machine instructions.
- golang/go#53048 (struct field reordering): closed; backlog covers profile-guided
  struct reorder — skip.
- golang/go#44578 (bool-key maps), #43020 (big map literals): map-specialization-
  shaped → backlog overlap (complementary only).
- golang/go#66361 (random map entry): narrow (was hot in `database/sql` connection
  pool; they rewrote the call site via CL 572119).
- golang/go#76200 (built-in Set type): language change, not optimizer material.
- golang/go#80428 (ixmap), #80972 (fxhash for small keys): gc-team-adjacent in-flight
  work — do NOT claim; watch.
- golang/go#16798 (tail calls for self-recursion), #21536 (`//go:inline`): status TBD
  (fetch throttled in wave 1) — re-check in wave 2 before claiming anything.
- golang/go#39428 (struct equality codegen): partially addressed by the Nov-2025
  equality-function rework (CL 30dff416).
- **golang/go#60879 (loop batch allocation) — no Go-team engagement in 3 years;**
  the single comment is a real-world frequency sighting (finnishtable) → filed
  as §3 angle on SURV-058. The per-P bump-allocator alternative is raw/unvetted
  (fails bar leg (b)). (T4 wave 3)
- **golang/go#15397 (ephemeral slicing) — parked since 2018:** Go team's explicit
  position (unplanned; "safe points everywhere" hardened it); backend-codegen +
  GC-contract class, no speculative dimension. GC design note filed in §4.13.
  Adjacent #24314 (write-barrier omission on self-subslice) still open — wave-4
  watch. (T4 wave 3)

**From T5 (native compilers + RFCs):**
- LLVM `-tailcallelim`: Go's traceback/defer/panic semantics forbid silent TCO; gc
  deliberately lacks it.
- LLVM `-argpromotion`: Go's ABIInternal already passes small aggregates in registers.
- LLVM `-reg2mem`, `-loop-extract`: not performance optimizations for our purposes.
- LLVM CFI schemes: Go is memory-safe; CFI addresses C/C++ vtable-hijack classes that
  don't exist in Go (interface-dispatch guard ideas subsumed by the backlog's
  devirtualization cascade).
- PGO *data* placement: LLVM/GCC PGO is overwhelmingly code layout; no transferable
  data-placement pass found — the data-layout space is owned by the backlog.
- rustc MIR-level passes as a category: gc's SSA pipeline is already the
  semantics-aware mid-level; organizational lesson only.
- SE-0377 borrowing/consuming parameters: performance content is ARC-call reduction;
  Go has no ARC; the borrowed-parameter case is exactly what gc's EA proves.
- Swift copy-on-write value semantics: direction inverted for Go — CoW would add
  copies, not remove them. Salvage: uniqueness-tracked move elision (SURV-024).
- JEP 401 value classes (language-design angle): Go structs are already identity-free
  values; the vs-Java win doesn't exist vs gc. Non-actionable (the *analysis* angle is
  SURV-022).
- JEP draft 8316779 (null-restricted value types): Go's nil-able types already encode
  null for free; nothing further transfers.
- GCC `-fipa-sra`: Go's ABIInternal already scalarizes small aggregates; remaining
  large-struct-by-pointer cases are marginal.
- LLVM `-constmerge`: cmd/link doesn't merge duplicate read-only constants, but no
  benchmark case justifies a candidate — minor gap, noted.
- **golang/go#16798 (TCO for self-recursion — DECLINED).** Fails the bar: TCO
  removes observable frames (`runtime.Callers` / `debug.Stack`); stack-trace
  fidelity is a Go design constraint. Do not resurrect. (T4 wave 2)
- **golang/go#26891 (`unsafe.Asm` inline assembly — DECLINED).** Language feature,
  not a compiler optimization. No transfer. (T4 wave 2)
- **golang/go#67520 (`simd` package proposal — FROZEN).** User-facing API, not a
  compiler optimization. No transfer. (T4 wave 2)
- **Felix Geisendörfer — shadow stacks.** Author-declined ("not warranted"; FP
  unwinding already cheap). Profiling-infra note only. (T4 wave 2)
- **Jaana Dogan / William Kennedy / GopherCon 2024–25 compiler talks / go101
  (paywalled chapters) — swept, nothing.** The "Go Execution Tracer" posts are
  tooling documentation; no compiler ideas. (T4 wave 2)
- **Pipeline-order walk wave 3 — negatives (updated wave 4):** the
  `default<O3>` pipeline-dump gap is **closed at source level** (2026-10-04):
  no local `opt` was installable (apt mirrors dead), so verification ran on
  `PassBuilderPipelines.cpp` @ `5e1ed67` (LLVM main, 2026-09-29) — strictly
  more authoritative for gating questions. Confirmed verbatim: arg promotion
  O3-only (FIXME still open, untracked), unroll-threshold and inline-param
  deltas. Three more structural deltas wave 3 missed: nontrivial loop
  unswitching (3 sites), CallSiteSplittingPass, ControlHeightReductionPass
  (all O3-only; → SURV-078). rustc MIR list re-verified pass-by-pass @
  master HEAD `4ddbc06` (2026-10-04): new `TailCopyToMove` (commit
  `7ce6f3f68`, 2026-09-23, gated `-Zmir-move-elimination` — the Project Goal
  materializing); removed `RenameReturnPlace`, `ConstDebugInfo`,
  `DeduplicateBlocks`, `ConstGoto`; tiering now per-pass policies
  (`DataflowConstProp` ≥3 — absent at default `-O2`; `MultipleReturn
  Terminators` ≥4 — never by default); `StateTransform` lost its
  optimization bracket (wave-3's R3 transfer note is stale — see §4.10
  correction). No post-2024 LLVM speculative-devirtualization development
  (`-fdevirtualize-speculatively` unchanged, mechanism still `devirt<4>`);
  no Rust RFC on `#[inline]` semantics changes; no new niche/pointer-metadata
  RFC (RFC 3204 nightly; `derive(CoercePointee)` blocked on Arbitrary Self
  Types); no `Box` representation change (doc clarification only); no
  closure-specialization RFC; const-eval GCA family — no transferable
  optimization for Phase 5 (Go has no const-eval surface); VPlan standalone
  cost model still not done (outer-loop vectorization experimental, SLP
  merge early planning). (T5 wave 3; T5 wave-4 verification)
- **T3 wave-3 source passes — negatives (all dated 2026-10-04, re-survey
  triggers named):** Swift move-only optimization exploitation — not found
  (enforcement lives in `Mandatory/` checkers only; trigger: a SIL pass keyed
  on move-only appears). SE-0474 post-acceptance optimizer movement — none
  (yielding accessors still experimental-gated; trigger: proposal status
  change). TruffleRuby language-level speculation policy — none (bounded
  ≤30 min check; `options.yml` has no guard/deopt knobs; trigger: new
  options mentioning speculation/guards). GraalPy JIT policy changes in
  2026 — none (CHANGELOG 25.x: only `-X jit=` presets + packaging/GC work;
  trigger: changelog entry on JIT internals). Sorbet — type checker only,
  no runtime artifact (permanent unless Sorbet grows a runtime). Near-misses
  struck as standalone claims: M1 Swift DeadObjectElimination (destructor-
  dependent; Go has none — transferable sliver is gc's EA), M2 pyston_lite
  as a whole-JIT transfer (N4-class compile-latency/dispatch engineering;
  slivers filed on SURV-052/SURV-004), M3 Maglev as a throughput story
  (N3 strike confirmed from the primary doc), M4 nitrous partial evaluation
  as a JIT architecture (never shipped; fact-registry sliver is SURV-068).
  (T3 wave 3)
- **V8 elements-kinds / hidden classes / map transitions** — already in §5
  (wave 1); the source pass found nothing reviving a transfer; the *policy*
  transfer (miss-against-own-cache → downgrade) is SURV-052. (T3 wave 2)
- **PyPy trace bridges** — no method-JIT analog; struck (the failure-budget idea
  is SURV-055). **JSC SpeculatedType lattice / DFG fixup mechanics** — JS-type
  speculation; only the profiling/exit machinery transfers (SURV-052…055). (T3 wave 2)
- **golang/go#19817 static-only copy→borrow (no guard).** Struck (wave 3):
  the issue's "copy isn't atomic either" argument covers only the racy case.
  Race-free counterexample: a write HB-ordered after the copy point but before
  the last load of the borrowed destination — no data race, yet the borrow
  observes a value the eager copy could never see. The write-free window must
  hold across all HB paths (incl. other goroutines): an undecidable static
  proof. Only the write-barrier-guarded speculative form survives (SURV-060).
  (T4-C-G wave 2 raw → wave-3 re-verification)
- **Wave-4 re-sweep negatives (all dated 2026-10-04):** Graal 25.5 (11 new
  compiler entries — aggressive partial unrolling/peeling GR-79881/83,
  RangeCheckEliminationPhase GR-79595, write sinking GR-79594,
  GuardRangeGroupingPhase GR-79587, lock-elimination extension GR-79526 —
  all folded as §3 convergence/adjacent angles; **no new escape-analysis
  capability, no inlining-policy change**); JEP 544 still Candidate for
  JDK 28 (JVM Weekly vol. 192 + InfoWorld, both fresh — watch for
  Proposed→Target at JDK 28 ramp-down Dec 2026–Jan 2027); JEP 401 unchanged
  (mainline, JDK 28 preview; no primitive-class movement); TruffleRuby
  speculation-options trigger NOT fired; GraalPy 25.0–25.4 changelogs carry
  no JIT-internal entries (25.4 = BouncyCastle removal only) — trigger NOT
  fired; T3 triggers: **zero of six fired** (Meta: only LLM Compiler research
  + coding agents, no new runtime JIT; Pyston dead — see §2.18; no move-only
  SIL pass — enforcement still `Mandatory/` checkers only; SE-0474 still
  Accepted/experimental-gated, not shipped in a stable release; Sorbet
  checker-only); #24314 (write-barrier omission on self-subslice) OPEN,
  unchanged, no movement; #16798 and #21536 both CLOSED/FrozenDueToAge —
  **watches retired**; #77893: all five CLs verified NEW/unmerged (go-review
  REST API) — SURV-041's gap does NOT narrow, though new fleet data
  (geekswaroop, 2026-09-27: watermark CL 793920 drives copystack → ~0 on 3
  production services) strengthens the global-heuristic precedent without
  touching SURV-041's per-spawn-site edge; next trigger: any CL
  → MERGED/ABANDONED. (wave 4: T2 re-sweep + T3 triggers + T4 watches)
- **SURV-077 STRUCK (wave 5) — vacuous analog, structural verdict.**
  (`track-surv077-wave5.md`, grounded in local Go 1.27.1 compiler source +
  disassembly.) Go has no array covariance (`[]string` is not assignable to
  `[]any`), so the declared element type of a Go slice is *always* exact and
  no store check can exist on the store path: a compiled `s[i] = v` into
  `[]any` is exactly `convTstring` boxing (no check) → bounds-check branch →
  type-word store → `writeBarrier.enabled` branch → `gcWriteBarrier2` call —
  no type check anywhere. The wave-4 note claiming "`[]any` slices are the
  exact analog of Java's `Object[]`" is wrong; covariance is the load-bearing
  difference. 11 candidate Go checks were vetted against the pattern's four
  requirements (hot check / declared-type-implied property / guard cheaper
  than the check / no gc duplication): type assertions are already a single
  pointer compare (`dottype1` — the guard *is* the check); bounds checks are
  removed by prove/loopbce with guard-is-the-check residuals (vet overlap
  with gc BCE explicitly excluded, C6); nil checks likewise (nilcheck.go);
  write-barrier checks guard the *global GC phase*, not a type property;
  interface method dispatch has no check at all (already owned by the
  SURV-004/SURV-070 devirt lane); slice-to-array-pointer conversion is a
  bounds check; map/channel ops are opaque runtime calls with no check. Zero
  candidates confirm. One real-cost shape (iface→iface assertion `x.(I2)`
  getitab path, ~order-10 cycles/hit — author's estimate) was spun off as a
  §3 angle on SURV-070 (assertion-site dominant-type guard, devirt lane,
  prototype-gated) — no SURV- ID minted: hotness unverified, win
  estimated-small. Caveats: GR-79590 internals taken from the wave-4
  changelog fetch (not load-bearing); verdict is structural, no exhaustive
  golang/go issue search was run. (wave 5, SURV-077 mechanism vetting)
- **T3 triggers wave-5: zero of six fired (all dated 2026-10-04).** Meta: no
  new optimizing JIT (only LLM Compiler / coding-agent stories). Pyston: no
  commits since Aug 12, 2024 — and method note: the real default branch is
  `pyston_main` (`/commits/main` is empty, `/commits/master` shows 2021
  CPython commits); repo still "(No longer maintained)". Swift move-only
  SIL pass: no (2026 activity is checker/enforcement fixes only). SE-0474:
  still Accepted, experimental-gated (`-enable-experimental-feature
  CoroutineAccessors`). TruffleRuby speculation options: none — method note:
  options now live in generated `Options.java` (options.yml no longer
  standalone); zero "speculat"/"guard" matches; repo HEAD unchanged from
  wave-3 pin (c734f265); CHANGELOG 40.x Performance = FFI/FFM, G1 macOS,
  strings. GraalPy JIT changelog: none (pinned at wave-4 commit `178ea261`,
  §25.4 = BouncyCastle removal only). (wave 5, T3 trigger re-survey)
- **T2/T4 re-sweep wave-5 (all dated 2026-10-04).** Graal changelog unchanged
  (master `compiler/CHANGELOG.md` 14,501 bytes still tops at 25.5, same 12
  entries; no 25.6; GitHub releases API is a dead end — legacy releases
  only) — no new speculation capability, no inlining/EA movement. JEP 401
  unchanged (Integrated, Release 28, Value Objects Preview; "Unifying
  Primitives and Classes" still a future Valhalla stream). #24314 OPEN, no
  movement (updated 2022-07-13). #77893: all five CLs (758140, 758141,
  764220, 793780, 793920) still NEW via Gerrit REST API — trigger NOT fired;
  SURV-041's per-site edge intact. Label-sweep capacity: adequate and
  unchanged (686 open `label:Performance` + 2068 open `label:compiler/runtime`
  = 28 anonymous-API calls for a full re-sweep); top-8 probe surfaced
  watch-notes only, none bar-clearing (#81887 BinarySearchFunc inlining;
  #81846 + #81755 loop-invariant constant-materialization theme in Go
  backends; #81957 GC stack-shrink O(n²); #81968 lock2 sleeper overtakes).
  (wave 5, T2 re-sweep + T4 watches)
- **Rust #157943 watch (wave 5): #163338 MoveElimination still OPEN** (gated
  on #163337, under active review through 2026-10-03; #163335/59/39/40 also
  open; author swapped review order). #163336 TailCopyToMove stays merged
  (no revert). goals#396 still Accepted; RFC #3943 still Open (RalfJung
  engaging on #163340 — the "technically unsound" caveat stands). Transfer
  ruling unchanged (§5 static-core strike stands; guarded speculative arm
  only). (wave 5, #157943 watch)
- **GraalCC / XGBoost call-count profiler: not extractable (wave 5).** The
  JDK 25 `-H:+MLCallCountProfileInference` ("GraalCC") is docs-only (Oracle)
  with zero trace in the public oracle/graal repo at HEAD (tree-listing +
  spot-checked option-declaration files; grep.app was rate-limited, 429 —
  the claim rests on filenames, and a documented `-H:` option must be
  declared by name, so a filename-obfuscated implementation is unlikely but
  not provable from tree names alone). Answered as the §6 item-4 second
  question: no. (wave 5, SURV-075 follow-up)
- **Wave-4 CGO'25 negatives:** Basso et al. DOI unverifiable (authors' PDF
  carries placeholder `10.1145/nnnnnnn.nnnnnnn`; ACM DL 403 — recorded as
  unknown, not invented); Basso heap-ordering regressed long-running Havlak
  0.97–0.98× (startup layout is steady-state-pessimal — cold-start gate is
  load-bearing); Basso's heap-path ID machinery is over-engineering for Go
  (deterministic builds — encounter-order mapping suffices); Basso needs a
  representative training run (staleness risk unaddressed); GraalNN full text
  paywalled (mechanism beyond abstract + Oracle docs unverified); GraalNN
  predicts control-split branch probabilities only (no trip counts — not an
  E4 substitute; call counts → separate XGBoost model); GraalNN "not
  available in GraalVM Community Edition" (JDK 24 docs — non-blocking only
  while Oracle GraalVM LTS is pinned); per the Phase-5 rule, Basso-style
  startup work is enabling infrastructure, never a peak-throughput
  candidate. (wave 4, CGO'25 pair)
- **Rust static move-elimination core does not transfer to Go (§5 strike on
  the static variant).** rustc's #157943 soundness rests on
  borrow-checker-style single-ownership proofs (RFC #3943; the pass is
  "technically unsound" pending the RFC's new `move` semantics per Amanieu,
  2026-06-22). Go has no such proof source — a static move-elimination pass
  for Go is killed; only the guarded speculative variant (SURV-024's E3 arm,
  keyed on escape facts with interface/closure/unsafe/reflect vetoes) is
  viable. (wave 4, Rust Goals follow-up)
- **Wave-6 re-sweep negatives (2026-10-04, all exact-check records in the
  wave-6 track files):** T3 — 0/6 standing triggers fired (Meta new JIT,
  Pyston pyston_main, Swift move-only SIL pass, SE-0474, TruffleRuby
  speculat options, GraalPy changelog); T2 — Graal 25.x changelog
  byte-identical (25.5 tops, 14,501 bytes, no new EA/inlining material),
  JEP 401 unchanged (Integrated/Release 28), OpenJ9 v0.62.0 release
  housekeeping only, no .NET movement; JEP 544 direct re-verification
  BLOCKED (HTTP 403 — recorded as a gap, wave 7 re-verifies; secondaries
  show no Targeted→Integrated movement); T4 — #24314 static,
  #77893's five CLs all still NEW via Gerrit REST (timestamps
  byte-identical; SURV-041's gap does not narrow), label-probe watches
  #81887/#81846/#81755/#81957/#81968 zero activity; #157943 — 0/3 trip
  conditions fired (#163338 still OPEN/S-waiting-on-author, no try-perf
  on #163338, RFC #3943 still Open). **SURV-070 C4 hotness: measured and
  killed as a candidate** — iface→iface assertion sites are hot in
  fmt/error shapes but the itab-probe residual is sub-1% (getitab 0.85%
  cum); the dominant-profile site costs 4.78 ns/op on the site-cache hit
  path, so the dominant-type guard's marginal saving is not load-bearing
  (details in `track-surv070-wave6-c4hotness.md`; stays a §3 angle).
  (wave 6)

---

## §6 Coverage: wave-1 notes and wave-2 gap analysis

**Wave 1 completed 2026-10-04** — 40 ranked candidates (SURV-001…SURV-040), 13 ATTEMPTED
failure-lessons (§2), consolidated negatives (§5).
**Wave 2 completed 2026-10-04** — 19 new ranked candidates (SURV-041…SURV-059:
1 Tier 1, 15 Tier 2, 3 Tier 3); 3 ATTEMPTED failure-lessons (§2); speculation
policy candidates (SURV-052…055) as E3 governance; complementary angles (§3);
enabling-infra additions (§4); negatives (§5). 59 ranked total. **Raw, not yet
ranked:** C-G (read-only clone→borrow elision) — pending a soundness
re-verification pass (read-only-by-convention claim vs Go's value semantics)
before it can enter the pipeline; open boundary question: C-F vs SURV-012
(const-value vs concrete-type parameter specialization) kept separate pending
wave-3 review.

### Per-track wave-1 coverage

- **T1 academic** (`track-1-academic.md`, 402 lines): PLDI/CGO/OOPSLA/ASPLOS/VEE/ISMM/
  POPL/ICS venues; 8 candidates (C1–C8), 3 near-misses, 10 negatives. Primary sources
  with title/venue/year/authors throughout. **Gaps:** ~15-year window not yet
  systematically swept — no forward-citation sweep on the cited papers (papers citing
  Dolby 1997/2000, Lattner 2005, Chilimbi 1998/1999, Shuf 2002); VEE/PACT/ASPLOS
  post-2015 under-covered; value-speculation literature beyond Costa 2014 (Suganuma
  lineage) thin.
- **T2 industry JVMs** (`track-2-jvms.md`, 513 lines): HotSpot, GraalVM, OpenJ9, Azul,
  .NET (Native/AOT) each with own pass; THINKING (Valhalla JEP 401/539, Leyden JEP 544,
  4-byte headers draft); ATTEMPTED 3; WOULD-LIKE 1; bucket tally ABLE 14 / THINKING 4 /
  ATTEMPTED 3 / WOULD-LIKE 1; 6 candidates, 5 near-misses. **Gaps:** Graal
  `compiler/CHANGELOG.md` is a moving target — re-sweep each wave; TruffleRuby/GraalPy
  not surveyed (coordinate with T3 to avoid duplication); OpenJ9 JITServer and Azul
  primary sources beyond marketing materials; .NET RyuJIT source for guarded-
  devirtualization codegen shapes; Leyden JEP 544 status watch; Crema/Terminus —
  **no verifiable public record found** (re-check; do not cite until verified).
- **T3 dynamic runtimes** (`track-3-dynamic-runtimes.md`, 195 lines): V8, JSC, .NET,
  CPython (PEP 659), Cinder, Pyston, HHVM, Swift (SE-0193/0377/0437/0460/0474),
  Ruby YJIT/ZJIT, LuaJIT, SpiderMonkey history; 9 candidates (C1–C9), 10 near-misses,
  15 negatives; explicit Transfer lines (what transfers / what does NOT transfer).
  **Gaps (13-item wave-2 list in the track file):** V8 source comments (`map.h`,
  `src/compiler/` IC/deopt); JSC source (`dfg/`, `ftl/b3`) value-profiling
  implementation; .NET RyuJIT dynamic-PGO probes + GDV codegen; Cinder primary
  sources (Meta eng blog, repo); modern Pyston (post-Meta-fork, "pyston-lite"); HHVM
  source region-selector heuristics; Swift compiler source (specializer/SIL) + more
  evolution proposals; TruffleRuby/GraalPy; PyPy meta-tracing (guard/deopt discipline
  in its purest form); RaptorJIT; ZJIT re-check when mature; V8 Maglev design docs;
  .NET static+PGO interplay docs; Ruby Sorbet-typed JIT directions.
- **T4 Go-adjacent** (`track-4-go-adjacent.md`, 164 lines): (a) golang/go issue mining
  — ~18 issues verified by fetch (recalled numbers proved unreliable; every citation
  fetched); 7 candidates (C1–C7); (b) community blogs — go101.org "Go Optimizations
  101" (BCE chapter), dgryski/go-perfbook, golang-design/under-the-hood, titpetric/gozero
  docs; (c) TinyGo (pipeline, differences-from-go, CHANGELOG incl. PR #3923), llgo
  (original dead; xgo-dev evaluated), gccgo. **Gaps:** systematic label sweeps
  (`label:Performance` + `label:compiler/runtime` open issues — thousands; needs paged
  API work), declined-proposal enumeration via `go.dev/s/proposal-status`; areas
  strings/`append`/select/goroutine-spawn/atomics/code-layout/reflect/interface-
  comparison/panic-cost; pre-2020 issues by year; blog authors Felix Geisendörfer,
  Jaana Dogan, William Kennedy, Bryan Boreham, Phil Pearl, Michael Stapelberg;
  GopherCon compiler talks; citation-following between posts; TinyGo's exact
  escape-analysis gap vs gc (they re-implemented it — better on any case?);
  xgo-dev/llgo GC design notes. #16798/#21536 status TBD (fetch throttled) —
  re-check before claiming.
- **T5 native compilers + RFCs** (`track-5-native-compilers.md`, 696 lines): LLVM 16
  transform passes classified pass-by-pass vs gc 1.27.1 SSA passes (**verified** from
  local source; gc PGO consumers verified by grep: inlining budgets + devirtualize
  only); GCC regular IPA passes + -O2/-O3 flags; rustc (niches, attributes,
  monomorphization); Rust RFCs (2000, 1210, 469/809/1228, 3204); Swift evolution
  (SE-0377, SE-0390); JDK JEPs (401, 417, drafts 8316779, 8261529); wishlist mining
  (LLVM VPlan, GCC value-profiling, open Rust RFCs); 14 candidate sections + 3
  ATTEMPTED + 12 negatives. **Gaps:** LLVM full `-O2`/`-O3` *pipeline order* walk
  (not just the pass list); NewPM vs legacy pass names; Propeller docs URL; BOLT
  details; LLVM Discourse "RFC" category wishlist threads; `llvm/docs/Vectorizers.rst`
  for the multi-versioning citation; GCC early/late/small IPA passes; Graphite
  maintenance-status citation; rustc full MIR pass list (`run_optimization_passes`
  order); `-Z mir-opt-level` semantics; rustc perf-guide claims; rust-lang/rfcs
  full-text search for accepted-but-unimplemented perf RFCs; Swift proposals index
  filtered by performance impact; Swift forums "Compiler" pitch threads; SE-0427
  implications; JEP index sweep for optimizer-content JEPs; LLVM/GCC bugzilla
  "missed optimization" sampling.

### Wave-2 queue (prioritized)

1. **T4 issue-tracker label sweeps** — the highest-yield gap: systematic
   `label:Performance` + `label:compiler/runtime` mining (paged API work) and the
   named-author blog pass. Go-specific proposals are the closest precedent class to
   our candidates.
2. **T3 source-level passes** — V8/JSC/.NET source comments on IC/deopt/miss policies
   and GDV codegen shapes; PyPy meta-tracing for the guard/deopt discipline.
3. **T1 forward-citation sweep** — papers citing the wave-1 corpus (Dolby, Lattner,
   Chilimbi, Shuf, Seidl&Zorn, Artigas); VEE/PACT/ASPLOS post-2015.
4. **T5 pipeline-order walk** — LLVM `-O2`/`-O3` pipeline order (not just the pass
   list); rustc MIR pass order; RFC full-text search.
5. **T2 continuous re-sweep** — Graal changelog each wave; Leyden JEP 544 / Valhalla
   JEP 401 status watches; TruffleRuby/GraalPy (with T3); Crema/Terminus re-check.
6. **Re-rank pass** — after each wave, re-rank §1 against new evidence; promote
   prototype-gated items only on measured prototypes, never on paper numbers.

### Per-track wave-2 coverage

- **T4 wave 2** (`track-4-wave2-tracker.md`, 613 lines): 447 open
  `label:Performance`+`label:compiler/runtime` issues swept (5 pages cached in
  `.t4-wave2-cache/`; 47 fetched with bodies verified; recalled numbers proved
  unreliable — wave-1 rule held); declined-proposal enumeration; pre-2020
  issues; named-author blogs (Felix Geisendörfer, Jaana Dogan, William Kennedy,
  Bryan Boreham, Phil Pearl, Michael Stapelberg), GopherCon talks; TinyGo EA
  analysis (verdict: **not better than gc's** — edge is whole-program scope,
  supporting SURV-001). 15 candidates mined (C-A…C-O), 14 ranked
  (SURV-041…051, SURV-056…059), 1 raw (C-G). Strongest: SURV-041 (per-spawn-site
  stack size — Tier 1; fleet-measured 3.9% core-weighted in copystack) and
  SURV-050 (speculative inline-stack spawn; only spawn-time signal we have is
  the profiler's own). Boundary question open: C-F (SURV-044) vs SURV-012 —
  both specialize on parameter values; split kept (const-value vs concrete-type
  parameter) pending wave-3 review. Declined/attempted mined with obstacle
  reasons (#16798 TCO, #21536 `//go:inline`, #41948 selectgo lock-all,
  #26891, #67520, #47529).
- **T4 wave 2 targeted** (`track-4-wave2-deadobj-reuse.md`, 344 lines): gc's
  heap-object-reuse experiment pinned down — **`GOEXPERIMENT=runtimefreegc`**
  (renamed from `runtimefree`; disabled by default for Go 1.26 per CL 720120),
  tracking issue golang/go#74299, design doc go.dev/design/74299-runtime-freegc,
  CL 710015 (alias analysis + auto-free after growslice), CL 673695
  (runtime.freegc/freegcTracked); soundness bugs #80697 (open), #79909 (fixed).
  Verdict recorded in SURV-056 and §2-16: static free-early sub-case raced;
  allocator-bypass slot reuse + speculative guard/deopt is the open gap.
- **T3 wave 2** (`track-3-wave2-sources.md`): source-level passes at pinned
  commits — V8 `7dce5a4` (ic.cc, js-type-hint-lowering.cc,
  compilation-dependencies.h), JSC `40508b4` (DFGOSRExit.cpp, ExitKind.h,
  ArithProfile.h, jsc.cpp, value-profile + bytecode/exit taxonomy), .NET
  `d90dbf4` (JIT GDV source: guard diamond, ladder cap 5/default 1/3, 75%
  chain gate, 32-slot histogram sampled every 64th, 6 cold-disable conditions),
  PyPy `15f6096` (counter.py, jit.py, jit.py blackhole interpreter).
  4 policy candidates ranked (SURV-052…055: miss-against-own-cache → downgrade;
  no-evidence → generic + named reason; kinded OSR exits with monotone kill;
  guard-failure budgets with decay + one-shot retrace); P4
  (absence-profiled check hoisting) → §3 complementary angle on SURV-009/033;
  .NET ladder numbers → §3 complementary angle on the devirt cascade.
  Remaining in the wave-1 13-item list: Cinder primary sources, modern Pyston,
  HHVM region-selector, Swift compiler source, TruffleRuby/GraalPy, RaptorJIT,
  ZJIT re-check, Maglev docs, Sorbet → wave-3 queue.
- **T1 / T5 wave-2**: not worked (T4 + T3 prioritized per the §6 queue); carry
  forward unchanged.

### Wave-3 queue (prioritized)

1. **C-G soundness re-verification + C-F/SURV-012 boundary** — ✅ done
   (coordinator): static-only form struck to §5 (race-free HB counterexample);
   guarded write-barrier-epoch variant ranked SURV-060; SURV-044/012 kept
   separate (const-value vs type-set), boundary documented. Notes:
   `track-4-wave3-cg.md`.
2. **T1 forward-citation sweep** — papers citing the wave-1 corpus (Dolby,
   Lattner, Chilimbi, Shuf, Seidl&Zorn, Artigas); VEE/PACT/ASPLOS post-2015.
   (W1 working → `track-1-wave3-citations.md`)
3. **T5 pipeline-order walk** — LLVM `-O2`/`-O3` pipeline order (not just the
   pass list); rustc MIR pass order; RFC full-text search.
   (W2 working → `track-5-wave3-pipeline.md`)
4. **T2 continuous re-sweep** — Graal changelog each wave; Leyden JEP 544 /
   Valhalla JEP 401 status watches; TruffleRuby/GraalPy (with T3).
   Crema/Terminus re-check **retired** — both verified 2026-10-04 (THINKING;
   see §3/§5). (W3 complete → `track-2-wave3-resweep.md`)
5. **T3 remaining source passes** — Cinder, modern Pyston, HHVM
   region-selector, Swift compiler source, TruffleRuby/GraalPy, RaptorJIT, ZJIT
   re-check, Maglev docs, Sorbet. (W4 complete → `track-3-wave3-sources.md`,
   SURV-066…072)
6. **T4 comment-depth** — #60879 design discussion, #77893 follow-ups, #51553
   beyond part 1; #15397 deeper read (parked since 2018).
   (W5 working → `track-4-wave3-comments.md`)
7. **Re-rank pass** — after wave 3, re-rank §1 against new evidence; promote
   prototype-gated items only on measured prototypes, never on paper numbers.

### Per-track wave-3 coverage (in progress)

- **Coordinator item 1** (`track-4-wave3-cg.md`): C-G re-verified; SURV-060
  ranked (Tier 2); static-only variant struck (§5); SURV-044/012 boundary
  resolved (separate, documented).
- **T2 wave 3** (`track-2-wave3-resweep.md`, 340 lines): Graal 25.0.3/25.0.4
  changelog (GR-78795 unsigned div/mod, GR-79029 DuplicationPhase,
  specialization→inliner feedback edge); Leyden JEP 544 = JDK 28 candidate
  (iterative training → SURV-014 layered-cache angle; inspectability → §4
  infra); Valhalla JEP 401 mainline-bound (arraycopy 8–20× regression →
  SURV-022 bulk-primitive risk); TruffleRuby/GraalPy project-level negative;
  Crema/Terminus verified (THINKING) — re-check retired. 0 new ranked IDs
  (RC-W3-1 folded as §3 angle on SURV-006); §3 +6 angles, §4 +2 infra,
  §5 updated.
- **T1 wave 3** (`track-1-wave3-citations.md`, 408 lines): ~15 searches, ~120
  entries triaged, 4 citing papers mined with reference-list verification
  (PLDI'21 value fields → Dolby; ISMM'15 DSA-GC → Shuf; CGO'25 GoFree —
  region-lineage, does NOT cite Lattner; ISMM'14 allocation folding, V8).
  → SURV-061 (closed-world value-field inlining, Tier 2), SURV-062
  (dominance-based allocation folding, Tier 2), SURV-063 (speculative
  tcfree + guard/deopt, Tier 2), SURV-064 (profiler-inferred DSA GC, Tier 3).
  5 dead research lines recorded in §5; Memento Mori filed as
  SURV-017/E4 angle. 2 CGO'25 papers spotted for T2/T5 (GraalNN,
  Native-Image startup) — wave-4 queue.
- **T5 wave 3** (`track-5-wave3-pipeline.md`, 551 lines): verbatim O2 pipeline
  dump (issue #200926); devirt<4> repeater, IPSCCP-before-inline, scalar
  sandwich, ~8× simplifycfg re-runs; rustc MIR order (2022 primary, 2024–2026
  restructure confirmed); RFC triage (1210 still uninstalled — §2 item 6
  re-confirmed; Allocator stabilized Sep 2026 → SURV-003 angle; no inline-
  hint/closure-spec/Box/niche RFCs; const-eval GCA no-transfer; VPlan
  transitional). → SURV-065 (devirt-triggered re-pipelining, Tier 2);
  RC-W3-2 folded into §4 as guard-fact materialization infra; R2/R3/R4/R5/T5
  → §4 JIT-pipeline design notes; VPlan profiler-costing + arg-promotion →
  §3 angles.
- **T4 wave 3** (`track-4-wave3-comments.md`, 309 lines): full threads on
  #77893 (in-flight; 5 CLs unmerged; SURV-041 corrected — gc HAS adaptive
  global heuristic, gap is per-site only; obstacle catalog + histogram-key
  refinement), #51553 (20× bulk-send prototype never submitted; mpx scope
  narrowing; robpike guard sharpening → SURV-045), #60879 (no Go-team
  engagement; finnishtable frequency sighting → SURV-058), #15397 (parked
  since 2018; unsafe-points design note → §4.13). 0 new ranked IDs; no
  ATTEMPTED lessons (nothing abandoned with a reason).
- **T3 wave 3** (`track-3-wave3-sources.md`, complete 2026-10-04): source-level
  passes on 9 runtimes at pinned SHAs. 7 new ranked candidates (SURV-066…072,
  all Tier 2); §2 items 17–18 ATTEMPTED (Cinder HIR/Static-Python abandonment,
  Pyston LLVM/nitrous never shipped); 7 complementary §3 angles (confidence-
  product gating → SURV-053; counted failures → SURV-052; entry-hoisted
  guards → SURV-004; CHA shape → SURV-001; wall-clock decay → SURV-055;
  cross-runtime ≥75%/4–6 convergence priors); §4 item 1 (bloom-filter
  dependency sets) + item 14 (Maglev bailout discipline); T3 negatives in
  §5 with re-survey triggers. Key finds: Cinder's HIR JIT is gone from the
  live tree (rides upstream PEP 744 tier-2); Pyston LLVM era dead (frozen
  2024-08-12); ZJIT skewed-dispatch lattice (SkewedPolymorphic, 0.75
  threshold) converging with .NET 75% / JSC 2:1; HHVM guard *widening* vs
  backoff as the open design choice. **Re-run (2026-10-04, post-reboot):**
  the first W4 died in a VM reboot; its record showed
  `track-3-wave3-sources.md` already held W1–W9 (the "no output file" claim
  in the resume log was wrong — corrected) and SURV-066…072 were already
  synthesized. The re-run re-seeded all repos at the recorded pins and
  mined the deeper passes (W4b–W9b: Swift devirt ordering/write-once
  promotion, RaptorJIT JIT_PARAMDEF + narrowing rationale, ZJIT invariants
  patch-in-place, Maglev docs re-read, TruffleRuby/GraalPy language-level,
  Sorbet Compiler history) → SURV-073 (patch-in-place guard self-downgrade,
  Tier 4), SURV-074 (speculative contention-free lock fast path, Tier 2);
  §2 item 19 (Sorbet Compiler ATTEMPTED); 7 new §3 angles; T3 negatives
  extended. Sections W4b–W9b disambiguate the two records — merge/renumber
  at a future cleanup pass.

### Wave-3 re-rank pass (2026-10-04, final)
Tier structure stable — no movements. New evidence strengthens feasibility
claims in place (SURV-045's 20× prototype anchor; SURV-041's feasibility data
+ narrowed gap; SURV-009's sharpened edge vs Graal's shipped static case;
SURV-058's frequency sighting; SURV-022's bulk-primitive risk) but changes no
tier. 12 new candidates slotted (11 Tier 2, 1 Tier 3): SURV-060…065 from the
first synthesis, SURV-066…072 from the T3 source passes (all policy/mechanism
class — the same family as SURV-052…055, slotted Tier 2). No prototype-gated
promotions (no new measured prototypes). 72 ranked total (SURV-001…072).

### Wave-3 re-run re-rank pass (2026-10-04)
Post-reboot W4 re-run synthesized: SURV-073 (Tier 4 —
infrastructure-dependent; patch-in-place self-downgrade composes with
§4.1/§4.5) and SURV-074 (Tier 2 — speculative lock fast path, GraalPy
precedent). No tier movements. **74 ranked total (SURV-001…074).**

### Wave-4 queue (prioritized) — ALL COMPLETE 2026-10-04

1. **T3 re-survey triggers** — ✅ done: **zero of six fired** (Meta: no new
   runtime JIT; Pyston: repo "(No longer maintained)", 0 commits past year;
   no move-only SIL pass; SE-0474 still Accepted/experimental-gated;
   TruffleRuby speculation options: none landed; GraalPy: no JIT-internal
   changelog entries in 25.0–25.4). All triggers remain armed for wave 5.
   (→ `track-resweep-wave4.md` §B)
2. **CGO'25 pair** — ✅ done: both spotted titles verified accurate.
   GraalNN (Milikic/Cugurovic/Jovanovic, CGO'25, DOI 10.1145/3696443.3708958
   — ACM paywalled, mechanism beyond abstract + Oracle docs unverified) →
   SURV-075 (Tier 2: ML-inferred branch profiles as E5 AOT profile source;
   open measurement: Java-trained model generalization to Go idioms). Basso
   et al. (CGO'25, 13-pp PDF read, DOI unverifiable) → SURV-076 (Tier 4:
   profile-guided first-use-order binary layout, cold-start-only enabling
   infra; Havlak steady-state regress 0.97–0.98× gates it). Sharp divider:
   static-generalizable (P1) vs training-run-dependent (P2) AOT profiling.
   (→ `track-cgo25-wave4.md`)
3. **T2 continuous re-sweep** — ✅ done: Graal 25.5 CHANGELOG (11 entries;
   new speculation capability GR-79590 speculative store checks → SURV-077
   Tier 2; GR-79031 exact-arithmetic deopt → §3 angle on SURV-009; GR-79526
   → §3 convergence on SURV-023; GR-79594/79585 → §3 adjacent on SURV-008;
   no new EA/inlining policy); JEP 544 still Candidate (watch: JDK 28
   ramp-down Dec 2026–Jan 2027); JEP 401 unchanged. (→ `track-resweep-wave4.md` §A)
4. **T5 verification gaps** — ✅ done: O2/O3 deltas verified at source level
   (`PassBuilderPipelines.cpp` @ `5e1ed67`; local `opt` install infeasible
   — apt mirrors dead); arg-promotion-at-O3 FIXME still **open and
   untracked**; 3 more O3-only deltas found (nontrivial unswitching,
   CallSiteSplitting, CHR) → SURV-078 (Tier 3, prototype-gated); rustc MIR
   list re-verified @ `4ddbc06`: new `TailCopyToMove` (Project Goal
   materializing), 4 passes removed, tiering now per-pass policies
   (§4.10/§4.11 corrected). (→ `track-5-wave4-verify.md`)
5. **T4 watches** — ✅ done: #24314 OPEN unchanged; #77893 all five CLs
   NEW/unmerged → SURV-041's gap does NOT narrow (fleet data strengthens the
   precedent only); #16798/#21536 CLOSED/FrozenDueToAge → **watches
   retired**. (→ `track-resweep-wave4.md` §C)
6. **Rust Project Goals 2026 follow-up** — ✅ done: 2026 goal Accepted
   (goals#396), RFC #3943 open under review, prototype #157943 in flight
   (`-Z mir-move-elimination`; #163336 merged 2026-09-30; try-perf:
   −0.3…−0.7% instr, −2.2…−2.5% cycles, −3.2% bootstrap — their numbers).
   Transfer ruling: static soundness core does NOT transfer (§5 strike);
   guarded speculative variant viable. **Prototype-gated spike designed**
   for the §3 angle on SURV-024 (Stage-0 headroom kill trigger: <10%
   dead-source-unique or <1% wall-time headroom). (→ `track-rustgoals-wave4.md`)
7. **Re-rank pass** — ✅ done (below).

### Per-track wave-4 coverage

- **CGO'25 pair** (`track-cgo25-wave4.md`, 275 lines): both papers verified;
  2 ranked (SURV-075 Tier 2, SURV-076 Tier 4); 8 first-class negatives; §3 +7
  angles (AOT profile consumers); §4 items 15 (AOT profile-source duality)
  + 16 (cold-start binary layout).
- **T2 re-sweep / T3 triggers / T4 watches** (`track-resweep-wave4.md`,
  290 lines): Graal 25.5, JEP 544/401, TruffleRuby, GraalPy, 6 T3 triggers,
  4 golang/go issue watches; 1 ranked (SURV-077 Tier 2); §3 +4 angles
  (SURV-023 convergence, SURV-008 adjacent, SURV-009 mechanism, AOT
  consumers); §5 extended (re-sweep negatives, watch retirements).
- **T5 verification** (`track-5-wave4-verify.md`, 340 lines): O2/O3 deltas at
  source level; rustc MIR pass-by-pass @ `4ddbc06`; 1 ranked (SURV-078
  Tier 3); §4.10/§4.11 corrected (StateTransform bracket gone; tiering now
  per-pass policies); §5 pipeline-order negative updated (dump gap closed at
  source level).
- **Rust Project Goals 2026** (`track-rustgoals-wave4.md`, 438 lines):
  Accepted goal, open RFC, in-flight prototype, try-perf numbers; transfer
  ruling (static core struck, speculative arm viable); prototype-gated spike
  design for SURV-024 with explicit kill triggers; §3 SURV-024 bullet
  updated; §5 strike on the static variant.

### Wave-4 re-rank pass (2026-10-04)
Tier structure stable — no movements. 4 new candidates slotted: SURV-075
(Tier 2 — GraalNN AOT profiles; the conditionality is the Go-idiom
generalization measurement), SURV-077 (Tier 2 — speculative store-check
removal; mechanism question on the Go check-to-target stated as the gate),
SURV-078 (Tier 3 — O3-only LLVM pass set; prototype-gated on compile-time
economics), SURV-076 (Tier 4 — cold-start-only layout; infrastructure +
cold-start gating). No prototype-gated promotions (no new measured
prototypes — the rule holds: paper numbers never promote). **78 ranked
total (SURV-001…078): 7 / 40 / 17 / 14.**

### Wave-5 queue (prioritized) — ALL COMPLETE 2026-10-04

1. **T3 re-survey triggers** — ✅ done: **zero of six fired** (Meta: no new
   JIT; Pyston: no commits since Aug 2024 — method note: real default branch
   is `pyston_main`; move-only SIL: checker-only activity; SE-0474 still
   Accepted/experimental-gated; TruffleRuby: no speculation options — method
   note: options now in generated `Options.java`, repo HEAD unchanged from
   wave-3 pin; GraalPy: no JIT changelog entries, pinned at `178ea261`).
   (→ `track-t3-wave5-triggers.md`)
2. **T2 continuous re-sweep** — ✅ done: Graal changelog unchanged (still
   25.5, 12 entries — no new speculation/inlining/EA movement); **JEP 544
   Candidate → Targeted for JDK 28** (direct fetch openjdk.org/jeps/544;
   transition within ~last 10 days; wave-4's Proposed→Target trigger
   satisfied — new watch: ramp-down survival to 28 GA ~Mar 2027); JEP 401
   unchanged. (→ `track-t2t4-wave5-resweep.md` §A)
3. **T4 watches** — ✅ done: #24314 OPEN unchanged; #77893 all five CLs still
   NEW → SURV-041's gap does NOT narrow; label-sweep capacity adequate,
   top-8 probe surfaced watch-notes only (#81887, #81846/#81755, #81957,
   #81968 — none bar-clearing). (→ `track-t2t4-wave5-resweep.md` §B)
4. **SURV-075 follow-up** — ✅ done: three-stage measurement protocol
   designed (Stage 0 gc-only constructible today; Stage 1 Java-trained
   surrogate on Go CFGs; Stage 2 Oracle-GraalNN confirmation; 9+1 benchmark
   corpus; 8-shape branch taxonomy with S1 error branches highest-risk;
   kill triggers K1–K4 armed; promotion bar ≥1.5% M2 geomean). Executing
   Stage 0 is a program-owner decision (prototype-adjacent measurement
   work). XGBoost/GraalCC verdict: **not extractable** (docs-only, zero
   public-source trace; plus granularity-killed vs SURV-006 regardless).
   (→ `track-surv075-wave5.md`)
5. **SURV-077 mechanism question** — ✅ done: **KILL**. 11 candidate Go
   checks vetted (Go 1.27.1 source + disassembly); the direct analog is
   vacuous (no array covariance → no store check exists on the store path).
   Spun off a §3 angle on SURV-070 (assertion-site dominant-type guard,
   devirt lane, prototype-gated) — no new SURV- ID minted. (→
   `track-surv077-wave5.md`)
6. **Rust #157943 watch → SURV-024** — ✅ done: #163336 stays merged;
   #163338 still OPEN (gated on #163337); new try-perf numbers on #163359
   (instr −0.4%, cycles 0, RSS −0.6%, binary −0.9% — their claims) mildly
   strengthen "real but young"; transfer ruling unchanged. Wave-6 trip
   conditions: #163338 merges, #163338 try-perf run, or RFC #3943 accepted.
   (→ `track-t3-wave5-triggers.md` §2)
7. **Re-rank pass** — ✅ done (below).

### Per-track wave-5 coverage

- **T3 triggers + #157943 watch** (`track-t3-wave5-triggers.md`, 230 lines):
  6 negatives with exact-check records + wave-6 method notes (pyston_main
  branch, TruffleRuby generated Options.java, GraalPy pin); #163336 merged
  stays, #163338 open; §3 SURV-024 angle refreshed; §5 extended.
- **T2 re-sweep / T4 watches** (`track-t2t4-wave5-resweep.md`): JEP 544
  Candidate→Targeted (JDK 28) — §3 angle, watch updated; Graal 25.5
  unchanged; JEP 401 unchanged; #24314 static; #77893 CLs all NEW (SURV-041
  edge intact); label probe watch-notes only; §5 extended.
- **SURV-075 protocol** (`track-surv075-wave5.md`, 339 lines): full
  load-bearing measurement protocol (corpus, 8-shape taxonomy, M1/M2/M3
  metrics, K1–K4 kill triggers, promotion bar) + GraalCC not-extractable
  verdict with granularity kill vs SURV-006; SURV-075 entry updated with
  the wave-5 protocol state; open questions recorded.
- **SURV-077 vetting** (`track-surv077-wave5.md`): 11 candidates killed with
  primary-source evidence; SURV-077 struck to §5 (vacuous analog); C4 shape
  filed as §3 angle on SURV-070 (prototype-gated, no new ID); no overlap
  with SURV-070/SURV-074; gc-BCE duplication explicitly excluded.

### Wave-5 re-rank pass (2026-10-04)
No promotions. SURV-077 is struck (vacuous analog — structural, not a
win/loss verdict on the pattern family); the C4 assertion-site shape is
filed as a §3 angle on SURV-070 without a new ID (hotness unverified,
estimated-small win). JEP 544's Targeted-for-28 movement is a status
update, not a candidate promotion. No new ranked IDs this wave (nothing
bar-cleared); no prototype-gated promotions (no new measured prototypes —
the rule holds: paper numbers never promote). **77 active ranked
(SURV-001…078 minus struck SURV-077): 7 / 39 / 17 / 14.**

### Wave-6 queue (prioritized) — ALL COMPLETE 2026-10-04

1. T3 re-survey triggers (standing) — 0/6 fired, exact-check records.
2. #157943 watch trip conditions — 0/3 fired; ruling unchanged.
3. T2 re-sweep — all negative; JEP 544 direct re-verification 403-blocked
   (gap, wave 7 re-verifies); JEP 401 unchanged; Azul Cloud Native
   Compiler watch note filed.
4. T4 watches — #24314 static; #77893 CLs all still NEW; label-probe
   watches zero activity.
5. SURV-075 Stage-0 decision — re-checked; no owner decision recorded.
6. SURV-070 C4 hotness — MEASURED (Go 1.27.1, EPYC 9D25): sites hot,
   probe residual sub-1%, guard's marginal saving not load-bearing →
   no new SURV-ID.
7. Re-rank pass — no promotions, no prototype-gated movements.

### Per-track wave-6 coverage

- **T3 standing triggers** (`track-t3-wave6-triggers.md`): all six
  re-checked ~1h after the wave-5 baseline — 0 fired. Method notes from
  §6 honored (Pyston default branch = pyston_main, still Aug 12 2024
  "(No longer maintained)"; TruffleRuby options read from the full
  generated Options.java on master, 628 lines, no speculat/guard knob;
  GraalPy CHANGELOG checked against pin 178ea261, latest commit
  Aug 18 2026, §25.4 unchanged). Wave-7 refinement: for GraalPy, check
  BOTH the file-history page and raw-vs-pin to disambiguate "file
  unchanged" from "pin stale".
- **#157943 watch** (`track-surv024-wave6-watch.md`): binary checks via
  GitHub API state/timeline endpoints (method note: faster and more
  precise than page fetches for these binary checks). #163338 OPEN
  (S-waiting-on-author, last activity 2026-09-30 force-push, gated on
  #163337); no try-perf on #163338; RFC #3943 Open (newest review
  comment 2026-09-30; 2026-10-02 `updated_at` bump is review-churn).
  Siblings unchanged. 0/3 trip conditions; SURV-024's transfer ruling
  (§5 static-core strike; guarded-speculative E3 arm only) stands;
  trip conditions re-armed for wave 7.
- **T2/T4 re-sweep** (`track-t2t4-wave6-resweep.md`): Graal 25.x changelog
  byte-identical (25.5 tops, 12 entries, 14,501 bytes); JEP 544 direct
  fetch 403-blocked (tool-level, not retried — gap, wave 7 re-verifies;
  last verified state = wave 5 Targeted/Release 28; fresh secondaries show
  no Integrated movement); JEP 401 Integrated/Release 28 unchanged, no
  primitive-class movement; OpenJ9 v0.62.0 (2026-09-15) housekeeping only;
  Azul Oct-1 Cloud Native Compiler announcement → watch note only
  (2x–5x warm-up claim, unverified, no published technique); .NET
  nothing. T4: #24314 byte-identical (open, updated 2022-07-13, 8
  comments); #77893's five CLs all NEW via Gerrit REST (SURV-041's gap
  intact); label-probe watches (#81887, #81846/#81755, #81957, #81968)
  zero activity. No bar-clearing material anywhere.
- **SURV-075 Stage-0 owner decision** (coordinator check): memory,
  survey dir, and goal files searched — no owner decision recorded.
  Remains pending; wave 7 re-checks.
- **SURV-070 C4 hotness** (`track-surv070-wave6-c4hotness.md`, coordinator
  measurement): Go 1.27.1, linux/amd64, AMD EPYC 9D25. Sites hot in
  fmt/error shapes (printArg 33.05% cum, errors.as 27.12% cum) but the
  itab-probe residual sub-1% (getitab 0.85% cum). Monomorphic site =
  4.78 ns/op (site-cache hit path); 3-type rotation = 10.94 ns/op with
  typeAssert+getitab ≈ 20% of site samples — the thrash case the
  dominant-type guard does NOT help. Verdict: not bar-clearing; C4 stays
  a §3 angle on SURV-070 with hotness measured.

### Wave-6 re-rank pass (2026-10-04)

No promotions. C4 hotness was measured and killed as a candidate (stays
a §3 angle); JEP 544's unverified state is a check gap, not a movement.
No new ranked IDs this wave (nothing bar-cleared); no prototype-gated
promotions (no new measured prototypes — the rule holds: paper numbers
never promote; Azul's 2x–5x and the #163359 try-perf numbers are source
claims). **77 active ranked (SURV-001…078 minus struck SURV-077):
7 / 39 / 17 / 14.**

### Wave-7 queue (prioritized)

1. **JEP 544 direct re-verification** — wave 6's fetch was 403-blocked;
   re-verify Status/Release directly, then continue the ramp-down
   survival watch (JDK 28 ramp-down Dec 2026–Jan 2027; GA ~Mar 2027).
2. **#157943 trip conditions (re-armed)** — #163338 merges, try-perf run
   lands on #163338, or RFC #3943 accepted (GitHub API state/timeline
   endpoints per the wave-6 method note). Any one refreshes SURV-024's
   transfer ruling and Stage-0 priors.
3. **T3 standing triggers** — with the wave-6 GraalPy refinement (check
   BOTH file-history and raw-vs-pin).
4. **T2 continuous re-sweep** — Graal changelog 25.x; JEP 401 /
   primitive-class movement; Azul Cloud Native Compiler watch note
   (any published technique or independent measurement); OpenJ9/.NET
   optimizer-relevant items only.
5. **T4 watches** — #24314; #77893 CLs (MERGED/ABANDONED narrows
   SURV-041's gap); label-probe watch notes (#81887, #81846/#81755,
   #81957, #81968) for anything bar-clearing.
6. **SURV-075 Stage-0 decision** — re-check for a program-owner decision
   (the survey does not execute the protocol itself).
7. **Re-rank pass** — promote prototype-gated items only on measured
   prototypes.

---

## §7 Candidate pipeline (how to add a candidate)

Raw → vetted → ranked. A new candidate enters **raw** in the relevant track file with
its precedent and bar-check. It becomes **vetted** when all four bar legs hold with a
named benchmark and a citable precedent. It enters §1 **ranked** with a fresh
`SURV-###` ID (never reuse or renumber IDs) and a bucket label. Struck candidates are
not deleted — they move to §5 with the reason, so nobody re-surveys them. Backlog
promotion (PHASES.md §7) is a separate program-owner decision; this document never
promotes on its own.

---

*Wave-1 synthesis: 2026-10-04. Wave-2 synthesis: 2026-10-04 — SURV-041…SURV-059
ranked (1 Tier 1, 15 Tier 2, 3 Tier 3), §2 +3 ATTEMPTED, speculation policy
SURV-052…055, C-G raw pending soundness re-verification. Wave-3 synthesis:
72 ranked (SURV-001…072) — SURV-060 (guarded borrow, Tier 2), SURV-061…063,
065 (Tier 2), SURV-064 (Tier 3), SURV-066…072 (T3 source passes, Tier 2);
§3 +25 angles; §4 +9 infra; §5 updated; §2 +2 ATTEMPTED (Cinder HIR,
Pyston LLVM); Crema/Terminus verified THINKING; re-rank pass — tiers stable
(7/37/16/12). Wave-3 complete 2026-10-04. Wave-4 synthesis: 78 ranked
(SURV-001…078, 7/40/17/14) — SURV-075 (GraalNN AOT profiles, Tier 2),
SURV-076 (cold-start layout, Tier 4), SURV-077 (speculative store-check
removal, Tier 2, mechanism question open), SURV-078 (O3-only LLVM pass set,
Tier 3); §2 strengthened (Pyston confirmed dead); §3 +12 angles (SURV-024
spike design + transfer ruling, AOT profile consumers, SURV-023/008/009
Graal-25.5 angles, §4.10/4.11 rustc corrections); §4 items 15–16 (AOT
profile-source duality, cold-start layout); §5 extended (re-sweep
negatives, CGO'25 negatives, static move-elimination strike). Wave-5
synthesis 2026-10-04: 77 active ranked (SURV-001…078 minus struck
SURV-077, 7/39/17/14) — no new IDs (nothing bar-cleared); SURV-077 STRUCK
(vacuous analog, 11-candidate vetting); SURV-075 measurement protocol
designed with armed kill triggers K1–K4 + GraalCC not-extractable verdict;
JEP 544 Candidate→Targeted for JDK 28 (§3 angle, watch updated to
ramp-down survival); #163338 still open (wave-6 trip conditions recorded);
§3 +7 angles (SURV-070 assertion-site sibling, SURV-024 priors refresh,
SURV-075 protocol, SURV-006 GraalCC, SURV-061 arraycopy-flattening watch,
JEP 544 status); §5 extended (SURV-077 strike, T3/T2/T4 wave-5 negatives,
#157943 status, GraalCC); re-rank: no movements, no prototype-gated
promotions. Next SURV- IDs: 079+. Wave-6 synthesis 2026-10-04: 77 active
ranked (SURV-001…078 minus struck SURV-077, 7/39/17/14) — no new IDs
(nothing bar-cleared); T3 0/6 triggers, #157943 0/3 trips (ruling
unchanged, trip conditions re-armed), T2/T4 all negative (Graal 25.x
identical, JEP 401 static, #77893 CLs all NEW, label watches silent);
JEP 544 direct re-verification 403-blocked (gap — wave 7 re-verifies;
secondaries show no Integrated movement); Azul Cloud Native Compiler
filed as watch note only; SURV-075 Stage-0 still no owner decision;
SURV-070 C4 hotness MEASURED (sites hot, probe residual sub-1%,
monomorphic site 4.78 ns/op — not bar-clearing, stays a §3 angle);
re-rank: no movements, no prototype-gated promotions. Next SURV- IDs:
079+.*
