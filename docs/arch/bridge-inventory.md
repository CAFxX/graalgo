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

**Re-derived from disk on HEAD `21fa01d9` (2026-11-01, review R-05).**
Counts, file paths, and line numbers below are HEAD values, re-counted with
anchored patterns (`synth_.*\.go$`, the `syntheticSources` map body, the
`P3KindRegistrations`/`P3SyncMain` registration sites, and the frontend
`e.node("kind", …)` emission sites). The unintegrated 2026-10-19
`rem-bridge-inventory` branch (`895976b4`) was used only as a lead: every
kind it listed was re-verified on HEAD, two kinds it missed
(`unsafederive`, `atomicptrop`) are included here, and two of its claims
are corrected (the `abifuncpc` registration asymmetry is resolved on HEAD;
`reflectop` did NOT retire with the synthetic reflect package). Numbers are
never reused: B-1 and B-6 are retired (see Retired), so the Active list
runs B-2–B-5, B-7–B-12.

## Phased removal order

- **Phase 1 — done except `crypto/x509` and `net` (2026-11-01).** The
  2026-10-15 in-flight list is now mostly retired: `fmt` (B-1, master
  `c2c0718c`), `reflect` (2026-10-16, `d2804a52` — see Retired),
  `net/http` + `net/http/httptest` (2026-10-16, `cc0a78f4` — see Retired).
  Still synthetic: `crypto/x509` (B-5, active EC/DER-key half) and `net`
  (B-4). The reflect/time/math name-keyed predicates in B-3 did NOT all
  retire with their packages: the time/math predicates still serve the
  synthetic `time`/`math` packages, and the reflect predicate survives as
  the `reflect_m7` helper-table predicate (see B-3).
- **Phase 2 — package-by-package.** Retire each remaining `synth_*.go` as
  its root cause lands (real trap path for `internal/poll`+`syscall`; the
  `io`/`fs` export fix for `net`; a DNS resolver; …). Every retirement
  shrinks the B-3 predicate chains, the `mrdesugar` pair tables, and
  `syntheticSources`.
- **Phase 3 — machinery deletion.** When the last synthetic package retires,
  delete the machinery wholesale: `syntheticSources` + the import overlay,
  `overlayImporter`/`resolvePkgTree` package switches, the linkname
  allowlist, `isMultiResultSynthetic`/`multiResultSyntheticError`, the
  intrinsic predicates, the `"syncop"`/`"atomicaddr"`/`"atomicptrop"`/
  `"syncnew"` wire nodes, and `NodeFactory.syncPrimitiveName`. Retires
  B-2, B-12 (in part), and the rest of B-3. The `"runtimecaller"` family
  does NOT retire here — it retires in Phase 4 with B-7/B-10.
- **Phase 4 — needs design.** `frontend-rewrite` (frontend-v2
  parser→checker→emitter pipeline) replaces the name-keyed export machinery
  entirely; real `runtime.Callers` replaces the `rtstack` intrinsics; a
  byte-addressed value model (or documented trap contract) resolves the
  remaining `unsafe` derivation surface (B-11 `unsafederive`,
  B-12 `sliceoverlap`). Retires B-7, B-11 (in part), B-12 (in part).
  B-6 (`unsafe.Add` traps) is already retired — see Retired.

## Active bridges

### B-2. `NodeFactory.syncPrimitiveName` (name-keyed sync lowering)

- **What:**
  [NodeFactory.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/NodeFactory.java)
  `syncPrimitiveName` (line 1963): matches `sync.*`/`sync/atomic.*` by
  package/name and lowers them to Java representations instead of
  compiling the real `sync` sources.
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
  `sync.Pool` keeps its Java object; `sync.Map` no longer does
  (fix-redmaster-r42: go1.27's `sync.Map` is a thin wrapper over its `m`
  HashTrieMap field, so as a plain `GoStruct` the disk method bodies work
  end-to-end). Full deletion of the method awaits `sync.Pool` and the
  `sync/atomic.*` cell representation.
