# Truffle modernization — design and migration plan (lane T2 design)

**Status:** design, decision-complete except where §13 says a prototype must
run first. Written 2026-11-17 for track T2 of [RECOVERY.md](RECOVERY.md), with
inputs to T0.B, T1 and T3. [SPEC.md](SPEC.md) is authoritative; this document
implements SPEC §2.4–2.7, §3.3, §3.4, §3.10, §3.11 and §4.3 and requirements
GG-0003/0005/0006/0008/0014/0015/0024/0025/0049/0050. Where it disagrees with
SPEC it says so and lists the disagreement in §14 as a proposed amendment;
nothing here overrides SPEC by itself.

It was written against the code at `a0df481b` and against the pinned Truffle
jars in [internal/backend/truffle/lib/](../../internal/backend/truffle/lib/)
(25.0.4.1.1). The companion design for the build path is `toolexec-design.md`
on branch `wt/recovery/toolexec-design` (referred to below as "the toolexec
design"; it is not on this branch, so it is not linked).

Evidence labels used throughout:

| Label | Meaning |
|---|---|
| `[CODE]` | read in the tree at `a0df481b` |
| `[JAR]` | checked with `javap` against the pinned jars |
| `[PROTO]` | observed by running the prototype of §13.1 on this box |
| `[DOC]` | official Truffle documentation or changelog, fetched 2026-11-17 |
| `[RECALL]` | known from other Truffle languages, not re-verified here; verify before depending on it |
| `[EST]` | estimate |

## 0. Summary

### 0.1 Decisions

| # | Decision | Why (short) |
|---|---|---|
| T1 | One `GoLanguage extends TruffleLanguage<GoContext>`, id `go`, registered for the byte MIME type `application/x-graalgo-bundle`. `parse()` takes a **linked bundle** (toolexec design §4.2), never Go source text. | The frontend is `go/types` in a Go process (GG-0012); the JVM only ever sees typed IR. |
| T2 | `contextPolicy = SHARED`. The AST, type descriptors, itabs, function units and the intrinsic table belong to the language instance; globals, package-init state, scheduler, timers, OS state and the unsafe handle table belong to `GoContext`. No node holds context state. | Required for multi-context engines, image-heap pre-initialization and engine caching; single-context runs lose nothing because `ContextReference.get(node)` folds to a constant on a bound engine. |
| T3 | Goroutines stay virtual threads (SPEC §3.5) but are created with `Env.newTruffleThreadBuilder(r).virtual(true)`, and the goroutine ↔ thread binding goes through one `ContextThreadLocal`. A context option selects platform threads instead. | A thread that runs guest code must be entered in the context. `[PROTO]` it works on the pin; it is experimental upstream and ~10× more expensive to create than a bare virtual thread, so §2.6 adds thread recycling. |
| T4 | Preemption moves from `ThreadLocal` + `System.nanoTime()` at every back-edge to **Truffle safepoints**: a monitor thread submits a `ThreadLocalAction` to a goroutine whose quantum expired; the action performs the quantum park. | Compiled loops carry no scheduler code at all; debugger suspend, cancellation and stack sampling use the same mechanism. `[PROTO]` delivered and parked on virtual threads. Fallback in §2.7. |
| T5 | Every node class is written against the conventions of §11: statically typed expression families, one small DSL node per operation and carrier, `@Cached` inline caches only at the four genuinely dynamic sites of SPEC §3.3, `@TruffleBoundary` on every runtime entry that is not PE-safe. | This is what makes PE succeed by construction instead of by repair. `[PROTO]` the pattern compiles with the pinned DSL processor and JIT-compiles. |
| T6 | The node factory, not the DSL, selects operations by type. Sized integers are carried as `int` (≤ 32 bit) or `long` (64 bit) in canonical extended form; narrowing wrap is a separate tiny node. | Go types are static; no type discovery is needed and no boxing or specialization explosion occurs. |
| T7 | Closures capture **typed cells**, never a `MaterializedFrame`; defers are **open-coded per function** in frame slots; the per-call shadow stack is deleted. | Each of the three currently forces heap allocation or thread-local access on every call. |
| T8 | Go values export `InteropLibrary` directly from the value classes (`@ExportLibrary`); the hot path never dispatches through a library. Polyglot `Proxy*` classes are not used for Go values. | Zero cost when unused; gives `Value`, tools and other languages one contract. |
| T9 | Stack traces (`panic` output, `runtime.Callers`) come from `TruffleStackTrace` / `iterateFrames`; the `rtstack` bridge and `GoStackFrame` chain are deleted. | Truffle already records call nodes lazily; the shadow stack allocates per call. |
| T10 | `go test -cover` keeps the stock `cover` source rewriting (toolexec design D2). Truffle's coverage instrument is an extra, not the implementation. | Drop-in by construction; profile format identical. |
| T11 | Stay on the AST interpreter now; decide on the Bytecode DSL at a named gate after toolexec M1d with the bake-off of §8. Node conventions are chosen so operations port. | Bytecode DSL is experimental in 25.0 and would contradict the letter of SPEC §3.3; it is also the only structural answer to interpreter speed, footprint and recursion depth. |
| T12 | Stack overflow is a fatal error (gc semantics), reported from a fresh thread; the depth limit is raised first by compiled code and a large carrier stack, measured before anything structural is built. | §9. |
| T13 | The new node factory of toolexec M1a targets a **new node package** written to §11; the existing ~110 classes are the semantic reference and are deleted at cutover (M3). | Otherwise the new path inherits every pattern of §1.2 and needs a second rewrite. This amends toolexec design §4.3 ("the node classes are the existing ones"). |

### 0.2 Findings that need the owner's attention

1. **Virtual threads under a polyglot context are experimental on HotSpot and
   emulated by platform threads in native images** `[DOC]` (Truffle changelog
   24.1, GR-40931; no later entry changes it up to 25.x). In the AOT and hybrid
   modes one goroutine would be one OS thread. This touches GG-0006/0049/0050
   and SPEC §3.5; see §7.4.
2. **Mode (b) "pure AOT" runs the Go program in the Truffle interpreter.**
   Native Image compiles the interpreter ahead of time, not the guest program.
   GG-0050 ("performant without the runtime JIT") is therefore a statement
   about interpreter speed; see §7.3 and the measurement in §13.2 (P-D).
3. **Creating a goroutine through the polyglot thread API cost 20–32 µs against
   ~2 µs for a bare virtual thread** `[PROTO]` (one box, under load, `nice 19`).
   §2.6 has the mitigation; it needs its own measurement (P-A).
4. SPEC §4.3's host-access default (`SCOPED`, never `ALL`) makes the
   `graalgo/java` package unable to call unannotated JDK classes; §5.6 proposes
   a split default.
5. The devirtualization thesis of SPEC §2.6.1 needs **call-target splitting**
   for library code (§4.7); inlining alone shares one inline cache among all
   callers.

### 0.3 What the prototype established

A 450-line standalone language (not in the repository; §13.1) compiled with
`javac -Xlint:deprecation -Werror` against the pinned jars and run on the
pinned JDK shows `[PROTO]`:

- `@TruffleLanguage.Registration` with a byte MIME type, `parse()` over a byte
  `Source`, `Context.eval`, on the class path, with the optimizing runtime
  ("Oracle GraalVM").
- Statically typed DSL nodes (`@NodeChild(type = I64Node.class)`, one
  `@Specialization`, no `@TypeSystem`, no `UnexpectedResultException`) generate
  and JIT-compile with `engine.CompilationFailureAction=ExitVM`: recursive
  `fib(32)` runs in 6.5 ms steady state, a 3·10⁸-iteration loop entered once
  runs in 11 ms through OSR.
- A struct in the SPEC §3.4 shape (descriptor + `long[]` + `Object[]`)
  exporting `InteropLibrary` is read, written and enumerated from Java through
  `org.graalvm.polyglot.Value`; a function value is executed from Java.
- 20,000 virtual threads created through `Env.newTruffleThreadBuilder`, each
  reading its `ContextThreadLocal`; and a `ThreadLocalAction` that parks a
  running virtual thread at a safepoint.

## 1. Baseline: how the backend uses Truffle today

### 1.1 Shape `[CODE]`

- There is no `TruffleLanguage` subclass. Root nodes are constructed with
  `super((TruffleLanguage<?>) null, descriptor)`
  ([GoRootNode.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/GoRootNode.java),
  [GoFuncRootNode.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/GoFuncRootNode.java)).
  The launcher builds a frame by hand and calls `program.root().execute(frame)`
  inside a scheduler-spawned virtual thread
  ([P3SyncMain.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3SyncMain.java),
  [Main.java](../../internal/backend/runtime/src/main/java/graalgo/launcher/Main.java)).
  No `org.graalvm.polyglot` class is referenced anywhere.
- The 104 files under
  [frontend/](../../internal/backend/truffle/src/main/java/graalgo/frontend/)
  hold the node classes and both factories. Across all three backend modules
  there are 2 `@Specialization` methods and 2 `@Cached` parameters (all in
  [CallDispatchNode.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/CallDispatchNode.java)),
  156 `@Child`, 20 `@Children`, and no `@TruffleBoundary`,
  `CompilerDirectives`, `@CompilationFinal`, `@ExplodeLoop`, profile,
  `@ExportLibrary` or instrumentation tag.
- Nodes are "kind-switched": one class per syntactic construct with a
  `GoKind` field, and `switch (kind)` inside every `execute*`
  (24 files, 49 switches). The carriers are already the right ones: `int` for
  8/16/32-bit integers, `long` for 64-bit, `float`, `double`, `boolean`,
  `Object`.
- The module named
  [interop/](../../internal/backend/interop/) contains stdlib bindings
  (`p3os`, `p3net`, `p3reflect`, …), the interface-dispatch node and the
  `rtstack` bridge. It contains no polyglot interop.
- Two factories exist: the JSON
  [NodeFactory.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/NodeFactory.java)
  (6,100 lines, 85 string-keyed `case` arms) and
  [NodeFactoryV2.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/v2/NodeFactoryV2.java).
  Both are deleted at toolexec M3.

Lane T0.B (branch `wt/recovery/jit`) is adding the minimum needed for
compilation to succeed on these classes (`@TruffleBoundary` on runtime entry
points, `@CompilationFinal(dimensions = 1)` and `@ExplodeLoop` in the root
node, the runtime's OSR `LoopNode`). That work is complementary: it makes v1
measurable until cutover. This design does not ask T0.B to refactor nodes.

### 1.2 Catalogue of PE-hostile patterns `[CODE]`

Paths are relative to
[internal/backend/](../../internal/backend/); `F/` abbreviates
`truffle/src/main/java/graalgo/frontend/`. "Fix" names the section of this
document that replaces the pattern in the new node package.

| # | Pattern | Where | Effect | Fix |
|---|---|---|---|---|
| H1 | No `@TruffleBoundary` anywhere; nodes call straight into `java.util` collections, string formatting, locks and I/O | all of `runtime/src/main/java/graalgo/runtime/`, `interop/src/main/java/graalgo/p3*/` | PE inlines the JDK until "Too deep inlining"; every function bails out | §11 rule B1 |
| H2 | `GoroutineState.current()` (`ThreadLocal.get`) on every call, twice per indirect call, plus in every `defer` and panic | `F/GoFuncRootNode.java`, `F/FuncValueCallNode.java`, `F/DeferNode.java`, `F/RangeFuncNode.java`, `interop/…/iface/IfaceDispatchNode.java`; holder in `runtime/…/runtime/GoroutineState.java` | thread-local lookup and null checks on the hottest path | §2.5, §4.5 |
| H3 | Shadow call stack: `state.pushGoStack(funcName, section)` allocates a `GoStackFrame` on every call; `updateTopFrameSection` writes it before every indirect call | `F/GoFuncRootNode.java`, `F/GoRootNode.java`, `F/FuncValueCallNode.java` | allocation and escaping writes per call | §6.5 |
| H4 | `Safepoint.poll()` → static field → `ThreadLocal.get` → lock-ownership check → `System.nanoTime()` at every function entry and loop back-edge | `runtime/…/safepoint/Safepoint.java`, `runtime/…/sched/SchedulerCore.java` (`pollCurrentThread`) | a clock read per iteration | §2.7 |
| H5 | Interface dispatch looks the itab up **by type-name string** on every call (`IfaceMethodTable.itabFor(descName, ifaceId, names)`), then walks a hand-rolled PIC of three `volatile` fields and a `Map` | `interop/…/iface/IfaceDispatchNode.java` | hash lookups and volatile reads at the site SPEC §2.6.1 bets the program on; target called through `GoCalls.invoke` (no `DirectCallNode`, never inlined) | §4.6 |
| H6 | Megamorphic and interface calls go through `RootCallTarget.call(Object...)` | `runtime/…/runtime/GoCalls.java` (`invoke`), `F/CallDispatchNode.java` (`callMegamorphic`) | the slow, never-inlined call path | §4.5 |
| H7 | Per-call argument handling by runtime `GoKind` test and `instanceof` chains (interface wrap/unwrap, atomic-cell cloning), with exception construction inline | `F/GoFuncRootNode.java` (`execute`), `F/Boxing.java` (`copyForAssign`) | large untaken branches in every callee prologue | §4.5 |
| H8 | Every call boxes every argument and result (`Boxing.box`, `Object[]`), results unboxed through `(Number).longValue()` | `F/FuncValueCallNode.java`, `F/Boxing.java` | boxing that escape analysis can only remove after inlining | §4.5 (accepted at non-inlined boundaries only) |
| H9 | Closures capture the parent's `MaterializedFrame`; captured loop variables live in `GoBox { Object value }` | `F/FuncLitNode.java`, `truffle/…/values/GoFunc.java`, `truffle/…/values/GoBox.java` | materializing a frame defeats scalar replacement of *all* locals of any function containing a closure; `GoBox` boxes every captured integer | §4.4 |
| H10 | Globals live in a `MaterializedFrame` built by the factory and embedded in nodes | `F/NodeFactory.java` (`globalsFrame`), `F/GlobalReadNode.java`, `F/GlobalWriteNode.java`, `F/GlobalBox*.java` | AST is welded to one program run; cannot share code between contexts | §2.4 |
| H11 | Control flow by exception with string labels: `BreakSignal`, `ContinueSignal`, `GotoSignal`, `FallthroughSignal` extend `RuntimeException` and compare labels with `String.equals`; `GoReturn` carries a boxed value | `F/BreakSignal.java`, `F/ContinueSignal.java`, `F/GotoSignal.java`, `F/FallthroughSignal.java`, `F/GoReturn.java`, `F/ForRepeatingNode.java` | not recognized as control flow by PE unless `ControlFlowException`; string compares in loops; boxed returns | §4.8 |
| H12 | `BlockNode` runs its children in a plain loop without `@ExplodeLoop`, wrapped in `try/catch (GotoSignal)`, and finds labels by scanning for `instanceof LabeledStmtNode` with `String.equals` | `F/BlockNode.java` | child calls are not unrolled into the compilation unit | §4.8 |
| H13 | `GoLoopNode extends LoopNode` with a hand-written `while` | `F/GoLoopNode.java` | no back-edge counting, no OSR (fixed by T0.B) | §4.8 |
| H14 | `switch (kind)` / `switch (op)` over node fields inside `execute*`, with `throw new UnsupportedOperationException("…" + op)` arms | 24 files, e.g. `F/BinaryOpNode.java`, `F/VarReadNode.java`, `F/ConvertNode.java`, `F/IndexNode.java`, `F/AssignNode.java` | folds under PE (fields are final) but is paid on every interpreted execution, inflates host-compiled `execute` methods, and concatenates strings on paths PE must still parse | §4.2 |
| H15 | JDK collections as node state and in execute paths: `List<String>`, `List<GoTypeDescriptor>`, `Map<String, Set<String>>`, `ArrayDeque` | `F/TypeSwitchNode.java`, `F/TypeAssertNode.java`, `F/TypeAssertOkNode.java`, `F/MethodValueNode.java`, `F/ConvertNode.java`, `F/DerefNode.java`, `runtime/…/runtime/DeferChain.java`, `truffle/…/values/GoStructDescriptor.java` (`List<Field>` read by `GoStruct.getLong`) | interface calls on non-constant receivers; not PE-safe | §4.6, §11 rule B3 |
| H16 | Dispatch on the Java class of a backing array at run time (`getClass().isArray()`, `instanceof long[]` ladders) although the element type is static | `F/IndexNode.java`, `F/IndexWriteNode.java`, `F/LenNode.java`, `F/CapNode.java`, `F/SliceExprNode.java`, `F/RangeArrayNode.java`, `F/Boxing.java` | type tests the factory could have resolved | §4.2 |
| H17 | `volatile` fields and static counters in nodes (`probeSite`, PIC slots, `megamorphicTransitions`) | `F/FuncValueCallNode.java`, `F/DeferNode.java`, `interop/…/iface/IfaceDispatchNode.java` | volatile reads are never constant-folded; static counters are process-global | §6.6 |
| H18 | Type identity by name: descriptors minted by Go type name through static `ConcurrentHashMap`s; fallback boxing infers a Go type from the Java class of the value | `F/NodeFactory.java` (`basicDescriptorForName` and five static maps), `F/GoFuncRootNode.java` (`wrapInterface`) | string hashing on boxing paths; a `Long` cannot say whether it is `int`, `int64` or `uint` | §4.6 (itabs and descriptors are PE constants) |
| H19 | `GoPointer` is one class with five mode fields and two static `Function` hooks | `truffle/…/values/GoPointer.java` | megamorphic field reads and calls through static mutable lambdas on every dereference | §4.4 |
| H20 | Process-global mutable state: `SchedulerCore.installed`, `GoroutineState.HOLDER`, `FuncIds` maps, factory descriptor maps, `GoOs` statics | `runtime/…/sched/SchedulerCore.java`, `truffle/…/values/FuncIds.java`, `interop/…/p3os/GoOs.java` | one program per JVM; blocks contexts, isolates-in-process and image-heap initialization | §2.4 |
| H21 | Node source sections stored in a mutable field per node with a setter | `F/GoExprNode.java`, `F/GoStmtNode.java` | 8 bytes on every node; sections exist only where the factory remembered to set them | §6.2 |

Two properties of the present code are worth keeping: the carrier mapping for
sized integers, and the value model (`GoStruct`/`GoSlice`/`GoString`/`GoMap`),
which RECOVERY.md's baseline already rates as matching SPEC §3.4.

## 2. `TruffleLanguage`, contexts and threads

### 2.1 Registration

```java
@TruffleLanguage.Registration(
    id = "go", name = "Go", implementationName = "graalgo",
    version = /* tracked Go release, e.g. */ "1.27.1",
    byteMimeTypes = GoLanguage.BUNDLE_MIME,          // "application/x-graalgo-bundle"
    defaultMimeType = GoLanguage.BUNDLE_MIME,
    contextPolicy = TruffleLanguage.ContextPolicy.SHARED,
    website = "…")
@ProvidedTags({StandardTags.RootTag.class, StandardTags.RootBodyTag.class,
               StandardTags.StatementTag.class, StandardTags.CallTag.class,
               StandardTags.ExpressionTag.class,
               StandardTags.ReadVariableTag.class, StandardTags.WriteVariableTag.class})
public final class GoLanguage extends TruffleLanguage<GoContext> { … }
```

All attributes exist on the pin `[JAR]`: `id`, `name`, `implementationName`,
`version`, `defaultMimeType`, `characterMimeTypes`, `byteMimeTypes`,
`interactive`, `internal`, `dependentLanguages`, `contextPolicy`, `services`,
`fileTypeDetectors`, `needsAllEncodings`, `website`, `sandbox`,
`internalResources`, `optionalResources`.

- **No `characterMimeTypes`.** The language does not accept Go text. A `Source`
  for a `.go` file is still created (language id `go`, no MIME type needed)
  for `SourceSection`s, with content loaded lazily from the path recorded in
  the bundle (`Source.CONTENT_NONE` until a tool asks). Evaluating Go text
  fails with one line naming `go build`. An inline-parse entry
  (`parse(InlineParsingRequest)`, used by the debugger's "evaluate expression")
  is out of scope for v1; the debugger's variable view does not need it.
- **`dependentLanguages`: none.** cgo → Panama (SPEC §3.7) uses
  `java.lang.foreign` directly, not Truffle NFI.
- **`services`: none in v1.** Tools reach profiler and debugger through their
  own instruments. If the pprof endpoint (§6.7) is implemented outside the
  language it gets a service interface then.
- **`internalResources`: none.** Every bundle carries all packages it needs
  (toolexec design §4.2), so there is no language home to locate at run time.
- **`sandbox`: default (`TRUSTED`).** A Go program performs raw system calls;
  it cannot meet a stricter sandbox policy.
- **`needsAllEncodings = false`.** Go strings are `byte[]`; conversion to host
  strings is UTF-8 only.
- `-proc:full` (or `-processorpath`, as the existing Makefiles do) is required
  on JDK 23+ or the DSL and registration processors silently do not run
  `[PROTO]`.

Options are declared with `@Option` in a `GoOptions` class and returned from
`getOptionDescriptors()`: `go.RunMain` (boolean, §2.3), `go.GoroutineThreads`
(`virtual` | `platform`, §2.6), `go.Args` is **not** an option — program
arguments come from `Env.getApplicationArguments()`.

### 2.2 What `parse()` takes

`ParsingRequest.getSource()` is a byte-based `Source` with MIME type
`application/x-graalgo-bundle` whose bytes are the bundle of toolexec design
§4.2 (manifest + per-package GIR), or a small reference record
`{path, offset, length}` when the bundle sits inside an executable — the
loader then maps the file instead of copying it into the Java heap. The
launcher builds it with
`Source.newBuilder("go", ByteSequence, name).mimeType(BUNDLE_MIME).cached(true)`.

`parse()` does the context-independent work once per language instance and
`Source` (Truffle caches the returned `CallTarget` per source):

1. `BundleLoader` reads the manifest and builds `PackageUnit`s, the symbol
   table and the lazily decoded `FunctionUnit`s (toolexec design §4.3).
2. Type descriptors are interned in the language-level `TypeTable`.
3. It returns the call target of a `GoProgramRootNode`.

Executing that call target does the per-context work: allocate this context's
global storage for every package, run package initialization in manifest
order, and — if the bundle has a `main` package and `go.RunMain` is true —
run `main.main` as the first goroutine and wait for it. The value returned to
`Context.eval` is the program's **module object** (§5.4).

For the transition, while v1 still exists, the same language class also
accepts `application/x-graalgo-wire+json` (the v1 JSON wire format) and hands
it to the old factory. That MIME type is deleted with v1 at M3 (§10, TR-1).

### 2.3 Program lifecycle inside a context

| Go event | Truffle mechanism |
|---|---|
| `main.main` returns | all other goroutines are abandoned: the context is marked finished, goroutine threads are cancelled through the context's cancellation, `eval` returns the module object. The launcher exits 0. |
| `os.Exit(n)` | `env.getContext().closeExited(node, n)` `[JAR]`; the host sees `PolyglotException.isExit()` with the status. `exitContext()` flushes what gc flushes (nothing: Go runs no deferred calls or finalizers on exit). |
| unrecovered panic, `fatal error` | the runtime prints gc's text to the context's fd 2, then `closeExited(node, 2)`. |
| host closes or cancels the context | `finalizeContext` stops the scheduler monitor; goroutines observe cancellation at their next safepoint or blocking call (§2.7); `disposeContext` releases OS resources. |
| deadlock ("all goroutines are asleep") | detected by the scheduler as today (lane T0.A), reported as a fatal error. With `go.RunMain=false` there is no main goroutine and the detector is off, exactly as lane T0.A arms it "only for programs". |

`go.RunMain` defaults to `true`, so generic launchers and tools that only call
`eval` run the program. An embedder that wants to call exported Go functions
sets it to `false`: `eval` then initializes packages and returns the module
object without running `main`.

### 2.4 Language instance versus context

`ContextPolicy.SHARED` means one `GoLanguage` instance may serve several
contexts of one engine, concurrently. The rule that makes this safe is
mechanical: **a node may reference only language-level objects; context-level
objects are reached through `GoContext.get(node)`.**

| Owned by `GoLanguage` (shared, immutable or append-only, thread-safe) | Owned by `GoContext` (one program run) |
|---|---|
| decoded bundles, `PackageUnit`s, `FunctionUnit`s and their `RootCallTarget`s | package-level variable storage (`long[]` + `Object[]` per package, same carrier split as structs) |
| `TypeTable`: one descriptor per canonical type key (toolexec design §3.4) | package-init state and the init-once guards |
| itab cache `(dynamic type, interface type) → GoItab`, computed lazily, entries immutable | scheduler (`SchedulerCore`), run-token accounting, timers, the monitor thread |
| intrinsic table (symbol name → node factory) | goroutine registry, goroutine ids, panic/exit state |
| generic-instantiation cache (function unit × type arguments → call target) | OS surface: argv, environment, cwd, fd table, signal handlers |
| `Source` objects, PC table for `runtime.Callers` (§6.5) | `unsafe.Pointer` handle table, finalizer queue, `sync.Pool` per-thread caches |
| the `ContextThreadLocal` and `ContextReference` objects themselves | the Truffle `Env`, loggers, allocation reporter |

Consequences for the code:

- `GlobalReadNode`-style nodes hold `(packageIndex, slotIndex)` and read
  `GoContext.get(this).globals[packageIndex]`. On a bound engine the context
  reference is a PE constant, so the read compiles to a load from a constant
  array — the same code as today's embedded frame, without H10.
- Every static mutable field listed under H18 and H20 moves to one of the two
  columns. A review gate (§11, rule S1) forbids new ones.
- References are created once and are static finals, as Truffle prescribes:

  ```java
  private static final LanguageReference<GoLanguage> LANGUAGE = LanguageReference.create(GoLanguage.class);
  private static final ContextReference<GoContext>  CONTEXT  = ContextReference.create(GoLanguage.class);
  ```

- Context-thread state is created through the `locals` provider
  (`locals.createContextThreadLocal(...)`); the like-named methods directly on
  `TruffleLanguage` are deprecated since 23.1 `[DOC]` and the build has
  `-Xlint:deprecation -Werror`.
- A single-context fast path must not be hand-rolled (no
  `Assumption singleContext`). If measurements later show a multi-context
  penalty that matters, `initializeMultipleContexts()` is the hook.

Until TR-5 lands (§10), the language declares `ContextPolicy.EXCLUSIVE` and
refuses a second context with a clear error; statics stay where they are.

**Isolates (GG-0025/0026).** A momo module generation is one `Context` in its
own isolate (native-image modes) or OS process (JIT mode). Nothing is shared
between isolates, so the table above describes sharing *within* an isolate
only. What this design contributes to GG-0025 is the precondition: a real
`TruffleLanguage` whose values cross the boundary only as interop values
(§5), no process-global state, and context cancellation that reliably stops
goroutines. Spawning a custom language in a polyglot isolate
(`engine.SpawnIsolate`) is an Oracle GraalVM feature whose availability for
third-party languages on the pin is unverified — it stays under GG-0027's
"re-verify on the pinned distribution" and belongs to track T3.

### 2.5 Goroutine state: `G` and `M`

Two objects replace `GoroutineState.HOLDER`, `SchedulerCore.currentHandle`
and the other thread-locals of §1.2:

- `G` — the goroutine: id, scheduler handle (`GoroutineHandle` today), panic
  state, `recover` eligibility, `sync.Pool`/`rand` caches. Created by the `go`
  statement.
- `M` — the thread's slot: `ContextThreadLocal<M>`, one per (context, thread),
  created by the factory lambda on first use. It has one mutable field,
  `M.g`, the goroutine currently running on this thread.

`language.m.get()` is the only way runtime code finds its goroutine. It is
needed on slow paths only (blocking operations, panic, `recover`, `go`), which
is what makes T4 and T7 matter: with open-coded defers (§4.5), safepoint
preemption (§2.7) and Truffle stack traces (§6.5), **compiled Go code that
does not block, panic or spawn never touches `G`**.

Remaining thread-locals: `GoNetpoll.LAST_OPEN_ERRNO`,
`ReflectLinknames.SYNTHESIZING`, `ChanOps.SELECT_RESULT` and `GoPool.locals`
become fields of `G` or `M`. `ThreadLocalRandom` in `SelectOp` is fine (it is
not context state).

### 2.6 Threads

`[JAR]` `TruffleLanguage` offers `isThreadAccessAllowed(Thread, boolean)`,
`initializeMultiThreading(C)`, `initializeThread(C, Thread)`,
`finalizeThread`, `disposeThread`; `Env` offers `isCreateThreadAllowed()`,
`newTruffleThreadBuilder(Runnable)` (builder methods `context`, `threadGroup`,
`stackSize`, `beforeEnter`, `afterLeave`, `virtual(boolean)`, `build`),
`createSystemThread(Runnable)` and `submitThreadLocal(Thread[], ThreadLocalAction)`.

Decisions:

1. `isThreadAccessAllowed` returns `true` for every thread, single- or
   multi-threaded. Go is multi-threaded from the first instruction;
   `initializeMultiThreading` is a no-op because the runtime never has a
   single-threaded mode.
2. **Goroutine threads** are built with
   `env.newTruffleThreadBuilder(body).virtual(true).build()`. The polyglot
   layer enters and leaves the context around `body`. `createContext` fails
   with a clear message if `!env.isCreateThreadAllowed()`: a Go program cannot
   run without goroutines, so embedders must build the context with
   `allowCreateThread(true)`.
3. **Runtime service threads** (scheduler drainer, preemption monitor, timer
   thread) use `env.createSystemThread`. They never run guest code and are
   stopped in `finalizeContext`.
4. **Foreign threads.** A host thread that calls into Go (an exported
   function, a `func` value) gets an `M` from the thread-local factory and a
   *foreign* `G`. A foreign `G` holds no run token and is never
   quantum-parked: GOMAXPROCS bounds goroutines, not the embedder's threads.
   When it blocks (channel receive, mutex) it parks the host thread itself.
   This mirrors a cgo callback on a non-Go thread, minus the P acquisition,
   and is a documented difference.
5. **`go.GoroutineThreads=platform`** builds the same threads with
   `virtual(false)`. The scheduler is indifferent (it parks and unparks
   `Thread`s). This option exists for three reasons: debugging if P-C (§13.2)
   shows the caller-frame limitation hurts the inspector; native images, where
   Truffle maps virtual threads to platform threads anyway (§7.4); and
   bisecting Loom-related problems.

Upstream status `[DOC]`, changelog 24.1 (GR-40931): "Virtual threads with a
polyglot context are now experimentally supported on HotSpot. Experimental
because access to caller frames in write or materialize mode is not yet
supported"; "Using virtual threads in a native-image will now emulate virtual
threads using platform threads until Loom support for Truffle languages in
native-image is implemented." The pinned runtime prints the corresponding
warning on first use `[PROTO]`; the launcher sets
`engine.WarnVirtualThreadSupport=false`. Go code itself never reads or writes
a caller's frame (§4.4), so the limitation concerns tools, not the language.

**Cost and recycling.** `[PROTO]` spawn + join of 20,000 trivial threads:
395–523 ms through the polyglot builder (20–26 µs each; 32 µs in a run of
5,000) against 37–55 ms (1.8–2.7 µs each) for bare `Thread.ofVirtual()`,
measured under `nice -n 19` on a box at load average ≈ 9, one JVM, no
repetition control. The order of magnitude is the finding: the polyglot layer
registers every thread with the context (thread info, `initializeThread`,
`disposeThread`, safepoint bookkeeping). gc creates a goroutine in a fraction
of a microsecond, and Go programs create goroutines freely.

Decision: goroutine threads are **recycled**. A goroutine body that returns
does not end its thread; the thread takes the next pending `go` closure from a
per-context queue, or parks in a bounded idle pool (retire after an idle
timeout, cap = a small multiple of GOMAXPROCS plus the number of threads
currently parked). Because goroutine identity is `G`, not the thread (§2.5),
recycling is invisible to Go code: `M.g` is replaced, a fresh `G` gets a fresh
id. It is visible in JFR and thread dumps, where thread names no longer map
1:1 to goroutines; the `graalgo.GoroutineStart/End` events (SPEC §3.10) carry
the goroutine id and remain correct. P-A (§13.2) measures the gain and the
steady-state cost of a `go` statement before this is built.

### 2.7 Preemption and blocking through Truffle safepoints

Truffle polls its own safepoint at every `LoopNode` back-edge and call-target
boundary in compiled and interpreted code; the poll is what lets the debugger
suspend a thread, the sampler take a stack and the engine cancel a context.
`ThreadLocalAction` runs a callback on a chosen thread at its next poll
`[JAR]`.

Decision T4:

- `Safepoint.poll()` as a Go-level node disappears. Loops are
  `Truffle.getRuntime().createLoopNode(...)`; function entry needs nothing.
- The scheduler keeps, per running goroutine, the deadline it already keeps
  (`quantumDeadlineNanos`, quantum 10 ms `[CODE]`). A per-context **monitor
  thread** (gc's `sysmon`) wakes every quantum, and for each goroutine that
  holds a run token past its deadline submits
  `new ThreadLocalAction(false /*side effects*/, false /*synchronous*/)` whose
  `perform` calls the existing `quantumPark(handle)`. At most GOMAXPROCS
  submissions per quantum.
- Sections that must not be preempted (the `PinLock` sections of
  `sync.Pool`) bracket themselves with
  `TruffleSafepoint.getCurrent().setAllowActions(false)` and restore the
  previous value — replacing the `PinLock.isHeldByCurrentThread()` test in
  the poll.
- **Every park in the runtime goes through one helper** that uses
  `TruffleSafepoint.setBlockedThreadInterruptible(node, …)` (or
  `setBlockedFunction`) `[JAR]`. A goroutine blocked in a channel operation
  then still answers thread-local actions: cancellation, the debugger, and the
  all-goroutine stack dump of §6.5. Blocking system calls made on behalf of
  the program get the same wrapper.
- Runtime loops that are not Truffle loops (a `@TruffleBoundary` method
  scanning a large map, `memmove` of a huge slice) do not poll. The engine
  option `engine.TraceMissingSafepointPollInterval=N` `[DOC]` reports
  stretches without a poll and is run over the corpus as a gate (§10, TR-6).

`[PROTO]` An asynchronous action submitted to a spinning thread was performed
on that thread and parked it with `LockSupport.parkNanos`, for platform
threads, virtual threads, and virtual threads that also yield. One prototype
variant never received the action: a loop compiled by the *host* JIT (not by
Truffle PE) whose only other memory access was a racy read of a plain array
element; it failed identically on platform threads. The likely cause is that
the host compiler hoisted the handshake read out of a loop with no memory
kill, which cannot happen in guest loops (PE inserts the safepoint as a
compiler node) but can in hand-written runtime loops — hence the rule that
runtime loops poll through `TruffleSafepoint.poll(node)` only next to real
work, and the gate above. This explanation is a hypothesis; P-B must confirm
it.

`[RECALL]` GraalPy releases and re-acquires its GIL from a `ThreadLocalAction`
submitted by a timer, which is the same shape (block inside the action).

What P-B (§13.2) must show before the calling convention is frozen: fairness
and latency equal to the current poll on the scheduler torture battery
(`TortureBattery`, `WakeupModelTest`), no lost wake-ups when an action parks,
and correct behaviour when cancellation arrives while a goroutine is parked
inside the action.

**Fallback** if P-B fails: an explicit flag. Every Go call passes the current
`G` as a hidden leading argument; the poll is `if (g.preempt) slowPath()`
(one load and branch, the flag set by the same monitor thread — the analogue
of gc's stack-guard poisoning). This costs an argument slot per call and an
entry lookup at every host → Go boundary, which is why it is the fallback and
why the decision is taken before TR-3 fixes the calling convention.

## 3. Carriers and frames

### 3.1 Sized integers without boxing or specialization explosion

Go's types are static, so the mapping from Go type to Java carrier is a pure
function applied by the node factory. The current mapping is kept:

| Go type | Carrier | Canonical form held in the carrier |
|---|---|---|
| `bool` | `boolean` | |
| `int8`, `int16`, `int32` (`rune`) | `int` | sign-extended value |
| `uint8` (`byte`), `uint16` | `int` | zero-extended value (0…255, 0…65535) |
| `uint32` | `int` | the 32 bits; arithmetic is two's-complement identical, the unsigned operations differ |
| `int`, `int64` | `long` | value |
| `uint`, `uint64`, `uintptr` | `long` | the 64 bits |
| `float32` / `float64` | `float` / `double` | |
| `complex64` / `complex128` | immutable value object (as today) | scalar-replaced when it does not escape |
| `string`, slices, maps, channels, funcs, pointers, interfaces, structs, arrays | `Object` (exact class known statically except where §4.4/§4.6 say otherwise) | |
| `unsafe.Pointer` | per SPEC §3.4 (handle) | |

Invariant: **every `int`/`long` produced by an expression node is in canonical
form for its Go type.** With that invariant most operations need no knowledge
of the width:

| Operation class | Node variants needed | Notes |
|---|---|---|
| `+ - *`, unary `-`, `<<`, `++ --` | `I32`, `I64`, then a **wrap node** for the four narrow types | Java wraps at 32/64 bits exactly as Go does. For `int8/16`, `uint8/16` the result leaves canonical form and is re-normalized by `WrapI8Node` = `(byte) x`, `WrapI16Node` = `(short) x`, `WrapU8Node` = `x & 0xFF`, `WrapU16Node` = `x & 0xFFFF`. |
| `& \| ^ &^` | `I32`, `I64` | canonical operands give a canonical result for signed and unsigned narrow types alike; no wrap. Unary `^` on unsigned narrow types wraps. |
| `/ %` | `I32`, `U32`, `I64`, `U64` | unsigned variants use `Integer/Long.divideUnsigned`/`remainderUnsigned`. Narrow unsigned operands are already non-negative `int`s and use the signed `I32` node. Signed narrow division wraps only for `MIN / -1`; the factory adds the wrap node. Division by zero panics behind a branch profile. `MIN / -1` needs no check: Java and Go agree (`MIN`, remainder 0). |
| `>>` | `I32` arithmetic, `U32` logical, `I64`, `U64` | canonical operands, no wrap. Shift counts are normalized to `long` by the factory; a count ≥ width gives 0 or the sign fill, never Java's masked count; a negative signed count panics. |
| `== !=` | `I32`, `I64` | |
| `< <= > >=` | `I32` (all of `int8/16/32`, `uint8/16`), `U32`, `I64`, `U64` | canonical narrow unsigned values compare correctly as signed `int`s. |
| integer ↔ integer conversion | one node per (carrier, target form) pair: 2 carriers × 8 forms, most of them the wrap nodes above plus `I32→I64` sign- and zero-extension | |
| integer ↔ float | per carrier and signedness (`U64→F64` and `F64→U64` need the unsigned algorithms; `F→int` out-of-range results follow gc on amd64, which the differential corpus pins) | |

So the arithmetic surface is roughly 20 operations × at most 4 carrier
variants, written once as small DSL nodes, plus 4 wrap nodes — not a class per
(operation, Go type). In compiled code a wrap node is one instruction fused
into its consumer. In the interpreter it is one extra node dispatch on narrow
types only; if the interpreter benchmark of P-D shows that it matters for
`uint8` loops, fused variants (`AddU8Node`) are added for the hot operations,
which the conventions allow without touching the factory's logic.

No `@TypeSystem`, no implicit casts, no `UnexpectedResultException`: an
expression node's Java return type is fixed by the Go type of the expression
(§4.1).

Untyped constants never reach the backend as values: the frontend emits the
exact constant already converted to the type the context requires (toolexec
design §3.1). `exactDecimal()` and the `UNTYPED_*` kinds disappear from nodes.

### 3.2 Frame layout

- The factory builds one `FrameDescriptor` per function with
  `FrameDescriptor.newBuilder()`; each Go local, parameter and named result
  that is neither captured nor address-taken gets one slot whose kind is
  decided once from its Go type. Distinct Go variables in disjoint scopes may
  share a slot only if they have the same carrier (the factory's choice;
  default: no sharing, simpler debugging).
- Slots are accessed with the kind-specific accessors (`getLong`/`setLong`,
  …). Because kinds never change, the `FrameSlotTypeException` path is dead
  code; nodes do not catch it (H1-style `catch` blocks around frame reads, as
  in today's root nodes, go away). `[JAR]` Truffle also offers *static* slots
  (`FrameSlotKind.Static`, `getLongStatic`/`setLongStatic`, …), which skip the
  tag byte entirely and are intended for statically typed languages. They are
  a later optimization: the debugger's default frame views read tagged slots,
  so static slots require the scope objects of §6.4 first. P-D measures
  whether the tag writes show up at all.
- Locals of struct, array and other reference types use `Object` slots holding
  the exact value class.
- A captured or address-taken variable's slot holds its **cell** (§4.4).
- Slot names and the Go type are stored as slot name/info so that tools can
  show locals (§6.4).
- `defaultValue`: slots are explicitly zeroed by the declaration node (Go's
  zero value), never left to a descriptor default; `defaultValueIllegal()`
  `[JAR]` is set in test builds so that a read-before-write is an error rather
  than a silent 0.
- Arguments arrive in `frame.getArguments()` and are copied into typed slots
  by a prologue with `@ExplodeLoop` over a `@CompilationFinal(dimensions = 1)`
  slot table (what T0.B did to the existing root node), or read in place by
  `ArgNode`s when a parameter is never assigned (factory decision; default:
  copy).
- Go code never materializes a frame, never reads a caller's frame, and never
  stores a frame in a heap object.

## 4. Node design

### 4.1 Families and base classes

SPEC §3.3 prescribes "expression nodes with typed execute methods … and void
statement nodes". The realization:

```java
public abstract class GoNode extends Node { /* source section encoding, §6.2 */ }

public abstract class ExprNode extends GoNode implements InstrumentableNode {
    public abstract Object executeGeneric(VirtualFrame frame);   // boxing boundary, tools, interop
}
public abstract class I64Node  extends ExprNode { public abstract long    executeLong(VirtualFrame f);
    @Override public final Object executeGeneric(VirtualFrame f) { return executeLong(f); } }
public abstract class I32Node  extends ExprNode { public abstract int     executeInt(VirtualFrame f); … }
public abstract class F64Node  extends ExprNode { public abstract double  executeDouble(VirtualFrame f); … }
public abstract class F32Node  extends ExprNode { public abstract float   executeFloat(VirtualFrame f); … }
public abstract class BoolNode extends ExprNode { public abstract boolean executeBoolean(VirtualFrame f); … }
public abstract class RefNode  extends ExprNode { /* executeGeneric only; subclasses narrow with casts */ }

public abstract class StmtNode extends GoNode implements InstrumentableNode {
    public abstract void executeVoid(VirtualFrame frame);
}
```

A child field is declared with the family it needs (`@Child I64Node left`), so
a type error in the factory is a Java compile error or a `ClassCastException`
at tree construction — not an `UnsupportedOperationException` at run time as
with today's `GoExprNode` defaults. `[PROTO]` DSL nodes declared as

```java
@NodeChild(value = "left",  type = I64Node.class)
@NodeChild(value = "right", type = I64Node.class)
abstract static class AddI64Node extends I64Node {
    @Specialization static long add(long a, long b) { return a + b; }
}
```

generate an `executeLong` that calls the children's `executeLong` directly,
with no state bits and no generic fallback.

This is how SPEC §3.3's "DSL specialization budget is spent only at genuinely
dynamic sites" is read here: the DSL is used everywhere as a *code generator*
(child evaluation, inlined helper nodes, uncached variants for interop), while
*run-time specialization* — more than one `@Specialization` chosen by observed
values, `@Cached` inline caches — appears only at the sites of §4.6.

### 4.2 The factory selects; nodes do not switch

For every construct whose behaviour depends on a Go type, the factory picks
the node class (or DSL factory) from the static type. Nodes contain no
`switch (kind)` (H14) and no class tests on arrays (H16).

| Construct | Variants (chosen statically) | Notes |
|---|---|---|
| local read/write | per carrier: `LocalI32/I64/F32/F64/Bool/RefNode`, cell variants (§4.4) | `@NodeField int slot` |
| global read/write | per carrier, `(packageIndex, index)` fields, context reference (§2.4) | |
| arithmetic, compare, convert | §3.1 | `@NodeChild` + single `@Specialization` |
| string ops | `StrLen`, `StrIndex` (`bytes[i] & 0xFF`), `StrConcat`, `StrEq`, `StrLess`, `StrSlice`, conversions | `GoString` stays `byte[]`-backed (SPEC §3.4). `Arrays.equals`/`mismatch` and `System.arraycopy` are on the allow-list of §11 rule B4; `for` loops over bytes are fine under PE (they are not unrolled). |
| slice index / store | per element carrier: `byte[]`, `short[]`, `int[]`, `long[]`, `float[]`, `double[]`, `boolean[]`, `Object[]` | `CompilerDirectives.castExact(s.array, long[].class)`; bounds test `Integer.compareUnsigned(i, s.len) < 0`, failure behind `InlinedBranchProfile` → boundary method that builds the panic. Unsigned narrow element reads mask. |
| array (`[N]T`) index / store | same carriers; constant index proven in range by `go/types` needs no check | |
| `len`, `cap` | per operand kind (string, slice, array constant, map, chan) | |
| slicing `s[i:j:k]` | per operand kind | `GoSlice` is a small final-field object; scalar-replaced when it does not escape |
| struct field read/write | per carrier: `FieldI64Node(index)` reads `s.longs[index]`, `FieldRefNode(index)` reads `s.objects[index]`; float fields via raw-bits conversion | the field path through embedded structs is flattened by the frontend into a chain of nodes with constant indices; `GoStruct.getLong`'s per-access validation against `List<Field>` (H15) is replaced by asserts |
| struct copy (value assignment) | `StructCopyNode(descriptor)` with the descriptor as a PE constant and `@ExplodeLoop` over nested value fields | replaces `Boxing.copyForAssign`'s `instanceof` ladder (H7) |
| composite literals, `new`, `make` | per type | allocation nodes report to `AllocationReporter` only when it is active (§6.7) |
| `append`, `copy`, `clear`, `delete`, `min`, `max` | per element carrier / key kind | growth path behind `@TruffleBoundary`-free helper with `@InliningCutoff`; `System.arraycopy` |
| map read/write/delete/range | per key kind: integer carriers, `string`, generic (interface/struct/array keys) | §4.6 |
| channel ops, `select`, `go` | one node each, runtime entry behind `@TruffleBoundary` | non-blocking fast paths may be added later behind profiles |
| conversions to interface | `ToIfaceNode(itab)` / `ToEfaceNode(type)` with constant descriptor | §4.6 |

Runtime helpers that nodes call live in final classes of static methods
(`GoStrings`, `GoSlices`, `GoMaps`, …), each method either PE-safe by rule B4
or annotated `@TruffleBoundary`. That is also what keeps the operations
portable to Bytecode DSL operations (§8).

`@NodeField` is used for immutable per-node data (slot, index, descriptor);
plain `final` fields in hand-written nodes are equivalent. Arrays in nodes are
`@CompilationFinal(dimensions = 1)`. `@ImportStatic` brings guard helpers into
DSL expressions (`@ImportStatic(GoGuards.class)`); `@Fallback` is used only in
the pointer-shape nodes of §4.4 and the interop conversion nodes of §5.3,
where it throws an internal error — in statically typed nodes there is no
fallback to write.

`@GenerateInline`, `@GenerateCached`, `@GenerateUncached`:

- AST nodes built by the factory are ordinary cached nodes (`@GenerateInline`
  does not apply to nodes with `@NodeChild`).
- **Helper nodes** used from specializations and from interop exports
  (`ToGoNode`, `ToInteropNode`, `ItabLookupNode`, `MapHashNode`,
  `CallFuncNode`) are `@GenerateInline @GenerateCached(false)` and take a
  leading `Node inliningTarget`; they are used as `@Cached ItabLookupNode n`
  plus `@Bind Node node` (the pinned DSL accepts `@Bind` on a `Node` parameter
  without an expression `[PROTO]`). They add no child node and no extra
  interpreter dispatch.
- Helpers reachable from interop exports or from runtime slow paths also get
  `@GenerateUncached`, so `InteropLibrary.getUncached()` callers and
  `@TruffleBoundary` code can use them.
- Profiles in DSL nodes are the inlined ones (`InlinedBranchProfile`,
  `InlinedConditionProfile`, `InlinedCountingConditionProfile`,
  `InlinedLoopConditionProfile` `[JAR]`); hand-written nodes use
  `BranchProfile`/`CountingConditionProfile` fields.

Profile policy: `CountingConditionProfile` on `if` and loop conditions (it
feeds branch probabilities to Graal); `InlinedBranchProfile` in front of every
panic and every slow path; no value profiles on statically typed data. The
only value profiles are the type-identity caches of §4.6.

### 4.3 Root nodes

```java
public final class GoFuncRootNode extends RootNode {          // no defers
    @Child private PrologueNode prologue;                      // args → slots, cells for captured params
    @Child private StmtNode body;
    @Child private ResultNode result;                          // reads the result slot(s), boxes at the boundary
    @Override public Object execute(VirtualFrame frame) {
        prologue.executeVoid(frame);
        try { body.executeVoid(frame); } catch (ReturnException r) { /* singleton, no payload */ }
        return result.executeGeneric(frame);
    }
}
public final class GoDeferFuncRootNode extends RootNode { … }  // §4.5
```

- `super(language, descriptor)` with the real language. `getName()` returns
  gc's symbol name (`pkg.F`, `pkg.(*T).M`, `pkg.F.func1`),
  `getQualifiedName()` the same, `getSourceSection()` the function's section,
  `isInternal()` true for runtime-generated wrappers (method-value thunks,
  goroutine entry, package init driver).
- No per-call work that is not the function's own: no safepoint call (§2.7),
  no `G` lookup, no shadow-stack push (§6.5), no `try`/`catch` for panics
  unless the function has a `defer`.
- Results: one result of a primitive carrier is returned boxed (the `Object`
  return of `execute` forces it; inlining removes the box). Multiple results
  are returned as an `Object[]` tuple created by `ResultNode`; the call node
  unpacks it into the destination slots. Escape analysis removes the tuple
  when the callee is inlined.
- `isCloneUninitializedSupported()`/`cloneUninitialized()` are implemented by
  asking the `FunctionUnit` to decode the body again (§4.7).
- `prepareForInstrumentation` is where a function with elided statement
  wrappers materializes them (§6.3).

### 4.4 Variables that escape: cells, closures, pointers

The frontend flags every local that is captured by a closure or has its
address taken (toolexec design §3.1). Such a variable lives in a **typed
cell** allocated at its declaration; its frame slot holds the cell.

```java
public final class I64Cell  { public long value; }      // also I32Cell, F64Cell, F32Cell, BoolCell
public final class RefCell  { public Object value; }
```

- A closure value is `GoFunc { RootCallTarget target; Object[] captures; }`
  where `captures` holds the cells (shared, by-reference capture as Go
  specifies) in the order of the IR's capture list. The callee prologue copies
  them into its own frame slots with `@ExplodeLoop`. Per-iteration loop
  variables (Go 1.22 semantics) are simply a fresh cell per iteration,
  allocated by the loop body's declaration node; the "snapshot slot" machinery
  of today's root node disappears.
- `frame.materialize()` is banned in Go nodes (rule F2). Graal's partial
  escape analysis removes cells and closure objects that do not escape the
  compilation unit — which, after inlining a callee that takes a `func`
  argument, is the common case.
- A top-level function referenced as a value is a preallocated `GoFunc` with
  `captures == null`; a method value is a `GoFunc` whose target is a thunk
  root and whose single capture is the receiver.

**Pointers** (replacing the five-mode `GoPointer`, H19):

| Pointer to | Representation |
|---|---|
| a struct (`*T`, T a struct type) | the `GoStruct` reference itself. Nested struct values and struct elements of arrays/slices are their own `GoStruct` objects in the parent's `Object[]`, so `&x.inner` and `&s[i]` are also plain references. |
| a variable of non-struct type | its cell (`I64Cell`, …) |
| an element of an array or slice of non-struct type | `ElemPtr { Object array; int index; }` |
| a non-struct field of a struct | `FieldPtr { GoStruct s; int index; }` |

The pointee *type* is static, the pointer *shape* is not, so load and store
through `*p` for non-struct pointees are DSL nodes with one specialization per
shape — the one place outside §4.6 where a node has several specializations:

```java
@NodeChild(value = "ptr", type = RefNode.class)
abstract class LoadI64Node extends I64Node {
    @Specialization static long cell(I64Cell c)  { return c.value; }
    @Specialization static long elem(ElemPtr p)  { return ((long[]) p.array)[p.index]; }
    @Specialization static long field(FieldPtr p) { return p.s.longs[p.index]; }
    @Fallback static long nil(Object p, @Bind Node n) { throw GoPanics.nilDeref(n); }   // p == null
}
```

Pointer equality is reference equality for structs and cells and
`(array, index)` / `(struct, index)` equality for the other two; the factory
knows from the type which comparison node to build, and the comparison node
specializes on shape like the loads.

This is a value-model change within the latitude of SPEC §3.4 ("Go pointer =
opaque Java reference"); the final shape is owned by whoever implements W4 of
the toolexec design. The requirements from this document are: final,
single-purpose classes; typed cells; no static hooks.

### 4.5 Calls, the calling convention, defers and panics

**Convention.** `arguments[0]` is the callee's `GoFunc` (for its captures;
`null` for a statically called top-level function), `arguments[1..]` are the
Go arguments in canonical carrier form, boxed. A method's receiver is the
first Go argument. Interface arguments are passed as interface values; there
is no caller- or callee-side re-wrapping by runtime kind test (H7): the
factory inserts `ToIfaceNode` where the *static* types require it.
(If P-B fails, `arguments[0]` becomes the `G` and the function value moves to
`arguments[1]`; §2.7.)

**Static calls** (callee known to `go/types`: package-level functions,
methods on concrete types — the large majority):

```java
final class CallNode extends … {
    @Children private final ExprNode[] args;
    @Child private DirectCallNode call;            // created on first execution
    private final FunctionUnit callee;             // lazily decoded (toolexec design §4.3)
    @ExplodeLoop Object call(VirtualFrame f) {
        if (call == null) { CompilerDirectives.transferToInterpreterAndInvalidate();
                            call = insert(DirectCallNode.create(callee.callTarget())); }
        Object[] a = new Object[args.length + 1];
        for (int i = 0; i < args.length; i++) a[i + 1] = args[i].executeGeneric(f);
        return call.call(a);
    }
}
```

`[PROTO]` this shape inlines and compiles recursive `fib`. Typed variants
(`CallI64Node`, …) unbox the result with a cast, not `((Number) r).longValue()`.
Value-typed struct and array arguments are copied by a `StructCopyNode`
inserted by the factory at the call site.

**Dynamic calls** (func values): §4.6.

**Open-coded defers.** A function containing `defer` is built as a
`GoDeferFuncRootNode`:

- Each `defer` statement that is not inside a loop gets an index *i*, frame
  slots for its callee and evaluated arguments, and bit *i* in an `int`
  "defer bits" slot. Executing the statement evaluates callee and arguments
  into the slots and sets the bit.
- The root's exit sequence runs, in reverse index order with `@ExplodeLoop`,
  `if (bits & (1 << i)) != 0 { clear bit; call }`. A deferred call to a
  statically known function is a `DirectCallNode` and inlines:
  `defer mu.Unlock()` compiles to the unlock.
- A `defer` inside a loop (or more than 32 sites) appends a record to a
  per-frame list held in a frame slot; the exit sequence drains it first.
- The root wraps the body in `try { … } catch (PanicException p)`; on panic it
  marks `G.panicking`, runs the same exit sequence with recovery enabled, and
  either rethrows or — if a deferred call recovered — returns the named
  results. `Goexit` unwinding runs the exit sequence and rethrows.
- `recover()` reads `G` through `M` (§2.5). It is valid only when called
  directly by a deferred function during panicking; the root sets the
  eligibility flag around each deferred call, as today, but only on the panic
  path.
- Range-over-func bodies that `defer` attach to the enclosing function's list
  through a cell passed to the synthesized yield closure (gc's
  `deferrangefunc`), replacing the "range stash slot".

Functions without `defer` have no handler and no `G` access at all. This is
gc's own design since Go 1.14 and removes `DeferChain`'s `ArrayDeque` (H15)
from the call path.

**Panics.** `PanicException extends AbstractTruffleException` (as today) and
is constructed with the throwing node as location so that Truffle attaches the
guest stack lazily (§6.5). Run-time panics (nil dereference, index out of
range, division by zero, failed type assertion) are created by
`@TruffleBoundary` factory methods on `GoPanics` called behind an
`InlinedBranchProfile`. `Goexit`, `ReturnException`, `BreakException`,
`ContinueException` are `ControlFlowException`s.

Host `StackOverflowError` and `OutOfMemoryError` are never caught by Go nodes
(§9).

### 4.6 The four dynamic sites

These are the sites where run-time information beats the static type, and the
only ones with `@Cached` inline caches (SPEC §3.3).

**(a) Interface method call — the central one.**

An interface value is `GoIface { GoItab itab; Object data; }` for non-empty
interface types and `GoEface { GoType type; Object data; }` for `any`
(SPEC §3.4: "(dynamic type descriptor, data) + itab"). A `GoItab` is immutable:
`{ GoType dynamicType; GoType ifaceType; @CompilationFinal(dimensions = 1)
GoFunc[] methods; }`, with `methods` in the interface's method order, so a
call site holds a constant **method index** and never a name. Itabs are
interned in the language-level cache (§2.4).

```java
@ReportPolymorphism
abstract class IfaceCallNode extends ExprNode {
    final int methodIndex;
    @Specialization(guards = "recv.itab == cachedItab", limit = "3")
    Object cached(VirtualFrame f, GoIface recv, Object[] args,
                  @Cached("recv.itab") GoItab cachedItab,
                  @Cached("create(cachedItab.methods[methodIndex].target)") DirectCallNode call) {
        return call.call(pack(cachedItab.methods[methodIndex], recv.data, args));
    }
    @Specialization(replaces = "cached")
    Object megamorphic(VirtualFrame f, GoIface recv, Object[] args, @Cached IndirectCallNode call) {
        GoFunc m = recv.itab.methods[methodIndex];
        return call.call(m.target, pack(m, recv.data, args));
    }
}
```

- The guard is a reference comparison against a PE constant; itabs are
  immutable, so no `Assumption` is involved (SPEC §3.3).
- A nil interface receiver panics before dispatch (`recv == null` behind a
  branch profile).
- The megamorphic case is an array load and an indirect call — no hash lookup
  (SPEC §3.3 allowed one; the method-index layout makes it unnecessary).
- Where the value was converted to the interface in the same compilation unit
  (`ToIfaceNode` with a constant itab, callee inlined), the guard folds, the
  method inlines and the `GoIface` box is scalar-replaced: SPEC §2.6.1's
  cascade, step by step.
- Methods promoted through embedded fields get a thunk target generated when
  the itab is built, so the call site never walks field paths (today's
  `isEmbeddedRedispatch` loop).

**(b) Func-value call.** `guards = "fn.target == cachedTarget"`, limit 3,
`DirectCallNode`; megamorphic through `IndirectCallNode`. `@ReportPolymorphism`.
Today's `CallDispatchNode` has the right cached half and the wrong megamorphic
half (H6).

**(c) Type assertion and type switch.**

- To a concrete type `T`: `eface.type == T` with `T` a PE constant (for
  `GoIface`: `itab.dynamicType == T`). No cache.
- To an interface type `I`: the answer depends on the dynamic type's method
  set, so the node caches `dynamicType → GoItab-or-failure`
  (`guards = "type == cachedType"`, limit 4) through the inlinable
  `ItabLookupNode`; the uncached path is a `@TruffleBoundary` lookup in the
  language-level itab cache.
- A type switch is compiled by the factory into a decision sequence: concrete
  cases as identity comparisons in source order, each with a counting
  condition profile; interface cases through `ItabLookupNode`. For switches
  with many concrete cases the node instead caches `dynamicType → case index`
  (limit 4) and jumps — the "decision tree + value profile" of SPEC §3.3.
  Type lists and method-name sets (H15) are resolved at build time into arrays
  of descriptors.

**(d) Maps.** `GoMap` remains the hand-written open-addressing table SPEC §3.4
requires. The key *kind* is static, so the factory chooses
`MapGetI64Node`, `MapGetStrNode`, `MapGetGenericNode` (and the corresponding
put/delete/range nodes). For integer and string keys the probe loop is plain
Java over the table's arrays and is PE-safe; hashing a string is a loop over
bytes in a helper marked `@InliningCutoff` for host inlining. The slow paths —
growth, rehash, generic-key hashing and equality (which may call Go
`==` on interface values and may panic on uncomparable types) — are behind
`@TruffleBoundary`. SPEC's "inline fast path guarded on table version" applies
to iteration: a range loop captures the table's version and takes the slow
path when it changes. The concurrent-write detector is a plain flag check
behind a branch profile.

### 4.7 Inlining, splitting and compilation knobs

- Inlining is Truffle's language-agnostic inliner over `DirectCallNode`s.
  Nothing is forced by default. `DirectCallNode.forceInlining()` is used only
  for generated thunks (method values, promoted-method wrappers, goroutine
  entry).
- `RootNode.isTrivial()` returns true for leaf functions the factory can see
  are a single expression (accessors); it biases the inliner.
- **Splitting.** When a callee is inlined, its inline caches are *shared* by
  every caller. `sort.Sort(data sort.Interface)` therefore sees every `data`
  type the program ever sorts at one `IfaceCallNode` and goes megamorphic for
  everyone. Truffle's answer is splitting: a call site gets its own copy of
  the callee when the callee reports polymorphism. For Go this is what carries
  SPEC §2.6.1 from user code into library code, so it is part of the design:
  `IfaceCallNode`, the func-value call node and the type-switch cache are
  `@ReportPolymorphism`; root nodes implement `cloneUninitialized()` by
  re-decoding the function body from GIR (function bodies are independently
  decodable blobs, toolexec design D5 — no AST cloning code is needed).
  Splitting starts **enabled with Truffle's default budget** and is evaluated
  on the interface benchmark (TR-4); `engine.Splitting=false` is the control.
- Functions that are too large to compile (generated tables, giant `init`s):
  blocks use Truffle's `BlockNode.create(elements, executor)` `[JAR]`, which
  lets the runtime compile a huge block in pieces.
- `@TruffleBoundary` rules are in §11 (B1–B6). In short: everything in the
  runtime library is a boundary unless it is on the PE-safe allow-list;
  nothing reachable from a node's fast path may allocate through a JDK
  collection, format a string, take a lock, park, or perform I/O without one.
- `@InliningCutoff` (`HostCompilerDirectives.InliningCutoff` `[JAR]`) marks
  slow-path helpers so the *host* compiler does not inline them into
  interpreter `execute` methods; it matters for interpreter speed on HotSpot
  and in native images, not for PE.
- `@ExplodeLoop` goes on every loop over `@Children` or over a
  `@CompilationFinal` array; `CompilerAsserts.partialEvaluationConstant(n)`
  asserts the bound. Loops over guest data are never exploded.
- Options used by gates, verified present on the pin `[JAR]`:
  `engine.CompilationFailureAction` (`ExitVM`/`Throw`),
  `engine.CompileImmediately`, `engine.BackgroundCompilation`,
  `engine.Compilation`, `engine.CompileAOTOnCreate`,
  `engine.TraceTransferToInterpreter`, `engine.TraceDeoptimizeFrame`,
  `engine.TraceAssumptions`, `engine.CompilationStatistics`. From the
  documentation, to be confirmed when the gate is built `[DOC]`:
  `engine.TraceCompilation`, `engine.TracePerformanceWarnings=all`,
  `engine.TreatPerformanceWarningsAsErrors=all`, `engine.TraceSplitting`,
  `compiler.TraceInlining`, `engine.SpecializationStatistics`.

### 4.8 Statements and control flow

| Construct | Design |
|---|---|
| block | `@Children StmtNode[]` with `@ExplodeLoop`; blocks above a size threshold use Truffle's `BlockNode` |
| `if` | `CountingConditionProfile` |
| `for`, `range` | `Truffle.getRuntime().createLoopNode(repeatingNode)` (OSR, back-edge counting, safepoint). One `RepeatingNode` class per loop form: condition loop, `range` over int, string (UTF-8 decode inline), array, slice (length captured once), map (iterator object, version check), channel, function (yield closure). `continue` is caught inside `executeRepeating`, `break` outside `loop.execute`. |
| `break`, `continue` | `ControlFlowException` singletons for the unlabeled forms; a labeled form throws an instance preallocated per label and the target compares by identity. The factory omits the `catch` on loops whose body contains no matching jump. |
| `return` | store into the typed result slot(s), then `throw ReturnException.INSTANCE`; a `return` in tail position of the function body compiles to the store alone (factory decision). No boxed payload. |
| `switch` | the factory resolves `fallthrough` statically by chaining case bodies — no `FallthroughSignal`. Integer and string tag switches become compare sequences with profiles; a dense integer switch may use a `@CompilationFinal` table and `switch` on a PE-constant index. |
| `goto`, labels | Go restricts `goto` to labels in an enclosing block, not jumping over declarations. A block that contains goto-target labels is built as a `GotoBlockNode`: its statements are split into segments at the labels and an `int pc` loop with `@ExplodeLoop(kind = MERGE_EXPLODE)` dispatches segments; a `goto` out of nested statements throws a per-label `ControlFlowException` caught by that block, which sets `pc`. Labels are indices, not strings. |
| `select` | evaluates channel and value operands, calls the runtime (`@TruffleBoundary`), then dispatches on the returned case index with an exploded loop over the case bodies |
| `go` | evaluates the function value and arguments, then a boundary call to the scheduler |
| `defer`, `panic`, `recover` | §4.5 |

## 5. Interop (SPEC §2.4, §4.3)

### 5.1 Principles

1. **Go nodes never call `InteropLibrary` on Go values.** Interop appears in
   exactly three places: the exports on Go value classes (code that runs only
   when a foreign caller or a tool sends a message), the intrinsic nodes
   behind the `graalgo/java` package, and the two conversion nodes at the
   boundary. Implementing `TruffleObject` is a marker and costs nothing.
2. Go values are exported with `@ExportLibrary(InteropLibrary.class)` on the
   value classes themselves. `DynamicObject`/`Shape` is not used: Go types are
   closed and static, and SPEC §3.4 fixes the struct layout. Polyglot
   `ProxyArray`/`ProxyObject`/`ProxyExecutable` are host-side adapters for
   *Java* objects that want to look like guest objects; a language exports
   messages instead. SPEC §4.3's table names them in its "Java" column; what a
   Java embedder actually receives is `Value`, and `value.as(List.class)` /
   `as(Map.class)` / `as(Function.class)` give the `List`/`Map`/lambda views
   the table promises (amendment A3, wording only).
3. Every value that leaves Go passes through `ToInteropNode`; every value that
   enters passes through `ToGoNode(targetType)`. Both are inlinable DSL helper
   nodes, specialized on the (static) Go type at the boundary, with
   `assert InteropLibrary.isValidValue(v)` on the way out.
4. Copy is the default; views are explicit (SPEC §4.3).

### 5.2 Messages per Go value kind

| Go value | Exported as | Semantics |
|---|---|---|
| `bool` | Java `Boolean` | value |
| `int8…int64`, `int`, `uint8…uint32` | `Byte`/`Short`/`Integer`/`Long` chosen so the numeric value is preserved (`uint32` → `Long`) | value |
| `uint`, `uint64`, `uintptr` | `Long` when the value fits; otherwise a `GoUint64` object exporting `isNumber`, `fitsInLong = false`, `fitsInBigInteger`, `asBigInteger`, `fitsInDouble` when exact | value; never a negative `long` |
| `float32`, `float64` | `Float`, `Double` | value |
| `complex64/128` | object with members `real`, `imag` | copy |
| `string` | `GoString` exports `isString`, `asString` (UTF-8 decode; invalid sequences become U+FFFD as in Go's `[]rune` conversion), `asTruffleString` | copy (immutable anyway) |
| struct value `T` | `GoStruct` exports `hasMembers`, `getMembers`, `isMemberReadable`, `readMember`; **snapshot**: the boundary copies the struct, and the copy's members are modifiable | copy (Go value semantics) |
| `*T`, T struct | the same exports on the shared object plus `isMemberModifiable`/`writeMember`, `isMemberInvocable`/`invokeMember` for the pointer method set | reference |
| struct members | exported fields and promoted fields by Go name; unexported fields only with `includeInternal` (`isMemberInternal`), for debuggers; methods as invocable members | field values pass through the conversion nodes |
| `[N]T` | `hasArrayElements`, fixed size | copy |
| `[]byte` | `hasArrayElements` **and** `hasBufferElements`/`readBufferByte`…; `value.as(byte[].class)` copies | SPEC: bulk copy by default; the buffer messages are the explicit view |
| other slices | `hasArrayElements`, `getArraySize`, `readArrayElement`, `writeArrayElement` (in `len`), not insertable/removable | view of the backing array; `java.Of` / the typed conversions copy unless the view is requested (§5.5) |
| `map[K]V` | `hasHashEntries`, `getHashSize`, `isHashEntryReadable/Modifiable/Insertable/Removable`, `readHashValue`, `writeHashEntry`, `removeHashEntry`, `getHashEntriesIterator` | view; keys converted with `ToGoNode(K)`; iteration order randomized per iterator as in Go. Maps never export members, also for string keys. |
| `func` | `GoFunc` exports `isExecutable`, `execute`, `hasExecutableName`, `getExecutableName`, `hasSourceLocation` | arguments converted per parameter type (signature from the target's root node), arity checked (`ArityException`); results per §5.3 |
| channel | opaque object with invocable members `Send`, `Recv`, `TryRecv`, `Close`, `Len`, `Cap` | handle (SPEC: never a `BlockingQueue`) |
| pointer to non-struct | opaque object with invocable members `Load`, `Store` | reference. Go pointers **do not** export `isPointer`/`asPointer`: those messages mean a native address, which a Go pointer does not have (GG-0015). Only cgo-pinned memory (SPEC §3.7) may. |
| interface value | unwrapped: the boundary exports the dynamic value; if the dynamic type is a named non-struct type with methods, a `GoBoxed` object that forwards the underlying value's messages and adds the methods as invocable members | per dynamic value |
| nil pointer, slice, map, chan, func, interface | `GoNil` (one per static type where the type is known, else the untyped singleton): `isNull = true` | |
| `error` returned across the boundary | §5.3 | |
| `unsafe.Pointer` | not exportable; conversion fails with a Go error | |

All exported objects answer `hasLanguage`/`getLanguage` (`GoLanguage.class`),
`toDisplayString` (Go's `%v`, behind a boundary), `hasMetaObject`/
`getMetaObject` (the `GoType` descriptor, which exports `isMetaObject`,
`getMetaQualifiedName`, `getMetaSimpleName`, `isMetaInstance`), and identity
(`isIdentical`/`identityHashCode`) for reference kinds. On the pin the
language messages are `hasLanguage`/`getLanguage` `[JAR]`; 25.1 replaces them
with `hasLanguageId`/`getLanguageId` `[DOC]`, which the `-Werror` build will
flag at the next pin bump.

Member lookup is cached on `(descriptor identity, member name)` with limit 3
and a `@TruffleBoundary` uncached path `[PROTO]`:

```java
@ExportMessage static final class ReadMember {
    @Specialization(guards = {"s.desc == cachedDesc", "member.equals(cachedMember)", "idx >= 0"}, limit = "3")
    static Object cached(GoStruct s, String member, @Cached("s.desc") StructDesc cachedDesc,
                         @Cached("member") String cachedMember,
                         @Cached("cachedDesc.find(cachedMember)") int idx) { … }
    @Specialization(replaces = "cached") @TruffleBoundary
    static Object uncached(GoStruct s, String member) throws UnknownIdentifierException { … }
}
```

### 5.3 Calls and errors across the boundary

**Host → Go.** `execute` on a `GoFunc`:

1. The calling thread is entered by the polyglot layer; its `M` exists or is
   created; a foreign `G` is installed if the thread has none (§2.6).
2. Arguments are converted by `ToGoNode` for each parameter's static type. A
   host object passed for an interface parameter becomes a `java.Value`
   (§5.5) boxed in that interface if the interface is `any` or is implemented
   by `java.Value`; otherwise `UnsupportedTypeException`.
3. The call is a `DirectCallNode`/`IndirectCallNode` as in the prototype.
4. Results: none → the language's null object; one → converted; several → an
   interop array. **A trailing `error` result is not returned**: `nil` is
   dropped, non-nil is thrown as `GoErrorException` (an
   `AbstractTruffleException` carrying the error value).

**Exceptions.**

| Go | Seen by the host |
|---|---|
| unrecovered `panic(v)` unwinding out of a host-initiated call | `PolyglotException` with `isGuestException()`, guest object = `v`, message = gc's rendering (`panic: …` body), guest stack frames from §6.5. `PanicException` exports `isException`, `throwException`, `getExceptionType = RUNTIME_ERROR`, `hasExceptionMessage`, `getExceptionMessage`, `hasExceptionStackTrace`. The context stays usable: a panic on a foreign thread does not kill the program, because there is no Go caller to crash. |
| non-nil `error` result | `PolyglotException`, guest object = the error value (members = its methods), message = `Error()`, `hasExceptionCause`/`getExceptionCause` = `Unwrap()` when present. This is SPEC §4.3's "`RuntimeException` proxy (value + message + Go stack)". |
| `os.Exit(n)` | `isExit()`, `getExitStatus() == n` (§2.3) |
| `runtime.Goexit` on a foreign thread | converted to a `GoErrorException`; `Goexit` never crosses the boundary as a `ControlFlowException` |
| fatal error (stack overflow, concurrent map write, deadlock) | context closed with exit status 2 |

**Go → host.** A host method call made through `graalgo/java` catches, at the
intrinsic node, any exception for which `InteropLibrary.isException` holds
(host exceptions and other languages' exceptions) and returns it as a Go
`error` of type `*java.Throwable` (§5.5). **It is never a Go panic** (SPEC
§2.4). Host `Error`s that are not exceptions in the interop sense
(`StackOverflowError`, `OutOfMemoryError`, internal errors) are not converted.

**Scheduler interplay.** A goroutine in a host call keeps its run token, like
a short cgo call. The call node sets `G.inHostCall` around the call (two
field writes); the monitor thread of §2.7 hands the token to another
goroutine if a host call outlasts the quantum — gc's `sysmon` retaking a P
from a long syscall. Host code that calls back into Go on the same thread
continues on the same `G`.

**Thread safety.** Views expose Go's own data-race rules to Java: unsynchronized
concurrent slice access is a race, concurrent map mutation is a fatal error.
The contract documents this per view (SPEC §4.3: "views are explicit and
documented per call").

### 5.4 The module object and the embedding API

`Context.eval(bundle)` returns the **module object**: members are the bundle's
packages by import path; each package object's members are its exported
package-level functions (executable), variables (read/write through the
context's global storage), constants, and types (meta objects; instantiable
for struct types: `instantiate()` returns `*T` zero value).

```java
try (Context ctx = Context.newBuilder("go")
        .allowCreateThread(true)                      // goroutines
        .allowHostAccess(HostAccess.SCOPED)           // SPEC §4.3 default
        .option("go.RunMain", "false")
        .build()) {
    Source bundle = Source.newBuilder("go", ByteSequence.create(bytes), "app.ggb")
                          .mimeType("application/x-graalgo-bundle").build();
    Value mod   = ctx.eval(bundle);
    Value pkg   = mod.getMember("example.com/geo");
    Value p     = pkg.getMember("NewPoint").execute(3, 4);     // *geo.Point
    long x      = p.getMember("X").asLong();
    double d    = p.invokeMember("Norm").asDouble();
    List<?> xs  = pkg.getMember("Primes").execute(100).as(List.class);   // slice view
    try { pkg.getMember("Parse").execute("bad"); }
    catch (PolyglotException e) { Value goErr = e.getGuestObject(); … }  // Go error
}
```

`getBindings("go")` returns the same module object for the context's bundle
(a context runs one bundle; evaluating a second bundle in the same context is
an error in v1). `TruffleLanguage.getScope` returns it for tools.

Typed Java access ("Go-from-Java is annotation/codegen-driven at build time",
SPEC §4.3): in JIT mode `value.as(MyInterface.class)` already works for any
Java interface whose methods match member names. For closed-world images the
build generates the interfaces and the proxy registrations from the bundle's
export list — a small generator in the link step, specified with TR-9, not
here.

### 5.5 The `graalgo/java` package (GG-0003)

An ordinary Go package shipped in graalgo's `GOROOT`
(`$GOROOT/src/graalgo/java`), whose body-less functions resolve to Java
intrinsics by the toolexec design's rule D9 — no name is special-cased in the
tool. Under stock gc the package does not link, which is the intended
"additive" behaviour.

```go
package java

// Value is a handle to a host (Java) or foreign polyglot object.
// The zero Value is Java null.
type Value struct{ ref any }

func Class(name string) (Value, error)         // Env.lookupHostSymbol; ErrHostAccess if not allowed
func Import(symbol string) (Value, error)      // polyglot bindings (Env.importSymbol)
func Export(symbol string, v any) error

func (v Value) IsNull() bool
func (v Value) Call(method string, args ...any) (Value, error)   // invokeMember
func (v Value) Invoke(args ...any) (Value, error)                // execute (lambdas)
func (v Value) New(args ...any) (Value, error)                   // instantiate
func (v Value) Get(member string) (Value, error)                 // readMember
func (v Value) Set(member string, x any) error
func (v Value) Len() (int, error)
func (v Value) Index(i int) (Value, error)
func (v Value) SetIndex(i int, x any) error
func (v Value) String() string                                   // toDisplayString

// Conversions follow SPEC §4.3: copy by default.
func Of(x any) Value                           // Go → Java, copy (string → java.lang.String, []byte → byte[], …)
func View(x any) Value                         // explicit view for slices and maps
func As[T any](v Value) (T, error)             // Java → Go, copy; T = string, []byte, []E, map[K]V, numeric, bool, Value
func Func[F any](v Value) (F, error)           // Java lambda / functional interface → Go func of type F

// Throwable is the Go error for a Java exception.
type Throwable struct{ … }
func (t *Throwable) Error() string             // class name + ": " + message
func (t *Throwable) Message() string
func (t *Throwable) JavaStack() []string
func (t *Throwable) Unwrap() error             // cause, as *Throwable, or nil
func (t *Throwable) Value() Value              // the exception object
```

- Each body-less function is an intrinsic node using
  `@CachedLibrary(limit = "3") InteropLibrary`; `Class` uses
  `env.lookupHostSymbol`, host objects are recognized with `env.isHostObject`
  and unwrapped only inside the runtime (`env.asHostObject`) where a Java type
  is required (`As[[]byte]` on a host `byte[]`).
- `As` and `Func` are generic functions; under the toolexec design's run-time
  instantiation (D6) each instantiation gets its own tree, so the target Go
  type is a PE constant in the conversion node. `Func[F]` builds a `GoFunc`
  whose target is a thunk that converts arguments, calls
  `InteropLibrary.execute` and converts the result; a Java exception becomes
  the trailing `error` result if `F` has one, else a panic with the
  `*Throwable` (the one place a Java exception can surface as a panic, because
  the Go signature gives it nowhere else to go; documented).
- Go `func` → Java: `java.Of(f)` is the `GoFunc` itself; `HostAccess` lets the
  host coerce an executable to a functional interface.
- Typed bindings (`graalgo java-bind java.util.ArrayList` generating Go
  wrapper types over `Value`) are build-time code generation and need no
  runtime support beyond the above; later work.

### 5.6 Host access policy

SPEC §4.3: "Host access defaults to `SCOPED`-like scoping, never `ALL`."
`HostAccess.SCOPED` is explicit-annotation access plus scoped callback
parameters: a Go program could then call only methods annotated
`@HostAccess.Export`, which no JDK class is. Proposed reading (amendment A4):

- **Embedded** (`Context` created by a Java application): the embedder's
  `HostAccess` is used as given; the documented recommendation and the
  default in graalgo's own embedding helpers is `SCOPED`. `java.Class` fails
  with `ErrHostAccess` unless `allowHostClassLookup` permits the class.
- **Standalone** (`go run`, a built executable): the program is as trusted as
  any Go binary. If and only if the bundle's manifest records an import of
  `graalgo/java`, the launcher enables host class lookup and a `HostAccess`
  built from `SCOPED` with public access to an allow-list of packages
  (default `java.*`, `javax.*`; extended by a launcher flag). Programs that do
  not import the package get no host access at all.

This needs the owner's decision because it relaxes "never `ALL`" for
standalone programs in substance if not in name.
