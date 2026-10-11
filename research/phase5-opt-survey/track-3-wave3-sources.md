# Track 3 wave-3 — source-level passes on remaining dynamic runtimes

**Worker:** W4 (T3 remaining source passes), wave-3 (2026-10-04). **Status:** COMPLETE — two interleaved records in this file: (1) the earlier partial run's W1–W9 (already synthesized as SURV-066…072); (2) the re-run's W4b/W5b/W6b/W7b/W8b/W9b (new: raw R10 → propose SURV-073, raw R12 → propose SURV-074, Sorbet-Compiler ATTEMPTED lesson → §2, plus complementary angles). Raw-candidate numbering: R10/R12 are re-run numbers; R11 belongs to the earlier run (= SURV-072).
**Rule:** every candidate below passed the candidate bar at mining time (legs checked inline; failing legs named where they exist); coordinator assigns SURV- IDs at synthesis — re-run items marked **raw**. Nothing here is a measurement — all numbers are the cited source's claims or in-source constants.
**Method:** shallow clones (single-commit fetch at the recorded pins, sparse where large), reading actual source + in-source design comments. No builds. Scratch clones under `/tmp/wave3-src/` (ephemeral; this file is the durable record).
**Scope guard:** no re-mining of [track-3-dynamic-runtimes.md](track-3-dynamic-runtimes.md) (C1–C9) or [track-3-wave2-sources.md](track-3-wave2-sources.md) (S1–S4, P1–P5); no SURV-001…072 re-proposals; SPEC.md / PHASES.md / registry untouched.

**Source pins (2026-10-04):**
- Cinder `facebookincubator/cinder` @ `b731267e91a16ea9fb3cfc33ad6d4815c43029f5`
- Pyston `pyston/pyston` @ `fe8afded6166e0774a62bac627e3fd3ba92a41d2` (last commit 2024-08-12)
- HHVM `facebook/hhvm` @ `a8fc771d856c1c78908e91299936dc8c8deddf62` (sparse: `hphp/runtime/vm/jit`, `hphp/doc`, `hphp/runtime/base`)
- Swift `swiftlang/swift` @ `6b8daddf6f30bb1bbdcaaede7f673e76eb155278` (sparse: `lib/SILOptimizer`, `docs`)
- RaptorJIT `raptorjit/raptorjit` @ `1d83bb4ec7581cc1715725af1514297ca3f3ac64`
- CRuby `ruby/ruby` @ `b61936bc29156f6abff617f381d968a25b1a8e79` (sparse: `zjit`, `docs`)
- V8 (Maglev docs) `v8/v8` @ `7dce5a4a324258ba51b2a6f5dee7a1fa18c7671d` (sparse: `docs`, `src/maglev`)
- Sorbet `sorbet/sorbet` @ `15481bbc70595facb3d143d586936aca328de00a` (sparse: `docs`, `gems`)
- TruffleRuby `oracle/truffleruby` @ `c734f26543003fefd4519a29adbca62d0c711a2d` (sparse: options, doc)
- GraalPy `oracle/graalpython` @ `178ea261764d6478b09d7cdc2dee1060e139e91e` (sparse: `docs`, CHANGELOG)

---

## W1 — Cinder: the HIR JIT is gone; what the tier-2 optimizer actually does

**Headline finding:** the Cinder this survey described in wave 1 no longer exists in the live tree. There is no `JIT/` HIR/middle-end directory, no Static Python module (`grep -rli staticpython` over the whole tree: zero hits), and the only JIT is `Tools/jit` — the upstream PEP 744 copy-and-patch tier-2 (the README opens with "This version of CPython can be built with an experimental just-in-time compiler[^pep-744]", LLVM 19 build requirement). Meta's fork now rides upstream CPython's adaptive interpreter + uop optimizer. The speculation machinery worth mining is therefore CPython's `Python/optimizer.c` + `Python/optimizer_analysis.c` + `Include/internal/pycore_optimizer.h`.

**Bucket of precedents:** the tier-2 optimizer is **ABLE** (shipped in CPython 3.13+); the HIR JIT + Static Python are **ATTEMPTED → abandoned** (absent from the current tree; the obstacle that killed them — maintaining a parallel optimizing JIT and a typed dialect against upstream CPython's velocity — does not apply to us, but the *lesson* does: don't build a second language or a second backend; speculate inside the one language. Our plan already does this).

### W1.1 — Confidence-product gating for trace formation
`Python/optimizer.c:467`: `#define CONFIDENCE_CUTOFF 333` (out of 1000). During trace formation each branch bytecode contributes its inline-cache counter bitcount: `jump_likely = bitcount > 8`, and `confidence` is multiplied by `bitcount/16` (or `(16-bitcount)/16`) per branch (`optimizer.c:670-690`). Below the cutoff the trace is abandoned (`goto done`, `OPT_STAT_INC(low_confidence)`). Transferable: speculation regions form only when the *product* of per-branch confidences clears a threshold — a quantitative refinement of P2's "no-evidence → generic".

### W1.2 — Side-exit chain depth cap with a progress requirement
`Include/internal/pycore_optimizer.h:165`: `#define MAX_CHAIN_DEPTH 4`. `optimizer.c:127-133`: the first executor in a chain and the MAX_CHAIN_DEPTH'th executor *must make progress* ("We can only insert the executor into the bytecode if this is true, since a deopt won't infinitely re-enter the executor"). A chain that can't show progress at depth 4 is refused. This is a deopt-loop breaker in the same family as V8's S1.3 and PyPy's retrace cap — but the mechanism is a *progress proof*, not a counter.

