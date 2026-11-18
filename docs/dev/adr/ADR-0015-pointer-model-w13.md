# ADR-0015 — W13 on the flat value model: checked pointer conversions, view pointers, and a byte region for byte-sized leaves

**Date:** 2026-11-18
**Status:** **accepted by the owner 2026-11-18** (all four decisions). It
decides how toolexec work package W13 is built in the node package and
amends [SPEC.md](../../arch/SPEC.md) §3.4's wording (decision 3). No
requirement changes: GG-0061 and GG-0062 stay as they are.
**Scope:** [toolexec-design.md](../../arch/toolexec-design.md) §3.4, §5.6,
§9 (W13, W9a, W8a); [truffle-design.md](../../arch/truffle-design.md) §15
(rows "Pointers", "Slices", "Complex");
[abi-spike.md](../../arch/abi-spike.md) §1 (P1).
**Evidence:** the M1r first slice (toolexec-design §13, "W5 + W6, first
slice"): upstream's std source now runs on the node package, which is what
exposed the two facts below.

## Context

As built (truffle-design §15): every struct, array, slice backing store and
escaping variable is one `Flat { type, long[] prims, Object[] refs }`; a
`*T` is the referent `Flat` when it points at the object's start, else
`Ptr { obj, prim, ref }` (slot offsets); a slice is
`Slice { base, prim, ref, len, cap }`; a string is `Str { byte[] }`.

1. **Pointer conversions through `unsafe.Pointer` are an unchecked
   identity.** `(*U)(unsafe.Pointer(p))` hands the same Java object to code
   that addresses it with `U`'s slot offsets. When `U` is `p`'s pointee type
   (the `atomic.Pointer[T]` round trip) that is right; when it is not
   (`(*abi.EmptyInterface)(unsafe.Pointer(&i))`, `atomic.Value`'s
   `efaceWords`, `math.Float32bits`) it reads the wrong slots and **nothing
   fails**. Before the fork existed only user code could reach this; now
   every std package compiled through the path can. Until W13 lands, each
   std package admitted to the M1r gate has to be read for such conversions,
   which is the opposite of "compile what cmd/go hands over".
2. **A `[]byte` element is a `long` slot.** SPEC §3.4 says "slices as
   `{typedArray, offset, len, cap}` with typed Java arrays". The node
   package postponed it because with inlined arrays a `[]byte` can alias a
   struct's `[N]byte` field, whose elements are `long` slots, so a typed
   backing "needs a second slice shape" (truffle-design §15). The cost
   today: 8 bytes per byte, a loop for every `string` ↔ `[]byte`
   conversion, no zero-copy `unsafe.String`/`unsafe.Slice`, and every
   consumer that wants bytes — the `internal/bytealg` intrinsics, the
   syscall executor's `Bytes` role, descriptor images, `hash/*` — has to
   pack and unpack.

The next rung of the ladder stands on both: `errors`'s package initializer
runs `reflectlite.TypeOf((*error)(nil)).Elem()` (an interface-header view
and two descriptor reads), and `strconv`, `strings`, `bytes`, `time` import
`errors`.

## Decisions

1. **A pointer conversion is an identity that is checked, with a view as
   the fallback.** `unsafe.Pointer` carries the same Java object as the
   typed pointer it was converted from (`Flat` or `Ptr`). Converting to
   `*U` asks one question — do `U`'s leaves coincide with the referent's
   leaves from the pointer's location on (same kinds, same sizes, same
   relative gc offsets; decided from the two leaf tables and cached per
   (referent type, location class, `U`)) — and on yes returns the same
   object, as today. On no it returns a **view pointer**
   `View { obj, off }` (referent + gc byte offset, SPEC §3.4's definition).
   The location node of a `*U` (`Mem.PtrLoc`) gains that third shape as a
   DSL specialization: loads and stores through a view go by gc byte offset
   through the referent's leaf table — an N-byte scalar is assembled from
   or spread over the leaves it covers, a pointer word must be exactly one
   reference leaf, anything else is gc's `checkptr` fatal error. Typed
   code never sees a view and pays nothing; the slot offsets of a `Ptr`
   stay what they are. The gc byte offset of a `Flat`/`Ptr` is derived on
   demand from the inverse leaf table (cold path: `unsafe.Add`,
   `uintptr(p)`, view creation), so pointer values do not grow.
2. **Headers are viewed through the slot that holds them.** An interface,
   string or slice value is an immutable Java object in a reference slot.
   A view over a variable of such a type exposes gc's header words (type +
   data; data + len; data + len + cap) by reading the object in the slot
   and, on a store, **replacing** it (a store of the type word writes
   `new Iface(type, old.data)`). No header object is ever mutated, so
   values already copied elsewhere keep value semantics; `packEface`,
   `SetLen` and `atomic.Value` work as written.