- **Phase:** 3 (after the last synthetic package that depends on it retires).

### B-3. Frontend package-name-keyed special-case machinery

- **What:** in [internal/frontend/](../../internal/frontend/) (line
  numbers re-derived on HEAD `21fa01d9`):
  - `synth_machinery.go`: `syntheticSources` import overlay (map at lines
    70–339: 130 active entries, plus 2 commented-out retired keys
    `"reflect"` / `"internal/reflectlite"` = 132 key lines),
    `isMultiResultSynthetic` (line 2275) /
    `multiResultSyntheticError` (line 2369),
    `isTimeIntrinsicFunc` (line 2193), `isMathIntrinsicFunc` (line 2198),
    `isAliasIntrinsicFunc` (line 2212), `isAbiFuncPCIntrinsicFunc`
    (line 1654);
  - [synth_reflect.go](../../internal/frontend/synth_reflect.go):
    `isReflectIntrinsicFunc` (line 2908) — survives as the `reflect_m7`
    helper-table predicate (see B-4/Retired: the public reflect package
    retired, the helper table did not);
  - [export.go](../../internal/frontend/export.go): `overlayImporter`
    type (line 584) with `Import` (line 603) and
    `isDedicatedSyntheticStub` (line 598) — the dedicated stubs are now
    only `"runtime"` and `"unsafe"` (`fmt` retired with B-1; the
    `syntheticFmtPackage` name survives only in the `p3frontend` mirror
    importer, [importer.go](../../internal/frontend/p3frontend/importer.go)
    line 329);
  - [diskimport.go](../../internal/frontend/diskimport.go):
    `resolvePkgTree` (line 403, delegating to `isDedicatedSyntheticStub`)
    and the linkname package allowlist;
  - [export.go](../../internal/frontend/export.go): `"syncop"` (line
    9302) and `"runtimecaller"` (via `desugarRuntimeStack` in
    [mrdesugar_m5.go](../../internal/frontend/mrdesugar_m5.go) line 3261)
    custom wire nodes for stdlib functions, and the `time.Now`
    eta-expansion hardcoded by package+symbol name
    ([mr_disk.go](../../internal/frontend/mr_disk.go) lines 229+).
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
- **Phase:** 2 (shrink as packages retire) → 3 (delete). The reflect
  predicate's public-package half retired with reflect; its helper-table
  half (`reflect_m7`) survives until the M7 tables are reworked.
- **Note (2026-10-16, PKG-023 independent review + PKG-038; updated
  2026-11-01):** completing the *declared view* of the
  already-inventoried dedicated stubs (`syntheticRuntimePackage` in
  [export.go](../../internal/frontend/export.go),
  `syntheticUnsafePackage`) is NOT a new bridge. The `syntheticFmtPackage`
  named in the original note no longer exists in the top-level frontend
  (fmt retired with B-1); the name survives only as the `p3frontend`
  mirror's frozen stub (see above).
  The mechanical gate flags `addFunc(` lines in `export.go`, but the
  overlay's existence is grandfathered by this entry — extending its
  declared API surface toward gc's documented API adds no new bridge
  machinery (no new synth files, no new wire nodes, no new
  package-name-keyed branches). The PKG-023 reviewer judged the flag a
  mechanical false positive, not a veto; the same judgment covers the
  PKG-038 runtime-view completion.

### B-4. 140 `synth_*.go` + 12 `p3frontend` mirrors (synthetic stdlib)

