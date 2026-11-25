# P3-M3 code review — interface dispatch (ADR-0003)

**Reviewer:** independent code-review subagent
**Date:** 2026-10-06
**Snapshot reviewed:** `1bc1f36..0fe5b6c` (16 commits; live tree verified at `0fe5b6c` with no
modifications under `phases/` — empirical runs below are against the reviewed code)
**Normative:** `phases/phase3/adr/ADR-0003-interface-dispatch.md` incl. both amendments
**Verdict: NO-GO** — two confirmed P0 silent divergences (empirically proven below) must be
fixed before sign-off. Everything else is in good shape.

## Verdict rationale

The core mechanism (exporter wire format, PIC dispatch, itab construction, WS-D retirements)
is sound and well-evidenced. But I found two **P0** bugs where valid programs silently diverge
from gc — one in the type-assertion implements check, one in the builder's descriptor replica.
Both are proven by minimal repros (gc vs graalgo outputs shown). P1s are fail-fast parity gaps.
Fix directions are given per finding; nothing was modified (review only).

## Independently re-verified (all green)

1. **Differential corpus:** re-ran `phases/phase3/corpus/iface/run_differential.sh` myself —
   **11/11 byte-identical** vs gc 1.27.1 (stdout, exit codes, panic first-lines).
2. **Phase-3 unit tests:** `make test` → **17/17 green** (redgreen.md claims 13; 4 tests added
   since: promoted-receiver adaptation ×2, direct-receiver, null-index marking).
3. **Go exporter tests:** `go test -run 'TestIface|TestToolVersion'` → ok.
4. **Extra probes I ran beyond the corpus** (all gc-identical): anonymous-interface receivers
   + assertion to anonymous non-empty interface; generic interface `Getter[int]` (type params
   correctly substituted in the wire sig); interface→interface conversions (implicit and
   explicit); named-interface call-site boxing (`tee(w Writer, …)` with concrete arg).

## P0 — must fix before sign-off

### P0-1. Type assertion to a non-empty interface checks method NAMES only, not signatures
`phases/phase1/src/main/java/graalgo/frontend/TypeAssertNode.java:75-89`
(`holds` 4-arg overload). The runtime implements check is
`dynamicMethodSets.get(name).containsAll(assertedIfaceMethods)` — pure name-set inclusion.
For assertions from `any` (or a wider interface) there is no static check — go/types cannot
verify the dynamic type — so this runtime check is the *only* gate, and it is unsound:

```go
type Writer interface{ Write([]byte) int }
type Bad struct{}
func (b Bad) Write(s string) int { return 1 }   // same name, WRONG signature
func main() {
    var a any = Bad{}
    if _, ok := a.(Writer); ok { fmt.Println("assert ok (gc: NOT ok)") } else { fmt.Println("assert failed (gc parity)") }
}
```
gc prints `assert failed (gc parity)`; graalgo prints **`assert ok (gc: NOT ok)`** — silent
divergence of the assertion boolean on a valid program. (If the program then *calls* the
method it dispatches `Bad.Write(string)` with a `[]byte` arg — the boolean itself is already
wrong.) The wire already carries everything needed for a sound check: interface entries have
per-method `sig` ids, method-table entries have `obj` ids resolving to funcdecls with their
own `sig`. **Fix direction:** make the check signature-aware — e.g. at build time precompute
per-descriptor the set of (interface id → implemented?) using structural signature comparison
over the type table (`NodeFactory` has `ctx.types`; `dynamicMethodSets` is already built
per-build), turning the runtime check into a map lookup. Note the itab-build path is *not*
affected the same way (boxing sites had static assignability verified by go/types), so the
hole is specific to the assertion path.

### P0-2. `IfaceDispatchBuilder` mints non-canonical descriptors → silent wrong-branch in callee
`phases/phase3/src/main/java/graalgo/iface/IfaceDispatchBuilder.java:219,240`
(`replicaDescriptor`). On the `ctx.typeDescs` miss path the builder creates
`new GoTypeDescriptor(name, dataKind, false)` — name-correct but **not instance-identical**
to the factory's canonical descriptor. Type assertions, type switches, and interface `==`
all compare descriptors by **identity**:
- `TypeAssertNode.java:89` (`gi.descriptor() == asserted`)
- `SwitchNode.java:135,141,149`
- `BinaryOpNode.java:272` (interface equality)

