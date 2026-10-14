# ADR-0003 — Phase-3 interface dispatch: freeze exception for `ifacemethod` wire nodes

**Date:** 2026-10-06
**Status:** accepted 2026-10-06 — coordinator design (P3-M3 brief).
Spec-first ordering honored: ADR committed before any exporter change.
**Scope:** closes PLAN.md §4 P3-M3 (WS B); GG-0014, GG-0017 (iface slice).
**Normative basis:** `phases/phase1/INTERFACE_FREEZE.md` (v1, in force);
PLAN.md §§2–5 (WS B); SPEC §§3.3 (PIC on itab identity, limit ~3,
megamorphic hash fallback), 3.4 (value model); ADR-0002 (precedent).

## Context

P3-M3 (WS B) lands interface dispatch. The frozen exporter
(`phases/phase1/frontend/export.go`) rejects every interface method call:
`exportMethodCall` → `resolveMethodCall` fails with
`gofrontend: method Write not found on Writer` because `e.methodIndex`
holds only concrete-type methods (verified by probe 2026-10-06).
`InterfaceBoxNode` (frozen) boxes with a null itab, and no itab is ever
constructed. The named-type type-table entries carry `"methods": []` with
the comment "populated by WS D" — this milestone *is* WS D.

## Decision

A single documented exception to the frozen-dependency rule, following the
ADR-0002 precedent (additive wire kinds via the public
`NodeFactory.registerExprKind` hook; freeze §2.3; toolVersion bump per
freeze §5):

1. **Exporter** (`export.go`): in `exportCall`, after the sync-primitive
   interception and before the generic `exportMethodCall` path — when
   `selection.Kind() == types.MethodVal` and the receiver's static type
   has an interface underlying — emit the additive wire node
   `"ifacemethod"` (new expression kind):
   ```json
   {"kind": "ifacemethod", "fields": {
     "recv":   <node>,   // receiver expr; evaluates to GoInterface
     "method": "Write",   // method name
     "iface":  42,        // interface type-table id (method list source)
     "args":   [<node>],  // call args (receiver NOT included)
     "sig":    43,        // method signature type-table id (no receiver),
                         // for call-site arg adaptation (methodref precedent)
     "type":   44         // result type id (null when no results)
   }}
   ```
   The interface method's signature comes from go/types'
   `types.NewMethodSet(iface)`; the call is still fail-fast on variadic
   misuse via the existing `checkVariadicCall`.
   Out of scope (named scope errors, greppable re-test triggers):
   generic (template) methods via interfaces; method values
   (`f := w.Write`) and method expressions (`Writer.Write`) on
   interfaces.
2. **Method tables on type-table entries** (the WS D hook): populate
   `"methods"` on `named` entries as
   `[{"name": "Write", "obj": 17, "recv": "pointer", "index": [],
   "implkey": "Write\x00func(slice(basic:8))(basic:2)"}, ...]`
   (`obj` = funcdecl object id; `recv` = declared receiver kind;
   `index` = receiver-adaptation path: `Selection.Index()` minus its last
   entry — `[]` for direct methods, e.g. `[0]` for a method promoted via
   embedded field 0, `null` for the unsupported value-embedded +
   pointer-receiver shape, which fails fast at itab build with a named
   error if dispatched; `implkey` = method-identity key: method name —
   unexported names qualified with their package path, as gc compares
   them — plus `\x00` plus a structural signature key with aliases
   expanded, so `Write([]uint8)` and `Write([]byte)` share a key. The
   runtime implements check compares `implkey`s, not bare names —
   signature-aware since the R1 P0-1 remediation), and add
   the same field on `pointer` entries (full `*T` method set: value +
   pointer-receiver methods). Computed via `types.NewMethodSet` over the
   named type / pointer-to-named (this covers promoted/embedded methods,
   which `e.methodIndex` lacks); resolved to declarations via the
   existing `funcByObj`. Template methods are skipped (see out-of-scope).
   Additive data on existing fields — the factory's type decoders read
   named fields and ignore the rest (verified: named entries already
   carry `"methods": []`).
3. **Version:** `gofrontend` toolVersion **0.3.0 → 0.4.0** per freeze §5.
   Wire stays `v1` (additive kind + additive data, §2.3). Old consumers
   reject `ifacemethod` as an unknown kind (fail-fast, §2.3).
