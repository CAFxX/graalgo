# graalgo — Phased Roadmap

**Date:** 2026-10-03 · **Status:** plan, no implementation · **Study:** `FEASIBILITY.md`
**Evidence labels:** `[HIST]` · `[VERIFIED]` · `[PROPOSAL]` · `[DECIDED]` · `[OPEN]`

## 1. Contract & conventions

Every phase below ships only when its **binary done-gates** are met, in the style of the
world-class-implementation contract (`~/workspace/skills/world-class-implementation/SKILL.md`):
spec exists; no placeholders; test matrix (happy path, edge cases, failure modes, hostile inputs,
*combination* tests proving interop); non-tautological tests (red-green/mutation evidence);
full existing suite green; multiple independent peer reviews with all findings remediated and
re-tested; self-attack pass; documentation current and consistent. Gate evidence is delivered
with the work — "done" is audited, not trusted. **A phase that cannot fail is badly designed:**
each phase names its kill criteria. Any escaped defect triggers the escape protocol: fix the
instance, hunt the whole error class, patch the process.

---

## 2. Phase 0 — Spec + kill-or-commit spikes (0.5–1 PY)

**Goal:** retire the top-5 risks (§8 of the study) for <1 PY, or stop the program. Also decides
the two 2×-estimate architectural forks (frontend reuse; driver wrapper-vs-fork).

**Spikes (each ≤ 3 weeks, binary pass/fail verdict):**

1. **asm/cgo spike** — intrinsify amd64 atomics/bytealg, emulate the rest; run affected stdlib
   tests under a stub backend.
2. **Goroutines-on-Loom spike** — ~~1:1 virtual threads + back-edge yields + custom carrier pool~~
   **KILLED 2026-10-03** (systematic scheduling-observable divergence: preemption torture
   0 vs 200,000 latency samples; root cause verified in JDK 25 sources — Loom's FJP carrier
   has no woken-thread priority, no public scheduler API, JVM-global carrier sizing).
   Superseded by **spike 2b — user-space scheduler** (1:1 virtual threads retained;
   indefinite park on quantum expiry + FIFO/runnext token queue + platform-thread drainer +
   in-flight cap = GOMAXPROCS; FJP demoted to dumb thread pool), verdict **PASS**
   (`spikes/02b-userspace-scheduler/REPORT.md`); the differential-vs-gc `runtime`/`sync`
   stress runs now target the 2b design.
3. **Version-tax dry run** — perform a mock upgrade of the spike prototype across one real Go
   release boundary; measure toil against the ≤200 agent-hour bound.
4. **reflect/unsafe spike** — type-layout core of the intrinsic surface + handle-table `unsafe`;
   run `reflect`/`unsafe` suites.
5. **cmd/go coupling spike** — `-toolexec` backend swap across a matrix of txtar scripts.
6. **Sulong evaluation spike** — scored Go→LLVM→Truffle comparison vs direct Truffle AST
   (build cost, version coupling, semantic mismatch catalog).
7. **Testing-harness seed** — differential-vs-gc smoke harness operational from day one
   (it is the development conformance net, not just the upgrade net).

**Done-gates:** (1) spec skeleton closed: all 20 Phase-0 questions answered with evidence
labels, `[OPEN]` count = 0 for go/no-go items; (2) spikes 1–6 each have a written verdict +
reproduction — spike 2's verdict is KILLED (see above), spike 2b's PASS verdict stands in
its place; (3) differential smoke harness runs in CI with mutation-validated oracles;
(4) architecture decision record signed (frontend service, driver wrapper, three modes,
Linux-first, user-space scheduler per ADR-0001); (5) two independent peer reviews of the
spike verdicts, findings remediated.

**Kill criteria:** any spike kill trigger fires without a scoped workaround (see study §8);
the version-tax dry run breaches the toil bound with no credible reduction path; Sulong wins
the scored comparison and the direct-AST estimate doubles (→ replan, not necessarily stop).

---

## 3. Phase 1 — Language core (3–6 PY)

**Goal:** complete Go language (through generics) on the Truffle AST, value model, and
differential smoke harness — no `net`/`os` yet.

**Scope:** `go/parser`+`go/types` frontend service; Truffle nodes for all statements/expressions;
descriptor-based value model; defer/panic/Goexit; intrinsic surface stubs sufficient for
language tests; differential corpus = language test suites + generated programs.

**Done-gates:** (1) `go test` language-facing suites (incl. generics) pass differentially vs gc
with zero unexplained divergences; (2) fuzz-soak layer of the testing strategy operational on
the language subset; (3) **interface freeze**: AST↔runtime intrinsic signatures and the
`-toolexec` argv contract versioned and documented — Phase 4's parallel start depends on this;
(4) peer reviews + self-attack (adversarial programs: pathological generics, deep recursion,
defer storms); (5) no placeholders (`grep` gate).

**Kill criteria:** systematic semantic divergence vs gc that the intrinsic-surface design cannot
close; interface freeze impossible without breaking the toil bound (→ architecture replan).

---

## 4. Phase 2 — Runtime: goroutines, channels, GC interop (4–8 PY)

**Goal:** faithful concurrency runtime on the user-space scheduler (option C,
spike 2b PASS — the 1:1-on-FJP design of the original plan was killed in
Phase 0; the M:N re-scope was not pursued).

**Scope:** goroutine = virtual thread + scheduler token; quantum-based
preemption via `Safepoint.poll()` (indefinite park + resume-token enqueue on
quantum expiry, 10ms default); platform-thread drainer (FIFO `runFifo` +
priority `wakeFifo`/`initFifo`); in-flight cap = `GOMAXPROCS` (hard assert);
all blocking through scheduler-aware primitives (channel/timer/sleep/select
park; the F5 deviation class — Java blocking that bypasses the scheduler —
documented and measured); channels + select; netpoller deletion → blocking NIO
on virtual threads; timers; GC interop (exact roots, write barriers free from
JVM); `GODEBUG` surface triage.

