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

Counts in this file are verified mechanically, not hand-copied:
[.github/scripts/check_bridge_inventory.py](../../.github/scripts/check_bridge_inventory.py)
recomputes the B-4 file counts and the B-3 `syntheticSources` entry count
from the tree and fails when the inventory disagrees with disk (it runs in
`make check` and in CI). Counts below were last regenerated 2026-11-01 at
HEAD `21fa01d9` (review F-05); if the script and this file ever disagree,
the script's tree-derived numbers win and this file must be regenerated.

In-flight claims in this file name a worktree only when its registration
and branch were verified live at regeneration time. Prior worktree names
that failed that check are NOT carried forward: at the 2026-11-01
regeneration, `real-fmt2`/`real-fmt-rebase` were landed-or-superseded
(`real-fmt-rebase` is an ancestor of HEAD; `real-fmt2`'s tip is
2026-10-16 and its goal retired on master), and `real-x509`
(tip 2026-10-15), `real-nethttp` (tip 2026-10-15), `real-reflect2`
(tip 2026-10-17) are stale registrations whose branches are not on
master — none of them is verifiable in-flight work, so none is listed
as in flight below. Re-add a worktree name only after re-verifying it
against `wt.py list` and branch state.

## Phased removal order

- **Phase 1 — next retirements.** Retire the highest-value synthetic
  packages via root-cause fixes: `crypto/x509`, `net`/`net/http`,
  `reflect`. Retires B-5 and the reflect/time/math name-keyed
  predicates in B-3. (`fmt` is already retired — see B-1 in Retired.
  No in-flight worktree was verified for these packages at the
  2026-11-01 regeneration; see the note above.)
- **Phase 2 — package-by-package.** Retire each remaining `synth_*.go` as
  its root cause lands (real trap path for `internal/poll`+`syscall`; the
  `io`/`fs` export fix for `net`; a DNS resolver; …). Every retirement
  shrinks the B-3 predicate chains, the `mrdesugar` pair tables, and
  `syntheticSources`.
- **Phase 3 — machinery deletion.** When the last synthetic package retires,
  delete the machinery wholesale: `syntheticSources` + the import overlay,
  `overlayImporter`/`resolvePkgTree` package routing, the linkname
  machinery, `isMultiResultSynthetic`/`multiResultSyntheticError`, the
  intrinsic predicates, the `"syncop"`/`"runtimecaller"` wire nodes, and
  `NodeFactory.syncPrimitiveName`. Retires B-2 and the rest of B-3.
- **Phase 4 — needs design.** `frontend-rewrite` (frontend-v2
  parser→checker→emitter pipeline) replaces the name-keyed export machinery
  entirely; real `runtime.Callers` replaces the `rtstack` intrinsics.
  Retires B-7. (`unsafe.Add` is already retired — see B-6 in Retired.)

## Active bridges

### B-2. `NodeFactory.syncPrimitiveName` (name-keyed sync lowering)

- **What:**
  [NodeFactory.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/NodeFactory.java)
  `syncPrimitiveName` (line 1963): matches `sync.Mutex`/`sync.Map`/
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
  - `synth_machinery.go`: `syntheticSources` import overlay (line 70, 130
    entries), `isMultiResultSynthetic` (line 2275) /
    `multiResultSyntheticError` (line 2369),
    `isTimeIntrinsicFunc` (line 2193), `isMathIntrinsicFunc` (line 2198),
    `isAliasIntrinsicFunc` (line 2212), `isAbiFuncPCIntrinsicFunc`
    (line 1654);
  - [synth_reflect.go](../../internal/frontend/synth_reflect.go):
    `isReflectIntrinsicFunc` (line 2908);
  - [export.go](../../internal/frontend/export.go):
    `overlayImporter` (line 584, map-based package routing) and the
    linkname machinery (`linknameAlias`, `linknameDirective`);
    [diskimport.go](../../internal/frontend/diskimport.go):
    `resolvePkgTree` (line 403), which routes a path to its synthetic
    package, a dedicated stub, or the disk importer;
  - [export.go](../../internal/frontend/export.go): `"syncop"` and
    `"runtimecaller"` custom wire nodes for stdlib functions, and the
    `time.Now` eta-expansion hardcoded by package+symbol name
    ([mr_disk.go](../../internal/frontend/mr_disk.go) lines 229–231).
- **Why:** each synthetic package needed bespoke import routing,
  multi-result desugaring, and intrinsic hooks; the machinery grew
  package-by-package instead of through a general mechanism.
