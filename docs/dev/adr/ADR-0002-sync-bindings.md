# ADR-0002 — Phase-3 `sync` Go bindings: freeze exception for a synthetic `sync` package

**Date:** 2026-10-06
**Status:** accepted 2026-10-06 — coordinator design direction (P3-M2b
brief) validated as workable by the implementer; implementation follows this
ADR. Spec-first ordering honored: ADR committed before the exporter change.
**Scope:** closes PLAN.md §4 P3-M2 (WS D) `sync` slice; discharges the P2-M8
re-test trigger ("when WS G overlay lands, bind the four primitives and
re-run stress as Go programs differential vs gc",
`phases/phase2/hidden_files/torture/REPORT.md` §5).
**Normative basis:** `phases/phase1/INTERFACE_FREEZE.md` (v1, in force) §§1,
2, 5; PLAN.md §§2–5 (WS D; GG-0017 sync slice, GG-0048); SPEC §§2.8, 3.2,
3.3, 3.6, 3.8, 5.

## Context

P3-M2 (WS D) lowers `import "sync"` onto the Phase-2 Java primitives
(`graalgo.runtime.sync.{GoMutex,GoRWMutex,GoWaitGroup,GoOnce}` — stressed at
Java level in P2-M8; Go bindings deferred). The frozen `ExportFile`
(`phases/phase1/frontend/export.go`) rejects `import "sync"` twice: the
`ExportFile` import gate (only `"runtime"`/`"fmt"` pass) and `failImporter`.
No public hook exists for new intrinsics on the *producer* side:
`NodeFactory.registerExprKind`/`registerStmtKind` let a runtime *consume*
new wire kinds, but only the exporter can *produce* them from Go source —
and producing `m.Lock()` requires go/types to resolve the method against a
`sync` package with exact gc signatures, which only `overlayImporter`
(frozen file) can supply.

## Decision

A single documented exception to the frozen-dependency rule, following the
P2-D8 precedent (`2e5f012`: additive `gostmt` wire node, toolVersion
0.1.0→0.2.0, no `NodeFactory` change) and the P2-M4 precedent (`851e080`:
synthetic `runtime` package gains SetFinalizer/KeepAlive/GC — the synthetic
overlay packages are INTERNAL per freeze §1, not enumerated in frozen
§§2–6):

