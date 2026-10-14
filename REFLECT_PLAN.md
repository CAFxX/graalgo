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
- [ ] Verify `internal/abi` compiles under graalgo
- [ ] Fix any compilation issues (should be minimal — it's pure Go)

### Phase 2: Type descriptor bridge
- [x] Runtime hook: construct `abi.Type` from graalgo's wire type table (via ReflectTypes)
- [x] Frontend: intercept `reflect.TypeOf` → runtime hook (already existed)
- [x] Frontend: intercept `reflect.ValueOf` → runtime hook (already existed)
- [x] Iter family: Type.Methods/Fields/Ins/Outs/CanSeq/CanSeq2, Value.Seq/Seq2/Methods/Fields (commit 7b02e11)
- [x] Type constructors: ArrayOf/SliceOf/MapOf/ChanOf/PtrTo (commit 7b02e11)
- [ ] Type constructors: FuncOf, StructOf (need slice arg support in reflectop)

### Phase 3: Real reflect package
- [ ] Remove synthetic reflect, use real reflect source
- [ ] Keep interceptions ONLY for unsafe-based primitives
- [ ] All other reflect code runs unmodified

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
