# Track 3 — Dynamic-language runtimes (V8 / JavaScriptCore / .NET / Cinder / Pyston / HHVM / Swift / Ruby YJIT-ZJIT / LuaJIT)

**Owner:** Track-3 subagent, graalgo Phase-5 optimization survey.
**Mission:** mine speculation-heavy dynamic-language runtimes for techniques portable to Go semantics.
**Phase-5 rule:** every claimed optimization must beat stock gc **with PGO** on a named benchmark, or the claim is struck.
**Standing directive (program owner, 2026-10-04):** LONG-TERM background endeavor, not a time-boxed report. Wave 1 = this file's current contents. Every major runtime gets its own systematic pass from PRIMARY sources (engineering blogs, design docs, source comments), not secondary summaries. Negative results are first-class. "Coverage notes" at the end drives wave 2.
**Standing constraint:** Go is statically typed, so *type* speculation is mostly unnecessary. What transfers is *value, layout, branch, and representation* speculation plus the *deopt discipline* — stated explicitly per candidate via a `Transfer:` line (what transfers / what does NOT transfer).
**Already in backlog (do NOT propose; note complementary angles only):** speculative interface devirtualization cascade; profile-guided struct field reorder/pack; speculative AoS→SoA; type narrowing + bit-packing; speculative data representation (map specialization, unboxing); small string optimization; dynamic per-site string interning; inferred store-once values; idle-time repack/defrag/shrink; adaptive concurrency primitives; vectorization; always-on profiling; partial escape analysis.
**gc-excluded (do NOT list):** basic inlining, gc's escape analysis, bounds-check elimination.
**Evidence honesty:** "blog claims X" ≠ "we verified X". Others' numbers are never presented as our measurements.

## Bucket system (per program-owner refinement; maps to brainstorm names able-to-do / thinking-to-do / attempted-and-why-failed / would-like-to-do)

