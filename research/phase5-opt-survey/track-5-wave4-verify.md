# Track 5 — wave 4: verification gaps (O2/O3 pipeline deltas + rustc MIR pass list)

**Worker:** wave-4 track worker. **Date:** 2026-10-04 (~10:37–11:15 CEST).
**Mission:** close the two verification gaps left by wave 3
(track-5-wave3-pipeline.md §6.1 and §6.9): (1) the O2-vs-O3 delta, in particular
the standing "arg-promotion at O3" claim with its upstream FIXME; (2) a
pass-by-pass re-verification of rustc's *current* MIR pass list.
Verification-only — no SURV- IDs assigned, no edits to SURVEY.md/CHECKPOINT.md,
no registry heartbeat.

**Sources (all primary, all fetched 2026-10-04):**
- LLVM: `llvm/lib/Passes/PassBuilderPipelines.cpp` @
  `5e1ed67dbadba1b2bc254411cf78e61db51687d1` (LLVM main, 2026-09-29);
  `llvm/include/llvm/Transforms/IPO/Inliner.h`,
  `llvm/lib/Transforms/IPO/Inliner.cpp` (same tree).
- rustc: `compiler/rustc_mir_transform/src/lib.rs` (+ individual pass files,
  `pass_manager.rs`, `compiler/rustc_session/src/config.rs`) @ master HEAD
  `4ddbc06ea09abcda34f80baf31cc9bc2686b0ae6` (2026-10-04).
  2022 baseline cross-checked at `d2731d8e9338d8fe844e19d3fbb39617753e65f4`
  (the commit wave 3 used).

## Method note

1. `opt` installation: **attempted, abandoned.** `apt-get update` produced no
   output for ~4 min (dead mirrors; process killed). No `llvm-*` debs in
   `/var/cache/apt/archives`. Per the 15-minute budget, verification proceeded
   at the **source level**, which is the more reliable route anyway: the O2/O3
   deltas are gated by `if (Level == OptimizationLevel::O3)` in the pipeline
   *construction* code — a printed pipeline would show the same thing one level
   removed.
2. rustc: read `run_analysis_cleanup_passes`, `run_runtime_lowering_passes`,
   `run_runtime_cleanup_passes`, `run_optimization_passes`, `mir_built`,
   `mir_promoted` in full from `lib.rs`, plus per-pass `policy()` functions and
   the `mir_opt_level` mapping in `rustc_session`.

---

## 1. O2-vs-O3 pipeline deltas — verified in source

**Complete verified delta** (every `Level == OptimizationLevel::O3` gate in
`PassBuilderPipelines.cpp` @ 5e1ed67, plus level-parameterized passes):