- **Root-cause fix:** retire with the synthetic packages (Phase 2 shrinks
  the tables; Phase 3 deletes the machinery). The general mechanism is the
  disk importer + on-demand method indexing; the `time.Now` case needs a
  general eta-expansion for any selector-expr function value ("needs
  design" for the mechanism — the inventory entry is the deliverable now).
  `frontend-rewrite` (frontend-v2) replaces this machinery entirely
  (Phase 4). No in-flight worktree was verified for this entry at the
  2026-11-01 regeneration (prior `real-reflect2` name: stale
  registration, tip 2026-10-17, not on master).
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

### B-4. 140 synth_*.go files (132 non-test + 8 *_test.go) + 12 p3frontend mirrors (synthetic stdlib)

- **What:** 140 files in [internal/frontend/](../../internal/frontend/)
  (`synth_*.go`: 132 non-test + 8 `*_test.go`; the test/non-test split
  uses the anchored `_test.go$` suffix — a bare `grep -v _test` drops
  the 3 real `synth_testing*.go` sources, since `_test` is a substring
  of `testing`, and undercounts to 129) plus 12 `synth_*.go` mirrors under
  [internal/frontend/p3frontend/](../../internal/frontend/p3frontend/) —
  synthetic reimplementations of stdlib packages standing in for the real
  sources. The counts in this heading and the mirror split below are
  checked mechanically by
  [.github/scripts/check_bridge_inventory.py](../../.github/scripts/check_bridge_inventory.py):
  plus 12 `synth_*.go` mirrors under p3frontend/ — all 12 non-test (0 `*_test.go`).
  18 of the top-level files are generator outputs listed in the
  `GENERATED_SYNTH` block of the [Makefile](../../Makefile) (regenerated
  by `make gen-synth` from gc 1.27.1 sources); the remainder are
  hand-written or vendored stdlib sources. The previously stated
  "~24 authored, ~110 vendored" split is withdrawn: it summed to 134,
  not the then-claimed 142, and could not be re-derived from the tree,
  so no authored/vendored split is asserted here until one is derived
  mechanically.
- **Why:** the real stdlib sources did not compile under graalgo
  (`go:linkname`, intrinsic surface, `unsafe` patterns); synthetics bought
  coverage fast (his 2026-10-10 dual-track decision).
- **Root-cause fix:** per-package: fix the compiler/runtime cause that
  forces the synthetic (the 2026-10-15 directive: only packages that
  ABSOLUTELY MUST be synthetic survive, with a documented, specific,
  verifiable reason), then delete the `synth_*.go`. The `p3frontend`
  mirrors retire with the top-level tree or via an explicit retire-or-keep
  decision. No in-flight worktree was verified for these packages at the
  2026-11-01 regeneration: `fmt` is retired (B-1), and the prior
  `real-x509` / `real-nethttp` / `real-reflect2` names are stale
  registrations (tips 2026-10-15/17, not on master).
- **Phase:** 1 (`crypto/x509`, `net`/`net/http`, `reflect`) → 2 (remainder).

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
  gc `x509.ParseECPrivateKey` over P-224/256/384/521. No in-flight
  worktree was verified at the 2026-11-01 regeneration (prior `real-x509`
  name: stale registration, tip 2026-10-15, not on master); whoever
  retires synthetic `crypto/x509`, the Go-side counterpart, coordinates
  with this entry.
- **Phase:** 1.

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

- **2026-10-28 — B-6 `unsafe.Add` always traps (bridge by omission).**
  Retired by fix-redmaster-r39 (commit `383e7faf`, an ancestor of HEAD
  `21fa01d9`): [UnsafeAddNode.java](../../internal/backend/interop/src/main/java/graalgo/p3unsafe/UnsafeAddNode.java)
  now derives via
  [UnsafeTable.derive](../../internal/backend/interop/src/main/java/graalgo/p3unsafe/UnsafeTable.java)
  of the live base handle by the byte count, instead of the former
  unconditional D5 trap, and `addTrap` no longer exists anywhere in the
  tree. Scope note: the *unproven* `uintptr`→`unsafe.Pointer`
  conversion still traps (D5, in
  [UnsafeOps.java](../../internal/backend/interop/src/main/java/graalgo/p3unsafe/UnsafeOps.java))
  — that is a separate documented trap, not `unsafe.Add`, and it is not
  grandfathered by this retired entry.
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
