# Freeze-doc stub — signatures frozen across workstreams

Signatures other workstreams depend on. Do not change without a
cross-workstream review.

## D8 safepoint seam (frozen 2026-10-04, M3)

- Signature: `graalgo.safepoint.poll()` — `public static void`, no arguments,
  no return value, in `src/main/java/graalgo/safepoint/Safepoint.java`.
- Call sites (inserted by the AST builders, not by user code):
  - function entries — `GoRootNode.execute` (M3, WS D);
  - loop back-edges — WS E (when loops land).
- Phase-1 semantics: no-op. Phase 2 gives it meaning (thread suspension
  for GC). Callers must not depend on any side effect today.

## GG-0020 intrinsic surface, freeze v1 (frozen 2026-10-04, M4/WS G)

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
| `public static void memmove(Object dst, int dstOff, Object src, int srcOff, int n)` | Untyped word copy; used by slice growth. |
| `public static boolean memequal(Object a, int aOff, Object b, int bOff, int n)` | Byte equality. |

### Slice intrinsics — `graalgo.runtime.Intrinsics` (REAL)

| Signature | Notes |
|-----------|-------|
| `public static GoSlice makeslice(GoKind elemKind, int len, int cap)` | Exact cap (no size-class rounding observable). |
| `public static GoSlice growslice(GoSlice old, GoKind elemKind, int newLen, long elemSizeBytes, boolean elemHasPointers)` | gc's growslice ported exactly (nextslicecap + roundupsize). |
| `public static GoSlice growslice(GoSlice old, GoKind elemKind, int additional)` | Convenience for `append`; computes size/hasPointers from kind. |
| `public static int slicecopy(GoSlice dst, GoSlice src, GoKind elemKind)` | `copy` builtin; memmove semantics; returns count. |
| `public static long elemSizeBytes(GoKind kind)` | D5 element size. |
| `public static boolean elemHasPointers(GoKind kind)` | GC scanning hint. |

### Map intrinsics — `graalgo.runtime.Intrinsics` (REAL)

| Signature | Notes |
|-----------|-------|
| `public static void mapAssign(GoMap m, Object key, Object value)` | Insert or overwrite. |
| `public static Object mapAccess(GoMap m, Object key)` | Null when absent (caller applies comma-ok). |
| `public static void mapDelete(GoMap m, Object key)` | No-op on absent. |
| `public static int mapLen(GoMap m)` | |
| `public static Object[] mapIterInit(GoMap m)` | Snapshot for range. |
| `public static Object mapIterNext(Object[] snapshot, int index)` | |

### Interface / itab — `graalgo.runtime.Intrinsics`

| Signature | Status | Notes |
|-----------|--------|-------|
| `public static Object itabFor(GoTypeDescriptor descriptor, GoTypeDescriptor iface)` | STUB | Phase-2 type/metadata tables. |

### Random — `graalgo.runtime.Intrinsics` (REAL)

| Signature | Notes |
|-----------|-------|
| `public static synchronized long fastrand64()` | wyrand (gc's runtime/rand.go port). |
| `public static int fastrand()` | Low 32 bits. |
| `public static int fastrandn(int n)` | `[0, n)`; n must be positive. |

### Traceback — `graalgo.runtime.Intrinsics`

| Signature | Status | Notes |
|-----------|--------|-------|
| `public static String traceback()` | STUB | Needs Phase-2 scheduler stacks. |

### Builtin print — `graalgo.runtime.BuiltinPrint` (REAL)

| Signature | Notes |
|-----------|-------|
| `public static void print(Arg[] args)` | `print` builtin → **stderr** (gc parity). |
| `public static void println(Arg[] args)` | `println` builtin → **stderr**. |
| `public record Arg(GoKind kind, Object value)` | Argument box. |

### fmt stub — `graalgo.runtime.Fmt` (D4; replaceable-in-Phase-3)

The seven pure functions (D4). Real formatting logic held to the
verb-conformance matrix (`corpus/fmt/matrix.tsv`, 122 rows differential
vs gc); the *package* is a stub — resolved only via the frontend Phase-1
import overlay, never linked as a real `fmt`.

| Signature | Notes |
|-----------|-------|
| `public static GoString sprintf(String format, Arg[] args)` | |
| `public static GoString sprint(Arg[] args)` | |
| `public static GoString sprintln(Arg[] args)` | |
| `public static int print(Arg[] args)` | → stdout; returns bytes written. |
| `public static int printf(String format, Arg[] args)` | → stdout; returns bytes written. |
| `public static int println(Arg[] args)` | → stdout; returns bytes written. |
| `public static GoInterface errorf(String format, Arg[] args)` | Dynamic type `*errors.errorString`. |
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
| `public static long nanotime()` | REAL | `System.nanoTime()`. |
| `public static void sleep(long ns)` | STUB | Phase-2 scheduler sleep. |
| `public static void startTimer(long whenNanos, Runnable callback)` | STUB | Phase-2 timer heap. |
| `public static void stopTimer(long timerId)` | STUB | |

### Scheduler glue — `graalgo.runtime.Scheduler` (STUB)

Phase-2 owns the implementation (park/queue/drainer per §3.5).

| Signature | Notes |
|-----------|-------|
| `public static void spawn(GoFunc fn)` | `go` statement (Phase 1: factory scope error). |
| `public static void yield()` | |
| `public static void park(Runnable wake)` | |
| `public static int runnableCount()` | |

### Netpoller API contract — `graalgo.runtime.Netpoll` (STUB)

The contract `net` calls into (blocking NIO per §3.5), not the deleted
event loop.

| Signature |
|-----------|
| `public static void init()` |
| `public static long open(int fd, int mode)` |
| `public static void close(int fd)` |
| `public static boolean waitForEvents(boolean block)` |
| `public static void breakWait()` |

### Channel ops — `graalgo.runtime.ChanOps` (STUB)

| Signature |
|-----------|
| `public static Chan makeChan(long elemSizeBytes, int capacity)` |
| `public static void send(Chan ch, Object value)` |
| `public static Object recv(Chan ch)` |
| `public static void close(Chan ch)` |
| `public static int len(Chan ch)` |
| `public static int cap(Chan ch)` |
| `public static int selectGo(SelectCase[] cases)` |

### Panic / recover — `graalgo.runtime.Panic` (REAL; WS F)

Frozen cross-workstream interface (verbatim; WS F implements).

| Signature | Notes |
|-----------|-------|
| `public static void throwPanic(Object goValue)` | `panic(x)` builtin calls this. |
| `public static Object recover()` | `recover()` builtin calls this. |
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
| `PanicNode(GoExprNode)` / `PanicNode()` | `panic`/`recover` | — / INTERFACE |
| `FmtCallNode(String, GoExprNode[], ...)` | `fmt.*` (D4) | STRING / INTERFACE |

## Version history

- **v1 (2026-10-04, WS G)**: initial freeze. 58 functions + 11 builtin
  nodes. STUBs: itabFor, traceback, timers (3), scheduler (4), netpoll
  (5), chanops (7).

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