- **What:** 140 files in [internal/frontend/](../../internal/frontend/)
  matching `synth_*.go` on HEAD: 132 non-test sources + 8 `*_test.go`
  files — plus 12 `synth_*.go` mirrors under
  [internal/frontend/p3frontend/](../../internal/frontend/p3frontend/) =
  152 matching files in total. The 132 non-test sources include
  `synth_testing.go`, `synth_testing_iotest.go`, and
  `synth_testing_quick.go`, which implement Go's `testing` packages —
  they are NOT test files (an earlier count of "11 test files" made that
  mistake; the anchored count is 8 `*_test.go`). The old "~24 authored,
  ~110 verbatim vendored" split is dropped: a header scan of the 132
  classifies 27 as generated and 56 as vendored/ported, with 49 carrying
  neither marker, so the split was not reproducible. The 130 active
  `syntheticSources` entries (B-3) are two fewer than the 132 sources
  because two of the counted files are not registered packages:
  `synth_machinery.go` (the machinery itself) and `synth_unsafe.go`
  (the `unsafe` package is a dedicated stub, B-3, not a
  `syntheticSources` entry). Multi-file packages (e.g. the `crypto/cipher`
  main + GCM fragments) register once per import path. A
  whole-tree anchored `synth_.*\.go$` count is 155, but 3 of those are
  not synth packages (`cmd/graalgo/testsynth_test.go`,
  `tools/stdlib-linkdiff/synth_test.go`, and the generator helper
  `internal/frontend/gen_synth_helper/extract_decls/main.go`) — do not
  use the whole-tree count for this entry.
- **Why:** the real stdlib sources did not compile under graalgo
  (`go:linkname`, intrinsic surface, `unsafe` patterns); synthetics bought
  coverage fast (his 2026-10-10 dual-track decision).
