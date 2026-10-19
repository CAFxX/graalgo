# Design: live aliasing for `unsafe.Slice`/`unsafe.String` over scalar cell pointers (I-10)

**Lane:** `rev-unsafe-slice-alias` · **Status:** design only — awaiting coordinator approval before implementation.
**Review ref:** `REVIEW-2026-10-19-05.md` §1, I-10 (P0).

## 1. Problem

`unsafe.Slice(ptr, len)` / `unsafe.String(ptr, len)` over a scalar cell pointer
(`new(T)`, `&x`) have **snapshot** semantics, not gc aliasing:

- writes through the view never reach `*p`;
- `*p = v` after view creation is invisible through the view;
- views created before/after a `*p = v` alias *different* carriers (the
  `normalized` cache is keyed on the box object, which `set` replaces).

**Root cause.** [UnsafeSliceNode.java](../../internal/backend/interop/src/main/java/graalgo/p3unsafe/UnsafeSliceNode.java)
(and [UnsafeStringNode.java](../../internal/backend/interop/src/main/java/graalgo/p3unsafe/UnsafeStringNode.java))
pass `gp.referent()` — the cell's *content box* — and
[UnsafeOps.java](../../internal/backend/interop/src/main/java/graalgo/p3unsafe/UnsafeOps.java)
`openScalar` → [UnsafeTable.java](../../internal/backend/interop/src/main/java/graalgo/p3unsafe/UnsafeTable.java)
`normalizeScalar`/`makeCarrier` copies the box's current value into a fresh
1-element array. The corpus test
[05_slice_shapes.go](../../test/corpus/wave2/unsafe/05_slice_shapes.go) only
exercises view↔view aliasing, never view↔storage.

## 2. Design: the carrier IS the cell's storage

### 2.1 Who owns the array — the cell

- **GoBox** ([GoBox.java](../../internal/backend/truffle/src/main/java/graalgo/values/GoBox.java)):
  when its value is a scalar box (`Integer`/`Long`/`Float`/`Double`/`Boolean`),
  the cell's storage *is* an owned 1-element typed array
  (`int[1]`/`long[1]`/`float[1]`/`double[1]`/`boolean[1]`), allocated at
  construction. `get()` boxes from the array; `set()` unboxes into it. The
  plain box field remains for non-scalar values.
  `set` transitions: scalar box on a non-carrier box *adopts* a carrier
  (covers `var x any` nil→scalar); non-scalar on a carrier box *abandons* it
  (degenerate-view case, documented — the program was already in gc-UB
  territory); scalar-kind change *re-adopts* (legal Go for `any`; never panics
  on legal code).
- New `GoCell.unsafeCarrier()` (default `null`): returns the live carrier
  array, or null when the cell's storage cannot be viewed as a Java array.
- **GoFieldCell** ([GoFieldCell.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/GoFieldCell.java)):
  storage lives in the struct carrier (possibly bit-packed in a long) — no
  Java array can alias it. The view path fails fast with an `unsafe.<op>`
  D5 message (replacing today's silent snapshot; no corpus test covers this
  shape — verified).
- **GoArrayCell** ([GoArrayCell.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/GoArrayCell.java)):
  never constructed anywhere in the repo (dead code) — out of scope.
- Single source of truth for the box↔carrier mapping: new
  `graalgo.values.ScalarCarrier` helper (`make`/`read`/`write`/`isScalarBox`);
  `UnsafeTable.makeCarrier`/`normalizeScalar` delegate to it (removes the
  twin-switch drift risk flagged in `UnsafeOps.expectedArrayClass`).

### 2.2 Lifetime vs cell

The carrier is created with (or adopted by) the cell and dies with it — no
table-level pinning. The `normalized` box→carrier cache stays, but only for
the plain-pointer `intern()` path. Handles keep interning the *cell*
(P3-M10, unchanged), so `unsafe.Pointer` identity is untouched. A surviving
`GoSlice` keeps the carrier alive after the cell dies (matches gc: the memory
stays alive via the slice).

### 2.3 View-path changes (p3unsafe)

- `UnsafeSliceNode`/`UnsafeStringNode`: for cell pointers pass `gp.cell()`
  instead of `gp.referent()`.
- New `UnsafeOps.openCell(referent, op, site)`: `GoCell` → `unsafeCarrier()`;
  carrier → used directly (offset 0); null carrier → `cell.get()`: null → nil
  path; `GoFieldCell` + scalar box → D5 panic naming the op; otherwise
  `normalizeScalar(value)` (preserves today's exact behavior for non-scalar
  cells, e.g. `&arr` whole-array views).
