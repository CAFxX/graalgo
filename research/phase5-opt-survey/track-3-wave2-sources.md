# Track 3 wave-2 — source-level passes on dynamic runtimes

**Worker:** T3 wave-2 subagent (2026-10-04). **Status:** wave-2 in progress — this file is the checkpoint; wave 1 lives in [track-3-dynamic-runtimes.md](track-3-dynamic-runtimes.md).
**Rule:** every candidate below passed the candidate bar at mining time (check the legs); coordinator assigns SURV- IDs at synthesis. Nothing here is a measurement — all numbers are the cited source's claims.
**Coverage:** V8 source (map.h, src/compiler IC/deopt), JSC source (dfg/ftl/b3 value profiling + OSR exit), .NET RyuJIT source (dynamic-PGO probes, GDV codegen), PyPy meta-tracing (guard/deopt discipline). Cinder/HHVM-Swift/Pyston/RaptorJIT/Maglev/ZJIT remain in the wave-2 gap list.

**Coverage:** V8 source (map.h, src/compiler IC/deopt), JSC source (dfg/ftl/b3 value profiling + OSR exit), .NET RyuJIT source (dynamic-PGO probes, GDV codegen), PyPy meta-tracing (guard/deopt discipline). Cinder/HHVM-Swift/Pyston/RaptorJIT/Maglev/ZJIT remain in the wave-2 gap list.

**Source pins (depth-1 clones, 2026-10-04):**
- V8 `github.com/v8/v8` @ `7dce5a4a324258ba51b2a6f5dee7a1fa18c7671d` (under `/tmp/wave2-src/v8`)
- JavaScriptCore `github.com/WebKit/WebKit` @ `40508b494c5250e0d8fac2cd5a3a50ca84447d20` (under `/tmp/wave2-src/webkit`)
- .NET `github.com/dotnet/runtime` @ `d90dbf43be153cc7ba7f49bb271e0b7e56a81891` (under `/tmp/wave2-src/dotnet`)
- PyPy `github.com/pypy/pypy` @ `15f609668f503862b7c9c22373436382f77527de` (under `/tmp/wave2-src/pypy`)

---

## S1 — V8: IC miss policy, deopt thresholds, what speculation never pays

**Bucket of precedents:** ABLE (all in production).

**S1.1 — Poly cap = 4, then cliff to megamorphic.** `src/flags/flag-definitions.h:3251`: `DEFAULT_MAX_POLYMORPHIC_MAP_COUNT = 4` ("maximum number of valid maps to track in POLYMORPHIC state"). `src/ic/ic.cc:813`: `UpdatePolymorphicIC` returns false (→ MEGAMORPHIC) once `number_of_valid_maps >= v8_flags.max_valid_polymorphic_map_count`. Megamorphic = generic handler (`GetHandlerMegamorphic`, ic.cc:963); no more speculation, no more deopts at the site.

**S1.2 — Miss that doesn't progress the lattice → megamorphic immediately.** `src/ic/ic.cc:755-766`: if the same map+handler is already in the feedback but the site missed anyway ("we're not progressing in the lattice"), go MEGAMORPHIC instead of growing the PIC. Transferable rule: a miss that contradicts the cache's *own* contents is evidence the cache is wrong-headed, not evidence to enlarge it.

**S1.3 — Two deopt-loop breakers in the miss path.** `src/ic/ic.cc:779-790`: (a) dictionary rehash miss → MEGAMORPHIC "to prevent deopt loops"; (b) prototype-chain failure on an already-polymorphic receiver → *overwrite the handler in place* instead of adding an entry. Transferable: distinguish "new evidence" (grow) from "same evidence, new failure mode" (replace / give up).

**S1.4 — No feedback → generic code, and the deopt reason names the missing evidence.** `src/compiler/js-type-hint-lowering.cc` (lines 397-734): every specialization site guarded by `DeoptimizeReason::kInsufficientTypeFeedbackFor{Unary,Binary,Compare}Operation`, `...ForCall`, `...ForConstruct`, `...ForGenericNamedAccess/KeyedAccess`. The pattern is uniform: if the feedback slot doesn't pin the fact, emit the generic op *with no deopt point at all* — speculation is only ever inserted where evidence exists. This is V8's answer to "what speculation never pays": speculation without measured evidence.

