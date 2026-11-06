# frontend/v2 — method sets and method dispatch contract

**Status:** implemented for concrete-receiver method values and method
expressions (lane `review-1106-p1-v2-methodsets`, review R-P1-2,
2026-11-06). Interface method dispatch and receiver adaptation are
designed here and fail loudly until implemented — see §7.

**Scope:** the v2 type table's method sets (`named`, `pointer`, `struct`,
`interface` entries) and how the Java factory (`NodeFactoryV2`) dispatches
on them. This is the v2 counterpart of the v1 contract in
[ADR-0003](../dev/adr/ADR-0003-interface-dispatch.md) (P3-M3): the wire shape and the method
identity key are deliberately the SAME as v1's, so the two frontends agree
on what "implements" means. Where v2 cannot yet do what v1 does, this
document says so explicitly instead of emitting a degraded table.

## 1. Wire shape

Every type-table entry whose type HAS a method set carries a `methods`
list. The rule is uniform: the list is the method set of exactly that
type — never of some other type, never empty as a placeholder.

| Entry kind | `methods` content |
|---|---|
| `named` | `types.NewMethodSet(T)`: value-receiver methods plus methods promoted through embedded fields. Pointer-receiver methods are NOT here — they live on the `pointer` entry. |
| `pointer` | `types.NewMethodSet(*T)`: the full set (value + pointer receivers). Present on every pointer entry; empty when the element type declares no methods. |
| `struct` | `types.NewMethodSet(S)`: non-empty only for structs with embedded fields (promoted methods). Plain structs carry `[]`. |
| `interface` | The interface's own methods (see §3). |
| anything else (`basic`, `slice`, `map`, `chan`, `array`, `signature`) | No `methods` field: no method set exists. |

Method entries (named / pointer / struct):

```text
{"name":    string,              // method name, as declared
 "obj":     int,                 // object-table id of the *types.Func,
                                 // or -1 for an embedded-interface redispatch (§5)
 "sig":     int,                 // type-table id of the *types.Signature
 "implkey": string,              // method identity key (§2)
 "recv":    "value" | "pointer", // declared receiver kind
 "index":   [int],               // receiver adaptation path (§4), [] for direct methods
 "addrOf":  bool,                // true when dispatch must take the address
                                 // of the promoted field value (§4)
 "embeddedIface": int}           // present only on redispatch entries (§5):
                                 // type-table id of the embedded interface
```

Interface method entries carry only what an interface method HAS — there
is no funcdecl to point at:

```text
{"name": string, "sig": int, "implkey": string}
```

Interface entries also carry `"embedded": [int]` — the type-table ids of
the embedded types (`NumEmbeddeds` / `EmbeddedType`), in declaration
order. `NumMethods` on an interface is the FLATTENED count (embedded
interfaces' methods included), so `methods` already contains the promoted
methods; `embedded` records where they came from.

## 2. Method identity: `implkey`

An implkey identifies a method by (name, signature) — never by name
alone. Same-name / wrong-signature methods do NOT satisfy an interface
(R1 P0-1 in v1). The algorithm is v1's (`implKeyOf` / `sigKeyOf` /
`typeKeyOf` in `internal/frontend/export.go`), reimplemented in
`internal/frontend/v2/emit/` because v2 is a separate module and must not
import the v1 exporter:

```text
implkey = qualifiedName + "\x00" + sigKey
qualifiedName = pkgPath + "." + name   (unexported methods only; exported
                                        methods use the bare name, as gc does)
sigKey = "func(" params ")(" results ")"   // receiver excluded; names ignored;
                                           // "...T" for the variadic tail
```

Types render structurally with aliases expanded (`types.Unalias`):
named types by qualified name + type arguments (never expanded —
expansion does not terminate on recursive types); basic types by KIND
(`byte` and `uint8` are the same type); interfaces by their sorted method
implkeys. Two methods share an implkey iff gc considers their (name,
signature) pairs identical.

The emitter computes implkeys from `go/types` alone — no string
comparison against the Java side, no package-name special cases.

## 3. What the emitter guarantees

