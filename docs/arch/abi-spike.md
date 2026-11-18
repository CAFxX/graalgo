# ABI spike: reflect/`internal/abi` on the JVM value model (P1) and the syscall boundary (P2)

**Status:** spike report, 2026-11-18, lane `wt/recovery/abi-spike`. It
answers the two prototype questions of
[toolexec-design.md](toolexec-design.md) §12 (P1, P2) behind risks R1 and R2
of its §10. [SPEC.md](SPEC.md) is authoritative; this document proposes
amendments (§4) and decides nothing by itself. All numbers were measured on
go1.27.1 linux/amd64 on this box; anything else is labelled an estimate.

| | Verdict | Mechanism | Effort (agent-days, estimate) |
|---|---|---|---|
| **P1** | **Go** for verbatim `internal/abi` + `internal/reflectlite` + `reflect`. The fallback (150–200 patched functions) is not needed. | Byte-exact gc-layout descriptor images + a layout-addressed pointer model (§1.3) | 14–20 for W9 (was "37–200 intrinsics", unsized) |
| **P2** | see §2 | see §2 | see §2 |

## 1. P1 — verbatim reflect on handle-table / descriptor views

### 1.1 What the source actually does (static inventory)

Tool: [unsafescan](../../test/spikes/abi/unsafescan/main.go) type-checks the
packages from `GOROOT/src` and assigns every occurrence to exactly one
pattern (type-directed, not textual). Columns: `internal/abi`,
`internal/reflectlite`, `reflect`, `sync`, `internal/sync`, `sync/atomic`,
then the sum over all 13 scanned packages (the six named plus `strings`,
`bytes`, `fmt`, `encoding/json`, `sort`, `slices`, `internal/unsafeheader`).

| Pattern | abi | rlite | reflect | sync | i/sync | atomic | all | Mechanism |
|---|---|---|---|---|---|---|---|---|
| `unsafe.Pointer` in type position (no operation) | 13 | 24 | 135 | 8 | 3 | 12 | 197 | none |
| `unsafe.Pointer(&x)` | 3 | 3 | 52 | 19 | 17 | 15 | 113 | (a) |
| `unsafe.Pointer(p)` from a typed pointer | 35 | 15 | 100 | 9 | 6 | 8 | 174 | (a) |
| `unsafe.Pointer(uintptr(p) + n)` | 3 | 1 | 1 | 1 | 0 | 0 | 6 | (a) |
| `unsafe.Pointer(integer)` not derived from a pointer | 1 | 0 | 3 | 0 | 0 | 0 | 4 | (a)¹ |
| `(*T)(p)`, T a type descriptor struct | 28 | 11 | 93 | 0 | 0 | 0 | 132 | (b) |
| `(*T)(p)`, T an interface/slice/string header | 1 | 5 | 31 | 0 | 0 | 0 | 37 | (b) |
| `(*T)(p)`, T scalar or array of scalars | 3 | 5 | 83 | 0 | 0 | 0 | 91 | (a) |
| `(*T)(p)`, T pointer/string/slice/interface/func/map/chan | 1 | 17 | 59 | 3 | 0 | 2 | 82 | (a) |
| `(*T)(p)`, T another struct | 4 | 0 | 0 | 2 | 2 | 12 | 21 | (a)/(b)² |
| `uintptr(p)` | 4 | 1 | 12 | 5 | 0 | 0 | 27 | (a) token |
| `unsafe.Add` / `add()` | 0 | 1 | 29 | 0 | 0 | 0 | 30 | (a)³ |
| `unsafe.Slice/String/SliceData/StringData` | 3 | 2 | 8 | 0 | 0 | 0 | 15 | (a) node kinds |
| `unsafe.Sizeof/Offsetof/Alignof` | 18 | 0 | 2 | 3 | 0 | 0 | 24 | frontend constant |
| atomic op on an `unsafe.Pointer` word | 0 | 0 | 0 | 3 | 0 | 0 | 3 | (a) |
| body-less function (linkname/assembly) | 2 | 7 | 70 | 16 | 8 | 41 | 144 | (c) by definition |
| field read/write on a descriptor struct | 67 | 50 | 449 | 0 | 3 | 0 | 569 | (b) |
| arithmetic past the end of a descriptor struct or into name bytes | 6 | 1 | 0 | 0 | 0 | 0 | 7 | (b) |

