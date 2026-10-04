# T5: Truffle implementation architecture for graalgo

Research track T5 — how Go executes on GraalVM. Covers the frontend (go/ast → Truffle AST),
the value model, goroutines → virtual threads, channels/select/defer/panic, the
specialization + partial-evaluation strategy (adversarial), and a comparison of three
execution architectures with a corrected AOT+JIT hybrid deep-dive.

Method: GraalVM/Truffle docs and source (github.com/oracle/graal), Loom JEPs, and the
OpenJDK Leyden page are primary sources. Claims are labeled [PRIMARY], [SECONDARY-ONLY],
or [UNVERIFIED]. Negative verdicts are scoped to the tested configuration. No
computed-as-measured numbers: every figure is either quoted from a published source or
explicitly labeled an estimate.

Assumed inputs from sibling tracks: T2 reuses go/parser + go/types from the Go
distribution for the frontend; the runtime library (channels, maps, scheduler glue) is a
Java implementation of Go runtime semantics.

---

## 1. Frontend: go/ast → Truffle AST

1. Pipeline. `go/parser` produces go/ast; `go/types` produces full type info
   (types.Info with Types/Defs/Uses/Selections). A node factory walks the typed AST and
   builds Truffle nodes. go/types resolves everything static: method sets, interface
   satisfaction, constant values, escape-relevant facts. The Truffle AST therefore starts
   life fully typed — unlike JS/Ruby frontends, there is almost no "unknown" to discover
   at parse time. [PRIMARY: go/types is the standard typed-AST pipeline of the Go
   distribution; Truffle node construction from a typed AST is the standard recipe,
   e.g. SimpleLanguage.]
2. Node taxonomy. Two node families, following Truffle convention:
   1. Expression nodes with typed execute methods: `executeLong(VirtualFrame)` for Go
      `int`/`int64`/`uint64`, `executeInt` for 32-bit ints, `executeDouble` for
      `float64`, `executeBoolean` for `bool`, generic `execute(VirtualFrame)→Object`
      for heap values. Typed methods keep primitives unboxed in the interpreter and,
      after PE, in compiled code. [PRIMARY: oracle/graal
      truffle/docs/PartialEvaluation.md — PE-constant @Child fields let PE inline
      concrete execute methods.]
   2. Statement nodes returning void (`executeVoid`): assignments, if/for/switch,
      return, go/defer statements. Loops use `@ExplodeLoop`-friendly counted structures
      or Truffle's `LoopNode`/`RepeatingNode` for OSR. [SECONDARY-ONLY: OSR via
      RepeatingNode is the documented Truffle loop recipe; exact OSR behavior inside
      Native Image is [UNVERIFIED].]
3. Frames and locals. One `FrameDescriptor` per Go function; locals map to typed
   `FrameSlotKind` slots (Long/Int/Double/Boolean/Object). Closures capture via
   `MaterializedFrame` — the standard Truffle closure recipe. Function values are
   `{RootCallTarget, MaterializedFrame|null}` pairs.
4. Truffle DSL usage patterns, node by node:
   1. `@NodeChild("left") @NodeChild("right")` on binary ops; `@Specialization` per
      operand-type combination with `guards` for overflow-relevant cases (Go's signed
      overflow wraps — no deopt needed, unlike Java's exact math; unsigned ops are
      bit-identical on long).
   2. `@Cached` parameters build bounded polymorphic inline caches (PICs), e.g.
      `@Cached("recv.getItab()") Itab itab` with `limit="3"`; `@Fallback` goes generic.
   3. Profiles: `ValueProfile` for dynamic types (type switches, interface unwraps),
      `ConditionProfile` for branches, `BranchProfile` for rare paths (panic paths,
      map growth). Profiles feed PE speculation.
   4. `@TruffleBoundary` on runtime slow paths: map growth/rehash, channel
      slow paths, reflect, `println` to stderr, GC-pressure paths. Keeps compiled code
      small and PE-friendly. [PRIMARY: HostCompilation.md — PE boundaries are defined
      by @TruffleBoundary / transferToInterpreter / inInterpreter.]
   5. `CompilerDirectives.transferToInterpreterAndInvalidate()` on first-time
      specialization; `Assumption` objects for things that must never change within a
      run (there are almost none in Go — itabs are immutable — so assumptions are
      cheap and strong).