**Done-gates:** (1) `runtime`/`sync`/`context` stress suites pass differentially vs gc,
including a pinned `GOMAXPROCS` matrix and the preemption-torture suite re-run against the
2b design (the falsifying experiment for the scheduler — spike 2b F1–F6 must reproduce on
the production path, not just the prototype); (2) pinning audit: `jdk.VirtualThreadPinned`
shows zero unexpected pins on the conformance corpus — under option C a pinned carrier only
consumes dumb-pool capacity transiently (fairness is drainer-owned), but the audit stays as
the cgo-budget check; (3) JFR goroutine/channel events flowing; (4) peer reviews +
race-detector-equivalent stress (JVM race tools on the runtime, not the guest), including a
lost-wakeup model-checking pass (spike 2b F6 was stress-tested interleavings only);
(5) scheduling-observable divergence list closed or explicitly accepted with rationale.

**Kill criteria:** preemption-torture shows systematic divergence the user-space scheduler
cannot close; in-flight-cap economics make `GOMAXPROCS` fidelity impossible within the perf
budget (e.g., the F5 under-utilization class proves unfixable and observably violates the
cap semantics).

---

## 5. Phase 3 — Stdlib bring-up (6–12 PY)

**Goal:** the full stdlib (Linux-first) green against the intrinsic surface.

**Scope:** ~60–70 intrinsics; adapted stdlib fork limited to the ~78 coupled packages;
asm strategy per category (intrinsify / evaporate / emulate-then-intrinsify / JVM-covers);
linkname-diff tooling operational; build-tagged `syscall`/`os`/`net` for Linux; `reflect`'s
unsafe surface; versioned backend-specific exclusion list with reason codes.

**Done-gates:** (1) `go test ./...` (stdlib) passes with zero unexplained divergences vs gc —
the exclusion list is versioned, reason-coded, and review-capped; (2) all four testing-strategy
layers operational (smoke → conformance → stdlib+script suites → fuzz soak); (3) behavior
snapshots recorded for this Go release (the upgrade baseline); (4) peer reviews per package
group + self-attack (I/O fault injection, signal storms, file-descriptor exhaustion);
(5) per-release API/ABI diff tooling produces a scoped work list on a trial re-run.

**Kill criteria:** exclusion-list growth exceeds the review cap twice (long-tail divergence);
linkname churn defeats the diff tooling's scoping.

---

## 6. Phase 4 — Driver drop-in (3–6 PY; starts after Phase-1 interface freeze)

**Goal:** `alias go=graalgo` indistinguishable from stock `go`.

**Scope:** `-toolexec` backend swap in the stock distribution; `go env` parity; module/proxy/
sumdb behavior inherited; `go test` JSON protocol + two-level result caching; vet integration;
exit codes; `go version` string; the 929 txtar scripts as the conformance suite; cgo via Panama
(link/ABI boundary + JVM-aware `runtime/cgo`) — the riskiest integration item, staffed first.

**Done-gates:** (1) all 929 txtar scripts pass (or are on the versioned exclusion list with
reason codes); (2) `go test` caching behavior matches gc observably (hit/miss patterns, not
byte-identical cache entries); (3) cgo test corpus (stock `cmd/cgo` tests where applicable)
passes via Panama; (4) behavioral paper-cut list closed or accepted; (5) peer reviews +
self-attack (malformed modules, hostile proxies, cache corruption).

**Kill criteria:** argv-interception gaps force the driver fork AND the fork's merge burden
breaks the toil bound (→ replan with fork priced honestly); Panama layout-engine risk
materializes as systematic ABI divergence.

---

## 7. Phase 5 — Optimizations + three modes (3–6 PY)

**Goal:** the three execution modes with the hybrid as default, and measured (not claimed)
wins over gc+PGO.

**Scope:** (a) JIT-only on HotSpot; (b) AOT-only Native Image (reachability-metadata pipeline
for the full stdlib); (c) AOT+JIT hybrid (embedded Graal JIT in image). Falsifying benchmarks
per claimed optimization (vectorization, always-on profiling, partial escape analysis,
speculative interface devirtualization); the six hybrid experiments (image-size delta, warmup,
safepoint overhead, pinning audit, deopt torture, stack-depth parity).