- **Root-cause fix:** per-package: fix the compiler/runtime cause that
  forces the synthetic (the 2026-10-15 directive: only packages that
  ABSOLUTELY MUST be synthetic survive, with a documented, specific,
  verifiable reason), then delete the `synth_*.go`. The `p3frontend`
  mirrors retire with the top-level tree or via an explicit retire-or-keep
  decision. Retired since the last inventory update: `fmt` (B-1),
  `reflect` (public package; the `reflect_m7` helper source remains —
  see Retired), `net/http` + `net/http/httptest` (see Retired). Still
  synthetic, in flight: `crypto/x509` (B-5, "remove synthetic
  crypto/x509, route via general disk mechanism") and `net`.
- **Phase:** 1 (`crypto/x509`, `net`) → 2 (remainder).

### B-5. `GoX509` hand-rolled elliptic-curve arithmetic + DER key parsing (active half only)

- **What:**
  [GoX509.java](../../internal/backend/interop/src/main/java/graalgo/p3x509/GoX509.java):
  hand-rolled `scalarMultiply` (line 764) / `ecAdd` (line 782) /
  `ecDouble` (line 812) and byte pattern-matching DER helpers
  (`extractBitString` line 310, `skipLength` line 350, `derLength` line
  366, `parsePkcs1Integers` line 885, `extractSec1PrivateKey` line 817)
  instead of delegating to JDK crypto (`java.security`). The EC
  arithmetic derives the public point from a SEC1 private key (`trySec1`,
  line 730); the DER helpers parse SPKI / PKCS#1 / SEC1 *key* material.
  This entry covers the key-parsing half ONLY: certificate parsing itself
  was delegated to the JDK long ago and is recorded in Retired (B-5,
  part) — the previous version of this entry conflated the two halves.
- **Why:** expedience during the TLS bring-up; the directive (prefer JDK
  delegation when semantically compatible) was not applied to key parsing.
- **Root-cause fix:** delegate to the JDK (e.g. derive SEC1 public points
  via `KeyAgreement("ECDH")`, parse keys with `KeyFactory`) or document
  specifically why the JDK cannot cover a case, with differential tests vs
  gc `x509.ParseECPrivateKey` over P-224/256/384/521. In flight: worktree
  `real-x509` (coordinate — it retires synthetic `crypto/x509`, the Go-side
  counterpart). Note: the TLS verification audit (review R-08 lane) owns
  the question of whether this hand-rolled code is on a
  security-critical path; this entry records its existence, not its audit
  verdict.
- **Phase:** 1.

### B-7. `rtstack` synthetic `runtime` stack intrinsics

- **What:** [rtstack/](../../internal/backend/interop/src/main/java/graalgo/rtstack/)
  (`RuntimeCallerNode`, `RuntimeCallersNode`, `RuntimeCallersFramesNode`,
  `RuntimeFramesNextNode`, `RuntimeFuncForPCNode`, `RuntimeFuncNameNode`,
  `RuntimeFuncEntryNode`, `RuntimeFuncFileLineNode`, `RtStack`):
  synthetic implementations of the
  `runtime.Callers`/`CallersFrames`/`Func` surface, sound but not the real
  runtime's stack walker. The wire-node kinds these back are inventoried
  as the stack sub-family of B-10; this entry is the backend
  implementation, B-10 is the frontend emission — they retire together.
- **Why:** the real runtime stack metadata was not available to the
  backend; intrinsics filled the gap.
- **Root-cause fix:** needs design — thread real call-site metadata
  through the backend so `runtime.Callers` walks the actual guest stack;
  then delete the synthetic intrinsics.
- **Phase:** 4.

### B-8. `time*` wire-node family (synthetic `time` intrinsics)

- **What:** eight wire nodes for the synthetic `time` package, emitted
  from `timeIntrinsicNode` in
  [synth_machinery.go](../../internal/frontend/synth_machinery.go) (line
  1453), `exportTimeOp` in the same file (line 1809), and `timec`
  handling in [export.go](../../internal/frontend/export.go):
  `timenow` (line 1465), `timesleep` (line 1471), `timeafter` (line
  1477), `timetick` (line 1483), `timenew` (timer line 1489, ticker line
  1495 — one kind, two variants), `timeafterfunc` (line 1500),
  `timeop` for `(*time.Timer).Stop/Reset` and `(*time.Ticker).Stop/Reset`
  ([synth_machinery.go](../../internal/frontend/synth_machinery.go) line
  1838), `timec` for the `t.C` field on `*time.Timer`/`*time.Ticker`
  ([export.go](../../internal/frontend/export.go) line 11839 — the Go
  values are opaque Java objects; the struct field exists only for
  type-checking). Backend:
  [p3time/nodes/](../../internal/backend/interop/src/main/java/graalgo/p3time/nodes/)
  (`TimeNowNode`, `TimeSleepNode`, `TimeAfterNode`, `TimeTickNode`,
  `TimeNewNode`, `TimeAfterFuncNode`, `TimeOpNode`, `TimeCNode`),
  registered in
  [P3KindRegistrations.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3KindRegistrations.java)
  (lines 132–139).
- **Why:** the real `time` sources did not compile; the synthetic package
  declares the timer API and the backend reimplements the timer
  machinery in Java.
- **Root-cause fix:** compile the real `time` sources (Phase 2); these
  nodes retire with the synthetic package. Note: whether timers work
  end-to-end (the PKG-048 claim in `Makefile` /
  `test/regression/pkg031/repeat_test.go`) is owned by the R-06 lane —
  this entry documents the mechanism, not the verdict.
- **Phase:** 2.

### B-9. `math*` wire-node family (synthetic `math` intrinsics)

- **What:** six wire nodes — `mathf64bits`, `mathf64frombits`,
  `mathf32bits`, `mathf32frombits`, `mathsqrt`, `mathfma` — emitted from
  the intrinsic map in
  [synth_machinery.go](../../internal/frontend/synth_machinery.go) (lines
  1588–1593) for the synthetic `math` package. Backend: pure-arithmetic
  nodes registered in
  [P3KindRegistrations.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3KindRegistrations.java)
  (lines 157–167).
- **Why:** the real `math` sources did not compile; these six functions
  lower directly to backend arithmetic instead of Go bodies.
- **Root-cause fix:** retire with the synthetic `math` package (Phase 2).
  Alternative, not yet adjudicated: gc itself intrinsifies `Sqrt`/`FMA`/
  bit-casts, so the wire node may BE the real implementation shape — that
  adjudication needs a fresh independent review before these move to
  `ADJUDICATED_WIRE_NODES`; until then they stay inventoried here.
- **Phase:** 2.

### B-10. `runtime*` wire-node family (synthetic `runtime` overlay API)

- **What:** sixteen wire nodes emitted in the synthetic `runtime`
  package's guard block in
  [export.go](../../internal/frontend/export.go) (emissions at lines
  8586–8789) and
  `desugarRuntimeStack` in
  [mrdesugar_m5.go](../../internal/frontend/mrdesugar_m5.go) (line 3252;
  `runtimecaller` line 3261, `runtimefuncfileline` line 3283,
  `runtimeframesnext` line 3290):
  - *Scheduler/host primitives:* `runtimenumcpu`, `runtimenumgoroutine`,
    `runtimegosched`, `runtimegomaxprocs`.
  - *Stack inspection:* `runtimecallers`, `runtimecaller`,
    `runtimecallersframes`, `runtimeframesnext`, `runtimefuncforpc`,
    `runtimefuncname`, `runtimefuncentry`, `runtimefuncfileline`
    (backend: B-7 [rtstack/](../../internal/backend/interop/src/main/java/graalgo/rtstack/)).
  - *Environment introspection:* `runtimereadmemstats`, `runtimegoroot`,
    `runtimeversion`, `runtimestack`.
  Backend nodes registered in
  [P3KindRegistrations.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3KindRegistrations.java)
  (lines 174–212) via the public `registerExprKind`/`registerStmtKind`
  hooks — no Phase-1 factory change.
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
    set/return-previous semantics. `runtimenumcpu`/`runtimegomaxprocs`
    predate the gate and were never gate-flagged; the PKG-023/PKG-040
    adjudications cite "the `runtimenumcpu` precedent" — this entry is
    that precedent, recorded with its evidence. Formal gate-allowlist
    adjudication for `runtimenumcpu`/`runtimegomaxprocs` still needs a
    fresh independent review before they move to `ADJUDICATED_WIRE_NODES`
    (on HEAD the gate allowlists only `runtimenumgoroutine` and
    `runtimegosched`).
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

- **What:** nine wire nodes — `unsafeslice`, `unsafeslicedata`,
  `unsafestring`, `unsafestringdata`, `unsafeadd`, `unsafefromptr`,
  `unsafefromuint`, `unsafetooptr`, `unsafederive` — emitted from
  [synth_unsafe.go](../../internal/frontend/synth_unsafe.go) and
  [unsafe_derive.go](../../internal/frontend/unsafe_derive.go) (line 290,
  the `unsafederive` node, fix-redmaster-r39) for the synthetic `unsafe`
  package (emission lines: `unsafeslice` 142, `unsafeslicedata` 150,
  `unsafestring` 162, `unsafestringdata` 170, `unsafeadd` 187,
  `unsafefromptr` 219/224, `unsafefromuint` 238, `unsafetooptr` 256).
  Backend:
  [p3unsafe/](../../internal/backend/interop/src/main/java/graalgo/p3unsafe/)
  (`UnsafeSliceNode`, `UnsafeStringNode`, `UnsafeAddNode`, …),
  registered in
  [P3KindRegistrations.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3KindRegistrations.java)
  (lines 215–231). Note the count is nine, not eight: the 2026-10-19 lead
  missed `unsafederive`, which postdates it as a separately registered
  kind.
- **Why:** the value model has no byte-addressed pointer arithmetic, so
  `unsafe` conversions are implemented as backend nodes over the Go
  value representations.
- **Root-cause fix:** the byte-addressed value model (or documented trap
  contract) — Phase 4. Note the split with retired B-6: the `unsafeadd`
  node itself remains, but it is now a real implementation (via
  `UnsafeTable.derive`) rather than the unconditional trap B-6 recorded;
  only the trap retired, the node did not.
- **Phase:** 4.

### B-12. Miscellaneous stdlib wire nodes

Total stdlib-specific wire kinds on HEAD, all families: 8 (B-8) + 6 (B-9)
+ 16 (B-10) + 9 (B-11) + 6 (this entry) = 45 emitted kinds, plus 1
registered-but-never-emitted kind (`syncnew`, below). General
builtin/language lowerings (`minmax`, `clear`, channel ops, `gostmt`,
`ifacemethod`) are NOT stdlib-specific and are deliberately not counted.

- **`abifuncpc`** — the `internal/abi` `FuncPCABI0`/`FuncPCABIInternal`
  compile intrinsics (real-reflect Phase 1). Emission:
  `exportAbiFuncPCIntrinsic` in
  [synth_machinery.go](../../internal/frontend/synth_machinery.go) (line
  1681). Backend:
  [AbiFuncPCNode.java](../../internal/backend/interop/src/main/java/graalgo/p3abi/AbiFuncPCNode.java)
  returns an opaque stable func id from the `FuncIds` registry (same func
  → same id, non-zero for non-nil funcs) — gc implements these as compile
  intrinsics returning the entry PC, and graalgo has no PCs, so the opaque
  id IS the gc-parity mechanism. Verdict: permanent intrinsic mechanism,
  not a logic shim; retirement only with a real PC model (not planned).
  Registered in both launchers on HEAD
  ([P3KindRegistrations.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3KindRegistrations.java)
  line 247,
  [P3SyncMain.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3SyncMain.java)
  line 449) — the registration asymmetry the 2026-10-19 lead noted is
  resolved.
- **`atomicaddr`** — wraps the `*T` operand of `sync/atomic` word ops in
  a delegating `AtomicLong`/`AtomicInteger` cell so the existing
  `AtomicOpNode` serves the op unchanged (emission: `exportAtomicWordOp`
  in [export.go](../../internal/frontend/export.go) line 9301; backend
  [AtomicAddrNode.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/nodes/AtomicAddrNode.java)).
  Lowering glue for the B-2 cell representation; retires with B-2 (Phase 3).
- **`atomicptrop`** — `atomic.Pointer[T]` / `CompareAndSwapPointer`
  word ops (fix-redmaster-r39; emission in
  [export.go](../../internal/frontend/export.go) line 9339; backend
  [AtomicPtrOpNode.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/nodes/AtomicPtrOpNode.java),
  registered in both launchers). Missed by the 2026-10-19 lead; retires
  with B-2 (Phase 3).
- **`reflectop`** — single-kind dispatch on `"op"` for reflect method
  calls (emission: `exportFuncCall` for `MakeFunc` values in
  [export.go](../../internal/frontend/export.go) line 9439,
  `exportMethodCall` for `reflect.Value` methods line 10068,
  `exportIfaceMethodCall` for `reflect.Type` methods on interface values
  line 11042; backend
  [ReflectOpNode.java](../../internal/backend/interop/src/main/java/graalgo/p3reflect/ReflectOpNode.java),
  registered in both launchers). Correction to the 2026-10-19 lead:
  `reflectop` did NOT retire with the synthetic reflect package — on HEAD
  it backs real (disk-imported) reflect method dispatch, because guest
  dispatch cannot serve these calls (interface-method tables deliberately
  carry no methods). Retirement needs a general mechanism for reflect
  method dispatch ("needs design"), not the synthetic-package deletion.
- **`sliceoverlap`** — `crypto/internal/fips140/alias.AnyOverlap`/
  `InexactOverlap` (emission: `exportAliasIntrinsic` in
  [synth_machinery.go](../../internal/frontend/synth_machinery.go) line
  1631). The real package uses `uintptr(unsafe.Pointer(...))`, which
  traps under D5; the node implements overlap detection directly on
  `GoSlice` values (array identity + range intersection), registered in
  the Phase-1
  [NodeFactory.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/NodeFactory.java)
  (line 372). Retires with the byte-addressed value model (Phase 4).
- **`syncop`** — `sync/atomic` word ops (via `exportAtomicWordOp` in
  [export.go](../../internal/frontend/export.go) line 9302) and the
  synthetic `sync` package's multi-result `Map` methods (via
  `desugarSyncMap` in
  [mrdesugar_m5.go](../../internal/frontend/mrdesugar_m5.go) line 3329).
  Backend:
  [SyncOpNode.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/nodes/SyncOpNode.java) /
  [AtomicOpNode.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/nodes/AtomicOpNode.java).
  Live for atomic word ops; the `sync.Map`-method path has no live
  emitter on HEAD (the plan forms only if the synthetic sync package's
  scope contains `mapAnyBoolPair`,
  [mrdesugar_m5.go](../../internal/frontend/mrdesugar_m5.go) line 2360,
  and `sync` now resolves from disk). Retires with B-2/B-3 (Phase 3).
