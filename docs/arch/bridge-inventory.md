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
  intrinsic predicates, the `"syncop"`/`"runtimecaller"` wire nodes, and
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
  - `synth_machinery.go`: `syntheticSources` import overlay (~line 59, 133
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
  - [export.go](../../internal/frontend/export.go): `"syncop"` and
    `"runtimecaller"` custom wire nodes for stdlib functions, and the
    `time.Now` eta-expansion hardcoded by package+symbol name
    ([mr_disk.go](../../internal/frontend/mr_disk.go) ~lines 211–228);
  - [export.go](../../internal/frontend/export.go): the
    package-qualified `sync/atomic` + `internal/runtime/atomic` selector
    interceptions, the same interceptions in the function-value path, and
    the statement-form `atomic.StorePointer` interception — see the B-3
    addendum (2026-11-01, P1-4) below;
  - [export.go](../../internal/frontend/export.go): the `runtime`
    overlay guard block in `exportCall` (Goexit, the `gcinterop` marker
    for `SetFinalizer`/`KeepAlive`/`GC`, and the remaining
    `runtime*` wire nodes) — see the B-3 addendum below;
  - [export.go](../../internal/frontend/export.go): the
    `(*runtime.Func).Name` panic marker in `resolveMethodCall` — see the
    B-3 addendum below (inventory only; the behavior fix belongs to the
    P1-3 lane).
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
### B-3 addendum (2026-11-01, REVIEW-2026-11-01-21 P1-4)

The families below existed in code but had no B-3 entry, so by the
gate's own rule ("grandfathered only while it has an entry here") they
were ungrandfathered yet passing, because the gate's patterns never
matched the `Pkg().Path() == "<stdlib>"` form. They are grandfathered
by this addendum; the gate extension in
[check_no_bridges.py](../../.github/scripts/check_no_bridges.py) (check
2b) now matches that form, so any *new* addition of the same shape
fails the gate. Nothing in this addendum changes behavior: the
interceptions stay exactly as they are until their root-cause fix lands.

#### B-3.1. `sync/atomic` + `internal/runtime/atomic` selector interceptions

- **What:** in [export.go](../../internal/frontend/export.go):
  `exportCall` intercepts package-qualified calls whose resolved
  `*types.Func` satisfies `Pkg().Path() == "sync/atomic"` (~line 8494:
  `LoadPointer` → deref read, `SwapPointer`/`CompareAndSwapPointer` →
  `"atomicptrop"`, word/bool ops → `exportAtomicWordOp` →
  `"atomicaddr"`+`"syncop"`; any other name fails fast with a named
  scope error) and `Pkg().Path() == "internal/runtime/atomic"`
  (~line 8525: word ops via the same `exportAtomicWordOp`, e.g.
  `LoadAcquintptr`/`StoreReluintptr`); `exportFuncCall` repeats both
  interceptions for the function-value path, where the call arrives as
  a resolved `*types.Func` rather than a selector (~lines 9446–9470:
  word ops plus the unqualified in-package `LoadPointer`/`SwapPointer`/
  `CompareAndSwapPointer` forms, e.g. the `atomic.Pointer[T]` method
  bodies); `exportAtomicStorePointerStmt` (~lines 9350–9379) intercepts
  the statement-only `atomic.StorePointer` in both spellings (qualified
  and unqualified in-package) and lowers it to a plain deref write.
  Method calls on the typed wrappers (`slot.Store` on
  `atomic.Pointer[T]`, etc.) are explicitly NOT intercepted here — they
  fall through to the normal method-call path.
- **Why:** the real `sync/atomic` and `internal/runtime/atomic` sources
  now resolve from disk (the synthetic packages were retired), but
  their package-level functions are largely `//go:noescape` bodyless
  functions whose real implementations are assembly/linkname in gc —
  with at least one exception on amd64, where `LoadAcquintptr` has a
  real Go body (`return *ptr`) while `StoreReluintptr` is a
  `//go:noescape` declaration — so there is no Go body for the general
  disk-importer path to compile for the bodyless ones, and the
  `funcByObj` lookup the function-value path would otherwise use has
  nothing to find. The interception is therefore the lowering itself,
  not a shortcut around a compilable body. Much of it is legitimate
  lowering for the atomic-cell representation (see B-2: atomicity is
  load-bearing for the `sync/atomic.*` wrappers; `LoadPointer`/
  `StorePointer` are documented scoped divergences — plain deref
  accesses without gc's ordering guarantee). The defect this entry
  fixes is that the lowering was uninventoried, not that it exists —
  do not rip it out.
- **Root-cause fix:** a general bodyless-function intrinsic mechanism
  (needs design): a way for the backend to provide implementations for
  `//go:noescape` functions that does not key on package path in the
  frontend — e.g. intrinsic declarations carried by the package's own
  import data, or a general assembly/linkname surface — so each of the
  three interception sites above deletes in favor of the same path
  every other bodyless function takes. Until then, extending the
  `atomicWordOps` table is extending this entry, not a new bridge.
- **Phase:** 3 (with B-2's cell machinery).

#### B-3.2. `runtime` overlay guard block in `exportCall`

- **What:** in [export.go](../../internal/frontend/export.go)
  (~lines 8537–8741): `runtime.Goexit()` is intercepted on the
  resolved object (`Pkg().Name() == "runtime"`, so a shadowed local
  named `runtime` is not intercepted) and emitted as a `"call"` node
  with the `goexit` marker; the enclosing guard block (identifier
  spelling `runtime` plus the same resolved-object check) then
  dispatches by symbol name: `SetFinalizer`/`KeepAlive`/`GC` → a
  `"call"` node with the `"gcinterop"` marker naming the intrinsic
  (the factory builds a `CallNode` delegating to
  [GcInterop.java](../../internal/backend/runtime/src/main/java/graalgo/runtime/GcInterop.java));
  `NumCPU` → `"runtimenumcpu"`, `GOMAXPROCS` →
  `"runtimegomaxprocs"`, `ReadMemStats` → `"runtimereadmemstats"`,
  `GOROOT` → `"runtimegoroot"`, `Version` → `"runtimeversion"`,
  `Stack` → `"runtimestack"`, `Callers` → `"runtimecallers"`,
  `CallersFrames` → `"runtimecallersframes"`, `FuncForPC` →
  `"runtimefuncforpc"`; `Caller` fails fast with a named scope error
  because it is multi-result and only the N-target assignment desugar
  (the `"runtimecaller"` node in
  [mrdesugar_m5.go](../../internal/frontend/mrdesugar_m5.go)) may lower
  it. Two members of this block are NOT re-inventoried here because
  they are already adjudicated non-bridges below: `NumGoroutine`
  (`"runtimenumgoroutine"`, PKG-023) and `Gosched`
  (`"runtimegosched"`, PKG-040) — real implementations on scheduler
  primitives, following the same additive-wire-node precedent.
- **Why:** the `runtime` package the frontend sees is the synthetic
  overlay (`syntheticRuntimePackage` in
  [export.go](../../internal/frontend/export.go)): it declares the
  surface with exact gc signatures but provides no bodies, so the
  general call path has nothing to compile. Each member is a host or
  scheduler primitive that cannot be a Go body: terminating the
  goroutine (`Goexit`), JVM GC hooks (`SetFinalizer`/`KeepAlive`/`GC`),
  host queries (`NumCPU`, `GOROOT`, `Version`, `ReadMemStats`), or
  stack metadata that only the backend owns (`Callers` family — see
  also B-7, which covers the backend `rtstack` intrinsics for the same
  surface).
- **Root-cause fix:** Phase 4 (frontend-v2 replaces the name-keyed
  export machinery entirely): a general intrinsic registry in which a
  runtime primitive is declared once (surface + backend node) instead
  of dispatched by symbol name inside `exportCall`; the stack family
  retires with B-7 when real call-site metadata threads through the
  backend. New runtime primitives must follow the adjudicated
  `NumGoroutine`/`Gosched` precedent (additive wire node + real backend
  implementation) and get an adjudication entry, not a silent
  `exportCall` case.
- **Phase:** 4 (stack family with B-7).

#### B-3.3. `(*runtime.Func).Name` panic marker (inventory only)

- **What:** in [export.go](../../internal/frontend/export.go)
  `resolveMethodCall` (~lines 4762–4765): a method call whose receiver
  type is `Func` from package `runtime` and whose method name is
  `Name` returns a marker `methodCall` whose `panicMsg` is
  `"runtime.Func.Name not implemented (no PC-to-function table)"`;
  `methodRefNode` emits a runtime panic for it. This is the same
  finding as REVIEW-2026-11-01-21 P1-3, which owns the behavior fix.
- **Why:** the type surface exists (synthetic, via `AddMethod`) but
  the method is not in `methodIndex`, and there is no PC-to-function
  table yet to implement it from. The marker is recorded here so the
  gate's grandfather list covers it; this lane makes no behavior
  change to it and does not duplicate the P1-3 lane's fix.
- **Root-cause fix:** implement `runtime.Func.Name` from the existing
  PC interning (the `runtimefuncforpc` wire-node precedent in B-3.2),
  or document it as a known gap — as decided by the P1-3 lane.
- **Phase:** 4 (with B-7's real stack metadata).

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

### B-4. 142 generated `synth_*.go` + 12 `p3frontend` mirrors (synthetic stdlib)

- **What:** 142 generated files in [internal/frontend/](../../internal/frontend/)
  (`synth_*.go`: ~24 authored, ~110 verbatim vendored from go1.27.1) plus 12
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
