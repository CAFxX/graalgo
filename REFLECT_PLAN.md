# Real Reflect Implementation Plan

Per Carlo's 2026-10-14 orders:
- Zero duplication of stdlib logic anywhere
- Real `reflect` package compiles unmodified; graalgo provides runtime hooks
- Do NOT reimplement reflect's logic — provide the low-level primitives it needs

## Architecture

The real `reflect` package accesses type info via:
1. `unsafe` casts on interface values → `*rtype` (CANNOT work on Truffle backend)
2. `internal/abi` types (pure Go, CAN compile)
3. 3 runtime linknames: `reflectcall`, `zeroVal`, `getStaticuint64s`

Strategy: compile real `reflect` + `internal/abi` unmodified. Provide runtime
hooks for the unsafe-based primitives via frontend interception (same mechanism
as existing `__reflect_*` intrinsics, but ONLY for the primitives — all logic
comes from real reflect).

## Phases

### Phase 1: `internal/abi` compilation
- [x] Probe real internal/abi under graalgo (2026-10-15): loads from GOROOT
      disk importer, but fails: `func FuncPCABI0 without body` — funcpc.go's
      two functions are gc compile intrinsics (asm-implemented). Everything
      else in the package is pure Go. Needed: frontend intrinsics
      `__abi_FuncPCABI0` / `__abi_FuncPCABIInternal` returning opaque stable
      func ids (used by reflect/makefunc.go, map.go race hooks, type.go
      embeddedIfaceMethStub).
- [ ] Provide the two FuncPC* intrinsics (frontend + runtime).

### Phase 2: Type descriptor bridge
- [x] Runtime hook: construct `abi.Type` from graalgo's wire type table (via ReflectTypes)
- [x] Frontend: intercept `reflect.TypeOf` → runtime hook (already existed)
- [x] Frontend: intercept `reflect.ValueOf` → runtime hook (already existed)
- [x] Iter family: Type.Methods/Fields/Ins/Outs/CanSeq/CanSeq2, Value.Seq/Seq2/Methods/Fields (commit 7b02e11)
- [x] Type constructors: ArrayOf/SliceOf/MapOf/ChanOf/PtrTo (commit 7b02e11)
- [x] Type constructors: FuncOf, StructOf (slice-arg coercions in ReflectOpNode;
      Go-side validation mirrors gc panic messages; commit pending)
      Bugs fixed along the way (2026-10-15):
      - structOf token-index: newTok computed before promoteMethods appended
        wrapper func types -> struct landed at wrong index, typeString
        recursed forever. Fixed by reserving the index first.
      - basicSize(STRING) was 8, must be 16 (gc ABI) — field offsets/sizes.
      - reflectElem POINTER case: New(T).Elem() CCE'd on Cell-unwrapped
        referent (pre-existing, named structs too) — keep original handle.
      - zeroFor: nested struct fields were null — recursive zeroStruct.
      - invokeMakeFunc: result slice element read (was array cast).
- Differential: test/differential/corpus/47_reflect_funcof_structof/main.go
  byte-identical vs gc 1.27.1 (FuncOf/StructOf basics, panics, embedding
  promotion + calls, MakeFunc, New/Elem/Set, offsets/sizes/tags).

### Phase 3: Real reflect package (DESIGN, grounded 2026-10-15)
- [ ] Remove synthetic reflect, use real reflect source
- [ ] Keep interceptions ONLY for unsafe-based primitives
- [ ] All other reflect code runs unmodified

Design (empirically grounded):
1. `internal/abi` loads from GOROOT today; only blocker is the two
   FuncPC* compile intrinsics (Phase 1 finding above).
2. Cast interception point exists: `exportUnsafeConversion` in
   `internal/frontend/synth_unsafe.go` (`unsafefromptr`/`unsafetooptr`
   wire nodes). Scoped rule: inside packages reflect/internal-abi,
   `(*T)(unsafe.Pointer(x))` where T is a descriptor type lowers to a
   `__reflect_cast` intrinsic instead of the generic path.
3. Materialized descriptors: runtime keeps token -> materialized *abi.Type,
   allocated as the MOST-DERIVED Go struct (abi.StructType for structs,
   abi.FuncType for funcs, ...). Then `(*structType)(unsafe.Pointer(t))`
   is a valid downcast: unsafefromptr/unsafetooptr round-trip the same
   object (handle-based pointer model, UnsafeToPtrNode), and field reads
   go through the referent's most-derived descriptor. All pure-Go methods
   on *abi.Type (Kind, Elem, Field, ...) then work unmodified.
4. Interface-word reads: `*(*abi.EmptyInterface)(unsafe.Pointer(&i))` in
   abi.TypeOf -> intercept to (typeTok, data) from the graalgo `any`;
   materialize *abi.Type from the token (cached). NoEscape -> identity.
5. Value data path: real reflect's Value methods do `*(*T)(v.ptr)` loads,
   stores, and `add` pointer arithmetic on the DATA pointer (152
   unsafe.Pointer uses in value.go). These need memory primitives, not
   logic reimplementation: typed load/store through unsafe.Pointer +
   add. Enumerate the pattern set; each becomes an intrinsic. The TYPE
   side (all of type.go) needs no value-memory support.
6. Linknames (Phase 4): runtime.reflectcall (Value.Call),
   runtime.zeroVal, runtime.getStaticuint64s.

NOT pursued: real gc-layout flat memory for descriptors (backend has no
flat memory model; p3unsafe is handle/object-based — verified 2026-10-15).

### Phase 4: Runtime linknames
- [ ] Implement `runtime.reflectcall` (for Value.Call)
- [ ] Implement `runtime.zeroVal` (for zero values)
- [ ] Implement `runtime.getStaticuint64s` (static data)

### Phase 5: Verification
- [ ] Run reflect package tests
- [ ] Verify fmt works with real reflect
- [ ] Kill backend Fmt.java/FmtScan.java duplications

## Current State

- Synthetic reflect: `internal/frontend/synth_reflect.go` (2477 lines, 254 panics)
- Java runtime: `internal/backend/interop/src/main/java/graalgo/p3reflect/` (~4000 lines)
- `GoTypeDescriptor`: `internal/backend/truffle/src/main/java/graalgo/values/GoTypeDescriptor.java`
- `ReflectTypes` table: built from wire type table, has field/method info
