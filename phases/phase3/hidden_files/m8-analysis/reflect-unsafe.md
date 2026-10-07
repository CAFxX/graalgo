# P3-M8 analysis: synthetic bring-up of `unsafe` and `reflect`

Worker 5/5, 2026-10-07. Read-only analysis; no tree modifications.
Vendored source of truth: `~/workspace/graalgo-stdlib/go1.27.1` (go1.27.1).
All gc behaviors below were verified against `~/tools/go/bin/go` (go1.27.1)
unless marked.

## 0. Verdicts (summary)

1. **unsafe: FULL** — all 9 members real, with exactly one deliberate
   fail-fast: `Add` traps (D5/GG-0015: no observable addresses; handles
   admit no byte arithmetic). `Pointer→uintptr` and `uintptr→Pointer`
   likewise trap — already the D5 decision
   (`GoConvert.unsafePointerToUintptr`).
2. **reflect: FULL with a precise fail-fast list** — the full `Type`
   read API, the full `Value` read/write API (incl. `Call`/`Method`/
   `MakeFunc`/`Select`), `DeepEqual`, `Swapper`, `VisibleFields`,
   `MapIter` (minus `Seq`), `StructTag`. Fail-fast only where a
   named missing capability blocks: the `iter`-family, the type
   constructors (`ArrayOf`…`StructOf`), `TypeAssert[T]` (generics),
   and the `uintptr`-leaking accessors (D5 traps). Rationale §3.
3. The wire type table **already carries everything reflect needs**
   (struct field name/type/tag/embedded/byte-offset, method tables,
   signatures, map key/elem, chan dir) — the missing piece is a
   *runtime* rtype table built from it, plus ~35 small intrinsics,
   each mapping to an existing Java capability. No deep new runtime
   subsystem is required except the rtype table itself and two
   call trampolines.

---

## 1. unsafe — full API inventory

go1.27.1 `unsafe` has 9 members: `Pointer`, `Sizeof`, `Offsetof`,
`Alignof`, `Slice`, `SliceData`, `String`, `StringData`, `Add`
(`go doc unsafe`).

### 1.1 Why it cannot be a plain synthetic Go source

Two facts force frontend support (both verified):

- (a) `type Pointer *ArbitraryType` (the doc trick) breaks
  conversions under `go/types`: `*T → *int` is an illegal conversion,
  so user code `unsafe.Pointer(p)` would not type-check. The
  frontend already anticipates the real solution: `basicClass`
  maps `types.UnsafePointer` to class `"pointer"` (export.go:1984),
  and `GoKind` maps `"pointer" → UNSAFE_POINTER` (GoKind.java:186,
  "only unsafe.Pointer has basic class pointer"). So the synthetic
  `unsafe` package must expose `Pointer` as an **alias to
  `types.UnsafePointer`**, constructed programmatically in
  `syntheticUnsafePackage()` (export.go ~643, today the empty
  package). Conversions then type-check natively.
- (b) `Sizeof`/`Alignof`/`Offsetof` are **constant expressions** in
  gc: `const x = unsafe.Sizeof(int(0))` and
  `const off = unsafe.Offsetof(S{}.b)` both build under go1.27.1
  (verified), and are usable as array lengths. [SUPERSEDED by RULING.md
  R11-supersede (2026-10-07): go/types folds these to constants itself;
  the implemented design uses *types.Builtin from types.Unsafe with no
  pre-pass.] A declared Go func
  can never be constant under `go/types`, so the exporter needs a
  **const-context pre-pass**: two-pass type-check in `ExportFile`
  (pass 1 with a collecting `Config.Error` handler; rewrite matched
  `unsafe.Sizeof/Alignof/Offsetof` call sites to `BasicLit`; pass 2
  clean). Runtime-context calls fold at export time from
  `types.Info` directly — no pre-pass needed there. The operand is
  never evaluated (verified: `unsafe.Sizeof(boom())` with
  `boom` panicking prints `8`, no panic) — folding is faithful.

### 1.2 Per-member mechanism

