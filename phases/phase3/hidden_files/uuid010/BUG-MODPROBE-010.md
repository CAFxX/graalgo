# BUG-MODPROBE-010: Multi-result call as sole function argument not unpacked

## Status
FIXED (frontend) — pending JVM differential verification (memory gate).

## Reproducer
From uuid case06 (`uuid.Must(mustText(...))`):
```
ArrayIndexOutOfBoundsException: Index 2 out of bounds for length 2
    at GoFuncRootNode.execute:119
```

Minimal reproducer (`repro_min.go` in this directory):
```go
package main
import "fmt"
func two() (int, int) { return 3, 4 }
func sum(a, b int) int { return a + b }
func main() {
    fmt.Println(sum(two()))  // Go: binds two()'s results to sum's params
    fmt.Println(two())       // Go: variadic spread
}
```

## Root Cause
Go spec: `f(g())` where `g()` returns multiple values binds g's results to
f's params in order (g() must be the sole argument; type checker enforces).

The frontend's `exportCallArgs` (phases/phase1/frontend/export.go) exported
each argument as-is, with no handling for multi-result calls in argument
position. The pair-valued call node was passed as a SINGLE argument to `f`,
so the runtime's `GoFuncRootNode.execute:119` (`Object arg = args[i+1]`)
found the args array too short → AIOOBE.

The existing `multiResultHelperArgs` (mrdesugar_m5.go) already implemented
the hoisting logic, but was only wired into the MR-call desugar path (for
an MR call's OWN args), not the general call path. The `hoistNestedMRExpr`
AST transform only covered disk-package files, never main-package user code.

## Fix
General frontend fix (no module-specific code, no stdlib changes):

1. **`exporter.mrHoistPre []*Node`** (export.go): side channel for temp
   vardecls hoisted from MR calls in argument position.

2. **`exportCallArgs`** (export.go): for each arg that is a `*ast.CallExpr`
   classified by `classifyMultiResultCall` as `mrUser` or `mrFuncField`
   (user-defined MR only — synthetic M5/M7 kinds keep their pinned
   fail-fast per TestMultiResultFailFastKept/GG-0017), hoist via
   `hoistMRCallArg` and spread the pair fields as separate args.

3. **`hoistMRCallArg`** (mrdesugar_m5.go, new): exports the inner MR call,
   creates a temp var (`__p5mr_N`), emits `tmp := <call>` to preStmts,
   returns pair-field selectors. Handles `mrUser` (via `exportCall`),
   `mrFuncField` (via `funcfieldcall` node), with defensive scope errors
   for `mrIfaceMethod` and inlining for `mrCacheGet`.

4. **`exportStmt` wrapper** (export.go): renamed existing to
   `exportStmtInner`; wrapper flushes only preStmts added during its own
   execution (length-tracked, so nested statements flush correctly) by
   wrapping the statement in a block with hoisted vardecls first.

5. **Package-level safeguard** (export.go, `exportPackageVars`): MR calls
   in argument position at package level (`var x = f(g())`) fail fast
   with a scope error (no statement context for hoisting) rather than
   emitting wrong code.

## Verification
- [ ] Go vet clean on frontend package.
- [ ] `TestMultiResultFailFastKept` passes (M5/M7 fail-fast preserved).
- [ ] Phase1 frontend unit tests: 7 pre-existing failures confirmed
      identical at base (037af0c) and with fix — 0 NEW failures.
- [ ] Minimal reproducer + battery (9 cases: fixed arity, variadic,
      3-results, error result, nested MR, method values, return position)
      byte-identical vs gc 1.27.1, gmp=1 and gmp=4. [PENDING: memory gate]
- [ ] uuid case06 passes. [PENDING: memory gate]
- [ ] arrdispatch 89-line battery re-run. [PENDING: memory gate]

## Files Changed
- `phases/phase1/frontend/export.go`: exporter struct field, exportCallArgs,
  exportStmt wrapper, exportPackageVars safeguard.
- `phases/phase1/frontend/mrdesugar_m5.go`: hoistMRCallArg function.

## Test Files
- `repro_min.go`: minimal reproducer.
- `battery.go`: 9-case battery (gc baseline verified).
