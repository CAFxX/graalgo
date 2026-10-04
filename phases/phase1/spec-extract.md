# Phase 1 — Normative SPEC extract

**Sources:** `~/workspace/graalgo/SPEC.md` (1242 lines, ACCEPTED 2026-10-03; 70 requirements locked)
and `~/workspace/graalgo/PHASES.md` §3 (lines 62–77).
**Extracted:** 2026-10-03. **Scope of this file:** Phase-1-relevant normative content ONLY.
Concurrency/scheduler internals (§3.5 except defer/panic/Goexit on the main goroutine),
stdlib adaptation, and AOT/JIT mode implementation (§3.9, §3.11) are excluded except where
they constrain the AST↔runtime interface. `net`/`os` are explicitly out of Phase 1
(PHASES.md §3: "no `net`/`os` yet").

**Phase-1 mission (PHASES.md:62–69):** complete Go language (through generics) on the
Truffle AST — `go/parser`+`go/types` frontend service; Truffle nodes for all
statements/expressions; descriptor-based value model; defer/panic/Goexit; intrinsic
surface *stubs* sufficient for language tests; differential corpus = language test suites
+ generated programs.

**Phase-1 done-gates (PHASES.md:71–77):**
(1) `go test` language-facing suites (incl. generics) pass differentially vs gc with zero
unexplained divergences; (2) fuzz-soak layer operational on the language subset;
(3) **interface freeze**: AST↔runtime intrinsic signatures and the `-toolexec` argv
contract versioned and documented (Phase 4 starts in parallel only after this);
(4) peer reviews + self-attack (pathological generics, deep recursion, defer storms);
(5) no placeholders (`grep` gate).
**Kill criteria:** systematic semantic divergence vs gc unclosable by the intrinsic-surface
design; interface freeze impossible without breaking the toil bound (→ architecture replan).

---

## A. Frontend service — `go/parser` + `go/types`, verbatim (SPEC §3.2; GG-0012, GG-0013)

1. SPEC.md:237–239 — *"``go/parser`` produces go/ast; ``go/types`` produces full type info; a node factory walks the typed AST and builds Truffle nodes `[DECIDED]`."* (GG-0012)
   → Paraphrase: build a **frontend service** whose node factory consumes the fully
   type-checked `go/ast` from `go/types` and emits Truffle nodes — one pipeline stage,
   no `cmd/compile` IR involved.

2. SPEC.md:240–242 — *"The ``go/*`` tree is reused **verbatim** from the tracked release and re-vendored **atomically** (frontend + stdlib as one unit) every release."* (GG-0012)
   → Paraphrase: never fork or hand-port `go/*`; vendor it whole each release.
   Normative counterexample: *"pinning the frontend to N while tracking stdlib N+1 does
   not work in general (go1.27's stdlib itself uses a 1.27 language feature, generic
   methods on `math/rand/v2.Rand` `[VERIFIED]`)"* (SPEC.md:243–244).

3. SPEC.md:244–246 — *"Old code is supported via per-file/per-module `-lang` gates exactly like the stock toolchain, never by pinning (GG-0013)."*
   → Paraphrase: old language versions are handled by honoring `-lang` flags per file/module, not by keeping an old frontend around.

4. SPEC.md:246–248 — *"The Go 1 compatibility promise covers ``go/*`` as additive-only API `[VERIFIED]`; per-release frontend deltas are new exported API, new accepted syntax, and type-checker behavior behind `-lang` gates."* (GG-0012)
   → Paraphrase: frontend upgrades are expected to be additive + gated; the node factory
   must be written against this stability assumption (falsifier: spike 3's dry-run
   measures frontend rebase toil against the ≤200 agent-hour toil bound, SPEC.md:250–251).

## B. Value model — Java objects throughout (SPEC §3.4; GG-0015, GG-0016)

5. SPEC.md:276–277 — *"descriptor-based structs (per-type descriptor + flat ``long[]`` for scalars + ``Object[]`` for references)"* (GG-0015)
   → Paraphrase: Go structs live in a per-type **descriptor + flat `long[]` (scalars) + `Object[]` (references)** layout.

