# T7 — JVM Ecosystem Integration Surface

Research track for **graalgo** (GraalVM/Truffle-based drop-in Go toolchain).
Status of everything below: research findings, not decisions. Illustrative APIs are labeled as such.
Primary sources are listed at the end; claims I could not verify are labeled **[UNVERIFIED]**.
Negative verdicts are scoped to the tested/documented construction only.

## 0. Method note

1. Loom/Panama/JFR status was checked against OpenJDK JEP pages and the JDK bug tracker (primary), plus GraalVM reference docs (primary).
2. `io_uring`-in-the-JDK status was searched as of October 2026; where no primary source exists the verdict is labeled **[UNVERIFIED]** rather than asserted.
3. The momo SPEC was read for isolation requirements (§§4–8, 10–12, 5.6). Note: momo's fork-vs-toolexec decision is still pending (2026-10-03 review), which makes the isolate-based alternative in §5 *more* relevant, not less — it is a path that needs no runtime fork at all.

## 1. Momo isolation requirements relevant to graalgo (summary)

Source: `~/workspace/momo/SPEC.md` (Phase 0 draft; §§4–7, 10–12 prescriptive parts are `[PROPOSAL]`, not decided).

1. **Module model:** a module is an annotated package tree (`//momo:module`) with exactly one service struct + constructor; its import closure excludes other modules' packages; cross-module APIs are module-free facade interfaces (MM-0001).
2. **Generations:** each instantiation creates a *generation* — fresh globals block, re-executed package inits, fresh service instance. Supervisor goroutines are explicitly tagged generation-none.
3. **Crash containment:** on contained panic the generation is marked dead; its goroutines are killed with an *unkillable* sentinel panic (async preemption at lock-depth zero; blocked goroutines woken); generation-owned fds are closed via pollDesc tagging; teardown kill-then-joins on a pin refcount with a bounded budget. Stuck-syscall goroutines are a tracked residual leak, never silent.
4. **Transparent restart:** a new generation is atomically swapped in; shims resolve the current generation per call. Dual-window parking (N-in-T short, M-in-24h long) yields `ErrModuleDown` + backoff; a crash-loop cycle detector is the backstop.
5. **Value-only boundaries:** deep-copy codegen at every crossing (memo-passing; pure, deterministic `momo.Copier` escape hatch); finalizers do not transfer (rejected loudly); per-copy size/depth budgets; no cross-generation references (MM-0010).
6. **Failure semantics:** in-flight calls are killed on callee death (no drain); idempotent calls retry once (at-least-once); errorless facades get an *honest cascade* (caller's boundary contains the panic, caller tears down too); cascade teardowns do not increment the caller's own crash counter.
7. **State model:** crash-only (Erlang-style) — restart rebuilds from the constructor; snapshot/restore was considered and rejected.
8. **Resources:** per-module CPU/mem quotas; memory overrun → generation panic → restart (self-healing); CPU overrun → throttling.
9. **Observability:** per-generation attribution (`g.gen`) in traces/profiles; crash diagnostics on a privileged channel; sanitized `CrashError` across boundaries.
10. **Non-guarantees:** external side effects are neither rolled back nor deduped; `unsafe` misuse and in-module deadlocks are out of scope.
11. **cgo stance (MM-0017):** `import "C"` banned process-wide by verifier rule, plus bans on cross-package linkname, raw-syscall assembly, dangerous `syscall` families, and `plugin`. Rationale: C code defeats generation-relative addressing and generation tagging. **Any graalgo C-interop path must be subject to the same verifier gate** — the Panama mapping in §3 must be refusable in module code.

## 2. Polyglot API design: Go <-> Java

### 2.1. Substrate

The natural substrate is the Truffle polyglot API (`org.graalvm.polyglot`: `Context`, `Value`, `HostAccess`, `ProxyExecutable`, `ProxyObject`, `ProxyArray`). Graalgo-as-a-Truffle-language gets this for free in JVM mode; the Go side exposes values through the interop protocol and the Java side consumes them via `Value`. Host-access policy should default to `HostAccess.SCOPED`-like scoping (callback parameter validity limited to the callback) rather than `ALL`, which the docs "highly discourage" for untrusted guests (graalvm.org HostAccess javadoc).

### 2.2. Type mappings (proposed)

1. `string` <-> `java.lang.String`: copy semantics. (Truffle-level string sharing is an optimization; the contract is copy.)
2. `[]byte` <-> `byte[]`: bulk copy on crossing; no shared backing array (preserves Go memory-safety reasoning).
3. Slices (other element types) <-> `java.util.List` via `ProxyArray`; maps <-> `java.util.Map` via `ProxyObject`. Both directions are *views with copy-on-explicit-request* semantics to be pinned in the API contract (view vs copy is the sharpest edge — decide per call, default copy).
4. `error` <-> exception: Go `error` crossing to Java becomes a `RuntimeException` proxy carrying the Go error value, message, and a captured Go stack; Java `Throwable` crossing to Go becomes a Go `error` implementation exposing `Message()`, `JavaStack()`, and `Unwrap()`. Panics and exceptions must not be conflated: a Java exception is a Go error, never a Go panic.
5. `func` values <-> lambdas / functional interfaces: Go funcs exposed as `ProxyExecutable`; Java lambdas consumed as Go funcs via generated adapters. Single-method interfaces get SAM-style adapters; multi-method interfaces get explicit adapter structs (generated).
6. Goroutines vs threads: a goroutine is a virtual thread (§4). Java code calling into Go runs on the calling thread; Go code calling Java blocks the goroutine (= parks the virtual thread) like any blocking call. A Java `Thread` started from Go is a platform thread and is visible as such (naming: `graalgo-goroutine-<id>` for the virtual threads so profilers read cleanly).
7. Channels: exposed to Java as an opaque handle with `Send`/`Recv`/`Close` methods (blocking, virtual-thread-friendly), not as a `BlockingQueue` — queue adaptation is a convenience wrapper, not the primitive.

### 2.3. Illustrative API shape (ILLUSTRATIVE — not the final API)

```go
// Illustrative only. Package: graalgo/java (extra JVM-enabled API surface,
// outside the "indistinguishable from go" core).
package java

// Import returns a handle to a Java class visible to the current context.
func Import(class string) Class
// Call invokes a method on a Java object; args/returns use the §2.2 mappings.
func (o Object) Call(method string, args ...any) (any, error)
// Export publishes a Go value (func, struct, channel handle) to Java bindings.
func Export(name string, v any)
```

Go-from-Java direction is annotation/codegen-driven: the graalgo tool generates `ProxyExecutable` registrations and `HostAccess` export lists at build time (closed-world-friendly — no runtime reflection scanning, which also keeps the native-image reachability story in §7 tractable).

## 3. cgo -> Panama FFI mapping

### 3.1. Mechanism

JEP 454 finalized the Foreign Function & Memory API in JDK 22 (primary: openjdk.org/jeps/454; GraalVM doc confirms "finalized in JDK 22 with JEP 454"). The mapping:

1. `import "C"` + preamble becomes a build-time binding-generation step (jextract-style, from C headers): `SymbolLookup` finds symbols; `Linker.downcallHandle` creates the call handle from a `FunctionDescriptor`; calls are `MethodHandle.invokeExact`.
2. C structs become `MemoryLayout`/`StructLayout` descriptors with explicit padding. **Struct layout is graalgo's responsibility**: it must implement the platform C ABI (field offsets, alignment, `_Bool`, bitfields, unions) — the highest-risk part of the mapping, and the part with no JVM help.
3. Go funcs passed to C as callbacks become `Linker.upcallStub` handles tied to an `Arena`; arena scoping replaces cgo's pointer-passing rules and is arguably *easier to get right* (lifetimes are lexically visible).
4. Restricted operations require `--enable-native-access` (JVM) / `--enable-native-access=` (native image; FFM support is on by default since GraalVM 25 — primary: GraalVM FFM-API.md). The graalgo launcher must pass this through; in native-image builds it is a build-time flag.

### 3.2. Difficulty assessment vs classic cgo

1. **Easier for users:** no C toolchain at user build time, no cgo pointer-rule footguns (arenas make lifetimes explicit), no generated `_cgo_` glue to debug.
2. **Harder for toolchain authors:** the C ABI layout engine and the header-to-descriptor generator are new, Go-version-independent but platform-dependent work (linux/x64, linux/AArch64, macOS/AArch64, windows/x64 per the GraalVM FFM doc — the platform list is narrower than Go's).
3. **Behavioral caveat (primary, JDK-8337395 / JEP 491):** a virtual thread executing a native/FFM call is *pinned* to its carrier for the call's duration, and a native call that calls back into Java code that blocks pins too. This matches Go's existing "cgo calls consume an M" economics, so it is not a regression — but graalgo must not let users believe Panama calls are as cheap as pure-Java calls.
4. **Scoped verdict:** Panama is a viable cgo replacement for the *supported-platform* subset; platforms outside the FFM list and code depending on cgo's exact threading/garbage-collection interactions are out of scope until re-tested per platform.

## 4. Virtual-thread scheduler mapping

### 4.1. Goroutine <-> virtual thread

1. 1:1 mapping: each goroutine is a virtual thread (`Thread.ofVirtual()`). The Go scheduler's M:N multiplexing is replaced by the JVM's scheduler; graalgo keeps only the Go-visible semantics (GODEBUG-relevant behaviors, `runtime.Gosched`, preemption points).
2. `GOMAXPROCS` maps to carrier-thread parallelism: `-Djdk.virtualThreadScheduler.parallelism` (and `.maxPoolSize` as the ceiling, analogous to `debug.SetMaxThreads`). The graalgo launcher translates `GOMAXPROCS=n` to these properties.
3. Structured concurrency (finalized after several preview rounds; reported final in Java 27 — secondary source, treat the version number as **[UNVERIFIED]**) gives graalgo an `errgroup`-shaped primitive for internal runtime use; it does not change the goroutine mapping.

### 4.2. Carrier-pinning risks (primary: JEP 491, JDK 24; JDK-8337395)

1. `synchronized` **no longer pins** as of JEP 491 (JDK 24) — the old "replace synchronized with ReentrantLock" guidance is obsolete on the target JDK. `-Djdk.tracePinnedThreads` was removed for the same reason.
2. What **still pins**: native calls (JNI **and** FFM downcalls — §3.1 item 3), class loading / static initializers, and native→Java upcalls that block.
3. Go code patterns that would pin, and the design response: (a) cgo/Panama calls — expected, matches Go's M-consumption; (b) package-level `init` doing class loading — happens once, acceptable; (c) a Go callback passed to C that blocks while C holds the upcall — pin; document as a known sharp edge, consider routing such callbacks onto dedicated platform threads.
4. Free observability: `jdk.VirtualThreadPinned` (enabled by default, 20 ms threshold) becomes the goroutine-pinning detector for the momo-style health endpoint (§1 item 9). `jdk.VirtualThreadStart/End` (disabled by default) and `jdk.VirtualThreadSubmitFailed` cover lifecycle accounting (primary: JDK-8303683, the JEP 444 umbrella).

### 4.3. Netpoller: replace or reuse?

1. Go's netpoller (per-thread epoll/kqueue event loop) is unnecessary on Loom: blocking `java.nio` socket operations park the virtual thread and the JDK's internal poller handles readiness. **Recommended: implement the netpoller on virtual-thread-friendly blocking NIO and delete the Go event loop** — this is the entire Loom value proposition and the lowest-risk path.
2. `io_uring` in the JDK: **no JEP has delivered it as of October 2026**; it remains a Project Loom roadmap "medium term" item (secondary: InfoWorld interview with Ron Pressler). Built-in JDK io_uring status: **[UNVERIFIED]** — assume absent.
3. If io_uring is wanted anyway, the options are: (a) Netty 4.2's `IoUringIoHandler` (third-party, real, 2026-era); (b) Panama FFI straight to liburing for a graalgo-owned poller (most work, most control); (c) experimental Netty/virtual-thread scheduler integrations (secondary sources; JDK 27+ Loom builds). None is needed for correctness — file this under measured-optimization, not architecture.

## 5. GraalVM isolates as module-isolation domains

### 5.1. What isolates give (primary: GraalVM embed-languages docs; `org.graalvm.nativeimage.Isolates`)

1. An isolate is an independent execution environment with **its own heap and GC** inside one OS process; isolates share AOT-compiled code and a copy-on-write image heap. No isolate can access another's heap by design.
2. Polyglot engines can run in a dedicated isolate (`--engine.SpawnIsolate`, `Context.Builder.spawnIsolate(true)`); per-isolate heap caps via `--engine.IsolateOption.MaxHeapSize`, with `PolyglotException.isResourceExhausted()` on breach; optional hardware enforcement via Intel MPK (`--engine.MemoryProtection`) on Linux.
3. Calls across the isolate boundary are "more costly as they need to cross a native boundary" (primary: embed-languages.md); `HostAccess.SCOPED` is recommended to avoid host/guest cyclic references (the two GCs are unaware of each other).

### 5.2. Fit to momo's requirements (§1)

1. **Crash containment:** isolate-per-module-generation is containment *by construction* — stronger than momo's copy-boundary. A panicking module tears down its isolate (free its heap); no kill-sentinel, no pin-join protocol, no stuck-residual class. Value-only boundaries become unnecessary *within* the mechanism (cross-isolate values still cross a boundary, where SCOPED params map neatly onto momo's pin protocol).
2. **Transparent restart:** new isolate + re-run constructor + atomic pointer swap = momo's §4.4 with less machinery. Crash-only state model (§1 item 7) fits perfectly: there is nothing to snapshot.
3. **Resources:** `MaxHeapSize` + `isResourceExhausted` maps directly onto momo's memory-quota→panic→restart. **CPU quota has no isolate equivalent — honest gap** (secondary thesis: "no mechanism in place to control the CPU utilization"). Workarounds: partition carrier pools per isolate (coarse, not a quota) or keep CPU accounting in the supervisor. Momo's CPU-throttle requirement cannot be met by isolates alone.
4. **Startup/teardown cost:** isolates are designed for per-request serverless use, so creation is cheap relative to a JVM boot; **exact numbers are [UNVERIFIED]** — the feasibility spike must measure isolate create/teardown latency and cross-isolate call overhead against the momo kill+join baseline.

### 5.3. Scoped negative verdicts

1. `SpawnIsolate` was EE-only and experimental with a limited language set in 22.0 (primary: SDK CHANGELOG) — verify availability on the target GraalVM distribution before depending on it. The verdict "isolates are production-ready for this" is **not established** for the community edition.
2. Isolates are a **native-image-only** feature. In pure-JIT mode the fallback is polyglot `Context`s in one shared heap (weaker isolation; `Context` sandbox policies `UNTRUSTED` et al. are policy, not heap isolation). The momo-on-graalgo story therefore differs by execution mode — this must be explicit in the architecture, not footnoted.
3. Native code (FFM/JNI) breaks heap isolation the same way cgo breaks momo's generation model — the MM-0017 verifier gate (§1 item 11) applies unchanged.

## 6. Observability

### 6.1. JFR events

1. graalgo defines custom `jdk.jfr.Event` subclasses: `graalgo.GoroutineStart/End` (in JIT mode these can alias the JDK's `jdk.VirtualThreadStart/End`), `graalgo.ChannelBlock` (send/recv blocked beyond threshold), `graalgo.ModuleCrash` (momo attribution: module, generation, crash class), and maps GC activity onto the standard `jdk.GC*` events.
2. Pinning detection is free (§4.2 item 4). In native-image mode JFR is supported (primary: GraalVM release notes reference `-XX:FlightRecorderLogging` and JFR GC event fixes), so the event story survives AOT.

### 6.2. Debugging: Delve cannot work; the replacement is the Truffle inspector

1. **Scoped verdict: `dlv` cannot work with graalgo.** Delve debugs Go binaries via DWARF and Go runtime internals; graalgo executes Truffle ASTs, so there is no Go binary to attach to. This is structural, not a tooling gap to be closed.
2. Replacement (primary: graalvm.org chrome-debugger docs): the Truffle Chrome Inspector — `--inspect[=[host:]port]` (default 9229), Chrome DevTools Protocol, breakpoints/stepping/variable inspection/eval, and explicitly "stepping through language boundaries during guest language interoperability." Graalgo must emit accurate `SourceSection`s from Go source positions so the inspector shows Go code, not generated code.
3. Programmatic alternative: the Truffle Debug API (`com.oracle.truffle.api.debug`) for scripted/CI debugging.

### 6.3. Profiling and `pprof` compatibility

1. `go tool pprof` consumes the pprof protobuf format (`profile.proto`). **graalgo can emit it**: CPU profiles from stack sampling (or translated JFR `jdk.ExecutionSample` streams) and heap profiles from allocation samples, serialized as `profile.proto` and served from a `net/http/pprof`-compatible endpoint. This is a translation layer, not a format invention — no compatibility verdict is at risk beyond getting the proto right.
2. async-profiler and JMC remain available in JVM mode for JVM-level questions (carrier pools, GC). In native-image mode, VisualVM/JMX/JDWP-style attach is unavailable — **known tooling gap** (secondary reports; absent from primary native-image docs) — so the pprof endpoint and JFR files are the primary interfaces there, not a fallback.

## 7. ADDENDUM — AOT+JIT hybrid, corrected design

**Corrected definition** (per the user's 2026-10-03 correction): the hybrid is *not* HotSpot-style tiering. The application code **and the JIT compiler itself** (Truffle partial evaluator + Graal compiler) are AOT-compiled into the native image; at runtime that AOT-resident JIT further optimizes hot code. This is GraalVM's documented JIT-in-Native-Image capability: the build output docs list "Runtime Compiled Methods — only shown if runtime compilation is built into the executable, for example, when building a Truffle language. Runtime-compiled methods account for graph encodings in the heap" (primary: oracle/graal `BuildOutput.md`); the "Java on Truffle — Going Fully Metacircular" writeup demonstrates the same pattern.

### 7.1. Version-tracking impact of the corrected hybrid

1. **The embedded compiler is GraalVM-version-coupled, not Go-version-coupled.** The partial evaluator, Graal compiler, and Truffle runtime ship inside the GraalVM distribution. Embedding them adds binary size and a validation surface, but that surface moves on the **GraalVM bump cadence**, not the Go release cadence.
2. **The per-Go-release bump surface is unchanged vs pure AOT:** (a) front-end AST generation from new stdlib sources; (b) closed-world reachability metadata (`reflect-config`/`jni-config`/`proxy-config`/`serialization-config`) re-derived via the native-image agent whenever the stdlib's dynamic features change — **this is the dominant per-release cost and is identical in pure AOT and hybrid**; (c) PGO profile re-collection per release *if* Oracle GraalVM PGO is used (PGO is an Enterprise feature — primary: Oracle GraalVM architecture material); (d) runtime-compiled-method graph encodings are regenerated automatically at build time from the front-end output, so they move with (a) for free.
3. **Headline: the corrected hybrid adds ~zero marginal per-Go-release maintenance vs pure AOT.** The one cross term: a GraalVM version bump can change points-to-analysis behavior and invalidate previously-derived reachability metadata — so schedule GraalVM-bump validation together with the metadata pipeline, and never bump both in the same release train without a combined regression run.

### 7.2. Observability: code compiled at runtime inside a native image

1. **JFR:** supported in native images (primary: GraalVM release notes reference `-XX:FlightRecorderLogging` and JFR GC-event fixes), so graalgo's custom events (§6.1) work in all three modes. Whether Truffle runtime compilations surface as `jdk.Compilation` events or only through Truffle instrumentation is **[UNVERIFIED]** — do not promise JMC compilation timelines for the hybrid until measured.
2. **Heap dumps / VisualVM:** the known native-image tooling gap (§6.3 item 2) applies unchanged to the hybrid — no JMX/JDWP attach, so no VisualVM, in any AOT-containing mode. The pprof-format heap exporter (§6.3 item 1) is therefore a requirement, not a nice-to-have, for the AOT and hybrid modes.
3. **Chrome inspector:** documented for language launchers generally (primary: chrome-debugger docs); native-launcher `--inspect` behavior on the target GraalVM version is **[UNVERIFIED]** — verify before promising Delve-replacement parity in AOT/hybrid modes.

### 7.3. Polyglot / Panama boundary: AOT-compiled caller vs runtime-JIT-compiled caller

1. **Semantics are identical** in both cases; the difference is optimization only. The AOT baseline will not speculate or inline across the Truffle interop boundary without profiles; the AOT-resident runtime JIT can inline through *stable* interop call targets after warmup via partial evaluation.
2. **FFM downcalls:** supported in native images since GraalVM 25 (primary: FFM-API.md, `--enable-native-access=`). No primary source promises cross-boundary inlining for downcall handles in either mode — treat any such performance claim as **[UNVERIFIED]** and do not let the design depend on it. Design for the conservative case (boundary cost ~= native call + handle dispatch); the runtime JIT is upside.
3. Consequence for §2.2: the view-vs-copy decisions at the polyglot boundary must be correct under the *unoptimized* AOT baseline, since a fresh isolate (momo restart, §5.2) runs cold.

### 7.4. Momo-relevant: isolates in JIT-in-image mode

1. **Isolates work the same structurally:** the isolate is a heap/GC boundary, orthogonal to how the code was compiled. AOT-compiled code is shared across isolates; runtime-compiled code is installed per engine/isolate (Truffle compilation is per-context). Isolate creation remains a heap-setup operation, not a codegen operation — cheap; teardown frees the heap and drops that isolate's compiled-code references.
2. **One real cost: warmup is per isolate.** A freshly restarted module isolate loses its runtime-JIT profiles and recompiles hot paths. This is acceptable — momo restarts are rare by design (parking bounds churn, §1 item 4) — but it sharpens the requirement that the **AOT baseline be good without the runtime JIT**, since every generation starts cold. The runtime JIT is then strictly a steady-state bonus, never a correctness or tail-latency dependency.
3. Cross-isolate call cost (§5.1 item 3, native-boundary crossing) applies unchanged in hybrid mode; runtime-compiled code on either side does not remove the boundary.

## T7 key takeaways

1. Goroutine=virtual thread is the cleanest mapping in the JVM ecosystem: JEP 491 (JDK 24) removed the `synchronized` pinning hazard, leaving only native/FFM calls as pin sources — which matches Go's existing cgo economics.
2. The polyglot API should be built on `org.graalvm.polyglot` with `SCOPED` host access and a new `graalgo/java` package; Delve is structurally impossible and the Truffle Chrome Inspector (`--inspect`, CDP) is its replacement.
3. cgo maps to Panama FFI (JEP 454, final in JDK 22) with real user-facing wins (no C toolchain, arena lifetimes) but new toolchain-author risk concentrated in the C ABI struct-layout engine; momo's MM-0017 verifier ban must gate the new path too.
4. GraalVM isolates fit momo's crash-containment + transparent-restart model better than the runtime fork does (containment by construction, crash-only restart = new isolate), with two honest gaps: no CPU quota mechanism, and `SpawnIsolate` maturity must be verified on the target GraalVM distribution.
5. The corrected AOT+JIT hybrid (JIT-in-image) adds ~zero marginal per-Go-release maintenance vs pure AOT — the compiler is GraalVM-version-coupled; the dominant per-release cost is the closed-world reachability-metadata pipeline, identical in both modes — but every module restart starts cold, so the AOT baseline must be good without the runtime JIT.

## Primary sources

1. JEP 491 (Synchronize Virtual Threads without Pinning), JDK 24 — https://openjdk.org/jeps/491 (contents confirmed via JDK-8337395: https://bugs.openjdk.org/browse/JDK-8337395)
2. JEP 454 (Foreign Function & Memory API), finalized JDK 22 — https://openjdk.org/jeps/454
3. JEP 444 JFR events (`jdk.VirtualThreadStart/End/Pinned/SubmitFailed`) — https://bugs.openjdk.org/browse/JDK-8303683
4. GraalVM FFM API in Native Image — https://github.com/graalvm/graalvm-community-jdk25u/blob/HEAD/docs/reference-manual/native-image/FFM-API.md
5. GraalVM embedding: polyglot isolates (`engine.SpawnIsolate`, `MaxHeapSize`, MPK) — https://github.com/graalvm/graalvm-for-jdk17-community-backports/blob/HEAD/docs/reference-manual/embedding/embed-languages.md
6. Native-image build output: "Runtime Compiled Methods" (JIT-in-image) — https://github.com/oracle/graal/blob/master/docs/reference-manual/native-image/BuildOutput.md
7. GraalVM reachability metadata (closed-world assumption) — https://github.com/graalvm/graalvm-community-jdk21u/blob/HEAD/docs/reference-manual/native-image/ReachabilityMetadata.md
8. Truffle Chrome Inspector — https://www.graalvm.org/dev/tools/chrome-debugger/
9. Java on Truffle: mixing AOT and JIT (precedent) — https://medium.com/graalvm/java-on-truffle-going-fully-metacircular-215531e3f840
