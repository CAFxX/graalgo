# [SYNC_DESIGN.md](SYNC_DESIGN.md) — P3-M2b `sync` Go bindings

**Scope:** PLAN.md §4 P3-M2 (WS D), GG-0017 sync slice. Implements
ADR-0002 (freeze exception for the synthetic `sync` package).
**Status:** implemented 2026-10-06; differential corpus 22/22 vs gc 1.27.1.

## 1. Value model

A `sync.Mutex` Go value **is** the `GoMutex` Java object (opaque, like
`GoChan`); likewise `GoRWMutex`, `GoWaitGroup`, `GoOnce`. Copying a sync
value copies the reference — `Boxing.copyForAssign` deep-copies only
`GoStruct`, so reference semantics are automatic. Go's `noCopy` is
advisory (vet-level); the runtime does not enforce it.

Consequence: the factory's struct zero (`GoStruct`) is never a valid
sync value. Every construction site is exporter-normalized to the
`syncnew` wire node (see §3).

## 2. Wire (additive, v1)

Two new node kinds, produced only by the exporter, consumed only by the
phase-3 module via the public `registerExprKind` hook:

- `"syncnew"`: construction. Fields: `synctyp` (`"Mutex"` | `"RWMutex"`
  | `"WaitGroup"` | `"Once"`), `type` (type id — the named type, or a
  pointer to it for `&`/`new` forms).
- `"syncop"`: method call. Fields: `op` (`"Lock"`, …), `synctyp`,
  `recv`, `args`, `type` (null — all in-scope ops return void).

Old consumers reject both as unknown per freeze §2.3. `toolVersion`
0.2.0 → 0.3.0 per freeze §5.

## 3. Exporter (`internal/frontend/export.go`, documented exception)

The synthetic `sync` package lives in `overlayImporter` with the four
named types and exact gc 1.27.1 method signatures (pointer receivers).
`import "sync"` passes the `ExportFile` gate.

Construction → `syncnew`:

| Go form | Wire |
|---|---|
| `var m sync.Mutex` (no init, local or package-level) | implicit `syncnew` per name (exporter-normalized; the frozen factory zero is unusable) |
| `sync.Mutex{}` | `syncnew` (empty literal; elements are a scope error — fields are unexported) |
| `&sync.Mutex{}` | `syncnew`, pointer type id |
| `new(sync.Mutex)` | `syncnew`, pointer type id |

Struct fields, array/slice elements, and map keys/values of sync type
are named "not in M2 scope" scope errors — the factory manufactures
those zeros and they cannot be intercepted. `*sync.Mutex` in composites
is fine (nil is the correct zero).

Method calls → `syncop`, dispatched on go/types `MethodVal` selections
whose receiver unwraps (through pointers) to a sync named type:

- In-scope ops: `Lock`/`Unlock`, `RLock`/`RUnlock`/`Lock`/`Unlock`,
  `Add(int)`/`Done`/`Wait`, `Do(func())`. All others (`TryLock`,
  `TryRLock`, `RLocker`, `WaitGroup.Go`) are defined with exact gc
  signatures, then fail fast with "not in M2 scope" — never silently.
- Method values (`f := m.Lock`) and method expressions: the existing WS H
  scope error fires — verified by test.
- `defer m.Op(args)`: the frozen `buildDefer` only builds
  funclit/ident/builtin deferred calls, so the exporter desugars to
  `defer func(…){ …Op(…) }(…)` — all standard v1 wire. Receiver
  semantics are gc-exact: a value-typed ident receiver is **captured**
  (`defer m.Unlock()` uses `&m`; verified: reassigning `m` before
  return makes the deferred call hit the new object, matching gc's
  `fatal error: sync: unlock of unlocked mutex`). Pointer-typed and
  non-ident receivers, and all args, are params evaluated at defer time.
- `go m.Op(args)`: desugars to `go func(){ …Op(…) }()` (zero-arg spawn,
  GoStatementNode's P2-M1 contract); the receiver is captured, which is
  identical to Go's address semantics even under races.

## 4. p3frontend (`phases/phase3/src/p3frontend/`)

- [importer.go](importer.go): mirrors the synthetic `sync` signatures for p3frontend's
  own type-check.
- [flatten.go](flatten.go): keeps `import "sync"` specs, like `runtime`/`fmt`.
- [loader.go](loader.go): resolves `"sync"` as synthetic — never the GOROOT stdlib.

The `defer`/`go` desugars need no p3frontend work: they are standard
`funclit`/`call`/`defer`/`gostmt` wire.

## 5. Java (`graalgo.p3sync`, `phases/phase3/src/main/java`)

- `nodes/SyncNewNode.java`, `nodes/SyncOpNode.java` (`verifies: GG-0017`):
  build the `GoMutex`/`GoRWMutex`/`GoWaitGroup`/`GoOnce` objects and
  dispatch the ops. Misuse panics (`unlock`/`rUnlock` of unlocked,
  negative `WaitGroup` counter) throw `IllegalStateException` with gc's
  message text, translated to a Go panic via `Panic.throwPanic`.
- `launcher/P3SyncMain.java`: registers `syncnew`/`syncop` (plus `gostmt`,
  reusing phase-2 `GoStatementNode`) via the public hooks; classpath
  phase3 > phase2 > phase1.
- `Once.Do`: the winner runs the func via `GoCalls.invoke` inside
  `GoOnce.doOnce`; a nil func panics via `GoCalls.nilFuncCall()`.
  A panicking `Do` func counts as done (gc parity); the panic propagates.

## 6. Semantics & divergences (all differential vs gc 1.27.1)

Corpus `phases/phase3/corpus/sync/` (sy00–sy12), `run_differential.sh`:
byte-identical stdout, equal exit codes, equal first stderr line for
panic programs. 22/22 green.

| Behavior | gc 1.27.1 | graalgo | Notes |
|---|---|---|---|
| unlock/RUnlock of unlocked | `fatal error: sync: …` (exit 2) | `panic: sync: …` (exit 2) | Scoped divergence (ADR-0002 D4): same message text, same exit code; recoverable here. Re-test when the scheduler gains a fatal-error path. |
| WaitGroup negative counter | `panic: sync: negative WaitGroup counter` (exit 2) | identical | Full parity. |
| `defer m.Unlock()` receiver | `&m` (address) | captured closure ≡ `&m` | Verified by sy12. |
| Once.Do exactly-once under contention | 1 run | 1 run | sy10, 32 racers. |
| `TryLock`/`TryRLock`/`RLocker`/`WaitGroup.Go` | — | "not in M2 scope" scope error | Fail fast, never silent. |

## 7. Known limitations & re-test triggers

1. **Fatal vs panic** (above): re-run the unlock-misuse corpus expecting
   `fatal error:` once a scheduler fatal-error path exists.
2. **No sync values in composites**: struct fields / array / slice / map
   element of sync type are scope errors (factory-owned zeros).
3. **No address-of / deref of sync vars** (`&m`, `*pm`): pre-existing v1
   WS D limitation, orthogonal to sync.
4. **Method values / method expressions**: WS H scope error (existing).
5. **Re-test trigger discharge**: the P2-M8 trigger ("bind the four
   primitives and re-run stress as Go programs differential vs gc") is
   discharged by sy01 (exclusion, incl. `--gmp=1` cap-1 leg), sy04
   (RWMutex), sy06 (WaitGroup), sy10 (Once) — all byte-identical vs gc.