6. SPEC.md:277–279 — *"`one-class-per-struct is rejected because runtime class definition is unavailable in closed-world images`"* (GG-0015)
   → Paraphrase: never generate one JVM class per Go struct type; the descriptor design
   is load-bearing for Native Image closed-world compatibility.

7. SPEC.md:279–280 — *"slices as `{typedArray, offset, len, cap}` with typed Java arrays"* (GG-0015)
   → Paraphrase: slice = typed Java array + offset + len + cap (backing store must be a *typed* Java array).

8. SPEC.md:280–282 — *"maps as a hand-written open-addressing table (Go specifies randomized iteration order, so the runtime must own the iterator)"* (GG-0015)
   → Paraphrase: implement maps as a hand-written open-addressing table; the runtime
   (not any Java map) must own iteration order to give Go's randomized order.

9. SPEC.md:282–283 — *"strings as immutable ``GoString{byte[]}`` (`java.lang.String` is UTF-16 and wrong for arbitrary Go bytes)"* (GG-0015)
   → Paraphrase: strings are an immutable `GoString` wrapping `byte[]`; never `java.lang.String` (UTF-16 cannot hold arbitrary Go bytes).

10. SPEC.md:283–284 — *"interfaces as `(dynamicType descriptor, data)` + itab"* (GG-0015)
    → Paraphrase: interface value = (dynamic-type descriptor, data) + itab.

11. SPEC.md:284–285 — *"closures as `{RootCallTarget, MaterializedFrame|null}`"* (GG-0015)
    → Paraphrase: closure = RootCallTarget + optional MaterializedFrame for captures.

12. SPEC.md:285–287 — *'"Go pointer" = opaque Java reference: the GC moves objects freely; write barriers and exact roots come free from the JVM'* (GG-0015)
    → Paraphrase: a Go pointer is an opaque Java reference; rely on JVM write barriers
    and exact roots (no manual barrier machinery in Phase 1).

13. SPEC.md:287–288 — *"pinning happens only at the cgo/FFI boundary, scoped to the call `[DECIDED]`"* (GG-0015)
    → Paraphrase: object pinning is allowed only at the cgo/FFI boundary and only for
    the call's duration (out of Phase 1 scope, but constrains the pointer model).

14. SPEC.md:288–289 — *"Off-heap as primary storage is rejected (scoped verdict, T5 §2.3): for a managed Go implementation it reimplements the GC's job worse."* (GG-0015)
    → Paraphrase: values live on the managed heap; do not build an off-heap value store.

15. SPEC.md:290–292 — *"``unsafe.Pointer`` v1 = handle-table emulation (object identity + field offset); arithmetic escaping the object traps — a documented semantic restriction, never silent (GG-0016) `[DECIDED]`."* (also §7.3 closure, SPEC.md:910–913)
    → Paraphrase: `unsafe.Pointer` is emulated as a handle table (object identity +
    field offset); out-of-object arithmetic **traps** and the restriction must be
    documented, never silently divergent.

16. SPEC.md:293–294 — falsifier for the v1 unsafe semantics: *"if `reflect`'s 243 unsafe uses require true raw-address arithmetic beyond what the handle table + a raw-address FFM-arena mode can cover, the v1 semantics are re-scoped."* (GG-0016)
    → Paraphrase: Phase-1 `unsafe` semantics must survive spike-4's evidence — the
    handle table + an isolated FFM-arena raw-address mode already passed spike 4
    against `reflect`'s 243 unsafe uses (SPEC.md:10.1 ledger).

## C. Truffle node design (SPEC §3.3; GG-0014)