- **ABLE**: shipped in production today.
- **THINKING**: roadmaps / active proposals not yet shipped.
- **ATTEMPTED**: tried and abandoned/reverted, WITH the reason — the failure reason names the obstacle; stated explicitly when our architecture (descriptors, scheduler signals, speculative JIT) routes around it.
- **WOULD-LIKE**: wishlist writeups, "we wish the runtime did X".
- Every candidate below carries `Bucket:` (the candidate's own status) plus the bucket of its precedent — they compose (e.g. WOULD-LIKE candidate on an ABLE precedent; an ATTEMPTED item whose obstacle was dynamic typing transfers cleanly to static Go).

Architectural edges (cite ≥1 per candidate):
1. Descriptor-based value model — representation ≠ semantics; per-value or per-allocation-site layout/storage decisions, not type-global.
2. User-space goroutine scheduler with precise runtime signals (lock-holder-running, waiter counts, true idleness).
3. Speculative JIT with guards + deoptimization.
4. Planned always-on profiler (field-access patterns, array-access patterns, value ranges, representation stability, store counts).
5. Three execution modes: JIT-only, pure AOT, hybrid AOT+JIT.

---

## Candidates (wave 1)

### C1 — Speculative monomorphic func-value call sites (guard + direct call + inline)
**Bucket:** WOULD-LIKE (precedents: ABLE — V8 call ICs; ABLE — .NET dynamic-PGO guarded devirtualization)
**Idea:** At indirect-call sites through func values, the JIT caches the observed target. Guard = one pointer compare on the func value; on hit, direct-call and inline the body. Miss → generic indirect call (diamond speculation, no deopt) or deopt + reprofile.
**Precedent:** V8 inline caches for calls, states uninitialized→monomorphic→polymorphic→megamorphic (https://github.com/jayakrishnanmurali/ghostkode/blob/HEAD/content/v8-hidden-classes-inline-caches.mdx); .NET dynamic PGO emitting a type-checked direct call with the virtual call as fallback (https://devblogs.microsoft.com/dotnet/performance-improvements-in-net-11/).
**Transfer:** Transfers the call-IC discipline and the guard+direct-call shape. Does NOT transfer: hidden-class checks — a Go func value's identity IS the guard (one pointer compare; no shape polymorphism) — and this is NOT the backlog's interface devirtualization (different dispatch mechanism: func values, not itabs).
**Why gc doesn't do it:** no speculation infrastructure; an indirect call through a func value is opaque to the optimizer; PGO cannot invent guards.
**Architectural fit:** edge 3 (speculative JIT with guards).
**Benchmark sketch:** `sort.Slice` / `slices.SortFunc` with a fixed comparator closure in a tight loop — comparator inlined, call overhead gone, loop optimizable. Secondary: `strings.Map`-style mapper funcs.
**Open risks:** megamorphic sites (e.g. per-request-varying handler funcs) must not pessimize — needs a polymorphism cap + fallback; guard placement must not blow inlining budgets.

### C2 — Value-profile-driven arithmetic specialization
**Bucket:** WOULD-LIKE (precedents: ABLE — CPython PEP 659; ABLE — JSC value profiling)
**Idea:** The always-on profiler records *value* facts at hot integer ops (divisor constant, shift-amount constant, operand ranges). The JIT emits strength-reduced code (div-by-profile-constant → multiply-by-magic; `x << k`) behind a value guard; miss → generic op (diamond, no deopt).
**Precedent:** PEP 659's `BINARY_OP` specialization families with guards and de-specialization on repeated failure (https://peps.python.org/pep-0659/); JSC value profiling feeding speculation (https://webkit.org/blog/10308/speculation-in-javascriptcore/).
**Transfer:** Transfers "specialize the operation for observed operand properties" plus the saturating-counter de-specialization policy. Does NOT transfer: type speculation — Go operands are statically typed; we speculate on *values*, never types.
**Why gc doesn't do it:** strength reduction requires compile-time-constant divisors; profile-constants are invisible to AOT.
**Architectural fit:** edges 3 + 4 (value-range profiling).
**Benchmark sketch:** hash-table probe `h % nbuckets` with runtime-sized tables; image row-stride division `i / stride`; any hot 64-bit div/mod by a runtime-stable value (div ~30–90 cycles → multiply ~3 cycles).
**Open risks:** guard-branch mispredict cost if the value is less stable than profiled; magic-number codegen must be proved exact (differential tests against runtime div).

### C3 — Allocation-site capacity speculation (profile-observed pre-sizing)
**Bucket:** WOULD-LIKE (precedent: ABLE — V8 per-allocation-site backing-store policy)
**Idea:** Profiler records observed max length per allocation site (`make`/`append`/literals). The JIT pre-sizes backing stores to the observed max; if exceeded, normal growth takes over — semantics-preserving, no deopt needed.
**Precedent:** V8's per-array elements-kind/backing-store policy decided per allocation site, generalizing one-way on violation (https://github.com/v8/v8.dev/blob/HEAD/src/blog/elements-kinds.md).
**Transfer:** Transfers per-allocation-site (not per-type) representation decisions + one-way generalization on violation. Does NOT transfer: the kinds lattice itself — Go element types are static; no SMI→double→dictionary transitions, no holes.
**Why gc doesn't do it:** allocation sizing has no profile-feedback path; growth policy is fixed in the runtime.
**Architectural fit:** edges 1 (per-site representation) + 4 (length profiles).
**Benchmark sketch:** `encoding/json` unmarshal into `map[string]any` (many small maps); `net/http` header parsing; append-loops with stable small N.
**Open risks:** over-allocation on unrepresentative profiles (memory bloat) — needs hysteresis (size at p99, not max) and a shrink-back policy.

### C4 — Inline-cached map slot lookup for constant keys
**Bucket:** WOULD-LIKE (precedent: ABLE — inline-cache discipline in V8/JSC/CPython). Complementary to backlog map specialization (representation vs access path).
**Idea:** For repeated `m[K]` with constant K, cache (map → bucket, slot). Guard: `B` and `count` unchanged since specialization + tophash/key revalidation at the slot. Hit → direct value load, no hash walk. Miss → generic lookup (diamond; optionally re-specialize).
**Precedent:** the inline-cache discipline generally — cache + guard + miss fallback (V8 IC states; PEP 659 `LOAD_ATTR_*` families, https://peps.python.org/pep-0659/).
**Transfer:** Transfers the IC discipline. Does NOT transfer: hidden classes / type-version tags — Go maps have no shape polymorphism; the guard is on *table state* (B, count) plus slot key revalidation. Sound because Go never moves entries except on grow (B change ⇒ guard fails) and delete marks the slot (key check fails ⇒ fallback).
**Why gc doesn't do it:** map access is an opaque runtime call; no per-site caching; no guard infrastructure.
**Architectural fit:** edges 3 + 4.
**Benchmark sketch:** config/metadata lookups (`cfg["timeout"]`) in request hot paths; repeated header/field extraction loops.
**Open risks:** best for integer keys / short string keys (key-revalidation cost); must not regress megamorphic-key sites — needs per-site polymorphism cap.

### C5 — Speculative loop unswitching / branch hoisting with OSR exit
**Bucket:** WOULD-LIKE (precedents: ABLE — JSC OSR-exit speculation; ABLE — YJIT lazy basic-block versioning)
**Idea:** Profile says a branch inside a hot loop is never (or rarely) taken — feature flags, `if err != nil`, debug checks. The JIT hoists the check to a single loop-entry guard; the loop body becomes branchless. Guard failure → OSR exit to the unoptimized version. With no control-flow merge, downstream checks become redundant (JSC's key OSR-exit property).
**Precedent:** JSC OSR-exit speculation — "OSR speculation has higher benefit"; after a hoisted check the compiler treats the fact as established (https://webkit.org/blog/10308/speculation-in-javascriptcore/); YJIT's Lazy Basic Block Versioning, specializing paths incrementally on observed facts (https://github.com/chiditarod/dogtag/blob/HEAD/docs/ruby-3.1-released.md).
**Transfer:** Transfers the OSR-exit speculation model + incremental block versioning. Does NOT transfer: type speculation — we speculate on *branch outcomes*.
**Why gc doesn't do it:** PGO can lay out cold blocks but cannot delete the branch; nothing justifies elimination without deopt.
**Architectural fit:** edges 3 + 4 (branch profiles).
**Benchmark sketch:** request-handler loops with feature-flag branches; checksum/hash loops with never-taken error checks; batch loops with per-item `if debug` logging checks.
**Open risks:** needs real OSR-exit machinery (deopt infra); phase changes in branch behavior cause exit storms → needs a re-versioning policy with hysteresis (ties to C7).

### C6 — Closed-world whole-program AOT (repo-authoritative analog)
**Bucket:** WOULD-LIKE (precedents: ABLE — HHVM repo-authoritative + HHBBC; ABLE — Swift SE-0193; ATTEMPTED — HPHPc, whose obstacle is absent in Go — see Transfer)
**Idea:** Pure-AOT mode with a build-time closed-world assertion (no `plugin` import — statically detectable): whole-program analysis enumerates all interface implementations — Go cannot create new named types at runtime — then devirtualizes every interface call, inlines cross-package, and drops dead methods. No guards needed: this is sound static analysis, not speculation.
**Precedent:** HHVM repo-authoritative mode — "assumes that no code exists outside of what's already in the currently-loaded repo, allowing much more aggressive optimizations" (https://github.com/facebook/hhvm/blob/HEAD/hphp/doc/hackers-guide/glossary.md), plus HHBBC whole-program bytecode optimization; Swift SE-0193 cross-module inlining/specialization as the mechanism shape (https://github.com/swiftlang/swift-evolution/blob/main/proposals/0193-cross-module-inlining-and-specialization.md).
**Transfer:** Transfers the closed-world assumption + whole-program analysis. Does NOT transfer: HHVM's speculative guards (unnecessary — Go's implementation set is statically enumerable) or PHP-specific dynamic handling. ATTEMPTED datapoint: HPHPc (PHP→C++ transpiler) was abandoned because dynamic-language features (eval, dynamic includes) defeated static whole-program compilation — that obstacle does not exist in statically-typed Go; the only hole is `plugin`, which is statically detectable. The failure reason names the obstacle, and our setting routes around it.
**Why gc doesn't do it:** separate compilation (one package at a time); the runtime must support `plugin`; the linker has no whole-program view.
**Architectural fit:** edge 5 (pure AOT mode) + edge 1.
**Benchmark sketch:** `net/http` JSON API server (Handler chains, io.Reader/Writer indirection); `database/sql` with driver indirection; any interface-heavy service binary.
**Open risks:** `plugin` users must get a loud build error, never a silent pessimize; reflect-based method invocation keeps working (itabs still exist) but won't devirtualize; cross-package inlining needs code-bloat budgets.

### C7 — Phase-adaptive recompilation (rejit on profile shift)
**Bucket:** WOULD-LIKE (precedents: ABLE — .NET tiered rejit; ABLE — JSC re-optimization on OSR exits)
**Idea:** In JIT mode, keep Tier0-style lightweight probes live after Tier1 compilation. When the profile shifts significantly (new hot path, changed branch behavior, new receiver types), rejit with fresh speculation. gc+PGO is frozen at build time and cannot follow phase changes — this is a win *only* a live JIT can claim.
**Precedent:** .NET tiered compilation (Tier0 probes → Tier1 rejit) and "recompile when new interface implementations arrive on scene" (https://github.com/dotnet/runtime/blob/main/docs/design/features/OnStackReplacement.md); JSC re-optimization triggered by OSR-exit counts (thresholds double on each recompile).
**Transfer:** Transfers the probe→rejit loop and invalidation triggers. Does NOT transfer: the *startup-latency* rationale for tiering (AOT gc already wins startup); the transfer is *phase adaptation*.
**Why gc doesn't do it:** AOT + static PGO; no runtime code regeneration exists.
**Architectural fit:** edges 3 + 5 (JIT mode).
**Benchmark sketch:** long-running server with shifting workload phases — e.g. an API gateway whose hot endpoint mix changes diurnally, or a batch pipeline with distinct phases. Measure: stale-PGO gc vs rejitting JIT across a phase change.
**Open risks:** rejit storms on flapping profiles → hysteresis + cooldowns required (adopt JSC's double-thresholds-on-recompile policy); code-version memory churn.

### C8 — Speculative generic-instantiation specialization
**Bucket:** WOULD-LIKE (precedent: ABLE — Swift generic specialization + SE-0460)
**Idea:** Go generics compile via GC-shape stenciling (one version per shape). The JIT additionally creates per-concrete-type versions at hot call sites for *observed* type args, guarded on the type argument, with the body specialized and inlined — e.g. devirtualizing dictionary-passed method calls on type parameters. Profile data = automatic `@specialized`.
**Precedent:** Swift: "if the generic function's definition is visible to the optimizer and the concrete type is known, the compiler emits a version specialized to the specific type"; SE-0460 `@specialized` makes it explicit/pre-declared (https://github.com/mihaelamj/cupertino-docs/blob/HEAD/swift-evolution/SE-0460.md).
**Transfer:** Transfers per-concrete-type specialization driven by *observed* (not just statically visible) type arguments. Does NOT transfer: Swift's witness-table model — Go uses GC-shape stenciling/dictionaries; the guard is on the concrete type argument.
**Why gc doesn't do it:** stenciling is per GC-shape, not per exact type; no guards exist; cross-shape method-call devirtualization is impossible.
**Architectural fit:** edges 3 + 5.
**Benchmark sketch:** generic numeric kernels over `constraints.Integer`/`Float` with runtime-chosen element type; `slices.SortFunc` on concrete element types with inlined comparators; generic container hot paths (e.g. a generic LRU with `comparable` K).
**Open risks:** code bloat — cap versions per site, share with AOT-stenciled versions when identical; type-polymorphic sites fall back to the stenciled version.

### C9 — Map read-modify-write fusion via borrowed interior pointer
**Bucket:** WOULD-LIKE (precedent: THINKING — Swift SE-0474 yielding accessors, Accepted, behind `-enable-experimental-feature CoroutineAccessors`)
**Idea:** `v := m[k]; …; m[k] = v+delta` with no intervening map mutation is two hash lookups today. The JIT fuses them: one lookup yields a borrowed interior pointer held across the region; the write goes in place. Sound when the JIT proves (or guards, C4-style on `(B,count)`) no intervening mutation — and within one goroutine Go maps cannot be touched concurrently without a fatal error anyway.
**Precedent:** Swift SE-0474 `yielding mutate` / `yielding borrow` accessors — "lend" access to a value without get-copy-set (https://github.com/swiftlang/swift-evolution/blob/main/proposals/0474-yielding-accessors.md).
**Transfer:** Transfers the borrow-during-mutation idea. Does NOT transfer: the coroutine machinery or any language change — Go needs no new syntax; the JIT implements it as a fused intrinsic.
**Why gc doesn't do it:** map elements are not addressable; each access is an independent opaque runtime call; no cross-access fusion.
**Architectural fit:** edges 1 + 3.
**Benchmark sketch:** histogram loops (`freq[w]++`); accumulator maps (`totals[k] += delta`); counter patterns.
**Open risks:** the no-intervening-mutation proof must be airtight — calls in between that could touch the map force a guard or a bail; the borrowed pointer must not escape the region (interior pointer is GC-safe as long as the map SSA value keeps it alive).

## Near-misses (analyzed; fail the bar as standalone claims)

- **N1 — Loop-backedge OSR as a standalone win.** Precedent ABLE (V8/JSC/.NET/Pyston all do it). Needed so long-running loops in cold functions (e.g. `main`'s event loop) ever reach optimized code in JIT mode — but it is parity-enabling infrastructure, not a win *over* gc+PGO. Becomes a win only combined with speculation (C2/C5). Keep as required JIT-mode machinery, not a ranked candidate.
- **N2 — Type-switch monomorphic fast path.** gc's type-switch lowering (sequential itab compares / hash dispatch) is already decent; the speculative version (guard dominant case, inline case body) adds only branch-ordering + I-cache effects over gc+PGO. Marginal; struck as standalone.
- **N3 — Sparkplug/Maglev-style baseline tiers as a throughput story.** ABLE precedent; buys warmup/startup latency only. Cannot beat AOT gc on throughput. Relevant to JIT-mode latency budgets, not Phase-5 claims.
- **N4 — Copy-and-patch / DynASM template JIT.** ABLE (CPython 3.13+, LuaJIT). Compile-*latency* technique; worse code than an optimizing backend by design. No throughput claim vs gc+PGO.
- **N5 — JSC-style polyvariant inlining as a standalone claim.** Per-call-site multi-version specialization of the *same* statically-typed callee is marginal vs gc+PGO inlining; the valuable part (different *value* assumptions per version) is C5/C8.
- **N6 — HHVM region-based JIT compilation.** ABLE (https://research.facebook.com/publications/hhvm-jit-a-profile-guided-region-based-compiler-for-php-and-hack/). Region selection across function boundaries ≈ Graal's inlining scope; no distinct transfer beyond what Truffle/Graal already does.
- **N7 — .NET deferred compilation (compile hot parts first, OSR-compile cold parts on entry).** Warmup-only; no steady-state claim.
- **N8 — Swift CoW for large Go array values.** ABLE precedent (Swift Array/Dictionary/String share-until-mutate via `isKnownUniquelyReferenced`). Solves a problem Go's slice model mostly avoids (slices already share); the remaining case — large arrays passed by value — is rare in idiomatic Go. The *principle* (representation ≠ semantics) feeds edge 1's descriptor design instead.
- **N9 — Pyston-style runtime-IR inlining (embed runtime IR in the binary so the JIT can inline runtime helpers with caller context).** Real mechanism (Pyston embedded LLVM IR of its runtime for inlining), but for Go it overlaps the backlog's adaptive-concurrency and map-specialization items; the distinct sliver (inlining `growslice`/map helpers with caller profile context) has no clean standalone benchmark. Revisit if those backlog items need it.
- **N10 — YJIT-style C-method inlining for runtime helpers.** gc already intrinsifies the hot runtime helpers; the remaining gap is small.

## Surveyed, nothing applicable (negative results — first-class, so nobody re-surveys)

- **V8 hidden classes / transition trees** — solve the dynamic-shape problem (objects gaining properties at runtime in any order); Go field layout is static and fixed at compile time. Nothing to transfer.
- **V8 elements-kinds lattice (PACKED_SMI → PACKED_DOUBLE → HOLEY → DICTIONARY)** — Go slice element types are static; there are no holes and no dictionary mode. The transferable sliver (per-allocation-site sizing policy) is C3.
- **V8 Smi tagging as the default integer representation** — Go integers are already unboxed machine words; the `any`-payload sliver is the backlog's unboxing item.
- **Polymorphic inline caches for *method* dispatch** — overlaps the backlog's speculative interface devirtualization cascade; not duplicated here (complementary angle noted below).
- **TraceMonkey (SpiderMonkey)** — ATTEMPTED, abandoned: trace explosion at control-flow merges; method JITs (JägerMonkey → IonMonkey) won (http://en.wikipedia.org/wiki/SpiderMonkey). No obstacle our architecture routes around — we wouldn't build a tracer either.
- **LuaJIT trace compiler** — same lesson as TraceMonkey; DynASM is a compile-latency mechanism (N4). LuaJIT FFI (efficient C calls) is a cgo analog, not a speculation optimization.
- **HPHPc (PHP→C++ transpiler)** — ATTEMPTED, abandoned: dynamic-language features (eval, dynamic includes) defeated static whole-program compilation. Obstacle absent in statically-typed Go → revived as C6.
- **Ruby MJIT (external C compiler as JIT backend)** — ATTEMPTED, abandoned in favor of in-process YJIT: batch compilation through an external compiler couldn't deliver profile feedback or warmup. Lesson for our JIT-mode architecture: the JIT must be in-process with profile feedback, not a batch backend.
- **JSC LLVM-backed FTL** — ATTEMPTED, abandoned: LLVM compile times exceeded JIT budgets; replaced by the B3 backend. Lesson for C7's rejit budgets, not a candidate (our backend, Graal, is already fixed).
- **Sparkplug as a throughput story** — warmup only (see N3).
- **Cinder Static Python boundary checks** — "checks at the boundary, raw offsets inside" assumes a dynamic interior; Go is statically typed, so there is no dynamic boundary to verify.
- **V8 pointer compression / Orinoco GC internals** — GC and memory-encoding design, not speculation; out of this track's scope.
- **Swift small-string optimization** — already a backlog item; Swift's 15-byte tagged design is ABLE and is the reference design (complementary angle, not a new candidate).
- **Swift `_read`/`_modify` coroutine accessors as language features** — Go needs no language change for the transferable idea (C9 does it as a JIT intrinsic).
- **ZJIT (Ruby)** — THINKING: method-based JIT in active development, deliberately "textbook" after YJIT's LBBV experiment (https://www.slideshare.net/slideshow/zjit-building-a-next-generation-ruby-jit/278807093). Nothing transferable yet beyond YJIT's notes; re-check in wave 2 when it matures.

## Complementary angles on backlog items (not new candidates)

- **Devirtualization cascade:** .NET's guarded devirtualization uses an *inlined fallback* (branch to the virtual call) rather than deopt — cheaper when the guard is predictable-but-not-certain; V8/JSC use a mono→poly→megamorphic PIC ladder — the cascade's fallback chain should probably be a ladder, not a cliff.
- **Map specialization:** C4/C9 are access-path complements (representation vs access); consider them together.
- **Always-on profiling:** JSC's "inline caches *are* the profiler" (negative-cost profiling — the IC miss path records types with no extra instructions); PEP 659's counter policy (adaptive decrement, saturating counters, de-specialize on repeated failure) as the profiler→JIT gating policy.
- **Speculative data representation:** V8's elements kinds are the precedent for *per-allocation-site* (not per-type) representation decisions with one-way generalization on violation.
- **Adaptive concurrency primitives:** dynamic runtimes have no scheduler signals — our edge 2 (lock-holder-running, waiter counts, true idleness) is unique to us; JVM biased locking is Track 2's remit.
- **Small string optimization:** Swift's tagged 15-byte small strings are ABLE in production — use as the design reference, not a research item.

## Enabling infrastructure (not ranked; required by the candidates above)

- **Assumption-dependency / invalidation framework.** V8 tracks "dependent code" per assumption and deoptimizes all of it when a prototype/map changes; .NET rejits "when new interface implementations arrive on scene". We need the same registry: every speculative compilation records its assumptions (func-var target, value facts, branch facts, type-arg facts); any violation invalidates exactly the dependent set. Required by C1/C2/C5/C8; C7's rejit is the policy on top.
- **OSR-exit machinery.** V8, JSC, .NET, and Pyston (backedge-trip-count OSR entries) all have it; it is what makes speculation *sound without* keeping fallback paths in hot code. Required by C5/C7 (and N1).

## Coverage notes (drives wave 2)

**Wave 1 surveyed (primary sources):**
- V8: hidden classes, IC states, elements kinds, Smi tagging, deopt/OSR, tiered pipeline (Sparkplug/Maglev/TurboFan) — v8.dev blog sources, `thlorenz/v8-perf` notes, secondary explainers cross-checked.
- JavaScriptCore: tier architecture + speculation model — the WebKit "Speculation in JavaScriptCore" blog (primary); tier thresholds from secondary notes (flag: verify against `runtime/OptionsList.h` in wave 2).
- .NET: tiered compilation + dynamic PGO + guarded devirtualization — official .NET performance blog (primary); OSR design doc `dotnet/runtime` (primary).
- CPython: PEP 659 (primary); interpreter internal docs (primary).
- Cinder: Static-Python writeup via secondary (dev.to) — FLAG: read primary Meta engineering sources / `facebookincubator/cinder` in wave 2.
- Pyston: 0.5.1 release blog + design notes (primary but historical, 2016-era); modern Meta-internal Pyston not surveyed.
- HHVM: hackers-guide glossary/README (primary); region-JIT paper via Meta Research (primary).
- Swift: SE-0460, SE-0193, SE-0474, SE-0377, SE-0437 (primary, swift-evolution repo); CoW/existential writeups (secondary, cross-checked).
- Ruby: YJIT — release notes + LBBV paper reference (primary-ish); ZJIT — author slide deck (primary).
- LuaJIT: architecture docs, DynASM demo (primary-ish/secondary).
- SpiderMonkey history: TraceMonkey retirement (Wikipedia), IonMonkey/JägerMonkey (Mozilla blogs).

**NOT yet surveyed (wave 2+):**
- V8 source comments (`src/objects/map.h`, `src/compiler/` IC/deopt handling) — deeper pass on miss policies and deopt-point placement.
- JSC source (`dfg/`, `ftl/b3`) — value-profiling implementation; rest of the speculation blog (control/compilation sections).
- .NET RyuJIT source — dynamic-PGO probe implementation, guarded-devirtualization codegen shapes.
- Cinder primary sources (Meta eng blog, repo).
- Modern Pyston (post-Meta-fork; "pyston-lite").
- HHVM source — region-selector heuristics.
- Swift compiler source — specializer/SIL passes; more evolution proposals (ownership roadmap SE-0377 follow-ups).
- TruffleRuby / GraalPy — Truffle-based dynamic runtimes; coordinate with Track 2 (GraalVM) to avoid duplication.
- PyPy meta-tracing — the guard/deopt discipline in its purest form.
- RaptorJIT (LuaJIT fork) — production-hardening notes.
- ZJIT — re-check when mature.
- V8 Maglev design docs (mid-tier JIT).
- .NET static+PGO interplay docs (`dotnet/runtime` docs/design).
- Ruby Sorbet-typed JIT directions (static types → JIT, parallels our static-Go position).