5. Go's "dynamic-ish" spots, and how little dynamism there really is:
   1. Interface method dispatch. Value = (dynamic type descriptor, data). Dispatch
      node: PIC on itab identity (`@Cached` + guard, limit ~3, megamorphic fallback
      doing a hash lookup). Because go/types sees the whole program, the candidate
      method set per call site is statically bounded — the PIC almost always stays
      monomorphic. This is the single most valuable specialization in the design.
   2. Map access. Key type is static; only the *table state* is dynamic. Fast path:
      inline probe guarded on the map's internal version/flags; slow path
      (growth, indirect keys) behind @TruffleBoundary. Honest accounting: this is a
      hand-written hash table either way — Truffle's win is inlining the fast path
      into the caller, not discovering types.
   3. Type switches / type assertions. Static case list → decision tree over dynamic
      type identity with a `ValueProfile`; each arm guarded and compiled to a direct
      check. No megamorphism in practice.
   4. `reflect` and `interface{}`-heavy code. Megamorphic by construction; route to
      generic nodes, no PIC budget wasted. Scoped verdict: specialization pays only
      where the static type set is small — which, for Go, is nearly everywhere
      except reflect.
6. Prior art (scoped): PDZaninov/GoLang-Compiler implemented a *subset* of Go on
   Truffle as a 2018 UCSC student project for Oracle
   (https://github.com/PDZaninov/GoLang-Compiler). Verdict scoped to that project:
   subset-only, unmaintained, no concurrency story — it proves the frontend mapping is
   feasible, not that it is complete. [PRIMARY: repo README.]

## 2. Value model: Go values on the Java heap

1. Primitive mapping (unboxed end-to-end): Go `int/int64/uint64/uintptr` → Java
   `long` (bit-identical); `int32/uint32` → `int`; `float64` → `double`;
   `float32` → `float`; `bool` → `boolean`; `complex128` → value holder (two
   doubles; a Java value class is the natural carrier [UNVERIFIED for PE
   scalarization — experiment needed]). `byte/rune` are aliases, no representation.
2. Heap values — one Java class per Go *kind*, instances carry a type descriptor:
   1. Structs: descriptor-based storage ("GoShape/GoObject"): per-type descriptor
      (field offsets/kinds, known at parse time) + instance = flat `long[]` for
      scalars + `Object[]` for references. Rationale: struct *types* can appear at
      runtime (hybrid mode loads new guest code as ASTs), and runtime class
      definition (`Lookup.defineClass`) is not available in a closed-world Native
      Image — so one-class-per-struct is rejected for the shared value model.
      [UNVERIFIED: whether SVM permits runtime class definition — assume no until
      proven; design must not depend on it.] PE sees through `@Cached` descriptor +
      identity guards; field access compiles to indexed array loads.
   2. Slices: `{Object dataArray, int offset, len, cap}` where dataArray is a
      *typed* Java array (`long[]`, `double[]`, `Object[]`, …) chosen by element
      kind. Bounds checks are ordinary Java array checks — Graal eliminates them
      where provable, exactly like gc's BCE.
   3. Maps: custom open-addressing table over Java arrays (control over growth
      policy and iteration order — Go specifies *randomized* iteration order, so the
      runtime must own the iterator, not `java.util.HashMap`).
   4. Strings: immutable `GoString{byte[]}`. (`java.lang.String` is UTF-16 and
      wrong for arbitrary Go bytes; TruffleString is the Truffle-blessed API and an
      [OPEN] experiment, not the v1 choice.)
   5. Interfaces: `GoInterface{dynamicType descriptor, Object data}` + itab for
      method dispatch. Empty interface = just the pair. Nil interface = null data
      with null descriptor — nil checks stay null checks.
   6. Function values/closures: `{RootCallTarget target, MaterializedFrame env}`.
      Direct calls to known functions bypass the closure object (static info from
      go/types).
3. Tradeoffs — Java objects vs off-heap/byte-arrays:
   1. Java objects win on: GC integration (exact roots, no manual write barriers —
      every field/array store goes through the JVM's barriered path), tooling
      (heap dumps, JFR allocation events), and PE/escape analysis (Graal reasons
      about Java object graphs natively).
   2. Off-heap (byte arrays / FFM arenas) wins on: C-interop layout identity and
      bulk zeroing. It loses on: manual root management, opaque to escape analysis,
      and every access needs explicit bounds/translation logic the JIT cannot see
      through as well.
   3. Recommendation: Java objects everywhere; off-heap only at the cgo boundary
      (argument marshalling), never as the primary representation. Scoped verdict:
      for a *managed* Go implementation, off-heap as primary storage is a net loss —
      it reimplements the GC's job worse.
4. GC roots, write barriers, "Go pointer" semantics:
   1. Roots: Truffle `VirtualFrame` slots and interpreter locals are ordinary Java
      locals/references — visible to the GC with zero extra bookkeeping. No
      explicit root registration anywhere in the design.
   2. Write barriers: none handwritten. All mutation is a Java field/array store;
      G1/Shenandoah/ZGC barriers are emitted by the JIT. (Contrast with gc, where
      the compiler emits write barriers explicitly — here that is the JVM's job.)
   3. "Go pointer" = opaque Java reference. The GC may move objects at any time;
      no code may observe addresses. Consequence: pointer *identity* (`==` on
      pointers) is reference identity — correct; pointer *arithmetic* is confined
      to `unsafe.Pointer` (see 5).
   4. Pinning: needed only when handing an address to native code (cgo, NFI).
      Use `PinnedObject` (Native Image API) / JNI critical regions, scoped to the
      call. Never pin for language-level operation.
5. `unsafe.Pointer` and cgo — the hard boundary:
   1. v1: `unsafe.Pointer` emulated via a handle table (object identity + field
      offset); arithmetic that escapes the object traps. This is a semantic
      restriction vs gc, documented, not silent.
   2. cgo calls go through the Truffle NFI (or JNI on HotSpot), with arguments
      marshalled to native layout at the boundary. cgo calls pin the carrier
      thread (see §3.4) — budget accordingly.
   3. [OPEN] experiments: (a) measure what fraction of the Go module corpus
      (stdlib + x/tools) actually requires true unsafe arithmetic; (b) prototype a
      raw-address mode for `unsafe` using FFM arenas, isolated from the main heap.

## 3. Goroutines → virtual threads

1. Mapping: `go f(args)` ≡ `Thread.ofVirtual().start(() -> f.call(args))`. One
   goroutine = one virtual thread. Goroutine-local state (defer chain, panic
   context) lives in virtual-thread-scoped holders, not `ThreadLocal` on carriers.
   [PRIMARY: JEP 444 — virtual threads are the JDK's cheap-thread primitive.]
2. Scheduler semantics — Go's M:N work-stealing vs Loom's carrier scheduling:
   1. Both are work-stealing at the bottom: Go multiplexes goroutines over M OS
      threads bound to P contexts; Loom mounts virtual threads on a carrier
      `ForkJoinPool` (default size = available processors), which is itself
      work-stealing. [PRIMARY: JEP 444 describes mount/unmount on carriers;
      SECONDARY-ONLY for "carrier pool is a ForkJoinPool" — widely documented,
      verify in implementation.]
   2. Semantic gaps: (i) no goroutine priority or affinity — matches Go (Go has
      none either); (ii) no `runtime.LockOSThread` equivalent on the fast path —
      bound threads must be real platform threads, a special allocation;
      (iii) Go's network poller integrates with the scheduler; ours must integrate
      Loom-aware I/O (NIO channels unmount on block — the JDK's I/O is already
      Loom-aware) with Go's netpoller API surface. The netpoller *integration*
      (not the mechanism) is [UNVERIFIED] and owned by the runtime track.
3. Preemption — the sharpest semantic difference:
   1. Go has *async* preemption (signals at safe points, ~10ms granularity) for
      scheduler fairness. Java virtual threads are *never* preempted: a
      compute-bound goroutine holds its carrier until it blocks or yields.
   2. Consequence, scoped: with N carriers, N simultaneously-spinning goroutines
      starve every other goroutine (latency collapse, not incorrectness). Go has
      the same pathology at GOMAXPROCS spinners, but Go's async preemption bounds
      the unfairness window; Loom does not.
   3. Mitigation (required, not optional): compiler-inserted safepoint polls —
      `Thread.yield()` at loop back-edges and function entries on the
      interpreted/JIT'd path, mirroring gc's own morestack/preemption checks. This
      is semantically safe (Go's memory model promises no preemption timing) and
      converts the pathology into Go-equivalent behavior. Cost: one branch per
      back-edge; Graal folds it where provable. [UNVERIFIED: measured overhead —
      falsifying experiment §8.3.5.]
4. Carrier pinning:
   1. [PRIMARY: JEP 491, delivered in JDK 24] — virtual threads now
      acquire/hold/release monitors independently of carriers: `synchronized`
      blocking unmounts. The old JDK 21–23 pinning hazard for monitors is gone on
      current JDKs; graalgo targets JDK 24+, so channel/mutex internals may use
      `synchronized` without pinning risk.
   2. What still pins (JEP 491, primary): native methods and FFM calls — a
      virtual thread that blocks inside native code pins its carrier. cgo is
      therefore the pinning source. Policy: run cgo on a separate platform-thread
      pool (never on carriers), or budget `+k` carriers. `jdk.VirtualThreadPinned`
      JFR events audit this in production.
5. Stack growth: virtual-thread stacks are heap-allocated stack chunks that grow
   and shrink with execution (JEP 444, primary), starting small (~1KB scale per
   secondary reports). This *is* Go's growable-stack semantics, provided by the
   JVM — no segmented-stack machinery, no stack-copying write barriers in the
   language implementation. Limit, noted honestly: maximum depth is bounded by the
   platform-thread stack size (`-Xss`), whereas gc grows to 1GB default max.
   Pathological deep recursion that gc survives may `StackOverflowError` here.
   [SECONDARY-ONLY on the exact bound mechanics; falsifying experiment §8.3.5.]
6. GOMAXPROCS: default carriers = available processors ≡ Go's default
   GOMAXPROCS = NumCPU — same default, no work. `runtime.GOMAXPROCS(n)` →
   create new goroutines with a custom scheduler, `Thread.ofVirtual()`
   `.scheduler(new ForkJoinPool(n))` [SECONDARY-ONLY on the exact builder API —
   verify against JDK javadoc in implementation]. Threads already running keep the
   old scheduler (documented deviation; Go applies it process-wide — [OPEN]
   whether to migrate or document).

## 4. Channels, select, defer/panic on Truffle

1. Channels. `GoChannel` = Java object: `ReentrantLock` (or `synchronized`,
   post-JEP-491), circular buffer over a typed array, two wait-queues of parked
   virtual threads (`LockSupport.park/unpark`, Go's `sudog` ≈ `Parker{thread,
   elem}`). Fast paths (non-blocking buffered send/recv with room) inline in the
   interpreter; slow paths (park, close-broadcast, buffer growth — buffers don't
   grow in Go, so: park/wakeup only) behind `@TruffleBoundary`. Close semantics
   (send-on-closed panics, recv-on-closed drains-then-zero) implemented once in
   the runtime class, shared by all modes.
2. Select. Mirrors `runtime/select.go`: lock all involved channels in address
   order (deadlock avoidance), poll readiness, else build one `Parker`, enqueue
   it on every channel, unlock all, `park`. On wakeup, dequeue from all others
   and complete the chosen case. Spurious-wakeup loop mandatory. Pseudo-random
   among ready cases (`fastrand`). `select` with a `default` never parks.
   Implementation risk is concentrated and well-understood — this is a port of
   known-correct logic, not new design.
3. Defer. Explicit frame state, not interpreter magic: each goroutine holds a
   defer chain (linked list of `{CallTarget, args, frameSnapshot}`); a
   `DeferPushNode` conses, the function-epilogue node runs the chain LIFO on
   normal return *and* on panic unwind. Open-coded defers (gc's optimization) are
   a later phase, not v1. Defer-in-loop grows the chain — matches Go semantics
   exactly, including the memory cost.
4. Panic/recover — Java exception vs control-flow node:
   1. Recommended: `GoPanicException extends AbstractTruffleException` carrying
      the panic value. `panic(v)` throws; each Go function has a boundary node
      that catches it, runs the defer chain (a deferred `recover()` stops the
      unwind and yields the value), else rethrows. `AbstractTruffleException`
      gives lazy stack-trace capture — traces materialize only if the panic is
      fatal or inspected, so *recovered* panics stay cheap.
   2. Rejected alternative, scoped: `ControlFlowException` (never captures a
      trace) would be faster for recovered panics but Go *prints a stack trace on
      unrecovered panic* — the trace would have to be reconstructed manually on
      the fatal path anyway, duplicating what AbstractTruffleException already
      does. Verdict: use AbstractTruffleException; revisit only with measured
      evidence that recovered-panic throughput is a bottleneck.
   3. `runtime.Goexit()`: `GoExitException extends ControlFlowException` — runs
      defers, terminates the goroutine, no trace needed. Clean fit.
   4. `panic(nil)` → `*PanicNilError` (Go ≥1.21 semantics) — replicate exactly;
      add a conformance test.

## 5. Specialization + partial-evaluation strategy (adversarial)

1. Where Truffle PE genuinely wins for Go:
   1. Interface method dispatch: PIC on itab identity + speculative
      devirtualization and inlining of the concrete method body. gc *cannot*
      devirtualize interface calls speculatively — this is a real, Go-specific
      PE win.
   2. Cross-function inlining through CallTargets with splitting: Go programs
      are call-heavy with small functions; Truffle inlines across the interpreter
      boundary with per-call-site specialization (splitting), beyond gc's
      inlining budget in hot polymorphic cases.
   3. Partial escape analysis on value-model allocations: slice headers,
      interface values, defer records — Graal's PEA removes allocations gc's
      escape analysis keeps. Genuinely better than gc here.
   4. Type-switch decision trees and map fast-path inlining into caller context
      (see §1.5).
2. Where Go's static typing already gives gc most of the benefit — the adversarial
   ledger:
   1. Type discovery: Truffle's headline miracle (JS/Ruby: "we found the types!")
      is nearly vacuous for Go — go/types already knows every type. Do not sell
      PE as type discovery; it is inlining + speculation + escape analysis.
   2. Monomorphic direct calls: Go statically dispatches every concrete call;
      there is no vtable to devirtualize except interfaces. gc already compiles
      these optimally.
   3. Arithmetic/loop optimization: gc does BCE, LICM, strength reduction on
      statically-typed code. Graal is a *better* optimizer (bigger budget, better
      vectorizer), but the delta is optimizer quality, not a phase change.
   4. Honest summary: expected margin over gc comes from (i) speculative
      interface devirtualization, (ii) superior escape analysis, (iii) larger
      inlining/vectorization budget — real but narrower than the dynamic-language
      headlines. Any claim beyond "gc-class baseline with headroom above" needs
      measured evidence, not PE enthusiasm.
3. Strategy that follows:
   1. Spend DSL specialization budget *only* at genuinely dynamic sites:
      interface dispatch, type switches/asserts, closure calls, maps with
      interface keys, reflect. Everything else: typed execute methods, no PICs.
   2. `@TruffleBoundary` around all runtime slow paths (map growth, channel
      park, reflect, panics) — keeps compiled units small and PE time bounded.
   3. Itabs are immutable → speculation on them never invalidates; no Assumption
      lifecycle management needed. (A rare structural advantage — use it.)
   4. Splitting enabled for hot polymorphic call sites; polymorphic-degree caps
      tuned by measurement, not by analogy with JS workloads.

## 6. Three execution architectures (corrected framing)

The user's corrected hybrid: **the application code AND the JIT compiler itself
are AOT-compiled into the binary; at runtime that AOT-resident JIT further
optimizes hot code.** This is not HotSpot-style tiering — it is the shipping
GraalVM capability "Truffle languages on SubstrateVM with runtime compilation"
(see §7). The three modes:

1. (a) JIT-only. Truffle interpreter on a GraalVM JDK (HotSpot); guest ASTs
   partially evaluated and compiled by Graal via JVMCI at runtime. The classic
   Truffle deployment. Fastest to build, richest tooling, slowest startup, full
   JVM warmup tax.
2. (b) AOT-only. Pure Native Image, closed world, fallback runtime
   (`-Dtruffle.TruffleRuntime=...DefaultTruffleRuntime` — interpreter only, no
   runtime compilation). Guest code runs interpreted forever. Smallest binary,
   fastest startup, lowest peak performance, works on W^X platforms.
3. (c) AOT+JIT hybrid. App code + interpreter + the PE + Graal compiler (IR,
   backends) all AOT-compiled into one native binary; the embedded optimizing
   runtime compiles hot *guest* ASTs at runtime, with deoptimization supported
   inside the image. Startup ≈ AOT; peak ≈ JIT-only; warmup shorter than (a)
   because the compiler itself needs no warmup.

## 7. AOT+JIT hybrid deep-dive

1. How the JIT-in-image actually works [PRIMARY: oracle/graal
   truffle/docs/HostCompilation.md; GraalVM embed-languages docs]:
   1. *Host compilation* = compiling the interpreter's own Java code. On HotSpot
      this is JIT; in Native Image it happens once, at image build time — the
      interpreter, the partial evaluator, the Graal IR and backends, and the
      Truffle runtime (compilation queue, installer, deoptimizer) are all
      AOT-compiled into the binary.
   2. *Guest compilation* = partial evaluation of Truffle ASTs. At runtime, hot
      ASTs are enqueued exactly as on HotSpot: PE produces a Graal graph from the
      (PE-constant) AST, Graal optimizes it, and the result is installed into the
      SubstrateVM code cache. The AST then redirects to the installed code.
   3. Triggering is unchanged from HotSpot: invocation/loop counters on
      `OptimizedCallTarget`s cross thresholds → background compilation threads
      (AOT-resident) compile → install → on-stack or entry-point transfer.
   4. Deoptimization works in-image: `com.oracle.svm.core.deopt` exists precisely
      for runtime-compiled methods, and `--vm.XX:+TraceDeoptimizationDetails` is
      documented as native-image-only. Speculation failures invalidate installed
      code and resume in the AST interpreter — the same contract as HotSpot.
   5. What is AOT-compiled in: the Truffle DSL-generated node classes, the PE,
      Graal's graph IR + optimization phases + machine-code backends, the Truffle
      runtime, and the application's reachable Java code (interpreter + Go
      runtime library). What is *not*: guest Go code — it exists only as ASTs
      until the embedded JIT compiles it.
   6. Shipping precedent: GraalPy's native-image docs describe disabling runtime
      compilation as an *option* (i.e., it is on by default in their native
      builds) [PRIMARY: oracle/graalpython docs/user/Native-Images-with-Python.md];
      a 2026 third-party language (Protos) ships a native executable "retaining
      the Truffle optimizing runtime" that "can compile guest code at runtime"
      [SECONDARY-ONLY: protos 2026-09-26 news post]. The GraalVM team documented
      the same pattern as "Mixing AOT and JIT" [SECONDARY-ONLY: graalvm blog].
2. Cost model:
   1. Image size: GraalPy reports disabling runtime compilation shrinks their
      binary by ~40% [PRIMARY: oracle/graalpython docs]. Derived estimate (not
      measured by us): embedding the compiler + optimizing runtime costs roughly
      1.6–1.7× the interpreter-only image. Our Go interpreter is smaller than
      GraalPy's, so the *absolute* delta should be smaller; the ratio is a bound,
      not a prediction.
   2. Build time: image builds already run minutes-scale; including the compiler
      adds reachable code to the closed-world analysis. Magnitude [UNVERIFIED] —
      measure on the prototype (§8.3.1).
   3. Runtime memory: compiler threads + SVM code cache + PE metadata, all in the
      image's heap. Plus one hard requirement: runtime code installation needs
      writable-and-executable pages — runtime compilation in SVM is opt-in
      precisely for this reason [SECONDARY-ONLY: iOS feasibility audit noting
      "Truffle runtime compilation (opt-in, off by default)"].
3. What the embedded JIT can do that pure AOT cannot — and what it cannot do that
   HotSpot's JIT can:
   1. Can: speculation + deoptimization (interface PICs, type profiles that only
      exist at runtime), profile-guided inlining of guest call graphs, *late
      binding of new guest code* — `go run`, REPLs, and program-loaded Go source
      compile new ASTs at runtime with full optimization. Pure AOT cannot do any
      of this; it is limited to what the closed-world analysis saw.
   2. Cannot (vs HotSpot): define new *Java* classes at runtime for new guest
      types (closed world — new Go code arrives as ASTs over the fixed value
      model, never as new classes); use JVMCI plugins or agents loaded
      dynamically; rely on class-redefinition. Scoped verdict: the restriction
      bites only designs that smuggle guest types into Java classes — §2.2.1
      already rejected that design, so the restriction is aligned with the value
      model.
   3. Cannot (vs both): run on W^X-locked platforms (iOS without JIT
      entitlement) — no executable pages, no runtime compilation, period. That is
      what mode (b) is for.
4. Startup/warmup story (architectural reasoning, not measured):
   1. vs pure AOT (b): startup is equivalent (no JVM, pre-initialized heap); the
      hybrid additionally pays guest-code warmup (interpret → compile) that (b)
      pays forever as interpretation. For any run longer than seconds, hybrid
      wins; for single-shot scripts, (b) may win on total time — workload-
      dependent, measure.
   2. vs JIT-only (a): the hybrid removes JVM startup, class loading, *and* the
      JIT compiler's own warmup — the compiler is already native code
      ("libgraal runs fast from the start" is the documented argument for the
      AOT-compiled compiler on HotSpot [PRIMARY: graalvm compiler docs,
      libgraal vs jargraal]). Guest code still warms up, but the warmup tax is
      strictly smaller than (a)'s. Expected ordering: startup (c)≈(b)<(a);
      time-to-peak (c)<(a); peak (c)≈(a)>(b).
5. Debugging/profiling coherence [PRIMARY: graalvm.org Native Image
   debugging-and-diagnostics docs]:
   1. Native Image supports JFR (`--enable-monitoring=jfr`), heap dumps
      (`--enable-monitoring=heapdump`, `-XX:+HeapDumpOnOutOfMemoryError`), jcmd
      (`JFR.start/stop/dump`), JDWP, and remote JMX/VisualVM/JMC — the standard
      JVM observability story the program requires ("deep modern-JVM
      integration: JFR/VisualVM").
   2. Runtime-compiled guest methods appear in stacks and JFR; deoptimization is
      traceable in-image. Guest-language source-level debugging goes through
      Truffle instrumentation (DAP/chromeinspector tools can be built into the
      image).
   3. Coherence work (ours, not free): profilers will see AOT-compiled host
      frames (interpreter, runtime) *and* runtime-compiled guest frames (Go
      functions) interleaved. Mapping guest frames back to Go source across
      tiers needs Truffle stack-trace support wired to go/ast positions — a
      frontend instrumentation task, listed as implementation work, not a
      blocker.
6. Version-tracking impact: the JIT-in-image is **Go-version-independent**. It is
   a platform component (Truffle + Graal), not a per-Go-release artifact.
   Per-Go-release work stays where it belongs: the frontend (go/ast → nodes) and
   the Go runtime library. Compared with a hand-rolled AOT backend, the hybrid
   *reduces* per-release backend maintenance: optimization policy lives in the
   embedded compiler, which we do not rewrite per Go release. This directly
   serves the "always tracks the most recent Go release" requirement.
7. What breaks in hybrid mode (closed-world vs runtime dynamism):
   1. Closed-world build assumptions vs runtime code: reflection configs,
      resource configs, and JNI/NFI bindings must be complete at build time for
      the *host* (interpreter + runtime). Guest Go code is unaffected (ASTs, not
      classes) — but any host service a guest program can reach (new Java
      interop targets, new native libraries via cgo) must be pre-registered or
      it fails at runtime. `plugin.Open` semantics need redesign or explicit
      non-support [OPEN].
   2. Speculation invalidation is supported (deopt in-image), but each
      deopt/recompile cycle costs more in an image (no tiered C1 to fall back
      to — interpreter is the only fallback). Pathological
      megamorphic-flapping workloads could oscillate; mitigate with
      recompile backoff (standard Truffle policy).
   3. Build-time cost: the image build compiles the compiler — slowest build of
      the three modes, most build RAM. Developer inner loop should use mode (a),
      not (c).
   4. Debugging across tiers (§7.5.3) and the W+X requirement (§7.2.3) are the
      two hard constraints; everything else is engineering.

## 8. Three-mode matrix and recommendation

1. Mode × concerns matrix (qualitative; ordering claims are architectural
   reasoning unless cited):
   1. Startup: (b) fastest ≈ (c) fast (no JVM; pre-initialized heap) < (a) JVM
      startup + class loading.
   2. Peak performance: (a) ≈ (c) (same Graal optimizer, same speculation) >
      (b) (interpreter ceiling).
   3. Build cost: (a) cheapest (javac) < (b) < (c) most expensive (closed-world
      analysis over interpreter + compiler + app).
   4. Debuggability: (a) best (full HotSpot tooling, fastest rebuild) > (c)
      (Native Image JFR/heapdump/JDWP/jcmd all supported [PRIMARY], but slower
      rebuilds and tier-crossing stacks) > (b) (same tooling as (c), less
      tier complexity, but nothing to profile past the interpreter).
   5. Implementation effort: (a) baseline; (c) = (a) + image-build
      configuration (reflection/resources for host, W+X opt-in, tier-coherent
      stack traces); (b) = (a) minus the optimizing runtime (a build flag).
      One frontend, one value model, one runtime library serve all three —
      the modes differ only in packaging and whether the optimizing runtime is
      embedded.
2. Recommendation:
   1. **Default: (c) AOT+JIT hybrid.** Reasons: (i) it is the user's corrected
      design, and it is a real shipping mechanism, not a proposal; (ii) Go's
      distribution story is a single fast-starting static binary — the hybrid
      preserves that while recovering JIT-class peak performance; (iii) `go run`,
      tests, and REPL-style flows need runtime codegen anyway — pure AOT would
      leave the most common developer commands on the interpreter; (iv) the
      JIT-in-image is Go-version-independent, minimizing per-release maintenance;
      (v) observability (JFR/heapdump/VisualVM) is fully supported in-image.
   2. **Dev inner loop: (a) JIT-only.** Fastest builds, best debugger, same
      language semantics. Every developer-facing iteration uses (a); (c) is a
      release artifact.
   3. **Fallback: (b) AOT-only.** For W^X platforms (iOS-class lockdown), extreme
      size constraints, or single-shot scripts where warmup never pays off.
      Selected by a build flag, not a fork.
   4. Frontend/runtime sharing: **one IR** — the Truffle AST built from go/ast —
      feeds all three modes. No separate bytecode backend. The modes share
      everything except packaging; this is the central complexity-control
      decision of the whole track.
3. Falsifying experiments (each must be able to kill the recommendation):
   1. Image-size delta: build a Go-subset Truffle interpreter as a native image
      with and without the optimizing runtime; measure the delta against the
      ~40% GraalPy bound. Kills (c)-as-default if the delta makes the binary
      undistributable.
   2. Warmup: interface-heavy + channel-heavy Go benchmark; measure time-to-peak
      for (a) vs (c). Kills the "compiler needs no warmup" advantage claim if
      (c) ≈ (a).
   3. Safepoint overhead: tight-loop microbenchmark with back-edge yields on/off
      under (a). Kills the mitigation in §3.3 if overhead is prohibitive or
      unfairness persists.
   4. Pinning audit: cgo-heavy workload under (c) with JFR
      `jdk.VirtualThreadPinned` events. Kills the carrier-budget model if
      pinning escapes the cgo pool.
   5. Deopt torture: megamorphic interface dispatch flip-flopping inside an
      image; results must be bit-identical to the interpreter. Kills (c)
      correctness if any divergence.
   6. Stack depth: deep-recursion parity vs gc (expect StackOverflowError earlier;
      quantify the gap). Kills silent-compat claims if the gap breaks real
      programs (e.g., deeply recursive stdlib paths).

## T5 key takeaways

1. One Truffle AST frontend (from go/ast+go/types) feeds all three execution modes — no separate bytecode backend; modes differ only in packaging.
2. Go values live as Java objects (descriptor-based structs, typed-array slices, (itab,data) interfaces); "Go pointer" is an opaque reference — the GC moves objects freely, pinning only at the cgo/FFI boundary.
3. Goroutine = virtual thread is semantically sound (heap-growable stacks per JEP 444; synchronized no longer pins per JEP 491), but Loom never preempts — back-edge safepoint yields are mandatory, and cgo calls must leave the carrier pool.
4. Be adversarial about PE: Go's static typing means Truffle's win is speculative interface devirtualization + escape analysis + inlining budget, not type discovery — expect gc-class baseline with headroom, not a miracle.
5. **Recommend (c) AOT+JIT hybrid as default** (app + interpreter + embedded Graal JIT in one native binary — the shipping Truffle-on-SubstrateVM mechanism), (a) JIT-only for the dev loop, (b) AOT-only for W^X/size-constrained targets; six falsifying experiments in §8.3 decide whether the recommendation survives contact with measurement.