17. SPEC.md:254–257 — *"Two node families per Truffle convention: expression nodes with typed execute methods (`executeLong`/`executeInt`/`executeDouble`/`executeBoolean`/generic `execute→Object`) keeping primitives unboxed, and void statement nodes; loops use `@ExplodeLoop`-friendly structures or `LoopNode`/`RepeatingNode` for OSR `[DECIDED]`."* (GG-0014)
    → Paraphrase: expressions = typed-execute nodes with unboxed primitives
    (`executeLong/Int/Double/Boolean`, generic fallback `execute→Object`); statements
    = void nodes; loops = `@ExplodeLoop`-friendly shapes or `LoopNode`/`RepeatingNode`
    for OSR.

18. SPEC.md:258–263 — *"DSL specialization budget is spent **only at genuinely dynamic sites**: interface dispatch (PIC on itab identity, limit ~3, megamorphic fallback to hash lookup — the single most valuable specialization), type switches (decision trees + `ValueProfile`), maps (inline fast path guarded on table version, `@TruffleBoundary` slow path)."*
    → Paraphrase: add Truffle-DSL specializations ONLY at three dynamic sites:
    (a) interface dispatch — PIC on itab identity, ~3 entries, hash-lookup fallback;
    (b) type switches — decision trees + `ValueProfile`; (c) maps — inline fast path
    guarded on table version, `@TruffleBoundary` slow path.

19. SPEC.md:263–266 — *"Everything else uses typed execute methods with no PICs — Go's static typing vacates Truffle's type-discovery miracle, and the design must not pretend otherwise `[DECIDED]` (adversarial result, T5 §5)."* (GG-0014)
    → Paraphrase: everywhere else, typed execute methods with NO PICs — Go's static
    type info from `go/types` must be used directly instead of Truffle type profiling.

20. SPEC.md:266–267 — *"Itabs are immutable, so speculation on them never invalidates — no Assumption lifecycle management needed."*
    → Paraphrase: itabs may be speculated on without Assumption invalidation machinery
    (they are immutable by construction).

21. SPEC.md:536–538 — *"**The AOT baseline must be correct and performant without the runtime JIT** (GG-0050) `[DECIDED]`: every isolate/generation starts cold and loses its profiles on restart, so the JIT is a steady-state bonus, never a correctness or tail-latency dependency."*
    → Paraphrase: the interpreter path of the Phase-1 AST must be fully correct and
    reasonably fast on its own — PICs/guards must have correct megamorphic/cold
    fallbacks; the JIT may only ever be a bonus. (Modes constraint carried into Phase 1.)

22. SPEC.md:550–551 — *"graalgo must emit accurate `SourceSection`s from Go source positions so the inspector shows Go code."* (GG-0024, §3.10)
    → Paraphrase: every AST node the node factory builds must carry accurate
    `SourceSection`s derived from Go source positions (Truffle Chrome Inspector
    debugging depends on it; Delve is impossible for this architecture).

23. SPEC.md:271–272 — the Sulong fork is closed: spike 6 landed COMMIT-DIRECT (6 vs 19),
    *"The fork re-opens only on spike 6's re-test triggers"* (GG-0047).
    → Paraphrase: Phase 1 proceeds on the direct-AST route, no fallback work needed.

## D. defer / panic / Goexit (SPEC §3.5; GG-0019)

24. SPEC.md:441 — *"**Defer** = explicit per-goroutine chain (open-coded defers are a later phase, not v1)."* (GG-0019)
    → Paraphrase: implement defer as an explicit per-goroutine defer chain in v1;
    open-coded (gc-style inline) defers are explicitly NOT required in Phase 1.

25. SPEC.md:442–444 — *"**Panic** = `AbstractTruffleException` carrying the value with lazy traces (recovered panics stay cheap; the `ControlFlowException` alternative is rejected because fatal panics need printed traces anyway — scoped verdict, T5 §4.4)."* (GG-0019)
    → Paraphrase: panic is a Truffle `AbstractTruffleException` carrying the panicked
    value with lazily-computed stack traces; `ControlFlowException` for panic is
    rejected (§10.2 ledger).

26. SPEC.md:445 — *"**Goexit** = `ControlFlowException`."* (GG-0019)
    → Paraphrase: `runtime.Goexit()` unwinds via `ControlFlowException` (no trace needed).

