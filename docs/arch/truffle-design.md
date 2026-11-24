# Truffle modernization — design and migration plan (lane T2 design)

**Status:** design, decision-complete except where §13 says a prototype must
run first. Written 2026-11-17 for track T2 of [RECOVERY.md](RECOVERY.md), with
inputs to T0.B, T1 and T3. [SPEC.md](SPEC.md) is authoritative; this document
implements SPEC §2.4–2.7, §3.3, §3.4, §3.10, §3.11 and §4.3 and requirements
GG-0003/0005/0006/0008/0014/0015/0024/0025/0049/0050. Where it disagrees with
SPEC it says so and lists the disagreement in §14 as a proposed amendment;
nothing here overrides SPEC by itself. Three such disagreements sit inside
decisions T4 and T7 and are marked where they occur: **A7** (safepoint
preemption instead of `Safepoint.poll()`), **A8** (open-coded defers) and
**A9** (closures over typed cells instead of a `MaterializedFrame`). The owner
decided on 2026-11-18 ([ADR-0012](../dev/adr/ADR-0012-open-coded-defers.md)):
**A8 accepted** (SPEC §3.5 amended; requirements re-issued 2026-11-18 as GG-0070/GG-0071, locked on owner sign-off the same day); **A7 accepted conditionally
on P-B** `[OPEN]`, then **REJECTED** after P-B failed ([ADR-0013](../dev/adr/ADR-0013-a7-rejected.md); SPEC unchanged); **A9 deferred pending
benchmarks** (SPEC unchanged). Where a section below says "until accepted,
implementation follows SPEC", that applies to A7 and A9 only (§14.1 has the
trade-offs).

It was written against the code at `a0df481b` and against the pinned Truffle
jars in [internal/backend/truffle/lib/](../../internal/backend/truffle/lib/)
(25.0.4.1.1). The companion design for the build path is
[toolexec-design.md](toolexec-design.md) (referred to below as "the toolexec
design"). The owner decided on §14's items on 2026-11-18; §14 records the
outcome of each.

**Audit (2026-11-24):** [truffle-audit.md](truffle-audit.md) checks this
design and the as-built node package (§15, §16) against the owner's Truffle
playbook; its findings and work packages (WP-TR-AUDIT-n) supersede the
ordering of §10.2 where they differ.

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
| T1 | One `GoLanguage extends TruffleLanguage<GoContext>`, id `go`, registered for the byte MIME type `application/x-graalgo-bundle`. `parse()` takes a **linked bundle** (toolexec design §4.2), never Go source text. | The frontend is `go/types` in a Go process (GG-0069); the JVM only ever sees typed IR. |
| T2 | `contextPolicy = SHARED`. The AST, type descriptors, itabs, function units and the intrinsic table belong to the language instance; globals, package-init state, scheduler, timers, OS state and the unsafe handle table belong to `GoContext`. No node holds context state. | Required for multi-context engines, image-heap pre-initialization and engine caching; single-context runs lose nothing because `ContextReference.get(node)` folds to a constant on a bound engine. |
| T3 | Goroutines stay virtual threads (SPEC §3.5) but are created with `Env.newTruffleThreadBuilder(r).virtual(true)`, and the goroutine ↔ thread binding goes through one `ContextThreadLocal`. A context option selects platform threads instead. | A thread that runs guest code must be entered in the context. `[PROTO]` it works on the pin; it is experimental upstream and ~10× more expensive to create than a bare virtual thread, so §2.6 adds thread recycling. |
| T4 | **REJECTED (2026-11-18, [ADR-0013](../dev/adr/ADR-0013-a7-rejected.md)): prototype P-B failed, so SPEC AMENDMENT A7 is not adopted; `Safepoint.poll()` stays and SPEC is unchanged. The proposal below is kept as the record; the poll's cost is WP-POLLCOST (RECOVERY.md). (Was: accepted conditionally on P-B `[OPEN]`: the owner switches only if P-B validates.)** (GG-SCHED-0001, GG-0017, SPEC §3.5 require `Safepoint.poll()` at loop back-edges and function entries; §14.1). Proposal: preemption moves from `ThreadLocal` + `System.nanoTime()` at every back-edge to **Truffle safepoints**: a monitor thread submits a `ThreadLocalAction` to a goroutine whose quantum expired; the action performs the quantum park. | Compiled loops carry no scheduler code at all; debugger suspend, cancellation and stack sampling use the same mechanism. `[PROTO]` delivered and parked on virtual threads. Fallback in §2.7. |
| T5 | Every node class is written against the conventions of §11: statically typed expression families, one small DSL node per operation and carrier, `@Cached` inline caches only at the four genuinely dynamic sites of SPEC §3.3, `@TruffleBoundary` on every runtime entry that is not PE-safe. | This is what makes PE succeed by construction instead of by repair. `[PROTO]` the pattern compiles with the pinned DSL processor and JIT-compiles. |
| T6 | The node factory, not the DSL, selects operations by type. Sized integers are carried as `int` (≤ 32 bit) or `long` (64 bit) in canonical extended form; narrowing wrap is a separate tiny node. | Go types are static; no type discovery is needed and no boxing or specialization explosion occurs. |
| T7 | **SPEC AMENDMENT A9 — deferred pending benchmarks (owner, 2026-11-18); SPEC stays as is, implementation follows SPEC** (SPEC §3.4: closures are `{RootCallTarget, MaterializedFrame\|null}`): the proposal is that closures capture **typed cells**, never a `MaterializedFrame`. **SPEC AMENDMENT A8 — ACCEPTED 2026-11-18** (GG-0071 draft, SPEC §3.5, [ADR-0012](../dev/adr/ADR-0012-open-coded-defers.md)): defers are **open-coded per function** in frame slots when every defer is outside loops and range-over-func bodies and they number at most *L*; otherwise the function uses the explicit per-goroutine defer chain; on panic/Goexit each frame runs its own pending defers. Not affected by either: the per-call shadow stack is deleted. §14.1. | Each of the three currently forces heap allocation or thread-local access on every call. |
| T8 | Go values export `InteropLibrary` directly from the value classes (`@ExportLibrary`); the hot path never dispatches through a library. Polyglot `Proxy*` classes are not used for Go values. | Zero cost when unused; gives `Value`, tools and other languages one contract. |
| T9 | Stack traces (`panic` output, `runtime.Callers`) come from `TruffleStackTrace` / `iterateFrames`; the `rtstack` bridge and `GoStackFrame` chain are deleted. | Truffle already records call nodes lazily; the shadow stack allocates per call. |
| T10 | `go test -cover` keeps the stock `cover` source rewriting (toolexec design D2). Truffle's coverage instrument is an extra, not the implementation. | Drop-in by construction; profile format identical. |
| T11 | Stay on the AST interpreter now; decide on the Bytecode DSL at a named gate after toolexec M1d with the bake-off of §8. Node conventions are chosen so operations port. | Bytecode DSL is experimental in 25.0 and would contradict the letter of SPEC §3.3; it is also the only structural answer to interpreter speed, footprint and recursion depth. |
| T12 | Stack overflow is a fatal error (gc semantics), reported from a fresh thread; the depth limit is raised first by compiled code and a large carrier stack, measured before anything structural is built. | §9. |
| T13 | The new node factory of toolexec M1a targets a **new node package** written to §11; the existing ~110 classes are the semantic reference and are deleted at cutover (M3). | Otherwise the new path inherits every pattern of §1.2 and needs a second rewrite. **Accepted 2026-11-18**; applied as toolexec design D12 (§4.3, W4, M1a +6–10 agent-days, M3). |

### 0.2 Findings that need the owner's attention

1. **Virtual threads under a polyglot context are experimental on HotSpot and
   emulated by platform threads in native images** `[DOC]` (Truffle changelog
   24.1, GR-40931; no later entry changes it up to 25.x). In the AOT and hybrid
   modes one goroutine would be one OS thread. This touches GG-0006/0049/0050
   and SPEC §3.5; see §7.4. *Status 2026-11-18:* measure first — SPEC
   carries it as `[OPEN]` pending P-F (§13.2).
2. **Mode (b) "pure AOT" runs the Go program in the Truffle interpreter.**
   Native Image compiles the interpreter ahead of time, not the guest program.
   GG-0050 ("performant without the runtime JIT") is therefore a statement
   about interpreter speed; see §7.3 and the measurement in §13.2 (P-D).
   *Status 2026-11-18:* measure first — SPEC §3.9 carries it as `[OPEN]`
   pending P-D.
3. **Creating a goroutine through the polyglot thread API cost 20–32 µs against
   ~2 µs for a bare virtual thread** `[PROTO]` (one box, under load, `nice 19`).
   §2.6 has the mitigation; it needs its own measurement (P-A).
4. SPEC §4.3's host-access default (`SCOPED`, never `ALL`) makes the
   `graalgo/java` package unable to call unannotated JDK classes; §5.6's split
   default was **decided** 2026-11-18 (SPEC §2.4/§4.3).
5. The devirtualization thesis of SPEC §2.6.1 needs **call-target splitting**
   for library code (§4.7); inlining alone shares one inline cache among all
   callers.

### 0.3 What the prototype established

A 500-line standalone language (not in the repository; §13.1) compiled with
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
  [Main.java](../../internal/backend/truffle/src/main/java/graalgo/launcher/Main.java)).
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
| H1 | No `@TruffleBoundary` anywhere; nodes call straight into `java.util` collections, string formatting, locks and I/O | all of `truffle/src/main/java/graalgo/runtime/`, `interop/src/main/java/graalgo/p3*/` | PE inlines the JDK until "Too deep inlining"; every function bails out | §11 rule B1 |
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
does not block, panic or spawn never touches `G`**. (Open-coded defers are accepted
(A8, GG-0071); safepoint preemption was amendment A7, rejected ([ADR-0013](../dev/adr/ADR-0013-a7-rejected.md)):
`Safepoint.poll()` of SPEC stays, and this property holds minus the poll. Functions whose defers fall back to the chain — in a
loop or over the limit — do touch `G`, on the defer path only.)

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

Decision T4 — **SPEC AMENDMENT A7 — REJECTED 2026-11-18** ([ADR-0013](../dev/adr/ADR-0013-a7-rejected.md)). P-B (`wt/pb/safepoint`) failed: a park inside a `ThreadLocalAction` from compiled code is pinned to its carrier (94 `jdk.VirtualThreadPinned` events against 0), 3,000 CPU-bound goroutines never finish, F1 with 10,000 spinners hangs, Truffle's mount hook recursed, counted loops lose the safepoint under the community compiler configuration, and an unwind-and-resume variant never preempts a compiled `for {}`. `Safepoint.poll()` stays (SPEC, GG-SCHED-0001 and GG-0017 unchanged). Everything in this section from "`Safepoint.poll()` as a Go-level node disappears" through the fallback paragraph describes the rejected proposal and is kept as the record; only the `setBlocked…` park helper and the missing-poll gate survive, as the residual of TR-6 (§10.2). Re-test triggers: ADR-0013. Original text, now historical: GG-SCHED-0001 and GG-0017 (locked) and
SPEC §3.5 make `Safepoint.poll()` at loop back-edges and function entries
the preemption mechanism. Because A7 is rejected, the new node package
emits that poll (in its TR-1 form: countdown in the context thread-local
slot, §13.4), and only the parts of this section that do not replace it —
parks through `setBlocked…`, the missing-poll gate — are built (the
`PinLock` check stays in the poll, so `setAllowActions` pin sections are moot); the monitor +
`ThreadLocalAction` scheme ran only behind a switch for P-B and is not merged. Trade-offs: §14.1.

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

