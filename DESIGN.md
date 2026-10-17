# RV-P0-07 — `atomic.Value` type-consistency panics — DESIGN PROPOSAL

Lane: `fix-p0-atomic-value` · chat `graalgo-independent-codebase-review`
Phase: DESIGN ONLY (read-only analysis; no code written)
Status: awaiting coordinator approval

## 0. Behavioral spec (gc, `~/tools/go-1.27.1/src/sync/atomic/value.go`)

`atomic.Value` is a `(typ, data)` eface pair with a first-store protocol
(`firstStoreInProgress` sentinel + `procPin` spin). Panics, in order of
evaluation:

| # | Case | gc message |
|---|------|-----------|
| 1 | `Store(nil)` — checked **before** touching the cell, so panics even on first store | `sync/atomic: store of nil value into Value` |
| 2 | `Store` of a value whose type word `!=` stored type (only after first store completed; concurrent first-stores spin, then hit this check) | `sync/atomic: store of inconsistently typed value into Value` |
| 3 | `Swap(nil)` | `sync/atomic: swap of nil value into Value` |
| 4 | `Swap` inconsistent type | `sync/atomic: swap of inconsistently typed value into Value` |
| 5 | `CompareAndSwap(old, nil)` — checked **first**, before the old/new check | `sync/atomic: compare and swap of nil value into Value` |
| 6 | CAS with non-nil `old` where `old`'s type `!=` `new`'s type — checked **before** cell access (note plural "values") | `sync/atomic: compare and swap of inconsistently typed values` |
| 7 | CAS where stored type `!=` `new`'s type | `sync/atomic: compare and swap of inconsistently typed value into Value` |
| 8 | CAS on empty cell: `old != nil` → `false`; `old == nil` → first store, `true` | (no panic) |
| 9 | CAS equality: current vs `old` via Go `==` on interface values; mismatch → `false`; then single `CompareAndSwapPointer` on data | (no panic) |
| 10 | `Load` on empty/in-progress cell → `nil` | (no panic) |

`nil` here means the nil *interface* (type word AND data word nil). A
**typed nil** (e.g. `(*int)(nil)` in an `any`) has a non-nil type word:
`Store`/`Swap`/`CAS` accept it and record its type word (`*int`).

## 1. Cell representation — the type word lives inside the payload

