# Track 5 — wave 3: pipeline-order walk (LLVM -O2/-O3, rustc MIR) + Rust RFC triage

**Worker:** W2 (wave-3 coordinator task). **Date:** 2026-10-04.
**Mission:** the ORDER, not the list — where inlining sits relative to loop opts, where
IPA runs, what gets re-run, and what that implies for a JIT that currently compiles a
function once-ish. Plus Rust RFC full-text triage on backlog-adjacent axes, plus
post-2024 notes on LLVM speculative devirtualization and VPlan.

**Standing rules honored:** survey proposes only — SPEC.md / PHASES.md / requirements
registry NOT touched. No SURV-001…059 re-proposal; new items below are raw, unnumbered,
bar-checked, awaiting coordinator numbering (060+). Evidence honesty: every number is the
cited source's claim, never our measurement.

**Sources (primary unless labeled):**
- LLVM O2 pipeline: verbatim `opt -passes='default<O2>' -print-pipeline-passes` dump
  posted in llvm/llvm-project issue #200926 ("Should LLVM's pass pipeline have a YAML
  serialization?") — `https://github.com/llvm/llvm-project/issues/200926`. Issue ~2026,
  LLVM main (contains `move-auto-init`, `memprof-remove-attributes`; shape matches
  LLVM 16–19 `PassBuilderPipelines.cpp` plus incremental additions). Corroborated
  against the O2 string embedded in diffsl's codegen.rs
  (`https://docs.rs/diffsl/0.8.3/src/diffsl/execution/llvm/codegen.rs.html`, an older
  LLVM — lacks `move-auto-init`, has `inline<only-mandatory>`).
- LLVM pipeline code: reviews.llvm.org D78512, D148010, D13443 (PassBuilderPipelines.cpp
  diffs); the O2-vs-O3 deltas below come from these, not from a printed O3 dump
  (no published `default<O3>` dump found — recorded as a gap in Negatives).
- rustc MIR order: `compiler/rustc_mir/src/transform/mod.rs` @ d2731d8
  (2022, via fuchsia.googlesource mirror —
  `https://fuchsia.googlesource.com/third_party/github.com/rust-lang/rust/+/d2731d8e9338d8fe844e19d3fbb39617753e65f4/compiler/rustc_mir/src/transform/mod.rs`),
  read in full (lines 0–1242). Current phase restructure (post-2024) per third-party
  doc corroborated against rustc source: darkraider01 "rust-compile-time-instrumentation"
  research doc (2026, labels its own claims Fact/Inference — I cite only its
  Fact-marked pass lists) + rustc_middle `mir/mod.rs` @ f1bc669 (primary:
  Analysis/Runtime `MirPhase` enum).
- RFCs: rust-lang/rfcs texts (primary), rust-lang/rust PRs/commits (primary),
  Inside Rust blog 2026-10-02 (official), libs-team minutes 2026-05/08 (primary),
  Lang/RfL HackMD 2025-12-03 (primary-ish meeting notes). Medium pieces labeled as
  secondary claims.

---

## 1. LLVM `-O2`/`-O3` pipeline order

### 1.1 The shape (from the published O2 dump)