4. **Java** (`phases/phase3/src/main/java/graalgo/iface/`, new package):
   - `IfaceMethodTable`: static registry, `install(Map<String,
     Map<String, GoFunc>>)` — descriptor name → (method name → GoFunc).
     Installed by the P3 launcher after `factory.build()` resolves
     objIDs through `Program.functions()`.
   - `IfaceDispatchNode extends GoExprNode`: `@Child` receiver,
     `@Children` args; final `methodName`; final `List<String>`
     ifaceMethodNames (for itab construction). Dispatch:
     receiver must be `GoInterface` (nil → Go nil-dereference panic,
     gc parity); itab = built once per (descriptor name, iface id) as an
     immutable object (D6: itabs immutable, speculation needs no
     Assumptions); **PIC on itab identity, limit 3**, megamorphic
     fallback to hash lookup; invocation via `GoCalls.invoke` (the
     `FuncValueCallNode` protocol), receiver's `data` as the method's
     first arg. Every dispatch records
     `InterfaceDispatchProbes.record(itab)` (the M9 re-measurement hook).
     Call-site arg adaptation replicates the factory's
     `adaptArgsForInterfaceParams` using the wire `sig` (public
     `BuildCtx.types`/`typeDescs`; `InterfaceBoxNode` ctor is public).
   - Builder registered via the public `registerExprKind("ifacemethod",
     ...)` hook by the P3 launcher (no `NodeFactory` change).
5. **Descriptor-name keying:** named dynamic types key by
   `GoTypeDescriptor.name()` (canonical via the factory's `NAMED_DESCS`,
   equal to the exporter's `namedTypeName`); pointer dynamic types key by
   the factory's structural-descriptor naming —
   `"pointer#" + Integer.toHexString(structuralKey.hashCode())`
   (`NodeFactory.typeDescriptorFor`: `pointer` is a structural composite,
   so the `k+"#"+typeId` branch is unreachable for it). The launcher
   replicates `structuralKey` exactly (see `DescriptorNames` in P3 code,
   cross-checked by `DescriptorNamesTest` over 14 type-table shapes) and
   computes the same strings from the doc's type table, so exporter and
   runtime agree without a shared-code change.
   **Amendment 2026-10-06:** the original text claimed
   `"pointer#"+typeId`; the Java worker proved this wrong against the
   factory source (red: `expected=pointer#12a82948 actual=pointer#2`) and
   implemented the factory-accurate naming. The Go producer is unaffected
   (it never computes descriptor names).

## Amendment 2026-10-06 (P3-M3 remediation: R1 P0-1, R1 P0-2, R1 P1-2)

Independent code-review remediation
(`reviews/phase3-m3/reviewer1-code.md`). Two confirmed silent
divergences vs gc, both fixed with red-green evidence
(`phases/phase3/hidden_files/m3/redgreen.md`); both changes are additive:

1. **Signature-aware implements check (R1 P0-1).** The type-assertion
   implements check compared method NAMES only: `a.(Writer)` succeeded
   when the dynamic type declared `Write(string)` instead of
   `Write([]byte)` (gc: assertion fails). The exporter now emits an
   `"implkey"` on every method-table entry and every interface method
   entry (decision item 2, updated above); the runtime check
   (`TypeAssertNode.holds`) tests implkey inclusion instead of
   name inclusion. The key computation lives entirely in the exporter
   (go/types is authoritative there); the Java side compares opaque
   strings. A missing `"implkey"` on a method entry is a corrupt 0.4.0
   document — fail fast, never fall back to the name-only comparison
   (the fallback IS the soundness hole). Red-green: `p01.go` prints
   `assert ok` pre-fix vs gc's `assert failed` (RED), `assert failed`
   post-fix (GREEN); positive controls (correct signature with a
   differently-named parameter; `byte`/`uint8`/alias cross-spellings)
   stay `ok`. Pinned by corpus `if11_assert_signature.go`.
2. **Canonical descriptors from the dispatch builder (R1 P0-2).**
   `IfaceDispatchBuilder`'s call-site arg adaptation minted a
   non-canonical `GoTypeDescriptor` on the `ctx.typeDescs` miss path —
   name-correct but not instance-identical — so a type assert, type
   switch, or interface `==` inside the callee on that exact value took
   the wrong branch (repro: `i.M(&T{v:2})` with `x.(*T)` inside printed
   `no`; gc prints `yes-*T`). The miss path is routine, not exceptional:
   structural descriptors (notably every `&T{...}` pointer argument)
   live only in the factory's process-wide structural cache, never in
   the per-function `typeDescs` map. Fix: new public additive accessor
   `NodeFactory.canonicalDescriptorFor(BuildCtx, int, String)` — pure
   delegation to the existing `typeDescriptorFor`, no behavior change to
   any existing path — documented here as part of the factory exception
   (same change class as the WS-D retirement amendment, which already
   modified the frozen factory). The builder uses it instead of minting
   a replica. Red-green: the repro prints `no`/`no` pre-fix (RED),
   `yes-*T`/`yes-*T` post-fix (GREEN, assert + type-switch variants);
   pinned by corpus `if12_callee_type_identity.go`.
3. **Generic instantiation via a non-empty interface (R1 P1-2).** A
   generic instantiation (e.g. `Box[int]`) used where a non-empty
   interface is expected got an empty wire method table (template methods
   are skipped — out of P3-M3 scope), then died at first dispatch with
   the misleading `no method table installed ... (needs gofrontend >=
   0.4.0 WS D method tables)` — misleading because the document IS
   0.4.0. Chosen direction: fail fast with the named P3-M3 scope error
   (supporting instantiation method tables is the re-test trigger; grep
   `templateMethodsSkipped` + `P3-M3 scope`). The exporter marks such
   entries `"templateMethodsSkipped": true`; the frontend fails fast at
   every boxing site (`NodeFactory.maybeConvert`,
   `NodeFactory.adaptArgsForInterfaceParams`, the P3
   `IfaceDispatchBuilder.boxArg`) and — for the `any`-then-assert shape,
   where no boxing site sees the instantiation — at assertion runtime
   via a sentinel in the dynamic method set (`TypeAssertNode.holds`
   throws instead of reporting a silent wrong `false`). Boxing an
   instantiation as the empty interface stays legal. Red-green: `p12.go`
   dies pre-fix with the misleading runtime error (RED); post-fix all
   three box sites and the assert path fail fast with
   `cannot use generic instantiation main.Box[int] as a non-empty
   interface: method tables for instantiated methods are not supported
   (P3-M3 scope)` (GREEN).

## Amendment 2026-10-06 (factory-side WS-D retirements)

End-to-end testing found three fail-fasts in the frozen factory that made
the ADR-0003 corpus unexpressible — each one's named condition ("WS D")
is met by this milestone landing, so they are retired under this ADR
(commit `39b6696`; all previously-working paths unaffected —
phase-1 208/208, phase-2 194/194):

1. `NodeFactory.maybeConvert`: concrete→non-empty-interface conversions
   (`var w Writer = b`) now box via `boxInterface`. Boxing needs only the
   dynamic-type descriptor; itabs are built lazily at dispatch from the
   installed method tables. go/types already verified assignability
   (signatures included) at the conversion site.
2. `NodeFactory.adaptArgsForInterfaceParams`: now follows
   named→underlying (new `isInterfaceType` helper) for interface params.
   The old code skipped named interface params — passing the raw concrete
   value, a silent miscompile. The P3 `IfaceDispatchBuilder` replica gets
   the same fix.
3. `NodeFactory.buildTypeAssert`/`buildCommaOk`: assertions to non-empty
   interfaces (`a.(Writer)`) now allowed; the runtime `implements` check
   compares the dynamic type's method set (per-document map from the type
   table's named/pointer `"methods"`, carried in the node — no global
   state) against the asserted interface's methods. A successful assertion
   to an interface yields the interface value itself.