### W1.3 — Bloom-filter dependency sets (lossy invalidation registry)
`optimizer.c:1382-1407` (`_Py_BloomFilter_Init/Add`, `bloom_filter_may_contain`); each executor records the code objects it depends on (`_Py_BloomFilter_Add(dependencies, initial_code)` at :566, plus called functions' code at :890); `_Py_Executors_InvalidateDependency` (`:1527-1567`) drops every executor whose bloom filter may contain the mutated object. Over-invalidation is the safe direction. Design datum for our invalidation registry (wave-1 infra item): lossy bloom sets trade precision for O(1) dependency recording.

### W1.4 — Bounded-tolerance guards for global/module state
`pycore_optimizer.h:101-102`: `_Py_MAX_ALLOWED_BUILTINS_MODIFICATIONS 3`, `_Py_MAX_ALLOWED_GLOBALS_MODIFICATIONS 6`. Dicts carry a mutation counter (`_ma_watcher_tag`, `optimizer_analysis.c:get_mutations`); the uop optimizer folds a global/builtin load only while mutations are below the cap (`optimizer_analysis.c:198,220,236`), and the watcher invalidates executors once the cap is hit. Transferable policy: don't deopt on the *first* change — tolerate up to N mutations, then invalidate. The Go analog is package-level variables and other process-lifetime-but-mutable state read in hot loops.

**New ideas mined (bar-checked):**

- **R2 — "Side-exit chains: depth cap 4 + progress proof" (policy for C5/C7).** Cap speculative side-exit chains at 4 links; refuse to extend a chain unless the first and Nth links provably make forward progress (CPython's rule). Bar: (a) sound — refused chains fall back to the baseline; (b) benchmark: C5's loop-unswitching / C7's rejit chains — bounds worst-case chain explosion; (c) E3; (d) not in SURV-001…060 (P3 kinds exits, P5 budgets failures — neither caps chain *depth* with a progress proof); (e) gc has no side exits. **Bucket: WOULD-LIKE** (precedent ABLE — CPython tier-2). **Raw.**
- **R3 — "Bounded-tolerance guards for mutable global state" (policy for package-level state).** Guard hoisted loads of mutable-but-rarely-changed state with a mutation counter; tolerate up to N changes (CPython: 6 globals / 3 builtins), invalidate the specialization only when the cap is hit. Bar: (a) sound — invalidation restores correctness; (b) benchmark: request-handler hot loop reading a package-level config/feature flag (the C5 feature-flag example, but for *data* not branches); (c) E3+E4 (mutation profile); (d) not in SURV-001…060; (e) gc must reload every time. **Bucket: WOULD-LIKE** (precedent ABLE). **Raw.**
- **Complementary angle on P2 (not a new ID):** W1.1's confidence-product gate (333/1000 cutoff, multiplicative per-branch confidence) is the quantitative form of P2's "no-evidence → generic": gate *region formation* on the joint confidence, gate *site speculation* on evidence existence.
- **Complementary angle on the invalidation-registry infra (not a new ID):** W1.3's bloom-filter dependency sets — the concrete lossy design for the registry wave 1 listed as enabling infrastructure.

---

## W2 — Modern Pyston: the LLVM era ended; what survived

**Headline finding:** the wave-1 note ("LLVM-JIT era ended") is confirmed at source level and is stronger than expected. `pyston/llvm/` is **empty**. `pyston/nitrous/` (LLVM-based partial evaluator: `jit.cpp`, `Interpreter.cpp`, `facts.cpp`, `optimization_hooks.h`) remains, and `pyston/pystol/` wires it to `python3.8-pyston2.3` — but the entire repo's last commit is **2024-08-12** ("Fix file naming"). The LLVM JIT and the nitrous/pystol partial-evaluation experiment never shipped and are now stale (**ATTEMPTED**; obstacle: JIT cost/complexity and tracking upstream CPython — same family as Cinder's HIR lesson). What ships today (**ABLE**): `pyston/pyston_lite` — the DynASM copy-and-patch JIT as a pip-installable extension module: quickening + aggressive attribute caching + AOT speculation hints, minimal true speculation.

### W2.1 — Saturating per-cache failure counters with forgiveness
`pyston_lite/aot_ceval.c`: every inline cache carries `num_failed`; thresholds vary by site kind (`>=2`, `>=3`, `>=5`, `>=10`, `>=15` at :1747/:2024/:2159/:2078/:1853); exceeding the threshold abandons the cache permanently ("don't use the cache anymore we used all slots", :2078; the setup code also refuses to fill a cache whose counter is high, :4528-4530). Some sites *reset* the counter on success (:1859, :4691) — forgiveness for transient failures. The JIT fast path additionally requires `num_failed == 0` (`aot_ceval_jit.c:2622,3205`). Transferable refinement of P1's "miss-against-own-cache → downgrade": *counted* failures with per-site thresholds and occasional forgiveness, not a single-strike rule.

### W2.2 — AOT speculation hints: one check at the top
`aot_ceval_jit.c:150-165` (comment): per-`LOAD_METHOD` `CallMethodHint` linked list `(type, attr, meth_found)`; "we can skip extra guards by doing a single type check at the beginning." Transferable shape for C1: hoist the guard to the region entry instead of guarding each call — one check covers the whole hint chain.

### W2.3 — Nitrous "facts": declared-immutable memory ranges
`pyston/nitrous/optimization_hooks.h:35-36,44`: `JIT_IS_CONST (1<<0)`, `JIT_NOT_ZERO (1<<1)`; `addJitConst(addr, size, flags)` declares a memory range's property. The `Knowledge` lattice (`isnonzero`, `isheapalloc`, `known_value`) is keyed by `Location` (byte offset+size indirections) and scoped by `Domain` (dominated-by instruction); `facts.cpp:unionInto/intersectInto` join facts across control flow. Example use (`pystol.cpp:addImmortalTuple`): immortal tuple payloads are `MARKCONST` — the JIT const-folds loads through them with **no guard at all**. Transferable formalization of "facts the JIT trusts without guards" — for Go: type descriptors, itabs, method tables, map-type structs, and other process-immutable runtime structures.

**New ideas mined (bar-checked):**

- **R6 — "Fact-annotated runtime: declared-immutable ranges the JIT trusts guard-free" (E1+E3).** A registry of `(address, size, property)` facts (const / nonzero / known-value) maintained by the runtime; the JIT's knowledge lattice const-folds loads through declared ranges with no guard. Go instances: itab/method-table pointers in interface dispatch, map type descriptors, immortal string payloads. Bar: (a) sound — facts are process-lifetime invariants (or version-tagged; violation ⇒ fact withdrawn ⇒ recompile); (b) benchmark: interface-heavy dispatch (`net/http` handler chains — itab load folded), map access through `hmap` type pointers; (c) E1+E3; (d) not in SURV-001…060 (nothing proposes a declared-facts registry); (e) gc re-loads every time. **Bucket: WOULD-LIKE** (precedent ATTEMPTED — nitrous never shipped; the *mechanism* is small and its obstacle — LLVM JIT cost — doesn't transfer to a fact registry). **Raw.**
- **Complementary angle on P1 (not a new ID):** W2.1's counted-failure caches (thresholds 2–15 by site kind, forgiveness resets on success) — the policy numbers for P1's miss-against-cache downgrade.
- **Complementary angle on C1 (not a new ID):** W2.2's single-entry-check-over-hint-chain — hoist the guard to region entry.

---

## W3 — HHVM region selector: formation policy, guard placement, retranslation

**Source:** `hphp/runtime/vm/jit/region-selection.{h,cpp}`, `region-guards.cpp`, `region-hot-trace.cpp`, `region-prune-arcs.cpp`, `translate-region.cpp`, `hphp/doc/configs.specification`. **Bucket: ABLE** (production).

### W3.1 — Region formation modes and size caps
`region-selection.h:46`: `PGORegionMode::{Hottrace, Hotblock, HotCFG, WholeCFG}` — PGO-guided: follow hot arcs (Hottrace), single block (Hotblock), arbitrary CFG with cold-path pruning (HotCFG), whole function (WholeCFG). Caps (`configs.specification:2075-2076`): `Jit.MaxRegionInstrs = 3000`, `Jit.MaxLiveRegionInstrs = 50` (tracelet mode for live translations). Guard placement: type *preconditions* on entry blocks, *post-conditions* propagated block-to-block (`discardPoppedTypes`, `mergePostConds`, `preCondsAreSatisfied` in region-selection.cpp) — guards live at region entries, facts flow inside.

