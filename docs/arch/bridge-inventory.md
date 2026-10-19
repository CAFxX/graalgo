# Bridge inventory

His order (2026-10-15): **no new bridges**; existing bridges are inventoried
here and removed in phased order. A "bridge" is anything that is not the real
implementation: synthetic stdlib packages, Java reimplementations of Go logic,
package-name-keyed special cases, import overlays, custom wire nodes for
stdlib, workarounds, shims.

This file is the gate's grandfather list. The mechanical gate
([check_no_bridges.py](../../.github/scripts/check_no_bridges.py)) fails on
*added* bridge patterns; everything below is grandfathered **only while it
has an entry here**. When a bridge is retired, delete its entry in the same
change (docs are part of the change — see [AGENTS.md](../../AGENTS.md) §7).

Every entry states: what the bridge is, why it exists, the root-cause fix
that retires it, and its phased-removal order. "Needs design" marks entries
whose retirement mechanism is still open — the entry itself is the
deliverable until the design lands.

## Phased removal order

- **Phase 1 — in flight (2026-10-15).** Retire the highest-value synthetic
  packages via root-cause fixes: `fmt` (worktrees `real-fmt2`,
  `real-fmt-rebase`), `crypto/x509` (worktree `real-x509`), `net`/`net/http`
  (worktree `real-nethttp`), `reflect` (worktree `real-reflect2`). Retires
  B-1, B-5, and the reflect/time/math name-keyed predicates in B-3.
- **Phase 2 — package-by-package.** Retire each remaining `synth_*.go` as
  its root cause lands (real trap path for `internal/poll`+`syscall`; the
  `io`/`fs` export fix for `net`; a DNS resolver; …). Every retirement
  shrinks the B-3 predicate chains, the `mrdesugar` pair tables, and
  `syntheticSources`.
- **Phase 3 — machinery deletion.** When the last synthetic package retires,
  delete the machinery wholesale: `syntheticSources` + the import overlay,
  `overlayImporter`/`resolvePkgTree` package switches, the linkname
  allowlist, `isMultiResultSynthetic`/`multiResultSyntheticError`, the
  intrinsic predicates, the `"syncop"`/`"atomicaddr"` wire nodes and the
  `runtime*` stack-inspection wire nodes (`runtimecaller(s)`,
  `runtimecallersframes`, `runtimeframesnext`, `runtimefuncforpc`,
  `runtimefuncname`, `runtimefuncentry`, `runtimefuncfileline`), and
  `NodeFactory.syncPrimitiveName`. Retires B-2 and the rest of B-3.
- **Phase 4 — needs design.** `frontend-rewrite` (frontend-v2
  parser→checker→emitter pipeline) replaces the name-keyed export machinery
  entirely; real `runtime.Callers` replaces the `rtstack` intrinsics; a
  byte-addressed value model (or documented trap contract) resolves
  `unsafe.Add`. Retires B-6, B-7.

## Active bridges

### B-2. `NodeFactory.syncPrimitiveName` (name-keyed sync lowering)

- **What:**
  [NodeFactory.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/NodeFactory.java)
  `syncPrimitiveName` (~line 1908): matches `sync.Mutex`/`sync.Map`/
  `sync/atomic.*` by package/name and lowers them to Java representations
  instead of compiling the real `sync` sources.
- **Why:** the real `sync`/`sync/atomic` sources did not compile; the
  backend special-cased the primitives it needed.
- **Root-cause fix:** compile the real `sync` and `sync/atomic` sources
  (the 2026-10-15 on-demand disk method indexing work already routes `sync`
  through the disk importer — the remaining step is retiring the name-keyed
  lowering once the real package links end-to-end), then delete the method.
- **Partial retirement (PKG-053, 2026-10-16):** `sync.Mutex`/`sync.RWMutex`/
  `sync.WaitGroup`/`sync.Once` no longer lower to cells — their real method
  bodies (compiled from disk) assume struct layout, so they now materialize
  as plain `GoStruct`s and the real bodies run end-to-end. The
  `sync/atomic.*` wrappers keep the cell representation (atomicity is
  load-bearing); their real bodies are bridged by cell-transparent `&x.v`
  ([FieldAddrNode.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/FieldAddrNode.java)).
  `sync.Pool`/`sync.Map` keep their Java objects. Full deletion of the
  method awaits those.