Repro (assertion variant):
```go
type I interface{ M(x any) string }
type T struct{ v int }
func (t T) M(x any) string {
    if _, ok := x.(*T); ok { return "yes-*T" }
    return "no"
}
func main() { var i I = T{v: 1}; fmt.Println(i.M(&T{v: 2})) }
```
gc prints `yes-*T`; graalgo prints **`no`**. The type-switch variant (`switch x.(type) {
case *T: }`) diverges identically (proven). The miss path is easy to hit — any arg type not
otherwise interned in the caller's `BuildCtx` (here `*T`, since only `T` was interned).
The class javadoc *acknowledges* this ("a type switch/assert inside the callee on that exact
value could miss") but ships the known-wrong behavior — documenting without fixing, against
his standing principle. **Fix direction:** add a public additive accessor on `NodeFactory`
to the process-wide `NAMED_DESCS`/`STRUCTURAL_DESCS` caches (same change class as the
ADR-0003 amendment, which already modified the frozen factory) and use it in
`replicaDescriptor` instead of `new GoTypeDescriptor(...)`. Patching the comparison sites
to name-equality is the weaker alternative.

## P1 — parity gaps, fail-fast (not silent)

### P1-1. Alias-of-interface receiver fails export with an "exporter bug" message
`phases/phase1/frontend/export.go:4320-4328` (`exportIfaceMethodCall`). `recvIface` accepts
aliases (via `Underlying()`), but the `iface`-id selection only handles `*types.Named` /
`*types.Interface`:
```go
type Writer interface{ Write([]byte) int }
type W = Writer
var w W = &Buf{}
w.Write(...)  // gofrontend: receiver static type main.W is not an interface (exporter bug)
```
Valid program, build-time fail-fast with a message that blames the exporter for a user-valid
construct. Fix: unwrap the alias (`types.Unalias`, exists since Go 1.22) before the
Named/Interface switch — or take the id of the underlying interface entry. Note the
factory-side `isInterfaceType`/`dynamicMethodSets`/`interfaceMethodNames` all resolve
aliases correctly; only this exporter switch misses it.

### P1-2. Generic instantiation implementing a non-generic interface fails at first dispatch
`phases/phase1/frontend/export.go:4130` (`methodTableEntries`):
`if !ok || fi.isTemplate { continue }` skips the instantiated method, so `Box[int]` gets
wire `methods: []` (verified empirically). Then:
```go
type Getter interface{ Get() int }
type Box[T any] struct{ v T }
func (b Box[T]) Get() int { return 42 }
var g Getter = Box[int]{v: 7}
println(g.Get())   // gc: 42; graalgo: FrontendException at first dispatch:
                   // "interface dispatch: no method table installed for dynamic type
                   //  "main.Box[int]" (needs gofrontend >= 0.4.0 WS D method tables)"
```
Valid program, runtime fail-fast — but the message is misleading (it *is* 0.4.0 output; the
real cause is the template skip), and the ADR's "generic methods via interfaces" named scope
error never fires for this shape (only for generic methods *in the interface's own* method
set, cf. `TestIfaceGenericMethodScopeError`). Either support instantiated methods in the
table (resolve through the instantiation's funcID) or fail fast at export with the named
P3-M3 scope error.

## P2 — robustness, docs, test gaps

1. **M9 classification vs PIC semantics.** `hidden_files/m3/run_m9.py:56-58,98` labels
   `itabs >= 3` "megamorphic", but the PIC has limit 3 — the megamorphic fallback engages
   only on the **4th** distinct itab. Sites with exactly 3 itabs (if01, mp04, mp09 ×2, mp10)
   never overflow the PIC, yet the report headline says "22.6% megamorphic". Raw counts are
   correct; only the true-overflow sites (if05: 4, mp03: 6) are megamorphic in the
   implementation's sense. Suggest `1=mono, 2-3=poly(PIC-resident), ≥4=mega`.
2. **`freshSigID` breaks the `types`/`typeList` parallelism invariant.**
   `export.go:2374` appends to `e.types` without `e.typeList`. Currently benign — it only
   runs during body export, after `recomputeMethodTables` (the sole `typeList` consumer,
   `:4088`) — but any future pre-recompute caller would silently misalign method tables.
   Append a placeholder or assert the invariant.
3. **`mentionsTypeParam` misses `*types.Alias`/`*types.Union`** (`export.go`, P3-M3 section).
   Unions can't appear in method signatures today; aliases could in principle smuggle a type
   parameter past the fail-fast. One-line-each hardening.
4. **Dead code:** `NodeFactory.maybeConvert`'s `from == GoKind.INTERFACE` inner throw
   ("interface-to-interface conversion needs method tables") is unreachable —
   `GoKind.transparent(INTERFACE, INTERFACE)` returns true first (verified: interface→interface
   conversions work end-to-end). The 2-arg `TypeAssertNode.holds` overload (`:64-66`) also has
   no callers anymore.
5. **Doc gap:** `phases/phase1/docs/frontend-v1.md` documents `methods` as
   `{name, obj, recv}` but omits `index` (including the JSON-null unsupported marking).
6. **Conservative fail-fast scope:** `IfaceMethodTable.buildItab` throws on an unsupported
   promotion (pointer receiver via value-embedded field) for *every* method in the
   interface — a program calling only supported methods through the interface still fails at
   first dispatch. Fail-fast with a named error (good), but broader than the exporter's
   "only if the method is actually dispatched" comment implies. Per-method lazy itab
   entries would narrow it.
7. **`megamorphicTransitions++`** (`IfaceDispatchNode.java`) is a non-atomic increment of a
   volatile long — can lose counts under races. Observability only; fine, but say so.
8. **Red-green Mutation C caveat:** swapping two methods' GoFuncs crashed via arity
   mismatch (`ArrayIndexOutOfBoundsException`) — proving "wrong-method cannot be silent"
   only for differing arities. A same-arity swap is untested; the name→target mapping comes
   straight from go/types by name, so residual risk is low, but the claim should be scoped.
9. **`IfaceDispatchBuilder` javadoc** understates the miss-path frequency ("only triggers
   for … never otherwise interned") — P0-2's repro shows it triggers for ordinary `&T{…}`
   args.

## What is solid (no findings)

- **Wire format & exporter path:** `recvIface` discriminator (incl. the TypeParam exclusion
  from commit `2727ee4` — bound-method regression correctly avoided); `exportIfaceMethodCall`
  iface-id selection (underlying interface id, pinned by `TestIfaceMethodCallExports`
  regression test); method-expression/method-value scope errors; `enumerateInstantiations` /
  `findNested` skipping; variadic fail-fast parity with concrete calls; generic-interface
  (`Getter[int]`) substitution correct on the wire.
- **Method tables:** `NewMethodSet`-based computation covers promoted/embedded methods;
  `recv` (value/pointer) correct; `index` path semantics (`Selection.Index()` minus last)
  correct incl. multi-level paths; `embeddedFieldType` unsupported-marking sound
  (value-embedded + pointer receiver); `recomputeMethodTables` correctly repairs
  pre-`funcByObj` entries (incl. the duplicate-pointer-entry case noted in
  `TestIfaceMethodTablePromotedIndex`); entries sorted by name for determinism.
- **WS-D retirements:** `maybeConvert` (concrete→non-empty-interface boxing; typed-nil no
  longer collapses — `(*Buf)(nil)` boxes to descriptor+null data, proven by if07);
  `adaptArgsForInterfaceParams` named-interface fix (factory) faithfully replicated in the
  builder (if10 covers both); `buildTypeAssert`/`buildCommaOk` to non-empty interfaces
  (name-keyed check — sound *except* P0-1's signature hole); empty-interface paths unchanged.
- **`DescriptorNames` replica:** deviation from ADR-0003 D5 correctly identified and amended;
  pinned by `DescriptorNamesTest` over 14 shapes; red-green proved the test catches the
  ADR-literal error.
- **P3SyncMain:** builder registered before `buildProgram`; tables installed after build
  via `Program.functions()` with fail-fast on dangling obj ids; install on every build
  (no stale tables); install happens before scheduler start, matching `IfaceMethodTable`'s
  documented lifecycle.
- **Docs:** `frontend-v1.md` and ADR-0003 (both amendments) updated in the same change —
  consistent with the code as reviewed (modulo P2-5).

## PIC / megamorphic correctness — confidence: HIGH

- **Itab identity is stable:** cached per `(descriptorName, ifaceId)` in a
  `ConcurrentHashMap`; `buildItab` is deterministic given the installed table; `IfaceItab`
  is immutable with no `equals` override (identity semantics). PIC-on-itab-identity is sound.
- **Protocol:** volatile PIC slots, synchronized miss path with recheck-under-lock,
  megamorphic `HashMap` written and read under the same lock. `computeIfAbsent` passes the
  volatile table as a snapshot; the documented lifecycle (install before goroutines start,
  verified in `P3SyncMain` ordering) excludes install-vs-dispatch races.
- **Tests:** `testPicHit` (1 itab build, receiver-as-params[0], arg order), 
...[truncated 1062 chars]