### W3.2 — Retranslation chains with guard relaxation (the convergent alternative to deopt-retry)
`region-guards.cpp`: blocks in a retranslation chain (`prevRetrans`/`nextRetrans`) carry per-block states `relaxed / deleted / merged` (BlockData, :46). `relaxIsProfitable` (:124-155): for each guard, weight-profile the types observed at that location; relax the guard to the wider `DataTypeCategory` iff no single type dominates — `noRelaxWgt * 100 < relaxWgt * Cfg::Jit::PGORelaxPercent` (default `PGORelaxPercent = 100`, i.e. relax only if the widened type strictly covers more profile weight). On retranslation HHVM *widens the failing guard* to the observed distribution instead of retrying the narrow speculation — retranslation converges because relaxation is monotone.

### W3.3 — Call-target speculation caps
`configs.specification:2197-2199`: `Jit.PGOCalledFuncCheckNumSpeculations = 6`, `Jit.PGOCalledFuncCheckThreshold = 0.1` — at most 6 speculative call-target checks per site, each needing ≥10% profile weight. Converges with .NET's ladder cap 5 (S3.2) and V8's poly cap 4 (S1.1): independent runtimes land on "a handful".

**New ideas mined (bar-checked):**

- **R7 — "Convergent retranslation: widen the guard, don't retry the speculation" (E3+E4).** On repeated guard failure at a site, retranslate with the guard *relaxed to the profile-weighted observed type set* (monotone widening; dead blocks deleted/merged) instead of deopt-and-retry or exponential backoff. This is the third policy in the P3/P5 family (kinded exits / failure budgets): generalize rather than back off. Bar: (a) sound — the widened guard still checks; the unspecialized path remains; (b) benchmark: C1 func-value sites and C8 generic sites under type churn — the API-gateway phase-change scenario from C7, where backoff just delays and widening converges; (c) E3+E4; (d) not in SURV-001…060 (P3/P5 specify exit kinds and budgets, not guard widening); (e) gc cannot retranslate. **Bucket: WOULD-LIKE** (precedent ABLE — HHVM). **Raw.**

---

## W4 — Swift compiler: value-semantics passes in SIL

**Source:** `lib/SILOptimizer/` (SemanticARC, Transforms, Mandatory). **Bucket: ABLE** (all shipped in swiftc).

### W4.1 — Borrow-scope copy propagation (SemanticARC/CopyValueOpts.cpp)
Eliminate `copy_value` when the operand's every use is a non-consuming borrow inside a borrow scope: find all borrow introducers (`getAllBorrowIntroducingValues`), build the `OwnershipLiveRange`, and if there is no unknown consuming use, drop the copy and use the borrowed value directly (`performGuaranteedCopyValueOptimization`, :60-130). Conservative scoping is explicit in the comments: "Today we only perform this for guaranteed parameters since this enables us to avoid doing the linear lifetime check"; bails on pattern-match failure or any unknown consuming use. Transferable: with an ownership/borrow analysis (or E1 descriptor borrow facts), eliminate copies of values that provably never escape their borrow region — Go analogs: large struct by-value copies, interface conversions, slice-header copies.

### W4.2 — Dead-object elimination (Transforms/DeadObjectElimination.cpp)
File header (:22-30): eliminate store-only `alloc_ref` objects *with destructors* when (1) the destructor is proven well-behaved (no side effects outside itself) and (2) the use graph doesn't escape — the object, its stores, and its destroys all vanish. This is dead-store elimination lifted to objects with destructors.

### W4.3 — CHA devirtualization + generic specialization
`Transforms/Devirtualizer.cpp`: whole-module `ClassHierarchyAnalysis`-driven devirtualization of class methods (AOT analog — complements SURV-001). `Transforms/GenericSpecializer.cpp` / `EagerSpecializer.cpp`: prespecialization of generics at -O (the mechanism behind SE-0460 `@specialized`).

### W4.4 — Move-only: enforcement only, no optimization exploitation found
Move-only checking lives in `Mandatory/` (`MoveOnlyAddressCheckerUtils.cpp`, `ConsumeOperatorCopyableValuesChecker.cpp`, `DiagnoseLifetimeIssues.cpp`) — diagnosis, not optimization. No SIL pass was found that exploits move-only-ness to elide copies beyond what the borrow checker already enables. Likewise **SE-0474 (yielding accessors): no post-acceptance optimizer movement** — `_read`/`_modify` exist, `COWOpts.cpp` optimizes `begin_cow_mutation`, but no new pass targets yielding accessors; the feature remains behind the experimental gate. C9 (wave 1) remains the transfer vehicle.

**New ideas mined (bar-checked):**

- **R9 — "Borrow-scope copy elimination" (E1+E3).** With borrow/ownership facts, eliminate a value copy when every use is a non-consuming borrow within the borrow scope; bail on any unknown consuming use; scope the first version to the provably-safe subset (Swift: guaranteed parameters only). Go targets: by-value struct copies in hot calls, interface boxing of values that are only borrowed, slice-header copies. Bar: (a) sound — ownership analysis; the copy is semantically redundant by construction; (b) benchmark: `encoding/json` marshal of large value structs, hot functions taking big structs by value; (c) E1+E3; (d) not in SURV-001…060 (nothing does borrow-driven copy elimination); (e) gc copies unconditionally. **Bucket: WOULD-LIKE** (precedent ABLE — Swift). **Raw.**
- **Near-miss: DeadObjectElimination as a standalone claim.** W4.2's power comes from proving destructors side-effect-free; Go has no destructors. The transferable sliver (allocation provably doesn't escape and has no side effects ⇒ eliminate) is gc's escape analysis, which already does it. Struck.
- **Complementary angle on SURV-001 (not a new ID):** W4.3's CHA devirtualizer — the AOT whole-module mechanism shape for the closed-world half.

---

## W5 — TruffleRuby / GraalPy: bounded source check (≤30 min) → negative

