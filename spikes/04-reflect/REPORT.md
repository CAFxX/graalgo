# Spike 04-reflect — REPORT

**Verdict: PASS**

The intrinsic-surface design (descriptor-based type-layout core + handle-table
`unsafe.Pointer` emulation) covers everything `reflect`'s `unsafe` uses
require. Every one of 302 operational `unsafe` uses across 13 categories has a
viable formulation under the handle-table mode; none requires raw-address
materialization. Per-release linkname churn (measured 2 items 1.26.0→1.27.1,
skip-list 0) does not defeat the diff-scoping design. Neither kill trigger
fires.

Tested configuration: go1.27.1, linux/amd64, default build tags
(no experiments), `reflect` package's own `unsafe` uses. See §8 for scope
limits and re-test triggers.

## 1. Scope and method

### 1.1 The 243 vs 302 discrepancy (reconciled, not a conflict)

T4's "243" is a **per-line** count restricted to `type.go` (85 lines) +
`value.go` (158 lines). This spike counted **per-occurrence, non-comment**
across all 9 non-test `reflect` files that import `unsafe`
(`arena.go`, `iter.go`, `visiblefields.go` do not import it):

- 315 textual matches of `unsafe.` (or `unsafe` as a type) in non-test files
- − 13 non-operational (12 in comments, 1 in a string literal at `type.go:526`)
- = **302 operational uses**, every line hand-read and assigned to exactly one
  of 13 categories

`prototype/categorize.py` enforces 1:1 coverage mechanically: it exits non-zero
if any occurrence is unassigned or double-assigned. Raw evidence:
`logs/inventory_raw.txt` (every match with file:line), `logs/inventory_categorized.txt`.

### 1.2 Prototype language choice: Go (documented)

The oracle is gc itself (`unsafe.Offsetof/Sizeof/Alignof` run under stock
go1.27.1). The layout algorithms and handle-table semantics are
language-agnostic; a Truffle/Java port is mechanical once the formulations are
proven. Writing the prototype in Go let the differential test share the exact
same type battery with the oracle.

### 1.3 Protocol compliance

- sha256 archive of exact prototype sources with every log: `src_archive/`
  (7 files) + `logs/SHA256SUMS`. Toolchain pinned: `logs/toolchain.txt`
  (go1.27.1 linux/amd64).
- Checkpointing: all intermediate evidence on disk under `logs/`.
- No placeholders; negative claims scoped to the tested configuration (§8).

## 2. The 302-use categorized inventory (step 2) with per-category verdicts (step 4)

| # | Cat | Count | What it is | Verdict | Formulation |
|---|-----|-------|------------|---------|-------------|
| 1 | TP | 104 | Type-position: `unsafe.Pointer` in signatures, declarations, pure conversions (no memory touched) | handle-table | Opaque by construction — a `Handle` value flows through signatures exactly like a pointer; no dereference involved |
| 2 | TD | 86 | Type-descriptor reinterpret casts (`*(*rtype)(p)`, `*(*ptrType)(…)`) | handle-table | Descriptors are first-class Truffle objects, never raw memory; casts become descriptor accessor calls on the layout core |
| 3 | RW | 29 | Raw memory read/write (`*(*T)(p)`, `unsafe.Slice`, `SliceData`) | handle-table | Bounds-checked typed load/store through the handle table (`word R/W`, `SliceFrom`); 9 `unsafe.Slice` uses become handle-table slice constructors |
| 4 | CF | 19 | Call-frame machinery (`callReflect`, `callMethod`, regArgs) | dedicated intrinsic | Call-frame builder intrinsic: marshal `[]Value` → ABI frame via layout descriptors; the 19 uses never touch memory themselves, they orchestrate the frame |
| 5 | AD | 18 | Address-of locals/globals (`unsafe.Pointer(&x)`) | handle-table | Locals/globals are objects in the handle table; address-of = handle creation, never a real address |
| 6 | IF | 17 | iface/eface word poking, prototype extraction (`*(*iface)(…)`, `eface.data`) | handle-table | eface pack/unpack + interface-word accessors on handles; the two-word layout is emulated, not observed |
| 7 | ZV | 7 | `&zeroVal[0]` (pointer to the shared zero base) | handle-table | Canonical zero handle: a single immortal handle denoting zeroed memory of any type |
| 8 | TD-R | 6 | Module offset resolution (`resolveNameOff/TypeOff/TextOff`, `addReflectOff`) | dedicated intrinsic | Descriptor-slot resolution intrinsic: module + offset → descriptor object. Not blob arithmetic — offsets are resolved against the module's type table |
| 9 | FP | 6 | Func code-pointer extraction, PC↔pointer (`Value.Pointer` on funcs, `reflectcall` PCs) | dedicated intrinsic | Synthetic code-pointer table: func values map to tokens; `Value.Pointer()` on a func returns a token, never an executable address |
| 10 | PA | 3 | Pointer arithmetic (`unsafe.Add`, `uintptr(p)+off`) | handle-table | `Handle.Add(offset)` with bounds checks; arithmetic stays in (object, offset) space |
| 11 | SH | 3 | Slice/string header reinterpret (`*(*sliceHeader)(p)`) | handle-table | Headers are constructed by the handle table from (handle, len, cap), never by reinterpreting memory |
| 12 | AL | 2 | `unsafe.Sizeof` (not the 104 TP uses — actual size queries) | layout core | Answered by the type-layout core at compile time; no runtime memory involved |
| 13 | RC | 2 | Race-detector call arguments | handle-table | Handle-aware race note: race instrumentation keys on handle identity, not addresses |