27. SPEC.md:445–446 — *"**panic(nil)** → `*PanicNilError` (Go ≥1.21 semantics, replicated exactly)."* (GG-0019)
    → Paraphrase: `panic(nil)` must produce `*PanicNilError`, replicating Go ≥1.21
    semantics exactly (differential-test the boundary).

28. (Context, §3.5:432–433) — *"goroutine-local state (defer chain, panic context) lives in virtual-thread-scoped holders"* (GG-0017).
    → Paraphrase: the defer chain and panic context are goroutine-local state; in Phase 1
    (single main goroutine; concurrency is Phase 2) they hang off the executing virtual thread.

## E. Intrinsic surface + linkname contract (SPEC §3.6; GG-0020, GG-0021) — the frozen interface

29. SPEC.md:481 — *"The runtime exposes a **versioned intrinsic surface of ~60–70 functions** `[VERIFIED]` by source measurement of what the coupled stdlib needs"* (GG-0020)
    → Paraphrase: the runtime contract is ~60–70 versioned functions; the count comes
    from a source measurement of what the coupled stdlib needs.

30. SPEC.md:481–487 — the normative function catalog (quote): *"memmove/memequal, makeslice/growslice, map/chan/select ops, interface/itab machinery, panic/defer, scheduler glue, netpoller API contract (satisfied by blocking NIO per §3.5 — the contract `net` calls into, not the deleted event loop), atomics, timers, fastrand, write barrier, traceback — plus **exact `rtype`/itab layouts** for `reflect`'s 243 `unsafe` uses `[DECIDED]`."* (GG-0020)
    → Paraphrase: the intrinsic surface covers: memmove/memequal; makeslice/growslice;
    map/chan/select ops; interface/itab machinery; panic/defer; scheduler glue;
    the netpoller API contract (semantics only, realized by blocking NIO); atomics;
    timers; fastrand; write barrier; traceback; plus exact `rtype`/itab layouts.
    **Phase-1 scope (PHASES.md:67–69): "intrinsic surface *stubs* sufficient for language
    tests"** — stubs may stand in for the full Phase-2 runtime, but the *signatures*
    must be the frozen ones (see §H below).

31. SPEC.md:487–489 — *"Linkname compatibility between graalgo-compiled runtime and stdlib is **by symbol name, not gc ABI** (52 stdlib→runtime symbols + ~100 reverse edges `[VERIFIED]`)"* (GG-0021)
    → Paraphrase: linkname compatibility = symbol names only (not gc ABI), because
    graalgo compiles both runtime and stdlib; measured coupling is 52 stdlib→runtime
    symbols + ~100 reverse edges.

32. SPEC.md:489–490 — *"The tax is per-release churn of internal symbols, paid via a **linkname-diff step** in every upgrade's Phase 2 (GG-0021)."*
    → Paraphrase: internal-symbol churn is a known per-release tax handled by a
    linkname-diff tooling step (spike-4 falsifier: skip-list growth > 15 twice →
    re-scope, SPEC.md:489+§8 row 4).

## F. Generics strategy

33. PHASES.md:64 — Phase-1 goal is *"complete Go language (through generics) on the Truffle AST"*; done-gate (1) requires *"language-facing suites (incl. generics) pass differentially vs gc"*; self-attack includes *"pathological generics"* (PHASES.md:73–74); §5.4 lists *"generics instantiation"* as a fuzz feature-space dimension (SPEC.md:687).
    → Paraphrase: generics must be fully supported in Phase 1 (instantiation is a
    named fuzz/self-attack target).
    → **Gap:** the SPEC prescribes no instantiation strategy (monomorphization vs
    dictionary-passing/stenciling) — see item 44.

34. SPEC.md:243–244 — normative constraint bearing on generics: go1.27's stdlib uses
    *"generic methods on `math/rand/v2.Rand`"*, which is why frontend and stdlib must
    re-vendor atomically.
    → Paraphrase: generic methods must work end-to-end; the frontend emits typed
    generic instantiations via `go/types` and the node factory lowers them.