**Candidate optimization backlog** (each needs its falsifying benchmark per done-gate 1
before it becomes a claimed win):
- **Closed-world whole-program optimization (owner directive 2026-10-06).**
  gc compiles packages separately and never sees the whole program; graalgo
  does. Exploit the closed-world assumption thoroughly: extreme LTO/WPO across
  package boundaries that the vanilla toolchain is structurally unable to do,
  **extreme devirtualization** (the headline win — speculative interface
  devirtualization via PICs → inlining of concrete method bodies → escape
  analysis promoting heap to stack across abstraction boundaries, per the
  FEASIBILITY.md §6.5a thesis), and every other whole-program transform the
  closed world enables. Hard requirement: **single-callsite callees are ALWAYS
  inlined** — with a complete call graph this is provably safe, so no
  code-bloat heuristics are needed where gc must guess. The same closed-world
  leverage extends to: whole-program dead-code elimination; specialization of
  hot generic instantiations (monomorphization where it pays); whole-program
  escape analysis (the devirtualization cascade's final step);
  devirtualization of interface calls with a single observed concrete type
  (PIC evidence from P3-M3's re-measurement feeds this). Verification is
  mechanical, not heuristic: a call-graph audit asserts that no non-inlined
  single-callsite callee survives the build. The falsifying benchmark must
  show the win vs gc+PGO on interface-heavy and abstraction-heavy workloads.
- **Profile-guided struct field reorder/pack for locality.** gc already sorts fields by
  alignment for size; it does no hot/cold splitting or PGO field layout. Validity: field
  order is observable only via `reflect` field order, serialization order (e.g.
  `encoding/json`), and `unsafe.Offsetof` — all of which go through abstractions this
  toolchain controls (its own `reflect`, its stdlib; `Offsetof` is frontend-evaluated
  against the true layout, so pure-Go pointer arithmetic stays self-consistent).
  Architectural fit: the descriptor-based value model (`GoStruct{descriptor, ...}`)
  makes layout a descriptor rewrite, not an ABI change — and the always-on profiler
  (scope item) should capture per-type field-access patterns as the input this
  optimization needs.
- **Speculative AoS→SoA for hot arrays.** Same validity shape, harder: the backing-store
  layout changes, so every access to the array must be rewritten consistently —
  discharged by JIT speculation (guard on layout-kind, deopt fallback) or by
  PGO-baked proof in AOT. Architectural fit: `GoSlice`'s backing store is already
  descriptor-mediated, so layout can be a per-allocation-site decision (profile picks
  SoA where a hot loop streams one field) rather than a type-global one as in gc.
  This is the layout enabler for the vectorization claim: SoA field arrays are
  contiguous same-type data, directly auto-vectorizable via Graal's Vector API
  support. Candidate benchmarks: particle systems, columnar scans — classic SoA wins.
- **Speculative type narrowing and bit-packing of storage.** Narrow element/field
  storage (int64→int32 on proven value range; 8 bools per byte) where the
  *declared* type stays intact: the descriptor mediates, so storage type may
  differ from declared type — operations keep declared overflow/rounding
  semantics, `reflect` still reports the declared type, `unsafe.Sizeof` is
  frontend-evaluated. Hard constraints, all checkable: addressability
  (`&slice[i]` on a packed bool needs a materialized cell — escape analysis or
  deopt); float64→float32 narrowing is essentially never automatic (rounding is
  observable in arithmetic results); int narrowing needs per-slot value-range
  proof (profiler-observed min/max + static range analysis, V8-Smi-style guard
  + deopt in the JIT). This composes with the two items above — reorder, narrow,
  pack, then lay out — so Phase 5 should treat them as one joint layout
  pipeline fed by a single profiler, not three independent passes.
- **Profiler input contract (all three items):** the always-on profiler must
  capture per-type field-access patterns, per-allocation-site array-access
  patterns, AND observed value ranges (min/max) per field/element. Specify this
  in the profiler design up front; it is expensive to retrofit.
- **Umbrella: speculative data representation, generalized.** The three items
  above are instances of one principle — *representation is not semantics* —
  and the descriptor-based value model is the general enabler: every Go value's
  storage already sits behind a descriptor, so representation changes are local.
  Further instances, all behind-the-scenes and semantics-preserving: map
  specialization (`map[int]T` with small dense keys → array-backed; tiny maps →
  linear-scan pair arrays; empty maps → singleton); `map[int]bool`-as-set with
  bounded range → bitmap; single-concrete-type interfaces → unboxed storage with
  guard (overlaps the devirtualization thesis); profile-guided slice growth
  factors. Production precedents exist — JVM compact strings (JEP 254) and V8's
  element-kind transitions are exactly this technique, just never applied to
  Go's types in gc. The discipline is the same as the devirtualization cascade:
  speculate on the observed representation, guard, deopt on violation — applied
  to data instead of dispatch. Costs to budget: representation dispatch on every
  operation (or per-representation specialization), flapping when a value's
  representation oscillates (the profiler must capture representation
  *stability*, not just the current one), and a larger testing matrix —
  Phase-5 differential corpora need representation-transition torture cases.
- **Small string optimization.** Short strings (up to ~15 bytes) stored inline in
  the string value instead of a heap allocation + pointer chase — the C++
  `std::string` / Swift-string classic, never done in gc. Sharp edges, all
  solvable: `reflect.StringHeader` and `unsafe.StringData` expose the data
  pointer, so observing an SSO string must lazily materialize a heap copy
  (same "materialize on observation" pattern as packed-bool addressability);
  `==` and map hashing must handle mixed inline/heap representations;
  substrings choose per-case between copying into SSO and viewing the parent.
  Architecture-specific honesty: in this value model a string is a `GoString`
  Java object + `byte[]` — SSO eliminates the `byte[]` allocation but the
  wrapper object remains unless escape analysis scalar-replaces it. So the
  headline zero-allocation win is *coupled to the partial-escape-analysis item
  already in scope*; without EA it still halves allocations and kills the
  pointer chase, which is where map-lookup-with-short-keys workloads win.
  Candidate benchmarks: JSON parsing, short-keyed map workloads, concatenation
  microbenchmarks.
- **Aggressive optimistic escape analysis, incl. across goroutine boundaries.**
  Extends the in-scope "partial escape analysis" item: go beyond static proof
  with profile-driven speculation (guard + heap-materialization deopt) and make
  *communication transparent* to the analysis. gc's EA gives up at every
  goroutine boundary — channel sends, closure captures, `go` statements always
  heap-allocate. The cases: (a) `ch <- x` where the receiver uses x transiently
  — copy stack-to-stack at the rendezvous instead of heap-allocating (the sync
  edge stays; only the allocation goes); (b) `go f(x)` / closures where x is
  not retained past the child's execution — allocate the capture in the
  *child's* stack rather than the heap; (c) interface-call arguments with
  profile-known concrete callees (cf. survey SURV-002 — design these together).
  Soundness rules: the child's use must not outlive the storage's frame
  (parent's stack is a valid home only with a proven join; otherwise copy to
  the child's stack or heap); size heuristics against stack overflow on
  virtual-thread stacks; reflect/unsafe escape = bail or guard, as usual.
  Candidate benchmarks: producer-consumer pipelines, errgroup fan-out, decode
  into transient structs.
- **Build on Truffle/Graal machinery — don't reimplement it (owner directive
  2026-10-07).** The speculation discipline running through this entire
  backlog (speculate on observed behavior → guard → deopt on violation) is
  Truffle's native idiom, and every candidate's guard/deopt story must be
  expressed in Truffle terms, not as a parallel mechanism: the Truffle DSL
  (`@Specialization` with guards, `@Cached` inline caches, `@TruffleBoundary`
  for slow paths), assumptions + `transferToInterpreterAndInvalidate` for
  deopt, OSR for hot loops, `CompilerDirectives` for interpreter/compiler
  signaling. At the Graal level, feed the compiler well-shaped graphs and let
  its existing machinery (partial evaluation, PEA, inlining, auto-vectorization
  via the Vector API) do the work — hand-rolling optimizations Graal already
  does is pure cost. This is also what makes the graalgo/x polyglot feature
  cheap: cross-language calls ride the standard Truffle polyglot API, and
  cross-language inlining falls out of partial evaluation. The bar for any
  Phase-5 design doc: name the Truffle/Graal mechanism it builds on, or
  justify why a new one is needed.
- **Demand-driven (lazy) evaluation of expensive, usually-unused
  computations.** Go is strict: arguments evaluate before the call, struct
  literals populate every field, error values format eagerly. When profiling
  shows a computation is expensive and usually unused (or used only on cold
  paths), defer it to first use with memoization. Two mechanisms: (a) static
  — sink pure computations to their first use (lazy code motion) or delete
  if provably dead; always safe, zero runtime cost; (b) dynamic — compile a
  lazy variant returning a thunk forced on first demand, guarded by the
  observed usage pattern, deopting back to eager if the pattern shifts. This
  is the dynamic counterpart to the ownership/usage item's static "don't
  populate unused returns": static analysis takes the provable cases,
  profile-driven laziness takes the rest. Needs an explicit cost model —
  expected win = P(unused) × computation cost − thunk machinery (allocation
  + indirection + memo check); static analysis cannot supply P(unused), so
  the dynamic form is inherently profile-driven. Sharp edges: Go's evaluation
  order is observable — skipping evaluation changes *panic timing*, so
  purity/panic-freedom must be proven or speculated with a guard; side
  effects (I/O, channel ops, global writes) disqualify unless provably
  unobservable; thunks capture their environment and can extend lifetimes —
  the classic lazy-language space leak — so force promptly and bound the
  capture. Candidate benchmarks: `fmt.Errorf` with expensive formatting where
  callers discard the error, logging/tracing arguments evaluated at disabled
  levels, struct literals with expensive fields read only on some paths.
- **Pull-based loop fusion (deforestation of eager intermediates).** Go code
  habitually materializes intermediate collections: loop 1 builds a slice,
  loop 2 consumes it — often through 3+ stages. Replace the eager
  intermediate with a pull-based pipeline (the consumer draws items one at a
  time from the producer) and fuse the chain into a single loop with no
  intermediate allocation. This is the loop/collection counterpart to the
  previous item's scalar laziness, and multi-stage chains — not just pairs —
  are the target. Static form: prove the intermediate is append-only in the
  producer, read sequentially in the consumer, and unobserved elsewhere →
  fuse outright. Speculative form: guard the clean shape (single consumer, no
  external reads) and fall back to materialization on violation. Precedents
  are the existence proofs: Rust's zero-cost iterator chains, Java Streams'
  lazy fused pipelines; gc does nothing of the sort. Sharp edges: side
  effects in the producer change *interleaving* when fused (eager = all
  produce then all consume; fused = interleaved) — needs purity or a
  non-interference proof; early `break` in the consumer means the fused
  version runs less of the producer (a win, but observable through effects);
  diamond shapes (one producer, two consumers) must not fuse into silent
  recomputation — detect sharing, buffer, or bail; panic timing (append OOM,
  index out of range) shifts under fusion. Candidate benchmarks: ETL
  pipelines, map/filter/reduce chains over slices, multi-pass record
  processing.
- **Profile-guided initial sizing: goroutine stacks, slice/map capacities.**
  Allocation sites are heterogeneous: the same `make([]T, 0)` grows to 3 at
  one PC and 30,000 at another — so size by per-PC profile, not by global
  default. Record per-allocation-site final/peak sizes; at allocation time,
  start goroutine stacks at the predicted peak (killing morestack
  copy-growths), give slices `cap` ≈ predicted final length (killing the ~2x
  growth realloc-and-copy chain), and hint map sizes (avoiding incremental
  rehashing). Size at a robust percentile — median, not max, so one outlier
  can't permanently over-allocate every call; the percentile is tunable
  against the waste-vs-growth tradeoff, and sizing backs off under GC memory
  pressure. This is a consumer of the always-on profiler (per-PC final-size
  histograms belong in its input contract). Convergence note: the survey
  independently found the goroutine-stack half as SURV-041 (golang/go#77893,
  fleet-measured 3.9% core-weighted copystack savings) — this item
  generalizes the same principle to slices and maps. Sharp edges: cold start
  (no profile → defaults); phase changes (undershoot is harmless, overshoot
  wastes — hence robust percentiles and pressure-aware backoff); pre-size
  only heap-escaping allocations; respect maxstacksize. Candidate
  benchmarks: append-heavy builders, ETL record assembly, map-heavy
  config/init loading, goroutine-per-request servers with deep handler
  stacks.
- **Map representation specialization: write-once → MPHF, tiny → linear
  array.** Two concrete instances of the speculative-representation
  umbrella, driven by observed use: (a) a map populated once and then only
  read is rebuilt as a minimal perfect hash function over its key set — one
  array probe per lookup, no buckets, no collision chains, compact and
  cache-friendly (gperf is the static precedent; the novel part is doing it
  *dynamically*: freeze on observed write-quiescence, guard, deopt back to a
  hash map if a write ever arrives); (b) tiny maps become plain arrays with
  linear scan — no hash computation at all, better locality,
  branch-predictable. The size threshold must be per key type, not global:
  string keys hash expensively, so their linear-scan threshold is higher
  than int keys'. Promotion paths: a tiny map growing past threshold morphs
  into a hash map; a write to a frozen MPH map deopts. Sharp edges: Go
  deliberately randomizes map iteration order — array/MPH iteration must
  start at a random offset to preserve the anti-dependence property; MPHF
  construction cost needs amortizing (freeze only hot maps, or build lazily
  after N lookups); start with concrete key types, interface keys later;
  deletes disqualify write-once. Candidate benchmarks: `map[string]string`
  option bags (endemic in Go APIs), `http.Header`, keyword/opcode tables,
  string interning tables.
- **Channel-pipeline serialization (goroutine → pull iterator).** A 1:1
  pipeline over an unbuffered channel (`go produce(ch)` + `for x := range ch`
  { consume(x) }) that in practice never overlaps is a coroutine wearing a
  goroutine costume: every transfer is a rendezvous, and the channel
  machinery (hchan lock, sudog allocation, park/unpark, scheduler crossings,
  cache-line bouncing) is pure overhead. Fuse it — inline the producer as
  the consumer's pull-iterator `next()`, eliminating the channel and the
  second goroutine entirely. Soundness is structural: private non-escaping
  channel, single producer / single consumer, no shared-state or external
  interaction between transfers (then all inter-transfer actions commute and
  any interleaving is equivalent to the sequential fusion), no
  `select`/`default`/timeout, no `len(ch)` observation, panic-free producer
  (a producer panic does not propagate to the consumer in the concurrent
  version — it just stalls it — so fusion must route panics explicitly or
  require panic-freedom, proven or speculated). *Profitability* is
  profile-driven, and this is the speculative core: fuse only when the stages
  are effectively serialized; genuine productive overlap (both stages
  saturating different cores) must keep the concurrent form — fusing that is
  a slowdown — so guard the lockstep observation and deopt when overlap
  appears. The scheduler is the natural profiler here: it sees every
  rendezvous and can directly observe "these two goroutines never
  productively overlap." This is the concurrent counterpart to pull-based
  loop fusion: loops, channels, and iterators all dissolve into demand-driven
  pipelines. Candidate benchmarks: generator→transform→sink pipelines,
  single-consumer ETL over channels, fan-out/fan-in with fan = 1.
- **Per-allocation-site struct specialization (field dropping / reorder /
  narrowing).** A struct type is used heterogeneously: site A sets all 10
  fields, site B only ever writes 2. Mint a per-site slim variant without the
  never-written fields. Soundness is almost free: never-written means the
  zero value is the only observable value, so reads of a dropped field
  materialize zero with no storage behind them; a write to a dropped field
  promotes the instance to the full layout via a guarded check (needed only
  where analysis can't prove slim instances never reach such a write).
  Extend the same per-site mechanism to field reordering by hotness and type
  narrowing by observed value range. This unifies three existing items under
  one mechanism: profile-guided field reorder (per-type) and type narrowing
  become per-site transforms, and the representation umbrella gets its
  flagship struct instance. In the descriptor-based value model this is
  natural — the slim variant is just another descriptor — and fewer pointer
  fields also means less GC scanning work. Sharp edges: address-taken fields
  can never be dropped (`&x.F` needs the storage); `reflect` reads of dropped
  fields must materialize zero and `reflect` writes must promote; `unsafe`
  offset arithmetic on the type disqualifies it entirely (same observability
  rule as the other items); promotion must be atomic w.r.t. the GC and other
  goroutines. Candidate benchmarks: AST/IR node types (each node kind uses a
  different field subset — the perfect case), config structs with optional
  fields, parse trees.
- **Constant-field folding into the representation (generalizes field
  dropping).** Never-written was the special case C = zero: if a field is
  *always written with the same constant* C at an allocation site, elide the
  storage and synthesize C on read — the read itself constant-folds away.
  Same per-site variant mechanism as the previous item; the transforms now
  compose in one variant (drop never-written → zero, fold constant → C,
  reorder the rest, narrow what's narrowable). Applies beyond structs: a map
  whose values are all the same constant becomes a key set (values
  synthesized on lookup/iteration — `map[string]bool` with all-`true` is the
  endemic case); uniform slices/values fold the same way. This is partial
  evaluation applied to the data representation. Sharp edges: "always C"
  must hold for every write reaching a specialized instance — a differing
  write promotes via the same guarded machinery (a write of C itself is a
  no-op, no promotion); address-taken fields can't be folded; `reflect`
  reads materialize C; `unsafe` on the type disqualifies; struct `==` must be
  descriptor-aware (dropped fields compare as C by construction); constants
  are compile-time-known values, primarily of value types. Candidate
  benchmarks: AST/IR nodes with per-site-constant kind tags, config structs,
  versioned protocol messages, set-maps.
- **Map-to-struct specialization for known key sets.** A `map[string]V`
  whose key set is small and known — HTTP headers, gRPC metadata, OTel
  baggage, `url.Values` — becomes a struct with one field per key: lookup
  turns into a direct field access with no hashing at all, and the full
  per-site struct machinery then applies (drop headers never set at this
  site, fold constant-valued ones, reorder by hotness). Precedents are
  direct: V8 hidden classes do exactly this for JS objects, CPython's
  key-sharing dicts share the key storage across instances with the same key
  set; gc does nothing of the sort for Go maps. Key set sources:
  compile-time-known (map literals, fixed key constants) and
  observed-stable (profiled, guarded). Dispatch from runtime key to field:
  constant keys resolve at compile time to a direct field access (zero
  dispatch — the big win for `h.Get("Content-Type")`); runtime keys scan the
  small known set or go through an MPHF (composes with the map
  specialization item). Unknown keys spill to an overflow side-map; a key
  set that won't stabilize deopts back to a hash map. Sharp edges: deleted
  known keys need a presence bitmap (`len`, `, ok`, and iteration read it);
  iteration starts at a random offset (randomized-order rule); the
  concurrent read+write fatal-error detector must be preserved exactly;
  `reflect` presents map semantics via the descriptor. Candidate benchmarks:
  HTTP header access (hot path of every request), gRPC metadata handling,
  baggage propagation, query-param parsing.
- **Transparent dynamic memoization, gated by measured profitability.**
  Automatically cache function results across calls — CSE stretched across
  time. Soundness needs purity, so the shippable scope is statically
  provable pure functions (no channel/global/shared-map effects, pure
  callees) via a shared purity analysis that also serves dead-arg
  elimination; dynamic purity speculation with dependency tracking and
  invalidation is research-grade and out of initial scope. Profitability
  cannot be modeled statically — it needs the arg-repeat rate — so measure
  it: profile call count, repeat rate, and mean cost, then trial
  single-entry memoization (last-call cache: O(1) storage, no eviction
  policy, catches the loop-repeated-arg pattern) as a runtime A/B against
  the unmemoized version, keeping it only on a measured win with margin.
  Sharp edges: arg hashing/comparison can cost more than the function itself
  — the trial must include lookup overhead; the cache retains args+results
  (the space-leak theme again) so it stays bounded; recursive functions need
  recursive calls routed through the memoized entry; per-P caches avoid
  contention (composes with the scheduler's per-P structures). Candidate
  benchmarks: repeated pure computations in hot loops (parsers, encoders,
  math kernels), DP-style recursion.
- **Algorithmic choice primitive (programmer-declared *and* compiler-supplied
  variants).** Let the programmer assert "these N code paths have equivalent
  observable effect" — and let the compiler add its own variants to the set:
  alternative codegen strategies, representation variants (hash-map vs
  array-backed vs MPHF, per the map items), and optimization-configuration
  variants — different optimizations enabled, different pass orders (an
  empirical attack on the phase-ordering problem), different
  unroll/tiling/interleaving factors, ISA extensions on/off, prefetch
  strategies. The configuration space is combinatorial, so the compiler's
  heuristics become a *proposal distribution* (top-k plausible configs) and
  measurement makes the final call — which is also where static heuristics
  fail most systematically (e.g. whether AVX512 pays for itself given
  frequency throttling is genuinely system-dependent, unknowable
  statically). The
  runtime selects among the union by measurement. This makes the choice
  mechanism the unified substrate for runtime version selection: it subsumes
  loop multi-versioning, and gives speculative optimizations a "measure which
  wins" alternative to "guard and deopt" — a better fit when the question is
  profitability rather than correctness. (The memoization item's trial-A/B is
  a 2-variant instance of the same machinery.) This inverts the usual division of labor:
  the human declares a search space the compiler could never derive (radix
  vs quick vs insertion sort; hash-table variants; two JSON parsers), the
  runtime searches it. Precedents: FFTW/ATLAS (runtime plan selection by
  measurement — decades proven) for the pattern; PetaBricks (MIT) for the
  language-primitive form; nothing like it ships in a mainstream systems
  language. The primitive is shippable *because* of this project's testing
  doctrine: the N paths are differentially tested against each other (fuzzed,
  in CI), backstopping the programmer's equivalence assertion —
  mis-specified equivalence is caught before production, not in it.
  Selection is per-choice-point greedy (joint global optimization is
  intractable), keyed on input features through a learned selector, with a
  designated default for cold start. The online measurement discipline
  underneath — interleaved A/B, statistical confidence, perpetual
  re-exploration — is shared infrastructure with the memoization trial and
  the profitability guards elsewhere in this list: one experiment runtime
  serving many items. Sharp edges: "equivalent effect" needs
  the precise contract — same values, same panics, same externally visible
  side effects; only performance characteristics may differ; dispatch
  overhead must stay a small fraction of path cost; path switching must
  coordinate with the JIT to avoid recompilation thrash; nested choices must
  compose. Candidate uses: `sort` algorithm families, stdlib
  data-structure variants without forking stdlib, codec/parser alternatives,
  mutex-vs-lock-free keyed on observed contention.
- **Interprocedural ownership & usage propagation (per-callsite call
  contracts).** Propagate two facts through the call graph: (1) *ownership* —
  does the callee retain the argument past return (store it, hand it to
  another goroutine), or merely borrow it? (2) *usage* — does the caller
  actually consume each return value, and does the callee actually read each
  argument on the taken path? Consumers: escape analysis across call
  boundaries (borrowed args don't escape through the call); dead-store
  elimination on returns (callee need not populate unused fields/values);
  dead argument evaluation (skip evaluating unused args — needs
  purity/panic-freedom proof or speculation). One function has many call
  sites with different contracts, so specialize per call site — and
  cost-model the negotiation itself: if the callee is usage-insensitive
  (behavior doesn't vary with the contract), installing flag checks is pure
  overhead, so prove sensitivity before installing the mechanism. Key
  architectural point: in the JIT, per-callsite compilation *is* the
  negotiation — the observed contract is baked into the compiled call site
  and no runtime bitmask is ever read in steady state; an explicit flag
  protocol is only the AOT/shared-code fallback. Composes with the two
  previous items: this is the mechanism that lets escape analysis cross call
  boundaries, and callee-takes-ownership kills the caller's reference at the
  call — feeding dead-slot reuse. Sharp edges: Go evaluates arguments
  eagerly, so skipping evaluation changes panic behavior unless
  purity/panic-freedom is proven or speculated with a guard; interface calls
  resolve the contract per concrete callee at the PIC (ties to SURV-002 /
  SURV-004). Candidate benchmarks: builder patterns (strings.Builder
  chains), `(value, ok)` / `(value, error)` with one side unused, append
  chains, map lookups discarding the value.
- **Dead-object slot reuse (allocation hoisting).** When liveness proves an
  object dead at a later allocation point with compatible size/layout, reuse
  the slot instead of allocating — the heap analogue of register coloring, and
  the complement to escape analysis (EA: non-escaping → stack; reuse:
  escaping-but-dead → recycled slot). The canonical win is per-iteration
  temporaries that escape just enough to defeat EA: reuse turns N
  medium-lived objects into one long-lived slot and, crucially, avoids
  premature tenuring — which is where it beats even a good generational GC
  (fresh nursery allocation is already cheap; tenuring is not). **Competitive
  note: gc is doing this head-on** — the `runtimefree` experiment
  (golang/go#74299, design at go.dev/design/74299-runtime-freegc,
  `GOEXPERIMENT=runtimefreegc`, disabled by default for Go 1.26): compiler
  alias analysis proving memory non-aliased, then automatic `runtime.free`
  calls (first application: freeing the old backing array after a growing
  `append`), aiming at less GC CPU and longer GC intervals. Survey verdict
  (wave 2, `track-4-wave2-deadobj-reuse.md`): **SPLIT** — the static
  free-early sub-case races gc head-on and is conceded (SURV-056); our open
  gap is allocator-bypass slot reuse + the speculative guard/deopt
  dimension, which gc's static alias analysis does not cover. Differentiators
  for this backlog item: (a) direct *slot reuse* (allocation hoisting, no
  round-trip through the allocator) is the stronger form; (b) the JIT
  dimension — profile-driven speculation with guard/deopt where static alias
  analysis bails; (c) their experiment is already producing soundness bugs
  from premature alias-analysis commitment (e.g. #80697 multi-assign
  misclassification, #79909) — our differential harness must include
  aliasing-torture corpora, and the benchmark bar for this item is gc *with*
  runtimefreegc enabled, not stock gc. Sharp edges:
  finalizers (bail, or clear+re-arm with care); pointer-identity observation
  (`%p`, `unsafe`) — same observability rule as the other items, no reuse when
  the address can leak; partial initialization needs a zero-the-delta step
  (fresh zeroed-pool allocation is free, reuse is not); different-type reuse
  needs full reinit or zeroed pointer fields or the collector mis-scans;
  deadness proof must be race-aware. Static proof is the practical path;
  speculation covers profile-dependent retention branches. Candidate
  benchmarks: per-record marshaling, `Sprintf`-in-a-loop, append-growth
  temporaries.
- **Adaptive concurrency primitives.** The runtime owns `sync.Mutex`, channels,
  and the scheduler — so it can specialize all three on observed behavior, the
  way HotSpot's lock inflation switches optimistic→pessimistic transparently:
  (a) adaptive mutexes that spin only when the holder is *currently admitted*
  (the drainer knows precisely; spinning on a parked holder is pure waste —
  block immediately), upgrading to queue-based blocking under contention;
  (b) unlock fast path that skips the drainer round-trip when no waiter exists,
  and unlock+relock fusion that hands off directly to a waiter instead of
  release-then-reacquire; (c) per-select-site specialization on the observed
  configuration — nil channels pruned (the `ch = nil` disable idiom makes
  nil-ness a first-class dynamic state, not a rare deopt), default-branch
  presence, buffer occupancy — compiled to a bespoke select state machine with
  deopt on configuration change; (d) idiom recognition: `chan struct{}` of
  cap-1 used as lock/semaphore (alternating send/recv, lock-like pattern)
  transparently becomes a real mutex/semaphore, no buffer or select machinery.
  Hard constraints: preserve Go's uniform-random selection among ready select
  cases (differential tests must check distributions, not just outcomes);
  channel/mutex ops are memory-model synchronization edges — elision is valid
  only for provably thread-local data (feeds from the escape-analysis item);
  idiom recognition needs guard+deopt for the occasional non-idiomatic use.
  Cross-phase note: several of these need holder-running and waiter-count
  signals from the drainer — those queries belong in the frozen Phase-2
  scheduler-glue surface (D7), specified now, implemented in Phase 2.
  Candidate benchmarks: lock-heavy servers, select-heavy multiplexers,
  semaphore-bounded pipelines.
- **Idle-time repack/defragment/shrink.** When the scheduler reports true
  idleness (run queues empty, workers parked — a first-class signal the
  option-C scheduler provides and stock Go's runtime does not expose cleanly),
  do deferred structural maintenance: rehash maps heavy with delete tombstones
  into compact tables; shrink `append`-grown slice backing stores whose
  capacity long exceeds their length (copy-on-shrink via the descriptor, which
  tracks views); repack speculative representations back down (e.g. hash-mode
  map that is small again → array-backed); purge/rebuild the intern table.
  Design rules, all load-bearing: maintenance runs as preemptible low-priority
  drainer work (the runnext lane preempts it — it must never inflate the
  drainer wakeup path, cf. the 38µs residual); chunked with yields, never
  stop-the-world; hysteresis on shrink (spare capacity unused for N epochs)
  against shrink→regrow flapping. This is *logical* defragmentation
  (tombstones, spare capacity, representations) — the JVM GC already owns
  physical heap compaction; keep the layers clear. Precedent: V8's idle-time
  GC scheduling via embedder idle notifications. The metric is p99 latency in
  the burst *after* idle and RSS across bursts, not throughput during idle —
  benchmark accordingly (bursty request-response servers).
- **Inferred store-once ("final") values.** Detect locations written exactly
  once (initialization) and never mutated — statically via constructor-pattern
  / single-store analysis on heap slots (locals are already SSA), or
  speculatively via a trap-on-second-store guard with deopt. The payoff is not
  fewer stores (there is only one) but *free loads*: hoist out of loops, keep
  in registers, fold as constants across calls/safepoints, and safe publication
  with no further synchronization. Exact production precedent: Java `final` —
  HotSpot constant-folds static finals and trusts final instance fields; Go
  cannot express it, so gc cannot use it, but this toolchain can *infer* it.
  Sharp edges: `reflect.Value.Set` and `unsafe` can write anywhere — the same
  guard/deopt pattern as the other items, or bail when the address escapes to
  reflect; and "once" must be race-aware (two goroutines storing "once" is a
  race, not store-once). Profiler contract grows again: per-slot store counts.
  Candidate benchmarks: read-mostly server config, build-once/traverse-many
  trees, lookup tables.
- **Transparent dynamic string interning.** Deduplicate equal strings at runtime
  so `==` becomes a pointer comparison on the fast path and memory is shared —
  decided *dynamically* per allocation site from observed duplication rates
  (sites with high dedup get interning; others skip the table lookup entirely),
  not as a global always-on tax. Precedent: V8 internalizes strings
  automatically (Java/Python `intern()` are manual-only). Semantically almost
  free — strings are immutable, so sharing is invisible — with one sharp edge:
  `reflect.StringHeader.Data` pointer *identity* across two equal strings would
  then compare true, where gc says false. Same resolution pattern as SSO:
  materialize a private copy on `unsafe`/`reflect` observation, or explicitly
  exclude backing-store identity from the differential contract. Composes with
  SSO: short strings go inline (no alloc), long repeated strings go interned
  (shared alloc); equality specializes per site over
  inline/heap-private/heap-interned. The named generalization is hash-consing
  of immutable values, but cost/benefit only closes for strings — scope the
  item there. Candidate benchmarks: config/JSON parsing, HTTP header handling,
  short-keyed map workloads.
- **Copy elision on provably-unmodified copies.** Elide a copy — struct/array
  value copies on assignment and call, `append`-growth temporaries, explicit
  deep-copy sites (user deep-copy helpers, runtime refresh copies like the
  loop-capture per-iteration refresh), marshalling temporaries — when the copy
  is provably never modified; sharing the reference is then observationally
  identical, and eliding a *deep* copy saves the whole traversal. Two proof
  sources, as usual: (a) static — the copy's write set is empty (no store
  through its address range; escape + modification analysis); (b) speculative —
  JIT profiles stores and guards the shared backing store with
  deopt-on-first-write (copy-on-write). Relationship to neighbors: generalizes
  the store-once item (that one is about the source; this is about the copy);
  composes with escape analysis (thread-local copies elide freely) and with
  SSO/interning (strings are the immutable special case already covered).
  Precedent: C++ copy elision (RVO/NRVO, language-guaranteed), Swift
  copy-on-write value types (the direct model — CoW with uniqueness check),
  Rust's moves-instead-of-copies by construction. Sharp edges: `reflect`/
  `unsafe` can write anywhere — bail or guard, same pattern as the store-once
  item; pointer-identity observation (`%p`, `unsafe.Pointer` comparison) makes
  elision visible if the address leaks — same observability rule as the other
  items; concurrent writes are races, so the proof must be race-aware; partial
  aliasing — the unmodified proof must cover exactly the shared range; guard
  placement must not regress the fast path (guard the store, not the load).
  Candidate benchmarks: append-growth temporaries, struct copies in hot loops,
  marshal/unmarshal round-trips, slice-passing-heavy pipelines.

**Done-gates:** (1) every claimed win beats gc **with PGO** on its named benchmark, or the
claim is struck from the docs; (2) hybrid meta-gate resolved: it beats *both* single modes on
≥1 workload class, or the mode is cut; (3) AOT reachability metadata derived by the tracing
agent with zero hand-authored entries for the stdlib (or the hand-authored tax is measured and
accepted); (4) JFR/heap-dump coherence verified per mode; (5) peer reviews + self-attack
(deopt storms, W^X platforms, CE/Oracle split).

**Kill criteria:** hybrid meta-benchmark fails (cut the mode, continue with (a)+(b)); no
optimization beats gc+PGO on any workload class (the "beyond gc" requirement fails — program
continues only if the remaining requirements justify it; explicit decision gate with Carlo).

---

## 8. Phase 6 — Upgrade pipeline completion (1–2 PY)

**Goal:** the agent-driven upgrade workflow as a runnable, documented system.

**Scope:** runbook tooling (rebase automation, codemods, apidiff/rtlink/godebug/flagdiff,
linkname-diff); weekly release-watch agent (prepares, never merges); upgrade report template;
first **real** Go release upgrade executed through the workflow as the gate.

**Done-gates:** (1) one full release upgrade performed by the workflow within the bound
(P95 ≤ 200 agent-hours, ≤ 10 wall-clock days); (2) upgrade report generated from test results
with zero unexplained divergences; (3) toil accounting per component published; (4) breach →
toil-reduction sprint process exercised at least once on a drill.

**Kill criteria:** the first real upgrade breaches the bound with no credible reduction path
(→ program continues only with Carlo's explicit re-scoping of requirement (2)).

---

## 9. Cross-cutting: testing strategy built early

| Phase | Testing-strategy deliverable |
|---|---|
| 0 | Differential-vs-gc **smoke harness** live in CI (mutation-validated oracles) |
| 1 | Conformance corpora + fuzz-soak layer on the language subset |
| 2 | Preemption-torture (user-space scheduler) + pinning-audit suites (JDK/Loom pinning events, cgo-budget check) |
| 3 | All four layers operational; **behavior snapshots** recorded; exclusion list versioned |
| 4 | txtar driver conformance; cgo corpus via Panama |
| 5 | Falsifying benchmarks as regression gates (perf, not just correctness) |
| 6 | Upgrade-runbook tooling; per-release API/ABI diffing as the scoping engine |

The strategy is built early because it is the development conformance net first and the
upgrade-toil reducer second. Per-commit: smoke + conformance. Per-release: all four layers
(~2–6h attended oracle compute `[ESTIMATE]`).

---

## 10. Estimates, staffing, critical path

| Phase | PY range | Parallelizable with |
|---|---|---|
| 0 — Spec + spikes | 0.5–1 | — |
| 1 — Language core | 3–6 | — (interface freeze is the handoff) |
| 2 — Runtime | 4–8 (see note) | 4 (after interface freeze) |
| 3 — Stdlib bring-up | 6–12 | 4 |
| 4 — Driver drop-in | 3–6 | 2, 3 (after interface freeze) |
| 5 — Optimizations + three modes | 3–6 | — (needs 2+3 substantially green) |
| 6 — Upgrade pipeline | 1–2 | 5 |
| **Total one-time** | **20–40** | |
| Steady-state tracking | 2–4 PY/yr forever | — |

Critical path 0→1→2→3→5 ≈ **4–5 calendar years at full staffing** (LOW confidence, ±50%).
Staffing notes: Phase 0 needs 2–3 senior (compiler + runtime + JVM); Phase 2–3 are the
staffing peak (runtime and stdlib-port grind parallelize by package group); Phase 4 needs
toolchain-internals depth (cmd/go, cgo, Panama). No agent-leverage discount applied
(`[DECIDED]` per SPEC §7.14 — discount dropped).

**Phase-2 re-estimate note (option C, spike 2b §6.3)** `[ESTIMATE]`: the M:N re-scope
counterfactual for Phase 2 was never estimated in this plan; building it up from the
original 4–8 PY scope — keep channels/select, netpoller deletion, timers, GC interop,
GODEBUG triage (~3–5.5 PY), replace vthread-mapping/yields/carrier-pool (~1.5–3 PY) with
frame heapification (~1.5–2.5 PY) + runqueue/runnext/sysmon-equivalent (~1–1.5 PY) +
stack growth management (~0.5–1 PY) + deopt-safe compiled-frame suspend/resume (~1–1.5 PY)
— gives **7–12 PY**. Option C is 30–40% below that (no frame heapification; 1:1 vthreads
kept; scheduler is ~600 lines of Java, prototype-validated): 0.60–0.70 × (7–12) ≈
**4–8 PY**, coinciding with the original range — the user-space scheduler replaces the
custom-carrier-pool work at similar cost. The table above keeps 4–8 PY on this basis.

---

## 11. Per-phase kill criteria (summary)

| Phase | Kill trigger |
|---|---|
| 0 | Any spike kill trigger without scoped workaround; toil dry-run breaches bound |
| 1 | Unclosable semantic divergence vs gc; interface freeze impossible within toil bound |
| 2 | Preemption-torture divergence the user-space scheduler cannot close; in-flight-cap economics make `GOMAXPROCS` fidelity impossible in budget |
| 3 | Exclusion-list growth exceeds review cap twice; linkname churn defeats diff scoping |
| 4 | Forced driver fork breaks toil bound; Panama ABI divergence systematic |
| 5 | Hybrid meta-benchmark fails → cut the mode; no win vs gc+PGO → explicit re-scope decision |
| 6 | First real upgrade breaches bound with no reduction path → re-scope requirement (2) |

Program-level: two consecutive releases breach the toil bound → toil-reduction sprint, then
re-scope if unfixable. The study's §8 lists the cheapest falsifying experiment per risk;
Phase 0 executes them.
