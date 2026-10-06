# P3-M4 intrinsic audit — GG-0020 stub-vs-real (2026-10-06; refreshed
2026-10-06 post-remediation to the current tree)

**Scope:** every public method in `phases/phase1/src/main/java/graalgo/runtime/`,
`phases/phase2/src/main/java/graalgo/runtime/` (+`sync/`, `graalgo/sched/`),
`phases/phase3/src/main/java/graalgo/iface/` and `graalgo/p3sync/`.
Enumerated mechanically (signature regex + brace-matched body scan for
`throw new UnsupportedOperationException` / `throw deleted(`); the full
per-file table is §4.

**Headline (corrected 2026-10-06, methodology P2-1):** the M4-start
inventory was 277 public methods: **248 REAL** (non-throwing),
**19 STUB** (phase-1 throwing methods that are not intentionally
removed: 17 overlay-routed per §2a + 2 implemented in M4 per §2c),
**10 INTENTIONALLY-REMOVED** (5 phase-1 Netpoll + 5 phase-2 Netpoll,
GG-0018), 0 other. 248+19+10=277. (The original headline said "24
STUB + 5 INTENTIONALLY-REMOVED" — it counted the 5 phase-1 Netpoll
throwing methods as stubs although §2b classifies them
intentionally-removed, and missed the 5 phase-2 Netpoll.)

**Post-M4 refresh (current tree):** 286 public methods audited →
**259 REAL**, **17 STUB-shaped-but-REAL-via-overlay** (§2a; throwing
fail-fasts, real through the P2-D8 overlay), **10
INTENTIONALLY-REMOVED**, 0 true stubs. New since the M4-start
inventory: `Linknames` (6 public methods, all REAL — missed in the
original audit, methodology P2-2) and 3 M4-added `Intrinsics` methods
(`structSizeBytes`, `setItabProvider`, `renderGoStack`, all REAL);
the 2 §2c stubs (`itabFor`, `traceback`) are implemented (REAL).
259+17+10=286.

**Correction to the brief's rough inventory:** phase-2 `Scheduler` has
**0** stubs, not ~2 — the two grep hits were javadoc mentions of the
phase-1 stub it replaces. All other rough counts verified exactly
(ChanOps 7, GcInterop 3, itabFor+traceback 2, Netpoll 5, Scheduler 4,
Timers 3).

## 1. Classification rules