- **`syncnew`** (registered, never emitted) — both launchers register it
  ([P3KindRegistrations.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3KindRegistrations.java)
  line 111,
  [P3SyncMain.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3SyncMain.java)
  line 293; backend
  [SyncNewNode.java](../../internal/backend/interop/src/main/java/graalgo/p3sync/nodes/SyncNewNode.java)),
  but no frontend emission site references it. Legacy of the old
  construction path — delete with the Phase-3 machinery.
- **Phase:** needs design (`reflectop`, see above) → 3
  (`syncop`/`atomicaddr`/`atomicptrop`/`syncnew`) → 4 (`sliceoverlap`);
  `abifuncpc` is a permanent mechanism.

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

- **2026-10-28 — B-6 `unsafe.Add` always traps.** The unconditional trap
  is gone: [UnsafeAddNode.java](../../internal/backend/interop/src/main/java/graalgo/p3unsafe/UnsafeAddNode.java)
  implements the `"unsafeadd"` wire node via
  `UnsafeTable.derive` (fix-redmaster-r39, commit `383e7faf`), and zero
  `addTrap` matches remain on HEAD. Only the trap retired: the
  `unsafeadd` node itself remains as a real implementation (see B-11).
  The Phase-4 byte-addressed value model is still the retirement path
  for the remaining `unsafe` derivation surface, not for `Add`.