1. **ArgumentPromotionPass — O3 only, inside `buildInlinerPipeline`.**
   Exact code (lines 1034–1038):
   ```cpp
   // When at O3 add argument promotion to the pass pipeline.
   // FIXME: It isn't at all clear why this should be limited to O3.
   if (Level == OptimizationLevel::O3)
     MainCGPipeline.addPass(ArgumentPromotionPass());
   ```
   Placement, now pinned precisely (refines wave-3's "after inline+function-attrs"):
   the CGSCC pipeline (`MainCGPipeline`) is constructed as
   `InlinerPass(only-mandatory)` → `InlinerPass()` → attributor (optional) →
   `PostOrderFunctionAttrsPass(skip-non-recursive)` → **ArgumentPromotionPass
   (O3 only)** → `OpenMPOptCGSCCPass` → function-simplification pipeline
   (`no-rerun`) → `PostOrderFunctionAttrsPass()` → ShouldNotRunFunctionPasses →
   CoroSplit/CoroAnnotationElide. The whole CGSCC pipeline is wrapped by
   `createDevirtSCCRepeatedPass(PM, MaxDevirtIterations)` → printed as
   `devirt<4>` (`Inliner.cpp:664-676`; the wrapper comment at
   `Inliner.cpp:628-636` confirms the devirt-repeater semantics wave 3
   described). So at O3, arg-promotion runs *inside* the devirt repeater —
   it re-runs on every devirt-triggered iteration.
   **FIXME status: OPEN.** The FIXME is still verbatim in main (2026-09-29).
   GitHub issue/PR search (2026-10-04) found **no tracking issue or PR** for
   this FIXME. Related open items, neither of which resolves it:
   - llvm/llvm-project#111672 "[Pipelines] Run ArgProm before IPSCCP in
     non-LTO (WIP)" — about ArgProm *ordering*, not the O3 gate.
   - llvm/llvm-project#96620 "[Pipelines] Move IPSCCP after inliner pipeline".
   Note: `ArgumentPromotionPass` *also* runs unconditionally (all opt levels)
   in the LTO pre-link pipeline (line 2116, "Promoting by-reference arguments to
   by-value exposes more constants to IPSCCP") and the full-LTO post-link
   pipeline (line 2242). The O3 gate is specific to the default (non-LTO)
   pipeline.
2. **SimpleLoopUnswitchPass `NonTrivial=true` at O3 (false at O2)** — three
   sites: the function-simplification loop pipeline (line 735), the extra
   loop-pass manager (line 745), and the extra vectorizer passes (line 1422).
   (Wave 3 missed this one — it attributed the loop-unroll threshold delta
   correctly but not nontrivial unswitching.)
3. **CallSiteSplittingPass — O3 only**, in `buildModuleSimplificationPipeline`'s
   early function pipeline (line 1183).
4. **ControlHeightReductionPass — O3 only**, in `buildModuleOptimizationPipeline`
   (line 1608); `EnableCHR` defaults to `true`, so this is on by default at O3.
5. **Loop unroll thresholds** — `LoopUnrollPass(LoopUnrollOptions(static_cast<int>(Level), …))`
   (lines 1377, 1478): O2 vs O3 differ via level-derived thresholds
   (`loop-unroll<O2>` vs `loop-unroll<O3>` in printed pipelines). Confirmed as
   wave 3 stated.
6. **InlineParams** — `getInlineParamsFromOptLevel(Level)` (line 979): more
   aggressive at O3. Confirmed as wave 3 stated.

**Net:** wave-3's three deltas (arg promotion, unroll thresholds, inline
params) are confirmed verbatim against current source; three more structural
deltas were found (nontrivial unswitching, CallSiteSplitting, CHR). The
arg-promotion FIXME is **open and untracked** — still "isn't at all clear why
this should be limited to O3" after ~6 years (it dates to the new-pass-manager
port of the legacy O3 gating, reviews.llvm.org D78512, 2020).

### Raw candidate (bar-checked, unnumbered)

**RC-W4-1 — O3-only passes as JIT adoption targets.** Bucket: **ABLE**
(precedent: **ABLE** — all four run in every LLVM O3 compile).
**Idea.** The verified O3-only set is exactly the passes LLVM's own pipeline
authors couldn't justify gating at O3 (arg promotion, FIXME open) plus three
cheap targeted transforms (nontrivial loop unswitching, call-site splitting,
CHR). For our JIT's optimizing tier these are adoption candidates with an
existing cost/benefit precedent in a production pipeline — in particular
ArgumentPromotionPass, whose gating is admitted to be historical rather than
principled, maps directly onto T6 (wave 3): promote scalarizable args at inline
time rather than gating on an optimization level.
**Bar check:** (a) semantics-preserving — yes, all are standard LLVM passes;
(b) benchmark vs gc+PGO on call-heavy and loop-heavy workloads; (c) edge E3;
(d) not in backlog as named passes (T6 covers the arg-promotion *scheduling*
angle only). **Open risk:** compile-time cost at JIT scale needs measurement;
arg-promotion interacts with our calling convention work — prototype-gated.

---

## 2. rustc MIR pass list — re-verified pass-by-pass @ 4ddbc06 (2026-10-04)

`compiler/rustc_mir_transform/src/lib.rs`, read in full. Order below is the
execution order; per-pass opt-level policies follow in §2.5.

### 2.1 `mir_built` (runs for every body)

`LintAndRemoveUninhabited`, `CheckForceInline`, `CheckCallRecursion`,
`CheckPackedRef`, `CheckConstItemMutation`, `CheckMutRestriction`,
`FunctionItemReferences`, `SimplifyCfg::Initial`, `SanityCheck`.

### 2.2 `mir_promoted`

`PromoteTemps`, `SimplifyCfg::PromoteConsts`, `InstrumentCoverage`.
(Unchanged vs wave 3.)

### 2.3 `run_analysis_cleanup_passes` → `Analysis::PostCleanup`

`ImpossibleClauses`, `CleanupPostBorrowck`, `RemoveNoopLandingPads`,
`SimplifyCfg::PostAnalysis`, `Derefer`.

### 2.4 Conditional: `const_precise_live_drops` only

`RemoveUninitDrops`, `SimplifyCfg::RemoveFalseEdges`, `CheckLiveDrops` (lint).

