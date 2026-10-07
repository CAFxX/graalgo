# P3-M7: nil-interface NPE — red-green evidence (coordinator fix)

## Bug
Type-asserting (or switching on) a nil Go interface value NPEd the runtime:
a nil interface may flow as Java `null`, and `TypeAssertNode` /
`TypeAssertOkNode` / `SwitchNode` / `Fmt` cast the value to `GoInterface`
unchecked. The host `NullPointerException` escaped as
`graalgo-run: fatal internal error` instead of gc's semantics.

## RED (before fix, commit 5ad29b7^)
`npe.go` (comma-ok assert on nil `error`):
```
graalgo-run: fatal internal error in goroutine: java.lang.NullPointerException:
Cannot invoke "graalgo.values.GoInterface.isNil()" because "<parameter1>" is null
```
## GREEN (after fix, commit 5ad29b7)
- `npe.go` → `ok=false` (gc parity)
- `npe_panic.go` (single-valued assert) → `panic: interface conversion: error is nil, not interface{...}` (gc: same shape; wording nuance carried, see below)
- `npe_switch.go` → `switch-nil` / `typeswitch-nil` (gc parity)

## Fix
`GoInterface.narrow(Object)`: Java null → `GoInterface.NIL` (a typed nil is
never Java null — it carries a descriptor, so the normalization is exact).
Applied at the full error class: TypeAssertNode, TypeAssertOkNode,
SwitchNode (tag + both case-expression casts), Fmt INTERFACE case.
TypeSwitchNode already handled null explicitly (pre-existing proof of the
representation).

## Non-tautological check
The repro fails on the parent commit and passes on the fix commit; the
comma-ok/switch paths exercise `holds()` false-branches that no other test
hit (the errors-sort worker's synthetic `errors` hit the NPE in `Unwrap`).

## Carried divergence
Panic message says `error is nil` where gc says `interface is nil`
(pre-existing `ifaceName` convention in `GoErrors.interfaceConversion`,
unchanged by this fix; no test pins the wording). Re-test trigger: if the
interface-conversion message format is ever normalized to gc.
