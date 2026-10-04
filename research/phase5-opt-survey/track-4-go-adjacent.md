# Track 4 — Go-adjacent (golang/go issue mining + community blogs/talks + TinyGo/llgo/gccgo)

**Track:** 4 (Go-adjacent). **Started:** 2026-10-04 ~08:35 CEST. **Status:** in progress — wave 1 of a long-term background endeavor (per 2026-10-04 program-owner directive: systematic, indefinite waves; negative results are first-class).

Sub-tracks: (a) golang/go issue-tracker mining [primary], (b) community blogs/talks, (c) TinyGo / llgo / gccgo.

Candidate template used throughout:
- `### <name>` — Idea (≤3 lines) / Precedent (issue # / URL) / Why gc doesn't do it (decline reason where one exists) / Architectural fit (edge(s); does the edge change the decline calculus?) / Benchmark sketch / Open risks.

---

## (a) Issue-tracker mining

Method: GitHub issue search + `api.github.com` reads via browser.open (GitHub HTML search pages return only issue numbers; the REST API returns titles/bodies). Note: recalled issue numbers from memory proved unreliable (#18606 was a cmd/go issue, not a BCE proposal) — every citation below was verified by fetching.

### Confirmed leads (verified by fetch)

- golang/go#66318 "proposal: cmd/compile: allow bound check skip via directive" → **duplicate of golang/go#47529** "proposal: cmd/compile: no bound check compiler" (the `//go:noboundcheck` proposal). #47529 reactions: +1:0 / -1:11 — declined by negative consensus. Decline reason (from #66318 thread): "the individual directive has already been proposed and declined" — the Go team rejects a user-facing unsafety directive for skipping checks. **Obstacle named:** no safe, ergonomic way to say "this check is redundant" without an unsafe directive.
- golang/go#64673 "cmd/compile: PGO devirtualization of partial interface implementations" — OPEN. `io.NopCloser(bytes.Reader)` case: profile reports `foo -> bytes.Reader.Read` but devirt rejects because `bytes.Reader` doesn't implement the full `io.ReadCloser` (the wrapper is invisible to the profile). Suggested fix: type-check only the called method, not the whole interface ("risks more false positives in ambiguous situations").
- golang/go#72036 "cmd/compile: teach escape analysis to conditionally stack alloc interface method call parameters like a slice in w.Write(b)" — OPEN. Key passage: even with PGO devirtualization rewriting `w.Write(b)` into a type-asserted fast path, "the byte slice still must be heap allocated given the function can be called with something other than `GoodWriter`."
- golang/go#77088 "cmd/compile: conditional escape analysis (or better synergy with inlining)" — OPEN. Inlining melts the call away but escape analysis keeps the callee's conservative judgment; suggests parameterizing the inliner's summary over a caller-supplied allocator.
- golang/go#76488 "proposal: cmd/compile/internal: Exploiting the capabilities of reassigned oracle to make escape analysis a little less conservative" — proposal (open). `*ppd = pc` (single-assignment deref store) treated as heap hole; could add value-flow edge via the reassign oracle.
- golang/go#20533 "cmd/compile: improve escape analysis of make([]T, n) where n is non-constant" — OPEN since 2017. Non-constant-size non-escaping slice → heap because no `alloca`. Motivating workload named in the issue: **a bytecode interpreter's operand stack** (heap alloc + write barrier per function entry). Suggests decoupling "escapes to heap" from "requires a write barrier", or `alloca`.
- golang/go#77720 "cmd/compile: field access on struct-returning method copies entire struct" — fixed for the *small*-load case by CL 748200 ("forward small Load through Move"). General case (large structs, partial field use) still copies. Motivating workload: `unique.Handle[T].Value().field` in a hot loop (40-byte struct copied = 6 MOVUPS just to read 1 byte).
- golang/go#17566 "cmd/compile: improve inlining cost model" — 2016 umbrella issue (separate expression/control-flow budgets, param/return-size aware cost, intra- vs inter-package). Partially addressed by the new inliner (Go 1.24+ `GOEXPERIMENT=newinliner`); check remaining gaps before claiming.
- golang/go#16798 "proposal: cmd/compile: add tail call optimization for self-recursion" — 71 reactions (+42). Status TBD (fetch throttled); note Go is adding tail calls for embedded interface wrappers in 2026 (CL 751465) — verify whether self-recursive TCO is still missing.
- golang/go#21536 "Proposal: cmd/compile: add a go:inline" — `//go:inline` directive proposal. Status TBD (fetch throttled).
- golang/go#44578 "cmd/compile: optimize maps with bool keys" — proposal milestone; 60.28ns vs 0.75ns in the issue's benchmark. Map-specialization-shaped → backlog overlap (complementary only).
- golang/go#66361 "cmd/compile: special case getting a random map entry?" — `for k, v = range m { break }` idiom (was hot in `database/sql` connection pool; they rewrote the call site instead via CL 572119). Narrow.
- golang/go#23661 "cmd/compile: We can avoid extra mapaccess in m[k] op= r" — open since 2017; related #21687.
- golang/go#43020 "cmd/compile: big map literal creation performance" — map literal init slower than make+assign loop. Map-shaped → backlog overlap.
- golang/go#53048 "proposal: cmd/compile: type layout optimization: struct field reordering" — closed; backlog covers profile-guided struct reorder. Skip.
- golang/go#19331 "cmd/compile,runtime: compile multiway select statements as switch statements" — 2017 proposal, still the old lowering per 2026-10-04 fetch (returns-twice `selectsend/selectrecv/selectgo` setjmp/longjmp dance). Pros named: CFG simplification (caused SSA bugs), gccgo already does the switch form, cheaper composite-literal init, fewer fields. **gccgo data point for sub-track (c).**
- golang/go#80537 "proposal: cmd/compile: add `GOEXPERIMENT=2pointers-closures`" — open proposal: change closure representation to 2 words `{descriptor, receiver}` (like a single-method interface) so `closure := ptr.method` compiles to LEAQ+MOV instead of a heap allocation. Explicitly targeted at the **typescript-go** compiler workload. ABI-breaking (`Sizeof(func())` changes) → the obstacle for gc.
- golang/go#80428 "runtime: add indirect extendible hash map (ixmap) as alternative map implementation" — Vladimir Makarov's complete implementation + benchmarks, `GOEXPERIMENT=ixmap`. gc-team-adjacent in-flight work — do NOT claim; watch.
- golang/go#76200 "proposal: Add Built-in Set Type to Go" — language change; pushback "no good reason to put it in the language when it can be a library instead." Not optimizer material.
- golang/go#39428 "cmd/compile: struct equality code optimization" — struct `==` codegen worse than array `==`. Partially addressed by the Nov-2025 equality-function rework (CL 30dff416: one eq func per "signature", 634→286 eq funcs in the go binary).
- String concatenation: no gc issue found — gc already folds `s1+s2+...` chains into one `concatstrings` call; loop `s += x` is algorithmic (quadratic), not a compiler gap. → negative result.
- Atomics (`sync/atomic`): no optimization proposals found in search — only historic bugs (#19182 loop elision, #16985, #43007 race interaction). gc already intrinsifies atomics to machine instructions. → negative result.
- golang/go#80972 "internal/runtime/maps: use fxhash for very small keys" — OPEN, 2026, help-wanted; gc team actively working on map hashing itself (mcy's CL series). gc's own in-flight work — do not claim.
- golang/go#47174 "proposal: cmd/compile: add a optimization" — reactions +1:1/-1:6 (declined by vote); content TBD.

*(mining continues: strings, select, goroutine spawn, reflect, append, interface comparison, code layout, range-func, atomics)*

---

## Candidate drafts

### C1. Speculative bounds-check hoisting via loop versioning (guard + deopt)

**Idea.** For hot loops where gc's BCE leaves per-iteration checks (e.g. index expressions the SSA BCE can't prove in-range), the JIT compiles a versioned loop: one guard up front proving `i < len(s)` for the whole trip count (a single range check on the loop bounds), then a check-free body; on guard failure, deopt to the checked version. No user directive needed.
**Precedent.** golang/go#47529 (declined `//go:noboundcheck`, +1:0/−1:11) and its duplicate #66318.
**Why gc doesn't do it.** The decline was aimed at the *user-facing unsafe directive*, not at eliminating the checks: "the individual directive has already been proposed and declined." An AOT compiler can't insert a speculative guard with a deopt fallback — the obstacle the issue names is ergonomic/safety, i.e. exactly what a JIT guard removes.
**Architectural fit.** Edge 3 (speculative JIT with guards + deoptimization) — the decline calculus changes because the "directive" becomes a compiler-inserted, semantics-preserving guard; edge 4 (always-on profiler finds the hot loops where BCE fails). gc+PGO cannot do this: PGO has no deopt mechanism.
**Benchmark sketch.** Tight numeric loops over slices with non-trivial index arithmetic where `-d=ssa/check_bce/debug=1` shows un-eliminated checks (e.g. stencil/convolution kernels, `image/draw`-style loops; the #21735 thread cites an 11% image/draw regression from a single extra LEA — same workload family is check-sensitive).
**Open risks.** Guard cost must be amortized (needs trip-count ≫ 1; profiler must confirm); deopt path must be cold-verified; interaction with gc's existing BCE (only version loops where BCE demonstrably fails).

### C2. Scoped arenas for dynamic-size non-escaping allocations (compiler-managed `alloca`)

**Idea.** `make([]T, n)` with non-constant `n` that provably doesn't escape is heap-allocated by gc for lack of `alloca`. Allocate it from a per-goroutine scoped arena (bump pointer, freed at function return — explicit scope, not the Go stack, so no interaction with stack growth/copying), skipping both the heap alloc and the write barrier. Sound: lifetime bounded by the frame; arena freed on return/panic via the existing defer/panic machinery.
**Precedent.** golang/go#20533 (open since 2017).
**Why gc doesn't do it.** gc's stack-frame layout is fixed-size; true `alloca` conflicts with stack copying/growth bookkeeping, and the issue notes the alternative (decoupling "escapes" from "needs write barrier") was never built. The obstacle is frame-layout rigidity in the AOT pipeline.
**Architectural fit.** Edge 1 (descriptor value model: per-allocation-site layout — this site's backing array lives in the arena, not the heap, invisible to the type system); edge 2 (user-space scheduler: arena lifetime tied to goroutine scheduling, precise "function returned" signal; arena can be recycled across calls on the same goroutine). AOT gc can't change per-site layout without ABI/frame changes; a JIT owning frame layout can.
**Benchmark sketch.** The issue's own motivating workload: a bytecode interpreter with a non-constant-size operand stack (alloc + write barrier per function entry today). Also: `bytes.Buffer`-style growth in parsers, JSON decoders with dynamic scratch.
**Open risks.** Panic paths must free the arena (hook into panic unwinding); recursion depth × arena size bounded by guard; must not regress when `n` is huge (fallback to heap above a threshold — needs a size guard, which is a natural JIT branch).

### C3. Aggregate copy elision via field-use projection (call returns + range values)

**Idea.** gc copies whole aggregates by value and then reads a few fields: (1) at call sites using only some fields of a by-value struct return (`h.Value().typ` → whole 40-byte struct copied = 6 MOVUPS to read 1 byte); (2) in `for _, v := range sliceOfStructs` loops, where each element is copied per iteration even if the body touches one field (documented ~2x cost on trivial bodies). The JIT computes the field-use projection at the hot use site and materializes only the used fields — per-call-site callee specialization for (1) (no deopt needed: projection is a static property of the call site), loop-by-index rewrite loading only used fields for (2).
**Precedent.** golang/go#77720 (CL 748200 fixed only the *small*-load case); golang/go#33838 ("unnecessary array copying in for range loop", 2019, still open); golang/go#31588 ("range loops on slices of large types possibly surprisingly expensive" — "I could imagine the compiler looking at the contents, establishing that only one item is being accessed, and just copying that one item"; related #24416); golang/go#79819 (maintainer: "the compiler could do a better job here. It seems like a pretty low priority case though").
**Why gc doesn't do it.** AOT ABI fixes callee return layouts (needs inlining or a second callee version = code bloat without usage data); range-value copy elision is judged "low priority" — the obstacle is cost/benefit in a static compiler, not soundness.
**Architectural fit.** Edge 1 (per-call-site / per-loop representation decisions, not type-global); edge 4 (profiler identifies hot projection sites); edge 3 (JIT clones only hot sites — pay-as-you-go code, flipping gc's cost/benefit). gc+PGO has profile data but no per-site variant mechanism.
**Benchmark sketch.** `unique.Handle[T].Value().field` hot loop (#77720); `for _, v := range structs` with single-field body vs. `for i := range` rewrite (#31588's ~2x case).
**Open risks.** Proving no other observable effects (address-taken values, reflection, `v` escaping the loop body); generics per-instantiation projections; must not fight the inliner (don't project what's already scalarized); range case must preserve "evaluate once" semantics for the range expression.

### C4. Speculative per-call-site escape analysis behind interface calls (guarded stack allocation)

**Idea.** `w.Write(b)` with `w io.Writer`: gc heap-allocates `b` because *some* implementation could retain it. With the always-on profiler we know the hot concrete types at this call site and can prove *those* don't retain the argument; compile a fast path that stack-allocates `b` and dispatches to the known types (type-check cascade), falling back to the generic heap path for unseen types (recompile/extend on new types — a guard, not UB).
**Precedent.** golang/go#72036 — the issue explicitly notes PGO devirtualization alone can't fix this: "the byte slice still must be heap allocated given the function can be called with something other than `GoodWriter`."
**Why gc doesn't do it.** The set of possible implementations is open; AOT must be sound for all of them, so the conservative answer (heap) is the only answer. The obstacle is the open-world assumption — exactly what a speculative guard discharges.
**Architectural fit.** Edge 3 (guard on the dynamic-type set + fallback; recompile when a new type appears — this is the textbook JIT speculation win gc structurally cannot do); edge 4 (profiler supplies the hot-type set and the non-retention evidence per type). Complements (does not duplicate) the backlog's "speculative interface devirtualization cascade": that item devirtualizes the *call*; this one stack-allocates the *argument* using the same type knowledge.
**Benchmark sketch.** `io.Writer`-style hot paths: JSON/log encoders writing into an `io.Writer`, `fmt.Fprintf` wrappers, `hash.Hash` writes. Measure allocs/op → 0 on the fast path.
**Open risks.** Non-retention must be proven per concrete callee (needs an escape summary per type — the "parameterized summary" idea from #77088 is the same machinery); the type-check cascade cost must stay below the alloc savings (cap the hot-type set, e.g. ≤3); adversarial type churn could ping-pong recompiles (rate-limit recompilation).

### C5. Per-site fat closures for method values (descriptor-based closure representation)

**Idea.** `f := ptr.Method` allocates a closure object on the heap. Represent the closure at hot creation sites as a 2-word value `{method descriptor, receiver}` (identical shape to a single-method interface) — no allocation, LEAQ+MOV to materialize. The descriptor (in .rodata) carries the method PC plus the metadata reflect needs, so `reflect.ValueOf(f)` still works by consulting the descriptor.
**Precedent.** golang/go#80537 (open proposal, explicitly targeted at the typescript-go compiler workload).
**Why gc doesn't do it.** It changes `unsafe.Sizeof(func())` — an ABI break gc cannot make: the representation of `func` is type-global. The obstacle named is ABI rigidity, not the idea's merit.
**Architectural fit.** Edge 1 (descriptor-based value model — representation ≠ semantics: the JIT can use the fat representation per allocation site / per value and convert at ABI boundaries, getting the optimization without the global ABI break); edge 4 (profiler finds hot method-value sites). gc+PGO cannot vary representation per site.
**Benchmark sketch.** typescript-go compile workload (the proposal's named target); any codebase with hot `x.Method` captures (e.g. `http.HandlerFunc`-style adapters, sort comparators).
**Open risks.** Calling convention for the fat closure (descriptor-driven dispatch vs. adapter thunk); reflect metadata completeness (method value's `Func` must expose the same `reflect.Value` semantics); GC must understand the descriptor word (or keep it out of scanned memory — .rodata pointer is fine).

### C6. JIT-specialized multiway select lowering

**Idea.** gc lowers multiway `select` to the returns-twice `selectsend/selectrecv/selectgo` dance (setjmp/longjmp-like), which pessimizes the CFG and blocks SSA optimization. The JIT compiles hot `select` statements into a bespoke fast path: inline the buffered-channel readiness checks and the lock-ordering for the statement's fixed case set, falling back to the generic runtime select when channels are unbuffered/contended or the case set's behavior changes.
**Precedent.** golang/go#19331 (2017; still the old lowering per 2026-10-04 fetch; notes gccgo already uses the switch form).
**Why gc doesn't do it.** The proposal's own cons: switch lowering uses binary search vs. select's direct PC jump; and AOT must handle the fully general case (any channel combination, blocking). The obstacle is generality without usage data.
**Architectural fit.** Edge 2 (user-space scheduler signals: waiter counts and channel buffer states tell the JIT which cases are actually contended — specialize the observed shape); edge 3 (guard + fallback to the generic select); edge 4 (profiler finds hot selects). Complements the backlog's "adaptive concurrency primitives" (that item is about primitive behavior; this is about select *code generation*).
**Benchmark sketch.** Channel fan-in/fan-out dispatch loops (pub/sub routers, `net` poller-adjacent multiplexers); measure a hot 3–4-case select in a tight loop.
**Open risks.** Select fairness semantics (pseudo-random case choice) must be preserved exactly; lock-ordering for multi-channel select must remain deadlock-free; fallback must be provably taken whenever the observed shape is violated.

---

### C7. Read-only `[]byte(s)` copy elision (borrow the string buffer)

**Idea.** `[]byte(s)` always allocates + copies in gc, even when the resulting slice is only ever read (never written, never retained past the string's lifetime). At hot conversion sites where the JIT can prove read-only use (all uses are loads, no escape of a mutable reference), alias the string's buffer directly: zero alloc, zero copy. Soundness caveat (learned from TinyGo's bugfix): any write through the slice must be detected — the proof must cover all uses including inlined callees; fall back to the copying conversion where the proof fails.
**Precedent.** TinyGo's `StringToBytes` transform pass (tinygo-org/tinygo PR #3923: "convert any read-only use of a `runtime.stringToBytes` call to use the original string buffer instead", plus the follow-up soundness fix; v0.34 changelog: "transform: optimize range over `[]byte(string)`"). gc only elides at blessed sites for the *other* direction (`m[string(b)]`, `switch string(b)`, comparisons); golang/go#26498 (constant-folding of conversions, still NeedsInvestigation) shows even the constant case is unbuilt.
**Why gc doesn't do it.** No general read-only-use analysis for the conversion result; the team's posture (per #26498 discussion) is caution about the sharp edges (read-only memory faults on write). The obstacle is analysis precision, which a JIT with per-site profiling and exact use-tracking can afford.
**Architectural fit.** Edge 1 (per-allocation-site representation: this site's `[]byte` is an immutable alias view, not an owned copy — representation ≠ semantics, invisible to the type system); edge 4 (profiler finds hot conversion sites so the analysis cost is spent where it pays). Weaker edge-3 involvement (no speculation needed when the proof is static; a guard could cover cross-function uses in the JIT).
**Benchmark sketch.** `h.Write([]byte(s))`-style hashing/encoding loops over strings (hash.Hash has no WriteString); `for range []byte(s)` byte-scan loops. Measure allocs/op 1→0 and memcpy elimination.
**Open risks.** Write-detection must be airtight (TinyGo shipped this bug once — needs a red-green regression test); string data may live in read-only pages (faults on missed writes are unrecoverable); GC liveness: the slice must keep the string alive (pointer is enough); must not apply when the slice is passed to code that may write (e.g. `io.Reader`-shaped APIs that fill the buffer — those are writes).

---

## (b) Community blogs / talks

### Covered
- go101.org "Go Optimizations 101", BCE chapter (https://go101.org/optimizations/5-bce.html, Go 1.25): catalog of BCE works/doesn't-work cases; the paired-slice `NumSameBytes_1` case (needs a manual `if len(x) > len(y) { panic("unreachable") }` hint to kill `y[i]`'s check) directly motivates C1's guard approach; strided-loop and `min(len(a),len(b))` gaps noted.
- dgryski/go-perfbook (https://github.com/dgryski/go-perfbook, 10.9k stars, actively maintained): "Runtime and compiler" section bullets — interface call cost, `convT2E/convT2I`, type assertion vs switch, defer, map special-cases ("map for byte/uint16 not optimized; use a slice instead" → map-specialization backlog overlap), BCE, `[]byte<->string` copies, "two-value range will copy an array, use the slice instead" (mdempsky/rangerdanger — the #15812/#33838 family feeding C3).
- golang-design/under-the-hood (https://github.com/golang-design/under-the-hood): community book, part5toolchain ch15compile/optimize.md (§15.3.3 devirtualization) — documents what gc *does*; useful as the "gc already does this" reference for dedup.
- titpetric/gozero docs (https://github.com/titpetric/gozero/blob/HEAD/docs/inlining.md, overheads.md): measured gc inlining/escape-analysis behavior on interpreter-shaped workloads ("compiler can never inline across [closure] calls"; inlining feeds escape analysis: json/native 16→14 allocs).

### To cover (wave 2+)
- Felix Geisendörfer, Jaana Dogan, William Kennedy, Bryan Boreham, Phil Pearl, Michael Stapelberg posts; GopherCon compiler talks; follow citations between posts (perfbook TODO list is a citation source).

---

## (c) TinyGo / llgo / gccgo

### TinyGo (tinygo.org, LLVM-based) — read the docs, not memory
- Pipeline (https://tinygo.org/docs/concepts/compiler-internals/pipeline/): Go SSA → LLVM IR → handpicked LLVM passes + TinyGo-specific transforms (escape analysis, `string`-to-`[]byte` opts) → full LLVM optimizer. Default `-opt=z` (size-aggressive; inliner threshold slashed).
- Differences from Go (tinygo-site `differences-from-go.md`): (1) **compile-time global initialization** — package-level initializers interpreted at compile time into `.data` (gc initializes globals at startup via `sinit`); a startup/binary-size win, not a steady-state win → not a Phase-5 candidate. (2) **Interfaces are `{typecode, value}` with method lookup at the call site, no itab** — *slower* dispatch than gc, fewer allocs at interface creation; explicitly a size trade, not an optimization gc lacks. (3) **Whole-program single-step compilation** — more optimization *opportunity* in principle, but TinyGo's wins are size/MCU, and it has no PGO story.
- Applicable find: the **StringToBytes read-only copy elision** (above) — credited as precedent for C7. Everything else performance-relevant in TinyGo is LLVM's generic middle-end, not Go-specific.
- Verdict: no other applicable optimization; TinyGo never claims to beat gc+PGO on server benchmarks (its design center is binary size for MCUs/wasm).

### llgo
- Original `go-llvm/llgo`: **dead** — LLVM discourse "[RFC] Remove the Go bindings" (2022): "these bindings were originally added for llgo, which appears to be dead now."
- `xgo-dev/llgo` (new, active since Dec 2023, 7,460 commits, v1.0.2, LLVM 22, Go 1.27 syntax): goal is **C/Python/JS interop** ("LLGo := Go * C ecosystem"), not beating gc. Goroutines are **1:1 OS threads** (structurally worse for goroutine-heavy Go). They run 68 benchmark cases vs a Go baseline (xgo-dev/benchmarks `ci/llgo-performance/README.md`, benchstat, Go = baseline) — screened to be "within 20x of the Go baseline"; no claim of beating gc on any named benchmark; no PGO.
- Verdict: nothing applicable.

### gccgo
- Frontend stale (language support lags gc by years); no PGO-for-Go story; historically often *slower* than gc on Go benchmarks (runtime/ABI). Its one documented better lowering — multiway `select` as a switch over a single `runtime.select` call (per golang/go#19331, "gccgo already does this according to @ianlancetaylor") — is captured as precedent for C6.
- Verdict: nothing applicable beyond C6's precedent.

## Surveyed, nothing applicable

- TinyGo (sub-track c): optimizations are size/MCU-oriented (LLVM `-opt=z` default, whole-program compile, itab-less interfaces = *slower* dispatch than gc, compile-time global init = startup-only win); no PGO; no claim of beating gc on server benchmarks. The one transferable transform (read-only `[]byte(s)` copy elision) is captured as C7.
- xgo-dev/llgo (sub-track c): active LLVM-based Go (7,460 commits, LLVM 22), but goal is C/Python/JS interop, not performance; goroutines are 1:1 OS threads (structurally worse for goroutine-heavy Go); their 68-case benchstat suite uses Go as the baseline with no beating-gc claims; no PGO story.
- gccgo (sub-track c): stale frontend, no PGO-for-Go; its only documented better lowering (multiway select as a switch) is already captured as C6 precedent.
- String concatenation: no gc issue found — gc already folds `s1+s2+...` chains into one `concatstrings` call; loop `s += x` is algorithmic (quadratic), not a compiler gap.
- Atomics (`sync/atomic`): no optimization proposals found — only historic bugs (#19182 loop elision, #16985, #43007 race interaction); gc already intrinsifies atomics to machine instructions.

---

## Coverage notes (drives wave 2)

Wave 1 (2026-10-04, this file):
- (a) GitHub HTML issue search (`label:Proposal compiler optimization`, `devirtualization` keyword) — titles not shown, only numbers; usable for discovery but not triage.
- (a) `api.github.com` via browser.open: search endpoint works but is rate-limited (10/min unauth) and throttled intermittently (2 throttles hit); individual-issue endpoint also throttled once. Verified ~18 issues by fetch; every cited number verified (recalled numbers proved unreliable — #18606 was a cmd/go issue).
- (a) `browser.search` with `site:` / quoted issue titles: reliable; surfaced #64673, #60032, #52072, #72036, #76488, #77088, #20533, #42165, #43020, #66361, #23661, #44578, #21735, #20138, #24259, #17566, #69885, #21749, #29189, #66318, #47529, #77720, #63726, #19148, #70062, #72132, #77375, #78390, #71439, #66826, #80518.
- (b) go101.org "Go Optimizations 101" BCE chapter (https://go101.org/optimizations/5-bce.html, Go 1.25): catalog of BCE works/doesn't-work cases mined for C1.
- (c) TinyGo: pipeline internals page, differences-from-go page, CHANGELOG (all transform passes v0.34+, incl. StringToBytes read-only `[]byte(s)` elision + soundness fix PR #3923, "optimize range over []byte(string)"), itab-less interface docs, compile-time global init. llgo: original go-llvm/llgo DEAD (LLVM 2022 RFC to remove bindings); xgo-dev/llgo evaluated (active, 7,460 commits, LLVM 22, Go 1.27 syntax, v1.0.2): interop-focused, 1:1 OS-thread goroutines, 68-case benchstat suite with Go as baseline, no beating-gc claims, no PGO → nothing applicable. gccgo: stale frontend, no PGO-for-Go; select-as-switch better lowering already captured as C6 precedent → nothing applicable.
- NOT yet covered (wave 2+): (a) systematic label sweeps — `label:Performance` + `label:compiler/runtime` open issues (thousands; needs paged API work), declined-proposal enumeration via `go.dev/s/proposal-status`; areas strings/`append`/select/goroutine-spawn/atomics/code-layout/reflect/interface-comparison/panic-cost; pre-2020 issues by year; (b) beyond go101: dgryski, Geisendörfer, Dogan, GopherCon compiler talks, citation-following; (c) TinyGo's exact escape-analysis gap vs gc (they re-implemented it — better on any case?); xgo-dev/llgo GC design notes (wasm GC head-cache microbenchmark PR #2543 is GC-internal, not codegen).