### 2.5 `run_runtime_lowering_passes` → `Runtime::Initial`

`CriticalCallEdges`, `PostAnalysisNormalize`, `Subtyper`,
`ElaborateDrops`, `CheckDropRecursion` (lint), `AbortUnwindingCalls`,
`AddMovesForPackedDrops`, `EraseDerefTemps`, `ElaborateBoxDerefs`,
`StateTransform` (coroutine), `KnownPanicsLint` (lint).
Matches wave-3's §2.2 recording exactly. Note: the 2022
`optimizations_with_generators` pre-StateTransform opt set
(`UnreachablePropagation`, `UninhabitedEnumBranching`, `SimplifyCfg`) is
**gone** — `StateTransform` now runs with no optimization bracket around it
(see §3.4).

### 2.6 `run_runtime_cleanup_passes` → `Runtime::PostCleanup`

`LowerIntrinsics`, `RemovePlaceMention`, `SimplifyCfg::PreOptimizations`;
then `local_info` cleared on all locals.

### 2.7 `run_optimization_passes` → `Runtime::Optimized` (full order)

1. `CheckAlignment`, `CheckNull`, `CheckEnums` (UB checks — "Add some UB checks
   before any UB gets optimized away")
2. `LowerSliceLenCalls`
3. `InstSimplify::BeforeInline`
4. `ForceInline`
5. `Inline`
6. `RemoveStorageMarkers`
7. `RemoveZsts`
8. `RemoveUnneededDrops`
9. `UnreachableEnumBranching`
10. `UnreachablePropagation`
11. `SimplifyCfg::AfterUnreachableEnumBranching` *(o1)*
12. `MultipleReturnTerminators`
13. `InstSimplify::AfterSimplifyCfg`
14. `SimplifyConstCondition::AfterInstSimplify` *(o1)*
15. `ReferencePropagation`
16. `ScalarReplacementOfAggregates`
17. `SimplifyLocals::BeforeConstProp`
18. `DeadStoreElimination::Initial`
19. `GVN`
20. `SimplifyLocals::AfterGVN`
21. `SsaRangePropagation`
22. `MatchBranchSimplification`
23. `DataflowConstProp`
24. `SingleUseConsts`
25. `SimplifyConstCondition::AfterConstProp` *(o1)*
26. `JumpThreading`
27. `EarlyOtherwiseBranch`
28. `SimplifyComparisonIntegral`
29. `SimplifyConstCondition::Final` *(o1)*
30. `RemoveNoopLandingPads` *(o1)*
31. `SimplifyCfg::Final` *(o1)*
32. `StripDebugInfo`
33. `CopyProp`
34. `DeadStoreElimination::Final`
35. `DestinationPropagation`
36. **`TailCopyToMove`** ← new since wave 3 (see §3.1)
37. `SimplifyLocals::Final`
38. `MultipleReturnTerminators`
39. `CriticalCallEdges`
40. `ReorderBasicBlocks`, `ReorderLocals` (prettify — off by default)

*(o1)* = `WithMinOptLevel(1, …)` wrapper in the list itself; other gating is in
each pass's `policy()` — see §2.8.

### 2.8 Opt-level tiering (per-pass policies — the big structural change)

The 2022 two-list shape (`mir_opt_level > 0` → full list, else `ConstProp`
only) is **gone**. There is now a single list; each pass self-gates in
`policy()`. Effective default mapping (`rustc_session/src/config.rs`):
`OptLevel::No → mir_opt_level 1`, everything else → `2`
(2022: `-Zmir-opt-level` default 1 always, range 0–3; levels 0/3/4 now
reachable only via explicit `-Zmir-opt-level=N`).

| Tier | Passes |
|---|---|
| Always (Required or `optional(true)`) | `SimplifyCfg` variants, `SimplifyConstCondition` variants, `RemoveUnneededDrops`, `PromoteTemps`, `EraseDerefTemps`, `PostAnalysisNormalize`, `Subtyper`, `LowerIntrinsics`, `AbortUnwindingCalls`, `ElaborateBoxDerefs`, `MentionedItems` |
| ≥ 1 (`o1` wrapper or policy) | `CopyProp`, `SingleUseConsts`, `RemoveZsts`, `RemoveStorageMarkers` (unless lifetime markers emitted), `LowerSliceLenCalls`, `InstSimplify` both, `UnreachableEnumBranching`, `SimplifyCfg::AfterUnreachableEnumBranching/Final`, `SimplifyConstCondition::*`, `RemoveNoopLandingPads` |
| ≥ 2 | `Inline`, `ForceInline` (unless overridden), `ScalarReplacementOfAggregates`, `GVN`, `JumpThreading` (not on GPU-like targets), `DeadStoreElimination` both, `ReferencePropagation`, `DestinationPropagation`, `MatchBranchSimplification`, `UnreachablePropagation` ("only under mir-opt-level=2 as this can make programs less debuggable"), `EarlyOtherwiseBranch`, `SimplifyComparisonIntegral`, `SsaRangePropagation`, `TailCopyToMove` (+ `-Zmir-move-elimination`) |
| ≥ 3 | `DataflowConstProp` — **does not run at default `-O2`** (default is level 2) |
| ≥ 4 | `MultipleReturnTerminators` — **never runs by default** (max default level is 2) |
| Flag-gated | `StripDebugInfo` (`-Zmir-strip-debuginfo`), `ReorderBasicBlocks/ReorderLocals` (off by default), `CheckAlignment/CheckNull/CheckEnums` (part of UB-check semantics when `-Zub-checks`) |

**Corrections this forces on wave-3 §2.3:**
- R4 ("tiered pipelines with a correctness floor… the tier boundary should be
  a named, reviewable list") — the mechanism changed from a hardcoded
  two-list to per-pass policies; the transfer advice stands but should cite
  the policy-object pattern, not the list pattern.
- The 2022 "even at `-O0` the default mir_opt_level=1 ran the full `>0`
  list" quirk is gone; `-O0` now gets a genuinely thinner pipeline (level 1).
- Wave-3's R1 ("Inline runs BEFORE the scalar pack; const-prop AFTER inlining")
  still holds, now with finer granularity: `Inline` → `…` → `ReferencePropagation`
  → `SROA` → `SimplifyLocals::BeforeConstProp` → `DSE::Initial` → `GVN` →
  `SsaRangePropagation` → `MatchBranchSimplification` → `DataflowConstProp`(≥3).

---

## 3. Deltas vs wave-3's recording

### 3.1 NEW pass: `TailCopyToMove` (flagged — wave-3 §6.9 gap closed)

- Commit `7ce6f3f689efaedd5713b3fc3a208a35fcd7d840`,
  "Add a MIR pass which turns copies into moves just before a return",
  Amanieu d'Antras, **2026-09-23**.
- Position: #36 in §2.7 — immediately after `DestinationPropagation`,
  before `SimplifyLocals::Final`.
- What it does (from the file's doc comment): MIR building represents reads
  of `Copy`-type values as `Operand::Copy`, including at `return`. If the
  local's address was ever observed, the liveness analysis keeps it live until
  `StorageDead`/function exit, so the live range overlaps the return place
  and the source local can't be unified with `_0`. The pass rewrites
  final-use `Copy` → `Move` when provably unobservable before return.
- Policy: `mir_opt_level() >= 2 && -Zmir-move-elimination` — this is the
  **"MIR move elimination" Rust Project Goal 2026 materializing**, which wave-3
  (§3.7) filed as a bonus find / complementary angle on copy elision. No new
  candidate forced; the direction is confirmed active, not just a goal slide.

### 3.2 Removed passes (all verified absent from current `src/`)

| Pass (2022 list) | Removal | Evidence |
|---|---|---|
| `RenameReturnPlace` (NRVO) | subsumed when `DestinationPropagation` enabled by default | PR rust-lang/rust#142915, merged 2025-09-16 (last commit touching `nrvo.rs`) |
| `ConstDebugInfo` | deleted | commit `8fbab183d720`, "Delete `ConstDebugInfo` pass", 2024-06-10 (folded into the `SingleUseConsts` PR #125910) |
| `DeduplicateBlocks` | deleted — "I don't think this pass does anything. It's a lot of complexity for 🤷 amount of benefit." | PR rust-lang/rust#136786, merged 2025-02-09 |
| `ConstGoto` (+ `SeparateConstSwitch`) | removed when JumpThreading enabled by default | commit `014b29eecfbe`, 2024-02-09, part of PR #117206 "Enable MIR JumpThreading by default" |

### 3.3 Renamed / split / restructured

- `InstCombine` → `InstSimplify { BeforeInline, AfterSimplifyCfg }` (two pinned
  pipeline points — the "named pass instances" pattern wave-3 noted in R5).
- `SimplifyBranches {…}` → `SimplifyConstCondition { AfterInstSimplify,
  AfterConstProp, Final }`.
- `SimplifyCfg` variants now: `Initial, PromoteConsts, RemoveFalseEdges,
  PostAnalysis, PreOptimizations, Final, MakeShim, AfterUnreachableEnumBranching`
  (2022 had `Initial`, `"early-opt"`, `"elaborate-drops"`, `"final"`,
  `"promote-consts"`, `"after-uninhabited-enum-branching"`).
- `SimplifyLocals` gained `AfterGVN` (2022: single instance).
- `DeadStoreElimination` split into `{ Initial, Final }` bracketing GVN.
- `SimplifyArmIdentity` / `SimplifyBranchSame` gone (subsumed).

### 3.4 Structural changes vs 2022

1. **Coroutine `StateTransform` lost its optimization bracket.** 2022 ran
   `UnreachablePropagation, UninhabitedEnumBranching, SimplifyCfg,
   Inline, StateTransform` ("We run some optimizations before that, because
   they may be harder to do on the state machine"). Current:
   `StateTransform` sits at the end of `run_runtime_lowering_passes` with no
   pre-opts, and `Inline` is in the main optimization pipeline *after* it.
   **Wave-3's R3 transfer note ("cheap opts before, full opts after") describes
   a structure that no longer exists** — coordinator should mark R3's
   precedent as stale; the underlying principle (don't optimize half-lowered
   control flow) still holds via R2.
2. **Analysis-cleanup list changed:** 2022's
   `NoLandingPads, SimplifyBranches("initial"), RemoveNoopLandingPads,
   CleanupNonCodegenStatements, SimplifyCfg("early-opt")` + the
   must-run-together drop group → now `ImpossibleClauses, CleanupPostBorrowck,
   RemoveNoopLandingPads, SimplifyCfg::PostAnalysis, Derefer`, with the drop
   group moved to `run_runtime_lowering_passes`.
3. **New-in-list passes vs 2022** (beyond §3.1–3.3): `CheckAlignment`,
   `CheckNull`, `CheckEnums` (UB-check prologue), `LowerSliceLenCalls`,
   `RemoveZsts`, `RemoveStorageMarkers`, `ReferencePropagation`,
   `SsaRangePropagation`, `SingleUseConsts`, `StripDebugInfo`,
   `ImpossibleClauses`, `PostAnalysisNormalize`, `Subtyper`,
   `EraseDerefTemps`, `ElaborateBoxDerefs`, `AbortUnwindingCalls`,
   `RemovePlaceMention`, `MentionedItems`, `Derefer`, `LintAndRemoveUninhabited`,
   `CheckMutRestriction`, `CheckForceInline`, plus the `trivial_const` fast
   path in `mir_built`.
4. **mir_opt_level default mapping changed** (config.rs): 2022 always defaulted
   to 1; now `-Copt-level=0 → 1`, everything else → 2.

---

## 4. What wave-3 got right (confirmed, no change needed)

- LLVM `devirt<4>` repeater semantics and position (inliner pipeline wrapped by
  `DevirtSCCRepeatedPass`; wave-3's T2 precedent stands — mechanism verified
  at `Inliner.cpp:617-643`, print form at 664-676).
- IPA-before-inline ordering (`ipsccp`, `called-value-propagation`,
  `globalopt` precede the inliner pipeline — wave-3's T1; not re-verified in
  source this pass but the standing dump is corroborated by the pipeline
  construction order at `buildModuleSimplificationPipeline` line 1125+
  calling `buildInlinerPipeline` at line 1333 after the front-matter passes).
- rustc: inline-before-scalar-pack, elaborate-drops late, `SimplifyCfg` named
  instances, `pre_codegen_cleanup` → now `CriticalCallEdges` (+ prettify).
- The phase restructure (`Analysis::{Initial,PostCleanup}`,
  `Runtime::{Initial,PostCleanup,Optimized}`) matches `MirPhase` usage in the
  current source exactly.

## 5. Open / not verified this pass

- No printed `default<O3>` pipeline dump was produced (no `opt` available);
  the O2/O3 deltas above come from pipeline-construction source, which is
  strictly more authoritative than a dump for *gating* questions.
- The `cl::opt<unsigned> MaxDevirtIterations` definition site (default value)
  was not pinned to a file; the `devirt<4>` print form in the published O2
  dump (issue #200926) establishes the effective default.
- `EnableCHR` defaults `true` (line 269) — CHR runs at O3 in the default
  pipeline; not benchmarked by us, reported as the source's behavior.
- Whether `SingleUseConsts` fully subsumes the deleted `ConstDebugInfo`
  behavior was not audited beyond the deletion commit's file list.