**Fallback** (P-B failed; WP-POLLCOST in RECOVERY.md chose a third form, the shared poll epoch of §15 — one load and one compare, no hidden argument): an explicit flag. Every Go call passes the current
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
  code; nodes do not catch it (the `catch` blocks around frame reads in
  today's root nodes go away). `[JAR]` Truffle also offers *static* slots
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
  stores a frame in a heap object. (Depends on A9, deferred pending benchmarks: until it is decided,
  a closure's captured environment is the `MaterializedFrame` of SPEC §3.4,
  the one permitted materialization.)

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

**SPEC AMENDMENT A9 — deferred pending benchmarks (owner, 2026-11-18); implementation follows SPEC** for the closure representation below: SPEC §3.4 states
closures as `{RootCallTarget, MaterializedFrame|null}`; until A9 is decided
the new node package builds that shape (captured variables still get typed
cells, which SPEC does not constrain). The **pointer table** further down
predates the flat value model (GG-0072, re-issued from `GG-0072`) and GG-0062 (2026-11-18) and is superseded by them: structs and
arrays are flat, and a pointer is (referent, gc byte offset) with leaf
tables (SPEC §3.4, toolexec design §3.4); the table is kept for the
reasoning about pointer shapes only. Trade-offs: §14.1.

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

**Open-coded defers** — **SPEC AMENDMENT A8, ACCEPTED 2026-11-18**
([ADR-0012](../dev/adr/ADR-0012-open-coded-defers.md); SPEC §3.5 amended;
requirements GG-0070 (semantics) and GG-0071 (this mechanism), locked
2026-11-18 on owner sign-off, supersede `GG-0067`, which superseded `GG-0019`). The semantics are those of the Go
spec and gc and are the same on both paths — LIFO order, argument evaluation
at the `defer` statement, `recover` eligibility, `Goexit`, panics in deferred
calls, range-over-func, `runtime.Caller` — and the tests are shared. A
function containing `defer` is built as a `GoDeferFuncRootNode`:

- Choice is per function, never per statement, so open-coded and chained
  defers never interleave (gc's rule): if every `defer` of the function is
  outside any loop and the function is under the open-coded limit, all of its
  defers are open-coded; otherwise all of them use the chain. Each open-coded
  `defer` gets an index *i*, frame slots for its callee and evaluated
  arguments, and bit *i* in an `int` "defer bits" slot. Executing the
  statement evaluates callee and arguments into the slots and sets the bit.
  The limit *L* is the maximum number of `defer` statements of an
  open-coded function: a design parameter (the `int` bits slot allows 32; gc
  uses 8 plus a returns × defers ≤ 15 bound, which graalgo does not need
  because the root has one shared exit sequence), fixed in the node package
  and pinned by a boundary test at *L* and *L*+1, never by a function or
  package name. The choice is unobservable (gc probe,
  [traceback-normalization.md](traceback-normalization.md) §1), so *L* is a
  performance parameter.
- The root's exit sequence runs, in reverse index order with `@ExplodeLoop`,
  `if (bits & (1 << i)) != 0 { clear bit; call }`. A deferred call to a
  statically known function is a `DirectCallNode` and inlines:
  `defer mu.Unlock()` compiles to the unlock.
- **Fallback, the explicit per-goroutine defer chain** (the mechanism of the superseded `GG-0019`,
  retained): a function with a `defer` in a loop, over the limit, in a
  range-over-func body or otherwise not statically lowerable appends records
  to the goroutine's chain (reached through `G`, §2.5) and drains its own
  records at exit. **Unwinding is per frame:** when a panic or `Goexit`
  unwinds through a frame, that frame runs its own pending defers LIFO — an
  open-coded frame from its bits, by its own handler; a chained frame from
  its records on the chain — with `G`'s panic state (current panic,
  recovered flag, `Goexit` flag) as the single source of truth for `recover`
  and nested panics. The chain is not the unwinding path of open-coded
  frames (the superseded `GG-0067`'s "the chain SHALL also be the unwinding path" said
  otherwise; GG-0071 corrects it).
- The root wraps the body in `try { … } catch (PanicException p)`; on panic it
  marks `G.panicking`, runs the exit sequence with recovery enabled, and
  either rethrows or — if a deferred call recovered — returns the named
  results. `Goexit` unwinding runs the exit sequence and rethrows.
- `recover()` reads `G` through `M` (§2.5). It is valid only when called
  directly by a deferred function during panicking; the root sets the
  eligibility flag around each deferred call, on the panic path only.
- Range-over-func bodies that `defer` attach to the enclosing function's
  chain through a cell passed to the synthesized yield closure (gc's
  `deferrangefunc`), replacing the "range stash slot"; a function with such a
  defer is chain-only.
- `runtime.Caller`/`Callers` inside a deferred call, during recovery and
  after unwinding report the frames gc reports, compared as defined in
  [traceback-normalization.md](traceback-normalization.md) (GG-0070):
  `runtime.gopanic` between a deferred call run by a panic and the panicking
  frame, `runtime.Goexit` likewise; open-coding must not add or remove a
  frame (the exit sequence is not a Go frame).
- Verification (GG-0071): boundary tests at *L* and *L*+1 defers, a defer in
  a loop and one in a range-over-func body, each asserting which path the
  root took; every defer/panic/recover/Goexit corpus program runs twice —
  open-coded, and with a test-only switch that forces every defer onto the
  chain — with identical output, so each path can fail on its own; plus a
  compiled-graph test that a function with no or only open-coded defers has
  no `G` access and no allocation on the non-panic path.

Functions without `defer` have no handler and no `G` access at all, and
functions whose defers are all open-coded touch `G` only on the panic path.
This is gc's own design since Go 1.14 and removes `DeferChain`'s `ArrayDeque`
(H15) from the call path.

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
   the table promises (amendment A3, wording only; still pending — SPEC
   §4.3 notes it).
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
| pointer to non-struct | opaque object with invocable members `Load`, `Store` | reference. Go pointers **do not** export `isPointer`/`asPointer`: those messages mean a native address, which a Go pointer does not have (GG-0072). Only cgo-pinned memory (SPEC §3.7) may. |
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

**Decided 2026-11-18** (amendment A4, now SPEC §2.4/§4.3). The former SPEC
text ("defaults to `SCOPED`-like scoping, never `ALL`") would have allowed a Go
program to call only methods annotated `@HostAccess.Export`, which no JDK class
is, because `HostAccess.SCOPED` is explicit-annotation access plus scoped
callback parameters. The decided policy:

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

This relaxes "never `ALL`" for standalone programs in substance if not in
name; the owner accepted that because a standalone Go program is as trusted as
any Go binary, and access is opt-in through the import.

## 6. Instrumentation and tooling (GG-0024, GG-0008)

Everything in this section is unlocked by §2 (a registered language executing
inside a context) plus correct tags and source sections. None of it works
today, because the nodes belong to no language.

### 6.1 Jars to add

[lib/](../../internal/backend/truffle/lib/) holds only the language API,
runtime and DSL processor ([LIB.md](../../internal/backend/truffle/lib/LIB.md)).
The tools are separate Maven artifacts on the same version as the pin; TR-0
adds them with checksums under the existing lockstep rule: the Chrome
inspector, the profiler (CPU sampler, CPU tracer, memory tracer), the coverage
tool, the DAP server, Insight, their shared JSON dependency, and the polyglot
TCK for tests. The exact coordinates (`org.graalvm.tools:*-tool`,
`org.graalvm.sdk:polyglot-tck`, `org.graalvm.truffle:truffle-tck*`) are to be
confirmed against Maven Central when they are fetched; they were not
verifiable offline. **TR-0 (2026-11-18):** confirmed on Central at 25.0.4.1.1
and added: `profiler-tool`, `chromeinspector-tool`, `dap-tool`,
`coverage-tool`, `insight-tool`, `org.graalvm.shadowed:json`,
`org.graalvm.sdk:polyglot-tck`; the JUnit-based `truffle-tck*` artifacts are
deferred to TR-8 ([LIB.md](../../internal/backend/truffle/lib/LIB.md)).

### 6.2 Source sections

- One `Source` per Go file, created by the loader from the bundle's file table
  (trimpath-applied names, toolexec design §3.2), language `go`, content
  loaded lazily from disk; when the file is gone the section is still valid as
  line/column without text.
- Nodes do not store a `SourceSection` object (H21). `GoNode` stores a packed
  `int` (index into the function's position table: line, column, length) set
  by the factory at construction, and `getSourceSection()` builds the section
  on demand from the root's `Source`. Nodes without a position return `null`
  and tools use `getEncapsulatingSourceSection()`.
- Positions come from `go/token` via GIR and must be the positions gc reports:
  the *call* expression's position for a call node (what `runtime.Caller`
  reports), the statement's first token for statements. The panic-trace
  differential test of TR-7 is the oracle.
- `//line` directives are honoured by the frontend before positions are
  emitted (they change what gc reports, so they are part of drop-in).

### 6.3 Tags and wrappers

| Tag | Nodes |
|---|---|
| `RootTag` | the function root's body wrapper node |
| `RootBodyTag` | the user-visible body (after the prologue), so "step into" lands on the first statement |
| `StatementTag` | every Go statement node (simple statements, `if`/`for`/`switch` headers, `return`, `defer`, `go`) |
| `CallTag` | every call node (static, func-value, interface, `go`, `defer` at the point the deferred call runs) |
| `ExpressionTag` | expression nodes with a source position; off the hot path because wrappers exist only when an instrument asks |
| `ReadVariableTag` / `WriteVariableTag` | local and global variable nodes, with `getNodeObject()` giving the name |

Mechanics: `StmtNode` and `ExprNode` implement `InstrumentableNode` and are
annotated `@GenerateWrapper`; `isInstrumentable()` is `position != NONE`;
`hasTag` is a table lookup on a per-class tag mask. `@GenerateWrapper` generates a wrapper that delegates every `execute*`
method of the annotated class, so each typed family base class gets its own
wrapper and keeps its primitive return type. Wrappers are inserted
lazily by the instrumentation framework: an uninstrumented program has none,
so tags cost one `int` per node class.

Statement tagging is what `go test -cover` would need if it used Truffle; it
does not (T10). Truffle's `--coverage` then works as a free extra and as a
cross-check of the stock tool's counts.

### 6.4 Debugger (Chrome inspector, DAP)

With the inspector jar on the path the engine options `inspect` (CDP, SPEC
§3.10) and `dap` (Debug Adapter Protocol, i.e. VS Code and GoLand-style
clients; not required by GG-0024 but free) become available; the launcher
forwards `GRAALGO_INSPECT=port` / `-inspect` to them. What the language must
supply beyond tags:

- **Scopes.** Instrumentable nodes export `NodeLibrary` (`hasScope`,
  `getScope`): a scope object whose members are the Go locals visible at that
  node, built from the frame descriptor's slot names plus the factory's
  lexical-block table; cells are dereferenced transparently; `hasRootInstance`
  is false; the receiver is an ordinary named parameter.
- **Language views.** `getLanguageView(context, value)` wraps primitives so the
  debugger prints them as Go would: a `long` slot holding a `uint64`, an `int`
  holding a `uint8` or a `rune`. The view object gets the static type from the
  scope (the value alone cannot say, H18).
- **Meta objects.** `GoType` descriptors (§5.2).
- **Goroutines.** Each goroutine thread appears as a debugger thread. With
  recycling (§2.6) the thread name is updated to `goroutine N` when a `G` is
  installed.
- **Asynchronous stacks.** `RootNode.findAsynchronousFrames(frame)` `[JAR]` on
  the goroutine-entry root returns the `go` statement's stack, which is gc's
  "created by …" line and the inspector's async call stack.

Risk: the virtual-thread limitation of §2.6 ("access to caller frames in write
or materialize mode") is exactly what a debugger does to show and modify
variables in frames below the top one. P-C (§13.2) tests it. If it fails,
`--inspect`/`--dap` imply `go.GoroutineThreads=platform`; the scheduler is
unaffected, only the goroutine-count ceiling of a debug session drops.

Delve remains structurally unsupported (SPEC §3.10).

### 6.5 Stack traces, `runtime.Callers`, goroutine dumps

Today every call pushes a `GoStackFrame` (H3) and the
[rtstack/](../../internal/backend/interop/src/main/java/graalgo/rtstack/)
nodes read that chain. Replacement:

- **Panics.** `PanicException` is an `AbstractTruffleException` created with
  its throw-site node. Truffle records the guest frames lazily while the
  exception unwinds through call targets;
  `TruffleStackTrace.getStackTrace(ex)` `[JAR]` returns elements with
  `getTarget()` (function name from the root) and `getLocation()` (the call
  node, hence file:line). The fatal-panic printer formats gc's layout from
  those. Cost is paid only when a panic is thrown. The stack depth captured is
  unlimited for panics (`AbstractTruffleException.UNLIMITED_STACK_TRACE`),
  because gc prints the whole stack (with its own elision of the middle of
  very deep stacks, which the printer reproduces).
- **`runtime.Callers` / `Caller` / `Stack`.** A `@TruffleBoundary` intrinsic
  walks `Truffle.getRuntime().iterateFrames(visitor)` with read-only frame
  access, which works on virtual threads. Each frame yields
  `(call target, call node)`.
- **PCs.** Go code stores program counters as `uintptr` and resolves them
  later (`runtime.CallersFrames`, `runtime.FuncForPC`). A PC is
  `functionIndex << 24 | siteIndex + 1`, where `functionIndex` is the
  language-level index of the `FunctionUnit` and `siteIndex` the ordinal of
  the call node (or statement) within the function, both assigned at decode
  time. `FuncForPC(pc).Name()`, `.FileLine(pc)`, `.Entry()` are table lookups
  in the language-level PC table (§2.4). This keeps gc's property that
  `pc - 1` is inside the call instruction.
- **All-goroutine dumps** (`GOTRACEBACK=all`, SIGQUIT, `runtime.Stack(buf,
  true)`, `pprof` goroutine profile): `env.submitThreadLocal(threads, action)`
  with a synchronous action that captures each thread's guest stack — the
  purpose `ThreadLocalAction` was built for. Blocked goroutines respond
  because parks go through `setBlocked…` (§2.7); the dump prints the park
  reason from `G` as gc does (`[chan receive]`, `[select]`, …).
- Internal roots (thunks, goroutine entry) are `isInternal()` and skipped, so
  traces contain exactly the frames gc shows; inlining by Truffle never hides
  a frame (guest frames are reconstructed from compiler metadata), which is
  stronger than gc's own inlined-frame handling.

`rtstack` shrinks to the intrinsic bodies for the `runtime` facade functions;
`GoStackFrame`, `pushGoStack`, `updateTopFrameSection` and `StackTrace`'s
chain walk are deleted.

### 6.6 Logging and internal diagnostics

- Internal diagnostics use `TruffleLogger.getLogger("go", "<component>")`
  (`sched`, `loader`, `itab`, `interop`), enabled with
  `--log.go.<component>.level=FINE`. No `System.err` prints from runtime code.
- Output that is part of Go's observable behaviour (panic text, `GODEBUG`
  traces such as `schedtrace`, `println`) is **not** logging: it goes to the
  program's fd 2 exactly as gc writes it.
- The hand-rolled probes (`CallSiteProbes`, `InterfaceDispatchProbes`,
  `megamorphicTransitions`, H17) are deleted. Call-site polymorphism for SPEC
  §2.6.1's "measure real distributions" is read from the DSL's own
  introspection (`@Introspectable` on the dispatch nodes) or
  `engine.SpecializationStatistics`, by a test-only instrument.

### 6.7 Profiling and the pprof bridge

- **CPU.** The `cpusampler` instrument samples guest stacks through
  safepoints and works on any registered language; `--cpusampler` output shows
  Go function names once roots have names and sections. The pprof bridge
  (SPEC §3.10) obtains the sampler through the instrument lookup
  (`env.lookup(instrumentInfo, CPUSampler.class)`), starts and stops it for
  `runtime/pprof.StartCPUProfile`/`StopCPUProfile` and for the
  `net/http/pprof` handler, and translates samples (stack of
  `(root, source section)` plus counts) into `profile.proto`. JFR
  `jdk.ExecutionSample` translation remains the alternative for host-level
  frames.
- **Heap.** Allocation nodes (`new`, `make`, composite literals, `append`
  growth, string concatenation, closure creation) call the language's
  `AllocationReporter` (`env.lookup(AllocationReporter.class)`) guarded by
  `reporter.isActive()`, which is a constant-folded assumption when no
  listener is attached. `runtime.MemProfile` and the pprof heap profile attach
  a sampling listener (gc's default rate: one sample per 512 KiB allocated).
  Live-heap figures (`runtime.ReadMemStats`) stay on JVM memory beans as
  today.
- **Insight** scripts and the CPU tracer work unmodified once tags exist; no
  language work.
- **JFR** events of SPEC §3.10 are host-level and unchanged, except that
  goroutine events are emitted when a `G` is installed on or removed from a
  thread rather than at thread start/end.
- **Heap dumps / VisualVM** (JIT mode, GG-0008): a heap dump shows
  `GoStruct`, `GoSlice`, `GoString` instances. Truffle's heap-dump integration
  for guest-level views is not pursued in v1; `toDisplayString` and meta
  objects make the inspector's object view usable instead.

### 6.8 Acceptance tests for tooling

Each is a scripted, headless test (TR-10): (1) CDP client sets a breakpoint by
file:line in a goroutine, resumes, reads a local of each carrier and a struct
field, steps over, steps into a call through an interface, evaluates nothing;
(2) the same through DAP; (3) `--cpusampler` on the `fib` benchmark attributes
> 90 % of samples to `main.fib`; (4) `--coverage` line counts equal the stock
`cover` tool's counts on a corpus program; (5) a panic trace and a
`runtime.Callers` dump are byte-identical to gc after the harness's existing
normalization of addresses and goroutine ids.

## 7. Native Image and the three modes (GG-0006, GG-0049, GG-0050)

### 7.1 What Truffle on SubstrateVM requires of the language

- **Closed world.** No class is defined at run time. The DSL, library exports
  and wrappers are generated at build time, so §4–§6 comply. Struct layout by
  descriptor (SPEC §3.4) was chosen for this reason and is kept.
- **PE hygiene is checked statically.** The image builder computes every
  method reachable for runtime compilation from the `execute` methods. Code
  that is PE-hostile and not behind `@TruffleBoundary` fails the build or
  bloats the image (Truffle's image feature has a blocklist check for methods
  such as string formatting and collection internals reached without a
  boundary; the exact host option name is to be taken from the pinned
  `native-image`, `[RECALL]`). This is the strongest PE gate available and
  becomes part of CI once an image builds (TR-12).
- **Reflection, resources, proxies** need reachability metadata (GG-0052).
  The language itself uses none; see §7.2 for what the current runtime does.
- **Runtime compilation** is included when the Truffle runtime and compiler
  are on the image builder's module path, and is omitted for mode (b)
  (interpreter-only runtime). Languages are discovered from the module/class
  path (the `GoLanguageProvider` service entry the processor generates
  `[PROTO]`); there is no `--language:go` flag for a third-party language and
  no language home, since bundles are self-contained (§2.1).
- **Image-heap state.** Anything initialized at build time is frozen into the
  image. With §2.4 there is no static program state; `GoLanguage` and the
  `TypeTable` entries for predeclared types may be build-time initialized.
- **Host inlining** (`@InliningCutoff`, small `execute` methods) determines
  interpreter speed in the image; §11 rule P5.

### 7.2 What in the current code breaks it `[CODE]`

| Item | Where | Problem | Resolution |
|---|---|---|---|
| same-FQN shadow classes resolved by class-path order | (at `a0df481b`) 8 stubs in `truffle/`, shadowed by `runtime/` | the image builder sees two definitions of one class; which wins is an accident of path order | **done** — RECOVERY T0.D (2026-11-18) merged both into one module and deleted the stubs; [check_duplicate_fqn.py](../../.github/scripts/check_duplicate_fqn.py) keeps it so |
| `ClassPreloader` (`Class.forName` over every `graalgo.*` class) | [ClassPreloader.java](../../internal/backend/truffle/src/main/java/graalgo/launcher/ClassPreloader.java) | meaningless in an image, and drags every class into reachability | JIT-mode only; guarded by `ImageInfo.inImageCode()` |
| reflection | `GoOs` (`Class.forName("com.sun.security.auth.module.UnixSystem")`, `sun.misc.Signal`) | needs metadata; `sun.misc.Signal` is supported by Native Image but must be referenced directly | direct references or generated metadata (GG-0052) |
| `ServiceLoader` registrars | `WireKindRegistrar`, `OsSurfaceInitializer` | work in images when the service files are present, but exist only to break the compile cycle between the three modules | disappear with T0.D and the v1 factory |
| process-global mutable statics (H18, H20) | scheduler, descriptor maps, `FuncIds`, `GoOs` | initialized at build time they are frozen into the image; initialized at run time they still prevent a second context | §2.4 |
| `ThreadLocal` goroutine state (H2) | `GoroutineState`, `SchedulerCore` | functional in an image, but wrong with more than one context per thread and slower than the context-thread-local | §2.5 |
| string-keyed JSON factory and `Map<String, Object>` trees | v1 `NodeFactory` | huge reachable surface, no runtime compilation benefit | deleted at M3 |
| no `@TruffleBoundary` (H1) | everywhere | the runtime-compilable method set would be the whole JDK; the build fails its PE checks | T0.B for v1; §11 for the new package |
| `-Xss32m` assumption | launchers | image stack size is set differently (§9) | T3 |

### 7.3 The modes, stated precisely

| Mode | What is compiled ahead of time | How Go code runs |
|---|---|---|
| (a) JIT on HotSpot | nothing | interpreted, then compiled by Graal through PE, with OSR |
| (b) pure AOT | the Truffle interpreter, the runtime library, and (optionally) the decoded program in the image heap | **interpreted, always** |
| (c) hybrid | the same as (b) plus the Graal compiler and the encoded graphs of all runtime-compilable methods | interpreted, then compiled at run time as in (a) |

Native Image does not turn a Go function into machine code ahead of time in
any of these modes; it compiles the *interpreter*. SPEC §2.5 says so for (b)
("interpreter-only fallback runtime … lowest peak performance"), but GG-0050
then requires the AOT baseline to be "performant without the runtime JIT".
With an AST interpreter that baseline is the AST interpreter's speed, which
for Truffle languages is typically one to two orders of magnitude below
compiled code. Three levers exist, in order of cost:

1. Make the interpreter fast: the conventions of §11 (small typed `execute`
   methods, no switches, host-inlining discipline) and, structurally, the
   Bytecode DSL (§8).
2. In mode (c), compile eagerly instead of waiting for profiles:
   `engine.CompileAOTOnCreate` / `RootNode.prepareForAOT()` `[JAR]` ("Truffle
   AOT" in the documentation). Go's static types make profile-free
   compilation unusually effective, because nothing needs type feedback except
   the four sites of §4.6. This requires the compiler in the image and
   executable memory, so it is not available in (b) or on W^X platforms.
3. Persisting compiled code across runs (auxiliary engine caching) is an
   Oracle GraalVM feature for native images `[RECALL]`; whether it is
   available to a third-party language on the pin is a T3 question.

P-D (§13.2) measures lever 1's starting point today, in an afternoon, by
running the benchmark set with `engine.Compilation=false`. **Owner decision
2026-11-18: measure first** — P-D gates amendment A2; SPEC §3.9 carries the
question as `[OPEN]` until it reports. The number decides
how much weight §8 carries. This is finding 2 of §0.2.

Packaging follows the toolexec design §4.2: the first AOT deliverable is a
**generic runtime image** (launcher + language + runtime, built once per
graalgo release, loading a bundle at start). In this design that image is
simply `Context.eval(bundle)` in a native launcher; a per-program image is the
same launcher with the bundle embedded and, optionally, pre-parsed during
image build.

### 7.4 Goroutines in images

Per the changelog entry quoted in §2.6, a polyglot context in a native image
runs "virtual" threads on platform threads. Until upstream implements Loom
support for Truffle in images, modes (b) and (c) therefore have
goroutine = OS thread: correct, but with a goroutine ceiling of a few
thousand to tens of thousands and context-switch costs that SPEC §3.5 did not
assume. Options, none free:

- accept and document the ceiling for image modes, with thread-stack size
  tuned down and recycling (§2.6) to cap creation cost;
- track upstream and re-test at each pin bump (the natural re-test trigger);
- keep JIT mode the default (GG-0049's meta-gate would probably decide this on
  its own once "goroutine scalability" is measured).

A user-space continuation mechanism inside the interpreter is not an option:
Bytecode DSL continuations suspend a single method `[DOC]`, and a goroutine
can block at any call depth.

This must be verified on the pinned `native-image` before it is acted on.
**Owner decision 2026-11-18: measure first.** P-F (§13.2) is a gating work
item; until it reports, SPEC §3.5 carries the question as `[OPEN]`.

## 8. Bytecode DSL or AST

SPEC §3.3 and GG-0014 specify an AST with typed execute methods. Truffle's
Bytecode DSL generates, from operation definitions written as small DSL nodes,
a bytecode interpreter per language. It is in the pinned API
(`com.oracle.truffle.api.bytecode`, 51 classes `[JAR]`) and is documented as
experimental, with APIs that "may change between releases" `[DOC]`; the
changelog shows breaking changes in most 25.x releases.

| Criterion | AST (as designed in §4) | Bytecode DSL |
|---|---|---|
| Peak performance | same compiler, same PE | "same peak performance as ASTs" `[DOC]` |
| Interpreter speed (mode (b), warm-up, cold isolates — GG-0050) | one virtual call per node; megamorphic `execute` sites | one dispatch loop per function, quickening, boxing elimination; documented as the main advantage |
| Footprint (std has 2.48 M AST nodes; a hello-world executes ~300 functions — toolexec design §3.2, §5.5) | tens of bytes per node object; lazy decoding bounds it to executed functions | a byte array plus constants per function; an uncached tier executes without allocating specialization state |
| Host stack per Go call (§9) | one host frame per nested node on the path to the call: tens of frames | one interpreter frame per Go function plus the call boundary |
| `goto` | `GotoBlockNode` state machine (§4.8) | native branches (forward branches and structured loops; whether a backward `goto` needs restructuring is to be checked) |
| `defer` | open-coded in the root, chain fallback (§4.5; accepted amendment A8, GG-0071) | the same, expressed with the epilog/finally operations |
| `select`, goroutine parking | runtime call | runtime call; single-method continuations do not help (§7.4) |
| Instrumentation | wrappers (§6.3) | tag instrumentation built in |
| Serialization | not needed (GIR is the format) | available, not needed |
| Maturity on the pin | the mainstream path of every production Truffle language | experimental; 25.0 LTS receives fixes, API moves on the innovation line |
| Cost now | the plan of record | every statement and expression emitted through a builder; ~110 node classes re-expressed as operations; SPEC amendment |
| Risk to the recovery | none added | M1a slips while the team learns an experimental API |

**Recommendation.**

1. Build the new node package as an AST, as SPEC says. The recovery's critical
   path (toolexec M1a–M1d) cannot absorb an experimental interpreter
   generator.
2. Keep the door open by convention, not by abstraction layers: operation
   semantics live in static `@Specialization` methods and in static runtime
   helpers (§4.2), which is the form a Bytecode DSL `@Operation` or
   `@OperationProxy` consumes; type-directed selection lives in pure functions
   of `(operation, Go type)` in the factory, separate from node construction.
   A later port then rewrites the factory's emission layer and the control-flow
   nodes, and reuses the operation bodies, the value model, interop,
   the language class and the runtime.
3. Decide at a **named gate: after toolexec M1d** (real `fmt`), with the
   bake-off TR-13: port the benchmark set's functions to one
   `@GenerateBytecode` root and compare interpreter-only speed, bytes per
   loaded function, maximum recursion depth, warm-up and peak. Switch if the
   bake-off shows any of: interpreter-only ≥ 2× faster on the benchmark set;
   recursion depth ≥ 3× deeper at equal stack; footprint per function ≤ 50 %
   — and P-D has shown that interpreter speed matters for GG-0050.
4. A switch is a **spec amendment** to §3.3 (the wording "expression nodes
   with typed execute methods … void statement nodes"). GG-0014 itself ("one
   Truffle AST … no second backend") is about having a single executable
   representation for all three modes; a bytecode root node satisfies that
   intent, but the owner decides (§14, A5). Running both representations side
   by side is ruled out: that *would* be a second backend.

## 9. Stack depth and recursion

**Today** `[CODE]` ([limitations.md](limitations.md)): with `-Xss32m`,
recursion to depth 15,000 passes and 20,000 fails with a clean
`stack overflow`, i.e. about 1.6–2 KB of host stack per Go call in the
interpreter. gc's limit is a 1 GB goroutine stack with frames of tens of
bytes; programs that recurse 10⁵–10⁶ deep (recursive walks over long lists or
deep trees, naive recursive algorithms in tests) work under gc and fail here.

**Mechanics that matter.**

- A mounted virtual thread runs on its carrier's stack; frames are copied to
  the heap when it parks and thawed lazily when it resumes. The depth limit of
  a goroutine is therefore the **carrier's** stack size, i.e. the JVM's
  default thread stack size (`-Xss`), which is what the current launchers set.
- Compiled code uses one host frame per *call target*, not per node, and none
  for inlined calls. Once functions compile (T0.B), a hot recursive function
  uses a small fraction of the interpreter's stack per level. The limit then
  depends on whether the recursion has become hot before it becomes deep —
  usually yes for loops of recursive calls, not for a single cold deep
  descent, which runs entirely in the interpreter.
- Deoptimization of a deep compiled stack converts frames back to interpreter
  frames lazily, one activation at a time as control returns to it, so it does
  not need the whole interpreter-sized stack at once.
- `StackOverflowError` can surface anywhere, including inside runtime code
  holding locks. It is not safely recoverable as a guest-level event.

**Decisions.**

1. **Stack overflow is fatal**, as in gc (`runtime: goroutine stack exceeds
   1000000000-byte limit` … `fatal error: stack overflow`, exit status 2). It
   is never a `panic`, deferred functions do not run, `recover` cannot see it.
   Go nodes never catch `StackOverflowError`; the goroutine-entry root does,
   and hands the report to the fatal-error reporter running on a different
   thread with a fresh stack (lane T0.A already runs the reporter "unfrozen").
   The reporter prints gc's two lines and a truncated stack obtained from the
   failing thread *after* it has unwound to its entry (so from the exception's
   Truffle stack trace, not from a live walk).
2. **Raise the limit with stack size first.** Stack memory is reserved
   virtually and committed on touch, and only the carriers (≤ GOMAXPROCS plus
   a few compensating threads) and foreign threads ever run Go code. Setting
   the default thread stack size to hundreds of megabytes — up to gc's own
   1 GB — costs address space, not RSS, until a goroutine actually recurses.
   P-E (§13.2) measures depth, RSS after a deep recursion (carrier stacks do
   not shrink, gc's do), start-up time and any interaction with the JVM's own
   threads, at 32 MB, 256 MB and 1 GB. In images the equivalent is the image's
   stack-size setting; with platform-thread goroutines (§7.4) a large default
   multiplies by the goroutine count in *virtual* size only, but the
   per-thread reservation must then be chosen with the goroutine ceiling in
   mind.
3. **Re-measure after T0.B**, which the limitation entry already names as its
   re-test trigger, with two programs: a hot recursive function and a single
   cold descent.
4. **Reduce interpreter frames per call** by construction: no wrapper nodes
   around loops or calls, argument evaluation in the call node itself, blocks
   that execute children directly. The structural fix is the Bytecode DSL
   (one interpreter frame per function), and recursion depth is one of the
   three switch criteria in §8.
5. "Stack-depth parity" is one of GG-0049's six experiments; P-E plus item 3
   are that experiment's JIT-mode half.

Rejected: an explicit heap-allocated Go stack or CPS-transformed calls. They
would remove the host limit but defeat Truffle's inlining and escape analysis
and amount to a different execution model than SPEC §3.3.

## 10. Migration plan

### 10.1 Sequencing against the other lanes

- **T0.B (JIT lane)** keeps repairing v1 nodes minimally so the benchmark set
  compiles. Nothing below refactors v1 node classes; v1 nodes are deleted at
  toolexec M3.
- **T0.A (runtime lifecycle)** and **T0.D (flatten the module overlay)** land
  before TR-1: TR-1 edits the launchers and `SchedulerCore.spawn`, which T0.A
  is changing, and a language class needs one module.
- **Toolexec M1a (W4: loader and new node factory)** is where the idiomatic
  node package is born. TR-3 and TR-4 are not separate deliveries after W4;
  they are W4's node work done to §11, with the estimate below being the
  *additional* cost over reusing the old classes.
- The runtime library (`graalgo.runtime`, scheduler, channels) is shared by
  v1 and the new path until M3. Changes to it (TR-5, TR-6, TR-7) must keep v1
  green or be switchable; the table says which.

### 10.2 Work packages

Effort is in focused agent-days `[EST]`.

| WP | Scope | Depends on | Acceptance (all measurable) | Effort |
|---|---|---|---|---|
| **TR-0** Gates and jars | Tool and TCK jars added to `lib/` with checksums. `make pe-check`: runs the benchmark set (branch `wt/recovery/jit`, `test/bench/`) and a corpus subset with `engine.CompilationFailureAction=ExitVM`, `engine.BackgroundCompilation=false`, performance warnings fatal; counts bailouts. DSL warnings are already errors (`-Werror`). | T0.B merged | `pe-check` red on a seeded bailout (a node calling `String.format` without a boundary), green otherwise; bailouts on the benchmark set = 0 | 1–2 |
| **TR-1** Language shell on v1 | `GoLanguage`/`GoContext` (policy `EXCLUSIVE`, second context refused); root nodes get the language; launchers create an `Engine`/`Context` and `eval` a byte `Source` (bundle MIME type reserved; v1 wire JSON under a temporary MIME type); goroutine threads through `newTruffleThreadBuilder`; `os.Exit` through `closeExited`. Statics stay. | T0.A, T0.D | every existing suite green through the polyglot launcher; a JUnit test evaluates a program from Java and reads its exit status; `--cpusampler` lists Go function names; second context fails with the documented message; benchmark set within 5 % of pre-TR-1 except goroutine-spawn microbenchmarks, which are recorded | 3–5 |
| **TR-2** Decision spikes | P-A … P-F of §13.2 | TR-1 for P-A/B/C | a dated results section appended to this document; decisions T3, T4, T11, T12 confirmed or replaced | 4–6 |
| **TR-3** Core node package (inside toolexec W4) | §12 layout; base classes; locals, cells, globals; arithmetic/compare/convert for all carriers (§3.1); strings, slices, arrays, structs; control flow (§4.8); static calls; roots with defers — open-coded up to *L* defers, the explicit per-goroutine chain otherwise, per-frame unwinding (accepted amendment A8, GG-0071), with *L*/*L*+1 boundary tests and the corpus on both paths; closures per SPEC §3.4 until A9 is decided (deferred pending benchmarks); panics. Unit test and PE test per node class (§11, T-rules). | TR-2 (calling convention), toolexec W2 schema | toolexec M1a's corpus gate; `pe-check` bailouts = 0 on that corpus under `CompileImmediately`; arithmetic differential sweep vs gc over all widths and operators including overflow, shifts ≥ width, `MIN / -1`, unsigned compare (generated, ≥ 10⁵ cases); `fib`, `loop`, `slicesum`, `structs` benchmarks ≤ 2× gc steady state `[EST]` as the first target, tightened after measurement | +6–10 over W4's 15–25 |
| **TR-4** Dynamic sites and splitting | `GoIface`/`GoEface`/`GoItab`, language-level itab cache, `IfaceCallNode`, func-value call node, type assertion/switch nodes, map nodes per key kind, `cloneUninitialized` through GIR re-decode, `@ReportPolymorphism` | TR-3 | `iface` and `maps` benchmarks: the monomorphic interface call is inlined (asserted from the compilation trace), no megamorphic site on the benchmark set; a `sort.Interface`-style test with three receiver types shows three split copies and a direct call in each; SPEC §2.6.1's falsifying benchmark can now be run (gc+PGO vs graalgo steady state) | 6–9 |
| **TR-5** Context-ification | all statics of H18/H20 into `GoLanguage`/`GoContext`; `ContextThreadLocal<M>`, `G`; scheduler per context; policy `SHARED`; context cancel/close stops goroutines | TR-1; after M3 for v1-only statics, or switchable | two contexts on one engine run two programs concurrently with correct, separate output; closing a context with 10⁴ parked goroutines returns within 1 s and leaks no thread; a static-state audit script (no non-final or collection-typed static field under the node and runtime packages except an allow-list) is a `make check` gate | 4–7 |
| **TR-6** Safepoints | **REJECTED as written — replaced by WP-POLLCOST** (RECOVERY.md; [ADR-0013](../dev/adr/ADR-0013-a7-rejected.md)): the monitor thread + `ThreadLocalAction` preemption and the deletion of `Safepoint.poll` (amendment A7) failed P-B; `Safepoint.poll` stays (GG-SCHED-0001), and making it cheaper is WP-POLLCOST. Residual of TR-6, unchanged by the verdict: every park through `setBlocked…` | TR-5 | scheduler suites (`SchedulerCoreTest`, `WakeupModelTest`, `TortureBattery`, fairness tests) green; `engine.TraceMissingSafepointPollInterval` reports nothing above 10 ms on the corpus | 2–4 (was 3–5) |
| **TR-7** Stack traces | §6.5: Truffle stack traces for panics, `iterateFrames` for `runtime.Callers`, PC table, goroutine dump action; delete the shadow stack and the chain-walking half of `rtstack` | TR-1 (panics), TR-3 (new roots) | panic-trace and `runtime.Caller(s)` corpus byte-identical to gc after existing normalization; `fib` benchmark allocation rate per call = 0 in compiled code (JFR allocation profile) | 4–6 |
| **TR-8** Interop exports | §5.2–5.4: `@ExportLibrary` on all value kinds, `ToInteropNode`/`ToGoNode`, exceptions, module object, embedding tests | TR-4, TR-5 | Java-side conformance suite: one test per row of §5.2 and §5.3 through `org.graalvm.polyglot.Value`; the polyglot TCK passes for the language provider; hot-path check: benchmark set unchanged (±2 %) with exports present | 8–12 |
| **TR-9** `graalgo/java` | §5.5–5.6: package source, intrinsics, `Throwable`, `Func`/`As` generics, host-access policy, build-time Java stub generator | TR-8, toolexec M1r (intrinsic registry; was M1b before toolexec-design §8.1), owner decision A4 | Go-side conformance suite: every function of §5.5 against `java.util` classes; a Java exception is observed as `error`, never as panic; stock-behaviour-unchanged check of GG-0003 (corpus identical with the package present) | 6–10 |
| **TR-10** Instrumentation | §6.2–6.4: tags, wrappers, source sections, scopes, language views, goroutine thread naming, async stacks | TR-3, TR-0 | the five scripted tests of §6.8 | 5–8 |
| **TR-11** pprof bridge | §6.7: sampler → `profile.proto`, allocation reporter, `runtime/pprof` intrinsics, HTTP endpoint | TR-10, toolexec M4 (`net/http`) | `go tool pprof` reads a CPU and a heap profile produced by a graalgo run and attributes the top function correctly | 5–8 |
| **TR-12** Native image smoke (track T3 entry) | generic runtime image in interpreter-only and hybrid variants; blocklist/PE check as a gate; metadata; goroutine-thread behaviour measured | TR-5, T0.D, toolexec M1d | hello-world and the benchmark set run in both images with gc-identical output; image build has no PE-check violation; start-up, RSS and 10⁴-goroutine behaviour recorded | 8–14 |
| **TR-13** Bytecode DSL bake-off | §8 item 3 | toolexec M1d, P-D | the comparison table filled in with measurements; go/no-go recorded | 5–8 |

Total additional effort ≈ 60–100 agent-days, of which TR-0 … TR-2 (8–13 days)
can start as soon as T0.A/T0.D land and decide everything that is currently
open. Critical path for interop (GG-0005): TR-1 → TR-3/4 (with W4) → TR-5 →
TR-8 → TR-9. Critical path for debugging (GG-0024): TR-1 → TR-3 → TR-10.

### 10.3 What stays measurable throughout

| Quantity | How | Gate |
|---|---|---|
| PE bailouts | `make pe-check` (TR-0) | 0 on the benchmark set and corpus subset, per push once TR-0 lands |
| Compiled speed vs gc | `test/bench/run.sh` on the JIT lane's branch | reported per WP; thresholds set after the first full measurement, never asserted in advance |
| Interpreter-only speed | same with `engine.Compilation=false` | tracked from P-D; feeds §8 |
| Allocation per call | JFR allocation events on `fib`/`structs` | 0 in compiled code after TR-7 |
| Interop conformance | Java suite (TR-8), Go suite (TR-9), TCK | all rows of §5.2/§5.3 |
| Tooling | scripted CDP/DAP/sampler/coverage tests | §6.8 |
| Image | TR-12 smoke | per pin bump at least |

## 11. Truffle coding conventions (review checklist)

Applies to every class under the node, value and runtime packages of §12. A
reviewer checks each changed node against every rule; "n/a" is an answer,
silence is not. The rules a class file can show are enforced mechanically by
`NodeConventionsTest` (§16).

**Boundaries**

- **B1.** A method reachable from an `execute*` method is either PE-safe by
  B4 or annotated `@TruffleBoundary`. Runtime-library entry points are
  boundaries by default.
- **B2.** No `VirtualFrame` parameter on a boundary method (it would force
  materialization). Pass values.
- **B3.** No `java.util` collection, `String` concatenation/formatting,
  stream, lambda capture, lock, `synchronized`, park, I/O, reflection or
  `System.*` call on a fast path. Node state is fields and
  `@CompilationFinal(dimensions = 1)` arrays, never a `List` or `Map`.
- **B4.** PE-safe allow-list for JDK calls on fast paths: `System.arraycopy`,
  `System.identityHashCode`, `System.nanoTime`/`currentTimeMillis` (compiler
  intrinsics); `Arrays.equals/mismatch/compare/compareUnsigned/copyOf/copyOfRange`
  on primitive arrays and `Arrays.fill` on any array; `Math.*`; the boxed
  types' bit, unsigned, raw-bits and `valueOf` operations (not their
  `toString`/`parse*`); `Objects.requireNonNull/equals`;
  `java.util.concurrent.atomic.*`, `VarHandle`, `Reference.get`,
  `Thread.currentThread/onSpinWait`. Anything else needs a boundary or an entry
  added to this list by review (and to `NodeConventions.hostile`).
- **B5.** Exceptions are created behind a profile, in a boundary factory
  method (`GoPanics.*`). No `throw new …("…" + x)` in an `execute` method.
- **B6.** `CompilerDirectives.transferToInterpreterAndInvalidate()` precedes
  every write to a `@CompilationFinal` field or `@Child` after construction;
  `transferToInterpreter()` precedes unreachable-by-design paths
  (`CompilerDirectives.shouldNotReachHere()`).

**Node state and shape**

- **N1.** Node fields are `final`, or `@Child`/`@Children`, or
  `@CompilationFinal` with B6. No `volatile`, no mutable counters, no static
  mutable state (**S1**: none anywhere outside `GoLanguage`/`GoContext`).
- **N2.** No `switch` or `if` on a node's own constant configuration in
  `execute*`: the factory chooses the class (§4.2). A node whose `execute`
  would start with `switch (kind)` is several nodes.
- **N3.** Child fields have the narrowest family type (§4.1). `executeGeneric`
  on a child is allowed only at boxing boundaries (call arguments, interface
  conversion, interop).
- **N4.** Loops over `@Children` or `@CompilationFinal` arrays carry
  `@ExplodeLoop`; loops over guest data never do.
- **N5.** Guest loops are `Truffle.getRuntime().createLoopNode(...)`.
- **N6.** Control-flow exceptions extend `ControlFlowException`, carry no
  payload, and are preallocated.
- **N7.** No context state in nodes; use `GoContext.get(this)` /
  `GoLanguage.get(this)` (§2.4).

**DSL**

- **D1.** More than one `@Specialization`, or any `@Cached` inline cache,
  appears only at the sites listed in §4.4 (pointer shapes), §4.6 and §5. A new
  such site needs a sentence in the PR saying what is dynamic about it.
- **D2.** Every inline cache has an explicit `limit` (≤ 3; 4 for type caches),
  a `replaces` fallback that does not use a hash lookup on the fast path where
  an index exists, and `@ReportPolymorphism` if it guards a call.
- **D3.** Helper nodes are `@GenerateInline @GenerateCached(false)`, plus
  `@GenerateUncached` if reachable from interop or slow paths. Profiles in DSL
  nodes are the inlined variants.
- **D4.** Guards are side-effect free, compare identities or primitives, and
  bind PE constants on the cached side. No guard calls a boundary.
- **D5.** The annotation processor's warnings are errors; a suppression
  (`@SuppressWarnings("truffle-…")`) carries a comment with the reason.

**Frames and values**

- **F1.** Frame slots are read and written with the accessor of their static
  kind; no `catch (FrameSlotTypeException)`.
- **F2.** `frame.materialize()` and caller-frame access are forbidden in Go
  nodes. (A9 deferred pending benchmarks: until decided, the closure-creation node of SPEC
  §3.4 is the single allowed `materialize()`.)
- **F3.** Integer results are in canonical form for their Go type (§3.1); a
  node that can leave canonical form is followed by a wrap node, and its unit
  test covers the wrap.
- **F4.** Exact classes are asserted with `CompilerDirectives.castExact` (or a
  plain cast of a field with an exact static type), never tested with
  `instanceof` on a path whose type is static.

**Profiles and performance**

- **P1.** Every branch to a panic or slow path is behind a branch profile;
  `if`/loop conditions use counting profiles.
- **P2.** No value profile on statically typed data.
- **P3.** Allocation on a fast path is allowed only for Go-visible objects and
  boundary tuples; each is listed in the node's comment.
- **P4.** Allocation nodes call the `AllocationReporter` behind
  `isActive()`.
- **P5.** Slow-path helpers called from `execute` methods are
  `@InliningCutoff`; `execute` methods stay small.

**Interop and tooling**

- **I1.** No `InteropLibrary` use in Go nodes outside the `java` intrinsics
  and the boundary conversion nodes.
- **I2.** Every class whose instances can reach an `any` or cross the boundary
  exports `InteropLibrary` or is wrapped by `ToInteropNode`; the boundary
  asserts `isValidValue`.
- **I3.** Every node built from source has a position; statement and call
  nodes declare their tags.

**Tests (T-rules)**

- **T1.** Each node class has a unit test that executes it in the interpreter
  and a PE test that compiles a root containing it with
  `engine.CompileImmediately=true`, `engine.BackgroundCompilation=false`,
  `engine.CompilationFailureAction=Throw` and performance warnings fatal, then
  executes the compiled code and compares results. Both must be able to fail:
  the PE test is validated once against a seeded missing boundary.
- **T2.** Tests use only the polyglot API (`Context`, `Value`) or the node
  under a real `GoLanguage`; no hand-built frames, no `root.execute(frame)`.
  Tests that require the optimizing runtime skip explicitly (and loudly) when
  it is absent rather than passing vacuously — the role
  `TruffleTestAssumptions` plays in Truffle's own suite.
- **T3.** `CompilerAsserts.partialEvaluationConstant` on everything a node
  assumes constant; `CompilerAsserts.neverPartOfCompilation()` at the top of
  slow-path code that must stay behind a boundary.
- **T4.** A dynamic site's test covers monomorphic, polymorphic (at the
  limit), megamorphic, and the nil case, and asserts the specialization
  reached through DSL introspection.

## 12. Package layout for the new backend

One module (after T0.D), one source root. Names are proposals for toolexec W4.

```
graalgo.language   GoLanguage, GoContext, GoOptions, G, M, GoProgramRootNode
graalgo.loader     BundleLoader, PackageUnit, FunctionUnit, SymbolTable        (toolexec design §4.3)
graalgo.build      NodeFactory (typed-IR visitor), OpSelect (pure (op, type) → node choice),
                   FrameLayout, PositionTable
graalgo.types      GoType and subclasses, TypeTable, GoItab, ItabCache, StructDesc
graalgo.values     GoString, GoSlice, GoStruct, GoMap, GoChan, GoFunc, GoIface, GoEface,
                   cells (I64Cell…RefCell), ElemPtr, FieldPtr, GoNil, GoUint64, GoBoxed
graalgo.nodes      GoNode, ExprNode + typed families, StmtNode, wrappers
  .root            GoFuncRootNode, GoDeferFuncRootNode, thunks, PrologueNode, ResultNode
  .local           local / cell / global / argument nodes
  .arith           arithmetic, compare, wrap, convert (per carrier)
  .data            string, slice, array, struct, map, channel nodes
  .call            CallNode, FuncCallNode, IfaceCallNode, GoStmt (go), defer nodes
  .iface           ToIface/ToEface, type assert, type switch, ItabLookupNode
  .control         block, if, loops and repeating nodes, switch, goto block, select, signals
  .builtin         len/cap/append/copy/clear/delete/min/max/new/make/panic/recover/print
graalgo.interop    ToInteropNode, ToGoNode, GoErrorException, module/package objects,
                   scope objects (NodeLibrary), language views
graalgo.java       intrinsics behind the graalgo/java package
graalgo.intrinsics intrinsic registry and the runtime-facade intrinsic nodes (toolexec design §5.3)
graalgo.runtime    scheduler, channels, timers, panics, GoPanics, static helper classes
                   (GoStrings, GoSlices, GoMaps) — boundary or allow-listed methods only
graalgo.tools      pprof bridge, test-only instruments
graalgo.launcher   JVM/native launcher: builds Engine/Context, evals the bundle
```

Style guide for a node class (what "born idiomatic" means in review):

1. Class comment: the Go construct, the static types it is built for, what it
   allocates, which rules of §11 needed thought.
2. Constructor or DSL factory takes children of family types and immutable
   configuration; no setters.
3. `execute*` is short enough to read at a glance; anything else is a static
   helper with an explicit B1 status.
4. A neighbouring test class per T1–T4.
5. The existing class of the same purpose under
   [frontend/](../../internal/backend/truffle/src/main/java/graalgo/frontend/)
   is cited as the semantic reference (edge cases, gc-compatibility fixes are
   in its comments and tests), and its tests are ported, not its structure.

## 13. Prototypes

### 13.1 Done in this lane

A standalone language (five source files, ~500 lines) outside the repository,
in the session scratchpad
(`/tmp/claude-1000/-home-cafxx-dev-graalgo/695032aa-7918-4687-abb5-5d96a0af48b1/scratchpad/truffle/`,
not durable). It was compiled with the pinned jars and
`javac -proc:full -Xlint:deprecation -Werror`, and run with
`java -Xmx1g -Xss32m` under `nice -n 19` on a loaded box. Results are single
runs or three repetitions as stated; they establish feasibility and order of
magnitude, not benchmarks.

| Claim | Result |
|---|---|
| Registration with a byte MIME type, `parse()` over bytes, `Context.eval` on the class path with the optimizing runtime | works |
| Statically typed DSL nodes without a type system | generate and compile; `engine.CompilationFailureAction=ExitVM` did not fire |
| JIT and OSR | `fib(32)`: 6.5–6.7 ms steady state (12 ms in one run with more load); 3·10⁸-iteration loop entered once: 10–12 ms |
| Struct in the SPEC §3.4 shape exported through `InteropLibrary` | `Value.getMember`, `putMember`, `getMemberKeys`, `hasMembers` work; cached member lookup compiles |
| Function value executed from Java with a cached `DirectCallNode` | works |
| Virtual threads through `newTruffleThreadBuilder(...).virtual(true)`, each reading a `ContextThreadLocal` | works with the "experimental" warning; 20–32 µs per spawn + join vs 1.8–2.7 µs bare |
| `ThreadLocalAction` delivered to, and parking, a running thread | works for platform and virtual threads in loops with a memory barrier; **not delivered** in a host-compiled loop with only a racy plain-field read, identically on platform threads (§2.7) |
| Context close with threads still alive | fails with `IllegalStateException: The language did not complete all polyglot threads`, i.e. `finalizeContext` really must stop goroutines (§2.3) |

Because the scratchpad is not durable, TR-1 re-creates the relevant parts as
real tests; nothing depends on the prototype's files.

### 13.2 To run next (TR-2), cheapest first

**P-D and P-F are gating work items** (owner decision 2026-11-18): SPEC
amendments A1 and A2 are held `[OPEN]` in SPEC §3.5/§3.9 until they report,
and TR-12 / track T3 do not start before both results are appended here. They
need no TR-1 build and run first.

| # | Question | Method | Cost | Decides |
|---|---|---|---|---|
| P-D (**gate**) | How slow is interpreter-only execution? | benchmark set with `engine.Compilation=false`, on the JIT lane's branch as it is | 0.5 day | A2 (SPEC §3.9 `[OPEN]`); weight of §8; honesty of GG-0050 |
| P-E | Depth and RSS versus stack size | recursion program at `-Xss` 32 MB/256 MB/1 GB, interpreted and compiled | 0.5 day | T12 |
| P-A | Goroutine creation cost and what recycling recovers | TR-1 build, spawn microbenchmark and `chanpingpong`, with and without recycling | 1 day | T3 details |
| P-B | Safepoint preemption equals the current poll | scheduler torture and fairness suites with the monitor + action implementation behind a switch; cancellation during a park | 1–2 days | T4 or its fallback; the calling convention. **Done 2026-11-18: FAIL** (branch `wt/pb/safepoint`; [ADR-0013](../dev/adr/ADR-0013-a7-rejected.md)): T4/A7 rejected, the poll stays |
| P-C | Debugger on virtual-thread goroutines | inspector session: breakpoint in a goroutine, read and write a caller's local | 0.5 day | whether debugging forces platform threads |
| P-F (**gate**) | Threads and runtime compilation in an image | minimal language image (the prototype of §13.1) with 10⁴ "virtual" threads; `native-image` is present in the pinned JDK | 1 day; one image build, heavy — schedule it alone | A1 (SPEC §3.5 `[OPEN]`); §7.4, finding 1 |

### 13.3 Risky, not cheap

- Splitting through GIR re-decode interacting with lazily decoded generic
  instantiations (TR-4).
- The embedded lifecycle: cancellation of blocked goroutines, foreign threads
  holding Go locks when the context closes (TR-5).
- `ToGoNode` for interface-typed parameters receiving foreign objects (TR-8).
- Anything involving polyglot isolates for a third-party language (T3,
  GG-0027).

### 13.4 TR-1 as built (2026-11-18)

[GoLanguage](../../internal/backend/truffle/src/main/java/graalgo/language/GoLanguage.java)
(id `go`, byte MIME types `application/x-graalgo-wire+json` — the v1 wire
document — and the reserved bundle type) and
[GoContext](../../internal/backend/truffle/src/main/java/graalgo/language/GoContext.java).
The product launcher (P3SyncMain) runs every program as `Context.eval` of
the wire document through
[GoPolyglot](../../internal/backend/truffle/src/main/java/graalgo/launcher/GoPolyglot.java);
`Context.parse` is `--check`; `eval` returns the exit status as an `int`.
graalgo-run (`Main`) and `SchedMain` still run without a context (their
root nodes keep a `null` language).

- **Policy `EXCLUSIVE`**, as §2.4 prescribes until TR-5; a second context
  is refused while another runs a program.
- **Goroutine threads** come from `env.newTruffleThreadBuilder(..).virtual(true)`
  (option `go.GoroutineThreads=platform` for platform threads); the Cleaner
  (finalizer) thread enters the context around Go calls. `finalizeContext`
  tears the scheduler down (parked goroutines unwind without running defers),
  so an embedder can close a context whose program left goroutines blocked.
- **`ContextThreadLocal`**: one `GoThreadSlot` per thread (the `M` of §2.5)
  holds the current `GoroutineState` and the safepoint poll countdown.
  Language-bound roots read it inline (`GoroutineState.current(RootNode)`,
  `Safepoint.poll(Node)`); runtime code behind boundaries keeps the host
  `ThreadLocal` (both are written together).
- **Safepoints**: the counter fast path stays (P-B has not run; §2.7's
  `ThreadLocalAction` scheme is not used). Measured reason for moving the
  countdown into the slot: on polyglot threads the host `ThreadLocal` lookup
  misses its first probe (JFR: `ThreadLocalMap.getEntryAfterMiss` topped the
  profile) and cost ~20 % on `loop`.
- **Not done**: `os.Exit` still goes through the scheduler's exit request
  (not `closeExited`); the `--cpusampler` check; the static audit of TR-5.

Benchmarks (`test/bench`, nice 19, `-Xmx2g -XX:ActiveProcessorCount=4`, three
interleaved runs each, median best-round ms, TR-0 build vs TR-1 build):
chanpingpong 1582 → 1609, fib 8.71 → 11.81, iface 20.6 → 19.8, loop 48.5 →
17.3, maps 3.13 → 2.89, slicesum 16.5 → 9.26, strbuild 14.1 → 11.7,
structs 59.4 → 59.6. `fib`'s best-of-15 is a warm-up artefact: its tier-2
compilation (~300 ms) lands after round 15 because the earlier rounds are
now faster (first 15 rounds: 377 ms → 292 ms of wall time); at 80 rounds the
steady state is 8.5 → 6.5 ms. `make pe-check`: 0 bailouts.

## 14. Items for the owner

Proposed SPEC amendments (none is assumed by the work packages that precede
its decision). Status column: owner decisions of 2026-11-18
([ADR-0009](../dev/adr/ADR-0009-recovery-spec-amendments.md)).

| # | Section | Proposal | Needed by | Status |
|---|---|---|---|---|
| A1 | §3.5 / §2.5 | State that in native-image modes goroutines are platform threads until Truffle supports Loom in images, with the resulting goroutine ceiling; add the re-test trigger (each GraalVM pin bump). Verify first (P-F). | TR-12 / T3 | **Open — measure first.** Gated on P-F; SPEC §3.5 marks it `[OPEN]`. |
| A2 | §3.9, GG-0050 | Define "performant without the runtime JIT" as a measured interpreter-speed target, or restrict it to the hybrid's eager compilation; the current wording cannot be met by construction in mode (b). Measure first (P-D). | T3 | **Open — measure first.** Gated on P-D; SPEC §3.9 marks it `[OPEN]`. |
| A3 | §4.3 table | Wording: the "Java" column's `ProxyArray`/`ProxyObject`/`ProxyExecutable` are replaced by "polyglot `Value` (`as(List)`, `as(Map)`, functional-interface coercion)"; semantics unchanged. | TR-8 | Pending (independent of A4); SPEC §4.3 notes it. |
| A4 | §4.3 host access | Split default: embedded = the embedder's `HostAccess` (recommended `SCOPED`); standalone = host class lookup and public access restricted to an allow-list, only when the program imports `graalgo/java`. | TR-9 | **Decided**; SPEC §2.4/§4.3. GG-0003/GG-0005 text unchanged (they do not state a policy). |
| A5 | §3.3, GG-0014 | Only if TR-13 says so: allow function bodies to be Bytecode DSL roots; "one AST" read as "one executable representation for all modes". | after M1d | Pending — deferred to the TR-13 bake-off. |
| A6 | §3.3 | Clarification, no change of intent: the megamorphic interface-call fallback is an itab method-table index, not a hash lookup; splitting is part of the devirtualization design. | TR-4 | Pending. |
| A7 | §3.5; GG-SCHED-0001, GG-0017 | Preemption by Truffle safepoints: a monitor thread submits a `ThreadLocalAction` that performs the quantum park; `Safepoint.poll()` at back-edges and function entries is deleted (T4, §2.7, TR-6). Supersede GG-SCHED-0001 and GG-0017 with "quantum expiry is delivered at Truffle safepoints". | TR-6 (after P-B) | **REJECTED 2026-11-18** ([ADR-0013](../dev/adr/ADR-0013-a7-rejected.md)). Accepted conditionally on P-B by the owner ([ADR-0012](../dev/adr/ADR-0012-open-coded-defers.md)); P-B failed (pinned parks, hangs of plain programs, mount-hook recursion, counted loops, compiled `for {}`), so by the owner's rule the switch is not made. SPEC and the requirements are unchanged — `Safepoint.poll()` stays; the poll's cost on tight loops is WP-POLLCOST (RECOVERY.md). |
| A8 | §3.5; `GG-0019` | Open-coded defers per function, statically lowered for the common case (defers outside loops and range-over-func bodies, at most *L*), with the explicit per-goroutine chain as the fallback (loops, range-over-func, over *L*); each frame runs its own pending defers on panic/Goexit; `recover`/`Goexit`/`runtime.Caller` match gc (T7, §4.5, TR-3). | TR-3 | **Accepted 2026-11-18** ([ADR-0012](../dev/adr/ADR-0012-open-coded-defers.md)). `GG-0019` superseded by `GG-0067`, whose lock had no owner sign-off; **re-issued 2026-11-18** as GG-0070 (semantics, incl. `panicnil` and caller frames per [traceback-normalization.md](traceback-normalization.md)) and GG-0071 (mechanism), **locked 2026-11-18 on owner sign-off**; SPEC §3.5 and Appendix B amended. |
| A9 | §3.4 (text only; GG-0072 does not mention closures) | Closures are `GoFunc{target, captures}` over typed cells; `frame.materialize()` banned (T7, §4.4, rule F2). | TR-3 | **Deferred pending benchmarks** (owner, 2026-11-18): decided after the closure benchmarks; SPEC unchanged — `{RootCallTarget, MaterializedFrame\|null}`. |

Other decisions requested:

1. Amend the toolexec design §4.3 so that W4 builds the new node package of
   §12 instead of reusing the existing node classes (decision T13;
   +6–10 agent-days on M1a, avoids a second rewrite). **Decided 2026-11-18**;
   applied (toolexec design D12).
2. Approve adding the tool and TCK jars to the pinned set (TR-0).
3. Whether foreign (host) threads calling into Go should hold a GOMAXPROCS
   token; this design says no (§2.6, item 4).

### 14.1 Trade-offs of A7–A9

Status (owner, 2026-11-18, [ADR-0012](../dev/adr/ADR-0012-open-coded-defers.md)):
**A8 accepted** — the m1a-nodes lane builds open-coded defers with the chain
fallback; **A7 accepted conditionally on P-B** `[OPEN]`, then **REJECTED** after P-B failed ([ADR-0013](../dev/adr/ADR-0013-a7-rejected.md)) — the lane follows
SPEC (`Safepoint.poll()`); **A9 deferred pending
benchmarks** — the lane follows SPEC (`MaterializedFrame` closures). The
estimates of §10.2 assume acceptance, and the column "if rejected" says what
following SPEC costs instead (for A8 it is retained as the cost of the
fallback-only build, not an expected outcome).

| | A7 safepoint preemption | A8 open-coded defers | A9 closures over typed cells |
|---|---|---|---|
| Locked text it conflicts with | GG-SCHED-0001 ("on quantum expiry at a `Safepoint.poll()` … loop back-edges and function entries"), GG-0017 ("the Truffle AST SHALL insert `Safepoint.poll()`"), SPEC §3.5 | `GG-0019` ("defer SHALL be an explicit per-goroutine chain"; superseded by `GG-0067` on acceptance, re-issued as the drafts GG-0070/GG-0071), SPEC §3.5 ("open-coded defers are a later phase, not v1"; amended) | SPEC §3.4 prose only (`{RootCallTarget, MaterializedFrame\|null}`); no requirement text |
| What it buys | compiled loops and calls carry no scheduler code (Truffle already polls its own safepoint at `LoopNode` back-edges and call boundaries, so today every back-edge pays two polls); one mechanism for preemption, debugger suspend, cancellation and stack sampling; parks answer thread-local actions | functions without `defer` have no handler and no `G` access; `defer mu.Unlock()` inlines to the unlock; no `ArrayDeque` allocation per call (H15); matches gc since Go 1.14 | no frame materialization, so partial escape analysis can scalar-replace every local of a function that creates a closure (H9); captured integers unboxed; Go 1.22 per-iteration loop variables become "a fresh cell per iteration" |
| Measured so far | `[PROTO]` an action parks a running platform or virtual thread (§13.1); TR-1 moved the countdown into the context slot and `loop` went 48.5 → 17.3 ms (§13.4) — the poll is already cheaper than when T4 was written | none on this codebase; gc's own measurements motivated open-coding | none on this codebase; H9 is a code-reading finding |
| What breaks or gets harder | preemption latency becomes monitor-period-bound (up to ~2 quanta instead of ~1) — GG-FAIR-0001/0003 bounds must be re-measured (P-B); an action is **not** delivered to a host-compiled runtime loop without a memory kill (§2.7, `[PROTO]`), so runtime loops need explicit polls and the missing-poll gate; a monitor thread per context; relies on Truffle's experimental virtual-thread support; requires superseding two locked requirements and re-running the preemption-torture battery | more complex roots: loop defers need the fallback list, `recover` eligibility, `Goexit`, panics during defers and range-over-func defers each get a second code path; GG-0070's and GG-0071's verification plans (adversarial defer storms, every program on both paths, L/L+1 boundary tests) cover both paths; debugger/stack-trace views of pending defers change | the pointer model must treat a cell as a one-leaf referent of GG-0072's (object, byte offset) pointers; the inspector's scopes must show captured variables from cells instead of a parent frame; closure creation allocates one cell per captured variable instead of one frame (cheaper only once PEA applies) |
| If rejected / not yet decided (follow SPEC) | the poll stays at back-edges and function entries, in its TR-1 form; TR-6 shrinks to `setBlocked…` parks (≈ −1 agent-day `[EST]`) and the poll's cost becomes WP-POLLCOST — **this column is now the plan** | TR-3 builds the per-goroutine chain (≈ −1 to −2 agent-days `[EST]` now; the H15 allocation and `G` access on every deferring call stay) | TR-3 builds `MaterializedFrame` closures and rule F2 gets one exception; the H9 cost stays and is re-measured at M1a |
| Decision needed by | **decided 2026-11-18** (rejected: P-B failed) | **decided 2026-11-18** (accepted); the root nodes of TR-3 build it | before TR-3's closure nodes (M1a): deferred pending benchmarks |

## 15. W4 as built: the new node package (2026-11-18)

The first slice of toolexec W4 / TR-3 / TR-4:
[graalgo/go/](../../internal/backend/truffle/src/main/java/graalgo/go/)
loads a linked bundle through `GoLanguage` (`application/x-graalgo-bundle`)
and runs programs that import no package outside the bundle. Gate:
`make test-m1a` ([test/m1a/run.sh](../../test/m1a/run.sh)). What follows
records where the implementation departs from the sections above, and why;
[SPEC.md](SPEC.md) decided every case where the two disagreed.

| Topic | As built | Section it changes |
|---|---|---|
| Package names | `graalgo.go.{loader,types,values,nodes,build,runtime}`: the names of §12 without the prefix are taken by v1 (`graalgo.values`, `graalgo.runtime`) until M3. | §12 |
| Integer and float carriers | Every integer type is a `long` in canonical extended form, both float types a `double` (a float32 is a float-representable double; `+ - * /` computed in double and rounded once are correctly rounded). Four families: `I64`, `F64`, `Bool`, `Ref`. The `int`/`float` carriers of §3.1 are not introduced: this halves the arithmetic, comparison, conversion and location node surface, and a wrap node is one instruction after PE. To be revisited only if a benchmark shows a cost. | §3.1, §4.1 |
| Value model | Flat, per SPEC §3.4 / GG-0072: one class `Flat { type, long[] prims, Object[] refs, byte[] bytes }` is every struct and array value, every slice backing store and the box of every escaping variable; nested structs and arrays are inlined. Scalar leaves wider than a byte are `long` slots, reference leaves `Object` slots, and every byte-sized leaf (`uint8`, `int8`, `bool`, as field and as array element alike) is a byte of `bytes` ([ADR-0015](../dev/adr/ADR-0015-pointer-model-w13.md) decision 3). The type's layout gives each field its (scalar, reference, byte) slot offsets and its gc byte offset; `GoType.leafAt`/`gcOffOfLeaf` are the leaf table (gc byte offset ↔ leaf: type, region, slot), structural, so an array costs one division per level. gc's size, alignment and field offsets are computed from the type's structure for every type (instantiated and unnamed ones have no record) and checked against the frontend's record of each defined type at its first layout — a rule that diverges from gc stops the program. A slice backing store's `type` is its element type's `backing()` layout, so a pointer into one has a leaf table. The nested-`GoStruct` rows of §4.4's pointer table do not apply. | §4.4 |
| Pointers | `*T` is the referent `Flat` when it points at the object's start, else `Ptr { Flat obj, int prim, int ref, int byt }` (GG-0062's "referent + offset", in slot units, one per region); the gc byte offset is derived on demand from the inverse leaf table (the offsets sum to a leaf index), so pointers do not carry it. The third shape is the view pointer `View { obj, gc byte offset }`: what the **checked** conversion `(*U)(unsafe.Pointer)` yields when `U`'s leaves are not the referent's leaves at that location (same region and slot encoding, same relative gc offsets; decided from the two leaf tables, cached per referent type, offset class and `U`), and what `unsafe.Pointer` arithmetic yields; a conversion whose leaves coincide returns the typed pointer, so typed code never holds a view (ADR-0015 decision 1). Pointer equality is identity, field-wise, or by location when a view is involved. `uintptr(p)` is the referent's range base in the process's weak, range-allocating handle table plus the byte offset; integer → pointer is a floor lookup (lane perf-pe, 2026-11-23: one weak range per object, found by identity in an open-addressing table, kept in an append-only base-ordered array bisected newest-first; dead ranges go when it fills — amortized O(1)). The factory keeps two round trips out of the table: `unsafe.Pointer(uintptr(p) ± y)` in one expression is `unsafe.Add(p, ±y)` (same results and fatals), and an integer local whose every definition is `x := uintptr(p)` and every use `unsafe.Pointer(x)` / `(x ^\|+- 0)` (`abi.NoEscape`'s shape; not escaping, not a parameter) holds the pointer itself (`NodeFactory.pointerLocals`, gate [uintptr_roundtrip.go](../../test/m1w13/programs/uintptr_roundtrip.go)). The checked conversion caches up to four identity locations (referent type, leaf position) inline (`Unsafes.ToPtr`). gc's `checkptr` conditions — a pointee reaching past the referent, a misaligned pointer to a pointerful type, arithmetic leaving the referent, an integer naming no live object, `unsafe.Slice`/`String` past the referent — are Go fatal errors with gc's text (`runtime.Unsafe`, `GoProcess.fatal`). `ElemPtr`/`FieldPtr`/typed cells are not used. | §4.4 |
| Memory access nodes | A location node (`Mem.Loc`: field path into an object, through a pointer, slice element, array element, map entry) evaluates its operands once and exposes object + three offsets (constants, or `int` scratch frame slots); load, store and address-of nodes are shared by all shapes, and the region of a load or store is chosen by the factory from the leaf's static type (`LoadI64`/`LoadU8`/`LoadI8`/`LoadRef`, `StoreI64`/`StoreByte`/`StoreRef`). A location computes only the offsets its consumer reads (`Loc.need`; a dead scratch slot is otherwise kept alive by every frame state) and `&s[i]`/`&p.f` build the pointer without the scratch slots. Field offsets fold into the innermost location at build time. Behind a view pointer (`Mem.PtrLoc`'s third shape) `resolve` returns null and the consumer goes by gc byte offset through the referent's leaf table (`runtime.Unsafe`): an N-byte scalar is assembled from or spread over the leaves it covers, a pointer word is a reference leaf or a word of a string, slice or interface header, and headers are written by replacing the object in the slot (ADR-0015 decision 2) — behind a branch typed code never enters. | §4.2 |
| Slices | `Slice { Flat base, prim, ref, byt, len, cap }`; elements occupy slots of the backing `Flat`, so `s.arr[:]` aliases a struct's inlined array. One slice shape: a `[]byte` over a `make`d array and one over a struct's inlined `[N]byte` both address `base.bytes`, because the struct's bytes are typed too (ADR-0015 decision 3, which amends SPEC §3.4's "typed Java arrays" to a byte region). `string(b)`, `[]byte(s)`, `append([]byte, s...)` and `copy([]byte, s)` are one `arraycopy`; `unsafe.String`/`unsafe.Slice` over byte leaves share storage. Other narrow kinds (`int16`, `int32`, `float32`) stay `long` slots (a P-item after M2). The cost of the one shape: a `[]byte` over wider scalars (`unsafe.Slice((*byte)(unsafe.Pointer(&ints[0])), n)`) cannot share storage and is a `checkptr` fatal error ([limitations.md](limitations.md)). | §4.2 |
| Strings | `Str { byte[] b, off, len }`: immutable, substrings share the array as in gc. `unsafe.StringData` exposes the storage as a byte backing store (one wrapper per string object). | §3.1 |
| Complex | An immutable `Complex` object in a reference slot (not two scalar slots); a view reads and writes its gc bytes (two float halves) by replacing the object. | §3.1 |
| Interfaces | One class `Iface { type, data }` for empty and non-empty interfaces (SPEC §3.4: "(dynamic type descriptor, data) + itab"); interface-to-interface conversion is the identity. The call site's inline cache guards on the dynamic type and holds the itab method as a PE constant; the megamorphic path is an itab lookup on the type (a hash lookup, which SPEC §3.3 allows) plus a constant method index. `GoIface`/`GoEface` with the itab in the value are not built. | §4.6 a |
| Type descriptors (W9a) | An interface value holds its `GoType`; nothing typed reads a descriptor. The **descriptor image** of a type ([Descriptors.java](../../internal/backend/truffle/src/main/java/graalgo/go/types/Descriptors.java)) is the descriptor gc's compiler would emit — kind struct, `UncommonType`, parameter/field/interface-method array, method table, in `reflectdata.writeType`'s order — as one `Flat` whose layout type is a struct of the program's own `internal/abi` types (`struct { k abi.StructType; u abi.UncommonType; d [n]abi.StructField; m [k]abi.Method; t unsafe.Pointer }`, the last slot holding the `GoType`), so upstream's `internal/abi` and `internal/reflectlite` read it through the checked conversions of the pointer model like any object, and the conversions to `*abi.Type`, `*abi.StructType` and the uncommon-struct types are identities. Every field is written at the gc offset `internal/abi` gives its name (no descriptor offset in the backend); facts follow cmd/compile (`PtrBytes`, GC mask, `TFlag` from the alg classes, `Hash` from the link string, sorted method tables with package-qualified names, map group type). `NameOff`/`TypeOff` are negative ids in the type table's offset list (`resolveNameOff`/`resolveTypeOff`); `Equal` and a map type's `Hasher` are func values over one root per type (`loader/TypeEqual`, `loader/TypeHash`; `sync.Map`'s `HashTrieMap` takes both from the descriptor). Images are built on first use, published per closure, canonical per type. The **type word** of an interface header read through a view is the image (`Unsafe.typeWord`); written through a view it maps back (`Descriptors.typeOf`); a program that does not link `internal/abi` keeps the `GoType` as the word. Not built yet (W9b): `Method.Ifn/Tfn` (gc's "unreachable" -1), descriptors made at run time, itab words of non-empty interface headers (word 0 reads as the type). Gate: `make test-imgcheck` |
| Receiver adaptation | Method-set entries carry the embedding path from the frontend and their receiver-less signature; `Receivers.adapt` turns the interface's data word into the declared receiver (identity, copy, or path walk). No promotion thunks are generated except for methods promoted from an embedded *interface* (a forwarder that calls the embedded value's method). Itabs match name **and** signature (gc's runtime/iface.go), so the crash report's error/Stringer test is gc's type switch. Lane perf-calls (2026-11-20): an interface call passes the data word itself (no copy) to a value-receiver method whose GIR body only reads its receiver (not escaping, never assigned, incremented, ranged into, addressed, sliced or used as a method receiver) — Go never mutates an interface's data; gate [iface_value_receiver.go](../../test/m1a/programs/iface_value_receiver.go). | §4.6 a |
| Wrappers and `recover()` | Method values, method expressions, embedded-interface forwarders and the emitter's promoted-method-expression literals (GIR `FuncLit.wrapper`) are gc's `WRAPPER` frames: their call node passes `recover()` eligibility (the deferred call's argument array) on to the method they forward to. | §4.5 |
| Closures | `Closure { RootCallTarget, MaterializedFrame }` as SPEC §3.4 writes it; the frame is a capture *environment* created per function literal evaluation and holding the boxes of the captured variables (needed for Go 1.22 per-iteration variables), never the creating function's activation frame. All access goes through `values.Env`, so typed cells can be swapped in for the A9 benchmark. | §4.4, T7 |
| Defers | Open-coded (amendment A8, GG-0071) when no `defer` statement of the function can run twice (not in a loop or a range-over-func body, no `goto` in the function, at most *L* = 8 sites, `NodeFactory.MAX_OPEN_DEFERS`): per-site slots + a bit set, exit sequence with a per-site inline cache, goroutine state touched only while a panic or Goexit unwinds. Otherwise the explicit per-goroutine chain (SPEC §3.5, GG-0071; A8 accepted, [ADR-0012](../dev/adr/ADR-0012-open-coded-defers.md)). `recover()` is effective in the frame whose argument array is the deferred call's — no per-call flag clearing (H2). A prepared defer writes its target into the site's slot (a static callee's target is a compilation constant) and allocates only the argument array: that array is the calling convention's own and escapes unless the deferred call is inlined, so "no allocation on the non-panic path" holds for inlined deferred calls only. *L* is pinned by [defer_path.sh](../../test/m1a/defer_path.sh) (the trace hook `-Dgraalgo.go.traceDeferPath`, and `-Dgraalgo.go.forceDeferChain` to show the choice unobservable); the compiled-graph check of GG-0071 is WP-NODETEST. | §4.5 |
| Goexit | `runtime.Goexit` is the shared runtime's `Goexit` `ControlFlowException`; both deferring roots run their pending defers for it; `recover()` returns nil meanwhile; a Goexit from a deferred call aborts the panic in flight; a panic raised and recovered during a Goexit lets it resume (`GoPanic.resumeExit`). The scheduler ends the goroutine (main: gc's "no goroutines" deadlock report once the others are done). `runtime.Goexit` (declared by the facade `runtime`) and `syscall.Exit` (what `os.Exit` ends in) are entries of the intrinsic registry, bound at link time (toolexec W5/W6, toolexec-design §13). | §4.5 |
| Range over func | `RangeFunc` calls the sequence function with a yield root that runs the loop body on the enclosing function's materialized frame; break/continue map to false/true, other exits are stashed and rethrown after the sequence function returns; gc's `#state` machine and `panicrangestate` messages; body defers go to a `G.RangeDefers` record at the enclosing function's level of the chain (gc `deferrangefunc`); `recover()` in a body is nil. | §4.8 |
| Safepoints | `Safepoint.poll` at function entry and loop back-edges (SPEC §3.5, GG-SCHED-0001); T4 is rejected (P-B failed, A7). WP-POLLCOST (2026-11-19): the poll's fast path is one volatile load of the shared **poll epoch** compared with the epoch the thread saw at its last full poll (kept in `GoThreadSlot`) — no per-poll store, no countdown. A daemon `graalgo-poll-ticker` bumps the epoch every quarter of the smallest quantum (clamped to 50 µs–1 ms) and process exit bumps it with the exit epoch, so the full quantum check (behind a boundary) runs at the first poll after each tick and the exit freeze at the very next poll. Effect and method: [performance.md](performance.md). Lane perf-calls (2026-11-20): a function whose body contains no call (no static, func-value, interface, deferred or `go` call, no range-over-func) has **no entry poll** — loops poll themselves and every recursion cycle passes through a calling function's entry (gc: a leaf has no stack-bound check). A **counted loop** (`for …; i < X; i++` over a signed integer the body never assigns, X invariant: constants, unassigned locals, `len` of unassigned slice/string/array locals, their sums; range over slice, array, signed int) is **strip-mined in compiled code**: strips of at most 1024 iterations (and at most 2^15 node executions) of an inner loop tested only at its head, one poll per strip; the interpreter polls per iteration so OSR counts stay exact. Gate: [preempt_spin.go](../../test/m1r/programs/preempt_spin.go) (counted-loop spinners). Numbers: [performance.md](performance.md). | §2.7 |
| Panics | `GoPanic extends AbstractTruffleException` in the new package; runtime errors reuse v1's value model (`GoErrors`/`GoErrorValue`) boxed as interface values of Java-defined `runtime.*Error` types until the facade `runtime` (toolexec W5) declares them. Channel panics of the shared runtime are re-raised. | §4.5 |
| Reflection and `os` (fmt-path lane, 2026-11-20) | `reflect` is upstream's source over descriptor images; its call path enters the node package through four intrinsics (`callFunc`, `callMethodByName`, `makeFuncValue`, `methodFunc`: call targets invoked from a boundary with arguments loaded from addresses, methods found by name in the frontend's method sets) and its type constructors intern in the `TypeTable`. Interface data words follow `abi.TFlagDirectIface` for one-pointer-word structs and arrays; slice headers written word by word stand as untyped headers (`Unsafe.sliceWithData`); channels are `TypedChan` (the runtime's `GoChan` plus the element type). `graalgo.syscall.Poller` is the epoll poller (GG-0063): `ParkReason.NETPOLL` parks, timer-heap deadlines; `Executor` runs a ready `read`/`write` inline after a zero-timeout `poll(2)`. Nothing here is on a compiled hot path: every entry is behind a `@TruffleBoundary`. | toolexec-design §13 |
| Loops | `range` and `switch` are lowered by the factory onto one `LoopNode`-based loop node and `If` chains (no per-form repeating node classes); `goto` blocks are merge-exploded over a program counter. Lane perf-pe (2026-11-23): the caught label `Jump` is a phi of constants when several statements throw, so `GotoBlock` compares it with each label and continues the exploded loop from inside the matching branch (the new pc is a constant there); returning the target from a helper made the pc a phi and failed partial evaluation for every `goto` function (regexp's backtracker, jsonwire's number scanner, flate's decoder). A strip-mined loop's interpreter iteration tests `idx < limit` (exactly `cond`), so the `limit` node compiled code runs is specialized: a node only compiled code executes never leaves its uninitialized state and deopt-loops ("Deopt taken too many times"). Gate: `make pe-check-m1a` runs [test/m1a/pe_std.sh](../../test/m1a/pe_std.sh) (the std paths of the realistic programs hot under `CompilationFailureAction=ExitVM`, plus a planted failure); report: `make pe-report`. | §4.8 |
| Order of evaluation | gc's, where the spec leaves it open (cmd/compile's order pass): within a statement, calls, receives, `append`, `copy`, `len`/`cap`, slice expressions, string → slice conversions, type assertions to non-pointer types and a `&&`/`\|\|` with such a right operand are evaluated first, in lexical order, into frame temporaries (`NodeFactory.hoist`); variables, fields, element and pointer loads, arithmetic, `string(b)` and map indexes are read when the statement executes. `a, b := x, f()` sees the `x` that `f` left. Statements without a side-effecting operation are built as written; conditions are ordered in place. Pinned by [eval_order.go](../../test/m1a/programs/eval_order.go). | §4.2 |
| Maps | Iteration order is a random rotation of insertion order (`GoMap.Iter`: random start, then insertion order), not gc's bucket-randomized order; both are unspecified, but a program that checks the *kind* of randomness diverges ([bugs.md](../dev/bugs.md) BUG-0031). | §3.4 |
| Splitting | `FuncRoot.isCloningAllowed()` and `cloneUninitialized()` (lane perf-calls, 2026-11-20): the factory gives every function and literal root a rebuild recipe (instance, GIR body, name, wrapper flag, capture count) and a split copy is the body **decoded again from GIR** with fresh inline caches, as §4.7 designed; `Dispatch` and `IfaceDispatch` report polymorphism, Truffle's default splitting budget decides. `-Dgraalgo.go.forceSplit=true` splits every static call site at once (the m1a/m1r/m1c corpora pass under it; a planted throwing `cloneUninitialized` fails 16 of 17 m1r programs). Benchmark: `ifacesplit` ([performance.md](performance.md)). Thunks, yield roots and intrinsic roots are not split. | §4.7 |
| Not built in this slice | Source sections, tags and wrappers (§6, rule I3); `InteropLibrary` exports (§5); `AllocationReporter` (P4); per-node unit and PE tests (T1–T4: waived for M1a by [ADR-0014](../dev/adr/ADR-0014-node-tests-waived-m1a.md) — the corpus gate, `defer_path.sh` and `make pe-check-m1a` stand in; WP-NODETEST builds them); context policy stays `EXCLUSIVE`; `G` lives in the TR-1 thread slot. Unsupported constructs fail loudly at first call: slice → array conversion, type switch/assertion on a type-parameter operand. | §5, §6, §11 |

## 16. Java and Truffle conventions, enforced (lane java-practices, 2026-11-20)

The owner's standing rules for the Graal/Truffle code: everything that can be
`final`, `static final` or a record is; the Truffle annotations are used
wherever they apply (§11). `graalgo.conventions.NodeConventionsTest` (in
`make test-java-fast`, ~0.3 s: class files only, read with the JDK's
`java.lang.classfile` API, no class loaded, no JVM per case) checks the rules a
class file shows over the new node package (`graalgo.go.*`), the shared runtime
(`graalgo.sched.*` but v1's `sched.nodes`, `graalgo.syscall.*`,
`graalgo.safepoint`) and the classes of `graalgo.runtime`, `graalgo.values` and
`graalgo.language` those reference directly. DSL-generated classes are exempt.

| Rule | What fails |
|---|---|
| N1-field | a `Node` instance field that is not `final`, `@Child`, `@Children` or `@CompilationFinal` |
| N1-volatile, N1-sync | a `volatile` field, a `synchronized` method or block in a `Node` class (outside a boundary) |
| N1-cfdims | `@CompilationFinal` on an array field without explicit `dimensions` |
| N1-children | a `@Children` field that is not a final array |
| N2-switch | an `execute*` method that switches on the node's own field (the factory chooses the class, §4.2) |
| N4-explode | an `@ExplodeLoop` method that reads a node array field that is neither `@Children` nor `@CompilationFinal(dimensions >= 1)` |
| N4-unexploded | a PE-reachable loop over a `@Children` array without `@ExplodeLoop` |
| N8-typecase | an `execute*` method of a non-DSL node with `instanceof` tests of three or more types (a specialization set) |
| S1-static | a static field that is not `final` |
| S2-static-read | PE-reachable code that reads a non-final static |
| C1-final | a concrete class without subclasses that is not `final` |
| R1-record | a class with only final fields, a constructor that only stores them and only accessor/`equals`/`hashCode`/`toString` methods that is not a record |
| D2-limit | a `@Specialization` with guards and a value `@Cached` but no explicit `limit` |
| D4-idempotent | a guard helper (node, superclass or `@ImportStatic` method) without `@Idempotent`/`@NonIdempotent` |
| D6-neverdefault | a `@Cached` initializer helper returning a reference without `@NeverDefault` (or `neverDefault =`) |
| B2-frame | a `@TruffleBoundary` method taking a `VirtualFrame`/`Frame` |
| B3-boundary | PE-reachable code calling a JDK method outside the B4 list, concatenating strings, entering a monitor or calling a record's generated `equals`/`hashCode`/`toString` |
| B6-invalidate | a PE-reachable write of a `@CompilationFinal`/`@Child` field, or `replace`/`insert`, not preceded by `transferToInterpreterAndInvalidate` |

*PE-reachable* is computed, not guessed: from every `execute*`,
`isLoopCondition`, `@Specialization` and `@Fallback` method of an in-scope node,
over static and virtual calls (every override in the scanned classes), stopping
at `@TruffleBoundary` methods, at v1 code (the interop module, v1 nodes) and,
inside a method, at `transferToInterpreter*`/`shouldNotReachHere`/
`neverPartOfCompilation` (the code after them is interpreter-only). A finding
names its call path (`reached from …`).

**Allowlist.** `internal/backend/truffle/src/test/java/graalgo/conventions/allowlist.txt`
holds the exceptions, one `RULE location | reason` line each. The test fails on
a finding that is not listed **and** on a listed entry that is no longer a
finding (STALE: delete it), and `check_ratchets.py`'s `java-conventions`
counter fails a commit that adds an entry against its base: the list only
shrinks. The rules that need judgment (B4 additions, a new rule) change by
review of this section and of `NodeConventions`.

**Proof the rules fail.** `graalgo.conventions.fixtures.Fixtures` plants one
violation of every rule plus two conforming nodes; the test requires exactly
the planted set and fails when a rule has no plant.

**Sweep (2026-11-20).** The first run over the tree reported 81 findings, a
third of them in v1 code reached through virtual calls (now scoped out). Fixed: the Go value
carriers `Slice`, `Ptr`, `View`, `RawIface`, `Closure`, `Complex` (now also
`@ValueType`: no code compares complex values by identity) and `Itab`, and six
scheduler/audit carriers, became records (412 field reads became accessor
calls, which PE and C2 inline); `FuncRoot`'s factory-set fields became
`@CompilationFinal`; `IfaceDispatch.method` got `@NeverDefault`; the atomic
view-pointer slow paths got `@TruffleBoundary`; `Mem.Loc.viewObj/viewOff`
throw through `shouldNotReachHere`; `Flat.sub` copies references with
`arraycopy` instead of the reflective `Arrays.copyOfRange`. Allowlisted (21):
the process-wide statics of the scheduler, poller and v1 hooks (S1/S2, the
poll epoch by design), `runtime_rand`'s `ThreadLocalRandom` (B3) and v1's
`GoTypeDescriptor` (R1). Not applied, by measurement or by absence of a site:
`@GenerateInline` (every DSL node of the package is a `@NodeChild` AST node;
the cached helpers are already inlined profiles and call nodes), DSL rewrites of
hand-written caches (the one left, `Unsafes.ToPtr`, caches a per-call result,
not a specialization), `@ValueType` on carriers that are identity keys
(`Descriptors`' identity maps, `%p` printing), `@Introspectable` (no test
introspects yet: WP-NODETEST).

Effect (interleaved A/B against the base, 3 rounds, medians of best rounds in
ms, load 13–18; `mapint`/`mapstr` re-run 4 more rounds): `fib` 3.34 → 3.39,
`loop` 0.86 → 0.87, `slicesum` 1.91 → 1.92, `structs` 1.77 → 1.83,
`ifacepoly` 0.33 → 0.34, `mapint` 3.49 → 3.47, `strbuild` 0.76 → 0.74,
`closures` 0.90 → 0.90 — neutral, as expected of changes PE already saw
through; compilations per run unchanged or fewer (`slicesum` 18 → 13,
`strbuild` 16 → 10). Class files of `graalgo.go` +0.7 % (record accessors).