## G. `-toolexec` argv contract (SPEC §3.1, §4.2; GG-0011, GG-0059)

35. SPEC.md:224–228 — *"Ship the **stock Go distribution**; swap only the compile backend behind the existing `-toolexec` seam (`src/cmd/go/internal/work/build.go:354` wraps every `compile`/`asm`/`cgo`/`link`/`cover`/`pack` invocation; `toolID()` queries `tool -V=full` *through* the wrapper) `[VERIFIED]` on go1.27.1."* (GG-0011)
    → Paraphrase: Phase 1 (and the program) keep the stock driver; the backend is
    swapped at the `-toolexec` seam intercepting all six tool invocations
    (compile, asm, cgo, link, cover, pack).

36. SPEC.md:601–603 — *"The wrapper intercepts the tool invocations the driver routes through `-toolexec` — `compile`, `asm`, `cgo`, `link`, `cover`, `pack` `[VERIFIED]` — and answers `tool -V=full` with the graalgo backend identity so action IDs auto-namespace `[DECIDED]`."* (GG-0059)
    → Paraphrase: the wrapper must answer `tool -V=full` with the graalgo backend
    identity so that graalgo action IDs auto-namespace against gc in a shared GOCACHE.

37. SPEC.md:603–606 — *"The argv mapping per tool is documented and versioned at the same Phase-1 freeze; `GOFLAGS=-toolexec=…` handling (user flags composing with the wrapper) is part of the contract."* (GG-0059)
    → Paraphrase: per-tool argv mappings are documented+versioned at the freeze, and
    user-supplied `GOFLAGS=-toolexec=…` composition is in-contract (not an edge case).

38. SPEC.md:606–607 — *"Divergence from the stock tool's argv expectations is a drop-in bug, caught by the txtar matrix."* (GG-0059)
    → Paraphrase: any argv deviation vs stock tools is a drop-in bug by definition,
    caught by the 929-txtar conformance matrix (spike 5: PASS on 1.27.1 linux/amd64).

39. SPEC.md:230–233 — *"Forking `cmd/go` is the **escape hatch**: permitted only if argv interception proves insufficient"* (GG-0011).
    → Paraphrase: the driver fork is a priced, last-resort escape hatch, never the plan.

## H. Interface freeze (SPEC §4.1, §4.2; GG-0059) — the Phase-1 done-gate that Phase 4 depends on

40. SPEC.md:591–594 — *"The intrinsic signatures (GG-0020) are the contract between the Truffle AST lowering (Phase 1) and the Java runtime library (Phase 2) `[DECIDED]`. At the **Phase-1 interface freeze** the signatures are versioned and documented; Phase 4 (driver) starts in parallel only after this freeze."* (GG-0059)
    → Paraphrase: the freeze deliverable = **versioned + documented intrinsic signatures**
    forming the contract between AST lowering (Phase 1) and the Java runtime library
    (Phase 2); Phase 4 may not start in parallel before it.

41. SPEC.md:594–595 — *"Compatibility is checked in CI: the AST may only call frozen signatures."* (GG-0059)
    → Paraphrase: CI enforces the freeze mechanically — the AST lowering may reference
    only frozen intrinsic signatures (build the CI gate with the freeze).

42. PHASES.md:73 — done-gate (3): *"**interface freeze**: AST↔runtime intrinsic signatures and the `-toolexec` argv contract versioned and documented — Phase 4's parallel start depends on this."*
    → Paraphrase: the freeze has exactly two components: (a) AST↔runtime intrinsic
    signatures, (b) the `-toolexec` argv contract.

43. SPEC.md:595–597 — **Kill trigger:** *"interface freeze impossible without breaking the toil bound → architecture replan."* (PHASES.md §3)
    → Paraphrase: if a stable interface cannot be frozen within the toil bound, the
    program replans the architecture rather than proceeding with a drifting interface.

## I. Phase-1-relevant testing strategy gates (SPEC §5; GG-0029–GG-0038)