3. **Byte-sized leaves live in a byte region** (the amendment of SPEC
   §3.4's "typed Java arrays"). `Flat` becomes
   `{ type, long[] prims, Object[] refs, byte[] bytes }`; the leaf table
   maps every 1-byte leaf (`uint8`, `int8`, `bool`, as fields and as array
   elements alike) to an index of `bytes`. Consequences:
   - one slice shape still: `Slice { base, prim, ref, byt, len, cap }`. A
     `[]byte` over a `make`d array and one over a struct's inlined
     `[N]byte` field both address `base.bytes`; the aliasing case that
     blocked typed backing disappears because the *struct's* byte array is
     typed too;
   - `string(b)` and `[]byte(s)` are one `arraycopy`; `unsafe.String`,
     `unsafe.Slice` and `unsafe.StringData` can share storage (`Str` gains
     offset and length);
   - `internal/bytealg`'s intrinsics, the syscall executor's `Bytes` role
     and descriptor images work on `byte[]` directly;
   - region selection is static (by the leaf kind of the static type), so
     typed loads and stores keep constant offsets and no shape check.
   Other narrow kinds (`int16`, `int32`, `float32`) stay `long`/`double`
   slots: nothing semantic needs them typed, and whether `[]int32` as
   `int[]` pays is a benchmark question (P-item, after M2), not a W13 one.
4. **`uintptr(p)`** follows SPEC §3.4 unchanged: a range-allocating handle
   table keyed by referent identity; integer → pointer is a floor lookup
   that yields `(obj, off)`, converted to `*U` by decision 1.

## Alternatives considered

- *Guard on the dynamic type only* (reject a conversion when
  `Flat.type` is not the pointee type): wrong in both directions — a
  pointer to an object's first field **is** the object's `Flat`, so legal
  conversions would trap, and a `Ptr` carries no pointee type, so illegal
  ones would pass.
- *Every pointer is `(obj, gc byte offset)`*, slots looked up at each
  access: uniform, but puts a leaf-table lookup on every typed load.
- *Typed arrays per element kind with two slice shapes* (truffle-design
  §15's "second slice shape"): every slice site becomes polymorphic over
  "own typed array" and "window into a `Flat`"; decision 3 gets the bytes
  typed with one shape.
- *Keep bytes as `long` slots and copy at the boundaries*: no node change,
  but zero-copy `unsafe.String`/`Slice` is unreachable and W8a/W9a each
  grow a marshalling layer over `long[]`.

## Consequences and order of work

1. Leaf tables with regions and the inverse table on `GoType` (the
   frontend already delivers gc sizes and offsets for named types;
   toolexec-design §3.4's "descriptor facts for every type" extends that
   to unnamed ones).
2. The byte region: `Flat`, layouts, `Slice`, `Str`, the conversion and
   `copy`/`append` nodes — mechanical, wide, and verified by the existing
   M1a gate before anything new is enabled.
3. Checked conversions and `View` in `Mem.PtrLoc`; header views; `unsafe`
   builtins as node kinds; the handle table; `checkptr` fatal errors.
4. W9a on top: descriptor images as addressable objects, the seven
   `internal/reflectlite` intrinsics, imgcheck.

Gates that must be able to fail: a unit suite per addressable kind (load and
store of 1/2/4/8-byte scalars and pointer words at every offset of a
generated corpus of struct types, against gc's own answers); the
`math.Float64bits`-style bodies run as Go once views exist (the four
`replace` entries of the intrinsic registry then stay as optimizations and a
test runs both); `errors.Is`/`As`, `sort.Slice`, `atomic.Value` and
`strings.Builder` programs in `make test-m1r`; `pe-check-m1a` stays at zero
bailouts and `make bench-m1a` shows no regression on the typed paths.

Risks: the third location shape must not leak into typed code (PE test per
node class); the byte region touches most of `Data`/`Mem` and the factory
at once (decision 3 can land before 1 and 2, behind the M1a gate alone);
header-view stores racing with plain stores are as racy as in gc.

## Open questions for the owner

1. Accept decision 3's amendment of SPEC §3.4 (a byte region inside the flat
   value instead of "typed Java arrays" in general), or require typed arrays
   for all element kinds now?
2. Is a conversion that gc accepts but that needs a view allowed to be
   slower by a leaf-table access per load (it is today's `reflect` cost
   model, 15–20× gc before inline caching, toolexec-design R1), or must
   view access be specialized per (referent type, `U`) from the start?
3. `bool` in the byte region: gc stores one byte; the node package's `Bool`
   carrier is a Java `boolean`. Proposed: bytes, converted at load/store.