## Alternatives considered (and rejected)

- **(a) Eager itabs at box sites:** rejected — `InterfaceBoxNode`'s wire
  format is frozen; changing its fields breaks old consumers (§2.3).
- **(b) Exporter-side devirtualization** (resolve the concrete method per
  box site): rejected — the box site does not dominate the call site;
  a `GoInterface` value flows arbitrarily. Only the dispatch site sees
  the dynamic type.
- **(c) Keying the method table by type-table id instead of descriptor
  name:** rejected — the node sees only the `GoTypeDescriptor` object,
  which does not carry the id; names are the stable shared key.
- **(d) p3frontend desugaring without an exporter change:** rejected —
  the merged source still passes through frozen `ExportFile`, which
  cannot resolve interface methods at all (ADR-0002 alternative-(a)
  precedent).

## Verification

- Red-green-first per semantic (`phases/phase3/hidden_files/m3/redgreen.md`):
  PIC dispatch, megamorphic fallback, nil panic, arg adaptation, itab
  identity stability.
- Interface-heavy corpus (`phases/phase3/corpus/iface/`) differential vs
  gc 1.27.1: io.Writer-shaped, sort.Interface, error values, empty-interface
  boxing, megamorphic site (4+ types), nil/typed-nil receivers, embedded
  promotion.
- M9 re-measurement: `InterfaceDispatchProbes` enabled over the corpus;
  per-site itab-identity histograms classified mono/bi/megamorphic.
- Suites stay green: phase-1 (208/208), phase-2 (194/194), p3frontend,
  sync differential (25/25).
- `verifies: GG-0014` on the dispatch node; GG-0017 iface slice activates.

## Consequences

- `gofrontend` 0.4.0 emits `ifacemethod`; only the phase-3 launcher
  consumes it. Phase-1/2 launchers reject such documents as unknown kinds.
- A program using interface method calls compiled by gofrontend < 0.4.0
  still fails with the old error — no silent behavior change.
- Itabs are built lazily per (type, interface); the first dispatch pays
  table construction, steady state is PIC-hit.
