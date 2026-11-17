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