- **2026-10-16 — synthetic `reflect` package (public API; B-4/B-3).**
  Real `reflect` now compiles from GOROOT via the general disk importer
  (real-reflect2 lane, commit `d2804a52`): `"reflect"` and
  `"internal/reflectlite"` are commented out in `syntheticSources`
  ([synth_machinery.go](../../internal/frontend/synth_machinery.go) lines
  104–114). Retained, NOT retired: `synth_reflect.go` itself, re-registered
  as `"reflect_m7"` ([synth_machinery.go](../../internal/frontend/synth_machinery.go)
  line 121) because the M7 desugar tables still need its `__`-helpers and
  pair types, and the `reflectop` wire node (B-12), which now backs real
  reflect method dispatch. Retiring the helper source is separate work
  (cf. review R-12).
- **2026-10-16 — synthetic `net/http` + `net/http/httptest` (B-4).**
  Real `net/http` now compiles from GOROOT via the general disk importer
  (real-nethttp lane, commit `cc0a78f4`): the 1,275-line
  `synth_net_http.go` and 269-line `synth_net_http_httptest.go` were
  deleted, and neither package has a `syntheticSources` entry on HEAD.
  `net` itself (B-4) is still synthetic.
- **2026-10-10 (predates this inventory) — B-5, certificate-parsing half.**
  Certificate parsing in
  [GoX509.java](../../internal/backend/interop/src/main/java/graalgo/p3x509/GoX509.java)
  has been delegated to the JDK since P3-M9 (commit `ed9bc19f`):
  `CertificateFactory.getInstance("X.509")` (lines 38–42) backs `parse`,
  exposed to the synthetic `crypto/x509` package via the `graalgo.x509.*`
  linkname intrinsics. The original B-5 entry described this half as
  hand-rolled DER parsing; it was stale from the inventory's creation.
  The EC-arithmetic + DER *key*-parsing half is still active (see B-5).
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
