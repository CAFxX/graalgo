# ROOT-CAUSE.md — red-master fix lane fix-redmaster-snapshot-56a3dd6-r2

## STEP 3 — RUNTIME ROOT CAUSE (definitive, verified 2026-10-17 ~08:00–08:30 CEST)

### The runtime failure is NOT a dispatch bug — it is a broken unsafe pattern in `internal/abi.TypeOf`

**Definitive evidence** (Java stack traces captured via temporary
`SchedulerCore.recordCrash` stack-print, reverted after):

Both failure modes share the IDENTICAL stack shape:
```
java.lang.ClassCastException: GoString → GoStruct  (tip = 9105181e+98bb0ce9)
java.lang.NullPointerException: GoStruct.descriptor() on null  (9105181e alone)
    at graalgo.frontend.SelectorNode.readFieldPath(SelectorNode.java:62)
    at graalgo.frontend.SelectorNode.read(SelectorNode.java:99)
    at graalgo.frontend.SelectorNode.executeObject(SelectorNode.java:143)
    at graalgo.p3unsafe.UnsafeFromPtrNode.executeLong(UnsafeFromPtrNode.java:43)
    ... (unsafe.Pointer round-trip)
    at graalgo.p3unsafe.UnsafeToPtrNode.executeObject(UnsafeToPtrNode.java:29)
    ... (function call chain)
    at graalgo.frontend.GlobalWriteNode.executeVoid  (package-level var init)
```

**Minimal repro** (`~/workspace/tmp-fix-redmaster-snapshot-56a3dd6-r2/repro-unsafe.go`):
```go
type EmptyInterface struct { Type unsafe.Pointer; Word unsafe.Pointer }
func TypeOf(a any) unsafe.Pointer {
    eface := *(*EmptyInterface)(unsafe.Pointer(&a))  // <-- THE BROKEN PATTERN
    return eface.Type
}
func main() {
    var s any = "hello"
    println(TypeOf(s) == nil)  // CCE: GoString → GoStruct at .Type selector
}
```
Reproduces the EXACT CCE at the EXACT SelectorNode location. The pattern
`*(*EmptyInterface)(unsafe.Pointer(&a))` — taking the address of an interface
value and reinterpreting it as a 2-field struct — does not work in the
graalgo backend. The dereference yields the interface's DATA (or null),
not a struct with Type/Word fields.

**The real `internal/abi.TypeOf`** (Go 1.27.1 `src/internal/abi/type.go:182`):
```go
func TypeOf(a any) *Type {
    eface := *(*EmptyInterface)(unsafe.Pointer(&a))  // BROKEN in graalgo
    return (*Type)(NoEscape(unsafe.Pointer(eface.Type)))
}
```
This breaks `reflectlite.TypeOf` → `internal/reflectlite.TypeOf` → `abi.TypeOf`,
which breaks every package init that uses it:
- 9105181e: `errors` package init (`var errorType =
  reflectlite.TypeOf((*error)(nil)).Elem()`) → NPE (deref yields null for
  nil interface data).
- Tip (+98bb0ce9): errors init is lazy (deferred), but ANOTHER init calls
  `abi.TypeOf` (via `reflectlite.TypeOf`/`reflect.TypeOf`) → CCE (deref
  yields the interface's data, a GoString).

### Triage of 98bb0ce9 (definitive)

| Part | Claim | Verdict |
|------|-------|---------|
| Part 1: itab stub→helper mapping | Fixes "unknown function object id" → dispatch yields null → NPE | **NOT THE FIX.** The NPE/CCE is in `abi.TypeOf`'s unsafe pattern, NOT in interface dispatch of disk-MR methods. The itab warnings are a red herring (those methods aren't on the crashing path). Part 1 changes the failure mode (NPE→CCE) by altering init order, but does not address the root cause. DO NOT MERGE. |
| Part 2: lazy errorType | Defers errors init crash | **Valid mitigation, insufficient alone.** Defers the `errors` init crash, but the underlying `abi.TypeOf` bug still crashes other inits. Useful as a stopgap, but the real fix must make `abi.TypeOf` work. |

### Recommended fix direction (for a dedicated lane)

The `*(*EmptyInterface)(unsafe.Pointer(&a))` pattern is fundamentally
incompatible with graalgo's `GoInterface` representation. Options:
1. **Intrinsify `internal/abi.TypeOf`**: Replace its body with a backend
   intrinsic that directly extracts the type from the `GoInterface`
   descriptor, bypassing unsafe entirely. Requires constructing a valid
   `*abi.Type` from the descriptor.
2. **Fix the backend unsafe emulation**: Materialize a GoStruct when
   dereferencing a pointer-to-interface as a struct type. Complex and fragile.

This is a significant work item — NOT to be attempted in this lane.

## STEP 2 — BUILD ROOT CAUSE (verified by local reproduction, 2026-10-17 ~07:20–07:35 CEST)

### Observed failure (from CI job logs, FAILURE-LOG.md)
Master 56a3dd68: `make all` fails at `Makefile:263 snapshot-check` — all 38
corpus programs drift with `buildOk: want=true got=false` (byte-identical
across 5 CI runs). Toolchain javac succeeds on CI; failure is at
`graalgo-sync-run --check` (build-only) time.

### Reproduction (controlled experiment, same Java backend classes)
Built gofrontend at base (56a3dd68) and at tip; ran
`graalgo-sync-run --check corpus/01_hello/main.go` with each via
GRAALGO_GOFRONTEND:
- BASE: `graalgo-sync-run: internal error building AST: assign.fields: lhs/rhs length mismatch`, exit=1.
  **REPRODUCES the CI failure exactly.**