**No category requires raw-address mode.** The hardest cases and why they resolve:

- **User-facing `Pointer`→`uintptr`** (`Value.Pointer()`, `Value.UnsafeAddr()`): opaque
  tokens via a token table with round-trip. A `uintptr` obtained from a `Value`
  round-trips back through `unsafe.Pointer` only via the token table — it is
  never a dereferenceable address. This matches the documented Go semantic that
  such `uintptr`s are not valid pointers after the expression ends.
- **`isZero`'s `uintptr(p) % 8` idiom**: under handles, unaligned access cannot
  fault; alignment is answered from type metadata, not probed.
- **`verifyNotInHeapPtr`**: becomes a handle-kind check (foreign-memory kind vs
  heap-object kind), not a heap-bounds comparison.
- **TD-R module offsets**: resolved by a descriptor-slot intrinsic against the
  module's type table; no raw moduledata blob arithmetic.

Unsafe API surface actually exercised by `reflect`: `unsafe.Pointer` (298),
`unsafe.Slice` (9), `unsafe.Sizeof` (3), `unsafe.SliceData` (1), `unsafe.Add`
(1). (`unsafe.String`/`StringData` hits were comment-only.)

## 3. Layout differential evidence (step 3)

`prototype/layout/layout.go` implements Go struct/array/slice/map/string/interface
layout from first principles, replicating gc's exact algorithm as read from
`src/cmd/compile/internal/types/size.go` (`calcStructOffset`, including the
issue-9401 trailing-zero-size-field `size++` rule) and
`src/cmd/compile/internal/types2/sizes.go` (including the
`sync/atomic.align64` special case).

`prototype/difftest/main.go` runs a 32-type battery (padding, nested/embedded
structs, zero-size fields in every position, `[0]T`, slice/string headers,
`atomic.Int64`, recursive types) under stock go1.27.1 and compares
`unsafe.Offsetof/Sizeof/Alignof` ground truth against the prototype:
**32/32 match, 0 mismatches** (`logs/difftest.log`).

`prototype/handletable/` emulates the handle table (`Handle` = (object index,
offset), bounds-checked `Add`, word R/W, `SliceFrom`, eface pack/unpack, slice
headers, frame traffic, synthetic code-pointer table, canonical zero handle,
opaque Pointer↔uintptr token table, handle-aware race note, descriptor-slot
offset resolution): **15/15 formulation-existence tests pass**
(`logs/prototype_tests.log`).

## 4. Q3 recommendation: `unsafe.Pointer` v1 semantics (SPEC §10)

**Adopt the handle-table as the primary (and default) mode**, with two
narrowly-scoped companions:

1. **Handle-table mode (default):** every `unsafe.Pointer` inside managed Go
   code — including all of `reflect`'s own 302 uses — is a `(object, offset)`
   handle. Arithmetic is bounds-checked; `uintptr` exposure is via opaque
   tokens; interface/func internals are emulated, not observed.
2. **Raw-address mode (reserved):** for foreign/not-in-heap memory only
   (cgo-allocated, memory-mapped, arena-external). `reflect` itself never needs
   it for its own 302 uses; it exists so user code doing genuine FFI keeps
   working.
3. **Opaque tokens** for the `Pointer()`→`uintptr` boundary: round-trippable
   through the token table, never materialized as addresses.

**Raw-address materialization is NOT required for any of `reflect`'s own
302 uses.** The dedicated-intrinsic set is small and bounded: call-frame
builder (CF), descriptor-slot offset resolution (TD-R), synthetic code-pointer
table (FP), interface-word accessors (IF), canonical zero handle (ZV) —
all proven by the 15/15 prototype tests.