1. **Synthetic `sync` package** in `overlayImporter` (export.go): named types
   `Mutex`, `RWMutex`, `WaitGroup`, `Once` (underlying `struct{}`, matching
   gc's unexported-field structs observably), with pointer-receiver methods
   at exact gc 1.27.1 signatures (verified against
   `~/tools/go-1.27.1/src/sync/*.go`):
   `Lock/Unlock`, `RLock/RUnlock`, `Add(int)/Done/Wait`, `Do(func())`.
   `TryLock`/`TryRLock`/`RLocker`/`WaitGroup.Go` are *defined* (exact
   signatures, incl. a synthesized `Locker` interface for `RLocker`) and
   then rejected by the exporter with a named `not in M2 scope` scope
   error — fail-fast and explicit, never a confusing "undefined" type
   error and never silently wrong.
2. **Import gate**: `import "sync"` allowed in `ExportFile` (same carve-out
   shape as `runtime`/`fmt`).
3. **Wire**: two additive node kinds (freeze §2.3):
   - `"syncnew"` — construction: `var m sync.Mutex` (exporter-normalized
     implicit init — see D3), `sync.Mutex{}`, `&sync.Mutex{}`,
     `new(sync.Mutex)`. Fields: `synctyp` (type name), `type` (type-table
     id: named type, or pointer type for the `&`/`new` forms).
   - `"syncop"` — method call: `m.Lock()`. Fields: `op`, `synctyp`,
     `recv`, `args`, `type` (result type id; null for void ops).
   
   Old consumers reject both as unknown kinds (`unsupported expression
   kind`, naming the `registerExprKind` hook) — the correct
   forward-compatibility behavior for kinds they predate, per §2.3. No
   existing field is renamed, removed, or retyped; wire stays `v1`.
4. **Java** (`phases/phase3/src/main/java`, new package
   `graalgo.p3sync.nodes`): `SyncNewNode`/`SyncOpNode` builders registered
   via the public `registerExprKind` hook; a phase-3 launcher
   (`graalgo.p3sync.launcher.P3SyncMain`) mirroring `SchedMain`'s flow with
   classpath phase3 > phase2 > phase1. **No Phase-1 file changes** beyond
   this ADR's exception. Phase-2 sources are **frozen post-sign-off**;
   the documented exceptions to date:
   - **M2a** (`052c752`, DIV-M3-1 fix): `GoChan` un-final'd, `GoTimer`
     retargeted, new `TimerChan` + phase-2 test — behavior-preserving,
     red-green evidenced in
     `phases/phase3/hidden_files/m2/redgreen.md`, phase-2 suite 194/194.
   - **P3-M2remed P1-2**: one-word gc-parity fix to `GoRWMutex`'s unlock
     message (`sync: Unlock of unlocked RWMutex`, capital U).
   Future phase-2 source edits each get their own ADR.
5. **Version**: `gofrontend` toolVersion **0.2.0 → 0.3.0** per freeze §5
   ("any CLI/JSON behavior change"): `import "sync"` previously a scope
   error now compiles, and the JSON gains the two node kinds. Wire stays
   `v1` (additive kinds, §2.3). `phases/phase1/frontend/main.go`'s
   one-line constant bump is part of this exception (same as `2e5f012`).
6. **p3frontend** (not frozen): `importer.go` gains the synthetic `sync`
   package (mirrored signatures for its own type-check); `loader.go`
   resolves `"sync"` as synthetic (never the stdlib branch);
   `flatten.go` keeps `import "sync"` specs through the existing
   kept-import path (verified by test, no code change needed).

### Freeze-doc note

`INTERFACE_FREEZE.md` §2.1's `toolVersion` row still reads `"0.1.0"`
(stale since `2e5f012` bumped to 0.2.0 without amending it): the freeze
document is itself frozen, and §1(c) designates the ADR as the change
record ("No mechanism records (c) post-hoc — the ADR is the record"). This
ADR is that record for the 0.2.0→0.3.0 bump; the freeze text is not
edited.

## D1. Value model: the Go value IS the Java object

A `sync.Mutex` Go value *is* the `GoMutex` Java object (opaque, like
`GoChan`): `syncnew` allocates it; `syncop` casts the receiver. Copying
copies the reference — `Boxing.copyForAssign` deep-copies only `GoStruct`
values, and a `GoMutex` is not a `GoStruct`, so assignment, argument
passing, and interface boxing all carry the reference with no exporter or
factory change. Go's `noCopy` is advisory (vet-level); the reference copy
is the honest model.

**Rejected alternative** — a `GoStruct` wrapper holding the Java object:
adds an indirection to every op, and the wrapper would deep-copy on
assignment (wrong: two mutexes sharing no state after copy diverges from
the reference model and from gc's observable behavior under contention).

## D2. Method calls bypass `exportMethodCall`

`exportMethodCall` (frozen logic) scope-errors implicit `&`/`∗`
adjustments (WS D). Every real `sync` method has a pointer receiver, so
`m.Lock()` on an addressable value `m` would die there — yet at the value
level the adjustment is a no-op (the object IS the value). `exportCall`
therefore intercepts selections whose (pointer-deref'd) receiver is a
synthetic-sync named type *before* the generic method-call path (same
interception shape as the `runtime`/`fmt` branches) and emits `syncop`
directly. Non-sync method calls are untouched.

## D3. Zero values are exporter-normalized; composites scope-error

The factory's zero for a named-struct type is `GoStruct.zero` (frozen) —
an empty struct, not a usable mutex. Since the factory cannot be changed,
the exporter normalizes every direct construction to `syncnew`, which is
exactly gc's "zero value is ready to use" semantics:

- `var m sync.Mutex` (no init) → implicit `syncnew` init per declared
  name (locals and package-level vars alike);
- `sync.Mutex{}`, `&sync.Mutex{}`, `new(sync.Mutex)` → `syncnew`
  (pointer type id for the latter two; `*T` zero is nil — correct).

Positions where the *factory* would manufacture a zero (struct fields,
array/slice elements, map keys/elements — e.g. `var t T` with a
`sync.Mutex` field, or a missing-key map read) cannot be normalized by the
exporter and are a **named scope error** (`sync.Mutex as a struct field is
not supported in M2 scope`, etc.) — fail-fast, never a silently broken
empty-struct zero. `*sync.Mutex` in those positions is fine (nil is the
correct zero) and stays allowed.

**Re-test trigger:** full composite support (embedding the canonical
`struct { sync.Mutex }` pattern) when a later milestone addresses
factory-level zero manufacturing; the scope errors name M2 so the trigger
is greppable.

## D4. Misuse → Go panic with gc's message (known divergence, scoped)

`GoMutex.unlock()`, `GoRWMutex.rUnlock()`/`unlock()` and
`GoWaitGroup.add` raise Go panics with gc's message text directly via
`Panic.throwPanic` (RV18-P1-10: previously the primitives threw
`IllegalStateException` and the `SyncOpNode` binding translated it —
the host-signal layer is gone, so no caller can forget the translation
and leak a host exception):

- `sync: negative WaitGroup counter` — gc 1.27.1 panics (recoverable);
  full parity: `panic: sync: negative WaitGroup counter`, exit 2.
- `sync: unlock of unlocked mutex`, `sync: RUnlock of unlocked RWMutex`,
  `sync: Unlock of unlocked RWMutex` — gc 1.27.1 **fatal-errors**
  (`fatal error: ...`, unrecoverable, exit 2; verified against
  `~/tools/go-1.27.1`). The primitives panic with gc's message text
  (`panic: sync: ...`, exit 2) per the P3-M2 brief
  ("unlock-of-unlocked panics with gc's message"). **Scoped divergence:**
  ours is recoverable, gc's is not; the `panic:`/`fatal error:` wrapper
  line differs. A true fatal-error path needs scheduler machinery Phase 2
  does not have (and may not be modified).

**Re-test trigger:** when the scheduler gains a fatal-error path, re-run
the unlock-misuse corpus expecting gc's `fatal error:` line.

Inherited limitation (phase-2 primitive, out of M2 scope): `GoWaitGroup`
does not detect "Add called concurrently with Wait" (gc panics); the
binding cannot add what the primitive lacks.

## D5. `Once.Do` runs the closure synchronously on the winner

`SyncOpNode`'s `Do` evaluates the func value and invokes it via
`GoCalls.invoke` inside `GoOnce.doOnce`'s winner path — the same call
protocol `FuncValueCallNode` uses, including its `recoverAllowed` dance
(a direct `recover()` inside the Do func must return nil, gc parity).
`GoOnce` already implements exactly-once + "panic counts as done" (gc:
"If f panics, Do considers it to have returned"). A nil func value panics
with the nil-dereference `GoErrorValue` (nil-func-call parity).

## D5b. `defer`/`go` of sync calls desugar in the exporter

The frozen `buildDefer` only builds funclit/ident/builtin deferred calls
and the frozen `GoStatementNode` only builds zero-arg spawns, so the
exporter desugars (all standard v1 wire, no factory change):

- `defer m.Op(args)` → `defer func(…){ …Op(…) }(…)`; a value-typed ident
  receiver is **captured** (gc uses `&m` — verified: reassigning `m`
  before return makes the deferred call hit the new object, corpus
  `sy12`); pointer-typed/non-ident receivers and all args are params
  evaluated at defer time.
- `go m.Op(args)` → `go func(){ …Op(…) }()` (captured receiver — identical
  to Go's address semantics).

## D6. Out of scope (named scope errors, re-test triggers recorded)

| Out of scope | Fails as | Re-test trigger |
|---|---|---|
| `TryLock`/`TryRLock` | `sync.Mutex.TryLock is not supported in M2 scope` | needs `GoMutex.tryLock` (phase-2 primitive change) |
| `RWMutex.RLocker` | `not in M2 scope` | needs interface dispatch (P3-M3) for the `Locker` result |
| `WaitGroup.Go` (go1.25+) | `not in M2 scope` | trivial future addition (spawn+Add/Done) |
| sync type as struct field / array / slice / map key-elem | `not in M2 scope` | D3 trigger (factory zero manufacturing) |
| method values / method expressions (`f := m.Lock`) | existing WS H scope error (verified to fire for sync) | WS H follow-up |

## Alternatives considered (and rejected)

- **(a) p3frontend desugaring instead of an exporter change:** rejected —
  the merged file still passes through frozen `ExportFile`, which rejects
  `import "sync"`; any shim needs the synthetic signatures anyway, and
  synthesizing them anywhere but the exporter's `overlayImporter` breaks
  the single-source-of-truth for the frozen type-check.
- **(b) Compiling the real `sync` from GOROOT:** rejected — P3-M7+ wave
  work per PLAN.md; `sync` is runtime-coupled (`linkname`d to
  `internal/sync`, atomics) and far beyond "bind the four primitives".
- **(c) Waiting for the WS G full overlay:** rejected — P3-M2 explicitly
  scopes these bindings now, and the P2-M8 re-test trigger fires on them.
- **(d) Reusing `exportMethodCall`:** rejected — D2 (its WS D
  no-implicit-`&` rule contradicts pointer-receiver methods on values).

## Verification

- Red-green-first per semantic: exporter tests (wire shape + scope
  errors) fail before the exporter change; Go corpus programs
  (`phases/phase3/corpus/sync/`) run differential vs gc 1.27.1
  (byte-identical stdout, exit codes, `panic:` lines).
- P2-M8 re-test trigger discharge: every `SyncStressTest` leg ported to a
  Go program using `import "sync"` (mutex exclusion 32×1000, cap-1,
  unlock-misuse message, RWMutex readers/writers, WaitGroup 50 workers +
  reuse + negative-counter message, Once 32 racers) — differential vs gc.
- Suites stay green: phase-1 (208/208), phase-2 (194/194, post-DIV-M3-1-fix),
  p3frontend (`go test`).
- `verifies: GG-0017` annotations on the new Java nodes (GG-0017 already
  →active since P2-M1; this activates its sync slice).

## Consequences

- `gofrontend` 0.3.0 emits `syncnew`/`syncop`; the phase-3 launcher is the
  only consumer. Phase-1/2 launchers reject such documents as unknown
  kinds (fail-fast, §2.3).
- `import "sync"` in a program compiled by an *older* gofrontend is still
  a scope error — no silent behavior change for old binaries.
- The synthetic `sync` package is INTERNAL (freeze §1); its signatures
  are mirrored (not shared) in p3frontend's importer, with a signature-
  equality test pinning the mirror.
