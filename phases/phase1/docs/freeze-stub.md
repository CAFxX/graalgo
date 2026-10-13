# Freeze-doc stub — signatures frozen across workstreams

Signatures other workstreams depend on. Do not change without a
cross-workstream review.

## D8 safepoint seam (frozen 2026-10-04, M3)

- Signature: `graalgo.safepoint.Safepoint.poll()` — `public static void`, no arguments,
  no return value, in [Safepoint.java](../../../internal/backend/runtime/src/main/java/graalgo/safepoint/Safepoint.java).
- Call sites (inserted by the AST builders, not by user code):
  - function entries — `GoRootNode.execute` (M3, WS D);
  - loop back-edges — WS E (when loops land).
- Phase-1 semantics: no-op. Phase 2 gives it meaning (thread suspension
  for GC). Callers must not depend on any side effect today.

## GG-0020 intrinsic surface, freeze v1 (frozen 2026-10-04, M4/WS G)

**v2 amendment 2026-10-13 (ADR-0008):** `makeslice`/`growslice` gain a
`GoStructDescriptor elemDesc` parameter (struct-element slots are
zero-initialized via `GoStruct.zero(desc)`; null for non-struct kinds);
interface freeze v1 → v2. All other rows unchanged.

The full ~60-function runtime contract the adapted stdlib builds against
(SPEC §3.6). Each entry gives the frozen Java signature, the owning file,
and whether Phase 1 provides a REAL implementation or an honest STUB
(throws `UnsupportedOperationException` with a Phase-2 message; listed as
such in code). A STUB that returns a wrong value silently is a defect; a
STUB that throws is documented Phase-2 surface.

Conventions:
- `REAL` — implemented, tested, differential vs gc where applicable.
- `STUB` — throws; Phase 2 owns the implementation.
- `D4` — the minimal `fmt` stub: real formatting logic, but the *package*
  is a stub (replaceable-in-Phase-3; resolved only via the frontend
  Phase-1 import overlay).

### Memory intrinsics — `graalgo.runtime.Intrinsics` (REAL)

| Signature | Notes |
|-----------|-------|
| `public static void memmove(Object dst, int dstOff, Object src, int srcOff, int n)` | Untyped word copy; used by slice growth. [T] |
| `public static boolean memequal(Object a, int aOff, Object b, int bOff, int n)` | Byte equality. [T] |

### Slice intrinsics — `graalgo.runtime.Intrinsics` (REAL)

| Signature | Notes |
|-----------|-------|
| `public static GoSlice makeslice(GoKind elemKind, GoStructDescriptor elemDesc, int len, int cap)` | Exact cap (no size-class rounding observable). `elemDesc` (v2, ADR-0008): struct-element descriptor, null for non-struct kinds; struct slots zero-initialized. [T] |
| `public static GoSlice growslice(GoSlice old, GoKind elemKind, GoStructDescriptor elemDesc, int newLen, long elemSizeBytes, boolean elemHasPointers)` | gc's growslice ported exactly (nextslicecap + roundupsize). `elemDesc` (v2, ADR-0008): as in `makeslice`; also covers the fresh growth region. [T] |
| `public static GoSlice growslice(GoSlice old, GoKind elemKind, int additional)` | Convenience for `append`; computes size/hasPointers from kind. [T] |
| `public static int slicecopy(GoSlice dst, GoSlice src, GoKind elemKind)` | `copy` builtin; memmove semantics; returns count. [T] |
| `public static long elemSizeBytes(GoKind kind)` | D5 element size. |
| `public static boolean elemHasPointers(GoKind kind)` | GC scanning hint. |

### Map intrinsics — `graalgo.runtime.Intrinsics` (REAL)

| Signature | Notes |
|-----------|-------|
| `public static void mapAssign(GoMap m, Object key, Object value)` | Insert or overwrite. [T] |
| `public static Object mapAccess(GoMap m, Object key)` | Null when absent (caller applies comma-ok). [T] |
| `public static void mapDelete(GoMap m, Object key)` | No-op on absent. [T] |
| `public static int mapLen(GoMap m)` | [T] |
| `public static Object[] mapIterInit(GoMap m)` | Snapshot for range. |
| `public static Object mapIterNext(Object[] snapshot, int index)` | |