**S1.5 — The permanent cliff exists.** `src/objects/shared-function-info.cc:526`: `SharedFunctionInfo::DisableOptimization(isolate, BailoutReason reason)` — records a named reason (`DisabledOptimizationReasonBits`) and drops the function back to interpreted forever. V8 does not keep retrying a function that proves hostile to speculation.

**S1.6 — Assumption-dependency framework = the invalidation registry wave 1 listed.** `src/compiler/compilation-dependencies.h`: `DependOnStableMap`, `DependOnProtector` (array-buffer-detaching, array-species, no-elements…), `DependOnFieldConstness`, `DependOnGlobalProperty`, `DependOnPretenureMode`. Every speculation registers a dependency; the invalidator deopts exactly the dependent code. Note the comment at `DependOnFieldConstness`: "This is to ensure that fast elements kind transitions cannot be used to mutate fields without deoptimization of the dependent code" — i.e. the dependency granularity is chosen so *representation changes* are covered, not just value changes. (For Go: dependencies should cover representation changes — field constness, map B — not just value facts.)

**S1.7 — Slack tracking: profile-guided layout trim with a countdown.** `src/objects/map.h:410-437`: inobject slack tracking detects the first constructor call, initializes unused fields with a filler map, and after a countdown (`kSlackTrackingCounterStart`→`kSlackTrackingCounterEnd`, `kGenerousAllocationCount`) recomputes instance size over the whole transition tree. Transferable pattern: bounded-count profile phase → one-time layout decision → no per-access cost afterward.

**Transfer / does-NOT:** Transfers the *miss/retry/deopt policy ladder* (S1.1–S1.5) and the *dependency-registry shape* (S1.6) — both are type-system-independent and apply directly to Go value/layout/branch speculation. Does NOT transfer: hidden classes, map transitions, elements kinds (Go layout is static — wave-1 negative, unchanged). S1.7's *mechanism* (filler-map trick) doesn't transfer; the *countdown-then-decide* policy does.

**New ideas mined (bar-checked):**