- TIP (9105181e+98bb0ce9): exit=0. **Fixes it.**
- 9105181e ALONE (without 98bb0ce9): exit=0. **9105181e is sufficient.**

### Root cause
`fmt/scan.go:189` contains a 3-target assignment from a multi-result
interface-method call: `r, size, err = s.rs.ReadRune()`. Every fmt-importing
program (49/50 corpus programs; all 38 snapshot programs) compiles fmt, so
all of them hit this.

The frontend's `classifyMultiResultCall` (internal/frontend/mrdesugar_m5.go)
had an asymmetry: the CALL-EXPRESSION path (exportIfaceMethodCall) chained
the general interface-MR classifier `classifyUserIfaceMultiResultCall` after
the table classifiers, but the ASSIGNMENT path's recvIface arm did NOT.
Result: the N-target assignment kept the raw pair-typed ifacemethod node as
the single RHS. The v1 backend (NodeFactory.java:4525) then threw
`assign.fields: lhs/rhs length mismatch` (3 LHS vs 1 RHS) → buildOk=false
for every program.

### The fix (9105181e) — VERIFIED root-cause fix for the BUILD
Chains `classifyUserIfaceMultiResultCall` after the table classifiers in the
recvIface arm of classifyMultiResultCall, mirroring the call-expression
path. General mechanism (existing classifier + existing
desugarIfaceMultiResult/hoistMRCallArg machinery); no package-name special
cases; no bridges. Regression test `internal/frontend/iface_mr_ntarget_test.go`
verified red-green: fails on base ("found 2 mismatched assign/vardecl nodes
with raw ifacemethod RHS"), passes on tip. CI on 9105181e (run 39008452595):
build phase now PASSES (no more `buildOk: want=true got=false`); all 38
programs now fail at RUNTIME with `exitCode: want=0 got=1` — a SEPARATE bug.
KEEP 9105181e.

## Runtime failure (SEPARATE bug, needs dedicated lane)

After 9105181e fixes the build, all 38 programs build but fail at runtime
(exitCode=1). Local repro with 9105181e-only frontend:
`graalgo-run: fatal internal error in goroutine: java.lang.NullPointerException:
Cannot invoke "graalgo.values.GoStruct.descriptor()" because "<local8>" is null`.

With 98bb0ce9 added: `java.lang.ClassCastException: class graalgo.values.GoString
cannot be cast to class graalgo.values.GoStruct`. Different failure, still broken.

The itab/dispatch mechanism has issues with disk-MR stubs ("unknown function
object id" warnings persist). 98bb0ce9 is NOT the fix. This needs a dedicated
runtime-dispatch investigation lane — do NOT merge 98bb0ce9 as-is.

## Mapping of candidate fixes in flight (vs the OBSERVED failure)

| Branch / commit | Claim | Evidence-based verdict |
|---|---|---|
| r2 (mine) 9105181e "Fix N-target multi-result interface method assignment desugar" | fixes universal buildOk=false | VERIFIED: root-cause fix. Base fails with the exact CI error; +9105181e alone succeeds. KEEP. |
| r2 (mine) 98bb0ce9 "Fix method table itab for disk-MR stubs; lazy-init errors.errorType" | (1) itab entries for disk-MR stubs → runtime NPE on iface dispatch; (2) errors package-init NPE via reflectlite.TypeOf | NOT THE FIX: with 9105181e alone, runtime fails with NPE (`GoStruct.descriptor()` on null, exit=1); with 98bb0ce9 added, runtime fails DIFFERENTLY (`ClassCastException: GoString cannot be cast to GoStruct`, exit=1). 98bb0ce9 changes the failure mode but does not fix it. The runtime dispatch bug is deeper and needs dedicated investigation. DO NOT MERGE 98bb0ce9 as a runtime fix. |
| fix-multival-rhs c6cfd945 + de6d37ab | MR RHS desugar | Adjacent area, but the observed failure's root cause is the missing classifier chain (9105181e), verified in isolation. Not needed for this failure. |
| fix-redmaster-conformance-abifuncpc-r3: 54895984 + a1d45bb1 + cde90675 | iface MR classifier wiring; abifuncpc wire kind | a1d45bb1 overlaps 9105181e's territory — superseded by the verified 9105181e. cde90675 addresses a different (conformance) failure. |
| fix-redmaster-snapshot-drift-r5: 6812e97e + 7c67d8f1 | same N-target fix | STALE BASE (517e7435, predates the regression window). 9105181e is the cherry-pick onto current master. Superseded. |
| fix-redmaster-snapshot-build-r2: 0fc36e76 | PKG-082 addressability | VETOED by independent re-reviewer (2026-10-17): does NOT address the observed red-master failure. CI evidence: 38-program drift byte-identical across 954b758b (run 38908979469), 061f7e48 (run 38937963217), 56a3dd68 (run 38992323081), AND 0fc36e76 (run 38992997320) — the fix changes nothing about the drift. The claimed target `&mt.(*rtype).t` was already fixed on master by 061f7e48 (stale diagnosis); 0fc36e76's only real delta is the latent `&f().N` bug (genuine, red at base, but orthogonal to the drift). VETO on the claim, not the code. **PKG-082/isAddressable-pointer-base is OFF the red-master critical path — no fix effort there for the red-master mandate.** |

### Bridge watch (this lane)
- 9105181e: no new synth_*.go, no import-overlay allow-lists, no package-name-keyed conditionals (verified in diff: single classifier-chain call + test file). CLEAN.
- No other commits on this branch.