- **Completeness:** every method in `types.NewMethodSet` appears exactly
  once, in `go/types` order (sorted by name). A type never looks
  methodless when it has methods (the R-P1-2 bug), and a method is never
  silently dropped because its funcdecl lives elsewhere: methods of
  imported types and of embedded interfaces are IN the table (with
  `obj` pointing at the method's object, or `-1` for redispatch, §5).
  Dispatch of a method with no funcdecl in this program fails loudly at
  execution with `unresolved method object N` — the method exists, this
  program cannot call it yet (cross-package calls are v2 future work).
- **Struct offsets are real:** field `"offset"` is the gc/amd64 byte
  offset from `types.SizesFor("gc", "amd64").Offsetsof`, pinned in the
  checker (`types.Config.Sizes`) and the emitter from the same source so
  the two can never drift. `-1` means "unknown", emitted only when the
  size cannot be computed: a field type containing a type parameter
  (`go/types` panics on `Sizeof` there; offsets of a generic struct are
  instantiation-dependent anyway) or a value-nesting depth over 16
  (the v1 M10 cap — `Sizeof` is exponential in nesting depth). The
  factory indexes fields by position and never trusts an offset for
  layout; `-1` is never mistaken for 0.
- **Determinism:** method order is `go/types` order; implkeys and offsets
  are pure functions of the checked package.

## 4. Receiver adaptation (emitter-side, mirroring v1)

The factory's `MethodValueNode` binds the receiver expression as-is. Any
adaptation between the receiver expression's type and the method's
declared receiver type is therefore the EMITTER's job — exactly as v1's
`methodRefNode` / `methodValueNode` adapt `recv` before the factory sees
it. The `index` path on a method entry is `Selection.Index()` minus its
last entry: the field path from the receiver to the embedded field
declaring the method (`[]` for direct methods, e.g. `[0]` for a method
promoted via embedded field 0). `addrOf` is true when the method has a
pointer receiver promoted via a value-embedded field: dispatch must take
the address of the promoted field value (gc's implicit `&`).

What v2 supports TODAY: direct methods only — empty `index` path AND
receiver type identical to the declared receiver type. Anything else is
rejected LOUDLY at emit time (`emitSelector` returns an error naming the
method, the receiver type, and the adaptation needed):

- promoted method (`index` non-empty): needs a field-walk adaptation
  the factory cannot build yet (no promoted-field selection in v2);
- implicit address-of (`*T` receiver, `T` value): needs address-of
  machinery v2 does not have (no `&` node, no address-taken analysis);
- implicit deref (`T` receiver, `*T` value): needs a deref node v2 does
  not have.

These are real programs failing with a precise error instead of the old
silent misbuild (the factory used to treat every selector as a field
access). The follow-up lane is: address-taken analysis + `FieldAddrNode`
wiring in the v2 factory + emitter-side adaptation, after which the
fail-loud branches become adaptations. Method expressions (`T.M`) need
no adaptation at the selector — `go/types` already rejects the cases
that would need it (`Colored.Scale` is a type error: a method expression
on a value type needs a value receiver) — but a promoted method
expression (`Colored.Area`, whose func type takes the OUTER type as its
first parameter) is rejected the same way: its `index` is non-empty.

## 5. Embedded-interface promotion (redispatch entries)

A method promoted via an embedded INTERFACE field (e.g. `Addr` on a type
embedding `net.Listener`) has no funcdecl: its method object is the
interface's declaration. The table entry records this honestly instead of
dropping the method:

```text
{"name": "Addr", "obj": -1, "recv": "value", "index": [0],
 "implkey": "...", "addrOf": false, "embeddedIface": <type id of net.Listener>}
```

`obj: -1` means "no GoFunc exists for this entry — dispatch by reading
the embedding path to the interface field value and dispatching the
method on IT". The v2 factory rejects such a selector at BUILD time with
a named error (embedded-interface redispatch is not yet implemented in
v2). The future dispatcher follows the `index` path to the interface
field value and dispatches on it exactly like a direct interface method
call (§6).

## 6. Dispatch contract (Java side)

`NodeFactoryV2` distinguishes selectors by the emitter's `selkind`:

| `selkind` | Factory action |
|---|---|
| `field` | Existing path: `findFieldIndex` + `SelectorNode`. Unchanged. |
| `method` (MethodVal) | Receiver's static type resolves (through `named` → `underlying`) to an interface → BUILD error naming v2 interface method dispatch as unimplemented. Otherwise `MethodValueNode(recv, funcTable, objId)` with `objId` from the selector's `"obj"`. `objId == -1` → BUILD error naming embedded-interface redispatch. |
| `methodexpr` (MethodExpr) | `FuncConstNode(funcTable, objId)` — the plain function value; the caller supplies the receiver as the first argument. `objId == -1` → BUILD error as above. |

`funcTable` is the program's object-id → `GoFunc` table, owned by the
factory and shared (same map instance) by every `MethodValueNode` /
`FuncConstNode` it builds — v1's lazy-resolution design
(`NodeFactory.funcTable`). Resolution is lazy: a node whose `objId` is
absent from the table throws `FrontendException("unresolved method
object N")` when EXECUTED, never silently. The table is populated when
the v2 launcher builds `RootCallTarget`s for the program's funcdecls
(each funcdecl carries its `"obj"`); until then it is empty by
construction and any method-value execution fails loudly with that
message. Building a `RootCallTarget` per funcdecl (the v2 calling
convention: closure slot, result slot, receiver-as-`params[0]`) is a
separate lane — this lane deliberately does NOT wrap bodies in
`GoFuncRootNode`, because a half-wired root would mis-execute instead of
failing loudly.

`buildProgram` validates the type table before building any function:
every `methods` entry has a name, an `obj` ≥ -1 resolving to a `func`
object (unless `-1`), a `sig` resolving to a `signature` entry, a
non-empty `implkey`, a `recv` of `value`/`pointer`, and an `index` list;
no duplicate method names within one set; every interface `embedded`
entry is a valid type id; every struct field `offset` is `-1` or ≥ 0 and
the offsets are non-decreasing (zero-size fields may share an offset).
A malformed table is a `FrontendException`, never a silently
methodless type.

### Interface method dispatch (future — designed, not yet built)

When v2 gains interface values (boxing), dispatch works as follows —
this is the design the table shape above is built for:

1. Boxing a concrete value into an interface installs, alongside the
   dynamic-type descriptor, a method table: for each implkey in the
   interface's `methods` list, the implementation's `obj` from the
   dynamic type's method table (matched BY IMPLKEY, not by name —
   §2). If any implkey has no implementation, boxing fails loudly
   (the value does not implement the interface; gc would have rejected
   the program, so this is unreachable for checked code except via the
   `templateMethodsSkipped`-style gaps, which fail with a named scope
   error instead of a bare `false`).
2. A method call on an interface value looks up the method by its
   position in the interface's `methods` list and invokes the installed
   `GoFunc` with the UNBOXED dynamic value as the receiver (adapting
   via the implementation entry's `index`/`addrOf` exactly as §4).
3. A method VALUE on an interface value (`f := iface.M`) captures the
   same lookup at formation time (the receiver is evaluated once, per
   Go semantics) and yields a `GoFunc` with the dynamic receiver bound.

## 7. Open items (not papering over — named gaps)

1. **Receiver adaptation** (§4): implicit `&` / `*` and promoted-method
   field walks. Needs: address-taken analysis in the v2 factory,
   `FieldAddrNode` wiring, emitter-side adaptation in `emitSelector`.
   Until then the emitter fails loudly on those selections.
2. **Interface method dispatch** (§6): needs interface value boxing in
   v2 (no `InterfaceBoxNode` use yet) plus the method-table install at
   boxing time. Until then the factory fails loudly at BUILD time.
3. **Embedded-interface redispatch** (§5): needs the §6 dispatcher plus
   the embedding-path read. Until then the factory fails loudly at
   BUILD time on `obj == -1` selectors.
4. **v2 launcher + `RootCallTarget`s**: `funcTable` population. Until
   then method values build correctly but throw `unresolved method
   object N` when executed.
5. **Generic instantiation method tables**: a method of an instantiated
   generic type (e.g. `List[int].Get`) has an object id with no funcdecl
   in this program (the template's funcdecl is a different object).
   v1 monomorphizes these on demand; v2 has no monomorphization.
   Execution fails loudly with `unresolved method object N`. Owned by
   the generics track (R-P1-1).

## 8. References

- [frontend-v2.md](frontend-v2.md) — the v2 pipeline (this contract is
  §3 of that document's Stage 3).
- [ADR-0003](../dev/adr/ADR-0003-interface-dispatch.md) — v1 method identity (`implkey`) and
  the implements check; the algorithm reused here.
- `internal/frontend/export.go` (`methodTableEntries`,
  `methodTableEntry`, `embeddedInterfacePromotion`, `implKeyOf`,
  `sigKeyOf`, `typeKeyOf`, struct offset computation) — the v1
  precedent this contract mirrors; v2 reimplements the general
  mechanism in its own module rather than importing the v1 exporter.
- `internal/backend/truffle/src/main/java/graalgo/frontend/NodeFactory.java`
  (`buildMethodValue`, `methodref` branch, `interfaceMethodImplKeys`,
  `dynamicMethodSets`) — the v1 factory side.
- `internal/backend/truffle/src/main/java/graalgo/frontend/MethodValueNode.java`,
  `FuncConstNode.java` — the nodes v2 reuses for dispatch.
