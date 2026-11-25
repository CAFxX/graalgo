# TruffleString and the Bytecode DSL: two go/no-go studies (WP-TR-AUDIT-12)

**Date:** 2026-11-25 · **Lane:** research-tsbc · **Work package:** WP-TR-AUDIT-12
of [truffle-audit.md](truffle-audit.md) (F-9.1, §4) · **Decisions:** proposed in
[ADR-0021](../dev/adr/ADR-0021-truffle-string.md) and
[ADR-0022](../dev/adr/ADR-0022-bytecode-dsl.md) (status PROPOSED; the
coordinator decides) · **Playbook:** `implementing-truffle-languages` §3.2,
§8, §9, §16, §18.

Both studies were run against the pinned Truffle jars (25.0.4.1.1) on
Oracle GraalVM JDK 25, on the shared host (load average 7–12 throughout),
every JVM under `nice -n 19` with `-Xmx2g -XX:ActiveProcessorCount=4`, one
JVM at a time. Times are **main-thread CPU time**; each number is the median
of 3 JVM runs of a median of 5–7 timed samples (JIT legs: synchronous
compilation, `engine.BackgroundCompilation=false`, warmed until the last
tier). graalgo's own spread under this load is ±20–40 %
([performance.md](performance.md) §2): differences under 1.3× are noise.

## Summary

| Study | Recommendation | Main reason | Cheap wins taken out of it |
|---|---|---|---|
| A. `TruffleString` for `string` | **Reject as the representation** (re-open only for a polyglot-interop milestone) | Std is compiled from upstream source, so only the runtime's own operations (`==`, `<`, `+`, hashing, conversions, `range`, the `internal/bytealg` intrinsics) touch the representation; TS beats `Str` on exactly the ones a 30-line change gives `Str` too, and costs +8 B per string, 2–20× slower ops before JIT, and an L migration through `unsafe`/`reflect`/descriptor images | **Adopt** a cached hash in `Str` (free: it fits the object's padding; `mapstr` 2.0× faster in the product) and TS search kernels over zero-copy views in the bytealg intrinsics (`IndexString` 7×, `IndexByteString` 3.4× in isolation) |
| B. Bytecode DSL execution model | **Reject now; re-evaluate only after WP-TR-AUDIT-8 (≥ 25.6) and only if a heap census shows AST bytes dominate** | On the slice it brings no interpreter-speed win (before JIT the AST is 1.5–2.5× faster on 3 of 5 kernels in the realistic polluted-profile case, and first calls are 2–12× slower in bytecode), peak parity at best (9× slower on `defer`/`panic`: a deopt cycle), 20 % less heap only for functions called fewer than ~16 times, no backward branches (Go `goto`), and an XL migration that would also lose strip-mining and the merge-exploded `goto` dispatch | none needed |

## A. TruffleString

### A.1 Where the representation matters in the new path

Strings are `Str { byte[] b; int off, len; Flat flat }`
([Str.java](../../internal/backend/truffle/src/main/java/graalgo/go/values/Str.java)):
a window into a byte array, never written through, with the `unsafe.StringData`
storage cached in `flat` (ADR-0015 decision 3, SPEC §3.4). The new path compiles
`strings`, `bytes`, `strconv`, `unicode/utf8`, `fmt` … **from upstream
source**: `strings.Index`, `Fields`, `ToLower`, `Builder` are Go code over
bytes. The representation is touched only by the runtime's own operations:

- `==`/`!=`/`<` (`Str.eq`/`Str.cmp`: `Arrays.equals`/`compareUnsigned`, already
  vectorized intrinsics in the compiler), `+` (`Str.concat`, eager copy),
  map hashing (`Str.hashCode`, a byte loop recomputed on every lookup; `GoMap`
  stores the hashes of its keys, so only probe keys pay it);
- `string(b)`/`[]byte(s)` (one `arraycopy` each), `s[i:j]` (a new window),
  `s[i]`, `range s` (`Strs.decode` per rune);
- the intrinsics `runtime.cmpstring`, `internal/bytealg.IndexByteString`,
  `IndexString`, `CountString`
  ([BytealgIntrinsics.java](../../internal/backend/truffle/src/main/java/graalgo/go/intrinsics/BytealgIntrinsics.java),
  SWAR loops behind `@TruffleBoundary`).

So "use TruffleString" buys at most what TS does better on that list.

### A.2 Prototype