## 5. Linkname scoping assessment (step 5)

### 5.1 Inventory on go1.27.1 (default build)

- **43 runtime→reflect edges**: 42 in `src/runtime` (`stubs.go`, `mbarrier.go`,
  `map.go`, `chan.go`, `iface.go`, `malloc.go`, `unsafe.go`, `mbitmap.go`,
  `slice.go`, `linkname_shim.go`, `runtime1.go`) + 1 in `src/arena/arena.go`
  (`reflect.arena_New`, gated behind `goexperiment.arenas` — tracked, not in
  the default surface). Mechanism detail (verified via `go tool nm`): reflect's
  bodyless `//go:noescape` declarations are satisfied by **runtime-side**
  `//go:linkname reflect_X reflect.X` wrappers; the newer `//go:linknamestd`
  form is used for `compiledTypelinks` and `adjustAIXGCDataForRuntime`.
  (`logs/linkname_runtime_to_reflect.txt`)
- **3 reflect→runtime edges**: `runtime.getStaticuint64s`,
  `runtime.zeroVal`, `runtime.reflectcall` (declared as `call`).
  (`logs/linkname_reflect_to_runtime.txt`)
- **6 push markers** (ecosystem-compat, no in-tree consumers — verified by
  grep): `reflect.add`, `reflect.addReflectOff`, `reflect.mapassign`,
  `reflect.rtypeOff`, `reflect.toType`, `reflect.typesByString`. Excluded from
  the skip-list by rule. (`badlinkname.go`'s ~30 hooks are test-only by design
  and likewise excluded.)

### 5.2 The per-release diff-scoping rule

`prototype/linkname_diff.py <old_tree> <new_tree>` (archived in
`src_archive/`, checksummed):

1. Extracts edge sets mechanically from both trees (runtime/internal/arena
   provider side; reflect consumer side; test files and `badlinkname.go`
   excluded).
2. Classifies the delta: **REMOVED** (in old, gone in new), **ADDED** (new
   edge), **SIG-CHANGED** (same name, different Go signature), UNCHANGED.
3. **Skip-list** = REMOVED + SIG-CHANGED (needs manual adaptation or an
   adapter). **Watch-list** = ADDED (new intrinsic-surface requirement).
4. Kill-trigger check: skip-list growth > 15 in two consecutive releases.

### 5.3 Empirical calibration: go1.26.0 → go1.27.1

| Direction | Count | Edges |
|-----------|-------|-------|
| ADDED (watch-list) | 2 | `reflect.adjustAIXGCDataForRuntime` (AIX GC-data pointer fixup for the new typelinks scheme), `reflect.compiledTypelinks` (type enumeration refactored from raw `typelinks()` sections+offsets to pre-sorted `[]*abi.Type`) |
| REMOVED | 0 | — |
| SIG-CHANGED | 0 | — |
| reflect→runtime churn | 0 | 3/3 stable |
| push markers churn | 0 | 6/6 stable |

**Skip-list for this release: 0. Watch-list: 2, both absorbed** (AIX-only
fixup; type enumeration — the intrinsic "enumerate all linked types" is
unchanged, only the mechanism moved). Total manual-work items: 2, against a
kill threshold of >15.

Note the calibration validates the churn-risk ranking below: the one subsystem
that actually churned is TD-R (module/type-offset resolution) — ranked Tier 1.

### 5.4 Adversarial churn-risk ranking

- **Tier 1 (highest):** map edges (`mapassign0`, `mapaccess*`,
  `mapiterinit/next/key/elem`, `makemap`, `maplen`, `mapdelete*`, `mapclear`).
  The map implementation was carved into `internal/runtime/maps` in Go 1.24
  and `goexperiment.MapSplitGroup` is active in 1.27's `reflect/map.go` —
  this subsystem is under live experimentation. The `mapassign0`/`mapassign`
  and `mapassign_faststr0`/`mapassign_faststr` splits exist for inlining
  control and are exactly the kind of seam renamed during refactoring.
  TD-R edges (`resolveNameOff/TypeOff/TextOff`, `addReflectOff`,
  `compiledTypelinks`, `typelinks`) — **proven churny**: this is where the
  1.26→1.27 change landed.
- **Tier 2 (medium):** `growslice`/`unsafeslice` (slice growth is a perennial
  optimization target); `ifaceE2I` (interface representation); 
...[truncated 3828 chars]