- **REAL** — the method does its documented job on every supported
  classpath. Includes methods whose phase-1 body throws but which are
  unreachable: the P2-D8 overlay puts phase-2 classes first on the
  classpath, so the phase-2 same-FQN replacement always wins; the throw
  survives only as a fail-fast for a misconfigured classpath (guarded by
  the launcher's `Scheduler.isPhase2()` check).
- **STUB** — throws `UnsupportedOperationException`; Phase 2/3 owns the
  implementation. M4 must route each to a real implementation.
- **INTENTIONALLY-REMOVED** — the API was deleted by a recorded decision;
  the throw documents the deletion and where the functionality went. Not
  a stub; not implemented.
- **NO-OP-BY-DESIGN** — no intrinsic needed; the JVM covers the semantics.
  Documented per function, never hand-waved.

## 2. The 24 phase-1 stubs → routing

### 2a. REAL via the Phase-2 overlay (17) — no code change

P2-D8 (the documented overlay mechanism): phase-2 classes shadow the
phase-1 same-FQN stubs; frozen signatures are byte-identical
(`FreezeSignatureTest`); bodies are INTERNAL per freeze §8. The phase-3
launcher classpath is `phase3 > phase2 > phase1` (Makefile `RUN_CP`).
Routing: **keep the throws** (fail-fast on misconfiguration), classify
REAL.

| # | Stub | Phase-2 replacement | Evidence of realness |
|---|---|---|---|
| 7 | `ChanOps.makeChan/send/recv/close/len/cap/selectGo` | `graalgo.runtime.ChanOps` (P2-M2) | GoChan + SelectOp; differential corpus `corpus/sync` 25/25 |
| 3 | `GcInterop.setFinalizer/keepAlive/gc` | `graalgo.runtime.GcInterop` (P2-M4) | per-function justification §3 |
| 3 | `Timers.sleep/startTimer/stopTimer` | `graalgo.runtime.Timers` (P2) | timer heap + parkCurrent; `time` corpus (P3-M5) |
| 4 | `Scheduler.spawn/yield/park/runnableCount` | `graalgo.runtime.Scheduler` (P2) | SchedulerCore; P2-M1…M10 suites |

### 2b. INTENTIONALLY-REMOVED (5)

`Netpoll.init/open/close/waitForEvents/breakWait` (phase-1, all 5):
deleted by **GG-0018 (P2-M3)**. The phase-2 same-FQN replacement throws
the documented deletion error; there is no readiness-based network
poller in graalgo. The functionality went to **blocking NIO directly on
virtual threads**, bracketed by `SchedulerCore.ioEnter()/ioExit()` (which
releases/re-acquires the in-flight scheduler slot — gc netpoller parity
for GOMAXPROCS accounting). Falsifier: `make check`'s netpoller-deletion
gate fails on any reachable netpoller code path. No M4 code change;
classification recorded here.

### 2c. IMPLEMENTED in M4 (2) — REAL, Java-callable; NO Go wire path (P2-1)

| Stub | Routing |
|---|---|
| `Intrinsics.itabFor(descriptor, iface)` | Wired to the P3-M3 itab machinery (`graalgo.iface.IfaceMethodTable`): phase-1 has a provider hook (frozen signature kept; STUB bodies are INTERNAL per freeze §8), phase-3 registers the provider at launcher startup from the installed method tables + interface wire entries. Remediation P2-1: one iface-id namespace (the wire type-table ids) for the intrinsic and dispatch paths — the old process-counter minting could collide. Red-green: R5 (collision probe) + `ItabForIntrinsicTest` (identical cached instance, no cross-interface collision). |
| `Intrinsics.traceback()` | Implemented via `GoroutineState.current().goStack()` (phase-2 ThreadLocal holder under phase-2/3 classpaths; phase-1 static holder under phase-1). gc-shaped single-goroutine rendering shared with the panic path (`renderGoStack`). Differential vs gc panic tracebacks (goroutine IDs, `+0x` offsets, arg lists normalized). |

**Honest reclassification (P2-1):** both are REAL as Java intrinsics
(callable from any registered linkname target), but NEITHER has a Go
wire path — no wire node kind, no exporter support, no Go-callable
surface. Go-reachability is deferred, not silently assumed.
**Re-test trigger:** when wire nodes + exporter support are added for
`itab`/`traceback`, re-verify this routing, add differential corpora
calling them from Go, and re-run this audit's per-file table.

## 3. GcInterop — per-function justification (no hand-waving)

The JVM GC **is** the Go GC (P2 ADR-0002 D1); roots are the JVM's, exact
by construction (D2); JVM barriers cover every store, so there is **no
Truffle-level write barrier** (D3). The three functions are the
*observable* surface, each real:

1. `setFinalizer(obj, finalizer)` — REAL: single shared
   `java.lang.ref.Cleaner` (one dedicated thread ⇒ serialized, like gc's
   finalizer goroutine). **Accepted divergence DIV-M4-1:** the finalizer
   is invoked with a `nil` argument — a Cleaner action is denied the
   referent and the object cannot be recovered on a pure JVM.
   Finalizers dereferencing their argument diverge loudly, not silently.
2. `keepAlive(x)` — REAL, and **not** a no-op: `Reference.reachabilityFence(x)`.
   A plain no-op intrinsic would be unsound after JIT inlining (the
   referent could be collected past its last use); the fence is the
   load-bearing primitive.
3. `gc()` — REAL: `System.gc()` + poll of GC MXBean collection counts
   until they advance, bounded 30 s. **Documented approximation:** if the
   counts never advance (explicit GC disabled), returns anyway — callers
   must not depend on reclamation having completed.

## 4. Write barrier — decision

**NO-OP-BY-DESIGN; no intrinsic added.** gc's `writeBarrier` is a
*linknamed variable* (`runtime/mgc.go`: `//go:linkname writeBarrier var
writeBarrier struct{ enabled bool; ... }`), not a function — the compiler
emits a check of `enabled` before calling the barrier. In graalgo every Go
store is a JVM store and the JVM's own barriers (G1/ZGC as configured)
cover them (P2 ADR-0002 D3); there is no concurrent-mark phase needing a
mutator barrier, so the analog of `writeBarrier.enabled` is the constant
`false`. If a future `//go:linkname` target needs the symbol, it maps to
a constant-false accessor; no stub exists today and none is introduced.

## 5. Full per-file table

Format: `Class: <public methods> total, <stub> throwing`. Non-throwing
methods are REAL (spot-verified: every "real" verdict below comes from
the body scan, not from absence of evidence).

### phase1 `graalgo/runtime/` (24 stubs)

- `Atomics`: 16 total, 0 stub — REAL (int32/int64/uintptr/bool
  load/store/add/CAS/swap over `AtomicInteger`/`AtomicLong`).
- `BuiltinPrint`: 3 total, 0 stub — REAL.
- `ChanOps`: 8 total, 7 stub — `makeChan, send, recv, close, len, cap,
  selectGo` STUB → §2a (REAL via overlay); `SelectCase` record is data.
- `DeferChain`: 6 total, 0 stub — REAL.
- `DeferredCall`: 6 total, 0 stub — REAL.
- `Fmt`: 8 total, 0 stub — REAL.
- `GcInterop`: 3 total, 3 stub — `setFinalizer, keepAlive, gc` STUB →
  §2a (REAL via overlay); per-function justification §3.
- `GoCalls`: 2 total, 0 stub — REAL (`invoke`, `nilFuncCall`).
- `GoErrorValue`: 3 total, 0 stub — REAL (value type).
- `GoErrors`: 7 total, 0 stub — REAL.
- `GoFloatFmt`: 4 total, 0 stub — REAL.
- `GoIsPrint`, `GoQuote`: 0 public methods — helpers (package-visible).
- `GoStackFrame`: 4 total, 0 stub — REAL (data).
- `Goexit`: 1 total, 0 stub — REAL.
- `GoroutineState`: 10 total, 0 stub — REAL.
- `Intrinsics`: 19 total, 2 stub — `itabFor, traceback` STUB → §2c
  (implemented in M4); other 17 REAL (memmove/memequal, makeslice,
  growslice×2, mapAssign/mapAccess/mapDelete/mapLen, mapIterInit/
  mapIterNext, elemSizeBytes, elemHasPointers, slicecopy, fastrand64/
  fastrand/fastrandn).
- `Linknames` (new P3-M4 class, `graalgo.runtime`): 6 total, 0 stub —
  `register, clear, dispatch, bindGo, resolveGo, callGo` REAL (the
  linkname symbol registry, ADR-0004 D1).
- `Netpoll`: 5 total, 5 stub — `init, open, close, waitForEvents,
  breakWait` → §2b INTENTIONALLY-REMOVED (GG-0018).
- `Panic`: 3 total, 0 stub — REAL.
- `PanicException`: 7 total, 0 stub — REAL (data).
- `PanicNilError`: 1 total, 0 stub — REAL.
- `PanicRendering`: 4 total, 0 stub — REAL.
- `Runtime`: 1 total, 0 stub — REAL (`println`).
- `Scheduler`: 4 total, 4 stub — `spawn, yield, park, runnableCount`
  STUB → §2a (REAL via overlay).
- `Timers`: 4 total, 3 stub — `sleep, startTimer, stopTimer` STUB →
  §2a (REAL via overlay); `nanotime` REAL.

### phase2 `graalgo/runtime/` (5 intentionally-removed)

- `ChanOps`: 12 total, 0 stub — REAL (7 frozen + `makeChan` 3-arg,
  `recvOk`, `selectRecvValue`, `selectRecvOk` additive).
- `GcInterop`: 8 total, 0 stub — REAL (§3 + `newWeakRef`,
  `weakRefValue`, `memStats`, `pauseNsRing` additive).
- `GoChan`: 9 total, 0 stub — REAL.
- `GoStrings`: 0 public methods — internal helper.
- `GoTimer`: 9 total, 0 stub — REAL.
- `GoroutineState`: 10 total, 0 stub — REAL (ThreadLocal holder).
- `Netpoll`: 5 total, 5 throwing — INTENTIONALLY-REMOVED (GG-0018;
  `deleted()` errors, §2b).
- `Panic`: 5 total, 0 stub — REAL.
- `Scheduler`: 5 total, 0 stub — REAL (`isPhase2` diagnostic +
  4 frozen).
- `SelectOp`: 1 total, 0 stub — REAL.
- `TimerChan`: 2 total, 0 stub — REAL.
- `TimerHeap`: 5 total, 0 stub — REAL.
- `Timers`: 9 total, 0 stub — REAL (4 frozen + `startTimerId`,
  `after`, `newTimer`, `newTicker`, `tick` additive).

### phase2 `graalgo/runtime/sync/` — 13 total, 0 stub — REAL
(`GoMutex` 3, `GoOnce` 2, `GoRWMutex` 4, `GoWaitGroup` 4).

### phase2 `graalgo/sched/` — 30 total, 0 stub — REAL
(`Drainer` 2, `ResumeToken` 1, `SchedTrace` 3, `SchedulerCore` 24;
`GoroutineHandle` is a data record with no public methods of its own).

### phase3 `graalgo/iface/` — 21 total, 0 stub — REAL
(`DescriptorNames` 3, `IfaceDispatchBuilder` 1, `IfaceDispatchNode` 7,
`IfaceItab` 4, `IfaceMethodTable` 5 (`install, clear,
installInterfaces, itabForIntrinsic, itabForDescriptors`) +
`IfaceWireEntry` record (P2-1: wire id + method names, one id
namespace), `IfaceMethodTables` 2 (`resolve, resolveInterfaces`),
`IfaceMethodTarget` 2).

### phase3 `graalgo/p3sync/` — 5 total, 0 stub — REAL
(`P3SyncMain` 1, `SyncNewNode` 2, `SyncOpNode` 2).

### Post-M4 refresh deltas (2026-10-06, methodology P2-2)

- `Intrinsics`: 19 → 22 total, 0 stub. `itabFor`/`traceback`
  implemented (§2c); new: `structSizeBytes` (flat-struct ABI layout for
  append growth), `setItabProvider` (the provider hook),
  `renderGoStack` (gc-shaped rendering shared with the panic path) —
  all REAL. The one remaining `throw new
  UnsupportedOperationException` in `itabFor` is the documented
  no-provider fail-fast (phase-1-only mode), not a stub.
- `Linknames`: new class, 6 total, 0 stub — REAL (see above).
- `IfaceMethodTable`/`IfaceMethodTables`: see the iface section above
  (P2-1 one-namespace rework).

## 6. GG-0020 frozen-surface subset

The GG-0020 "versioned intrinsic surface of ~60–70 functions"
(`requirements/GG-0020.yaml`) is the `docs/freeze-stub.md` list, a subset
of the 277: Intrinsics (19) + BuiltinPrint (2 frozen) + Fmt (7) +
Atomics (12 frozen of 16) + Timers (4) + Scheduler (4) + Netpoll (5,
removed) + ChanOps (7) + Panic (2 frozen) + GcInterop (3) + the M5
surface (GoErrors 7, DeferChain 4 frozen, Goexit 1, GoroutineState,
GoCalls 2) + 11 builtin nodes ≈ 68 named functions. After M4: **68/68
real-or-intentionally-removed, 0 stubbed.**

## 7. M4 work items (from this audit)

1. `Intrinsics.itabFor` → provider hook + phase-3 wiring (red-green:
   itab identity with dispatch path).
2. `Intrinsics.traceback()` → GoroutineState rendering (red-green:
   differential vs gc panic tracebacks, normalized).
3. `//go:linkname` by symbol name, Go→intrinsic (gc symbol names map to
   intrinsics; corpus: fastrand via `runtime.fastrand`/`runtime.fastrandn`).
4. `sync/atomic` typed wrappers (`Int64/Int32/Uint64/Uint32/Bool`) via the
   synthetic overlay (ADR-0002 pattern) for the contention corpus —
   differential vs gc on identical source.
5. intrinsic→Go-helper by symbol name (`Linknames.callGo`) + SetFinalizer
   end-to-end corpus.
6. Write-barrier decision recorded (§4) — no code.
