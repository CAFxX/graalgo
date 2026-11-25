# Truffle playbook audit (2026-11-24)

Independent audit of the new path against the owner's Truffle playbook (the
skill `anthropic-skills:implementing-truffle-languages`, sections cited as
"PB §n"; [AGENTS.md](../../AGENTS.md) §9 makes it a standing order). Tree:
master `bc89d2e2`. Pinned Truffle: 25.0.4.1.1 (the 25.0 LTS line, jars in
`internal/backend/truffle/lib`) on Oracle GraalVM JDK 25.0.4. Scope:
`graalgo.go.*`, `graalgo.sched`, `graalgo.syscall`, `graalgo.safepoint`,
`graalgo.language`; v1 code is out of scope (deleted at M3). File references
are relative to
[graalgo/](../../internal/backend/truffle/src/main/java/graalgo/). Design
context: [truffle-design.md](truffle-design.md) §11/§15/§16,
[north-star.md](north-star.md) §4, [performance.md](performance.md) §5.

Evidence tags: **[RUN]** reproduced in this lane (one JVM, `nice -n 19`,
`-Xmx2g -XX:ActiveProcessorCount=4`, shared host, single runs — directions,
not benchmarks); **[CODE]** read in the source; **[INFER]** reasoned from the
Truffle contract, not yet demonstrated. Not in this tree: the carrier-mode
decision the brief cites (ADR 0018, requirement 0073) — audited from the
brief only. graalgo is unreleased: every recommendation below is the best
design, not the most compatible one.

## 1. Executive summary

The **core design is sound and unusually disciplined** for PE: statically
typed node families chosen by the factory, call caches keyed by
`RootCallTarget`, preallocated `ControlFlowException` jumps, lazy root
building, a context-free AST, and a mechanical convention gate
(`NodeConventionsTest`). `compiler.TracePerformanceWarnings=all` on
`textproc` reports **zero** warnings [RUN]. What is wrong is at the edges —
the runtime does not cooperate with Truffle (PB §1.8, §10, §12), and a few
hot paths are hidden from PE. Ranked:

1. **F-10.1 Blocking never goes through `TruffleSafepoint`** — `--cpusampler` records 0 samples (211 missed) [RUN]; cancel/close of a running context cannot be bounded.
2. **F-6.1 `goto` blocks fail PE** (`GotoBlock`, pc is a phi) — `regexp`, `encoding/json`, `compress/flate` hot functions run interpreted forever [RUN].
3. **F-6.2 Deopt cycle** in `text/template.(*state).evalFieldChain` → permanent bailout; cause not yet isolated [RUN].
4. **F-12.1 No source sections, tags or root names** — every Truffle tool and trace prints `(Unknown)` [RUN]; the playbook calls retrofitting expensive.
5. **F-6.3 Hot operations behind `@TruffleBoundary`**: every non-integer map lookup/insert/iteration step and every interface `==` [CODE].
6. **F-16.1 The realistic benchmark measures warmup, not peak** (4 s, 10–17 rounds, rounds still 85–466 ms) [RUN]; the 27× headline mixes three regimes.
7. **F-18.1 We sit on the LTS line**; 25.1+ carries the features we need next and every month of delay adds a release to step through.
8. **F-10.2 Process-global mutable state** (13 statics: scheduler, poll epoch, poller, live context) blocks `SHARED`, multi-context tests and image preinitialization.
9. **F-10.4 Carrier-mode continuations move Truffle's thread-bound state between threads** [INFER] — needs a prototype gate before the design is built on.
10. **F-15 The test matrix runs one mode** (default tiered + a `CompilationFailureAction=ExitVM` slice); interpreter-only, compile-immediately, safepoint stress, cancellation and invalidation are untested.

## 2. Experiments run [RUN]

| # | Command (options as `-Dpolyglot.…`) | Result |
|---|---|---|
| E1 | `engine.TraceCompilation`, `CompilationFailureAction=Print` on `textproc`, `tmplrender`, `regexscan` | done/failed/deopt: 142/0/5, 284/1/32, 88/1/0. `tryBacktrack`: "did not reduce value to a constant … ValuePhi" at `go/nodes/Ctl.java:257`. `evalFieldChain`: "Deopt taken too many times" |
| E2 | `engine.CompileOnly=evalFieldChain` + `-XX:+LogCompilation` | 16 uncommon traps `transfer_to_interpreter`, **no** `transferToInterpreter` stack printed by `engine.TraceTransferToInterpreter` (so not a `CompilerDirectives` call: an exception leaving a boundary or an implicit exception); frame state right after the poll's volatile epoch read; IR 142 nodes, identical on every recompile |
| E3 | `cpusampler`, `cpusampler.Delay=2500` on `textproc` | "Recorded 0 samples … Missed 211 samples" |
| E4 | `engine.TraceTransferToInterpreter` | guest frames print as `fmt.Sprintf(Unknown)`; OSR targets are named `Ctl.Repeat@47545ac5<OSR>` |
| E5 | `compiler.TracePerformanceWarnings=all` on `textproc` | 0 warnings (17 rounds) |
| E6 | `engine.Compilation=false` on `textproc` | works; ~600 ms/round vs 85–466 ms compiled in the same 4 s window (gc: 5.7 ms) |
| E7 | `engine.TraceSplittingSummary` on `textproc` | 0 splits, 92,411 nodes (run also had `compiler.InstrumentBoundaries`, which made compilations fail on 25.0.4 — rerun alone before relying on it) |
| E8 | `javap`/jar listing of the pinned jars | §5 table |