44. SPEC.md:669–671 — *"``src/test/`` language suite (356 files `[VERIFIED]`) — the spec-semantics anchor independent of the stdlib — plus new-feature tests written **red-green first** (they must fail before the lowering change). Runs per-commit on frontend changes."* (GG-0032)
    → Paraphrase: Phase-1 differential corpus = the 356-file `src/test/` language
    suite; every new lowering lands red-green-first; run per-commit on frontend changes.

45. SPEC.md:686–690 — *"program generation across the feature space (generics instantiation, interface dispatch, closure capture, defer/panic/recover chains, select, map iteration, atomics) through the §5.5 harness"*; *"a gosmith-class Go program generator is `[UNVERIFIED]` — verify existence/license and gc-parity before depending on it, else build a small template-based generator."* (GG-0037)
    → Paraphrase: Phase-1 fuzz-soak covers generics instantiation, interface dispatch,
    closure capture, defer/panic/recover chains, map iteration, atomics; a gosmith-class
    generator is unverified — fall back to a small template generator if needed.

46. SPEC.md:697–701 — *"The reference `go` toolchain is ground truth `[DECIDED]`. **Compared:** stdout bytes, stderr bytes (after normalization), exit codes, panic messages and goroutine traces **modulo addresses** (regex-scrub `0x…`, goroutine IDs, timestamps, durations, sandbox-external paths)… **Excluded:** wall-clock timings, absolute addresses, GC statistics, raw goroutine scheduling."* (GG-0030)
    → Paraphrase: differential comparison = stdout/stderr bytes, exit codes, panic
    messages + traces with addresses/IDs/timestamps scrubbed; timings, absolute
    addresses, GC stats, raw scheduling are excluded by design.

47. SPEC.md:723–725 — *"The harness is validated by **mutation** `[DECIDED]`: deliberately break graalgo's lowering (invert a comparison, drop a defer) and confirm the harness goes red."* (GG-0034)
    → Paraphrase: every oracle must be mutation-validated (e.g. inverted comparison,
    dropped defer must turn the harness red); no tautological tests.

## J. [OPEN] / [UNVERIFIED] items, gaps, and tensions touching Phase 1 scope

48. **[OPEN]** SPEC.md:1233 — *"GraalVM/JDK distribution | **to be pinned in Phase 0** (GG-0027) | `[OPEN]`"* (explicitly non-go/no-go; the note at SPEC.md:1239–1242 clarifies the *decision to pin and verify* is `[DECIDED]`, only the version string is open).
    → Phase-1 touch: the loop-node choice in item 17 (`LoopNode`/`RepeatingNode` for OSR) depends on §7.12 (SPEC.md:988–993): *"OSR behavior in Native Image is verified against the pinned GraalVM distribution in Phase 0… its exact behavior inside Native Image is `[UNVERIFIED]` until measured."* If the pin lacks usable OSR, the fallback is *"deopt-from-AOT-baseline only"* — a constraint the Phase-1 loop lowering must be designed to accommodate. Also "one Truffle AST… the modes differ only in packaging" (§3.9) means Phase-1 AST design may embed no mode-specific assumptions.

49. **[UNVERIFIED]** SPEC.md:688–690 — *"a gosmith-class Go program generator is `[UNVERIFIED]` — verify existence/license and gc-parity before depending on it, else build a small template-based generator."* (GG-0037)
    → Directly touches Phase-1 done-gate (2): *"fuzz-soak layer of the testing strategy operational on the language subset."* Until the generator is verified, Phase 1 should plan on the small template-based generator.

50. **Gap: no generics instantiation strategy is prescribed.** The SPEC mandates generics end-to-end (item 33) and names *"generics instantiation"* as a fuzz/self-attack dimension, but no section decides monomorphization vs dictionary-passing vs stenciling, nor where instantiation happens (frontend service vs node factory vs intrinsic). `go/types` exposes instantiated types, so the information exists — but the lowering strategy is unspecified and will need a decision before the interface freeze (it affects descriptor layout and intrinsic signatures).