Three big phases — **module front matter → inliner/simplification core →
vectorize/loop optimization pipeline → late cleanup**. Abbreviated; simplifycfg option
spam removed for readability (it appears ~8×, always as the "clean up after anything
that creates mess" pass):

**Phase A — module front matter (cheap, global, first):**
```
memprof-remove-attributes, annotation2metadata, forceattrs, inferattrs, coro-early
function<eager-inv>(ee-instrument, lower-expect, simplifycfg, sroa, early-cse)
openmp-opt
ipsccp, called-value-propagation, globalopt
function<eager-inv>(mem2reg, instcombine, simplifycfg)
always-inline
require<globals-aa>, function(invalidate<aa>), require<profile-summary>
```

**Phase B — the inliner core** (`buildModuleSimplificationPipeline`), the whole thing
wrapped in a **devirtualization repeater** (`devirt<4>`, max 4 iterations):
```
cgscc(devirt<4>(
  inline, function-attrs, openmp-opt-cgscc,
  function<eager-inv;no-rerun>(            # per-function simplification, bottom-up
    sroa, early-cse<memssa>, speculative-execution, jump-threading,
    correlated-propagation, jump-table-to-switch, simplifycfg,
    instcombine, aggressive-instcombine, libcalls-shrinkwrap, tailcallelim,
    simplifycfg, reassociate, constraint-elimination,
    loop-mssa(loop-instsimplify, loop-simplifycfg, licm<no-allowspeculation>,
              loop-rotate, licm<allowspeculation>, simple-loop-unswitch),
    simplifycfg, instcombine,
    loop(loop-idiom, indvars, extra-simple-loop-unswitch-passes, loop-deletion,
         loop-unroll-full),
    sroa, vector-combine, mldst-motion, gvn, sccp, bdce, instcombine,
    jump-threading, correlated-propagation, adce, memcpyopt, dse, move-auto-init,
    loop-mssa(licm<allowspeculation>), coro-elide, simplifycfg, instcombine),
  function-attrs, function(require<should-not-run-function-passes>),
  coro-split, coro-annotation-elide))
deadargelim, coro-cleanup, globalopt, globaldce, elim-avail-extern,
rpo-function-attrs, recompute-globalsaa
```

**Phase C — loop/vectorize optimization pipeline** (`buildModuleOptimizationPipeline`):
```
function<eager-inv>(
  drop-unnecessary-assumes, float2int, lower-constant-intrinsics,
  loop(loop-rotate, loop-deletion), loop-distribute, inject-tli-mappings,
  loop-vectorize, drop-unnecessary-assumes, infer-alignment, loop-load-elim,
  instcombine, simplifycfg, slp-vectorizer, vector-combine, instcombine,
  loop-unroll<O2>, transform-warning, sroa<preserve-cfg>, infer-alignment,
  instcombine, loop-mssa(licm<allowspeculation>), alignment-from-assumptions,
  loop-sink, instsimplify, div-rem-pairs, mergeicmps, expand-memcmp, tailcallelim,
  simplifycfg)
alloc-token, globaldce, constmerge, cg-profile, rel-lookup-table-converter,
function(annotation-remarks), verify
```

### 1.2 Where the interesting things sit

1. **IPA runs BEFORE inlining.** `ipsccp`, `called-value-propagation`, `globalopt` all
   precede the CGSCC/inliner pipeline. The comment in the source (D78512) is explicit:
   IPSCCP substitutes function pointers passed as arguments with direct uses, "opening
   opportunities for globalopt (and inlining)". Function attributes are deduced
   *after* inline (inside the CGSCC pipeline), then the full simplification pipeline.
2. **Inlining is inside a devirtualization repeater.** `devirt<4>` wraps the *entire*
   CGSCC pipeline (inline → function-attrs → full function simplification). The source
   comment (D78512): "We wrap the CGSCC pipeline in a devirtualization repeater. This
   will try to detect when we devirtualize indirect calls and iterate the SCC passes in
   that case to try and catch knock-on inlining or function attrs opportunities."
   Up to 4 iterations, triggered by *detected devirtualization*, not by fixpoint.
3. **Loop opts are interleaved with scalar cleanup, twice.** Inside Phase B's function
   pipeline: scalar (sroa→instcombine→simplifycfg→reassociate→constraint-elimination) →
   `loop-mssa` (licm, rotate, unswitch) → scalar (simplifycfg, instcombine) →
   `loop` (idiom, indvars, unroll-full) → scalar again (sroa, vector-combine, gvn,
   sccp, dse) → `loop-mssa(licm)` once more. Vectorization (loop-vectorize, slp) is
   deferred entirely to Phase C, after the inline/simplify core.
4. **Guard/fact-inserting passes sit at phase boundaries.** `speculative-execution`
   (the pass, not our speculation) runs early in the function pipeline;
   `constraint-elimination` runs immediately before the loop pipeline (turning assumes
   into branch facts the loop passes consume); `drop-unnecessary-assumes` runs right
   before `loop-vectorize` in Phase C; `inferattrs` runs at the very top of the module
   pipeline. Facts are derived early; consumes happen late; cleanup passes
   (instcombine, simplifycfg) re-run after *every* phase that creates mess (~8
   simplifycfg instances in O2).
5. **O2 vs O3 deltas** (from PassBuilderPipelines.cpp review evidence, D78512/D13443 —
   NOT from a printed O3 dump; none published found): (a) O3 adds `ArgumentPromotionPass`
   inside the inliner pipeline after inline+function-attrs; O2 omits it (source FIXME:
   "It isn't at all clear why this should be limited to O3"); (b) `loop-unroll<O2>` vs
   `loop-unroll<O3>` in Phase C (thresholds); loop-unroll-full runs at *both* in
   Phase B; (c) `InlineParams` more aggressive at O3. Structurally the O2/O3 pipelines
   are the same skeleton; O3 is mostly thresholds + arg promotion.
6. **`no-rerun` discipline.** The inner function pipeline is marked `no-rerun` within
   one SCC visit, but the outer CGSCC walk revisits callers bottom-up and the
   `devirt<4>` repeater re-runs the whole thing on devirtualization. Iteration is
   bounded by *unit-change detection* (a devirtualized call), not by fixpoint —
   a compile-time-bounded scheduling pattern.

### 1.3 Transfer insights for our speculative-JIT pipeline schedule

Our JIT currently compiles a function once-ish; gc does a single SSA pass sequence.
What LLVM's order argues for (each is a *scheduling policy*, not a new optimization):

**T1. Cheap IPA before inline/specialization decisions.** LLVM runs IPSCCP +
called-value-propagation + globalopt before the inliner to shrink the call graph and
turn indirect calls direct *before* spending the inline budget. Our analog: before the
JIT's inline/specialize pass over a compile unit, run a cheap whole-unit
constant/type-fact propagation (we have the always-on profiler + descriptor facts;
use them to resolve monomorphic interface targets statically *before* inline
budgeting, so the budget is spent on calls that are already direct). Edge: E4+E3.
Complementary angle on SURV-001/SURV-004 (the backlog devirt cascade) — ordering
detail, not a new candidate.

**T2. Devirt-triggered re-pipelining.** LLVM's `devirt<4>`: when a pass
devirtualizes an indirect call, the *entire* CGSCC pipeline (inline → function-attrs
→ full simplification) re-runs on the affected SCC, up to 4 times. Our analog: when
a JIT guard/specialization converts an interface call to a direct call (the exact
event our speculative devirtualization cascade produces), re-run the mid-level
pipeline — inline the newly-direct callee, re-run const-prop/simplify — on the
caller, bounded (e.g. ≤3 re-pipelines per compile, mirroring the ≤4 bound).
gc cannot do this (AOT, single pass); LLVM proves the pattern pays in an AOT
compiler; in a JIT with guards it's strictly more powerful because the
devirtualization *event* is observable at compile time. Edge: E3. → **Raw candidate
RC-W3-1** (bar-checked in §5).

**T3. Scalar sandwich around loop transforms; vectorize after facts settle.** LLVM:
scalar cleanup → loop canonicalize/rotate/unswitch → scalar → loop
idiom/indvars/unroll → scalar again (gvn/sccp/dse) → then, in a *separate later
phase*, loop-vectorize + slp. Implication for our single-compile JIT: structure the
compile as explicit sub-phases (canonicalize → loop transforms → re-canonicalize →
speculative loop transforms) rather than one linear pass list; and schedule
speculative loop specialization (guard insertion for trip-count/alignment
specialization) in the *post-inline* phase where scalar facts are settled — not
interleaved with inlining. Edge: E3. Complementary angle on the backlog's loop
specialization items — ordering detail.

**T4. Guard/fact discipline: derive early, convert at boundaries, consume late.**
`inferattrs` at the very top; `constraint-elimination` immediately before loop opts;
`drop-unnecessary-assumes` immediately before loop-vectorize. Our analog: the JIT
should have a dedicated "guard-fact materialization" step that converts
guard-derived facts (type sets, value ranges, alignment) into optimizer-visible
form *right before* the phases that consume them (loop opts, devirt), and a
corresponding cleanup that drops facts invalidated by deopt. Edge: E3+E4.
Complementary angle on SURV-009/SURV-033 (the assumption-dependency framework in
SURVEY.md §4) — this is the *scheduling* half of that infra item.

**T5. Re-run cheap canonicalization after every transform; bound iteration by
change-detection.** ~8 instcombine/simplifycfg instances; `no-rerun` inner +
change-triggered outer re-runs. Our analog: after every JIT transform, run the cheap
idempotent canonicalizer; never assume "already canonical" across a transform
boundary; re-pipeline only on detected unit changes (devirtualized call, new type
at a guarded site), never on blind fixpoint. Edge: E3. Process/pipeline note —
file as §4-style enabling infra, not a candidate.

**T6. O3's only structural addition is argument promotion — and it's arguably
misplaced.** The O2-vs-O3 delta shows LLVM gates `ArgumentPromotionPass` (scalarize
by-value/unused args after inlining) at O3 with an open FIXME questioning the
gating. Our analog: our JIT's inliner should consider *arg promotion as part of
inlining itself* (promote scalarizable args at inline time), not as a separate
O-level-gated phase — the LLVM FIXME is evidence the gating is historical, not
principled. Edge: E3 (inlining happens speculatively per call site). Complementary
angle on SURV-006 (priority inlining).

---

## 2. rustc MIR pass order

### 2.1 The 2022 baseline (primary source, read in full)

`rustc_mir/src/transform/mod.rs` @ d2731d8. rustc's MIR pipeline is *query-driven*:
`mir_const` → `mir_promoted` → `mir_drops_elaborated_and_const_checked` →
`optimized_mir`, each a separate `run_passes` invocation with its own `MirPhase`.
Passes are a flat ordered list per phase (no nested pass managers like LLVM).

**`mir_const` (MirPhase::Const)** — lints + minimal canonicalization so const-eval sees
valid MIR: `CheckPackedRef, CheckConstItemMutation, FunctionItemReferences,
SimplifyCfg("initial"), SanityCheck`.

**`mir_promoted` (MirPhase::ConstPromotion):** `PromoteTemps,
SimplifyCfg("promote-consts")`, [+ `InstrumentCoverage` if enabled].

**`mir_drops_elaborated_and_const_checked`** — forces `mir_borrowck` *first* (steal
ordering matters), then `run_post_borrowck_cleanup_passes` (MirPhase::DropLowering):
```
NoLandingPads, SimplifyBranches("initial"), RemoveNoopLandingPads,
CleanupNonCodegenStatements, SimplifyCfg("early-opt"),
# "These next passes must be executed together":
CriticalCallEdges, ElaborateDrops, NoLandingPads, AddMovesForPackedDrops,
AddRetag, LowerIntrinsics, SimplifyCfg("elaborate-drops"), Deaggregator
```

**`run_optimization_passes`** — two sub-invocations:
- GeneratorLowering phase (mir_opt_level > 0):
  `UnreachablePropagation, UninhabitedEnumBranching,
  SimplifyCfg("after-uninhabited-enum-branching"), Inline, StateTransform`
  (mir_opt_level == 0: just `StateTransform`).
- Optimization phase (mir_opt_level > 0):
  ```
  ConstGoto, RemoveUnneededDrops, MatchBranchSimplification,
  # "inst combine is after MatchBranchSimplification to clean up Ne(_1, false)":
  MultipleReturnTerminators, InstCombine, ConstProp,
  SimplifyBranches("after-const-prop"), EarlyOtherwiseBranch,
  SimplifyComparisonIntegral, SimplifyArmIdentity, SimplifyBranchSame,
  DestinationPropagation, SimplifyBranches("final"), RemoveNoopLandingPads,
  SimplifyCfg("final"), RenameReturnPlace (NRVO), ConstDebugInfo,
  SimplifyLocals, MultipleReturnTerminators, DeduplicateBlocks
  ```
  (mir_opt_level == 0: just `ConstProp` — kept because it doubles as a lint,
  FIXME #70073.)
- Then *always*, regardless of opt level: `pre_codegen_cleanup =
  CriticalCallEdges, Marker("PreCodegen")`.

### 2.2 What changed since (2024–2026)

The phase model was restructured around MIR *dialects/phases*
(`rustc_middle::mir::MirPhase`, primary source @ f1bc669): `Built`;
`Analysis::{Initial, PostCleanup}`; `Runtime::{Initial, PostCleanup, Optimized}`.
Per the 2026 third-party pipeline doc (Fact-marked claims only, corroborated against
rustc source): `run_analysis_cleanup_passes → Analysis::PostCleanup`;
`run_runtime_lowering_passes → Runtime::Initial` =
`CriticalCallEdges, PostAnalysisNormalize, Subtyper, ElaborateDrops,
CheckDropRecursion, AbortUnwindingCalls, AddMovesForPackedDrops, EraseDerefTemps,
ElaborateBoxDerefs, coroutine::StateTransform, KnownPanicsLint`;
`run_runtime_cleanup_passes → Runtime::PostCleanup`; `run_optimization_passes →
Runtime::Optimized`. Newer optimization passes named in a recent third-party
rustc deep-dive (label: secondary): `GVN`, `DataflowConstProp`, `CopyProp`
(`CopyPropagation`), `DeadStoreElimination`, `JumpThreading`. The old ordering
constraints are preserved through the restructure (elaborate-drops stays late,
inline stays before the scalar pack).

### 2.3 Transfer notes for our MIR-ish lowering

**R1. Inline runs BEFORE the scalar pack; const-prop runs AFTER inlining.**
`Inline` sits in the generator-lowering phase; `ConstProp`/`InstCombine`/
`SimplifyBranches("after-const-prop")` come after. The comment "inst combine is after
MatchBranchSimplification to clean up Ne(_1, false)" is the same "cleanup after the
pass that creates the mess" discipline as LLVM's repeated simplifycfg. Our analog:
in our MIR-ish lowering, schedule inline → const/copy-prop → branch-simplify as a
fixed triple, with a cleanup pass pinned immediately after each. (Complements T1/T3.)

**R2. Drop/defer/panic lowering is deliberately LATE.** `ElaborateDrops` runs after
borrowck, in its own phase, with a source comment that the surrounding passes "must
be executed together" — and optimizations run on pre-elaboration MIR. Our analog:
keep high-level control constructs (defer, panic paths, goroutine spawn points) as
first-class terminators *through* optimization; lower them in a late, dedicated,
atomic phase. Optimizing half-lowered control flow is where miscompiles hide.

**R3. Coroutine/state-machine lowering is bracketed by optimizations.**
`StateTransform` runs *after* a small pre-opt set ("We run some optimizations before
that, because they may be harder to do on the state machine than on MIR with async
primitives") and *before* the main optimization passes. Our analog: any lowering
that destroys high-level structure (e.g. transforming closures/goroutine entry
shapes) gets the same bracket — cheap opts before, full opts after. The principle
transfers even though Go has no async state machines.

**R4. Tiered pipelines with a correctness floor.** `mir_opt_level == 0` still runs
`ConstProp` (lint duty) and `pre_codegen_cleanup` always runs. Our analog: the JIT's
baseline tier should run the correctness-floor subset (canonicalization +
validation), the optimizing tier the full list — and the tier boundary should be a
named, reviewable list like rustc's, not an emergent property of flag checks.

**R5. Named pass instances.** `SimplifyCfg("initial")` vs `("early-opt")` vs
`("elaborate-drops")` vs `("final")`; `SimplifyBranches("initial")` vs
`("after-const-prop")` vs `("final")`. rustc re-runs the *same* pass at named
pipeline points rather than parameterizing once. Our analog: our canonicalizer
should be invocable at named points with per-point config (e.g. "don't touch
guarded diamonds yet" early, "aggressive" late) — supports T5.

---

## 3. Rust RFC full-text triage

Wave-1 already triaged RFC 2000 (const generics), 1210 (specialization), 469/809/1228,
3204 (reference niches). Below are the backlog-adjacent axes from the task brief;
each with disposition + relevance verdict. "Disposition" = RFC/repo status as found.

### 3.1 Specialization (incl. closures)

- **RFC 1210 (impl Trait specialization):** unchanged since wave-1 — **ATTEMPTED**,
  still nightly-only (`#![feature(specialization)]`), no stabilization path. A 2025
  writeup ("Rust 2.0: A Thought Experiment on Safe Specialization", dev.to,
  secondary) re-derives the known unsoundness (the `call_a_bomb` case: specialization
  introduces lifetime obligations the type-checker can't see; `min_specialization`
  rejects it). 2026 kernel-discussion notes (secondary, CSDN) show the lang team
  still treating full specialization as radioactive ("Oh no!" — decade-long type
  system problems). **Disposition:** open/unstalled. **Relevance:** NON-relevant
  (no new information vs wave-1; Go has no specialization language feature —
  our per-instantiation specialization is a JIT mechanism, SURV-016's domain).
- **Specialization of closures:** no RFC found proposing closure specialization.
  **Negative** (see §6).

### 3.2 Niche / pointer-metadata

- **RFC 3204 (reference niches):** unchanged — accepted, implemented behind
  `-Z reference-niches` (tracking rust-lang/rust#113166). No new status. NON-relevant.
- **`derive(CoercePointee)`:** tracking rust-lang/rust#123430; stabilization PR
  rust-lang/rust#133820 **waiting on Arbitrary Self Types** (Lang/RfL meeting
  2025-12-03, primary-ish notes). **Disposition:** THINKING (blocked).
  **Relevance:** LOW — trait-object metadata coercion; Go's `(tab, data)` interface
  words are not Rust fat pointers and our niche play is descriptor-driven
  (wave-1 entry), not language-metadata-driven.
- **Thin-pointer/niche layout RFCs:** none found beyond the above. **Negative.**

### 3.3 Allocator APIs

- **RFC 1398 (kinds-of-allocators):** the 2015-era RFC; the API lived behind
  `allocator_api` for ~7 years. **New development (primary):** rust-lang/rust#156882
  **merged Sep 2026** — "alloc: stabilise `Allocator`" (rollup commit f4b7c670,
  r=clarfonthey). It's an MVP subset: the `Allocator` trait + minimal stdlib
  functionality; the remainder of `allocator_api` is being renamed to
  `allocator_ext` (tracking issue #32838, wg-allocators roadmap). A secondary source
  (Medium, Sep 2026) claims it ships in **Rust 1.100** — labeled as the source's
  claim, not verified against release notes. **Disposition:** THINKING → ABLE
  (in progress; FCP was still collecting checkboxes in libs-team minutes 2026-08-25).
- **Adjacent (primary):** rust-lang/rust#157153 proposes letting `Allocator`s serve
  as `#[global_allocator]` via a new marker trait (deprecating `GlobalAlloc` over
  time); rust-lang/rust#156906 tracks stabilizing `dyn Allocator` (currently
  deliberately not dyn-compatible to reserve design space); rust-lang/rust#162960
  guards types with unstable `Allocator` params ahead of the partial stabilization.
  **Disposition:** THINKING.
- **Relevance:** MEDIUM, as a **§3-style complementary angle on SURV-003 (scoped
  arenas)**, not a new candidate. Go has no allocator hooks, so our arenas stay a
  JIT-internal mechanism — but Rust's stabilized shape (Layout-based
  alloc/dealloc, fallible allocation via `Result` instead of OOM-panic, `Allocator`
  as *the* trait superseding `GlobalAlloc`) is a reference design for our internal
  arena ABI: fallible, layout-parameterized, no panic-on-OOM. Also validates the
  industry direction (arenas are worth first-class API).

### 3.4 `Box` representation

- No RFC. A rust-lang/rust commit (318c5d6) **clarified docs**: "`Box<T>` has the
  same representation as `*mut T`" (FFI validity). Doc clarification only, no
  behavior change. **Disposition:** n/a. **Relevance:** none. **Negative.**

### 3.5 Inlining hints (`#[inline]` semantics)

- **No Rust RFC proposing `#[inline]` semantics changes found.** Rust's
  `#[inline]`/`#[inline(always)]`/`#[inline(never)]` are stable and uncontroversial;
  the action is all LLVM-side.
- **LLVM-side (primary):** LLVM Discourse RFC, June 2024 — "[RFC] Avoid inlining
  alwaysinline functions when they cannot be inlined"
  (`https://discourse.llvm.org/t/rfc-avoid-inlining-alwaysinline-functions-when-they-cannot-be-inlined/79528`):
  LLVM currently inlines `alwaysinline` even when the result is broken code
  (conflicting target-features/strictfp/pointer-auth/SME streaming-mode); proposal
  is to not inline when inlining is impossible, possibly diagnosing like GCC.
  **Disposition:** THINKING. **Relevance:** LOW for graalgo directly (Go has no
  always_inline; gc honors `//go:noinline` only), but it *is* evidence for T6's
  point: inlining legality must be checked against callee-body facts the frontend
  can't see — our JIT inliner needs the same legality gate (attribute/target-feature
  conflicts) at inline time.

### 3.6 Const-eval

- **Big new development (official):** "Generic Const Args and You", Inside Rust
  blog **2026-10-02** (`https://blog.rust-lang.org/inside-rust/2026/10/02/generic-const-args-and-you/`):
  the Const Generics project group has designed and implemented **"Generic Const
  Arguments" (GCA)** — a family of features intended to *replace*
  `generic_const_exprs` (tracking rust-lang/rust#76560), each feature covering a
  specific set of expressions usable in const generics with generic parameters.
  **Disposition:** THINKING (implemented as nightly features, replacing an older
  nightly feature).
- **Relevance:** LOW — Go has no const-eval language surface; the mechanism-level
  analog (compile-time-evaluated facts driving specialization) is already covered
  by our JIT specialization machinery. File as **negative**: no transferable
  const-eval optimization for Phase 5.
- Also noted in Rust 1.99 compat notes (secondary, newreleases.io): "Abort
  const-eval when there are generics in the type of the value being produced" —
  soundness hardening, not an optimization.

### 3.7 Bonus find (adjacent, from the same sweep)

- **Rust Project Goals 2026** include **"Inplace initialization"** and **"MIR move
  elimination"** (secondary: yugr/rust-slides deck). **Disposition:** THINKING.
  **Relevance:** MEDIUM as a §3-style complementary angle — MIR move elimination is
  the copy-elision family (CopyProp, DeadStoreElimination, DestinationPropagation,
  RenameReturnPlace already in rustc's pipeline); our descriptor model could carry
  "this slot is dead after this move" facts to eliminate copies speculatively.
  Not a ranked candidate: gc's existing copy/NRVO behavior makes the marginal win
  unclear; needs a prototype-gated spike. Flag for coordinator.

---

## 4. Post-2024 LLVM notes

### 4.1 Speculative devirtualization

**Negative: no post-2024 status change found.** `-fdevirtualize-speculatively`
remains an opt-in clang flag; the in-pipeline mechanism is still
`DevirtSCCRepeatedPass` (`devirt<4>`, max 4 iterations, change-triggered re-runs)
exactly as visible in the current O2 dump (§1.1). GCC's `ipa-devirt` (speculative
devirtualization from the type inheritance graph, `-fdevirtualize
-fdevirtualize-speculatively` at -O2) was already triaged wave-1 as ABLE precedent
for SURV-001. No 2024–2026 LLVM Discourse/devmtg development on speculative
devirtualization surfaced in this sweep. The one adjacent 2024 item is the
alwaysinline-legality RFC (§3.5). Recorded so a future wave doesn't re-search this.

### 4.2 VPlan

**Status: active, still transitional (primary: llvm/docs/VectorizationPlan.md,
current; LLVM DevMtg 2025-10 Hahn slides).**
- VPlan now drives codegen in LoopVectorize; VPlans are built after cost/legality
  decisions, with def-use chains fully modeled; VPlan-to-VPlan transforms do
  legalization (reduction/interleave-group recipes), optimization (redundant-recipe
  removal, active-lane masks), and VF/UF-specific opts (backedge removal).
- Some legality is already VPlan-native (fixed-order recurrence re-ordering as a
  VPlan→VPlan transform that marks the plan invalid on failure).
- **Not yet done:** the standalone VPlan cost model (still legacy LV cost model
  influence; transition "already started" — costs derived from VPRecipes directly);
  **outer-loop vectorization** (development slowed; inner/outer paths still separate;
  `-enable-vplan-native-path` still required experimentally); **SLP merge** (early
  planning phase); whole-function vectorization (no active upstream development).
- A 2025 seminar paper (secondary, atalantus/seminar-llvm-vplan) corroborates:
  tail-folding enablement was the near-term focus; VPlan-native cost model "could
  become vital" but unimplemented.

**Transfer (complementary angle on backlog vectorization, not a new candidate):**
VPlan's architecture is "plan first, transform second" — build 2–3 candidate
vector plans, cost them with a plan-level model, execute the winner. Our JIT has
something LLVM doesn't: the always-on profiler (E4) with trip-count/alignment
distributions. The VPlan-shaped policy for us: build *speculative* vector plans
per hot loop (plan A: no runtime checks; plan B: guarded), cost them with profiler
data instead of a static cost model, guard-and-execute the winner, deopt on drift.
That's the plan-level framing of backlog loop vectorization — file as §3 angle.

---

## 5. Raw candidates (bar-checked, unnumbered — coordinator assigns 060+)

**RC-W3-1 — Devirt-triggered re-pipelining in the JIT.** Bucket: **THINKING**
(precedent: **ABLE** — LLVM `devirt<4>` repeater in every O2/O3 compile).
**Idea.** When a JIT compile (or recompile) converts an interface/indirect call to
a direct call — via guard, profile, or closed-world proof — re-run the mid-level
pipeline (inline the newly-direct callee → const/copy-prop → simplify) on the
affected callers, bounded (≤3 re-pipelines per compile, mirroring LLVM's ≤4).
**Bar check:** (a) semantics-preserving — yes, pure re-optimization of an already-
compiled unit, guarded where speculative; (b) named benchmark — interface-heavy
workloads (e.g. JSON/codec dispatch, sort.Interface, http handler chains) vs
gc+PGO; (c) edge: **E3** (the re-pipeline is what converts a devirtualization
*event* into its full payoff; gc structurally cannot — AOT single pass);
(d) not in backlog (the backlog has the devirt cascade; the *scheduling policy*
of re-running the pipeline on the devirt event is new), not something gc does.
**Precedent note:** LLVM does this in an AOT compiler with static devirt only; our
version fires on *speculative* devirt events too, which LLVM cannot. **Open risk:**
re-pipeline cost vs win needs a budget (inline-budget discipline); megamorphic
sites must not trigger it (type-count cap, cf. T3 wave-2 policy P1–P5).

**RC-W3-2 — Guard-fact materialization pass at phase boundaries.** Bucket:
**WOULD-LIKE** (precedents: **ABLE** — LLVM `constraint-elimination` before loop
opts + `drop-unnecessary-assumes` before loop-vectorize; **ABLE** — rustc's
`SimplifyBranches("after-const-prop")`).
**Idea.** A dedicated JIT pass that converts guard-derived facts (observed type
sets, value ranges, alignment, non-nil) into optimizer-visible form immediately
before the phases that consume them (loop opts, devirt, inlining), plus a matching
cleanup that drops facts invalidated by deopt. **Bar check:** (a) sound with the
assumption-dependency framework (SURVEY.md §4) as the invalidation story;
(b) benchmark: bounds-check-heavy loops, type-switch-heavy dispatch vs gc+PGO;
(c) edges **E3+E4**; (d) not in backlog as a *scheduled pass* (§4 has the infra,
not the pipeline slot). **Note:** arguably §4 enabling infra rather than a
standalone candidate — coordinator's call whether to rank or fold into §4.

No other order insight cleared the bar as a standalone candidate; T1/T3/T5/T6/R1–R5
are filed above as complementary angles on existing SURV items (001/004/006/009/033)
or §4 infra, per the brief.

---

## 6. Negatives (first-class)

1. **No published `default<O3>` pipeline dump found.** The O2-vs-O3 deltas in §1.2
   come from PassBuilderPipelines.cpp review diffs, not from a printed O3 pipeline.
   Gap: verify with `opt -passes='default<O3>' -print-pipeline-passes` when a local
   LLVM is available; the arg-promotion-at-O3 claim in particular carries an upstream
   FIXME questioning its own gating.
2. **LLVM speculative devirtualization: no post-2024 development found.**
   `-fdevirtualize-speculatively` status unchanged; mechanism still `devirt<4>`.
3. **No Rust RFC on `#[inline]` semantics changes** (stable, uncontroversial).
4. **No new niche/pointer-metadata RFC** beyond RFC 3204 (shipped nightly) and
   `derive(CoercePointee)` (blocked on Arbitrary Self Types).
5. **No `Box` representation change** — doc clarification only (commit 318c5d6).
6. **No closure-specialization RFC found.**
7. **Const-eval (GCA family): no transferable optimization for Phase 5** — Go has
   no const-eval surface; mechanism analog already covered by JIT specialization.
8. **VPlan cost model still not standalone** (legacy LV cost model still influences
   decisions); outer-loop vectorization still experimental; SLP merge still early
   planning — VPlan is not yet the "plan-and-cost everything" system its roadmap
   describes. Our profiler-driven plan costing (§4.2) is ahead of LLVM's static
   position, not behind it.
9. **rustc MIR pass order source is 2022** for the full list; the current phase
   restructure (Analysis/Runtime dialects) is confirmed from 2024–2026 sources but
   the *complete current* ordered pass list was not re-verified pass-by-pass.
   The ordering constraints that matter for transfer (inline→scalar pack,
   elaborate-drops late, StateTransform bracketed) are confirmed stable across the
   restructure.

---

## 7. Coverage notes

- LLVM pipeline: O2 dump verbatim (issue #200926) + O2/O3 delta from source diffs.
  O3 printed dump: sought, not found (§6.1).
- rustc MIR: full 2022 list read from primary source; 2024–2026 restructure captured.
- RFC axes: specialization, niche/ptr-metadata, allocator APIs, Box representation,
  inlining hints, const-eval — all triaged with dispositions. Bonus: Rust Project
  Goals 2026 (inplace init, MIR move elimination), `dyn Allocator` tracking,
  `Allocator`-as-`#[global_allocator>` proposal.
- Post-2024: speculative devirtualization (negative), VPlan (status + transfer).
- Raw candidates: RC-W3-1 (bar-checked, THINKING/ABLE), RC-W3-2 (bar-checked,
  WOULD-LIKE/ABLE — possibly §4 infra). Everything else filed as complementary
  angles on SURV-001/004/006/009/033 or §4. Next SURV- IDs remain 060+ for the
  coordinator. Draft log: single session 2026-10-04 ~09:40–10:10 CEST.