No separate type slot is needed. The cell stays a bare
`AtomicReference<Object>` (allocated by `SyncNewNode`:
`case "atomic.Pointer", "atomic.Value" -> new AtomicReference<>()`;
Java `null` = empty, mirroring gc's `typ == nil`).

The stored payload IS the boxed `GoInterface`; its `descriptor` field is
the dynamic type word. Rationale:

- `AtomicOpNode.build()` already boxes every non-`Load` Value arg via
  `factory.boxToInterface` (concrete args get `InterfaceBoxNode` with the
  **canonical** `GoTypeDescriptor`; already-`any` args pass through as
  `GoInterface`/Java null). Nil `any` args evaluate to Java null
  (`Boxing.zero(INTERFACE)` → null) or `GoInterface.NIL`.
- Descriptors are canonical process-wide identity tokens:
  `NodeFactory.typeDescriptorFor` canonicalizes basics by name, named
  types by qualified name, structural composites by structural key, all
  via `putIfAbsent` caches. So descriptor `==` is the sound comparison —
  established precedent: `TypeAssertNode:122`
  (`gi.descriptor() == asserted`), `GoEquality.equal:65`
  (`ia.descriptor() == ib.descriptor()`), `BinaryOpNode:323`,
  `SwitchNode:137`.
- **Invariant** (structural, enforced at the single write site):
  Value-cell payloads ∈ { Java null (empty), `GoInterface` }. Since
  `Store(nil)`/`Swap(nil)`/`CAS(*, nil)` all panic *before* writing, a
  nil interface can never be stored — Java null unambiguously means
  "no store yet", exactly gc's `typ == nil`. Typed nils carry a
  descriptor, so they are storable and typed, exactly like gc.
- `GoInterface.narrow(Object)` (Java null → `NIL`) normalizes both nil
  representations at each op entry, mirroring gc's `val == nil` test on
  the eface.

## 2. gc-semantics mapping (exact)

Each new `Atomics` method normalizes args with `GoInterface.narrow`
first, then follows the spec table above:

- **Store**: `narrow(v).isNil()` → panic #1. Loop: `cur == null` →
  `CAS(null, vi)` (win = first store, return); else
  `curI.descriptor() != vi.descriptor()` → panic #2; else
  `addr.set(vi)` — plain set, exactly gc's `StorePointer` after the
  check (last same-type writer wins).
- **Swap**: `narrow(v).isNil()` → panic #3. Loop: `cur == null` →
  `CAS(null, vi)` → return Java null (gc: first Swap returns nil);
  else descriptor mismatch → panic #4; else `CAS(cur, vi)` → return
  `cur` (the previous `GoInterface`).
- **CompareAndSwap**: (a) `narrow(new).isNil()` → panic #5 **first**;
  (b) `!oldI.isNil() && oldI.descriptor() != newI.descriptor()` →
  panic #6 (plural, before cell access); (c) loop: `cur == null` →
  `oldI.isNil() ? (CAS(null, newI) → true) : false` — gc case #8;
  else stored-type mismatch → panic #7 (singular); else
  `!GoEquality.equal(curI, oldI)` → `false`; else
  `CAS(cur, newI)` → `true`, retry on lost race (same loop style as
  the existing `compareAndSwapGoValue`).
- **Load**: unchanged (`loadPointer`; Java null on empty = nil `any`,
  per the P3-M7 "nil interface may flow as Java null" convention).
  `storePointer`/`swapPointer` stay for `Pointer[T]`.

Panic surfacing: `Panic.throwPanic(GoStrings.of("<gc message verbatim>"))`
— the 1-arg form carrying a `GoString` (NEVER a raw Java String). Both
helpers live in the same `graalgo.runtime` package as `Atomics`
(`GoStrings` is package-private there). Precedent: `GoChan`
(`"send on closed channel"`, same module+package) and `ChanCloseNode`
(`"close of nil channel"`). This produces a Go `string` panic value
like gc's `panic("...")`; `recover()` yields the `GoString`; the
unrecovered path uses the existing printpanicval-compatible rendering.
The 4-arg `throwPanic` overload is not needed (no AST Node is available
in these static runtime helpers; GoChan precedent uses the 1-arg form).

## 3. Concurrency — lock-free, no `firstStoreInProgress` needed

gc needs the in-progress sentinel + `procPin` spin because its cell is
**two words** (`typ`, `data`) written non-atomically. Our cell is **one
word** (the `AtomicReference` holds the whole `GoInterface`), so a
single CAS linearizes the first store — no sentinel, no spinning:

- Two concurrent first-stores of different types: exactly one
  `CAS(null → v)` wins; the loser re-reads, sees the winner's
  descriptor, and panics — identical outcome to gc's spin-then-check.
- Concurrent same-type stores: type check passes for both,
  last-writer-wins via plain `set`/CAS — identical to gc.
- CAS on a racing cell: the existing CAS-retry loop re-checks the type
  after every re-read, so a type change between check and CAS still
  panics (gc's single `CompareAndSwapPointer` after the equality check
  can return `false` on a lost race where ours retries; both are
  linearizable, ours matches the existing `compareAndSwapGoValue` loop
  style).
- **Known benign divergence** (document in code): gc's `Load` during an
  in-progress first store returns nil (a two-word-write artifact). Our
  single-word CAS never exposes that state — strictly stronger
  atomicity, unobservable to any race-free program.

## 4. Files changed (implementation phase, after approval)

1. `internal/backend/runtime/src/main/java/graalgo/runtime/Atomics.java`
   - Add `storeGoValue(AtomicReference<Object>, Object)` and
     `swapGoValue(AtomicReference<Object>, Object): Object` with the
     protocols above.
   - Rework `compareAndSwapGoValue` in place (keep name/signature —
     its `AtomicOpNode` call site is unchanged) to add the nil/type
     checks around the existing Go-equality CAS loop.
   - Seven `private static final` message constants? No — inline
     `GoStrings.of("...")` at each site like `GoChan` does, keeping the
     gc-verbatim strings adjacent to the check.
2. `internal/backend/interop/src/main/java/graalgo/p3sync/nodes/AtomicOpNode.java`
   - `atomic.Value.Store` → `Atomics.storeGoValue(...)`;
     `atomic.Value.Swap` → `Atomics.swapGoValue(...)`.
   - `atomic.Value.CompareAndSwap` call site unchanged.
   - `build()` unchanged — args are already boxed to `GoInterface`.

## 5. Test plan (two layers + differential probe)

**(a) Java unit tests** — new file next to
`internal/backend/interop/src/test/java/graalgo/iface/AtomicContentionTest.java`,
driving `Atomics.storeGoValue/swapGoValue/compareAndSwapGoValue`
directly on `new AtomicReference<>()` cells with hand-built
`GoInterface` values (same `GoTypeDescriptor` instance = same type;
distinct instances = different types, exercising the `==` protocol;
`GoInterface(desc, null, null)` = typed nil):
- `Store(nil)` / `Swap(nil)` / `CAS(x, nil)` panic — even on an empty
  cell — with byte-exact gc messages (catch `PanicException`, assert
  `goValue()` equals the expected `GoString`).
- `Store(int)` then `Store(string)` → panic #2, exact message;
  `Store(int)` then `Store(int)` → ok.
- Typed nil stores without panic and fixes the type word
  (`Store((*int)(nil))` then `Store(int)` → panic #2).
- `Swap` on empty → returns null; `Swap(int→int)` returns the old
  `GoInterface`; `Swap(int→string)` → panic #4.
- CAS: empty + `old==nil` → `true` (first store); empty + `old!=nil`
  → `false`; old/new type mismatch → panic #6 **even on an empty
  cell**; stored/new mismatch → panic #7; equal values → `true`;
  unequal → `false`.
- Positive + negative coverage of every row in §0 (the MinMaxNode
  lesson: no panic case goes untested).

**(b) End-to-end Go programs** via `P3SyncMain.run` (the
`RangeConcurrentRegressionTest` pattern: write temp `.go`, capture
stdout+stderr, assert): a program per panic case using
`defer`/`recover()` printing the recovered value — asserts the exact
gc strings surface through the real pipeline (exporter boxing →
`GoInterface.narrow` → `PanicException` → rendering). Plus:
- Concurrent hammer: N goroutines × M iterations of
  `Store`/`Swap`/`CAS` of the **same** type on one `Value` — must
  never panic; final `Load` is one of the stored values
  (linearizability smoke test).
- Deterministic two-type race: fill with ints, then store a string →
  must panic with message #2 (proves the loser path, not just the
  winner path).
- Regression: `Load` on a zero `Value` → nil (already worked).

**(c) Differential probe vs gc**: the e2e programs are also run under
`go run` (`~/tools/go-1.27.1`); panic strings and `Load`/`Swap`/`CAS`
results are byte-compared. Any drift beyond the documented
first-store-window benign divergence blocks the lane.

## 6. No-bridge argument

Structural fix inside existing phase-3 machinery, no bridges:

- No synthetic stdlib: the **real** `sync/atomic` package is used
  (P3-M10); only its lowering gains the checks gc itself performs.
- No Java reimplementation of Go logic: the check protocol is a
  line-for-line port of `value.go`'s own algorithm, adapted to the
  single-word cell the D5 value model already provides.
- No name-keyed special cases: dispatch stays on the existing
  `synctyp` wire field (`"atomic.Value"`), the same mechanism as every
  other atomic op; the type word comes from the value's own
  `GoInterface.descriptor` — a D5 structural property, not a side
  table keyed by anything.
- Reuses existing frozen surfaces only: `SyncNewNode`'s cell,
  `InterfaceBoxNode`'s canonical descriptors, `GoEquality.equal`,
  `GoInterface.narrow`, `Panic.throwPanic` + `GoStrings.of`.

Open question for coordinator: none on the design itself. The one
judgment call is the documented benign divergence (no
`firstStoreInProgress` window); flagging explicitly rather than
assuming.