A standalone harness (deleted after the study; ~600 lines) ran each operation
as a Truffle root compiled by the optimizing runtime, in four representations:
**CUR** (verbatim copies of the product's `Str`, `Strs.decode` and bytealg
code, boundaries included), **CUR+** (CUR plus a cached hash and the searches
inlined), **TS** (`TruffleString`, cached nodes as `@Child`, encoding a
`static final` PE constant: `UTF_8` and, separately, `BYTES`), and **KERN**
(CUR+ whose search and decode kernels run TS nodes over a transient zero-copy
view, `FromByteArrayNode(b, off, len, enc, copy=false)`, of the `Str`'s bytes).

### A.3 Operation costs (ns per operation)

JIT = steady state after the last tier; "pre-JIT" = `engine.Compilation=false`
(the nodes run as host-compiled Java: what a function pays before Truffle
compiles it).

| Operation | CUR | CUR+ | TS `UTF_8` | TS `BYTES` | KERN | pre-JIT CUR | pre-JIT TS |
|---|---:|---:|---:|---:|---:|---:|---:|
| `==`, 4–16 B | 4.7 | 4.1 | 4.6 | 6.5 | | 13 | 124 |
| `==`, 200–256 B | 8.1 | 8.8 | 8.6 | 9.9 | | 13 | 43 |
| `<`, 4–16 B | 2.3 | 2.6 | 2.2 | 3.1 | | 24 | 46 |
| hash, same object, 16 B | 6.4 | **0.9** | 0.9 | 1.1 | | 14 | 16 |
| hash, same object, 256 B | 12.1 | **0.9** | 1.5 | 1.2 | | 142 | 18 |
| hash of a fresh slice, 16 B | 6.8 | 6.5 | 5.3 | 4.8 | | 23 | 183 |
| word count (split + map probe per word) | 32.0 | 24.0 | 25.3 | 28.3 | | 91 | 281 |
| 64 × `s += "abcdefgh"` (whole chain) | 1240 | 1279 | **527** | 750 | | 11079 | 25119 |
| `s[1:n-1]` + two byte reads | 1.1 | 1.3 | 2.0 | 1.8 | | 26 | 149 |
| `IndexString`, 1 KiB, 8 B needle | 215 | 293 | **29** | 24 | **34** | 2178 | 2007 |
| `IndexByteString`, 256 B | 28.7 | 27.8 | **6.9** | 4.4 | **7.5** | 44 | 1004 |
| `string(b)` + `[]byte(s)`, 40–120 B | 22.7 | 22.4 | 19.6 | 15.2 | | 139 | 287 |
| `for i, r := range s`, 4 KiB mixed UTF-8 | 8712 | 9068 | **4658** | n/a | 7381 | 18849 | 136208 |

Reading: TS wins big only on **searches** (its compiler stubs), **lazy
concatenation** and **rune iteration**; the searches are just as fast through
a zero-copy view of a `Str` (KERN); the hash win is the cache, which `Str`
gets for free (CUR+). Equality, ordering, slicing and conversions are at
parity or slower. Before JIT, TS operations are 2–20× slower than `Str`'s
(cached-node dispatch in the interpreter) — a warmup cost on every string
operation of every not-yet-compiled function.

### A.4 Memory

Retained bytes per string (1 M strings, heap delta after GC):

| Length | `Str` (own array) | TS `UTF_8` | TS `BYTES` | `Str` window over a shared array | TS lazy substring |
|---:|---:|---:|---:|---:|---:|
| 0 | 48 | 57 | 57 | 32 | 0 (singleton) |
| 8 | 57 | 65 | 65 | 32 | 40 |
| 64 | 112 | 120 | 120 | 32 | 40 |

TS costs +8 B per string. The product's `Str` is 28 B rounded to 32, so an
`int` hash field fits the padding at **zero** cost.

### A.5 In the product (flagged experiment, reverted)

Two changes behind system properties (not committed): `Str.hashCode` caches
its result in a new `int` field (`-Dgraalgo.exp.strhash`), and the
`IndexByteString`/`IndexString` intrinsic roots run
`ByteIndexOfAnyByteNode`/`ByteIndexOfStringNode` over zero-copy `BYTES` views
(`-Dgraalgo.exp.tskern`). `make bench-suite`/`bench-realistic` runner, both
legs interleaved in one run via `--gg-variant`, 3 reps, best round (ms):

| Benchmark | gc | graalgo | graalgo + experiment | change |
|---|---:|---:|---:|---|
| `mapstr` | 0.85 | 1.27 (1.5× gc) | **0.62 (0.73× gc)** | **2.0× faster** (lookups reuse key objects: the cached hash) |
| `striter` | 1.34 | 5.20 | 5.19 | none (its cost is per-rune `range` and `Count`) |
| `strbuild` (control) | 0.34 | 0.71 | 0.72 | none |
| `textproc` (realistic) | 4.38 | 7.56 | 7.58 | none (probe keys are fresh `ToLower` results) |
| `regexscan` (realistic) | 2.01 | 12.16 | 11.94 | none |
| `jsonrt` (realistic) | 2.12 | 18.25 | 16.75 | noise (medians 28.7 / 28.7) |

So on the realistic set the string representation is not where the time is;
`striter`'s 4× gap is the per-rune `range` node (performance.md §5 item 4),
which a better decode loop fixes in either representation.

### A.6 Semantics and the pointer model

- **Invalid UTF-8**: TS's `UTF_8` iterator with `ErrorHandling.BEST_EFFORT`
  matched Go's `range` (U+FFFD, width 1 per bad byte) on all 17 probe
  vectors (truncated 2/3/4-byte sequences, overlongs, surrogates, > U+10FFFF,
  lone continuation bytes, `ff`); arbitrary bytes round-trip exactly in both
  encodings; `CompareBytesNode` orders bytes unsigned like Go. A broken
  string is code range `BROKEN` under `UTF_8`: representable, but every
  `UTF_8` creation pays a code-range scan (the conversion and concat rows).
- **Addressable bytes** (`unsafe.StringData`, `unsafe.String`, reflect,
  GG-0072/ADR-0015): compatible in principle — `FromByteArrayNode(copy=false)`
  aliases the caller's array and `GetInternalByteArrayNode` returns
  (array, offset) of a lazy substring without copying. But a lazy
  concatenation must be materialized before its bytes have an address, and
  every `Flat` that today wraps `Str.b` would have to obtain the array
  through a node (the conversions, `reflect`, the syscall executor's byte
  roles and descriptor images all read `Str.b` directly today).
- **Aliasing hazard** (both TS and the cached hash): mutating bytes that an
  `unsafe.String` aliases leaves a stale cached hash (probe: hash unchanged,
  `==` sees the new bytes). Go forbids that mutation ("the bytes passed to
  String must not be modified as long as the returned string value exists")
  and gc's maps store hashes too, so it is undefined behavior either way;
  `strings.Builder` (the main `unsafe.String` user) never rewrites exposed
  bytes.

### A.7 Interop

Zero-copy `asTruffleString` is the one capability `Str` lacks — and a view
(`FromByteArrayNode(b, off, len, UTF_8, false)`) gives it at the interop
boundary without changing the representation. WP-TR-AUDIT-13 can use that.

### A.8 Migration cost (if adopted anyway)

L (≈ 3 lane-weeks): the 21 files of `graalgo.go` that use `Str`, the GIR
constant pool, `Flat`/`unsafe`/`reflect`/syscall byte roles, descriptor
images, and every static runtime helper that takes a `Str` becoming a node
(TS operations are nodes; uncached TS calls are slow). Gate:
`strbuild`, `striter`, `mapstr`, `textproc`, `regexscan` at ≥ parity — which
the numbers above say it would not beat the cheap path on.

## B. Bytecode DSL

### B.1 What the pinned 25.0.4.1.1 offers

`@GenerateBytecode` with boxing elimination, quickening, an uncached first
tier, `@ConstantOperand`, `@Variadic`, `TryCatch`/`TryFinally`/
`TryCatchOtherwise`, built-in `While` with back-edge OSR and safepoint
polling, lazily parsed source information (`BytecodeConfig.DEFAULT` vs
`WITH_SOURCE`), tags, serialization and `@Yield` continuations are all in the
pinned jar; the slice compiled and ran unchanged on it. Later releases add
(playbook §18): threaded switch, `@StoreBytecodeIndex`, `BytecodeFrame` (25.1);
multi-operand `@Yield` (25.2); metadata-only sources (25.3); `StackValue`s,
compressed sources (25.4); tail-duplicated handlers, `unwindExceptions`,
`mergeExplodeKey` (25.5); `BytecodeIndexMode` and an `AbstractTruffleException`
bytecode-index constructor (25.6).

### B.2 Prototype

One IR (a GIR stand-in: constants, `long` locals, arithmetic and comparisons,
direct calls, closures over cells, `switch`, `defer`/`panic`/`recover` with a
named result) built two ways: an **AST** in graalgo's shape (typed `execute*`,
`LoopNode`/`RepeatingNode` with the scheduler poll at the back-edge, a
monomorphic closure call cache, `ControlFlowException` returns, an exploded
`switch`, the `DeferringRoot` epilogue), and a **Bytecode DSL** root (25
operations: typed `@Specialization`s, `Invoke`/`CallClo` with a cached
`DirectCallNode` and an `IndirectCallNode` fallback, `Poll` emitted at the end
of every loop body, `switch` as an if-else chain on a local, defers as
`TryCatchOtherwise`). Kernels: `loop` (Σ i²%7), `fib(27)`, `closures` (a
captured counter), `switch` (5 arms), `defer/panic` (a deferring call per
iteration, 1 in 8 panics and is recovered). Results were checked against Java
references. Host-JIT call-site profiles were polluted with 40 random
expression programs before measuring (a large program's AST call sites are
megamorphic; the slice's alone are not), except where noted.

### B.3 Numbers

**Build and footprint** (2,000 copies of the 8 functions; per function):

| | build from IR (µs) | retained after build (B) | after 1 call (B) | after 21 calls (B) |
|---|---:|---:|---:|---:|
| AST | 1.6–2.4 | 1,000 | 1,165 | 1,165 |
| Bytecode, `DEFAULT` (no source info) | 2.8–3.5 | **792** | **930** | 1,246 |
| Bytecode, `WITH_SOURCE` | 2.2–2.6 | 1,022 | 1,158 | 1,510 |

Bytecode is 20 % smaller while a function runs in the uncached tier (fewer
than ~16 calls/back-edges) and 7–30 % *larger* once it specializes (cached
nodes plus the quickened copy). graalgo already decodes a function's GIR and
builds its AST lazily at the first call (`FunctionUnit`), so never-called
functions cost nothing in either model.

**Speed** (ns per work unit: loop iteration, fib call, closure call, switch
iteration, deferring call; first-call cost in brackets):

| Kernel | AST pre-JIT | BC pre-JIT | AST pre-JIT, polluted | BC pre-JIT, polluted | AST peak | BC peak |
|---|---:|---:|---:|---:|---:|---:|
| `loop` | 16.7 [118] | 18.8 [609] | 13.4 | 20.4 | 0.62 | 0.72 |
| `fib` | 273 [802] | 96 [2579] | 306 | 594 | 1.03 | 0.98 |
| `closures` | 194 [359] | 148 [812] | 218 | 188 | 0.39 | 0.32 |
| `switch` | 14.6 [29] | 57.3 [362] | 32.0 | 80.2 | 0.80 | 0.75 |
| `defer/panic` | 587 [1122] | 400 [2223] | 516 | 497 | 14.1 | **128.6** |

- **Pre-JIT**: no consistent interpreter win; the polluted runs (the
  realistic case) favor the AST on 3 of 5 kernels. First calls are 2–12× slower
  in bytecode (uncached tier, then a cold 3,247-byte `continueAt`). Pre-JIT
  numbers vary up to 2× between runs on this host.
- **Peak**: parity on four kernels. `defer/panic` is 9× slower because the
  deferring function and its caller **deopt-cycle**: 23 + 16 deopts, then
  "Deopt taken too many times" bailouts and 8–16 recompilations — also with
  no panic at all on the hot path, and with the final `return` falling
  through instead of branching out of the handler. The only traced
  transfer sites were normal quickening and frame-tag checks
  (`FrameWithoutBoxing.unsafeVerifyIndexedGetUnexpected`); not root-caused
  (a DSL usage error in the prototype or a 25.0 issue): it would have to be
  before any adoption.
- **OSR** (one call of `loop` with 2·10⁸ iterations): AST 1.26–1.53, BC
  1.45–1.55 ns/iteration; both OSR-compile.
- **Compilation** (last tier, per root): parity on IR size, code size and
  time (`loop` 516 vs 493 IR nodes, 2,470 vs 2,355 B; `closures` 1,981 vs
  2,013 B), except `fib` (bytecode inlined deeper: 2,381 vs 1,214 IR nodes,
  10,019 vs 5,112 B, 42 vs 15 ms) and the recompiling deferring roots.

### B.4 Stack traces, sources, instrumentation

- AST: every frame of an escaping panic resolves to its statement's line.
- Bytecode with `DEFAULT`: frames carry a bytecode index; lines need
  `ensureSourceInformation()` (a reparse). The deferring frame resolved to the
  **root's** section: the exception left that frame from the `catch`
  handler's re-throw, so its index is the handler's, not the `panic`'s. 25.6's
  bytecode-index constructor (or `storeBytecodeIndexInFrame` plus capturing at
  the throw) is the fix; on 25.0 it is a traceback regression against
  ADR-0020's exact positions.
- Tags: free with `enableTagInstrumentation` (not measured); the AST has none
  yet (F-12.1), which is an argument for bytecode only if tooling becomes a
  priority.

### B.5 How graalgo's constructs map

| Construct | Today (AST) | Bytecode DSL 25.0 |
|---|---|---|
| goroutines, carrier mode (`jdk.internal.vm.Continuation`) | yields the whole Java stack | unchanged: bytecode frames are Java frames; nothing pins. `@Yield` is not needed for goroutines (it could implement `iter.Pull`-style coroutines and range-over-func without a thread) |
| scheduler poll | `Safepoint.poll` at back-edges and entries; counted loops poll once per **strip** | an explicit `Poll` operation per loop body (the built-in back-edge only polls `TruffleSafepoint`); **strip-mining is not expressible** — the shape of the compiled loop is the DSL's, and strip-mining is what lets Graal vectorize `loop` (0.2× gc, performance.md §4.1) |
| `goto` | `GotoBlock`: a merge-exploded program counter, constant per branch in compiled code | **backward `Branch` is rejected by the builder** ("Backward branches are unsupported. Use a While operation"): `goto` state machines (regexp's backtracker, `flate`, the JSON scanner) become `While` + a dispatch on a local, whose value compiled code cannot fold (no `mergeExplodeKey` before 25.5) |
| `defer`/`panic`/`recover` | `DeferringRoot` epilogue (try/catch around the body) | `TryCatchOtherwise`; early returns become branches to an exit label so named results are read after the defers; see the deopt cycle above |
| `switch` | exploded key loop | if-else chain on a local (no switch operation) |
| calls | `Object[]` boxing (F-8.5) | the same `Object[]` convention |
| strings, values | flat value model | unchanged (operations take the same Java objects) |

### B.6 Risks specific to graalgo

- **Interpreter method size on HotSpot**: graalgo runs on HotSpot, not Native
  Image. The slice's 25 operations already produce a 3,247-byte
  `continueAt`; graalgo's ≈ 260 node classes (plus ≈ 220 intrinsic roots) would pass HotSpot's
  8,000-byte `HugeMethodLimit`, and a method over it is never host-compiled
  unless `-XX:-DontCompileHugeMethods` is set (to verify on a real operation
  set; 25.1's threaded switch and 25.5's tail duplication change the shape).
- **API stability**: documented as experimental; every Truffle step of
  WP-TR-AUDIT-8 would touch the generated interpreter (each of the six feature releases
  25.1–25.6 changes it).
- **SPEC**: §3.3 / GG-0014 ("one AST", typed execute methods, `LoopNode`)
  needs amendment A5.

### B.7 Migration cost

XL (≈ 8–12 lane-weeks): `NodeFactory` (≈ 3,400 lines) becomes an emitter;
the ≈ 6,000 lines of `graalgo.go.nodes` become operations; every PE-specific
design — strip-mining, merge-exploded `goto`, read-only receivers, positions
(ADR-0020), the deferring roots, `RangeFunc`'s yield roots — is redesigned
against the DSL's control-flow model, then re-verified by `test-m1a` …
`test-m1d`, `pe-check-m1a` and the bench suites.

## Reproducing

The prototypes were scratch code, deleted at the end of the lane (they are
not wired into any gate). What they were, so the numbers can be redone:
study A — a null-language `RootNode` per (operation, representation) with
`@Child` TS nodes, fixtures of 1,024 random lowercase words (4–16, 40–120 and
200–256 bytes), a 1 KiB haystack, a 4 KiB mixed UTF-8 text with 8 `0xff`
bytes, and a 4,096-word corpus over a 512-word vocabulary; study B — the IR,
AST, `@GenerateBytecode` root and harness described in B.2, compiled with the
pinned `truffle-dsl-processor`. The product experiment of A.5 is the two
changes described there (about 30 lines in `Str` and the generated
`IntrinsicTable`), run with `test/bench/suite.sh --gg-variant
"exp=-Dgraalgo.exp.strhash=true -Dgraalgo.exp.tskern=true"`.