| Member | Mechanism | Notes |
|---|---|---|
| `Pointer` | **frontend type**: alias to `types.UnsafePointer` in the programmatically-built synthetic package | Conversions `*T↔Pointer`: new `ConvertNode` combos → new intrinsics `__unsafe_fromptr` (`UnsafeHandles.alloc`) / `__unsafe_toptr` (resolve → `GoPointer`). `Pointer→uintptr`: keep the existing D5 trap (`ConvertNode.java:86-87` → `GoConvert.unsafePointerToUintptr`). `uintptr→Pointer`: accept in type-check (gc-legal), **trap at runtime** (new `GoConvert` trap mirroring the existing one; today `checkCombo` would throw `IllegalArgumentException` = frontend crash, must become a Go-visible trap). |
| `Sizeof(x)` | **frontend intrinsic**: fold to `uintptr` literal via `types.Sizes.Sizeof` (full type coverage — unlike M4's flat-only `structSizeBytes`) | Const-context via the §1.1(b) pre-pass. `Sizeof` of a bare type parameter → named scope error (`gcSizes` asserts; cf. the M6 `sizesWouldPanic` note in export.go). |
| `Offsetof(s.f)` | **frontend intrinsic**: selector → field index via `types.Info.Selections` → wire struct entry `"offset"` (export.go:2225 emits real `sizes.Offsetsof`, `-1` only for generic/deep structs) | If the wire offset is `-1` → **named scope error at export** (never emit `-1`). Non-selector operand → scope error (gc rejects too). |
| `Alignof(x)` | **frontend intrinsic**: `types.Sizes.Alignof` → literal | Same const handling as `Sizeof`. |
| `Slice(ptr, len)` | **call-site desugar** (mrdesugar precedent, cf. `synth_json.go` `Marshal→__marshal`) to wire node `unsafeslice{ptr,len,elemType}`; **runtime intrinsic** | Generic signature `Slice[T any]` needs no synthetic generics: the exporter reads `T` from `types.Info` at the call site (same trick as the multi-result desugar). Runtime: resolve handle → referent; construct `GoSlice`. Supported referent shapes: whole-array backing, existing `GoSlice` (sub-slice), single `T` with `len==1` — anything else fails fast (loud, never silent). The new slice retains the referent (GC-safe by construction). |
| `SliceData(s)` | **runtime intrinsic** | Nil slice → nil `Pointer` (handle 0). Else intern a handle for the backing array (see §1.3 interning — required for `==` parity). |
| `String(ptr, len)` | **runtime intrinsic** (`unsafestring` wire node) | **Accepted divergence DIV-US-1**: copies into a fresh `GoString`. `GoString` is defensively immutable (`bytes()` clones); sharing the caller's array would break the invariant the whole runtime relies on. Writes through the original pointer after `String()` are therefore *not* visible in the string (gc aliases). Documented, never silent. |
| `StringData(s)` | **runtime intrinsic** | Intern a handle for the string's backing `byte[]`. Empty string → nil (gc-faithful: `StringData("")` == nil, verified go1.27.1). |
| `Add(ptr, off)` | **fail-fast trap** | D5/GG-0015: handles are opaque, never raw addresses; byte arithmetic on them is meaningless in the longs/objects carrier model. Trap with `UnsafeEscapeException`, same as the existing conversion traps. Re-test trigger: only if graalgo ever moves to a byte-addressed value model (no such plan). |

### 1.3 Handle interning (required for `==` parity)

`UnsafeHandles.alloc` mints a fresh handle per call. Then
`p1 := unsafe.Pointer(&x); p2 := unsafe.Pointer(&x); p1 == p2` is
**false** on graalgo, **true** under gc — a silent divergence in the
most basic unsafe idiom. Fix: intern handles — `IdentityHashMap`
referent→handle for object referents, value-equality for `Ref`
holders (§2.3). Freed handles are still never reused (keeps the
D5 use-after-free guarantee). Bonus: interning makes `Value.ptr`
stable per referent, which is what `DeepEqual`'s cycle detection
keys on (§2.8) — no separate identity intrinsic needed.

### 1.4 What "making unsafe REAL" touches

1. `syntheticUnsafePackage()` (export.go ~643): build the package
   programmatically — `Pointer` alias + 8 `*types.Func` objects
   with gc-exact signatures (for `Slice`/`SliceData`, build the
   generic `*types.Signature` with `types.NewTypeParam`; `len`
   params typed `any` + exporter integer-kind check, since a union
   constraint would add a second type parameter and break explicit
   `unsafe.Slice[byte](p, n)` instantiation).
2. `ExportFile` import gate: allow non-blank `import "unsafe"`.
3. `isSyntheticIntrinsic("unsafe", …)` + `exportUnsafeIntrinsic`
   (synth_machinery.go pattern, cf. `exportTimeIntrinsic`).
4. `ConvertNode`: `POINTER↔UNSAFE_POINTER` combos; `UINT64→UNSAFE_POINTER`
   runtime trap.
5. Java: `__unsafe_fromptr/toptr/slice/slicedata/string/stringdata`
   (6 small methods reusing `UnsafeHandles`, `GoSlice`, `GoString`).

Re-test triggers: none open — this is the complete unsafe surface.

---

## 2. reflect — API surface by area and implementability

Notation: **S** = pure synthetic Go on the intrinsic set, **J** = new
Java (runtime) work, **I** = wire-node intrinsic (exporter
interception, `isSyntheticIntrinsic` pattern), **F** = fail-fast with
trigger.

### 2.1 `Type` area

The runtime already has: `GoTypeDescriptor` (name/kind only —
*insufficient*), `GoStructDescriptor` (field names/kinds/carrier
indices — no tags, no types, no offsets), structural func-type
keys (commit `720731d`: params/results/variadic), `Intrinsics.itabFor`
+ the P3-M3 method tables. The **wire type table** has everything
else: struct fields `{name, type, tag, embedded, offset}`
(export.go:2225), named `{name, underlying, methods[]}`
(export.go:2077), pointer `{elem, methods}`, signature
`{params, results, variadic}`, array `{len, elem}`, slice `{elem}`,
map `{key, elem}`, chan `{dir, elem}`, interface `{methods[]}`
(export.go:2242).

**Design (J):** new `GoRType`, built from the wire `TypeTable` at
load, canonical per `GoTypeDescriptor` (fill a
`Map<GoTypeDescriptor, GoRType>` inside `typeDescriptorFor`,
NodeFactory.java:877 — the same canonicalization that makes
`TypeOf(x) == TypeOf(y)` hold). Synthetic Go mirrors gc:
`type rtype struct { tok unsafe.Pointer }` implementing the `Type`
interface; every method is **S** over one token intrinsic:

| Member | How |
|---|---|
| `Kind/Name/PkgPath/String/Size/Align/Bits/ChanDir/Len/NumField/NumMethod/NumIn/NumOut/IsVariadic/Comparable` | **I** scalar intrinsics on the token (`__reflect_tKind` …). `String()` rendering in Java from wire entries (incl. struct tags, `*pkg.Name` for ptr-to-named per the M7 `*qual.Name` rule). |
| `Field/FieldByIndex/FieldByName/FieldByNameFunc` | **I** `__reflect_tField(tok,i) StructField` (Java assembles the public struct); `ByName/ByNameFunc` **S** loops (gc does the same). |
| `Method/MethodByName` | **I** `__reflect_tMethod(tok,i) Method`; `ByName` **S** loop. |
| `Elem/Key/In/Out` | **I** token→token. |
| `Implements/AssignableTo/ConvertibleTo` | **J** on `GoRType` (method-set compare reusing the P3-M3 `implkey` machinery; conversion rules reimplemented — bounded, mirrors `ConvertNode`'s combos). `CanConvert` = `ConvertibleTo` (**S**). |
| `OverflowInt/Uint/Float/Complex` | **S** (bits from token). |
| `Methods/Fields/Ins/Outs/CanSeq` | **F** — need the `iter` package (not in v1 scope). Re-test: iter bring-up. |
| `ArrayOf/SliceOf/MapOf/ChanOf/FuncOf/PtrTo/StructOf` | **F** — need runtime type *synthesis* (new `GoTypeDescriptor`+`GoRType` for types absent from the wire table, plus value allocation against them). Re-test: type-synthesis milestone. Nothing in encoding/json, fmt, or text/template needs them. |

### 2.2 `Value` construction

Mirror gc's layout in synthetic Go:
`type Value struct { typ unsafe.Pointer // rtype token; ptr unsafe.Pointer // Ref handle (§2.3); flag uintptr // flagRO|flagAddr }`.

| Member | How |
|---|---|
| `TypeOf/ValueOf` | **I** `__reflect_typeof(v any)`, `__reflect_valueof(v any) Value` (Java boxes from `GoInterface{descriptor,data}`; `flag=0`, unaddressable — mutations forbidden by flag checks, so aliasing the interface data is unobservable). |
| `New/Zero` | **I** `__reflect_new(tok) Value` (addressable zero; Java allocates per kind — `GoStruct` via the rtype's struct descriptor, `GoSlice` via existing `makeslice`, etc.). `Zero` = `New(tok).Elem()` (**S**). |
| `NewAt` | **I** — `Value{typ:tok, ptr:p, flag:flagAddr}` directly over the caller's handle. Needs real `unsafe` (§1) — the two bring-ups compose. |
| `Indirect` | **S** (Kind+Elem loop, gc-identical). |
| `TypeAssert[T]` | **F** — needs generics instantiation in synthetic scope (same blocker as `errors.AsType`, synth_errors.go). Re-test: synthetic generics. |

### 2.3 The `Ref` holder (the one new Java concept)

`Value.ptr` is an interned handle (§1.3) to a Java `Ref`:
`ObjRef(Object)` (whole value), `StructFieldRef(GoStruct, idx)`,
`ArrayElemRef(Object array, idx)`, `SliceElemRef(GoSlice, idx)`,
`MapElemRef` (not addressable — gc same), each with
`load()/store(Object)`. All field/element navigation in Java
resolves through the carrier model in exactly one place; synthetic
Go never does address arithmetic. Handles are interned by
(referent-identity) / (base,index)-equality → `==` parity and
stable `DeepEqual` cycle keys.

### 2.4 `Value` navigation & scalars

| Member | How |
|---|---|
| `Field/FieldByIndex/FieldByName/FieldByNameFunc/FieldByIndexErr` | **I** `__reflect_field(v,i) Value` (sets `flagRO` when the wire field name is unexported — `unicode.IsUpper` on first rune, gc rule); the rest **S** loops. |
| `Index` | **I** `__reflect_index(v,i) Value` (array/slice/string; addressable iff base addressable). |
| `MapIndex/MapKeys/MapRange` (+`MapIter.Key/Value/Next/Reset`) | **I** `__reflect_mapIterInit/Next/Key/Val` over the existing `Intrinsics.mapIterInit/mapIterNext` (M4 audit: REAL); `MapIndex`/`MapKeys` **S** over the iterator. `MapIter.Seq` **F** (iter). |
| `Elem` | **I** (ptr→`GoPointer.referent`; interface→data). |
| `Addr/CanAddr` | **S** flag check + **I** rewrap with `flagAddr`. |
| `Len/Cap/IsNil/IsValid/IsZero` | **I** on the holder (+ token for `IsZero`'s type-directed zero). |
| `Int/Uint/Float/Complex/Bool/String/Bytes` | **I** per-kind-group loads (`__reflect_getInt` … — reads the longs/objects carrier regardless of *named* type; kind check + RO/panic messages in **S**, gc-exact). |
| `Set/SetInt/SetUint/SetFloat/SetComplex/SetBool/SetString/SetBytes/SetPointer/SetZero` | **I** stores (flag checks in **S**). `Set` = assignability check (**S** via `__reflect_assignableTo`) + holder store. |
| `SetMapIndex` | **I** via `Intrinsics.mapAssign/mapDelete` (REAL per M4). |
| `Interface/CanInterface` | **I** `__reflect_interface(v) any` (RO→ panic, gc-exact message — **S** check); boxes holder-loaded data with the rtype's descriptor. |
| `Convert` | **I** applying `ConvertNode`/`GoConvert` semantics to the holder value. |
| `Slice/Slice3` | **I** via `GoSlice.slice/slice3` (exist). |
| `Pointer/UnsafeAddr/InterfaceData` | **F-trap** — return `uintptr`; D5 forbids address escape (`UnsafeEscapeException`, same as `unsafePointerToUintptr`). `UnsafePointer()` is **I** (returns the handle — no escape). |
| `Grow/SetLen/SetCap/Clear` | **I** small (slice-header/mutation helpers; `GoSlice` needs additive setters or the intrinsic reaches in). |
| `Seq/Seq2/Fields/Methods/CanSeq` | **F** (iter). |

### 2.5 Call / methods / MakeFunc / Select (highest-risk area, all IN)

| Member | How |
|---|---|
| `Value.Call/CallSlice` | **I** `__reflect_call(fn Value, args []Value) []Value`: unpack holders → D5 args → existing `GoCalls.invoke` → repack results as `Value`s. Nil-func → gc's nil-dereference panic. |
| `Value.Method/MethodByName/NumMethod` | **I** `__reflect_methodVal(v,i) Value`: Java builds a **bound-method trampoline** — a `GoFunc` whose `RootCallTarget` prepends the receiver and delegates to the method target. Needs an objId→call-target registry in `NodeFactory` (the wire `"methods"` entries already carry `"obj"`; the itab machinery maps names→targets — the intrinsic needs the same for (type, index)). |
| `MakeFunc` | **I** `__reflect_makeFunc(tok, fn func([]Value) []Value) Value`: Java trampoline converting D5 args → `[]Value` (rtypes from the signature entry), invoking the `GoFunc` via `GoCalls.invoke`, converting back. Panics propagate as `PanicException` (gc: panic in the callback propagates through `Call`). |
| `Select/Send/Recv/TrySend/TryRecv/Close` (+`MakeChan`) | **I** mapping `SelectCase{Dir,Chan,Send}` onto `ChanOps.SelectCase[]` + existing `SelectOp`/`ChanOps` (P2, REAL). `MakeChan` lowers to the existing `make` wire node (`MakeNode`). |
| `Append/AppendSlice` | **I** `__reflect_append(v, []Value)` (growslice + element sets). |
| `Copy` | **S** via Len/Index/Set (slices only) — semantically identical to gc's memmove version. |
| `Swapper` | **S** via Index/Set (gc's own `swapper.go` is reflect-based too). |

### 2.6 `DeepEqual`, `VisibleFields`, misc — **S**

- `DeepEqual`: adapt gc's `deepequal.go` (239 lines, **zero**
  linknames — verified by grep) to the synthetic `Value` API.
  Cycle detection keys `(typ tok, ptr handle)` — stable thanks to
  §1.3 interning. Func semantics (`both nil → true`, else false),
  NaN, unexported-field readability (intrinsics bypass RO —
  `DeepEqual` must, like gc) all expressible.
- `VisibleFields`: **S** (embedded traversal over `FieldByIndex`).
- `StructTag.Get/Lookup`: **S** (pure string parsing).
- `Value.String()` / `Kind.String()`: **S** over token `String()`.

### 2.7 `internal/reflectlite` (fmt, sort need this)

Public surface is tiny: `TypeOf`, `ValueOf`, `Swapper`, plus the
`Type`/`Value`/`Kind` types with a small method set (verified).
**S** adapter over synthetic `reflect`'s *exported* API
(`reflect.Type` satisfies `reflectlite.Type` by identical method
sets). None of the linkname targets in the M6 trial table
(`internal/reflectlite.chanlen/ifaceE2I/maplen/resolveNameOff/…`)
are needed — the adapter never touches `abi.Type`. Register as
synthetic package path `"internal/reflectlite"` (the overlay
importer keys on path strings; the `internal/` visibility rule is
a toolchain rule, not enforced by `go/types`).

---

## 3. Proposed scope: FULL with named fail-fasts

**Recommend FULL**, not a subset. Reasons:

1. `fmt` imports real `reflect` (fmt/print.go:11) — the D4 seven-function
   fmt stub can only graduate to real fmt on top of a working
   reflect read API (`TypeOf/ValueOf/Kind/Field/Elem/Interface/
   MethodByName…`). A subset that omits any of these strands the
   fmt bring-up.
2. `encoding/json` decode (the M7 synthetic json's stated P3-M8
   re-test: "need reflect" at synth_json.go:1889/2081) needs the
   *write* API too: `New`, `Elem`, `Set*`, `MakeSlice`, `MakeMap`,
   `Indirect`, `Addr`, struct tags. A read-only subset does not
   unblock it.
3. `text/template` (the third big consumer) needs `MethodByName`+
   `Call`, `MapIndex`, `IsNil`, `Interface` — i.e. the areas a
   subset would most plausibly cut.
4. The marginal cost of the "hard" areas (`Call`, method values,
   `MakeFunc`) is two trampolines on existing machinery
   (`GoCalls.invoke`, objId→target registry) — not a new subsystem.
5. Precedent: `errors.As/AsType` and the json fail-fasts already
   use the "exact gc signature + fail-fast body + re-test trigger"
   pattern (synth_errors.go). The fail-fast list below follows it.

### 3.1 Fail-fast list (exact)

| Member(s) | Fail-fast message shape | Re-test trigger |
|---|---|---|
| `Type.Methods/Fields/Ins/Outs/CanSeq`, `Value.Methods/Fields/Seq/Seq2`, `MapIter.Seq` | `reflect: <name>: not in M8 scope (needs iter)` | `iter` package bring-up |
| `ArrayOf/SliceOf/MapOf/ChanOf/FuncOf/PtrTo/StructOf` | `reflect: <name>: not in M8 scope (needs runtime type synthesis)` | type-synthesis milestone |
| `TypeAssert[T]` | `reflect: TypeAssert: not in M8 scope (needs generics in synthetic scope)` | synthetic generics (cf. `errors.AsType`) |
| `Value.Pointer/UnsafeAddr/InterfaceData`, `unsafe.Add`, `uintptr→unsafe.Pointer`, `unsafe.Pointer→uintptr` | trap (`UnsafeEscapeException`, D5) | byte-addressed value model (no plan; intentional) |

Everything else in §2 is IN, with exact gc 1.27.1 signatures.

---

## 4. Linkname/asm triage for reflect

**asm (all 13 files: `asm_*.s`, `float32reg_*.s`):** NOT NEEDED.
The M6 inventory already records the decision: "reflect is
reimplemented on Truffle in the wave-2 bring-up — per-file strategy
then (re-test trigger: reflect wave)". This analysis discharges it:
`makeFuncStub`→`__reflect_makeFunc` trampoline,
`callReflect`→`__reflect_call` via `GoCalls.invoke`,
`ifaceE2I`→`__reflect_interface` boxing. The `.s` files stay
fail-fast in the importer.

**Runtime linknames std reflect needs (all have graalgo analogs):**

| std linkname | graalgo analog |
|---|---|
| `runtime.reflectcall` (`call`, value.go:3837) | `GoCalls.invoke` (M5, REAL) |
| `runtime.mapassign` etc. (value.go:3785, map.go) | `Intrinsics.mapAssign/mapAccess/mapDelete/mapLen` (M4, REAL) |
| `runtime.zeroVal` (value.go:3190) | `__reflect_new` zero-manufacturing (§2.2) |
| `runtime.getStaticuint64s` (type.go:1209, tostring) | Java `String()` rendering from wire entries (§2.1) |
| `addReflectOff`/`add`/`rtypeOff`/`resolve*Off` (type.go) | not needed — offsets/names come from the wire table directly |
| `compiledTypelinks`/`typesByString`/`typelinks` | not needed — the wire type table *is* the type registry |
| `adjustAIXGCDataForRuntime` | AIX-only; not needed |

**Reverse linknames (others → reflect):** `badlinkname.go`'s
`reflect.(*rtype).*` / `reflect.(*Value).pointer` and
`internal/reflectlite.*` targets exist for third-party packages
(goccy/go-json et al.) that reach into gc's memory model — a
synthetic reflect exposes **none** of these symbols (no `rtype`
struct, no `abi.Type`); such linknames fail at linkname-resolution
time with the existing ADR-0004 named error. This is correct:
those packages are inherently coupled to gc's allocator and can
never work on the handle model. `internal/reflectlite` itself is
replaced by the §2.7 adapter.

---

## 5. Hardest behavioral risks

### unsafe
1. **(R-US-1) `Slice`/`String` referent-shape dispatch.** gc accepts
   any in-bounds pointer; the handle model only knows whole
   referents. The intrinsic must fail fast on shapes it cannot
   represent (e.g. interior pointers that could only arise via the
   trapping `Add` — unreachable — or via `reflect.NewAt`). Risk:
   an over-permissive shape silently aliasing the wrong object.
   Mitigation: closed shape enumeration + red-green probes per
   shape + differential corpus §6 pinning each accepted shape.
2. **(R-US-2) Handle interning vs. D5 lifetime.** Interning (§1.3)
   keeps one handle per referent forever (strong refs) — matches
   gc's "live unsafe.Pointer keeps the object alive", but a program
   churning `SliceData` in a loop pins unbounded memory where gc
   would free. No silent wrongness, but a memory-retention
   divergence to document; consider weak interning if it bites.
3. **(R-US-3) `String` copy divergence (DIV-US-1).** Post-`String()`
   writes through the original pointer are invisible to the
   string. Deterministic and documented, but it *is* observable —
   corpus must pin the exact semantics (copy-at-call), and any
   future `unsafe.String`-heavy stdlib bring-up (e.g. `strings`
   internals) must be audited against it.

### reflect
1. **(R-RF-1) Unexported-field flagRO parity.** gc panics with
   exact messages on `Interface()`/`Set()` through unexported
   fields (`reflect: reflect.Value.Interface: cannot return value
   obtained from unexported field or method`), while `DeepEqual`
   and `fmt` *can* read them. The flag must propagate through
   `Field`/`Elem`/`Index`/`Method` chains and be checked at
   exactly the gc check sites. Risk: a missed propagation = silent
   encapsulation break; an over-eager one = spurious panics in
   `fmt`/`json`. Mitigation: mirror gc's `flagRO`/`mustBeExported`
   sites one by one; corpus §6 pins both directions.
2. **(R-RF-2) Method-value and `MakeFunc` trampolines.**
   `__reflect_methodVal` needs (type, method-index)→call-target;
   the wire `"methods"` entries carry `"obj"` ids but `NodeFactory`
   must retain an objId→`RootCallTarget` registry (new). The
   `MakeFunc` trampoline re-enters Go via `GoCalls.invoke` —
   panic translation (`PanicException` vs `GoErrorValue`), argument
   count mismatches, and nil-receiver handling must match gc.
   This is the least mechanical part of the bring-up; red-green
   per trampoline before any corpus.
3. **(R-RF-3) `Implements`/`AssignableTo`/`ConvertibleTo`
   soundness.** `Implements` reuses the P3-M3 `implkey` method-set
   machinery (name+structural-signature — names alone are unsound,
   per the R1 P0-1 note in export.go); `ConvertibleTo` reimplements
   gc's conversion lattice in Java. Risk: a wrong `ConvertibleTo`
   makes `Value.Convert` accept/reject conversions gc wouldn't —
   silent type-system divergence. Mitigation: differential corpus
   over the full conversion matrix (named/unnamed × all kinds).

---

## 6. Differential corpus sketches (deterministic only)

Conventions per `corpus/wave1/README.md`: no pointers/addresses in
output, `.panic` files for panic programs (first stderr line
byte-compared), `.scopelimited` with `retest:` where a program
cannot pass the frontend.

### unsafe (10)
1. `00_sizeof_table`: `Sizeof`/`Alignof` over a padding struct,
   nested struct, `[3]int64`, `string`, `[]byte`, `map`, `chan`,
   `func()`, `*int`, `any`; plus `const _ = unsafe.Sizeof(int(0))`
   in const position and as an array length.
2. `01_offsetof`: offsets incl. field after embedded struct,
   trailing field; `unsafe.Offsetof(S{}.b)` as array length
   (const-context pre-pass); unexported field from same package.
3. `02_pointer_roundtrip`: `*T→Pointer→*T` read/write through the
   pointer; nil `Pointer` round-trip; `==` on two handles to `&x`
   (interning parity, §1.3).
4. `03_traps`: `Pointer→uintptr`, `uintptr→Pointer`, `Add` —
   three `.panic` programs asserting the `UnsafeEscapeException`
   trap (documents D5; never silent).
5. `04_slicedata_roundtrip`: `SliceData` → `Slice(p, n)` on
   `[]byte`; write through the new slice, observe via original;
   nil slice → nil pointer.
6. `05_slice_shapes`: `Slice(&arr[0], 3)` over `[4]int`;
   `Slice(&x, 1)` single; over-long `len` → fail-fast `.panic`
   (not silent truncation).
7. `06_string_roundtrip`: `StringData` → `String(p, n)` content
   equality; `StringData("")` nil (gc-faithful); DIV-US-1 pin: write via the
   original bytes *after* `String()` is not visible (documents the
   copy).
8. `07_sizeof_generic_scopelimited`: `Sizeof` inside generic code →
   `.scopelimited` (`retest: sizes over type parameters`),
   proving the fail-fast fires rather than miscompiling.
9. `08_offsetof_deep`: struct nesting past the wire depth cap →
   `.scopelimited` or export-time scope error pin (offset `-1`
   never leaks).
10. `09_addrof_patterns`: `&s.field`, `&arr[i]`, `&m[k]`(compile
    error control) combined with `Slice`/`String` — the realistic
    composition.

### reflect (12)
1. `00_typeof_table`: `TypeOf` over every kind; `String()` incl.
   `*main.T`, `struct{ A int "json:\"a\"" }`, `[]map[string]*int`,
   `func(int) string`, `chan<- int`; `Kind()` for all.
2. `01_type_queries`: `Bits/Size/Align/Elem/Key/Len/ChanDir/
   NumIn/NumOut/In/Out/IsVariadic/Name/PkgPath/Comparable/
   Implements/AssignableTo/ConvertibleTo` matrix incl. named-vs-
   underlying and ptr-vs-value method sets.
3. `02_struct_fields`: `NumField/Field/FieldByName/FieldByIndex`
   over embedded (value+pointer), tags (`Get/Lookup` found/missing/
   malformed), unexported fields (`PkgPath` set, `CanInterface`
   false).
4. `03_getters`: `Int/Uint/Float/Complex/Bool/String/Bytes` on
   *named* types; wrong-kind getter → `.panic` with gc-exact
   message; getter on zero `Value` → `.panic`.
5. `04_setters`: `New/Elem/Addr/CanSet/CanAddr`; `SetInt/SetString/
   SetBytes/SetMapIndex/Append/AppendSlice/MakeSlice/MakeMap`;
   set-on-unaddressable and set-on-unexported → `.panic`
   (gc-exact messages); `SetZero`.
6. `05_unexported_parity`: `Interface()` on unexported field →
   `.panic` (exact message); `CanInterface()==false`; but
   `DeepEqual` and struct print read it fine (both directions
   pinned).
7. `06_deepequal`: nested structs, cyclic (self-pointer, mutual),
   func values (nil/nil→true, nil/non-nil→false,
   same-func→false), `NaN`, `[]byte(nil)` vs `[]byte{}`,
   maps with unexported fields, `any` chains.
8. `07_call`: `MethodByName`+`Call` (value & pointer receiver,
   missing method → `.panic`), `CallSlice` variadic,
   wrong-arity → `.panic`, nil func → `.panic` (nil dereference).
9. `08_makefunc`: `MakeFunc` adapter (arg transform, multi-result,
   panic inside callback propagates with original message);
   `NewAt`+`unsafe.Pointer` composition.
10. `09_maps_chans`: `MakeMapWithSize`, missing-key invalid
    `Value`, `MapKeys` count, `MapRange` sum (order-free),
    delete-then-absent; `MakeChan/Send/Recv/TrySend/TryRecv/
    Close`, `Select` send/recv/default branches.
11. `10_failfast_pins`: `ArrayOf/StructOf/PtrTo` → `.panic`
    ("needs runtime type synthesis"); `Value.Seq` → `.panic`
    ("needs iter"); `TypeAssert[int]` → `.panic` ("needs
    generics"); `Value.Pointer()` → trap `.panic`.
12. `11_visiblefields_swappe
...[truncated 2172 chars]