Not yet run (PB §16.3 order): `compiler.InstrumentBoundaries` alone,
`MethodExpansionStatistics`/`NodeExpansionStatistics`, `TraceInlining`,
`engine.SpecializationStatistics`, IGV graphs, `SafepointALot`,
`TraceMissingSafepointPollInterval`, any run longer than 5 s.

## 3. Findings by playbook section

Severity: WRONG (wrong result), CRASH (crash/leak/hang), PERF, STYLE, GAP
(plan gap). Effort S ≤ 1 day, M 2–5, L > 5 agent-days. WP = §6.

### PB §1, §5, §6 — invariants, nodes, PE

| ID | Sev | Where | Invariant → consequence | Fix | Eff | WP |
|---|---|---|---|---|---|---|
| F-6.1 | PERF | `go/nodes/Ctl.java:253-275` (`GotoBlock`) [RUN E1] | PB §3.2/§6.1: a merge-exploded loop needs a PE-constant successor on every back-edge. `pc = target(j)` after `catch (Jump j)` merges the jumps of all nested `Throw`s (and `target`'s returns) into a phi → permanent bailout of any function with `goto` (13 in `backtrack.go`) | Lower `goto` functions in the factory to a block-dispatch node: flat basic blocks, each returning its successor index as a constant (structured exits become successor edges, no exception); match labels inside the exploded loop (`if (j == labels[i]) { pc = targets[i]; continue; }`) as the interim fix. On 25.5+: `CompilerDirectives.mergeExplodeKey` | M | 1 |
| F-6.2 | PERF | `text/template.(*state).evalFieldChain` [RUN E1/E2] | PB §6.5 row 3/5: a compiled path fails on the same input every time without advancing state → Truffle's deopt-cycle detector bails out permanently. Evidence points at an exception crossing a `@TruffleBoundary` (`transferToInterpreterOnException` defaults to true) or an implicit exception, before the body's first side effect | Diagnose with `CompilationFailureAction=Diagnose`/IGV on the single target; then guard the case or mark the boundary `transferToInterpreterOnException = false`. Add the gate of WP 2 so the class cannot return | M | 2 |
| F-6.3 | PERF | `go/values/GoMap.java:102,125,273,284`; `go/nodes/Arith.java:704,730`; `go/runtime/Values.java:94,120,232` [CODE] | PB §1.6/§6.1: boundaries on hot paths block inlining and escape analysis. Every `map[string]T`/struct-key/interface-key `find`/`put`, every `range` step over any map and every interface or struct `==` (`err == io.EOF`) is an opaque call; `Str.hashCode` (`go/values/Str.java:77`) rehashes the bytes on every lookup | Key-kind nodes chosen by the factory (string key: inline cached hash + `Arrays.equals`; scalar struct key: field loop exploded over the layout); `EqIface` as an inline cache on the dynamic type pair (limit 3, generic fallback behind the boundary); PE-visible map iterator; cache the hash in the string | M | 4 |
| F-6.4 | PERF | `go/nodes/Ctl.java:194-216` (`Repeat.strip`) [CODE] | PB §8: a custom loop must call `LoopNode.reportLoopCount`. A strip counts as one iteration in first-tier code, so hot counted loops under-report by up to 1024× and delay tier-2 of their callers | `if (CompilerDirectives.hasNextTier()) LoopNode.reportLoopCount(this, (int) (end - i));` after the strip | S | 5 |
| F-6.5 | CRASH | `go/nodes/Ctl.java:256-264` [CODE] | PB §10: a backward `goto` is a custom loop; in the interpreter it never reaches `TruffleSafepoint.poll`, so a `goto` loop cannot be cancelled or sampled until compiled (and with F-6.1 it never is) | `TruffleSafepoint.poll(this)` + `reportLoopCount` on backward jumps (subsumed by the block-dispatch node) | S | 1 |
| F-6.6 | STYLE | 31 `BranchProfile.create()` / 2 `CountingConditionProfile.create()` in `go/nodes/*` [CODE] | PB §18: allocated profiles are legacy; one object per node. Legal in hand-written nodes; footprint only | Keep until nodes become DSL nodes (F-7.1); then `Inlined*Profile` | — | 6 |
| F-6.7 | ok | `Calls.Call`, `MakeClosure`, `FuncConst`, `PrepStatic`, `FuncRoot.setRebuild` [CODE] | PB §1.3 holds: every post-construction `@CompilationFinal`/`@Child` write is preceded by `transferToInterpreterAndInvalidate` (also enforced by rule B6); no `Assumption` is used and none is needed (itabs and types are immutable). Wrapping integer ops make `rewriteOn` n/a. Jumps and returns are identity-compared preallocated `ControlFlowException`s (PB §6.3) | — | — | — |

### PB §7 — DSL

| ID | Sev | Where | Finding | Fix | Eff | WP |
|---|---|---|---|---|---|---|
| F-7.1 | PERF | 74 `@Specialization` in 3 files; 68 are single-specialization `@NodeChild` arithmetic (`go/nodes/Arith.java`) [CODE] | The DSL is used as sugar; the genuinely dynamic sites that would profit from it are hand-written or missing: interface equality, map-key hashing/equality, type assertions (`go/nodes/Ifaces.java`), receiver adaptation, `Unsafes.ToPtr`. "No `@GenerateInline` candidate" (truffle-design §16) is true of today's nodes but is the symptom: the helpers that should be `@GenerateInline @GenerateCached(false)` nodes do not exist yet and live as boundary calls (F-6.3) | Write `EqIfaceNode`, `MapKeyHashNode`/`MapKeyEqNode`, `TypeAssertNode` as inlined helper nodes with `Inlined*Profile`s (all available in 25.0.4); `@GenerateUncached` for the ones reflection and interop will share | M | 4 |
| F-7.2 | STYLE | `Calls.Dispatch`, `Calls.IfaceDispatch` [CODE] | Conform: explicit `limit = "3"`, `replaces`, `@ReportPolymorphism`, cache keyed by call target / type identity, static specializations. No `@Fallback` exists, and none is missing: operand types are static | After the 25.1+ upgrade check the single-specialization rule (PB §7: such nodes no longer act as implicit branch profiles) — none of ours relies on it [CODE] | — | — |
| F-7.3 | ok | `internal/backend/truffle/Makefile` [CODE] | `-Xlint:deprecation -Werror`: DSL processor warnings are build errors (rule D5) | Add `-Atruffle.dsl.GenerateSpecializationStatistics=true` and a `GenerateSlowPathOnly` build to the nightly matrix (§7) | S | 9 |

### PB §8 — frames, closures, loops, calls, splitting

| ID | Sev | Where | Finding | Fix | Eff | WP |
|---|---|---|---|---|---|---|
| F-8.1 | PERF | `go/values/Env.java`, `Calls.MakeClosure` (`go/nodes/Calls.java:731`) [CODE] | A closure environment is a `MaterializedFrame` created per literal evaluation: four heap objects (frame, two slot arrays, tags) to hold N boxes. The activation frame is **not** materialized (except by range-over-func, `go/nodes/RangeFunc.java`, the one `materialize()`), so amendment A9's main worry is already gone | Decide A9 as "`Closure{target, Object[] captures}`" (one array; `@CompilationFinal`-free, PEA-friendly); `Env` is the only access path, so the swap is local. SPEC §3.4 prose changes, no requirement text | S | 5 |
| F-8.2 | PERF | `go/nodes/N.java:241-336` [CODE] | Locals use tagged slots (`getLong`/`setLong`) although every slot's kind is static; no dead-slot clearing anywhere (0 hits for `clear`) → the interpreter pays a tag store per write, and a long-lived frame (a server's main loop, a parked goroutine) keeps dead references alive, which gc's liveness maps do not | Static slots (`FrameSlotKind.Static`, `*Static` accessors) for locals; `clearObjectStatic` at scope exit for reference slots the frontend knows dead; `defaultValueIllegal()` on the builder in test builds. Measure interpreter speed (E6 mode) before/after | M | 5 |
| F-8.3 | ok | `Ctl.For`, `Calls.FuncRoot` [CODE] | Loops are `LoopNode`s (OSR, Truffle safepoint at back-edges); roots poll at method end; splitting implemented by re-decoding GIR, context-free (no `GoContext` access in `go/build`, `go/loader`), so the 25.6 "`cloneUninitialized` without an entered context" change is safe | — | — | — |
| F-8.4 | GAP | [RUN E7] | Zero splits in a stdlib-heavy program (`sort.Slice`, `fmt`) is plausible for 4 s but unverified; PB §8 calls splitting essential for higher-order helpers | Run `engine.TraceSplitting` on a 60 s `textproc`/`reqpipe` run; assert `sort.pdqsort_func`-style helpers split | S | 10 |
| F-8.5 | PERF | calling convention [CODE] | Every non-inlined call boxes scalars into `Object[]` and returns `Object` (PB §9: typed `execute*` do not cross call targets). Known (performance.md §5.3); upstream improves it only in Native Image (25.5) | Bytecode DSL bake-off decides (§4) | — | 8 |