(a) = handled by the handle model (object identity + byte offset); (b) =
needs a typed view over synthesized descriptor/header memory; (c) = must be
an intrinsic or a patched function. `strings` has 4 operations (Builder's
`unsafe.String`/`StringData` and its `noescape` self-pointer), `fmt` 1
(`uintptr` of a pointer for `%p`), `slices` 9 (overlap tests), `bytes`,
`sort`, `encoding/json` none.

¹ `abi.go`'s register-bitmap helper and three `(*byte)(unsafe.Pointer(new(uintptr)))`
allocations in type constructors — all derive from a live object.
² 12 of the 21 are `sync/atomic.Value`'s `efaceWords`: an interface header
operated on one word at a time with atomics, with a non-descriptor sentinel
pointer stored in the type word. It needs the interface header view to keep
two raw words; if that proves awkward, `atomic.Value`'s four methods are the
one candidate for a patch outside the call path.
³ 19 of reflect's 29 are in the call-frame code (below).

Per function (declared functions and methods; strongest mechanism any of
its operations needs):

| Package | funcs | no unsafe op | (a) only | (b) | body-less (c) |
|---|---|---|---|---|---|
| `internal/abi` | 83 | 51 | 8 | 22 | 2 |
| `internal/reflectlite` | 68 | 40 | 11 | 10 | 7 |
| `reflect` | 393 | 184 | 59 | 80 | 70 |
| `sync` + `internal/sync` + `sync/atomic` | 205 | 101 | 39 | 0 | 65 |
| `strings`, `bytes`, `fmt`, `encoding/json`, `sort`, `slices` | 574 | 570 | 4 | 0 | 0 |

Two facts decide the design:

1. **The layout-dependent code is small and concentrated.** All arithmetic
   that leaves a descriptor *struct* (trailing `UncommonType`, method array,
   `FuncType` parameter array, name bytes) is in **7 sites in 7 functions**:
   `abi.(*UncommonType).Methods/ExportedMethods`, `(*FuncType).InSlice/OutSlice`,
   `Name.DataChecked/Data` and reflectlite's copy, plus `(*Type).Uncommon`'s
   wide-struct casts. Everything else is 569 plain field selections and 132
   pointer conversions between descriptor struct types. A view that is
   byte-exact makes all of them work without a per-site rule.
2. **reflect's (c) set is the declared runtime contract, not hidden
   coupling.** 70 body-less functions: 34 runtime linknames, 33 `badlinkname`
   self-aliases (resolved by D9 rule 1), 3 assembly. On
   top, one cluster cannot be served by any view because it manufactures gc
   call frames: `reflect/abi.go` (15 functions), `Value.call`, `callReflect`,
   `callMethod`, `methodReceiver`, `storeRcvr`, `funcLayout`,
   `makeMethodValue`, `moveMakeFuncArgPtrs` — 23 Go functions, of which
   **4 need a patch** (`Value.call`, `callReflect`, `callMethod`,
   `makeMethodValue` call an invoke intrinsic with `[]Value`); the other 19
   become unreachable and stay verbatim.

### 1.2 What the existing backend does with the verbatim source (dynamic evidence)

The v1 disk-import path already compiles `reflect`, `internal/reflectlite`
and `internal/abi` **unmodified from `GOROOT/src`** (synthetic reflect was
retired on 2026-10-16) and runs them on an object-graph model: descriptors
are synthesized as nested `GoStruct`s
([AbiTypes.java](../../internal/backend/interop/src/main/java/graalgo/p3reflect/AbiTypes.java))
and each kind of `unsafe` reinterpretation gets a view rule
([PrefixView.java](../../internal/backend/interop/src/main/java/graalgo/p3unsafe/PrefixView.java),
[HeaderView.java](../../internal/backend/interop/src/main/java/graalgo/p3unsafe/HeaderView.java),
[UnsafeToPtrNode.java](../../internal/backend/interop/src/main/java/graalgo/p3unsafe/UnsafeToPtrNode.java)).
About 11,400 lines of Java live in `p3reflect/` and `p3unsafe/`.

[reflectprobe](../../test/spikes/abi/reflectprobe/main.go) asks 126
questions across the reflect API (type headers, names, navigation, methods,
relations, constructors, value reads and writes, maps, calls, package
algorithms), one process per check, gc as oracle
([run.sh](../../test/spikes/abi/reflectprobe/run.sh)). Result on the
integrated tree `8792d9b1`
([recorded output](../../test/spikes/abi/reflectprobe/testdata/v1-result-8792d9b1.txt)):

**76 of 126 checks (60 %) agree with gc.** The 50 failures:

| Failure class | Checks | Typical symptom |
|---|---|---|
| Memory model: byte offset into a nested struct, aggregate copy through an offset, view/representation confusion | 20 | `byte-offset derivation from a non-array referent traps`, `typedmemmove: aggregate store through a byte-offset view`, `ByteRef cannot be cast to GoStruct` |
| Descriptor content missing or wrong (methods, interface methods, `ChanDir`, func parameters, `PkgPath`, `[]byte`) | 18 | `NumMethod` 0, `Implements` always true, `<-chan int` printed as `chan int` |
| Type identity (a descriptor copy is not the canonical one) | 2 | `TypeOf(&x).Elem() != TypeOf(x)` |
| Linkname intrinsic arity drift against go1.27.1 | 3 | `reflect.makemap takes 1 arguments, got 2` |
| Other (`Call`, `MakeFunc`, `Select`, `TypeAssert`, `Slice`, `SetLen`, `IsZero`) | 7 | wrong value or panic |

Reading: the question "can verbatim reflect run on the JVM value model" has
a working existence proof for the easy 60 %, and its failures name the two
things the object-graph approach cannot give: **addressing by byte offset**
(40 % of failures) and **complete, canonical descriptors** (40 %). Both are
properties of the representation, not missing cases; adding view rules one
cast at a time is what produced 11 k lines and still traps on
`v.Field(i).Field(j)`.

### 1.3 Recommended mechanism: layout-addressed pointers over byte-exact descriptors

Three pieces, all consistent with SPEC §3.4/§3.6/§7.3 (handle = object
identity + offset; "exact `rtype`/itab layouts"):