### Interface / itab — `graalgo.runtime.Intrinsics`

| Signature | Status | Notes |
|-----------|--------|-------|
| `public static Object itabFor(GoTypeDescriptor descriptor, GoTypeDescriptor iface)` | STUB | Phase-2 type/metadata tables. [T] |

### Random — `graalgo.runtime.Intrinsics` (REAL)

| Signature | Notes |
|-----------|-------|
| `public static synchronized long fastrand64()` | wyrand (gc's runtime/rand.go port). |
| `public static int fastrand()` | Low 32 bits. [T] |
| `public static int fastrandn(int n)` | `[0, n)`; n must be positive. [T] |

### Traceback — `graalgo.runtime.Intrinsics`

| Signature | Status | Notes |
|-----------|--------|-------|
| `public static String traceback()` | STUB | Needs Phase-2 scheduler stacks. [T] |

### Builtin print — `graalgo.runtime.BuiltinPrint` (REAL)

| Signature | Notes |
|-----------|-------|
| `public static void print(Arg[] args)` | `print` builtin → **stderr** (gc parity). [T] |
| `public static void println(Arg[] args)` | `println` builtin → **stderr**. [T] |
| `public record Arg(GoKind kind, Object value)` | Argument box. |

### fmt stub — `graalgo.runtime.Fmt` (D4; replaceable-in-Phase-3)

The seven pure functions (D4). Real formatting logic held to the
verb-conformance matrix (`corpus/fmt/matrix.tsv`, 122 rows differential
vs gc); the *package* is a stub — resolved only via the frontend Phase-1
import overlay, never linked as a real `fmt`.

| Signature | Notes |
|-----------|-------|
| `public static GoString sprintf(String format, Arg[] args)` | [T] |
| `public static GoString sprint(Arg[] args)` | [T] |
| `public static GoString sprintln(Arg[] args)` | [T] |
| `public static int print(Arg[] args)` | → stdout; returns bytes written. [T] |
| `public static int printf(String format, Arg[] args)` | → stdout; returns bytes written. [T] |
| `public static int println(Arg[] args)` | → stdout; returns bytes written. [T] |
| `public static GoInterface errorf(String format, Arg[] args)` | Dynamic type `*errors.errorString`. [T] |
| `public record Arg(Object value, GoKind kind, String typeName, GoKind keyKind, String keyTypeName, GoKind elemKind, String elemTypeName)` | Full type metadata. |

### Atomics — `graalgo.runtime.Atomics` (REAL)

JVM atomics backing Go's `sync/atomic` (int32/int64/uintptr).

| Signature |
|-----------|
| `public static int loadInt32(AtomicInteger addr)` |
| `public static void storeInt32(AtomicInteger addr, int v)` |
| `public static int addInt32(AtomicInteger addr, int delta)` |
| `public static boolean compareAndSwapInt32(AtomicInteger addr, int old, int new_)` |
| `public static int swapInt32(AtomicInteger addr, int new_)` |
| `public static long loadInt64(AtomicLong addr)` |
| `public static void storeInt64(AtomicLong addr, long v)` |
| `public static long addInt64(AtomicLong addr, long delta)` |
| `public static boolean compareAndSwapInt64(AtomicLong addr, long old, long new_)` |
| `public static long swapInt64(AtomicLong addr, long new_)` |
| `public static long loadUintptr(AtomicLong addr)` |
| `public static void storeUintptr(AtomicLong addr, long v)` |

### Timers — `graalgo.runtime.Timers`

| Signature | Status | Notes |
|-----------|--------|-------|
| `public static long nanotime()` | REAL | `System.nanoTime()`. [T] |
| `public static void sleep(long ns)` | STUB | Phase-2 scheduler sleep. [T] |
| `public static void startTimer(long whenNanos, Runnable callback)` | STUB | Phase-2 timer heap. |
| `public static void stopTimer(long timerId)` | STUB | |

### Scheduler glue — `graalgo.runtime.Scheduler` (STUB)

Phase-2 owns the implementation (park/queue/drainer per §3.5).

| Signature | Notes |
|-----------|-------|
| `public static void spawn(GoFunc fn)` | `go` statement (Phase 1: factory scope error). [T] |
| `public static void yield()` | [T] |
| `public static void park(Runnable wake)` | [T] |
| `public static int runnableCount()` | |

### Netpoller API contract — `graalgo.runtime.Netpoll` (STUB)

The contract `net` calls into (blocking NIO per §3.5), not the deleted
event loop.

| Signature |
|-----------|
| `public static void init()` [T] |
| `public static long open(int fd, int mode)` [T] |
| `public static void close(int fd)` [T] |
| `public static boolean waitForEvents(boolean block)` |
| `public static void breakWait()` |

### Channel ops — `graalgo.runtime.ChanOps` (STUB)

| Signature |
|-----------|
| `public static Chan makeChan(long elemSizeBytes, int capacity)` |
| `public static void send(Chan ch, Object value)` [T] |
| `public static Object recv(Chan ch)` [T] |
| `public static void close(Chan ch)` [T] |
| `public static int len(Chan ch)` |
| `public static int cap(Chan ch)` |
| `public static int selectGo(SelectCase[] cases)` |

### Panic / recover — `graalgo.runtime.Panic` (REAL; WS F)

Frozen cross-workstream interface (verbatim; WS F implements).

| Signature | Notes |
|-----------|-------|
| `public static void throwPanic(Object goValue)` | `panic(x)` builtin calls this. [T] |
| `public static Object recover()` | `recover()` builtin calls this. [T] |
| `public static void throwPanic(Object goValue, Object descriptor, String goTypeName, Node throwSite)` | Additive overload (rendering/boxing info); does not change the frozen pair. |

### Builtin nodes — `graalgo.frontend` (REAL; Truffle nodes)

The builtins as Truffle expression nodes (WS G). The factory
(`NodeFactory.buildCall`) dispatches on `call.fields.builtin`.

| Node | Builtin | Result kind |
|------|---------|-------------|
| `LenNode(GoExprNode)` | `len` | INT64 |
| `CapNode(GoExprNode)` | `cap` | INT64 |
| `MakeNode(GoKind, GoExprNode, GoExprNode)` / `MakeNode(int)` | `make` | SLICE / MAP |
| `NewNode(GoKind, GoStructDescriptor)` | `new` | POINTER |
| `AppendNode(GoExprNode, GoKind, GoExprNode[])` | `append` | SLICE |
| `CopyNode(GoExprNode, GoExprNode, GoKind)` | `copy` | INT64 |
| `DeleteNode(GoExprNode, GoExprNode)` | `delete` | — (statement only) |
| `ComplexNode(Op, GoKind, GoExprNode, GoExprNode)` | `complex`/`real`/`imag` | COMPLEX64/128 / FLOAT32/64 |
| `PrintNode(boolean, GoExprNode[])` | `print`/`println` | — (statement only) |
| `PanicBuiltinNode(GoExprNode, GoTypeDescriptor, String, boolean)` | `panic` | — (statement only) |
| `RecoverBuiltinNode()` | `recover` | INTERFACE |
| `FmtCallNode(String, GoExprNode[], ...)` | `fmt.*` (D4) | STRING / INTERFACE |

### M5 runtime surface (frozen 2026-10-04, WS F)

The AST nodes built by `NodeFactory` for M5 (defer/panic/recover/Goexit,
function literals) call into this runtime surface. It is frozen at the
signatures below; Phase 1 provides REAL implementations (differential vs
gc via the panic corpus).

| Signature | Owner | Notes |
|-----------|-------|-------|
| `GoErrors.bounds(BoundsCode, long, long, boolean)` | `graalgo.runtime.GoErrors` | Slice/index bounds error values [C] |
| `GoErrors.indexOutOfRange(long, long, boolean)` | `graalgo.runtime.GoErrors` | Index out of range [C] |
| `GoErrors.assignmentToNilMap()` | `graalgo.runtime.GoErrors` | Assignment to nil map [C] |
| `GoErrors.nilDereference()` | `graalgo.runtime.GoErrors` | Nil pointer dereference [C] |
| `GoErrors.integerDivideByZero()` | `graalgo.runtime.GoErrors` | Integer divide by zero [C] |
| `GoErrors.negativeShift()` | `graalgo.runtime.GoErrors` | Negative shift amount [C] |
| `GoErrors.interfaceConversion(String, String, String)` | `graalgo.runtime.GoErrors` | Failed type assertion [C] |
| `DeferChain.push(DeferredCall)` | `graalgo.runtime.DeferChain` | Register a deferred call [C] |
| `DeferChain.runAll()` | `graalgo.runtime.DeferChain` | Run defers on normal return (LIFO) [C] |
| `DeferChain.unwindReturning(int)` | `graalgo.runtime.DeferChain` | Run defers to a mark on return [C] |
| `DeferChain.unwindPanicking(int, PanicException)` | `graalgo.runtime.DeferChain` | Run defers during panic unwind [C] |
| `DeferChain.unwindGoexiting(int)` | `graalgo.runtime.DeferChain` | Run defers during Goexit [C] |
| `Goexit.goexit()` | `graalgo.runtime.Goexit` | `runtime.Goexit` — runs defers, then fatal deadlock [C] |
| `GoCalls.invoke(GoFunc, Object[])` | `graalgo.runtime.GoCalls` | Call a function value [C] |
| `GoroutineState` (class) | `graalgo.runtime.GoroutineState` | Per-goroutine panic/defer state [C] |
| `DeferredCall` (class) | `graalgo.runtime.DeferredCall` | A registered deferred call (fun + args) [C] |

Scope rule: the freeze covers the *signatures* the M5 AST calls. Internal
helpers and Phase-2 stubs (scheduler, netpoll, timers, chanops) are
documented in their own sections; a STUB that throws is not a signature
change.

## Version history

- **v1 (2026-10-04, WS G)**: initial freeze. 63 signatures (57 unique
  methods, some overloaded) + 11 builtin nodes. STUBs: itabFor,
  traceback, timers (3), scheduler (4), netpoll (5), chanops (7).
- **v1.1 (2026-10-04, WS F)**: added M5 runtime surface (16 signatures:
  GoErrors ×7, DeferChain ×5, Goexit, GoCalls ×2, GoroutineState,
  DeferredCall). Total: 79 signatures.

Coverage: `FreezeSignatureTest` verifies the frozen signatures by reflection
(name, params, return type, public/static shape), plus frozen class existence
and the `NodeFactory$Program` record components — see its `FROZEN` list for
the exact covered set (extended at M9: NodeFactory, node protocol,
M5 runtime surface, values types, probe APIs). The M5 runtime surface is
also verified differentially vs gc via the panic corpus (pn01–pn20). Rows
marked [T] are in `FreezeSignatureTest`; rows marked [C] are covered
by corpus differential tests.

Amendment (M9, per [INTERFACE_FREEZE.md](../INTERFACE_FREEZE.md) §1 conflict rule): the M5 runtime
surface is incorporated as FROZEN in [INTERFACE_FREEZE.md](../INTERFACE_FREEZE.md) §3.4; this
document's "frozen at the signatures below" stands unchanged. The D8
safepoint signature is `graalgo.safepoint.Safepoint.poll()` (class name
was missing in v1).

## Panic semantics (GG-0045) {#panic-semantics-GG-0045}

**Decision (M5, 2026-10-04, WS F)**: replicate gc exactly. `panic(nil)`
produces `*runtime.PanicNilError` with the message `runtime error: panic
called with nil argument`, matching stock go1.27.1. The
`GODEBUG=panicnil=1` legacy behavior (panic(nil) → nil interface) is out
of scope for Phase 1.

**Rationale**:
1. The task default is "replicate gc exactly per `-lang` gate"; gc ≥ 1.21
   (and go1.27.1, the pinned oracle) produces `*PanicNilError`.
2. The corpus pins this: pn06 recovers from `panic(nil)` and observes
   `r == nil` is false (gc: `r` is `*runtime.PanicNilError`).
3. No `-lang` gate exists in Phase 1 to select legacy behavior.

**Implementation**: `Panic.throwPanic` converts a nil interface value to
`PanicNilError.INSTANCE`; `PanicRendering` renders it as
`runtime error: panic called with nil argument`. `recover()` returns the
`*PanicNilError` value (not nil).

**Goexit in main (M5, WS F)**: `runtime.Goexit` in `main` runs the
deferred calls, then — with no other goroutine to run — gc reports a
fatal deadlock (`fatal error: no goroutines (main called
runtime.Goexit) - deadlock!`, exit 2). graalgo replicates this exactly
(pn19 pins the header and exit code). `recover()` cannot stop a Goexit.
