# graalgo.values

The Go value model (PLAN.md D5 [DECIDED], GG-0015). Every Go value lives as
a Java object (or unboxed primitive) in its D5 carrier:

| Go kind | Java carrier |
|---|---|
| int8/16/32, uint8/16/32 | `int` (unboxed) |
| int64, uint64, int, uint, uintptr | `long` (unboxed) |
| float32 | `float` (unboxed, custom `executeFloat`) |
| float64 | `double` (unboxed) |
| bool | `boolean` (unboxed) |
| string | `GoString{byte[]}` |
| slice | `GoSlice{typedArray,offset,len,cap}` |
| array | typed Java array (value copy) |
| struct | `GoStruct{descriptor,long[],Object[]}` |
| map | `GoMap` (open-addressing, linear probing) |
| interface | `GoInterface{(descriptor,data),itab}` |
| func | `GoFunc{RootCallTarget,Frame}` |
| pointer | opaque Java reference |
| unsafe.Pointer | handle-table entry (`UnsafeHandles`); the `→ uintptr` conversion yields the handle as an integer identity token (nil → 0, stable, distinct per referent); arithmetic and `uintptr →` materialization trap |

## Classes

- `GoKind` — the kind lattice; `fromBasic`/`fromCompositeKind` map
  frontend/v1 type-table entries; `slotKind()` gives the frame slot kind;
  `transparent()` encodes untyped-constant transparency (constants assume
  the context type, mirroring go/types — no conversion node inserted).
- `GoString` — immutable byte sequence; byte-wise equality, unsigned
  lexicographic comparison (matches gc's string ordering).
- `GoSlice` — descriptor over a typed Java array; subslicing shares the
  backing array.
- `GoMap` — open-addressing hash table; keys use `GoEquality` (Go `==`;
  NaN keys never match, matching Go); absent lookup returns null (the
  map-access node substitutes the zero value — WS D). A nil interface key
  is stored as the `GoInterface.NIL` singleton (never Java null).
- `GoStruct` / `GoStructDescriptor` — descriptor-assigned `long[]`/`Object[]`
  carriers; value-copy assignment semantics.
- `GoInterface` — `(descriptor, data)` + immutable itab (D6: itab
  speculation needs no Assumptions); `NIL` is the nil interface value.
- `GoFunc` — `RootCallTarget` + captured (materialized) closure frame.
- `UnsafeHandles` — the `unsafe.Pointer` handle table. Arithmetic on
  handles and materializing a handle from an integer trap
  (`UnsafeEscapeException`); the `unsafe.Pointer → uintptr` conversion
  yields the handle itself as an opaque integer identity token — never a
  raw address, never dereferenceable (PKG-007).
- `GoEquality` — Go `==` over carriers + consistent hashes.
- `GoConvert` — the non-bit-trivial conversion matrix. Integer↔integer is
  pure truncation/extension (in `ConvertNode`); float→int out-of-range
  rules mirror stock gc/amd64 **as measured** (the spec leaves them
  implementation-defined — probes in the M3 report); int/uint→float and
  float↔float are correctly rounded; int→string is the rune conversion
  (out-of-range → U+FFFD).
- `InterfaceDispatchProbes` — itab-identity histogram hooks for the M9
  call-site polymorphism measurement (WS D/L). Hooks only: no dispatch
  implementation in Phase 1; zero cost when disabled (one volatile read).

## Notes

- There is no observable address anywhere (GG-0015): pointers are opaque
  references, `unsafe.Pointer` is a handle. The `→ uintptr` conversion
  yields the handle as an integer identity token, not an address — it
  carries identity (nil → 0, stability, distinctness) and no path back to
  a referent.
- `GoMap` iteration order is unspecified (as in Go); deterministic
  iteration is not provided.
- Composite construction (composite literals, indexing, selectors) and
  interface dispatch arrive with WS D; the classes are complete and
  unit-tested here.