51. **Tension (flag, not contradiction): panic(nil) vs `-lang` gates.** Item 27 mandates *"panic(nil) → `*PanicNilError` (Go ≥1.21 semantics, replicated exactly)"* while item 3 (GG-0013) requires per-file `-lang` gates *"exactly like the stock toolchain."* The SPEC does not say whether pre-1.21 `panic(nil)` behavior (plain nil, no `*PanicNilError`) is required when `-lang<1.21` is in effect. Needs a parity-stance decision (GG-0045) before the freeze.

52. **Implication for the freeze (Phase-4-critical):** the frozen signature set is NOT limited to what Phase 1 implements. PHASES.md:67–69 scopes Phase 1 to *"intrinsic surface stubs sufficient for language tests,"* but §4.1 freezes *"the intrinsic signatures (GG-0020)"* — the full ~60–70-function catalog of item 30, including scheduler glue, the netpoller API contract, timers, and fastrand, whose real implementations land in Phase 2. Phase 1 must therefore version the signatures for entries it only stubs; Phase 2's runtime library builds against them, and Phase 4's driver work starts in parallel on that basis.

53. **No contradictions found** between §3.2/§3.3/§3.4/§3.6/§4.1/§4.2 and PHASES.md §3 after a full pass. Notable consistencies verified: (a) §4.1 ("Phase 4 starts in parallel only after this freeze") ≡ PHASES.md §9 ("driver (4) and pipeline (6) parallelize after the Phase-1 interface freeze"); (b) "defer = explicit chain, open-coded later" (§3.5) is compatible with Phase-1 single-goroutine scope; (c) "linkname by symbol name" (§3.6) is compatible with the verbatim-frontend decision since graalgo compiles both sides; (d) the spike-6 COMMIT-DIRECT verdict (§7.1) closes the only open fork that could have invalidated the Phase-1 AST plan.

## K. Requirement-ID quick map (Phase-1-relevant GG IDs and their primary homes)

54. GG-0011 (driver wrapper via -toolexec): SPEC §3.1 (items 35, 39), §4.2 (items 36–38).
    GG-0012 (verbatim frontend vendoring): SPEC §3.2 (items 1, 2, 4). GG-0013 (no language pinning): SPEC §3.2 (item 3).
    GG-0014 (one Truffle AST): SPEC §3.3 (items 17–20). GG-0015 (Java-object value model): SPEC §3.4 (items 5–14).
    GG-0016 (unsafe.Pointer v1): SPEC §3.4 + §7.3 (items 15, 16). GG-0017 (goroutines on vthreads — Phase 2, context only): SPEC §3.5 (item 28).
    GG-0019 (panic/defer/Goexit model): SPEC §3.5 (items 24–27). GG-0020 (intrinsic surface): SPEC §3.6 (items 29, 30), §4.1 (items 40, 41).
    GG-0021 (linkname by symbol name): SPEC §3.6 (items 31, 32). GG-0024 (debugging/SourceSections): SPEC §3.10 (item 22).
    GG-0030/GG-0031 (differential oracle + nondeterminism controls): SPEC §5.5 (item 46). GG-0032 (conformance corpora): SPEC §5.2 (item 44).
    GG-0034 (mutation-validated oracles): SPEC §5.7 (item 47). GG-0037 (generative differential fuzzing): SPEC §5.4 (items 33, 45).
    GG-0050 (AOT baseline quality): SPEC §3.9 (item 21). GG-0054 (OSR verification): SPEC §7.12 (item 48).
    GG-0059 (interface freeze): SPEC §4.1 + §4.2 (items 40–43).
    Full program index: SPEC.md Appendix B (lines 1155–1228); pinned dependencies: Appendix C (lines 1230–1242).

---
*End of extract. 54 numbered items. Single `[OPEN]` (GraalVM pin version string, non-go/no-go) and single `[UNVERIFIED]` (gosmith-class generator) touch Phase 1; no contradictions found; two flags recorded (items 50, 51) and one freeze-scope implication (item 52).*