**TruffleRuby** @ `c734f26543003fefd4519a29adbca62d0c711a2d`: language-level `Options.java` (generated from `options.yml`) contains **no speculation-policy options** — the only JIT-adjacent knobs are `INLINE_DEFAULT`, `YIELD_ALWAYS_INLINE`, `METHODMISSING_ALWAYS_INLINE`. All guard/deopt policy lives in Truffle DSL specializations (GraalVM's remit = Track 2, already surveyed). **GraalPy** @ `178ea261764d6478b09d7cdc2dee1060e139e91e`: CHANGELOG for 25.x shows no JIT/speculation policy changes — only `-X jit=0|1/2` throughput presets (25.1) and packaging/GC-footprint work. Consistent with W3 (T2 resweep): no project-level JIT changes in 2026. **First-class negative** (both): nothing below changelog granularity found in the bounded check; do not re-survey until a changelog signals otherwise.

---

## W6 — RaptorJIT: trace-economics and guard discipline

**Source:** `src/lj_record.c`, `src/lj_trace.c`, `src/lj_snap.c`, `src/lj_jit.h`. **Bucket: ABLE** (production, Snabb). RaptorJIT's delta vs LuaJIT is observability (always-on `auditlog`/`vmprofile`) and code reduction — the trace machinery is LuaJIT's, hardened.

### W6.1 — Guard discipline: snapshots, hot side exits, then blacklist
A failed guard restores interpreter state from the snapshot and side-exits. A *hot* side exit (`JIT_P_hotexit`) is re-recorded as a side trace; `last_try` (`lj_trace.c:423-429`): after `JIT_P_hotexit + JIT_P_tryside` attempts the exit **self-links** (blacklisted) — that path runs in the interpreter permanently. Guard failure never retries the same speculation indefinitely; the fallback is structural.

### W6.2 — Doubling penalty cache per pc + 1s hotcount decay
`lj_jit.h:248-251`: `PENALTY_SLOTS 64`, `PENALTY_MIN 72` (36*2), `PENALTY_MAX 6000000`; `lj_trace.c:398-417`: each trace abort **doubles** the penalty for that pc (`val = (val << 1) + ...`); `hotcount_decay` (`lj_trace.c:55-86`): "We reset all hotcounts every second. This is a rough way to establish a relation with elapsed time" — wall-clock phase adaptation.

### W6.3 — What made traces profitable: idiom-aware formation gates
- **Inner loops:** `rec_loop_interp` (`lj_record.c:582-600`): abort-and-wait for the inner loop to get its own trace; unroll the inner loop into the root trace *only* if it repeatedly failed to loop back (`innerloopleft`: penalty ≥ 2×PENALTY_MIN on LLEAVE/LINNER) **and** the unrolled prefix is short (≤24 IR insns, `J->loopref` check). Short, low-trip-count inner loops get unrolled; everything else becomes a linked trace.
- **Tail calls:** `lj_record.c:783-789` (comment): "Although it's true that tail calls can form a loop, the Lua programming idiom does not encourage this. Instead of counting tail calls towards the unroll limit … just rely on JIT_P_maxrecord to catch runaway tail-call loops." Budgets are spent where the language idiom puts the heat.

**New ideas mined (bar-checked):**

- **R11 — "Idiom-aware region-formation gates: link, don't unroll, nested loops" (E3).** When forming speculative regions, compile nested loops as separate linked regions by default; unroll/speculatively-inline the inner loop only if it is provably short (RaptorJIT: ≤24 IR insns) *and* low-trip-count; exclude idiom-rare patterns (there: tail-call loops; Go analog: `defer` in hot loops, `recover` paths) from speculation budgets, with a global runaway cap as the backstop. Bar: (a) sound — separate regions link through the baseline; (b) benchmark: nested-loop kernels (matrix multiply, image convolution — SURV-033's loop-versioning family) — bounds code-cache blowup from speculative unrolling; (c) E3; (d) not in SURV-001…060 (SURV-033 versions loops; nothing specifies the *formation* policy for nested loops); (e) gc unrolls by static heuristics only. **Bucket: WOULD-LIKE** (precedent ABLE — RaptorJIT/LuaJIT). **Raw.**
- **Complementary angle on P5 (not a new ID):** W6.2's policy numbers — per-pc doubling penalty (72 → 6M, 64 slots) + 1s wall-clock hotcount decay — a concrete instantiation of P5's failure-budget/decay design with wall-clock rather than GC cadence.

---

## W7 — ZJIT re-check (CRuby 4.0): the skewed-dispatch lattice

**New since wave 1** (ZJIT was THINKING/slides; now in-tree at `zjit/`):

### W7.1 — Seven-way receiver-resolution lattice with skew variants
`zjit/src/hir.rs:756-775` (`ReceiverTypeResolution`): `NoProfile / Monomorphic / Polymorphic / SkewedPolymorphic / Megamorphic / SkewedMegamorphic / StaticallyKnown`. The skewed variants carry the dominant `profiled_type` and get dominant-class speculation **with a guard + generic fallback** — even at polymorphic sites. `SkewedMegamorphic` is currently detected but not yet optimized ("may be optimized in the future", hir.rs:4582) — the frontier of the idea is visible.

### W7.2 — SKEW_THRESHOLD = 0.75
`zjit/src/distribution.rs:77`: `const SKEW_THRESHOLD: f64 = 0.75` — the dominant bucket must hold ≥75% of observations for the skewed verdicts. This number **converges independently** with .NET's GDV chain gate (75%, S3.3) and JSC's 2:1 double-vote (S2.6): three runtimes, same dominance threshold for "worth speculating on the winner".

### W7.3 — Kinded side exits + recompile-carrying guards; inlining knobs
`SideExitReason` (`hir.rs:626`): `GuardType, GuardShape, FixnumAddOverflow, …, PatchPoint(Invariant), …` — the JSC-ExitKind-shaped taxonomy. `Insn::GuardType` carries `recompile: Option<Recompile>` — guard miss ⇒ side-exit with an explicit recompile decision. Inlining defaults (`options.rs:34-52`): threshold 30, budget 200, max iterations (depth) 10, call threshold 30.

**New ideas mined (bar-checked):**

- **R8 — "Skewed-dispatch speculation: guard the dominant class at polymorphic sites" (E3+E4).** At call sites whose profile is polymorphic-but-skewed (dominant class ≥75% of observations), emit guard-dominant-class + inlined direct call, with the generic dispatch as fallback — *not* a ladder, not megamorphic-give-up. This extends C1 (monomorphic) and the backlog's devirt cascade into the realistic middle: most hot polymorphic sites have a winner. Bar: (a) sound — guard + generic fallback (diamond); (b) benchmark: `io.Writer` sites dominated by one impl (log-to-file hot path), `fmt.Fprintf` to a dominant writer; (c) E3+E4; (d) not in SURV-001…060 (C1/SURV-004 stop at monomorphic; .NET's ladder (S3.2) covers ≤5 without a dominance rule; nothing proposes dominant-class speculation with a generic fallback at poly sites); (e) gc+PGO can't guard. **Bucket: WOULD-LIKE** (precedents ABLE — ZJIT skewed variants, .NET 75% chain gate). **Raw.**
- **Complementary angle on C1/dev irt cascade (not a new ID):** W7.3's inlining numbers (threshold 30 / budget 200 / depth 10) as calibration data for guard+inline budgets.

---

## W8 — Maglev docs: what a fast tier deliberately does NOT do

**Source:** `docs/compiler/maglev/compiler-maglev.md` (+ `src/maglev/maglev-graph-builder.h` bailout hooks). **Bucket: ABLE.**

Design philosophy (doc, verbatim-shaped): a *fast* optimizing tier between Sparkplug and TurboFan — "10 times slower than Sparkplug but 10 times faster than TurboFan" (numbers are the doc's claims). Deliberate choices: **minimize phases** (Maglev's whole pipeline is ~7 steps vs TurboFan's dozens); **single IR** (no JS→Simplified→Machine lowering); **direct code generation** (each node emits via MacroAssembler, no instruction selection); **curated fast optimizations only** — inlining small functions, type guarding with deopt, Smi untag/retag. Everything else (loop nests, escape analysis, representation changes) is TurboFan's job. Speculation is feedback-driven (Ignition `FeedbackVector` → monomorphic assumptions) with `EagerDeoptInfo`/`LazyDeoptInfo` frame reconstruction. **Early bailout:** `BuildAbort(AbortReason)` in the graph builder — on unsupported bytecodes Maglev *refuses the function* rather than emitting low-quality speculative code (the tier's compile-time budget is the constraint, not peak code quality). Turbolev mode reuses Maglev's graph builder as Turboshaft's frontend.

Transfer: no new candidate (a mid-tier as a *throughput* story fails the Phase-5 bar — N3 already struck Sparkplug/Maglev-style tiers; this pass confirms the strike from the primary doc). Two durable policy data:
1. **Bailout discipline:** a fast tier must refuse to compile what it can't compile *well* — early abort beats slow generic code. If graalgo ever grows a fast JIT tier (E5), its bailout list is the design.
2. **Turbolev shape:** a fast graph-builder frontend feeding the optimizing backend reuses speculation work across tiers — relevant to E5's hybrid mode if tiering is ever built.

---

## W9 — Sorbet: quick negative

@ `15481bbc70595facb3d143d586936aca328de00a` (sparse: docs, gems). Sorbet is a static type checker + LSP (`docs/internals.md`: Parser → Desugar → Rewriter → LocalVars → Namer → Resolver → CFG → Infer). No runtime component, no JIT, no profile emission, no type-feedback artifact a JIT could consume; the `sorbet-runtime` gem is contract validation (`sig` blocks), not optimization. Its flow-sensitive inference (`Infer`) exists only to *check*. **First-class negative:** nothing runtime-optimization-relevant; Go already has the static types Sorbet retrofits onto Ruby.

---

## Cross-runtime synthesis: numbers that converge independently

| Policy | Instances | Value |
|---|---|---|
| Dominance threshold for "speculate on the winner" | ZJIT `SKEW_THRESHOLD` 0.75; .NET GDV chain gate 75%; JSC double-vote 2:1 | **≥75%** |
| Polymorphism cap before giving up | V8: 4 maps; .NET GDV: 5 (default 1); ZJIT: N buckets then mega; HHVM call checks: 6 | **a handful (4–6)** |
| Guard-failure response ladder | V8: megamorphic; JSC: kinded exit + doubling backoff; PyPy: bridge @200 + one-shot retrace; RaptorJIT: side trace ×(hotexit+tryside) then self-link blacklist; CPython: chain depth 4 + progress proof; HHVM: **widen the guard** | widen-vs-backoff is the open design choice (R7) |
| Phase-change adaptation | PyPy: decay/32 minor GCs; RaptorJIT: hotcount reset/1s; JSC: profile liveness gating | counters must expire |
| Invalidation registry | V8 CompilationDependencies; PyPy quasiimmut; CPython bloom-filter dependency sets | lossy-safe is acceptable |

## Near-misses (analyzed; fail the bar as standalone claims)

- **M1 — Swift DeadObjectElimination as a Go candidate.** Destructor-dependent; Go has none. Struck (W4.2).
- **M2 — pyston_lite as a whole-JIT transfer.** Copy-and-patch + quickening + attribute caching is compile-latency/dispatch-elimination engineering (N4-class); its transferable slivers are W2.1/W2.2 (filed as angles on P1/C1). Struck as a system.
- **M3 — Maglev as a throughput story.** Confirmed struck (N3) from the primary doc; kept only as tier-design discipline (W8).
- **M4 — Nitrous partial evaluation as a JIT architecture.** Never shipped; the fact-registry sliver is R6. Struck as architecture.

## Negatives (first-class; do not re-survey without a trigger)

1. **Cinder HIR JIT + Static Python — removed from the live tree** (2026-10-04). The repo is now upstream-CPython-shaped; only `Tools/jit` (PEP 744) remains. Trigger to re-survey: Meta announces a new optimizing JIT.
2. **Pyston LLVM JIT + nitrous/pystol — never shipped, repo frozen 2024-08-12.** Trigger: upstream commits resume.
3. **Swift move-only optimization exploitation — not found** (enforcement only, `Mandatory/` checkers). Trigger: a SIL pass keyed on move-only appears.
4. **SE-0474 post-acceptance optimizer movement — none found**; yielding accessors still experimental-gated. Trigger: proposal status change to shipped/implemented.
5. **TruffleRuby language-level speculation policy — none** (bounded ≤30 min check; Truffle DSL is Track 2's). Trigger: new options in `options.yml` mentioning speculation/guards.
6. **GraalPy JIT policy changes in 2026 — none** (CHANGELOG 25.x: only `-X jit=` presets). Trigger: changelog entry on JIT internals.
7. **Sorbet — type checker only** (2026-10-04). No runtime artifact. Trigger: none foreseeable; permanent unless Sorbet grows a runtime.

## Coverage notes (wave-3 T3 queue item 5 — complete)

All nine assigned runtimes read at source level with pinned commits: Cinder (tier-2 optimizer policy; HIR/Static-Python removal documented), Pyston (LLVM-era end confirmed; pyston_lite cache policy; nitrous facts), HHVM (region modes/caps, guard relaxation, retranslation chains, call-spec caps), Swift (CopyValueOpts, DeadObjectElimination, Devirtualizer, move-only/SE-0474 status), TruffleRuby/GraalPy (bounded negative), RaptorJIT (guard/snapshot discipline, penalty cache, formation gates), ZJIT (skewed lattice, 0.75, kinded exits, inline knobs), Maglev (philosophy doc + bailout discipline), Sorbet (negative). New raw candidates: **R2, R3, R6, R7, R8, R9, R11** (7). Complementary angles (no new IDs): P2-confidence-gating, invalidation-registry bloom design, P1 counted failures, C1 entry-hoisted guards + inline budgets, SURV-001 CHA shape, P5 wall-clock decay. Near-misses: M1–M4. Negatives: 7.

*(checkpoint: wave-3 T3 source passes complete — all 9 runtimes)*

---

## W5b — RaptorJIT re-run: JIT_PARAMDEF table, wall-clock decay, narrowing rationale

*Re-run pass (complements the earlier W6 trace-economics pass above): same repo @ `1d83bb4ec758`, different files.*

**Source:** `src/lj_jit.h` (JIT_PARAMDEF), `src/lj_dispatch.{h,c}`, `src/lj_trace.c`, `src/lj_opt_narrow.c` (rationale header comment). **Bucket: ABLE** (production tracing JIT; the narrowing rationale is a design-document-grade policy statement).

### W5.1 — Complete numeric policy table
`src/lj_jit.h:66-91` (`JIT_PARAMDEF`): `maxtrace=10000` (max traces in cache), `maxrecord=10000` (max recorded IR insns), `maxside=1000` (max side traces per root trace), `maxsnap=2000` (max snapshots per trace), `hotloop=56` (# iterations to detect a hot loop/call), `hotexit=10` (# taken exits to start a side trace), `tryside=4` (# of attempts to compile a side trace), `instunroll=4 / loopunroll=15 / callunroll=3 / recunroll=2`, `sizemcode=256` KB areas, `maxmcode=8192` KB total. `lj_dispatch.h:20-24`: 64-entry hotcount table (hashed by PC), loop iteration costs 2 (`HOTCOUNT_LOOP=2`), call costs 1 (`HOTCOUNT_CALL=1`) — i.e. ~56 loop iterations or ~112 calls trigger compilation.

### W5.2 — Wall-clock evidence decay
`lj_trace.c:78-89`: `hotcount_decay()` resets ALL hotcounts and clears ALL snapshot side-exit counts every 1 second of wall clock (`(ts - hotcount_decay_ts) > 1000000000LL`). Phase-change adaptation is wall-clock-driven, not GC-driven (compare PyPy's per-32-minor-GC decay, S4.4). Transferable to E4: profile/evidence freshness should decay on wall-clock time, not just on GC or compilation events.

### W5.3 — Side-trace attempt budget with saturation
`lj_trace.c:759-771`: a side exit that reaches `hotexit=10` starts side-trace recording, but only up to `tryside=4` attempts; `lj_jit.h:138`: `SNAPCOUNT_DONE=255` marks a side exit already compiled-and-linked (never retried). Converges with PyPy's `retrace_limit=0` (S4.2) and JSC's exponential backoff (S2.5): three runtimes independently bound re-speculation attempts to a small constant (0–4).

### W5.4 — The narrowing rationale: when value speculation does NOT pay
`lj_opt_narrow.c:21-100` (in-source design comment): Lua has one FP-double number type; LuaJIT does NOT eagerly narrow. The comment argues explicitly: eager speculative narrowing "causes many problems, like losing -0 or the need to resolve type mismatches" and forces overflow-checking integer semantics; "always replacing an FP op with an integer op plus an overflow check is counter-productive on a current-generation super-scalar CPU... you don't want to turn a pure data-flow dependency into a data-flow dependency plus a control dependency." Policy adopted: **predictive narrowing** for induction variables + **demand-driven narrowing** for index expressions, integer args, bit ops; "regular arithmetic computations are never narrowed to integers"; the resulting overflow guards are then *eliminated or hoisted* where possible. Numeric bounds: `NARROW_MAX_BACKPROP=100`, `NARROW_MAX_STACK=256` (bounded backpropagation for the narrowing proof).

**New ideas mined (bar-checked):**

- **Complementary angle on SURV-009 / SURV-033 (not a new ID):** W5.4 is the production-grade policy for Go's arithmetic specialization — narrow only where the narrowing proof is demand-driven/predictive (index expressions, induction variables, bit ops) and where the resulting check can be eliminated or hoisted; never narrow general arithmetic. The "don't turn a data-flow dependency into data-flow+control" argument is the counter-consideration the SURV-009 sketch must answer, and NARROW_MAX_BACKPROP=100 gives a bounded-analysis budget datum.
- **Complementary angle on P5/C7 (not a new ID):** W5.1–W5.3 give the concrete policy numbers — hotexit=10 → side compilation, tryside=4 attempts max, 1-second wall-clock evidence reset, maxside=1000 variants per root, SNAPCOUNT_DONE saturation. The wall-clock (vs GC-event) decay is the one datum P5 doesn't already name.

---

## W9b — Sorbet Compiler: typed-subset AOT with VM fallback (ATTEMPTED, abandoned)

*Re-run pass (refines the earlier W9 negative: current Sorbet is a checker-only; the 2021 compiler experiment was removed from the tree).*

**Source:** official announcement https://sorbet.org/blog/2021/07/30/open-sourcing-sorbet-compiler.html (Stripe, 2021-07-30); repo state `sorbet/sorbet` @ `15481bbc7059` — `git ls-tree HEAD --name-only -- compiler` returns nothing: the compiler directory is absent from the 2026 tree.

**Mechanism (as documented in the announcement):** Sorbet typechecks `.rb` files, emits a custom type-annotated IR, lowers to LLVM IR, produces native shared objects (`.so`) that are valid Ruby native extensions using the same object model and runtime representation as all Ruby code. For language features with no fast compilation strategy it "calls back into the Ruby VM" — i.e. typed fast path + VM fallback diamond, same shape as .NET's GDV (S3.1). Deployment: per-file opt-in via `# compiled: true` / `# compiled: false` magic comment; at runtime a support module monkeypatches `require_relative` to load the `.so` instead of the `.rb`.

**Bucket: ATTEMPTED.** Announced as "experimental" 2021; never graduated; the compiler was removed from the repo by 2026 (no deprecation note found in the pinned tree's docs). Failure reason (observed, not stated): it required dual artifacts (`.rb` + `.so`), per-file opt-in markers, and a patched require path — deployment friction plus the maintenance cost of a parallel LLVM backend for a dynamic language. This rhymes with Cinder's HIR lesson (W1) and Pyston's nitrous (W2): maintaining a second compiler for a dynamic language dies of friction, not of bad codegen.

**Transfer:** the *deployment* datum transfers, the backend doesn't. (1) Complementary angle on SURV-001: module-granular closed-world assertions (per-file `# compiled: true`) as an alternative to whole-binary closed-world — weaker assertion, smaller blast radius, deployable incrementally; our SURV-001 framing should consider a per-package flag form. (2) The failure lesson sharpens the bar: any Go AOT mode must be a single build flag with static detectability (SURV-001 already does this via the `plugin` check) — never per-file markers or dual artifacts.

---

## W6b — ZJIT second pass: invalidation by patch-in-place, version/threshold policy

*Re-run pass (complements the earlier W7 skewed-dispatch pass above): same repo @ `b61936bc2915`, invalidation mechanism + versioning policy.*

**Source:** `ruby/ruby` @ `b61936bc2915`, `zjit/src/{invariants.rs,hir.rs,options.rs,distribution.rs}`. **Bucket: ABLE** (ZJIT ships in CRuby 4.0; method-based SSA JIT). SURV-070 already took the skewed-dispatch policy; this pass covers the invalidation mechanism and versioning policy, which SURV-070 did not.

### W6.1 — Invalidation = patch the guard site, not deopt the version
`zjit/src/invariants.rs:1` ("Code invalidation and **patching** for speculative optimizations"): every speculation that depends on VM state registers a `PatchPoint { patch_point_ptr, side_exit_ptr, version }` in a per-invariant-kind table (`bop_patch_points`, `cme_patch_points`, `constant_state_patch_points`, `no_trace_point_patch_points`, `no_newobj_hook_patch_points`, `single_ractor_patch_points`, `no_singleton_class_patch_points`, `root_box_patch_points`). When the invariant breaks, `compile_patch_points!` rewrites the guard site **in place** to `jmp side_exit_ptr` (`invariants.rs:20-37`) — the compiled version is not discarded and no frame is deopted; the guard permanently self-downgrades to the fallback. Distinct from V8 (deopt + discard), JSC (OSR exit + recompile), PyPy (bridge): ZJIT's answer to a broken assumption is a 1-instruction patch.

### W6.2 — The invariant taxonomy is the "what do we assume" list
`hir.rs:199-224` (`enum Invariant`): `BOPRedefined`, `MethodRedefined { klass, method, cme }`, `StableConstantNames`, `NoTracePoint`, plus (`invariants.rs`) `NoNEWOBJ` hook (the inline-allocation fast path bypasses `rb_newobj`, so it assumes no NEWOBJ hook is installed — invalidated when one is enabled), single-ractor, no-singleton-class, root-box. Transferable shape for Go's assumption registry: the taxonomy mixes *language* facts (method sets — trivially stable in Go), *runtime* facts (hooks/profilers enabled — Go analog: CPU profiler, traceback capture, `GODEBUG` switches), and *allocation* facts (fast path assumes no allocation observer). Side exits still exist (`Snapshot` + frame reconstruction, `hir.rs:1168,7385`) — the patch-point mechanism handles *assumption* invalidation; *value* guard failures take side exits.

### W6.3 — Version and threshold policy numbers
`options.rs:18-62`: `DEFAULT_MAX_VERSIONS = 4` (max compiled versions per ISEQ), `DEFAULT_CALL_THRESHOLD = 30` (calls before JIT compile; profiling starts at threshold − num_profiles), `DEFAULT_INLINE_THRESHOLD = 30` (callee size in YARV bytecodes), `DEFAULT_INLINE_MAX_ITERATIONS = 10`. Cross-runtime convergence datum: ZJIT's 4 versions, CPython's `MAX_CHAIN_DEPTH = 4` (W1.2), V8's poly cap 4 (S1.1) — three independent runtimes cap speculative versioning at 4.

**New ideas mined (bar-checked):**

- **R10 — "Patch-in-place guard self-downgrade on assumption invalidation" (E3).**
  *(propose SURV-073 at synthesis)* When a registered assumption breaks (method redefined — n/a in Go; runtime hook enabled; profiler attached; `GODEBUG` switch flipped; new `plugin` implementation appears), rewrite the guard site in place to jump to the fallback instead of deopting or discarding the compiled version. Bar: (a) sound — the side-exit/fallback path is semantics-preserving; the patched code never executes the stale fast path again; (b) benchmark: workloads with rare invalidation events (config reload under SURV-067, profiler attach/detach, plugin load) — measures "cost of being wrong once" vs deopt; (c) E3 (the invalidation registry needs an *invalidation action*; this is cheaper than deopt); (d) not in SURV-001…072 (the registry is §4 infra with no specified invalidation action; nothing proposes patch-in-place); (e) gc has no speculation to invalidate. **Bucket: WOULD-LIKE** (precedent ABLE — ZJIT). **Raw.**
- **Complementary angle on the speculation-policy table (not a new ID):** W6.3's numbers — 4-version cap (the "rule of 4" convergence), call threshold 30, inline threshold 30 bytecodes / max depth 10 — feed the policy table alongside S1–S4/W1/W5 numbers.

---

## W7b — V8 Maglev docs re-read: eager/lazy deopt, Turbolev frontend

*Re-run pass (complements the earlier W8 Maglev pass above): same docs @ `7dce5a4a3242`.*

**Source:** `v8/v8` @ `7dce5a4a3242`, `docs/compiler/maglev/compiler-maglev.md` + `compiler-maglev-regalloc.md` (192 lines total). **Bucket: ABLE** (Maglev ships as V8's mid-tier).

**What the docs establish:** Maglev is a single-IR (no multi-tier lowering) mid-tier compiler that speculates on types from the Ignition `FeedbackVector`, with eager deopt (check failure → `EagerDeoptInfo` frame reconstruction) and lazy deopt (on return, environment changed). Nodes like `Int32AddWithOverflow` fuse the op with its deopt check. Regalloc handles deopt inputs in an "Allocate Deopts" phase (loadable inputs for eager/lazy deopt).

**Negative (first-class):** the docs contain no speculation *policy* (no thresholds, caps, counters, retry rules) — the policy lives in TurboFan/IC code already mined in wave 2 (S1). Nothing here adds to the miss/retry/deopt policy table.

**One transferable architecture datum (not a new ID — complementary angle on C7/SURV-037):** **Turbolev** mode (`--turbolev`): Maglev's fast graph builder + speculatively-optimized IR is reused as the *frontend* for the Turboshaft optimizing backend. I.e. speculation decisions (guard placement, type facts from the feedback vector) are made once, fast, and shared across optimization tiers instead of each tier re-deriving them. For our JIT tiering (C7 phase-adaptive rejit): a single speculation frontend feeding both the quick tier and the deep tier keeps guard placement consistent across re-jits and amortizes profiling/analysis cost. Filed as a C7 design angle, not a standalone candidate (compile-latency alone fails the Phase-5 bar; the shared-speculation-facts point is the transferable part).

---

## W4b — Swift compiler second pass: devirtualization ordering, write-once promotion, @_specialize

*Re-run pass (complements the earlier W4 SemanticARC/CHA pass above): same repo @ `6b8daddf6f30`, different passes.*

**Source:** `swiftlang/swift` @ `6b8daddf6f30`, `lib/SILOptimizer/{Utils/Devirtualize.cpp, IPO/LetPropertiesOpts.cpp, Transforms/EagerSpecializer.cpp}`. **Bucket: ABLE** (swiftc ships). SURV-071 already took SemanticARC borrow-scope; this pass covers what it didn't.

### W4.1 — Speculative devirtualization test ordering (CHA-based, static)
`Utils/Devirtualize.cpp:46-57`: when a class method can't be statically devirtualized, swiftc emits a *speculative* type-test cascade over the class hierarchy; subclasses are tested breadth-first with "the exact static type of the instance tested first." The ordering policy is explicit: most-likely-first, not declaration order. Transferable to our devirt cascade (backlog) and SURV-070: order the guard ladder by profile weight / static-type-first, not by discovery order. (Converges with .NET's 75% chain gate S3.3 and HHVM's `PGORelaxPercent` W3.2 — profile-ordered ladders.)

### W4.2 — Write-once field promotion (LetPropertiesOpts)
`IPO/LetPropertiesOpts.cpp:1-20`: for non-static `let` properties, if the pass can prove it analyzed *all* initializers and they assign the *same constant value*, loads of the property are replaced by the constant — no guard, pure static proof. (The header comment is admirably honest about the soundness hole: layout exposure via other modules can defeat the "all initializers seen" proof unless library-evolution/frozen rules hold — a cautionary tale for any closed-world proof.)

### W4.3 — @_specialize: programmer-directed eager specialization (negative for Go)
`Transforms/EagerSpecializer.cpp:18-24`: functions annotated `@_specialize(<type list>)` are cloned per listed concrete type; dispatch code is emitted at call sites. This is a *language feature* (programmer names the types), not an optimization discovery — Go generics already monomorphize per instantiation automatically, so there is nothing to transfer. Recorded as negative.

**New ideas mined (bar-checked):**

- **Complementary angle on SURV-067 (not a new ID):** W4.2 is the *static* sibling of SURV-067's speculative bounded-tolerance guards: where whole-program analysis proves write-once-with-constant (package-level `var x = f()` with a single initializer, struct fields assigned once in the constructor and never elsewhere — provable in closed-world AOT), fold the loads **guard-free**; reserve SURV-067's mutation-counter guards for the unprovable rest. The two compose: static proof first, speculation where the proof fails. Also note the LetPropertiesOpts soundness caveat as a warning for our own closed-world proofs (layout/visibility holes).
- **Complementary angle on the devirt cascade + SURV-070 (not a new ID):** W4.1's test-ordering rule (static type first, then breadth-first over the hierarchy; profile-ordered in the PGO case) — the ladder-ordering policy none of the existing candidates specify.

---

## W8b — TruffleRuby + GraalPy deep source pass (language level)

*Re-run pass (goes below the earlier W5 bounded-check negative: per-kind IC limits, @InliningCutoff, GilNode, frame-linking assumptions).*

**Sources:** `oracle/truffleruby` @ `c734f2654300` (`src/main/java/org/truffleruby/{language,options}`, `doc/`); `oracle/graalpython` @ `178ea261764d` (`docs/contributor/IMPLEMENTATION_DETAILS.md`). **Bucket: ABLE** (both ship on GraalVM). T2's resweep covered these at project/changelog level only (negative: no 2026 JIT features) — this pass is source-level, below changelog granularity.

### W8.1 — Per-kind configurable IC limits + inlining-cutoff fallbacks (TruffleRuby)
`language/methods/LookupMethodNode.java:46-77`: method-lookup cache = guards on `(metaClass, name, config)` + `assumptions = "methodLookupResult.getAssumptions()"` + `limit = "getCacheLimit()"`; the overflow fallback is `@ReportPolymorphism.Megamorphic @InliningCutoff` — the megamorphic path is explicitly marked so the host compiler does not inline through it (keeps compiled code small). Cache limits are per-kind language options, not one global: `METHOD_LOOKUP_CACHE` (`options/LanguageOptions.java:70`), `DISPATCH_CACHE` (:72), `YIELD_CACHE` (`language/yield/CallBlockNode.java:132-134`). Numeric defaults live in the Truffle framework's `OptionsCatalog` (not in this sparse checkout — not claimed here).

### W8.2 — Assumption-gated frame-linking elision (GraalPy)
`docs/contributor/IMPLEMENTATION_DETAILS.md:37`: `PFrame.Reference` creation (caller) and linking (callee) are skipped under Truffle Assumptions held on both the calling node and the callee root node; the assumption is invalidated the first time the reference is actually needed (traceback/exception materialization). The discipline is *assumption, not guard*: no per-execution check — the rare consumer invalidates compiled code instead.

### W8.3 — Profile-driven synchronization boundary elision (GraalPy GilNode)
`IMPLEMENTATION_DETAILS.md:178`: `GilNode` "internally uses profiles to ensure that if we are always running single threaded or always own the GIL already when we get to a specific message we never emit a boundary call to lock and unlock the GIL" — and "we may deopt in some places if, after compiling some code, we later start a second thread." I.e. speculation on *threading behavior*, not types: the profile fact is "this site never sees a second thread"; the guard is the thread-arrival event; the fallback is deopt.

**New ideas mined (bar-checked):**

- **R12 — "Speculative contention-free lock fast path" (E2+E3+E4).**
  *(numbered R12: the earlier pass's R11 is the region-formation candidate already ranked as SURV-072; propose SURV-074 at synthesis)* At a `sync.Mutex` (or channel-op) site whose contention profile says "never contended / effectively single-goroutine" (the overwhelmingly common shape: per-connection state guarded by a mutex touched by one goroutine), emit a fast path that skips the atomic CAS and records ownership via a cheap epoch/goroutine check; any arrival of a second contender invalidates (deopt to the real lock path). Bar: (a) sound — the epoch check is the guard; violation falls back to the real lock, never UB; (b) benchmark: mutex-protected per-connection/per-request hot paths in `net/http`-shaped servers vs gc+PGO; (c) E2 (our scheduler knows goroutine identity and can cheaply signal "second contender arrived") + E3 (guard+deopt) + E4 (contention profile is the arming signal); (d) not in SURV-001…072 — SURV-023 is *static* coarsening of adjacent same-mutex regions; nothing proposes *speculative* contention elision; (e) gc always emits the atomic. **Bucket: WOULD-LIKE** (precedent **ABLE** — GraalPy GilNode; related **ATTEMPTED** — HotSpot biased locking, removed JDK 18/JEP 374: its obstacle was global revocation-protocol cost at safepoints; this routes around it with per-site guard+deopt, no global protocol). **Raw.**
- **Complementary angle on C1/C4/P1 (not a new ID):** W8.1's two policy points — (1) per-*kind* IC limits (dispatch vs method-lookup vs yield) rather than one global cap: different site kinds have different guard costs and hit rates; (2) `@InliningCutoff` on the megamorphic/uncached fallback: the slow path must be fenced off from the inliner, or megamorphic sites bloat every caller. Both are directly applicable to Go's interface-dispatch and map-slot IC design.
- **Design note for the E3 machinery (not a new ID):** W8.2's assumption-not-guard discipline — for bookkeeping whose only consumer is a rare event, skip it under an assumption invalidated by the event, instead of guarding per execution. Candidate Go applications (defer-chain linking for cold defers, traceback metadata) need a dedicated mechanism sketch before this becomes a candidate; recorded as a principle, not a proposal.

---

## Re-run coverage summary (W4b/W5b/W6b/W7b/W8b/W9b — 2026-10-04)

**New raw candidates (bar-checked, propose at synthesis):**
- **R10 → propose SURV-073** — "Patch-in-place guard self-downgrade on assumption invalidation" (WOULD-LIKE; precedent ABLE — ZJIT `invariants.rs`). §W6b.
- **R12 → propose SURV-074** — "Speculative contention-free lock fast path" (WOULD-LIKE; precedent ABLE — GraalPy `GilNode`; related ATTEMPTED — HotSpot biased locking/JEP 374, obstacle routed around via per-site guard+deopt). §W8b.

**New ATTEMPTED failure lesson (→ [SURVEY.md](SURVEY.md) §2):** Sorbet Compiler (Stripe): experimental typed-Ruby AOT (LLVM, native-extension ABI, per-file `# compiled: true` opt-in, VM fallback for uncompilable features); announced 2021, removed from `sorbet/sorbet` by 2026. Observed failure mode: deployment friction (dual artifacts, per-file markers, patched require path) + parallel-backend maintenance cost — same family as Cinder HIR and Pyston nitrous. Transfer: module-granular closed-world as a SURV-001 deployment angle; hard bar that any Go AOT mode is one build flag, never per-file markers. §W9b.

**Complementary angles (no new IDs):** RaptorJIT narrowing policy → SURV-009/SURV-033 (demand-driven/predictive narrowing only; NARROW_MAX_BACKPROP=100); RaptorJIT JIT_PARAMDEF numbers → SURV-055 policy table (hotexit=10, tryside=4, 1s wall-clock decay, maxside=1000); Swift devirt test-ordering → devirt cascade/SURV-070 (static-type-first, profile-ordered ladders); Swift LetPropertiesOpts → SURV-067 (static write-once proof → guard-free folding; speculation for the rest); TruffleRuby per-kind IC limits + @InliningCutoff → C1/C4/P1; Maglev Turbolev → C7/SURV-037 (shared speculation frontend across tiers).

**Negatives confirmed/extended:** Maglev docs carry no speculation policy (W7b); Swift `@_specialize` is a language feature, not transferable (W4b); TruffleRuby numeric IC defaults live in the framework dep (not claimed).

**Reconciliation with the earlier run's record:** the earlier W5 bounded check on TruffleRuby/GraalPy reported "nothing below changelog granularity" — the deeper W8b pass found language-level policy (per-kind limits, @InliningCutoff, GilNode, frame-linking assumptions); no contradiction, different depths. The earlier W9 Sorbet negative ("checker only") stands for current Sorbet; W9b adds the removed-compiler history. Section numbering W4b…W9b disambiguates the two records.

*(checkpoint: W4 re-run complete — all assigned sources mined at pinned commits)*