- **P1 — "Miss-against-own-cache → downgrade, don't enlarge" (policy for C1/C4/C8).** When a guard fails at a site whose cache already contains the failing case (V8 ic.cc:755-766), the correct response is to collapse the speculation (megamorphic/generic), not to add another ladder rung. Bar: (a) sound — the generic fallback is semantics-preserving; (b) benchmark: same as C1/C4 (sort.Slice comparator, config-map lookups) — prevents pathological ladder growth at flapping sites; (c) E3; (d) not in SURV-001…040 (the bar is about *policy*, none of the candidates specify the miss-against-cache rule); (e) gc has no such sites. **Bucket: WOULD-LIKE** (precedent ABLE — V8).
- **P2 — "No-evidence → generic, named reason" (policy for all speculative candidates).** Only speculate where a profile fact exists; elsewhere emit the unspecialized op — and record *which evidence was missing* at the site (V8's `kInsufficientTypeFeedbackFor*` naming). This makes "profile coverage" a first-class, auditable quantity and prevents speculation from ever being inserted blindly. Bar: (a) sound — generic is the baseline; (b) benchmark: guards C2/C5/C8 against over-speculation regressions (measured as "no slowdown on unprofiled code"); (c) E3+E4; (d) not in SURV-001…040; (e) gc can't do it (no speculation at all). **Bucket: WOULD-LIKE** (precedent ABLE).

---

## S2 — JavaScriptCore: value profiling, OSR-exit discipline, reoptimization policy

**Bucket of precedents:** ABLE (all in production).

**S2.1 — Value profiles with dedicated spec-failure buckets.** `Source/JavaScriptCore/bytecode/ValueProfile.h:49-60`: every profile has normal buckets *plus* `specFailBucket(i)` slots. `computeUpdatedPrediction()` merges bucket types into the prediction. The failing speculation's offending value is stored into the spec-fail bucket *by the OSR-exit code itself* (`MethodOfGettingAValueProfile::emitReportValue`, `bytecode/MethodOfGettingAValueProfile.cpp:54,72`; invoked from `dfg/DFGOSRExit.cpp:506-515`). Transferable: the exit path is an information channel — the recompiler learns *what* broke the assumption, not just that something broke.

**S2.2 — Failure-address profiling.** `dfg/DFGOSRExit.cpp:447-496`: on an OSR exit through an array access, the JIT stores the failing value's structure ID into `ArrayProfile::addressOfSpeculationFailureStructureID()` and ORs its indexing mode into the profile's array-modes bitset. The next compilation *watches out* for exactly the representation that failed. Transferable to Go: on a guard failure at a value-profiled op (C2/C8), record the failing *representation* (e.g. which interface impl, which map B, which concrete type-arg), not just a failure count.

**S2.3 — Profiles of *absence*: ArithProfile records violations, not conformances.** `bytecode/ArithProfile.h`: per-op bitsets `Int32Overflow | Int52Overflow | NegZeroDouble | NonNumeric | HeapBigInt | BigInt32` — set only when the *bad* case is observed; `observeResult` returns early on the common int32 case (zero cost for the expected path). `dfg/DFGByteCodeParser.cpp:1435-1447`: speculation ("shouldSpeculateInt32", fixup) is inserted only when the bitset is *clean* AND the exit profile has no exit site of the matching kind at that bytecode index. Three evidence sources are ANDed: value profile, arith profile, exit profile. Transferable to Go: for hoisted/removed checks (bounds, overflow — SURV-033, C2), profile the *violations*; speculate only on clean bitsets.

**S2.4 — ExitKind taxonomy; exits are counted by kind.** `bytecode/ExitKind.h:32-68`: `BadType, BadConstantValue ("incorrect assumption about what value we would see. Usually used for function checks"), BadExecutable, BadCache, BadIndexingType, BadTypeInfoFlags, Overflow, NegativeZero, OutOfBounds, InadequateCoverage ("ended up in code that didn't have profiling coverage"), HoistingFailed ("Something that was hoisted exited. So, assume that hoisting is a bad idea"), Uncountable ("we should not count it")`. Two transferable policies: (1) `HoistingFailed` — a hoisted check that fails once is *never hoisted again* (monotone kill of the hoisting speculation); (2) `Uncountable` — don't learn from noise exits (debugger, watchdog, OOM).

**S2.5 — Reoptimization thresholds and exponential backoff.** `runtime/OptionsList.h:373-397`: `osrExitCountForReoptimization = 100`, `osrExitCountForReoptimizationFromLoop = 5` (loop exits are 20× more urgent), `reoptimizationRetryCounterMax = 0`. `bytecode/CodeBlock.cpp:2860-2895`: `adjustedExitCountThreshold` *doubles* the threshold per `reoptimizationRetryCounter()` (`result << 1`, saturating at `UINT32_MAX`). So: first recompile after 100 exits (5 from a loop); if the recompiled code fails again, 200; then 400… — exponential backoff on *re*-speculation. And `reoptimizationRetryCounterMax = 0` means a failed OSR *entry* is never blindly retried — the retry must come from fresh evidence, never from hope.

**S2.6 — Vote-ratio + monotone representation promotion.** `dfg/DFGVariableAccessData.cpp:88-140`: `shouldUseDoubleFormatAccordingToVote` uses `doubleVoteRatioForDoubleFormat = 2` (2:1 use-site votes); `tallyVotesForShouldUseDoubleFormat` is *monotonic* — "if the fixpoint leads us to conclude that we should switch back to int, we instead ignore this and stick with double." Transferable: value-representation speculation (unboxing, niche-filling — SURV-020, backlog unboxing) should generalize monotonically with a supermajority vote, never oscillate.

**S2.7 — Profile liveness/fullness gating before tier-up.** `runtime/OptionsList.h:399-401`: `desiredProfileLivenessRate = 0.75`, `desiredProfileFullnessRate = 0.35` — the tier-up decision weighs how much of the profile is live/full. Transferable to C7: don't rejit on a stale or sparse profile; gate recompilation on profile freshness.

**Transfer / does-NOT:** Transfers the *failure-information channel* (S2.1–S2.2), the *absence-profiling* discipline (S2.3), the *exit-kind taxonomy + monotone kill* (S2.4), the *reoptimization backoff* (S2.5), and the *vote/monotone promotion* (S2.6) — all representation/value/branch speculation machinery, type-system-independent. Does NOT transfer: JS type speculation itself (Go is statically typed); `SpeculatedType` lattice details.

**New ideas mined (bar-checked):**

- **P3 — "Kinded OSR exits with monotone kill" (policy for C5/C7).** Tag every OSR exit with a kind (value-assumption / branch / representation / hoisting / inadequate-coverage / uncountable-noise); feed per-(site,kind) counts to the recompiler; a `HoistingFailed`-style kind *permanently* disables that speculation at the site; `Uncountable` kinds never feed the counters. Bar: (a) sound — exits go to baseline; (b) benchmark: C5's loop-unswitching and C7's rejit across phase changes — prevents exit storms / hoisting oscillation; (c) E3+E4; (d) not in SURV-001…040 (none specify exit kinds); (e) gc has no OSR. **Bucket: WOULD-LIKE** (precedent ABLE — JSC).
- **P4 — "Absence-profiled check hoisting" (refinement of SURV-033/C2).** At hot integer ops, record a bitset of *observed violations* (overflow, neg-zero, out-of-range) at ~zero cost on the common path (JSC's early-return in `observeResult`); hoist/eliminate the check only on a clean bitset; the first violation poisons the bitset and permanently reinstates the check (S2.3+S2.4). Distinct from SURV-033's loop-versioning framing: this is the *profiling discipline* that makes the speculation sound-without-deopt (diamond: checked slow path remains). Bar: (a) sound — fallback is the checked op; (b) benchmark: hot 64-bit div/mod by runtime-stable divisor (SURV-009 sketch), bounds checks in image/signal loops; (c) E3+E4; (d) SURV-009 covers value-driven arithmetic specialization; this is the complementary *violation-bitset* mechanism — file as complementary angle on SURV-009/SURV-033, not a new ID. **Bucket: WOULD-LIKE** (precedent ABLE).

---

## S3 — .NET RyuJIT: dynamic-PGO probes, GDV codegen shapes, when GDV is disabled

**Bucket of precedents:** ABLE (shipped; dynamic PGO + GDV in production .NET).

**S3.1 — GDV codegen shape.** `src/coreclr/jit/indirectcalltransformer.cpp:739-780`: the guard is emitted as a single-word compare — `targetMethodTable != methodTable → GT_JTRUE` (class GDV) or delegate-target/function-pointer compare (method GDV) — with the direct call + inline in the then-block and the original virtual/indirect call as the else fallback. Diamond speculation: no deopt, the fallback is the unspecialized call. For Go's devirtualization cascade / C1 this is the production shape: *guard + direct call + original call*, not guard + deopt.

**S3.2 — The polymorphism ladder is SHORT — and the JIT decides the length.** `src/coreclr/jit/compiler.h:8300-8322` (`getGDVMaxTypeChecks`): hard cap `MAX_GDV_TYPE_CHECKS = 5`; default policy: **1** check for CoreCLR ("We plan to use 3 for CoreCLR too, but we need to make sure it doesn't regress performance"), **3** for NativeAOT. Config `JitGuardedDevirtualizationMaxTypeChecks` (`jitconfigvalues.h:746`). Comment: "we just suspect that even 4 type checks is already too much." Transferable to the cascade's fallback chain (wave-1 complementary angle): the production answer is a ladder of ≤3, default 1 in the dynamic-PGO setting.

**S3.3 — Chaining gate: 75% likelihood.** `jitconfigvalues.h:754`: `JitGuardedDevirtualizationChainLikelihood = 0x4B` (75) — a second GDV only chains onto the first's success path if the conditional likelihood clears 75%. (`indirectcalltransformer.cpp:579`: `canChainGdv`.) Transferable: conditional-probability gating for chained speculation, not just marginal probabilities.

**S3.4 — Likelihood comes from histogram proportions, with staleness guards.** `src/coreclr/jit/likelyclass.cpp:171-296`: `likelihood = 100 * count / totalCount` per histogram entry; abstract likely class → "stale profile", skip the guess (`importercalls.cpp:8925`). A monomorphic site returns likelihood 100 with 1 entry.

**S3.5 — Class probes are helper calls into a fixed 32-slot sampled histogram.** `src/coreclr/jit/fgprofile.cpp:2125-2160`: Tier0 inserts `CORINFO_HELP_CLASSPROFILE32/64(tmp, histogram)` before virtual/interface/delegate calls; `src/coreclr/inc/corjit.h:266-285`: `HandleHistogram32{ SIZE = 32, SAMPLE_INTERVAL = 64 }` — 32 (handle,count) slots, sampled every 64th execution. Separate method histograms for delegates/vtable targets (`compClassifyGDVProbeType`, `importercalls.cpp:10746-10782`): class profile, method profile, or both.

**S3.6 — When GDV/probing is disabled.** `importercalls.cpp:9071-9098` (`addGuardedDevirtualizationCandidate`): off when `JitEnableGuardedDevirtualization=0`; **bail on rare/cold blocks and on debug/minopts** ("rare / dbg / minopts"); `compClassifyGDVProbeType` returns None when not instrumenting or under AOT. `docs/design/features/DynamicPgo.md` caveats: the Tier0 profile slab is fixed-size — "If the slab fills up then subsequently jitted Tier0 methods simply aren't instrumented"; Tier0 methods may run "as few as 30" times before the Tier1 rejit; methods with loops bypass Tier0 (needs OSR); "we may not get good quality data… methods exhibit phased behavior." Transferable warnings for our always-on profiler (E4): fixed budgets must degrade by *skipping* (not corrupting), and loop-carried code needs OSR-entry profiling or its profile is systematically missing.

**S3.7 — Closed-world GDV needs no fallback.** `importercalls.cpp:8883-8947`: NativeAOT's `getExactClasses` returns the *complete* implementer set; with `GTF_CALL_M_GUARDED_DEVIRT_EXACT` the last check's block becomes `BBJ_ALWAYS` — the ladder is exhaustive, the fallback is deleted (`indirectcalltransformer.cpp:718-726`). This is the production precedent for SURV-001's hybrid half: closed-world → exhaustive guarded ladder with the final else removed.

**S3.8 — Value-histogram probes exist for casts.** `fgprofile.cpp` `BuildValueHistogramProbeSchemaGen`: `ValueHistogram{Int,Long}Count` probes record observed *values* (not just classes) at cast-helper sites — the precedent for value speculation (C2) in .NET's own toolchain.

**Transfer / does-NOT:** Transfers the *guard+direct-call+fallback diamond* (S3.1), the *short-ladder + chain-gate policy* (S3.2–S3.4), the *sampled fixed-slot histogram probe design* (S3.5), and the *disable/degrade rules* (S3.6–S3.7). Does NOT transfer: CLR method-table/vtable specifics, interface dispatch via stubs, R2R/AOT packaging concerns.

**New ideas mined (bar-checked):**

- **Complementary angle on the devirtualization cascade + C1 (not a new ID):** .NET's production numbers for the "ladder, not a cliff" question — cap 5, default 1 (CoreCLR) / 3 (NativeAOT), chain gate at 75% conditional likelihood, single-word guard compare, fallback = the original virtual call (diamond, never deopt). Also the S3.7 exact-class precedent strengthens SURV-001's hybrid-mode framing (exhaustive ladder, fallback deleted). No new candidate: the mechanism is already in the backlog/candidates; .NET supplies the *policy numbers*.
- **Negative / warning (S3.6):** .NET's own design doc admits Tier0 profiles can be thin (30 runs), loop methods bypass Tier0, and the profile slab silently stops instrumenting when full. For E4: our always-on profiler must (a) profile loop bodies via OSR-entry (else the hottest code is invisible), (b) degrade by skipping sites when budgets exhaust, never by corrupting data, (c) treat phase changes as first-class (ties to P3/C7).

---

## S4 — PyPy: meta-tracing guard/deopt discipline

**Bucket of precedents:** ABLE (production PyPy; the guard-based speculation model in its purest form).

**S4.1 — Guards are the ONLY speculation; failure ticks a lossy fractional counter.** `rpython/jit/metainterp/counter.py` (class `JitCounter` docstring): guard failures are counted in a **lossy 5-way associative timetable** (2048 entries × 5 ways, 6 bytes/way: float + subhash) — each compiled guard gets a random hash at compile time (`fetch_next_hash`); on failure the hash is `tick()`ed by a fraction `1/threshold`. When the fraction reaches 1.0 (i.e. `trace_eagerness = 200` failures, `rpython/rlib/jit.py:573,609`), a **bridge** is compiled from the failing guard — a side trace that rejoins the loop. Not a recompile of the loop; a narrow, additive continuation.

**S4.2 — Retracing is a one-shot privilege.** `rpython/rlib/jit.py:609`: `retrace_limit = 0` (production default); `rpython/jit/metainterp/optimizeopt/unroll.py:215-236`: when `cell_token.retraced_count >= retrace_limit`, further retraces are refused and the trace "jumps to the preamble" (the unoptimized entry path). `max_retrace_guards = 15` caps how many extra guards one retrace may add. Transferable: re-speculation budget per site is *counted in versions*, and the budget is tiny (0–1 retraces) — oscillation is structurally impossible.

**S4.3 — Abort → blackhole: compilation failure never retries compilation.** `rpython/jit/metainterp/compile.py:27-29` (`giveup()` raises `SwitchToBlackhole(Counters.ABORT_BRIDGE)`); `opencoder.py:549` (trace > `trace_limit = 6000` ops → `ABORT_TOO_LONG` → blackhole); `pyjitpl.py:458` (`ABORT_BAD_LOOP`). The blackhole is an interpreter that does *not* trace — execution always makes progress, and a hostile loop gets exactly one trace attempt per abort class. Transferable: every speculation pipeline needs a non-speculative fallback that cannot itself trigger re-speculation.

**S4.4 — Decay adapts to phase changes.** `rpython/rlib/jit.py:609`: `decay = 40`; `counter.py`: `decay_all_counters()` multiplies every timetable fraction down, invoked after every 32 minor collections. Guard-failure evidence *expires*. Transferable to C7/P3: failure counters must decay with GC or wall-clock time, or a phase change permanently poisons the site.

**S4.5 — Quasi-immutable invalidation registry.** `rpython/jit/metainterp/quasiimmut.py`: fields declared quasi-immutable get a per-field invalidation object; `make_invalidation_function` wires mutation to `qmut.invalidate(...)`, which drops all dependent traces. Same family as V8's CompilationDependencies (S1.6) — the transferable shape is *per-assumption invalidation lists*, and the Go analog covers representation facts (map B, field constness), not just values.

**S4.6 — Trace-abort taxonomy (what never pays).** `rpython/rlib/jit.py:1416-1435` (Counters): `ABORT_TOO_LONG` (6000 ops), `ABORT_BRIDGE` (already compiling), `ABORT_BAD_LOOP`, `ABORT_ESCAPE` (virtualizable escaped), `ABORT_FORCE_QUASIIMMUT`, `ABORT_SEGMENTED_TRACE`. PyPy's position: long traces, escaping virtuals, and re-entrant compilation are not retried — they go to the blackhole. For Go: cap speculative region size; on escape-analysis failure don't re-speculate the same region.

**Transfer / does-NOT:** Transfers the *guard-failure budget* (S4.1), the *one-shot retrace + blackhole* discipline (S4.2–S4.3), *counter decay* (S4.4), and the *invalidation-registry shape* (S4.5). Does NOT transfer: tracing itself (we are method-based — bridges don't map to Graal regions); green/red promotion mechanics.

**New ideas mined (bar-checked):**

- **P5 — "Guard-failure budgets with decay, retrace as one-shot privilege" (policy for C7).** Per-site speculation gets: a failure counter in a bounded lossy table (PyPy's 5-way fractional timetable), a bridge/recompile trigger at N failures (N≈200 scaled to our guard costs), decay of all counters on GC/time (phase adaptation), and at most one re-speculation per site per epoch — further oscillation falls back to the unspecialized path permanently (PyPy's preamble jump). Bar: (a) sound — fallback is the baseline; (b) benchmark: C7's phase-change scenario (API gateway with shifting endpoint mix) — this is what prevents rejit storms; (c) E3+E4; (d) not in SURV-001…040 (C7 names hysteresis but not the concrete budget/decay/one-shot design); (e) gc has no rejit. **Bucket: WOULD-LIKE** (precedent ABLE — PyPy).
- **Near-miss: trace bridges as a standalone claim.** PyPy's bridge (side trace from a failing guard) has no method-JIT analog — Graal compiles regions, not traces; the equivalent is a deopt + recompile with widened assumptions, which is P3/P5. Struck as standalone; the *budget* idea is P5.

---

## Cross-source synthesis: the miss/retry/deopt policy table

| Policy | V8 | JSC | .NET | PyPy |
|---|---|---|---|---|
| Polymorphism cap before giving up | 4 maps → megamorphic (S1.1) | (structure checks; megamorphic via IC state) | ≤5 type checks, default 1 (S3.2) | n/a (tracing) |
| Miss contradicting the cache | → megamorphic immediately (S1.2) | → OSR exit, kinded (S2.4) | n/a | → bridge after 200 fails (S4.1) |
| Deopt-loop breaker | dict rehash → megamorphic; proto failure → overwrite (S1.3) | threshold doubles per recompile (S2.5) | exact-class ladder drops fallback (S3.7) | retrace_limit=0; blackhole (S4.2–3) |
| No-evidence speculation | never: generic + named reason (S1.4) | `InadequateCoverage` exit kind (S2.4) | GDV needs PGO data or exact classes (S3.6–7) | untraced → blackhole |
| Failure information | (deopt reason names the site) | spec-fail buckets + failure structure IDs (S2.1–2) | histogram proportions (S3.4) | random-hash ticking (S4.1) |
| Phase-change adaptation | reopt_after_lazy_deopts | profile liveness gating (S2.7) | (none admitted — doc caveat) | decay=40 per 32 minor GCs (S4.4) |
| Permanent give-up | DisableOptimization(BailoutReason) (S1.5) | quick-tier-up disabled after speculation failure | (rare/cold bail) | preamble jump after retrace budget |

## Negatives (first-class)

- **V8 elements-kinds lattice, hidden classes, map transitions** — unchanged from wave 1: Go layout is static. Nothing new found at source level that revives a transfer.
- **Trace bridges** (S4, near-miss above) — no method-JIT analog; struck.
- **JSC's `SpeculatedType` lattice and DFG fixup-phase mechanics** — JS-type speculation; does not transfer to statically-typed Go. Only the *profiling/exit* machinery transfers (S2.1–S2.7).
- **.NET R2R/Tier0 packaging, interface stubs, delegate ABI** — CLR-specific; not transferable.
- **PyPy green-field promotion / virtualizable machinery** — interpreter-design-specific; the invalidation-registry *shape* transfers (S4.5), the mechanics don't.
- **V8 Maglev / JSC FTL-B3 backend internals** — compile-pipeline engineering, not speculation policy; out of scope for this pass (b3/ was cloned but the policy-relevant material was in dfg/ + bytecode/).

## Coverage notes (remaining wave-2 gap list, unchanged)

Cinder primary sources; modern Pyston; HHVM source (region-selector heuristics); Swift compiler source (specializer/SIL passes); TruffleRuby/GraalPy (coordinate with Track 2); RaptorJIT; ZJIT re-check; V8 Maglev design docs; .NET static+PGO interplay docs; Ruby Sorbet-typed JIT directions.

*(checkpoint: all four assigned sources complete — V8, JSC, .NET RyuJIT, PyPy)*