- **Phase:** 3 (after the last synthetic package that depends on it retires).

### B-3. Frontend package-name-keyed special-case machinery

- **What:** in [internal/frontend/](../../internal/frontend/):
  - `synth_machinery.go`: `syntheticSources` import overlay (~line 70, 130
    entries), `isMultiResultSynthetic` (~line 2113) /
    `multiResultSyntheticError` (~line 2207) ~60-path chains,
    `isTimeIntrinsicFunc` (~line 2069), `isMathIntrinsicFunc` (~line 2074),
    `isAliasIntrinsicFunc` (~line 2088), `isAbiFuncPCIntrinsicFunc`
    (~line 1701);
  - [synth_reflect.go](../../internal/frontend/synth_reflect.go):
    `isReflectIntrinsicFunc` (~line 2898);
  - [diskimport.go](../../internal/frontend/diskimport.go):
    `overlayImporter`/`resolvePkgTree` `"runtime"`/`"fmt"`/`"unsafe"`
    switches (~line 402) and the linkname package allowlist;
  - [export.go](../../internal/frontend/export.go): the `runtime*`
    scheduler/environment/stack wire nodes for the synthetic `runtime`
    package (see B-10; the `"syncop"`/`"atomicaddr"` atomic-op nodes are
    covered by B-2, the `time.Now` eta-expansion is hardcoded by
    package+symbol name ([mr_disk.go](../../internal/frontend/mr_disk.go)
    ~lines 211–228), and `t.C` on `*time.Timer`/`*time.Ticker` lowers to
    the `"timec"` node (see B-8).
- **Why:** each synthetic package needed bespoke import routing,
  multi-result desugaring, and intrinsic hooks; the machinery grew
  package-by-package instead of through a general mechanism.
- **Root-cause fix:** retire with the synthetic packages (Phase 2 shrinks
  the tables; Phase 3 deletes the machinery). The general mechanism is the
  disk importer + on-demand method indexing; the `time.Now` case needs a
  general eta-expansion for any selector-expr function value ("needs
  design" for the mechanism — the inventory entry is the deliverable now).
  In flight: `real-reflect2` already drops stale synthetic-reflect entries;
  `frontend-rewrite` (frontend-v2) replaces this machinery entirely
  (Phase 4).
- **Phase:** 1 (reflect/time/math predicates) → 2 (shrink) → 3 (delete).
- **Note (2026-10-16, PKG-023 independent review + PKG-038):** completing
  the *declared view* of the already-inventoried dedicated stubs
  (`syntheticRuntimePackage` in [export.go](../../internal/frontend/export.go),
  `syntheticFmtPackage`, `syntheticUnsafePackage`) is NOT a new bridge.
  The mechanical gate flags `addFunc(` lines in `export.go`, but the
  overlay's existence is grandfathered by this entry — extending its
  declared API surface toward gc's documented API adds no new bridge
  machinery (no new synth files, no new wire nodes, no new
  package-name-keyed branches). The PKG-023 reviewer judged the flag a
  mechanical false positive, not a veto; the same judgment covers the
  PKG-038 runtime-view completion.

### B-4. 140 generated `synth_*.go` + 12 `p3frontend` mirrors (synthetic stdlib)

- **What:** 140 files in [internal/frontend/](../../internal/frontend/)
  matching `synth_*.go` (129 implementation files + 11 `synth_*_test.go`
  test files; ~24 authored, the rest verbatim-ported from go1.27.1) plus 12
  `synth_*.go` mirrors under
  [internal/frontend/p3frontend/](../../internal/frontend/p3frontend/) —
  synthetic reimplementations of stdlib packages standing in for the real
  sources.
- **Why:** the real stdlib sources did not compile under graalgo
  (`go:linkname`, intrinsic surface, `unsafe` patterns); synthetics bought
  coverage fast (his 2026-10-10 dual-track decision).
- **Root-cause fix:** per-package: fix the compiler/runtime cause that
  forces the synthetic (the 2026-10-15 directive: only packages that
  ABSOLUTELY MUST be synthetic survive, with a documented, specific,
  verifiable reason), then delete the `synth_*.go`. The `p3frontend`
  mirrors retire with the top-level tree or via an explicit retire-or-keep
  decision. In flight: `real-fmt2`/`real-fmt-rebase` (`fmt`),
  `real-x509` (`crypto/x509`, "remove synthetic crypto/x509, route via
  general disk mechanism"), `real-nethttp` (`net`/`net/http`),
  `real-reflect2` (`reflect`).
- **Phase:** 1 (the four in-flight packages) → 2 (remainder).

### B-5. `GoX509` hand-rolled elliptic-curve arithmetic + DER parsing

- **What:**
  [GoX509.java](../../internal/backend/interop/src/main/java/graalgo/p3x509/GoX509.java):
  hand-rolled `scalarMultiply`/`ecAdd`/`ecDouble` and byte
  pattern-matching DER helpers (`extractBitString`, `skipLength`,
  `derLength`, `parsePkcs1Integers`, `extractSec1PrivateKey`, …) instead of
  delegating to JDK crypto (`java.security`).
- **Why:** expedience during the TLS bring-up; the directive (prefer JDK
  delegation when semantically compatible) was not applied.
- **Root-cause fix:** delegate to the JDK (e.g. derive SEC1 public points
  via `KeyAgreement("ECDH")`, parse with `CertificateFactory`) or document
  specifically why the JDK cannot cover a case, with differential tests vs
  gc `x509.ParseECPrivateKey` over P-224/256/384/521. In flight: worktree
  `real-x509` (coordinate — it retires synthetic `crypto/x509`, the Go-side
  counterpart).
- **Phase:** 1.

### B-6. `unsafe.Add` always traps (bridge by omission)

- **What:**
  [UnsafeOps.java](../../internal/backend/interop/src/main/java/graalgo/p3unsafe/UnsafeOps.java)
  `addTrap` (~line 248) throws on `unsafe.Add` — a legal, common gc
  operation (used by `reflect`, `encoding/binary` internals) — killing the
  program with an internal error.
- **Why:** the value model has no byte-addressed pointer arithmetic yet.
- **Root-cause fix:** needs design — a byte-addressed value model; at
  minimum, trap with a Go panic carrying the documented message instead of
  an internal-error crash.
- **Phase:** 4.

### B-7. `rtstack` synthetic `runtime` stack intrinsics

- **What:** [rtstack/](../../internal/backend/interop/src/main/java/graalgo/rtstack/)
  (`RuntimeCallerNode`, `RuntimeCallersNode`, `RuntimeCallersFramesNode`,
  `RuntimeFramesNextNode`, `RtStack`): synthetic implementations of the
  `runtime.Callers`/`CallersFrames`/`Func` surface, sound but not the real
  runtime's stack walker.
- **Why:** the real runtime stack metadata was not available to the
  backend; intrinsics filled the gap.
- **Root-cause fix:** needs design — thread real call-site metadata
  through the backend so `runtime.Callers` walks the actual guest stack;
  then delete the synthetic intrinsics.
- **Phase:** 4.

### B-8. `time*` wire-node family (synthetic `time` intrinsics)

- **What:** eight additive wire nodes emitted for the synthetic `time`
  package (P3-M5 WS E, ADR-0005 D1): `timenow`/`timesleep`/`timeafter`/
  `timetick`/`timenew` (timer + ticker kinds)/`timeafterfunc` from
  `timeIntrinsicNode` in
  [synth_machinery.go](../../internal/frontend/synth_machinery.go)
  (~line 1452); `timeop` for `(*time.Timer).Stop/Reset` and
  `(*time.Ticker).Stop/Reset` (`exportTimeOp`, ~line 1734); `timec` for
  the `t.C` field on `*time.Timer`/`*time.Ticker` (the Go values are
  opaque Java objects; the struct field exists only for type-checking —
  [export.go](../../internal/frontend/export.go) ~line 11507). Backend:
  `graalgo.p3time.nodes` (`TimeNowNode`, `TimeSleepNode`,
  `TimeAfterNode`, `TimeTickNode`, `TimeNewNode`, `TimeAfterFuncNode`,
  `TimeOpNode`, `TimeCNode`), registered in
  [P3KindRegistrations.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3KindRegistrations.java)
  (~lines 105–112), on the scheduler timer heap in
  [Timers.java](../../internal/backend/runtime/src/main/java/graalgo/runtime/Timers.java).
- **Why:** the real `time` sources did not compile; the synthetic package
  declares the timer API and the backend reimplements the timer
  machinery in Java.
- **Root-cause fix:** compile the real `time` sources (Phase 2); the
  timer heap becomes the trap path for the real runtime's timer code,
  and these nodes retire with the synthetic package.
- **Note (2026-10-19):** this entry closes the P1-5(a) staleness item —
  on disk at `56a3dd68` B-2 contains no `Timers` text at all (the
  "stubbed sleep/timer-heap" description does not exist in the current
  inventory); the current code state is: `Timers.java` claims sleep is
  "now real" (parks the goroutine on the scheduler timer heap,
  `Timers.java:34-38`), with `TimeAfter/Timer/Ticker` delivery nodes and
  differential tests (`TimerTest`, `TimerDifferentialTest`,
  `test/corpus/phase2/timers/tm01–tm09`). Whether timers work
  end-to-end (the PKG-048 claim in `Makefile:153` /
  `test/regression/pkg031/repeat_test.go:30`) is owned by the P1-1
  verify-or-fix lane — this entry documents the code's claims, not the
  verdict.

### B-9. `math*` wire-node family (synthetic `math` intrinsics)

- **What:** six wire nodes — `mathf64bits`, `mathf64frombits`,
  `mathf32bits`, `mathf32frombits`, `mathsqrt`, `mathfma` — emitted from
  the intrinsic map in
  [synth_machinery.go](../../internal/frontend/synth_machinery.go)
  (~line 1588) for the synthetic `math` package. Backend: pure-arithmetic
  nodes registered in
  [P3KindRegistrations.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3KindRegistrations.java)
  (~lines 130–140).
- **Why:** the real `math` sources did not compile; these six functions
  lower directly to backend arithmetic instead of Go bodies.
- **Root-cause fix:** retire with the synthetic `math` package (Phase 2).
  Alternative: adjudicate as permanent gc-parity intrinsics — gc itself
  intrinsifies `Sqrt`/`FMA`/bit-casts, so the wire node IS the real
  implementation shape; no independent adjudication has been requested
  yet, so they stay inventoried here.
- **Phase:** 2.

### B-10. `runtime*` wire-node family (synthetic `runtime` overlay API)

- **What:** sixteen additive wire nodes emitted in the synthetic
  `runtime` package's guard block in
  [export.go](../../internal/frontend/export.go) (~lines 8505–8720) and
  [mrdesugar_m5.go](../../internal/frontend/mrdesugar_m5.go)
  (`desugarRuntimeStack`, ~line 3183):
  - *Scheduler/host primitives:* `runtimenumcpu`, `runtimenumgoroutine`,
    `runtimegosched`, `runtimegomaxprocs`.
  - *Stack inspection:* `runtimecallers`, `runtimecaller`,
    `runtimecallersframes`, `runtimeframesnext`, `runtimefuncforpc`,
    `runtimefuncname`, `runtimefuncentry`, `runtimefuncfileline`
    (backend: [rtstack/](../../internal/backend/interop/src/main/java/graalgo/rtstack/)).
  - *Environment introspection:* `runtimereadmemstats`, `runtimegoroot`,
    `runtimeversion`, `runtimestack` (backend:
    [rtmemstats/](../../internal/backend/interop/src/main/java/graalgo/rtmemstats/)).
  Backend nodes registered in
  [P3KindRegistrations.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3KindRegistrations.java)
  (~lines 147–185) via the public `registerExprKind`/`registerStmtKind`
  hooks — no Phase-1 factory change. (The review's P1-2 list missed the
  five variable-emitted kinds `runtimecaller`/`runtimeframesnext`/
  `runtimefuncname`/`runtimefuncentry`/`runtimefuncfileline`; the full
  set was re-enumerated from disk for this entry.)
- **Why:** the synthetic `runtime` overlay declares these symbols with no
  bodies; the frontend lowers calls to wire nodes and the backend
  implements them.
- **Root-cause fix / verdict, by sub-family:**
  - *Scheduler/host primitives — real implementations (adjudicated
    precedent, evidence below):*
    [NumCPUNode.java](../../internal/backend/interop/src/main/java/graalgo/p3builtin/nodes/NumCPUNode.java)
    returns the host processor count;
    [NumGoroutineNode.java](../../internal/backend/interop/src/main/java/graalgo/p3builtin/nodes/NumGoroutineNode.java)
    reads the scheduler's live-goroutine counter (PKG-023, independent
    review CLEAN, gate-allowlisted);
    [GoschedNode.java](../../internal/backend/interop/src/main/java/graalgo/p3builtin/nodes/GoschedNode.java)
    calls `SchedulerCore.yieldCurrent()` (PKG-040, independent review
    CLEAN, gate-allowlisted);
    [GoMaxProcsNode.java](../../internal/backend/interop/src/main/java/graalgo/p3builtin/nodes/GoMaxProcsNode.java)
    keeps a process-wide cell initialized to the host CPU count with gc
    set/return-previous semantics. These four predate the gate and were
    never gate-flagged; the PKG-023/PKG-040 adjudications cite "the
    `runtimenumcpu` precedent" — this entry is that precedent, recorded
    with its evidence, closing the circular justification the review
    flagged. Formal gate-allowlist adjudication for `runtimenumcpu`/
    `runtimegomaxprocs` still needs a fresh independent review before
    they move to `ADJUDICATED_WIRE_NODES`.
  - *Stack inspection — bridges:* synthetic `rtstack` implementations of
    `runtime.Callers`/`CallersFrames`/`FuncForPC`/etc. Retire with B-7
    (Phase 4: real call-site metadata threading).
  - *Environment introspection — bridges with no Go-side retirement:*
    the launcher/VM is the only possible source of truth for GOROOT,
    the toolchain version string, and JVM memory stats; no real Go
    source could compute these. Keep inventoried; recommend an
    independent adjudication as permanent environment intrinsics.
- **Phase:** 4 (stack) / permanent-intrinsic adjudication (environment);
  the scheduler primitives stand as adjudicated.

### B-11. `unsafe*` wire-node family (synthetic `unsafe` intrinsics)

- **What:** eight wire nodes — `unsafeslice`, `unsafeslicedata`,
  `unsafestring`, `unsafestringdata`, `unsafeadd`, `unsafefromptr`,
  `unsafefromuint`, `unsafetooptr` — emitted from
  [synth_unsafe.go](../../internal/frontend/synth_unsafe.go) (~lines
  138–233) for the synthetic `unsafe` package. Backend:
  [p3unsafe/](../../internal/backend/interop/src/main/java/graalgo/p3unsafe/)
  (`UnsafeSliceNode`, `UnsafeStringNode`, `UnsafeAddNode`, …),
  registered in
  [P3KindRegistrations.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3KindRegistrations.java)
  (~lines 188–202).
- **Why:** the value model has no byte-addressed pointer arithmetic, so
  `unsafe` conversions are implemented as backend nodes over the Go
  value representations.
- **Root-cause fix:** the byte-addressed value model (or documented trap
  contract) — Phase 4, same as B-6 (`unsafeadd` already traps there via
  `UnsafeOps.addTrap`).
- **Phase:** 4.

### B-12. Miscellaneous stdlib wire nodes

- **`abifuncpc`** — the `internal/abi` `FuncPCABI0`/`FuncPCABIInternal`
  compile intrinsics (real-reflect Phase 1). Emission:
  `exportAbiFuncPCIntrinsic` in
  [synth_machinery.go](../../internal/frontend/synth_machinery.go)
  (~line 1665). Backend:
  [AbiFuncPCNode.java](../../internal/backend/interop/src/main/java/graalgo/p3abi/AbiFuncPCNode.java)
  returns an opaque stable func id from the
  [FuncIds](../../internal/backend/truffle/src/main/java/graalgo/values/FuncIds.java)
  registry (same func → same id, non-zero for non-nil funcs) — gc
  implements these as compile intrinsics returning the entry PC, and
  graalgo has no PCs, so the opaque id IS the gc-parity mechanism
  (resolvable back to the `GoFunc` by the Phase-4 `reflectcall`
  linkname). Verdict: permanent intrinsic mechanism, not a logic shim;
  retirement only with a real PC model (not planned). Note: registered
  only in
  [P3SyncMain.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3SyncMain.java)
  (~line 444), not in `P3KindRegistrations` — registration asymmetry to
  reconcile with the launcher audit.
- **`atomicaddr`** — wraps the `*T` operand of `sync/atomic` word ops in
  a delegating `AtomicLong`/`AtomicInteger` cell so the existing
  `AtomicOpNode` serves the op unchanged (emission:
  `exportAtomicWordOp` in
  [export.go](../../internal/frontend/export.go) ~line 9206; backend
  [AtomicAddrNode.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/nodes/AtomicAddrNode.java)).
  Lowering glue for the B-2 cell representation; retires with B-2 (Phase 3).
- **`reflectop`** — the synthetic `reflect` package's `__reflect_*`
  intrinsics (emission: `exportReflectIntrinsic` in
  [synth_reflect.go](../../internal/frontend/synth_reflect.go) ~line
  2914; backend
  [ReflectOpNode.java](../../internal/backend/interop/src/main/java/graalgo/p3reflect/ReflectOpNode.java)).
  Retires with the synthetic `reflect` package (Phase 1, `real-reflect2`
  in flight).
- **`sliceoverlap`** — `crypto/internal/fips140/alias.AnyOverlap`/
  `InexactOverlap` (emission: `exportAliasIntrinsic` in
  [synth_machinery.go](../../internal/frontend/synth_machinery.go)
  ~line 1615). The real package uses `uintptr(unsafe.Pointer(...))`,
  which traps under D5; the node implements overlap detection directly
  on `GoSlice` values (array identity + range intersection), registered
  in the Phase-1
  [NodeFactory.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/NodeFactory.java)
  (~line 372). Retires with the byte-addressed value model (Phase 4).
- **`syncop`** — `sync/atomic` word ops (via `exportAtomicWordOp`) and
  the synthetic `sync` package's multi-result `Map` methods (via
  `desugarSyncMap` in
  [mrdesugar_m5.go](../../internal/frontend/mrdesugar_m5.go) ~line 3294).
  Backend:
  [SyncOpNode.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/nodes/SyncOpNode.java) /
  [AtomicOpNode.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/nodes/AtomicOpNode.java).
  Retires with B-2/B-3 (Phase 3). Note: the backend also registers a
  `"syncnew"` kind that the frontend never emits (legacy of the old
  construction path — delete with the Phase-3 machinery).
- **Phase:** 1 (`reflectop`) → 3 (`syncop`/`atomicaddr`) → 4
  (`sliceoverlap`); `abifuncpc` is a permanent mechanism.

## Adjudicated non-bridges

Mechanical gate ([check_no_bridges.py](../../.github/scripts/check_no_bridges.py))
false positives: additions the gate flags by pattern that an independent
reviewer adjudicated as real implementations, not bridges. Each entry names
the commit, the reviewer verdict, and the narrowly-scoped allowlist in the
gate script (`ADJUDICATED_WIRE_NODES` / `ADJUDICATED_REGISTRATIONS`). Do NOT
extend these entries or add new ones without a fresh independent review.

- **2026-10-16 — `runtimenumgoroutine` wire node + `runtime.NumGoroutine`
  overlay declaration (PKG-023, commit `1d4e7964`).** The gate flags two
  added lines in [internal/frontend/export.go](../../internal/frontend/export.go):
  the additive `"runtimenumgoroutine"` wire node and
  `addFunc("NumGoroutine", ...)`. Independent review: CLEAN, no veto —
  adjudicated a mechanical false positive (first overlay addition since the
  gate landed). This is a real implementation following the `runtimenumcpu`
  precedent: the frontend declares `NumGoroutine() int` in the synthetic
  `runtime` package and lowers calls to the additive wire node; the backend
  [NumGoroutineNode.java](../../internal/backend/interop/src/main/java/graalgo/p3builtin/nodes/NumGoroutineNode.java)
  implements it on the scheduler's live-goroutine counter
  ([SchedulerCore.numGoroutines()](../../internal/backend/runtime/src/main/java/graalgo/sched/SchedulerCore.java)),
  verified by `SchedulerCoreTest.testNumGoroutines` and
  `TestRuntimeNumGoroutineExport`. Not a bridge: no synthetic stdlib logic,
  no name-keyed special case — the same mechanism as the grandfathered
  `runtimenumcpu`.

- **2026-10-16 — `runtimegosched` wire node + `runtime.Gosched` overlay
  declaration (PKG-040, lane commit `19786941` + coordinator adjudication).** The gate
  flags the additive `\"runtimegosched\"` wire node in
  [internal/frontend/export.go](../../internal/frontend/export.go); the
  `addFunc(\"Gosched\", ...)` overlay declaration was provided by PKG-038
  (master `bfc4b8e6`) and the lane's duplicate was dropped at integration.
  Independent review: CLEAN, no veto — adjudicated a mechanical false
  positive. This is a real implementation following the
  `runtimenumgoroutine` precedent: the frontend lowers calls to the
  additive STATEMENT wire node (Gosched returns no value, so bare
  statements return it unwrapped per the `chanclose` precedent, and
  defer/go positions desugar through a zero-arg funclit per the
  `exportCloseDeferGo` precedent); the backend
  [GoschedNode.java](../../internal/backend/interop/src/main/java/graalgo/p3builtin/nodes/GoschedNode.java)
  implements it on the scheduler's voluntary-yield primitive
  ([SchedulerCore.yieldCurrent()](../../internal/backend/runtime/src/main/java/graalgo/sched/SchedulerCore.java)),
  registered via the public `registerStmtKind` hook in
  `P3KindRegistrations`/`P3SyncMain` (no Phase-1 factory change),
  verified by `SchedulerCoreTest.testGoschedYields`,
  `TestRuntimeGoschedExport`, and `TestRuntimeGoschedDeferGoExport`. Not a
  bridge: no synthetic stdlib logic, no name-keyed special case — the
  call interception reuses the existing general runtime-overlay guard
  block, exactly like `NumGoroutine`.

## Retired

Entries move here when the bridge is gone (same change that removes the
code, per [AGENTS.md](../../AGENTS.md) §7).

- **2026-10-16 — B-1 `FmtCallNode` + `graalgo.runtime.Fmt`/`FmtScan`
  (Java reimplementations of `fmt`).** The real `fmt` package now compiles
  from GOROOT via the general disk importer (real-fmt2 lane, master
  `c2c0718c`): synthetic `fmt` registration removed from
  `dedicatedSyntheticStubs`, `exportFmtCall`/`fmtOverlayFuncs` deleted from
  the frontend, and 5,091 lines of Java (`Fmt.java`, `FmtScan.java`,
  `FmtCallNode.java`, `FmtHandleMethods.java`, `FmtMethodDispatch.java` plus
  tests) deleted. `fmt.Append`/`Appendf`/`Appendln` verified working from
  the real package.
- **2026-10-15 — stale `# BRIDGE` self-labels in module Makefiles.** The
  comments in
  [internal/backend/interop/Makefile](../../internal/backend/interop/Makefile)
  and
  [internal/backend/truffle/Makefile](../../internal/backend/truffle/Makefile)
  awaited code-moves that had already landed (frontend at
  `internal/frontend/`, corpus at `test/corpus/phase1`); the variables they
  guarded were already repointed. Retired by deleting the stale comments
  (comment-only change; the exports they described are harmless
  redundancies). Remaining stale labels: the `# BRIDGE (frontend lane)`
  comments in the launcher scripts
  [internal/backend/graalgo-run](../../internal/backend/graalgo-run) and
  [internal/backend/graalgo-sync-run](../../internal/backend/graalgo-sync-run)
  (the guarded `[ -x …/frontend/gofrontend ]` fallback resolves to the
  correct path by accident of layout; delete the comment or the dead
  block), and the dangling `internal/backend/runtime/corpus` symlink
  (owned by the test-infra lane — delete symlink + comment).
- **2026-10-15 — banned `fmt.Fprint`→`w.Write` desugar.** Removed before
  this inventory was created (the real-`fmt` lane's template: kill the
  desugaring, make the real package work). Recorded here so the pattern is
  not reintroduced.
