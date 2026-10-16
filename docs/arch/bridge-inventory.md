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

- **Phase 1 — in flight (2026-10-16).** Retire the highest-value synthetic
  packages via root-cause fixes: `crypto/x509` (worktree `real-x509`),
  `net`/`net/http` (worktree `real-nethttp`). Retires B-5 and the
  time/math name-keyed predicates in B-3. Landed 2026-10-16: `fmt`
  (B-1 retired — see [Retired](#retired)) and `reflect`
  (`real-reflect2`: `d2804a52` + `e0fe2650` on master; the synthetic
  `reflect`/`internal/reflectlite` registrations in
  [synth_machinery.go](../../internal/frontend/synth_machinery.go) are
  commented out as RETIRED — the `reflectop` intrinsic predicates remain
  active and now serve the disk-imported real `reflect` package).
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

### B-1. `FmtCallNode` + `graalgo.runtime.Fmt`/`FmtScan` (Java reimplementations of `fmt`)

**RETIRED 2026-10-16** — see [Retired](#retired). Entry moved there with the
retiring commits named.

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
    ([mr_disk.go](../../internal/frontend/mr_disk.go) ~lines 211–228).
- **Why:** each synthetic package needed bespoke import routing,
  multi-result desugaring, and intrinsic hooks; the machinery grew
  package-by-package instead of through a general mechanism.
- **Root-cause fix:** retire with the synthetic packages (Phase 2 shrinks
  the tables; Phase 3 deletes the machinery). The general mechanism is the
  disk importer + on-demand method indexing; the `time.Now` case needs a
  general eta-expansion for any selector-expr function value ("needs
  design" for the mechanism — the inventory entry is the deliverable now).
  `real-reflect2` landed 2026-10-16 (`d2804a52`, `e0fe2650`): the synthetic
  reflect/reflectlite *sources* are retired, but the `reflectop` intrinsic
  predicates (`isReflectIntrinsicFunc`, `reflectIntrinsicNames`) remain
  active — they now serve the disk-imported real `reflect` package — and
  retire with the machinery in Phase 3. `frontend-rewrite` (frontend-v2)
  replaces this machinery entirely (Phase 4).
- **Phase:** 1 (time/math predicates) → 2 (shrink) → 3 (delete).
- **Note (2026-10-16, PKG-023 independent review + PKG-038):** completing
  the *declared view* of the already-inventoried dedicated stubs
  (`syntheticRuntimePackage` in [export.go](../../internal/frontend/export.go),
  `syntheticUnsafePackage` in
  [synth_unsafe.go](../../internal/frontend/synth_unsafe.go)) is NOT a new
  bridge. (`syntheticFmtPackage` was removed with the B-1 retirement —
  see [Retired](#retired).)
  The mechanical gate flags `addFunc(` lines in `export.go`, but the
  overlay's existence is grandfathered by this entry — extending its
  declared API surface toward gc's documented API adds no new bridge
  machinery (no new synth files, no new wire nodes, no new
  package-name-keyed branches). The PKG-023 reviewer judged the flag a
  mechanical false positive, not a veto; the same judgment covers the
  PKG-038 runtime-view completion.

### B-4. 140 generated `synth_*.go` + 12 `p3frontend` mirrors (synthetic stdlib)

- **What:** 140 generated files in [internal/frontend/](../../internal/frontend/)
  (`synth_*.go`: ~24 authored, ~110 verbatim vendored from go1.27.1; count
  verified 2026-10-16 via `ls internal/frontend/synth_*.go | wc -l`) plus 12
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
  decision. In flight: `real-x509` (`crypto/x509`, "remove synthetic
  crypto/x509, route via general disk mechanism" — verified still in flight
  2026-10-16: `syntheticCryptoX509Source` still registered),
  `real-nethttp` (`net`/`net/http` — verified still in flight: `net` still
  registered as `syntheticNetSource`).
  Landed 2026-10-16: `real-fmt2` (`fmt` — B-1 retired, see
  [Retired](#retired)) and `real-reflect2` (`reflect` — synthetic sources
  retired, `d2804a52` + `e0fe2650` on master).
  Known gap: the top-level tree retired `fmt`, but the `p3frontend` tree
  still serves the synthetic fmt stub —
  [importer.go](../../internal/frontend/p3frontend/importer.go)
  `syntheticFmtPackage()` mirrors the frozen D4 fmt stub. The
  retire-or-keep decision for this mirror is pending (tracked here, not a
  new entry).
- **Phase:** 1 (the two remaining in-flight packages) → 2 (remainder).

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
  counterpart; verified still in flight 2026-10-16 — the synthetic
  `crypto/x509` + `crypto/x509/pkix` registrations are still active).
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

- **2026-10-16 — B-1: `FmtCallNode` + `graalgo.runtime.Fmt`/`FmtScan`
  (Java reimplementations of `fmt`).** Retired by the real-fmt2 lane:
  `ffc1a8bf` ("switch from synthetic fmt to real fmt from GOROOT") and
  `a527063d` ("remove Java fmt implementation": deleted `Fmt.java`,
  `FmtScan.java`, `FmtCallNode.java`, `FmtHandleMethods.java`,
  `FmtMethodDispatch.java`, the `"fmtcall"` wire node registration, and the
  `exportFmtCall` frontend dispatch), plus `c2c0718c` (follow-up:
  `isGenericFunc` helper + golden regeneration). Entry moved here
  2026-10-16 by the inventory-hygiene lane (MECH-2) — one day late; the
  retiring change did not delete it per the inventory's own rule.
  Verified 2026-10-16: no `FmtCallNode`/`exportFmtCall`/`fmtcall` in the
  top-level frontend, no `Fmt*.java` in the backend. Residue cleaned in the
  inventory pass: dangling `{@link Fmt}` javadoc in
  [GoFloatFmt.java](../../internal/backend/runtime/src/main/java/graalgo/runtime/GoFloatFmt.java)
  and [Intrinsics.java](../../internal/backend/runtime/src/main/java/graalgo/runtime/Intrinsics.java),
  stale `FmtCallNode`/`fmtcall` comments in
  [mrdesugar_m5.go](../../internal/frontend/mrdesugar_m5.go),
  [synth_math_big.go](../../internal/frontend/synth_math_big.go),
  [MainTest.java](../../internal/backend/truffle/src/test/java/graalgo/launcher/MainTest.java),
  and stale `Fmt.handleMethods` references in
  [IfaceMethodTable.java](../../internal/backend/interop/src/main/java/graalgo/iface/IfaceMethodTable.java),
  [synth_fmtstate.go](../../internal/frontend/synth_fmtstate.go),
  [export.go](../../internal/frontend/export.go).
  Known remaining fmt residue (NOT retired — separate work, tracked here):
  the synthetic `internal/graalgo/fmtstate` package is still registered
  and imported unconditionally during export, but its Java implementation
  (`FmtHandleMethods`) was deleted — its continued necessity is an open
  question; the `p3frontend` tree still serves the synthetic fmt stub
  (`syntheticFmtPackage()` in
  [importer.go](../../internal/frontend/p3frontend/importer.go)) —
  retire-or-keep decision pending under B-4; and
  [FreezeSignatureTest.java](../../internal/backend/truffle/src/test/java/graalgo/runtime/FreezeSignatureTest.java)
  still references the deleted `graalgo.runtime.Fmt` class (7 `Fmt.class`
  uses) — a latent compile break on the `make test` path, reported
  2026-10-16 as a P0 for a remediation lane.

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
  (owned by the test-infra lane — delete symlink + comment). Verified
  still present 2026-10-16 — not fixed by this lane (ownership: the
  test-infra lane).
- **2026-10-15 — banned `fmt.Fprint`→`w.Write` desugar.** Removed before
  this inventory was created (the real-`fmt` lane's template: kill the
  desugaring, make the real package work). Recorded here so the pattern is
  not reintroduced.