1. **Descriptor images.** For every type the backend builds the descriptor
   gc's compiler would emit: the kind-specific `internal/abi` struct, the
   trailing `UncommonType`, parameter pointers and method array, and the
   separately allocated names, field arrays and interface-method arrays —
   as bytes plus a parallel reference array for the pointer-shaped words.
   `NameOff`/`TypeOff`/`TextOff` are IDs in a process-wide table with gc's
   `reflectOffs` numbering (negative, allocated on demand), which is the
   scheme gc's runtime itself uses for type data outside a module.
   `resolveNameOff`/`resolveTypeOff`/`resolveTextOff`/`addReflectOff` are
   table lookups. The image is built from the frontend's type table alone;
   the offsets of `abi.Type`'s own fields come from `go/types` applied to
   `internal/abi` (§3.4's layout table), so **no offset is hard-coded per Go
   release**.
2. **One pointer model.** An `unsafe.Pointer`, and any `*T` obtained from
   one by conversion, is `(object, byte offset)`. Every addressable object
   kind answers four operations in gc byte offsets — load/store scalar of
   N bytes, load/store pointer word: descriptor images (array reads), flat
   values (offset → slot through a per-type leaf table derived from the gc
   layout), arrays (offset / element size), and the three header shapes
   (interface: type word + data word; string: data + len; slice: data + len
   + cap). A field access through a view pointer is "load at
   `ptr.offset + Offsetof(field)`" — the constant the frontend already
   folds. Arithmetic that leaves the object traps (GG-0016).
3. **A small intrinsic set** — exactly the body-less functions of §1.1 plus
   the four patched call functions.

Why this and not the two alternatives:

- *Object-graph views (v1):* measured above; does not converge.
- *Patch `rtype`/`Value` accessors into intrinsics (the §5.6 fallback):*
  the per-function table says 80 reflect functions need a view and 59 only
  the handle model; replacing them means re-implementing reflect's semantics
  (flag propagation, RO rules, panics with gc's messages) in Java, by hand,
  re-audited every release. The image builder is ~300 lines against a
  layout table and is checked mechanically against gc (below).

### 1.4 Prototype

[test/spikes/abi/descview/](../../test/spikes/abi/descview/) (standalone
Java + Go; no Truffle, no JVM flags; `run.sh` takes about a minute):

| Piece | Role |
|---|---|
| [fixture/fixture.go](../../test/spikes/abi/descview/fixture/fixture.go) | 25 root types (58 with their closure): structs with tags, embedding, unexported fields, methods on value and pointer receivers; pointers, slices, maps, arrays, directional channels, variadic funcs, anonymous structs, named and unnamed interfaces, basic types |
| [typetab](../../test/spikes/abi/descview/typetab/main.go) | the frontend's role: `go/types` + `types.SizesFor("gc","amd64")` → type table (kind, size, alignment, pointer bitmap, gc type string and link string, fields, method sets, flags) and the layout of `internal/abi`'s own structs. Never looks at compiler output |
| [Images.java](../../test/spikes/abi/descview/java/abispike/Images.java) | builds the descriptor images (205 blobs, 6,179 bytes for the 58 types) |
| [Mem.java](../../test/spikes/abi/descview/java/abispike/Mem.java), [Values.java](../../test/spikes/abi/descview/java/abispike/Values.java) | the pointer model; flat values (`long[]` + `Object[]`, SPEC §3.4) addressable by gc offset; interface/string/slice header views |
| [Abi.java](../../test/spikes/abi/descview/java/abispike/Abi.java), [Reflect.java](../../test/spikes/abi/descview/java/abispike/Reflect.java) | go1.27.1 `internal/abi` and `reflect` functions transliterated statement by statement onto the pointer model, each quoting its Go source: `Kind`, `Size`, `Elem`, `Key`, `Len`, `ChanDir`, `Uncommon`, `ExportedMethods`, `InSlice/OutSlice`, `Name.ReadVarint/Name/Tag/IsExported/IsEmbedded`, `rtype.String/Name/PkgPath/NumMethod`, `unpackEface`, `Value.Elem/Field/Index/Int/Uint/Float/Bool/String/Len/SetInt/SetFloat` |
| [imgcheck](../../test/spikes/abi/descview/imgcheck/main.go) | **gc's own unmodified reflect as the judge**: loads the Java-built images into real (mmap'd) memory, relocates pointers and registers offsets through `reflect.addReflectOff`, wraps each descriptor as a `reflect.Type`, and compares reflect's answers with its answers for the compiler-emitted descriptor of the same type; values are read through `reflect.NewAt(synthesizedType, &realValue)` |

Results ([run.sh](../../test/spikes/abi/descview/run.sh)):

- **607 / 607** answers of gc's reflect are identical on synthesized and on
  compiler-emitted descriptors: kind, size, alignments, `String`, `Name`,
  `PkgPath`, `Comparable`, fields (name, type, offset, tag, embedded,
  exported, `PkgPath`, `FieldByName` index paths, `VisibleFields`), methods
  (names and `Method(i).Type`, which goes through `FuncOf` over synthesized
  parameter descriptors), interface methods, `Elem`/`Key`/`Len`/`ChanDir`,
  func parameters and variadic bit, `PointerTo`, `SliceOf`, and 21 value
  reads/writes including nested fields, an embedded struct, an array
  element, `SetInt`/`SetFloat` through a pointer and the unexported-field
  panic.
- The fixed `abi.Type` header is **byte-identical to gc's for 25 / 25 root
  types in every field**: `Size_`, `PtrBytes`, `Hash` (SHA-256 of the link
  string as in `cmd/compile`), `TFlag` (ignoring gc's on-demand GC-mask
  bit), `Align_`, `FieldAlign_`, `Kind_`, `Equal` nil-ness.
- The transliterated code on the Java pointer model produced **427 answers,
  all identical to gc's** (the subset of keys it covers), and an
  out-of-object offset traps.
- Failability: a single corrupted field offset in the images is caught by
  gc's reflect (11 mismatches).
- Cost, indicative only (plain JVM JIT, unoptimized prototype, megamorphic
  interface calls, no Truffle): `ValueOf(x).Field(1).Int()` 33 ns against
  gc's 1.6 ns; `t.String()` 23 ns against 3.3 ns; `t.Kind()+t.Size()` folds
  to nothing on both. Image size: 107 bytes per type on average.

How the prototype routes the verbatim source: the **source-level** half
(real `reflect` compiled and run) is the v1 experiment of §1.2 — it is the
only compile path that executes today (the toolexec skeleton does not run
on the JVM, [toolexec-design.md](toolexec-design.md) §13). The
**mechanism** half is standalone because v1's nested-`GoStruct` model is
exactly what has to change, and v1 is scheduled for deletion (D11).
Transliteration by hand is the gap between the two; it is mechanical (every
Go statement maps to one model operation) but it is not a compiler run.

### 1.5 Fraction verbatim, and what is not

| | Functions | Verbatim | Patched | Intrinsic (body-less upstream) |
|---|---|---|---|---|
| `internal/abi` | 83 | 83 | 0 | 2 (`FuncPCABI0`, `FuncPCABIInternal`) |
| `internal/reflectlite` | 68 | 68 | 0 | 7 |
| `reflect` | 393 | 389 | **4** (`Value.call`, `callReflect`, `callMethod`, `makeMethodValue`) | 70, of which 37 need Java code (34 linknames + 3 assembly → 1 invoke intrinsic) |
| `sync`, `internal/sync`, `sync/atomic` | 205 | 205 | 0 (the `sync.Pool.indexLocal` patch of §5.5 is unnecessary: it is in-array arithmetic) | 65 |

So **99 % of reflect's functions (389 / 393) and 100 % of `internal/abi`
and `internal/reflectlite` stay verbatim**; the fork for this group is one
diff of 4 functions. The measured 60 % (§1.2) is what the *current*
representation delivers; the prototype shows the missing 40 % is reachable
without touching the Go source.

Not demonstrated by the prototype (sized in the estimate, in decreasing
risk):

1. **Descriptors made at run time by reflect** (`StructOf`, `FuncOf`,
   `MapOf`, `ArrayOf`, `SliceOf`, `ChanOf`, `PointerTo`). Verbatim reflect
   allocates an `abi.StructType` as an ordinary Go value and fills it; when
   such a pointer reaches the runtime (`Value.Interface`, `unsafe_New`,
   `typedmemmove`, map intrinsics) the backend must read the descriptor
   *through the same pointer model* and intern a backend type for it — the
   inverse of the image builder. imgcheck exercised `PointerTo`, `SliceOf`
   and `FuncOf` only on the gc side. Alternative: patch the constructors'
   allocate-and-fill tails into one "intern this shape" intrinsic (7 more
   patched functions). Recommendation: inverse reader first; patch only if
   map group layouts (`MapOf` builds the Swiss-table group struct) make it
   expensive.
2. **Writes through header views**: `packEface` stores the two words of an
   interface separately; `sync/atomic.Value` does it with atomics and a
   sentinel in the type word; `Value.SetLen` writes a slice header word.
   Each needs the addressed variable to tolerate a half-written header.
   Mechanical for cells; decide whether locals whose address reaches
   `unsafe.Pointer` are boxed as two/three raw words (frontend already flags
   address-taken locals).
3. **`typedmemmove`/`typedmemclr`/`typedslicecopy` over the leaf tables**
   (aggregate copies between two addresses). v1 fails exactly here; with
   leaf tables it is a loop over leaves.
4. **`uintptr` tokens** (27 sites): `Value.Pointer`, `UnsafeAddr`, `%p`,
   alignment tests, overlap tests in `slices`. Token table per spike 4;
   `uintptr(p) % align` must be answered from the offset.
5. **Map iteration types**: reflect's `MapIter` embeds
   `internal/runtime/maps.Iter`; the facade package (W5) must provide it.
6. **Performance under Truffle**: the indicative 15–20× over gc is before
   any inline caching; reflect-heavy code (`fmt`, `encoding/json`) will sit
   on this path. Needs a PE-friendly `Addressable` dispatch (one cached
   class per call site) — measure in W9's first week.

### 1.6 Effort (estimate)

| Work | Agent-days |
|---|---|
| Descriptor image builder in the backend from GIR `TYPES`/`TYPEDEFS`, incl. itab and code-pointer (`TextOff`) tables, GC masks, map type fields; imgcheck promoted to a gate over a 300-type corpus | 3–4 |
| Pointer model in the node classes: view pointers for `(*T)(unsafe.Pointer)`, offset → slot leaf tables for structs/arrays, header views with stores, traps | 4–6 |
| Runtime intrinsics over the model: `typedmemmove` family, `unsafe_New*`, `ifaceE2I`, `resolve*Off`, `addReflectOff`, `typelinks`, map/chan helpers (the 34 + 7 linknames) | 3–4 |
| Call path: invoke intrinsic + the 4-function patch; `MakeFunc`, method values | 2–3 |
| Inverse descriptor reader (run-time-constructed types) | 2–3 |
| **Total W9** | **14–20** |

This replaces the "15–30" of milestone M1d's reflect share and removes the
150–200-function fallback from the plan. The pointer-model item is shared
with W8 (P2 marshals the same handles) and with `sync`/`strings`.

### 1.7 How to rerun

```
export PATH=~/tools/go/bin:~/tools/graalvm-jdk-25/bin:$PATH JAVA_HOME=~/tools/graalvm-jdk-25
(cd test/spikes/abi/unsafescan && go run . [-funcs] [pkg ...])     # §1.1 tables, ~40 s
test/spikes/abi/descview/run.sh /some/scratch/dir                   # §1.4, ~1 min
make build graalgo && test/spikes/abi/reflectprobe/run.sh /some/scratch/dir   # §1.2, ~10 min, one JVM at a time
```

## 2. P2 — the syscall boundary

(Added by the second commit of this lane.)