- `normalizeScalar`'s bare-box branch is retained for plain (non-cell) pointers
  (the defensive snapshot path) and for `intern()`.

### 2.4 `unsafe.String` read-only path

Unchanged shape: still copies per DIV-US-1 — but the copy now reads the *live*
carrier at call time, so strings created after `*p = v` reflect the current
value (gc parity).

### 2.5 Why this doesn't repeat the P3-M9 carrier failure

The reverted P3-M9 design made the carrier the storage for `new(T)` scalars,
but `DerefWriteNode` *replaced* the carrier on `*p = v`. Here
`GoPointer.setReferent` → `cell.set` writes *into* the carrier, and every
frontend write path goes through `set()` — the carrier can never be silently
replaced. Verified call chain: `DerefWriteNode` → `setReferent` → `cell.set`.

### 2.6 Frontend touch points (mechanical)

`GoBox.value` becomes private; all direct accesses switch to `get()`/`set()`:

- [BoxReadNode.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/BoxReadNode.java) (`box.value` → `box.get()`)
- [BoxRefreshNode.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/BoxRefreshNode.java) (`carry.value` → `carry.get()`)
- [BoxWritebackNode.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/BoxWritebackNode.java)
  (`carry.value = …` / `iter.value` → `set()`/`get()`; the "never replacing the
  box object" invariant holds — `set()` writes into the carrier)
- [Boxing.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/Boxing.java)
  (`setBoxedCell`: `cell.value = boxed` → `cell.set(boxed)`)
- [GlobalBoxReadNode.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/GlobalBoxReadNode.java),
  [GlobalBoxWriteNode.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/GlobalBoxWriteNode.java),
  [GoFuncRootNode.java](../../internal/backend/truffle/src/main/java/graalgo/frontend/GoFuncRootNode.java)
  (same mechanical swap)

No interop-module changes are needed for these: its `Cell` is the separate
`ReflectRefs.Cell`, not `GoBox` (verified — no `GoBox` field access in
interop main).

## 3. Behavior preserved (no regressions)

- Narrow-kind scalars (`int8`, `byte`, …): carrier `int[1]` vs
  `expectedArrayClass` `byte[]` → same D5 panic as today (pre-existing,
  out of scope).
- `len > 1` over a scalar carrier → same "len out of range" panic as today
  (pre-existing cap model, out of scope).
- `unsafe.SliceData`/`StringData`, handle interning, `pointerFor` — untouched.
- `get()` returns a fresh box per call: safe, because interning keys on the
  cell object, never on `get()` results (P3-M10; `ReflectRefs.intern` callers
  pass cells/referents — verified).
- Thread-safety: unchanged (plain fields, as today; races on Go variables are
  the program's per the Go memory model).

## 4. Tests (the suite blind spot)

1. **Corpus** (differential + snapshot): new
   [test/corpus/wave2/unsafe/](../../test/corpus/wave2/unsafe/)
   `11_scalar_cell_aliasing.go` — view→storage (`s[0]=v` then `*sp`),
   storage→view (`*sp=v` then `s[0]`), view stability across `*p = v` (two
   views alias after a write), `&x` local (not only `new`), `unsafe.String`
   over `&b` reflecting the post-write value. Requires `make snapshot`
   re-record (new baselines only; verify zero drift in existing entries).
2. **In-JVM**: `GoBox` carrier unit tests
   (`internal/backend/truffle/src/test/java/graalgo/values/`): set→carrier→get
   round-trip, carrier identity stability across sets, adopt/abandon
   transitions; `UnsafeOps.slice(cell, …)`-level test in interop (install table,
   assert bidirectional aliasing).

## 5. Open questions for the coordinator

1. **GoFieldCell scalar fail-fast vs keep snapshot**: fail-fast is honest
   (frozen-contract taxonomy) but introduces a new panic on previously
   "working" (silently wrong) code. Recommended: fail-fast.
2. **Eager carrier per scalar GoBox** (uniform, no mode transition) **vs lazy
   adoption on first unsafe view** (fewer allocations on hot loop-var paths).
   Recommended: eager — one small array per scalar boxed variable (precedent:
   P3-M9's reverted design did the same).
3. `unsafe.Slice` len>1 over a scalar: keep today's cap panic — confirm out of
   scope.