### PB §9 — values

| ID | Sev | Finding | Recommendation |
|---|---|---|---|
| F-9.1 | PERF/GAP | Strings are `Str{byte[], off, len}` (`go/values/Str.java`): no cached hash, eager O(n) concat, Java-loop compare/search, no interop string | **Prototype `TruffleString` before more string work.** Encoding `UTF_8` with byte-index operations (Go indexes bytes; broken UTF-8 is representable) or `BYTES`; gains: cached hash (maps), lazy concat (`s += x`), SIMD equality/compare/indexOf stubs on Oracle GraalVM, zero-copy `asTruffleString` for interop, `FromByteArrayNode` without copy for `unsafe.String`/`string(b)` aliasing. Costs: larger per-string header, `MaterializeNode` before byte loops, encoding must be a PE constant, the `unsafe.StringData` wrapper needs the internal array. Gate: `strbuild`, `striter`, `mapstr`, `textproc`, `regexscan` at ≥ parity. Effort M (prototype), L (migration) |
| F-9.2 | PERF | `Flat{type, long[], Object[], byte[]}`: up to four allocations and two dependent loads per field access | Keep it: Go's interior pointers, inlined arrays and `unsafe` need index-addressable regions, which `DynamicObject`/`StaticShape` fields are not, and types are static so shapes buy nothing. Do the two cheap things performance.md §5.2 lists (single-region layouts, callee-side copies) and measure header cost with compact object headers on by default. `DynamicObject` is the wrong tool here; `StaticShape` only for never-addressed scalar structs if a profile ever shows them |
| F-9.3 | GAP | `GoMap.mix` (`go/values/GoMap.java:82`) and `Str.hashCode` are unseeded | Seed per map like gc (hash-flooding of servers is in the north star's threat model, PB §13) |
| F-9.4 | GAP | No value exports `InteropLibrary`; Go `nil` is Java `null` | See F-11 |

### PB §10 — contexts, threads, safepoints, exits

| ID | Sev | Where | Finding | Fix | Eff | WP |
|---|---|---|---|---|---|---|
| F-10.1 | CRASH | `sched/SchedulerCore.java:1262` (`awaitExit` = `Future.get`), every goroutine park, `syscall/Executor`, 0 uses of `TruffleSafepoint` [RUN E3] | PB §10: blocking must use `TruffleSafepoint.setBlockedThreadInterruptible`. Today a thread-local action is never answered by the evaluating thread or by any parked goroutine: the CPU sampler gets nothing, and `Context.close(true)`, `Context.interrupt`, resource-limit cancellation and debugger suspension [INFER] wait on threads that never poll | Route `awaitExit`, the scheduler park and blocking syscalls through `setBlockedThreadInterruptible`; `TruffleSafepoint.poll` in runtime loops that can run long behind a boundary (map growth, `copy`, `bytealg`). This is the "TR-6 shrinks to `setBlocked…` parks" residue of [ADR-0013](../dev/adr/ADR-0013-a7-rejected.md): `Safepoint.poll` stays, nothing locked changes | M | 3 |
| F-10.2 | GAP | `sched/SchedulerCore.java:98,983,1004,1016,1017`; `syscall/Poller.java:96`; `language/GoContext.java:33,44`; `sched/SchedTrace.java:38` [CODE] | PB §1.7: no static mutable state. One program per process is enforced (`claimRun`), so nothing is wrong today, but `EXCLUSIVE` + statics rule out the shared-engine/multi-context matrix, engine caching and context preinitialization | TR-5 as designed, right after M3 (v1 owns half of the statics): scheduler, epoch, ticker and poller into `GoContext`; the poll reads the epoch through the thread slot; then `contextPolicy = SHARED` with the two-context test of §7. 2026-11-25 (BUG-0052, lane mem-leak): the new path's per-context statics that kept every closed context alive (`Unsafe` memo, `TimeIntrinsics.STATES`, `SyncIntrinsics.TABLE`, `GoType.IFACE_IDS`) moved to `GoType`/`GoProcess`/`TypeTable`; sequential contexts are gated leak-free by `test/m1r/leak.sh` | L | 7 |
| F-10.3 | CRASH | `language/GoLanguage.java` [CODE] | No `exitContext`: `os.Exit` ends the scheduler and the launcher calls `Runtime.halt` (`launcher/BundleMain.java:41`), which is right for the drop-in binary but in an embedder's JVM `os.Exit` must become `TruffleContext.closeExited(node, status)` and `finalizeContext` must join goroutines after cancellation. `isThreadAccessAllowed` returns true unconditionally (acceptable: Go is multi-threaded by definition — say so in the Javadoc). `createContext` does not check `env.isNativeAccessAllowed()` although syscalls go through Panama, and `SyscallIntrinsics` reads `System.getenv` instead of `env.getEnvironment()`: an embedder's policy is bypassed | Implement `exitContext`; hard-exit via `closeExited` when not the standalone launcher; check native access and use `Env` for environment/arguments | M | 3 |
| F-10.4 | GAP | carrier-mode goroutines as `jdk.internal.vm.Continuation` (brief; not in tree) [INFER] | Truffle keeps per-thread state it assumes stays on the thread that created it: the entered-context stack, `EncapsulatingNodeReference` (pushed/popped around boundary and indirect calls), pending thread-local actions, the `ContextThreadLocal` slot. A continuation that yields inside a boundary (`Chans.send`) on carrier A and resumes on carrier B pops A's state on B. Our own state is fine if `G` travels with the goroutine and `GoThreadSlot` is never held across a yield (today `Safepoint.poll` re-reads it each time) | Before building on it: a prototype gate that migrates a goroutine between carriers inside a boundary and then (a) captures a `TruffleStackTrace`, (b) runs a `ThreadLocalAction`, (c) runs under `-ea` with Truffle's assertions; save/restore `EncapsulatingNodeReference` at yield/mount; rule "no yield while a Java monitor or a Truffle boundary frame that the runtime does not own is on the stack" enforced by a debug check | M | 11 |
| F-10.5 | ok | `safepoint/Safepoint.java:61-75`, `Ctl.Repeat` [CODE] | Two polls per back-edge (ours + Truffle's) is the right end state, not debt: Truffle's addresses threads (cancel, tools), ours addresses goroutines (quantum), and under carrier mode the two stop coinciding. 25.6 removes Truffle's poll before calls in compiled code | Keep ADR-0013 | — | — |

### PB §11 — interop and host access

Current surface: none (the bundle evaluates to an `int`; 0 `@ExportLibrary`)
[CODE]. truffle-design §5 is a complete design and matches the playbook.
Minimal surface, in dependency order (TR-8, pulled after TR-5): (1) `GoPanic`
already extends `AbstractTruffleException` — add `ExceptionType`, message and
the `error` value as a member; (2) a `nil` singleton exporting `isNull`
instead of leaking Java `null` at the boundary; (3) strings (`isString`,
`asTruffleString` — free with F-9.1); (4) `Slice`/array `Flat` as array
elements, struct `Flat` as members (names from `GoType`), `GoMap` as hash
entries, `Closure` as executable (arity/type errors as `ArityException`/
`UnsupportedTypeException`), `GoType` as meta-object, pointers with
`isIdentical`; (5) the module object. Exports must be context-independent
and use `@GenerateUncached` helpers (F-7.1). `graalgo/java` (A4, decided):
keep `HostAccess` allow-lists; never `allowAllAccess`; on 25.4+ use
`allowPublicAccess(Predicate)` for the standalone allow-list. The TCK jar
(`polyglot-tck`) is pinned but `truffle-tck-tests` is not: add it with TR-8.

### PB §12 — tooling

| ID | Sev | Finding | Fix | Eff | WP |
|---|---|---|---|---|---|
| F-12.1 | GAP → **fixed** (WP-TR-AUDIT-6, ADR-0020; scopes open) | No `SourceSection`, `InstrumentableNode`, `@ProvidedTags`, `getQualifiedName`, `NodeLibrary`, `TruffleLogger`, `TruffleStackTrace`, `AllocationReporter` in `graalgo.go.*` (0 hits each) [CODE]; consequences seen in E3/E4: unusable traces, anonymous OSR targets, and no path to `--inspect`, `--dap`, `--coverage`, `--cpusampler` | First slice of TR-10 now, because every later perf lane reads these traces: sources from the bundle's file table, a section per statement/call node (compact `int` offsets, PB §4), `RootTag`/`StatementTag`/`CallTag` with `@GenerateWrapper` on `Stmt`/`Expr` bases, `FuncRoot.getSourceSection/getQualifiedName`, named OSR roots. Then `runtime.Callers`/panic traces on `TruffleStackTrace` (WP-CALLERS, BUG-0047) instead of a shadow stack | L | 6 |
| F-12.2 | GAP | `go/build/DeferPathTrace.java:15`, scheduler diagnostics write `System.err` | `TruffleLogger` for runtime diagnostics; guest-visible fatal reports stay on fd 2 (gc parity) | S | 6 |
| F-12.3 | GAP | `go test -cover`: T10 keeps gc's source rewriting — correct for drop-in; Truffle `--coverage` becomes a free extra once F-12.1 lands | — | — | — |

### PB §13 — sandbox and limits

Stance to write down (SPEC §4.3 addendum): the standalone `go` replacement is
`SandboxPolicy.TRUSTED` by construction — real file descriptors through
Panama are the product. `os.Stdout` therefore bypasses `env.out()`; only
`println` and fatal reports use `env.err()` (`go/runtime/GoProcess.java:52`).
Embedding mode needs one option (`go.Stdio=fd|env`, default `env` when the
embedder redirected streams) so fds 0–2 map to `Env` streams, and must refuse
contexts without native access (F-10.3). Never declare a `sandbox =` above
`TRUSTED` until syscalls are mediated. Allocation caps for `make`/`append`
exist (`Data.maxElems`); statement limits need `StatementTag` (F-12.1);
unseeded hashes are F-9.3.

### PB §14 — Native Image / AOT

| ID | Sev | Finding | Fix |
|---|---|---|---|
| F-14.1 | GAP | `syscall/Executor.java:48-57` builds a Panama downcall handle in a static initializer; `SchedulerCore`/`Poller` statics hold threads and an epoll fd | Runtime-initialize (falls out of F-10.2); register the downcall in `reachability-metadata.json` (foreign section) |
| F-14.2 | GAP | The runner is class-path (`internal/backend/graalgo-bundle-run`), with `--enable-native-access=ALL-UNNAMED` | Package `graalgo` as a JPMS module providing `TruffleLanguageProvider` at M3 (PB §14); native access granted to the named module only |
| F-14.3 | GAP | Carrier-mode continuations need `--add-exports java.base/jdk.internal.vm`; an internal API on HotSpot and a different implementation in SubstrateVM | Record as an ADR risk with a re-test at every pin bump; keep the virtual-thread path selectable until WP-AOT measures both |
| F-14.4 | GAP | No `patchContext`, no `prepareForAOT`, no image-size/RSS tracking | WP-AOT entry criteria, after F-10.2 |

### PB §16 — performance method

| ID | Sev | Finding | Fix |
|---|---|---|---|
| F-16.1 | GAP | `make bench-realistic` runs 4 s per program (`h.MinTotal`): 10–17 rounds, a third of compilations at tier 2, rounds still varying 5× (E1). "Steady median" and "time to peak" are warmup numbers; interpreter-only is only 3–6× slower in that window (E6) | Report three regimes separately (PB §16.1): first round, warmup curve, **peak after the compilation queue is idle** (≥ 60 s or until `TraceCompilation` is quiet for 5 s); print `opt failed`/`opt deopt` counts and compile CPU next to every number; keep fresh processes and medians |
| F-16.2 | GAP | Diagnosis steps never run on the realistic set: sampler after warmup (blocked by F-10.1), `TraceSplitting`, `InstrumentBoundaries`, expansion statistics, IGV | Order: WP 3 (sampler works) → WP 6 (frames have names) → profile `textproc` (no failures, still 17×) with the sampler; expected top items are F-6.3 and F-9.1, to be confirmed, not assumed |
| F-16.3 | GAP | `Runtime.halt` in the launcher drops exit-time reports (`InstrumentBoundaries`, `CompilationStatistics`) | A `GRAALGO_DIAG=1` launcher mode that closes the engine and exits normally |

## 4. If we started clean (greenfield stance)

- **Latest Truffle line, not the LTS** (§5), stepping one release per lane-day.
- **Bytecode DSL as the execution model, decided by a bake-off, now rather than "after M1d"** (TR-13 pulled forward, after the upgrade): the playbook's criteria for it (PB §3.2: large code base of mostly cold functions, AST memory pressure, interpreter speed, complex control flow, an existing IR) describe a Go runtime exactly — 0.8–1 GB RSS, an interpreter ~100× gc, `goto` state machines, GIR already an IR; boxing elimination covers our `long`/`double`/`boolean` carriers and tags/source maps come with the framework. It is documented as experimental, so the bake-off (three realistic programs: parse time, retained heap, interpreter and peak speed, compile time) decides; SPEC §3.3/GG-0014 ("one AST") would need amendment A5.
- **`TruffleString` for `string`** (F-9.1) and inlined DSL helper nodes for every dynamic site (F-7.1).
- **`SHARED` policy and no statics from day one** (F-10.2); AST is already context-free.
- **One blocking primitive** (`setBlockedThreadInterruptible`) under every park (F-10.1).
- **Source sections and tags in the first node** (F-12.1).
- **Delete v1 now**: `GoLanguage` still defaults to the v1 MIME type and imports `graalgo.frontend`; every upgrade, test mode and context change is otherwise paid twice. M3 is the cheapest enabling step for §6's WP 7, 8 and 12.
- Keep: the flat value model (F-9.2), factory-selected typed nodes, target-keyed call caches, our own goroutine poll (F-10.5), gc's cover rewriting.

## 5. Version check: 25.0.4.1.1 vs the playbook's §18

| API / behavior | In 25.0.4 jars [RUN E8] | Note |
|---|---|---|
| `Inlined*Profile`, `@GenerateInline`, `@Idempotent`, `TruffleString`, `StaticShape`, Bytecode DSL, `BytecodeOSRNode`, `TruffleSafepoint.setBlocked*`, `CompilerDirectives.hasNextTier/castExact`, deopt-cycle detection | yes | nothing above blocks WP 1–6 |
| `FrameDescriptor.Builder.defaultValueIllegal()` | yes | the playbook names it `illegalDefaultValue()`; the pinned name is `defaultValueIllegal` |
| `DynamicObject.GetNode…`, `InteropLibrary.hasLanguageId`, `Engine.supportsCompilation()`, `HostCompilerDirectives.InliningRoot`, `CompilerDirectives.EarlyInline`, `TruffleString.MaterializeSubstringNode` | no (25.1+) | use `hasLanguage`/`getLanguage` only if interop lands before the upgrade — better: upgrade first |
| `mergeExplodeKey`, aux engine cache in CE, tail-duplicated bytecode handlers | no (25.5+) | relevant to F-6.1 and the bake-off |
| legacy table (PB §18) | clean except allocated profiles (F-6.6) | no `FrameSlot`, `createCallTarget`, `@CachedContext`, `DynamicObjectLibrary`, `TruffleException` |

**Upgrade (F-18.1).** The playbook: the optimizing runtime needs GraalVM
25.1+ except on the 25.0 LTS, and upgrades go one release at a time. Plan:
25.0.4 → 25.0.5 (CPU) → 25.1 → … → current (25.5.x; 25.6 when verified), one
commit per step: bump the toolchain JDK and all jars together
(`SHA256SUMS.txt`), rebuild with `-Xlint:deprecation -Werror`, run
`make all` + the nightly matrix + `bench-suite` baseline. Unlocks: constant
options, `InliningRoot`/`EarlyInline` for the poll and `Flat` accessors,
Bytecode DSL maturity (threaded switch, tracing), `supportsCompilation`
diagnostics, CE isolates/sandbox, IR-frequency inlining (25.4), aux engine
cache for warmup (25.5), no pre-call safepoint poll (25.6). Risks: virtual
thread behavior under polyglot contexts changes between releases (re-run the
scheduler torture set per step); 25.4 removes deprecated APIs; 25.1's
single-specialization rule; monthly cadence means a permanent small tax.
Cost of staying: every item above waits, and the LTS only gets CPU fixes.

## 6. Work packages (proposed; the coordinator decides)

| WP-TR-AUDIT- | Scope (findings) | Lane | Eff | Gate (must be able to fail) | Fits |
|---|---|---|---|---|---|
| 1 | `goto` block dispatch (F-6.1, F-6.5) — **done (lane perf-pe, 2026-11-24)**: the label dispatch continues the merge-exploded loop from inside each label branch, so the successor pc is a constant ([performance.md](performance.md) §8.1) | perf-pe | M | 0 `opt failed` on `regexscan`, `jsonrt`, `deflate` (met: 0 on all six pipelines); `pe-check-m1a` runs [pe_std.sh](../../test/m1a/pe_std.sh) over the std `goto` paths (with the fix reverted it fails on `jsonwire.ConsumeNumberResumable`) plus a planted compilation failure; m1a `goto` programs | WP-PERF |
| 2 | Deopt-cycle root cause + compile-health gate: realistic set under `CompilationFailureAction=ExitVM`, fail on any `opt failed` or > N deopts of one target (F-6.2) — **done (lane perf-pe)**: the cause was the strip-mined loop's `limit` node, executed only in compiled code, so its DSL state never left uninitialized (the interpreter now tests `idx < limit` too); nightly [compile-health.yml](../../.github/workflows/compile-health.yml) (`make pe-report --max-churn 15`) | perf-pe | M | nightly; `tmplrender` green | WP-PERF |
| 3 | Truffle cooperation: `setBlockedThreadInterruptible` under parks/`awaitExit`/syscalls, `exitContext`, native-access and `Env` checks (F-10.1, F-10.3) — **done except the `Env` environment (lane tr-safepoint, 2026-11-24, [ADR-0019](../dev/adr/ADR-0019-truffle-cancellation-exit.md))**: every blocking wait of a polyglot thread is Truffle-blocked (idle/pinned carriers, thread-mode parks, system-call wait, pin lock); may-block system calls are cancellable (call-owned buffers, abort `eventfd`); `os.Exit`/panic/fatal are `closeExited` hard exits (`PolyglotException.isExit`), `exitContext` implemented, native access required. Measured: sampler 232 samples / 7 missed (carrier), 21k / 8 (thread), was 0 recorded; `close(true)` 124–242 ms with 10k parked + read(2)-blocked goroutines; exit 42–200 ms. No extra `TruffleSafepoint.poll` in our slow path: not needed (mutation stayed green). Open: `openat`/`fsync` and >`PIPE_BUF` writes are abandoned, not interrupted; environment still from `/proc/self/environ` | sched | M | sampler records > 0 samples; `Context.close(true)` during `for {}` and during a blocked receive returns < 1 s; scheduler torture unchanged | residue of TR-6 |
| 4 | Hot boundaries → inlined DSL helper nodes; cached string hash; seeded hashes (F-6.3, F-7.1, F-9.3) | perf-peak | M | `mapstr`, `textproc` improve; T4-style tests (mono/poly/mega) per new node; perf-warning gate stays 0 | WP-PERF, WP-NODETEST |
| 5 | Loop/frame hygiene: `reportLoopCount`, static slots, dead-slot clearing, `Object[]` closure environments (F-6.4, F-8.1, F-8.2) | perf-peak | M | interpreter-only round time; `closures` benchmark; a weak-reference liveness test | WP-PERF; **decision A9** |
| 6 | Source sections, tags, names, `TruffleLogger`; then `TruffleStackTrace` for `runtime.Callers` (F-12.1, F-12.2) — **done except `TruffleLogger` (F-12.2) and the debugger's scopes (lane tr-sources, 2026-11-25, [ADR-0020](../dev/adr/ADR-0020-sources-stacks.md))**: GIR's per-body position table feeds character-less `Source`s and on-demand sections (an `int` index in statement and call nodes); gc root names; `StatementTag`/`RootTag`/`RootBodyTag`/`CallTag` with generated wrappers; `runtime.Caller`/`Callers`/`CallersFrames`/`FuncForPC`/`Stack` and fatal tracebacks from Truffle frames (BUG-0047 fixed). Gates: [tools.sh](../../test/m1r/tools.sh) (exact `ExecutionListener` statement/root counts, a collecting `CPUSampler` with Go names and sections — carrier mode, BUG-0050 in thread mode), the `traceback_*` programs of test/m1a (frames compared), [callers.go](../../test/m1r/programs/callers.go), reflect_probe's `panic.unexported-set`. Open: boundary runtime frames, `created by`, all-goroutine dumps, `ExpressionTag`, scopes for `--inspect`/`--dap` | tooling | L | `--cpusampler` output names Go functions with file:line; inspector breakpoint test; BUG-0047 closed | TR-10, WP-CALLERS |
| 7 | Context-ification and `SHARED` (F-10.2, F-14.1) | runtime | L | two contexts on one engine, sequential and concurrent, no leak, no extra deopt in the second | TR-5 (after M3) |
| 8 | Truffle upgrade to the current line (F-18.1) | infra | M | each step: `make pre-land` + nightly matrix + bench baseline within noise | new; **decision** |
| 9 | Test-mode matrix of §7 | test | M | each mode proven able to fail by a planted fault | WP-NODETEST |
| 10 | Benchmark method: peak vs warmup, compile health in the report, long runs, splitting check (F-16.1–3, F-8.4) — partly done (perf-pe): `--min-total S` long runs and the 4 s vs 20 s regimes reported separately in [performance.md](performance.md) §8.1; `make pe-report` for compile health | perf | S | report shows three regimes; rerun of north-star §4.1 | WP-PERF |
| 11 | Carrier-mode Truffle-state prototype gate (F-10.4, F-14.3) | sched | M | the three checks of F-10.4 | precondition of the carrier-mode lane |
| 12 | `TruffleString` prototype; Bytecode DSL bake-off (F-9.1, §4) | research | M+L | written comparison with numbers; go/no-go | TR-13; **decisions** |
| 13 | Minimal interop + TCK (F-11) | interop | L | TCK green; one test per message row | TR-8 (after 7) |

Suggested order: 1, 3, 2, 10 (unblock measurement) → 6, 4, 5 → 8 → 12 → M3 → 7 → 13; 11 before any carrier-mode code.

**Decisions requested** (each conflicts with locked text or an accepted plan):

1. **Leave the LTS line** (WP 8). Recommend yes: unreleased project, the features we need are 25.1+, stepping cost grows monthly.
2. **A9 closures**: accept as `Object[]` capture arrays (WP 5). Evidence: F-8.1 — the as-built already avoids frame materialization; the remaining cost is four objects per closure instead of one. SPEC §3.4 prose only.
3. **String representation** (SPEC §3.4, ADR-0015's byte region): authorize the `TruffleString` prototype and accept an amendment if it wins.
4. **Pull TR-13 (Bytecode DSL bake-off) forward** to right after the upgrade; touches GG-0014/A5 only if it wins.
5. **Pull the first slice of TR-10 forward** (WP 6) ahead of more perf lanes; it is what makes PB §16's diagnosis order executable.
6. **M3 cutover priority**: treat v1 deletion as an enabler (WP 7, 8, 12), not a milestone to arrive at.
7. **Carrier-mode gate** (WP 11) as an acceptance condition of that ADR.
8. **ADR-0013 stays** (F-10.5): no change requested; WP 3 is additive.

## 7. Test matrix (PB §15) and plan

Owner rule: per-push time must not grow. Per-push additions below total
≈ 20 s [EST] and are offset by WP 9 moving `pe-check-m1a`'s trace run into
the same JVM invocations as `test-m1a` where possible; everything else is
nightly or weekly. Durations: one realistic program run ≈ 6 s wall [RUN];
corpus-wide numbers are estimates to be replaced by measurements in WP 9.

| Mode (PB §15) | Today | Plan | Tier | Cost |
|---|---|---|---|---|
| Default tiered + OSR | all corpora (`test-m1a/m1r/m1c/m1d`) | keep | per-push | unchanged |
| Compile health (`CompilationFailureAction=ExitVM`) | `pe-check-m1a` (micro programs only) | add 3 realistic programs to nightly with the deopt-count check (WP 2) | per-push + nightly | +0 / ~1 min |
| Interpreter only (`engine.Compilation=false`) | never | 5 m1a programs per push; whole corpora nightly | per-push + nightly | ~8 s [EST] / ~1× corpus |
| Compile everything (`CompileImmediately`, `BackgroundCompilation=false`, `Throw`) | never on the new path (v1 `test/jit` only) | m1a + m1r nightly; this is the invalidation net for lazily resolved caches (`Call`, `FuncConst`, `MakeClosure`) | nightly | ~3–5× corpus [EST] |
| PE hygiene (`TreatPerformanceWarningsAsErrors=all`) | static only (`NodeConventionsTest`, 0.3 s) | realistic set nightly; clean today on `textproc` (E5) | nightly | ~30 s |
| Safepoint stress (`engine.SafepointALot`, `TraceMissingSafepointPollInterval=20`) | never | m1c (scheduler, syscalls) nightly, after WP 3 | nightly | ~1× m1c |
| Cancellation / close while running | per-push since WP 3: `test/m1r/cancel.sh` (both modes, ~7.5 s) | one JUnit class, 4 cases (spin loop, blocked receive, blocked syscall, `os.Exit`), condition waits, one JVM | per-push | ~3 s [EST] |
| Sampler smoke (samples > 0, names resolved) | never | 1 program | per-push | ~6 s, or nightly if over budget |
| Forced splitting (`-Dgraalgo.go.forceSplit=true`) | manual | nightly on m1a/m1r | nightly | ~1× corpus |
| Invalidation tests (mutate after warm-up) | none; Go has no redefinition, so the class is "cache populated after first compile" and "second receiver type after warm-up" | 6 table-driven programs in m1a (interface site mono→poly→mega after 10⁵ calls, func value retargeted, map key type via `any`) under compile-immediately | nightly | in the row above |
| Specialization statistics / `GenerateSlowPathOnly` build | never | weekly build variant over m1a | weekly | 1 extra build + corpus |
| Shared engine, ≥ 2 contexts | impossible (F-10.2) | after WP 7 | weekly, then per-push smoke | — |
| Fallback runtime (stock JDK, no JIT) | never | m1a smoke weekly — also the honest floor for GG-0050 | weekly | ~10× corpus slice |
| TCK | jar partly pinned | after WP 13 | weekly | — |
| Native Image | none | WP-AOT | weekly | — |
| Multi-context leak / create–close loops | none | after WP 7: 100 create/eval/close cycles, heap delta bound | nightly | ~20 s |

Every new gate ships with its planted violation (AGENTS §5): a seeded
missing safepoint for the stress mode, a seeded non-constant for compile
health, a seeded uninterruptible park for cancellation.
